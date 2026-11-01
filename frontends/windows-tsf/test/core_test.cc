// SPDX-License-Identifier: GPL-3.0-or-later
#include "core.h"
#include "keys.h"
#include "ring.h"
#include "scope.h"
#include "watchdog.h"
#include "recovery.h"
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <memory>
#include <new>
#include <sstream>
#include <thread>
#include <vector>
using namespace shixin;
static std::atomic<size_t> allocations{0};
void *operator new(std::size_t n) {
  ++allocations;
  if (auto p = std::malloc(n ? n : 1))
    return p;
  throw std::bad_alloc();
}
void operator delete(void *p) noexcept { std::free(p); }
void operator delete(void *p, std::size_t) noexcept { std::free(p); }
#define CHECK(x)                                                               \
  do {                                                                         \
    if (!(x)) {                                                                \
      std::cerr << __LINE__ << ": " #x "\n";                                   \
      std::abort();                                                            \
    }                                                                          \
  } while (0)
static std::vector<char> unhex(std::string_view s) {
  std::vector<char> b;
  for (size_t i = 0; i < s.size(); i += 2)
    b.push_back(char(std::stoul(std::string(s.substr(i, 2)), nullptr, 16)));
  return b;
}
static void golden() {
  std::ifstream file(std::filesystem::u8path(SHIXIN_GOLDEN));
  CHECK(file.good());
  std::string line;
  while (std::getline(file, line)) {
    if (line.empty() || line[0] == '#')
      continue;
    std::istringstream in(line);
    std::string name, hex;
    in >> name >> hex;
    auto b = unhex(hex);
    std::string_view view(b.data(), b.size());
    uint32_t n;
    CHECK(frame_size(view.substr(0, 4), n));
    CHECK(n + 4 == view.size());
    auto type = uint8_t(view[4]);
    auto payload = view.substr(5);
    Frame encoded;
    if (name == "hello") {
      hello(encoded, 1, 1234, "com.apple.TextEdit");
      CHECK(encoded.view() == view);
    }
    if (name == "key") {
      key(encoded, 1, 42, 'a', 0, 0);
      CHECK(encoded.view() == view);
    }
    if (name == "key_sync") {
      key(encoded, 1, 43, 'A', 1, 1);
      CHECK(encoded.view() == view);
    }
    if (name == "focus_in") {
      focus_in(encoded, 1, true, "com.apple.Terminal");
      CHECK(encoded.view() == view);
    }
    if (name == "focus_out") {
      focus_out(encoded, 1);
      CHECK(encoded.view() == view);
    }
    if (name == "caret") {
      caret(encoded, 1, -1920, 100, -1918, 124);
      CHECK(encoded.view() == view);
    }
    if (name == "select") {
      select(encoded, 1, 44, 2);
      CHECK(encoded.view() == view);
    }
    if (type == ResultType) {
      Result r;
      CHECK(r.decode(payload));
      CHECK(r.ctx == 1 && r.seq == 42 && r.flags == 3 && r.policy == 2);
      CHECK(r.preedit == "shi xin" && r.count == 2 &&
            r.candidates[0].text == "拾新" && r.candidates[1].comment == "shi");
      for (size_t i = 0; i < payload.size(); ++i) {
        Result truncated;
        CHECK(!truncated.decode(payload.substr(0, i)));
      }
      CHECK(r.decode(std::string(payload) + "future"));
      // Re-decode into storage that outlives the aliased fields.
      CHECK(r.decode(payload));
      display(encoded, r);
      CHECK(encoded.view() == view);
      r.commit = "private commit";
      display(encoded, r);
      Result visible;
      CHECK(visible.decode(encoded.payload()));
      CHECK(visible.commit.empty());
    }
    if (type == PolicyType) {
      Policy p;
      auto t = std::make_unique<Policy::Tables>();
      CHECK(p.decode(payload, *t));
      CHECK(p.id == 3 && p.lookup(*t, 'a', 0) == 6);
      CHECK(p.lookup(*t, 0xff0d, 0) == 10 && p.lookup(*t, '`', kControl) == 11);
      CHECK(p.lookup(*t, 0xffe1, kRelease) == Notify &&
            p.lookup(*t, 'a', kRelease) == Pass);
      for (size_t i = 0; i < payload.size(); ++i) {
        Policy truncated;
        CHECK(!truncated.decode(payload.substr(0, i), *t));
      }
      CHECK(p.decode(std::string(payload) + "future", *t));
    }
  }
}
struct Fake : Host {
  Core &c;
  uint64_t tick = 0;
  bool reply = true, full = false, text = true, refocus = false,
       focus_reply = false;
  uint32_t queued = 0, sym = 0, mods = 0;
  uint8_t flags = 0;
  int failures = 0, sends = 0;
  explicit Fake(Core &core) : c(core) {}
  bool send(uint32_t, uint32_t seq, uint32_t s, uint32_t m,
            uint8_t f) override {
    if (full)
      return false;
    ++sends;
    queued = seq;
    sym = s;
    mods = m;
    flags = f;
    return true;
  }
  void pump() override {
    if (focus_reply) {
      focus_reply = false;
      Result r;
      r.ctx = c.ctx;
      r.policy = 1;
      CHECK(c.result(r).accept);
      return;
    }
    if (refocus) {
      refocus = false;
      c.focus(c.ctx + 1, true);
      return;
    }
    if (queued && reply) {
      Result r;
      r.ctx = c.ctx;
      r.seq = queued;
      r.policy = 1;
      r.flags = kHandled;
      auto result = c.result(r);
      CHECK(result.accept);
      if (text)
        c.acknowledge(queued);
      queued = 0;
    }
  }
  uint64_t now() override { return tick; }
  void wait_until(uint64_t deadline) override { tick = deadline; }
  void degraded() override { ++failures; }
};
static void setup(Core &c) {
  c.reset();
  c.focus(1, false);
  c.connected = true;
  c.known = true;
  c.policy = 1;
  c.installed[1] = c.installed[2] = true;
  c.policies[1] = {};
  auto &p = c.policies[1];
  p.composing = 2;
  p.notify = 3;
  p.printable['a' - 0x20] = Eat | 1 << 2;
  p.printable['!' - 0x20] = Eat | 2 << 2;
  c.policies[2] = p;
  c.policies[2].printable[' ' - 0x20] = Eat | 2 << 2;
  c.policies[2].special_default = Ask | 2 << 2;
}
static void decisions() {
  {
    // The first key in a newly focused field waits for the focus reply's
    // policy instead of passing through raw.
    auto f = std::make_unique<Core>();
    setup(*f);
    f->focus(2, false);
    Fake h(*f);
    h.focus_reply = true;
    CHECK(f->key('a', 0, h));
    CHECK(h.sends == 1 && h.tick < Core::kBudget);
    // Without a reply in time, only that key passes through.
    f->focus(1, false);
    Fake g(*f);
    g.reply = false;
    auto before = f->timeouts;
    CHECK(!f->key('a', 0, g));
    CHECK(g.sends == 0 && f->connected && f->timeouts == before + 1);
  }
  // Two engine contexts per connection, alternating with each focus.
  CHECK(context_id(1) == 2 && context_id(2) == 1 && context_id(3) == 2 &&
        context_id(0xffffffff) == 2);
  auto c = std::make_unique<Core>();
  {
    setup(*c);
    Fake h(*c);
    CHECK(!c->key('z', 0, h));
    CHECK(h.sends == 0);
  }
  {
    setup(*c);
    Fake h(*c);
    h.reply = false;
    CHECK(c->key('a', 0, h));
    CHECK(c->policy == 2);
    CHECK(c->key(' ', 0, h));
    CHECK(!c->known);
    auto before = c->timeouts;
    CHECK(!c->key('b', 0, h));
    // A timeout passes this key through but keeps the connection (macOS
    // does the same): a slow result must not end the composition.
    CHECK(h.tick == Core::kBudget && h.failures == 0 && c->connected &&
          c->timeouts == before + 1);
  }
  {
    setup(*c);
    Fake h(*c);
    c->policy = 2;
    CHECK(c->key(0xff0d, 0, h));
    CHECK(h.flags == 1 && h.tick == 0);
  }
  {
    setup(*c);
    Fake h(*c);
    c->policy = 2;
    h.reply = false;
    auto before = c->timeouts;
    CHECK(!c->key(0xff0d, 0, h));
    CHECK(h.tick == Core::kBudget && c->connected && c->timeouts == before + 1);
  }
  {
    setup(*c);
    Fake h(*c);
    h.reply = false;
    CHECK(c->key('a', 0, h));
    h.reply = true;
    h.text = false;
    CHECK(!c->key('v', kControl, h));
    CHECK(h.tick == Core::kBudget && h.failures == 0 && c->connected);
  }
  {
    setup(*c);
    Fake h(*c);
    h.full = true;
    CHECK(!c->key('a', 0, h));
    CHECK(!c->connected);
  }
  {
    setup(*c);
    Fake h(*c);
    c->secure = true;
    CHECK(!c->key('a', 0, h));
    CHECK(h.sends == 0 && h.tick == 0);
  }
  {
    setup(*c);
    Fake h(*c);
    CHECK(!c->key(0xffe1, kShift, h));
    CHECK(h.flags == 2);
    CHECK(!c->key('z', kShift | kSuper, h));
    CHECK(h.flags == 2 && h.sends == 2);
  }
  {
    setup(*c);
    Fake h(*c);
    h.reply = false;
    CHECK(c->key('a', 0, h));
    Result r;
    r.ctx = 1;
    r.seq = 1;
    r.policy = 1;
    CHECK(c->result(r).repair == 'a');
    CHECK(!c->result(r).accept);
    c->focus(2, false);
    CHECK(!c->result(r).accept);
    r.ctx = 2;
    r.seq = 0;
    CHECK(c->result(r).accept);
    c->secure = true;
    CHECK(!c->result(r).accept);
  }
  {
    setup(*c);
    Fake h(*c);
    h.reply = false;
    CHECK(c->key('a', 0, h));
    CHECK(c->key(' ', 0, h));
    CHECK(!c->known);
    Result r;
    r.ctx = 1;
    r.seq = 1;
    r.policy = 1;
    r.flags = kHandled;
    CHECK(c->result(r).accept);
    CHECK(!c->known);
    c->migrating = true;
    CHECK(!c->key('a', 0, h));
    r.seq = 2;
    CHECK(c->result(r).accept);
    c->acknowledge(2);
    CHECK(c->applied == c->sent);
  }
  {
    setup(*c);
    Fake h(*c);
    h.reply = false;
    CHECK(c->key('a', 0, h));
    CHECK(c->key(' ', 0, h));
    h.refocus = true;
    CHECK(!c->key('a', 0, h));
    CHECK(c->secure && h.sends == 2);
  }
  {
    setup(*c);
    Fake h(*c);
    CHECK(!c->key('c', kControl, h));
    CHECK(h.sends == 1 && h.flags == 2);
  }
  {
    setup(*c);
    Fake h(*c);
    size_t before = allocations.load();
    for (int i = 0; i < 10000; ++i) {
      CHECK(c->key('a', 0, h));
      h.pump();
    }
    CHECK(allocations.load() == before);
  }
}
static void latency() {
  auto c = std::make_unique<Core>();
  setup(*c);
  Fake h(*c);
  constexpr size_t n = 10000;
  std::array<int64_t, n> samples{};
  for (size_t i = 0; i < n; ++i) {
    auto before = std::chrono::steady_clock::now();
    CHECK(c->key('a', 0, h));
    samples[i] = std::chrono::duration_cast<std::chrono::nanoseconds>(
                     std::chrono::steady_clock::now() - before)
                     .count();
    h.pump();
  }
  std::sort(samples.begin(), samples.end());
  std::cout << "client decision (fake transport): p50 " << samples[n / 2]
            << " ns, p99 " << samples[n * 99 / 100]
            << " ns; 0 allocations/key\n";
}
static void parser_stress() {
  std::array<char, 512> bytes{};
  auto tables = std::make_unique<Policy::Tables>();
  uint32_t state = 123;
  for (size_t n = 0; n < 100000; ++n) {
    for (auto &b : bytes) {
      state = state * 1664525u + 1013904223u;
      b = char(state >> 24);
    }
    std::string_view view(bytes.data(), n % bytes.size());
    Policy p;
    Result r;
    p.decode(view, *tables);
    r.decode(view);
    uint32_t size;
    frame_size(view, size);
  }
}
static void rings() {
  auto r = std::make_unique<Ring<uint32_t, 16>>();
  for (uint32_t i = 0; i < 16; ++i) {
    auto s = r->reserve();
    CHECK(s);
    *s = i;
    r->publish();
  }
  CHECK(!r->reserve());
  for (uint32_t i = 0; i < 16; ++i) {
    CHECK(r->front() && *r->front() == i);
    r->pop();
  }
  CHECK(!r->front());
  std::thread producer([&] {
    for (uint32_t i = 0; i < 100000; ++i) {
      uint32_t *p;
      while (!(p = r->reserve()))
        std::this_thread::yield();
      *p = i;
      r->publish();
    }
  });
  for (uint32_t i = 0; i < 100000; ++i) {
    uint32_t *p;
    while (!(p = r->front()))
      std::this_thread::yield();
    CHECK(*p == i);
    r->pop();
  }
  producer.join();
}

