// SPDX-License-Identifier: GPL-3.0-or-later
#include "spacer.h"
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <new>
#include <string>
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
static std::u32string decode(std::string_view text) {
  std::u32string result;
  while (!text.empty())
    result += char32_t(space_decode(text));
  return result;
}
struct App {
  std::u32string text, previous;
  size_t caret = 0, prev_caret = 0;
  uint64_t clock = 100000000000ull, slow = 0;
  bool noread = false, stale = false, fake = false;
  unsigned reads = 0;
  uint64_t now() { return clock; }
  bool read(uint32_t &r) {
    ++reads;
    clock += slow;
    auto &s = stale ? previous : text;
    auto pos = stale ? prev_caret : caret;
    r = fake || !pos ? 0 : uint32_t(s[pos - 1]);
    return !noread;
  }
  void settle() {
    previous = text;
    prev_caret = caret;
  }
  void insert(std::u32string_view s) {
    settle();
    text.insert(caret, s);
    caret += s.size();
  }
  void backspace() {
    settle();
    auto end = caret;
    while (caret && space_joins(uint32_t(text[caret - 1])))
      --caret;
    if (caret) {
      --caret;
      if (caret && text[caret] >= 0x1f3fb && text[caret] <= 0x1f3ff)
        --caret;
    }
    text.erase(caret, end - caret);
  }
  void key(Spacer &s, uint32_t sym, uint32_t mods,
           std::u32string_view inserted) {
    s.tick(clock);
    if (s.key(sym, mods, *this))
      insert(std::u32string(U" ") + char32_t(sym));
    else
      insert(inserted);
  }
  void commit(Spacer &s, std::string_view inserted) {
    s.last = clock;
    auto start = inserted;
    bool space =
        !inserted.empty() && s.prefix(space_class(space_decode(start)), *this);
    s.inserted(inserted, space);
    insert((space ? std::u32string(U" ") : std::u32string()) +
           decode(inserted));
  }
};
static void vectors() {
  std::ifstream file(std::filesystem::u8path(SHIXIN_SPACING));
  CHECK(file.good());
  std::string line;
  unsigned count = 0, number = 0;
  while (std::getline(file, line)) {
    ++number;
    if (!line.empty() && line.back() == '\r')
      line.pop_back();
    if (line.empty() || line[0] == '#')
      continue;
    auto separator = line.find(" => ");
    CHECK(separator != std::string::npos);
    std::string_view events(line.data(), separator);
    auto want = decode(std::string_view(line).substr(separator + 4));
    Spacer s;
    s.on = true;
    App app;
    while (!events.empty()) {
      auto end = events.find("; ");
      auto event = events.substr(0, end);
      events = end == std::string_view::npos ? std::string_view()
                                             : events.substr(end + 2);
      auto split = event.find(' ');
      auto name = event.substr(0, split);
      auto arg = split == std::string_view::npos ? std::string_view()
                                                 : event.substr(split + 1);
      app.clock += 100000000;
      if (name == "commit")
        app.commit(s, arg);
      else if (name == "key")
        for (auto r : decode(arg))
          app.key(s, uint32_t(r), 0, {&r, 1});
      else if (name == "bs") {
        auto n = arg.empty() ? 1 : std::stoi(std::string(arg));
        while (n--) {
          app.key(s, 0xff08, 0, {});
          app.backspace();
        }
      } else if (name == "return")
        app.key(s, 0xff0d, 0, U"\n");
      else if (name == "tab")
        app.key(s, 0xff09, 0, U"\t");
      else if (name == "esc")
        app.key(s, 0xff1b, 0, {});
      else if (name == "del")
        app.key(s, 0xffff, 0, {});
      else if (name == "left") {
        app.key(s, 0xff51, 0, {});
        if (app.caret)
          --app.caret;
      } else if (name == "right") {
        app.key(s, 0xff53, 0, {});
        app.caret = std::min(app.caret + 1, app.text.size());
      } else if (name == "cmd")
        app.key(s, uint32_t(arg[0]), kSuper, arg == "v" ? U"P" : U"");
      else if (name == "void")
        app.key(s, 0xffffff, 0, decode(arg));
      else if (name == "click") {
        s.clear();
        if (!arg.empty())
          app.caret = size_t(std::stoi(std::string(arg)));
        app.settle();
      } else if (name == "switch")
        s.clear();
      else if (name == "focus")
        s.clear();
      else if (name == "idle")
        app.clock += 31000000000ull;
      else if (name == "text") {
        app.insert(decode(arg));
        app.settle();
      } else if (name == "noread")
        app.noread = true;
      else if (name == "stale")
        app.stale = true;
      else if (name == "fakecaret")
        app.fake = true;
      else if (name == "on" || name == "off")
        s.on = name == "on";
      else
        CHECK(false);
    }
    for (auto &r : app.text) {
      if (r == '\n')
        r = U'⏎';
      if (r == '\t')
        r = U'⇥';
    }
    if (app.text != want) {
      std::cerr << "vector " << number << ": " << line << "\n";
      CHECK(false);
    }
    ++count;
  }
  CHECK(count >= 50);
  std::cout << count << " shared spacing vectors passed\n";
}
static void classification() {
  using C = SpaceClass;
  for (auto pair : {std::pair{U"中文〇々㐀𠀀豈あア한ㄅ", C::CJK},
                    {U"aZéßαЖ", C::Latin},
                    {U"09", C::Digit},
                    {U" \t\n 　", C::Space},
                    {U"，。、！.,!?()（）「」😀×÷・０Ａ€%", C::Other}})
    for (auto r : std::u32string_view(pair.first))
      CHECK(space_class(uint32_t(r)) == pair.second);
}
static void readers_and_cost() {
  Spacer s;
  s.on = true;
  App app;
  app.slow = 3000000;
  app.insert(U"iPhone");
  app.commit(s, "好");
  s.clear();
  app.commit(s, "好");
  CHECK(s.unreadable && app.reads == 2);
  s.clear();
  app.commit(s, "x");
  CHECK(app.reads == 2);
  s.focus();
  CHECK(!s.on && !s.unreadable);
  auto reads = app.reads;
  app.commit(s, "好");
  CHECK(app.reads == reads);
  struct NoReader {
    uint64_t now() { return 0; }
    bool read(uint32_t &) { return false; }
  } reader;
  s.on = true;
  auto count = allocations.load();
  auto start = std::chrono::steady_clock::now();
  constexpr unsigned rounds = 100000;
  for (unsigned i = 0; i < rounds; ++i) {
    s.tick(1000000000ull + i);
    s.inserted("中文", false);
    CHECK(s.needs_key_read('a', 0));
    CHECK(s.key('a', 0, reader));
    s.key(0xff08, 0, reader);
    s.key(0xff08, 0, reader);
    CHECK(!s.key('b', 0, reader));
    s.clear();
  }
  auto elapsed = std::chrono::steady_clock::now() - start;
  CHECK(allocations.load() == count);
  std::cout
      << "spacing boundary/history: "
      << std::chrono::duration_cast<std::chrono::nanoseconds>(elapsed).count() /
             rounds
      << " ns/sequence, 0 allocations\n";
}
int main() {
  vectors();
  classification();
  readers_and_cost();
}
