// SPDX-License-Identifier: GPL-3.0-or-later
#include "core.h"
#include "keys.h"
#include "ring.h"
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
      CHECK(p.decode(payload));
      CHECK(p.id == 3 && p.lookup('a', 0) == 6);
      CHECK(p.lookup(0xff0d, 0) == 10 && p.lookup('`', kControl) == 11);
      CHECK(p.lookup(0xffe1, kRelease) == Notify &&
            p.lookup('a', kRelease) == Pass);
      for (size_t i = 0; i < payload.size(); ++i) {
        Policy truncated;
        CHECK(!truncated.decode(payload.substr(0, i)));
      }
      CHECK(p.decode(std::string(payload) + "future"));
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
  uint32_t state = 123;
  for (size_t n = 0; n < 100000; ++n) {
    for (auto &b : bytes) {
      state = state * 1664525u + 1013904223u;
      b = char(state >> 24);
    }
    std::string_view view(bytes.data(), n % bytes.size());
    Policy p;
    Result r;
    p.decode(view);
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
static void keymap() {
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
int main() {
  keymap();
  golden();
  decisions();
  rings();
  parser_stress();
  latency();
  std::cout << "native client tests passed\n";
}