// Fills a frame of n bytes whose content identifies it (and its length).
static void fill(char *p, size_t n, uint32_t id) {
  for (size_t i = 0; i < n; ++i)
    p[i] = char(id * 31 + i * 7);
}
static bool matches(const char *p, size_t n, uint32_t id) {
  for (size_t i = 0; i < n; ++i)
    if (p[i] != char(id * 31 + i * 7))
      return false;
  return true;
}
static bool put(FrameRing &r, size_t n, uint32_t id) {
  auto p = r.reserve(n);
  if (!p)
    return false;
  fill(p, n, id);
  r.publish(n);
  return true;
}
static void frame_rings() {
  {
    // Bounded, first in first out, and frames are contiguous across wraps.
    FrameRing r(1024);
    CHECK(r.ok() && r.max_frame() == 508);
    size_t n = 0;
    CHECK(!r.front(n) && r.empty());
    CHECK(!r.reserve(0) && !r.reserve(509));
    uint32_t next = 0, expect = 0;
    std::array<size_t, 8> sizes{1, 5, 22, 100, 333, 7, 508, 64};
    for (int round = 0; round < 2000; ++round) {
      while (put(r, sizes[next % sizes.size()], next))
        ++next;
      CHECK(next > expect);
      // Drain a few so the write position wraps at varied offsets.
      for (int k = 0; k < 3 && r.front(n); ++k) {
        auto size = sizes[expect % sizes.size()];
        auto p = r.front(n);
        CHECK(n == size && matches(p, n, expect));
        r.pop();
        ++expect;
      }
    }
    while (auto p = r.front(n)) {
      CHECK(n == sizes[expect % sizes.size()] && matches(p, n, expect));
      r.pop();
      ++expect;
    }
    CHECK(expect == next && r.empty());
  }
  {
    // A maximum frame fits an empty ring wherever the position is, and a
    // slot is not reused until it is popped.
    FrameRing r(1 << 17);
    CHECK(r.max_frame() == kFrameCapacity);
    for (size_t shift : {size_t(0), size_t(1), size_t(100), size_t(65531),
                         size_t(65532), size_t(100000)}) {
      size_t n = 0;
      for (size_t moved = 0; moved < shift;) {
        auto step = std::min<size_t>(shift - moved, 4000);
        CHECK(put(r, step, 1) && r.front(n) && n == step);
        r.pop();
        moved += step;
      }
      CHECK(put(r, kFrameCapacity, 9));
      auto p = r.front(n);
      CHECK(p && n == kFrameCapacity && matches(p, n, 9));
      // Held by the consumer: whatever the producer adds meanwhile, the
      // frame in use stays intact.
      for (uint32_t id = 100; put(r, kFrameCapacity, id); ++id) {
      }
      CHECK(!r.reserve(kFrameCapacity) && matches(p, n, 9));
      r.pop();
      while (r.front(n))
        r.pop();
      CHECK(put(r, kFrameCapacity, 10));
      CHECK(r.front(n) && matches(r.front(n), n, 10));
      r.pop();
      CHECK(r.empty());
    }
  }
  {
    // Full: a frame that does not fit is refused, nothing is overwritten, and
    // space returns exactly when the consumer pops.
    FrameRing r(256);
    uint32_t id = 0;
    while (put(r, 20, id))
      ++id;
    CHECK(id >= 5);
    CHECK(!r.reserve(20));
    size_t n = 0;
    CHECK(r.front(n) && matches(r.front(n), n, 0));
    r.pop();
    CHECK(put(r, 20, id++));
    for (uint32_t i = 1; i < id; ++i) {
      auto p = r.front(n);
      CHECK(p && n == 20 && matches(p, n, i));
      r.pop();
    }
    CHECK(r.empty());
    // An unpublished reservation changes nothing.
    CHECK(r.reserve(20) && r.empty() && !r.front(n));
  }
  {
    // Two threads, mixed sizes, no allocation after construction.
    FrameRing r(1 << 12);
    constexpr uint32_t count = 200000;
    std::thread producer([&] {
      for (uint32_t i = 0; i < count; ++i)
        while (!put(r, 1 + (i * 37) % r.max_frame() / 4, i))
          std::this_thread::yield();
    });
    size_t before = allocations.load(); // after the thread's own allocations
    for (uint32_t i = 0; i < count; ++i) {
      size_t n = 0;
      const char *p;
      while (!(p = r.front(n)))
        std::this_thread::yield();
      CHECK(n == 1 + (i * 37) % r.max_frame() / 4 && matches(p, n, i));
      r.pop();
    }
    producer.join();
    CHECK(allocations.load() == before);
  }
}
static void policy_tables() {
  // Many installed policies share bounded tables; a rejected one claims
  // nothing, and a reset gives the space back.
  auto c = std::make_unique<Core>();
  auto encode = [](uint16_t id, size_t specials, size_t hotkeys) {
    Frame f;
    f.u16(id);
    f.u16(0);
    f.u8(0);
    f.u8(0);
    f.u8(0);
    for (int i = 0; i < 95; ++i)
      f.u8(0);
    f.u16(uint16_t(specials));
    for (size_t i = 0; i < specials; ++i) {
      f.u32(uint32_t(0xff00 + i));
      f.u8(0);
      f.u8(Eat);
    }
    f.u16(uint16_t(hotkeys));
    for (size_t i = 0; i < hotkeys; ++i) {
      f.u32(uint32_t(0x61 + i));
      f.u32(kControl);
    }
    CHECK(f.good);
    return std::string(f.view());
  };
  c->reset();
  for (uint16_t id = 0; id < 100; ++id)
    CHECK(c->install(encode(id, 40, 4)));
  c->policy = 77;
  CHECK(c->policies[77].lookup(c->tables, 0xff00 + 39, 0) == Eat);
  CHECK(c->policies[3].lookup(c->tables, 0xff00 + 40, 0) == 0);
  CHECK(c->policies[99].lookup(c->tables, 'b', kControl) == (Ask | 2 << 2));
  CHECK(!c->install(encode(101, Policy::kMaxSpecial + 1, 0)));
  // Exhaustion fails cleanly (the service degrades) and keeps what's there.
  auto used = c->tables.specials;
  while (c->install(encode(150, 128, 0))) {
  }
  CHECK(c->tables.specials >= used && c->installed[3]);
  CHECK(c->policies[3].lookup(c->tables, 0xff00 + 39, 0) == Eat);
  c->reset();
  CHECK(c->tables.specials == 0 && c->install(encode(1, 128, 128)));
}
static void scopes() {
  CHECK(absent_input_scope(0, 0));
  CHECK(absent_input_scope(1, 0));
  CHECK(
      absent_input_scope(0x80004005u, 0)); // no supplied scope in shell/Notepad
  CHECK(absent_input_scope(0x80004001u, 0));   // unsupported by context owner
  CHECK(!absent_input_scope(0x80004005u, 13)); // supplied IUnknown failed
  CHECK(!absent_input_scope(0x80040200u, 0));  // TSF lock error
  CHECK(!absent_input_scope(0x80040204u, 0));  // disconnected context
  CHECK(!absent_input_scope(0x80070057u, 0));  // invalid argument
  CHECK(!absent_input_scope(0, 13));           // inspect supplied input scope
  CHECK(!absent_input_scope(0, 3));            // malformed type
}
static void keymap() {
  CHECK(finish_before_shortcut('l', kControl, false));
  CHECK(finish_before_shortcut('v', kControl, false));
  CHECK(!finish_before_shortcut('a', 0, false));
  CHECK(!finish_before_shortcut('l', kControl, true));
  CHECK(!finish_before_shortcut('l', kControl | kRelease, false));
  CHECK(!finish_before_shortcut(0xffe3, kControl, false));
  CHECK(!finish_before_shortcut(0xffe1, kShift, false));

  CHECK(key_message(0x00310001) == key_message(0x00310000)); // shell rename
  CHECK(key_message(0x00310002) == key_message(0x00310000)); // repeat count
  CHECK(key_message(0xc0310001) == key_message(0xc0310000)); // key up
  CHECK(key_message(0x00310001) != key_message(0x40310001)); // prior-down flag
  CHECK(key_message(0x00310001) != key_message(0x01310001)); // extended key
  CHECK(key_message(0x00310001) != key_message(0x00170001)); // another scan

  CHECK(windows_key('A', 0, false, 0) == 'a');
  CHECK(windows_key('A', 0, false, kShift) == 'A');
  CHECK(windows_key('A', 0, false, kShift | kLock) == 'a');
  CHECK(windows_key('1', 0, false, kShift) == '!');
  // Keypad digits and operators send their characters; Shift doesn't
  // change them (Windows already reports navigation keys without Num Lock).
  CHECK(windows_key(0x61, 0, false, 0) == '1' &&
        windows_key(0x69, 0, false, kShift) == '9');
  CHECK(windows_key(0x6a, 0, false, 0) == '*' &&
        windows_key(0x6b, 0, false, 0) == '+' &&
        windows_key(0x6d, 0, false, 0) == '-' &&
        windows_key(0x6e, 0, false, 0) == '.' &&
        windows_key(0x6f, 0, true, 0) == '/');
  CHECK(windows_key(0x2d, 0, true, 0) == 0xff63);
  CHECK(windows_key(0x10, 0x36, false, 0) == 0xffe2);
  CHECK(windows_key(0x11, 0, true, 0) == 0xffe4);
  CHECK(windows_key(13, 0, true, 0) == 0xff8d);
  CHECK(windows_key(0xde, 0, false, kShift) == '"');
  CHECK(windows_key(0xff, 0, false, 0) == 0);
}
static void stalled_engine() {
  const auto before = allocations.load();
  ReplyWatchdog watch;
  watch.request(1, 1, 0);
  CHECK(!watch.expired(16)); // legitimate reranking hold
  CHECK(!watch.expired(99));
  CHECK(watch.expired(100));
  watch.result(1, 1, 101);
  CHECK(!watch.expired(10000));
  watch.request(1, 2, 10000);
  watch.request(1, 3, 10010);
  watch.result(1, 2, 10016);
  watch.result(2, 3, 10100); // old/future contexts cannot keep it alive
  watch.result(1, 4, 10100);
  watch.result(1, 2, 10100); // duplicate result is not progress
  CHECK(!watch.expired(10115));
  CHECK(watch.expired(10116));
  watch.result(1, 3, 10117);
  CHECK(!watch.expired(20000));
  Core c;
  c.secure = false;
  c.received = 1;
  c.sent = 6;
  c.pending[1] = {1, 'x', false,
                  true}; // reply already received: don't duplicate
  c.pending[2] = {2, 'n', false, true};
  c.pending[3] = {3, 'i', false, true};
  c.pending[4] = {4, '!', true, false};  // already passed
  c.pending[5] = {5, 'h', false, false}; // ASK already passed after its budget
  c.pending[6] = {6, 0xff0d, false, true}; // never invent a literal special key
  std::array<char, 128> text{};
  CHECK(c.fallback(text.data(), text.size()) == 2);
  CHECK(std::string_view(text.data(), 2) == "ni");
  CHECK(c.fallback(text.data(), 1) == 1);
  size_t size = c.fallback(text.data(), text.size());
  CHECK(append_literal(text.data(), size, text.size(), 'h', 0));
  CHECK(append_literal(text.data(), size, text.size(), 'A', kShift));
  CHECK(std::string_view(text.data(), size) == "nihA");
  CHECK(!append_literal(text.data(), size, text.size(), 'a', kControl));
  CHECK(!append_literal(text.data(), size, text.size(), 'a', kAlt));
  CHECK(!append_literal(text.data(), size, text.size(), 'a', kSuper));
  CHECK(!append_literal(text.data(), size, text.size(), 'a', kRelease));
  CHECK(!append_literal(text.data(), size, text.size(), 0xff51, 0));
  size = text.size();
  CHECK(!append_literal(text.data(), size, text.size(), 'z', 0));
  CHECK(size == text.size());
  c.secure = true;
  CHECK(c.fallback(text.data(), text.size()) == 0);
  c.focus(2, false);
  CHECK(c.fallback(text.data(), text.size()) == 0);
  CHECK(allocations.load() == before);
}
int main() {
  stalled_engine();
  scopes();
  keymap();
  golden();
  decisions();
  rings();
  frame_rings();
  policy_tables();
  parser_stress();
  latency();
  std::cout << "native client tests passed\n";
}
