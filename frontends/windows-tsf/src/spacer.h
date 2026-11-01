// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "wire.h"
namespace shixin {
enum class SpaceClass : uint8_t { Unknown, CJK, Latin, Digit, Space, Other };
struct SpaceRange {
  uint32_t lo, hi, stride = 1;
};
template <size_t N>
inline bool in_space_ranges(uint32_t r, const SpaceRange (&ranges)[N]) {
  for (auto range : ranges)
    if (r >= range.lo && r <= range.hi && (r - range.lo) % range.stride == 0)
      return true;
  return false;
}
// The script ranges below follow the same Unicode 17 script membership and
// deliberate block overrides as internal/client/spacer.go. Keep the shared
// vectors and the classification cases in sync when changing these rules.
inline SpaceClass space_class(uint32_t r) {
  using C = SpaceClass;
  if (r < 128) {
    if ((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
      return C::Latin;
    if (r >= '0' && r <= '9')
      return C::Digit;
    if (r == ' ' || r == '\t' || r == '\n' || r == '\r')
      return C::Space;
    return C::Other;
  }
  if ((r >= 0x4e00 && r <= 0x9fff) || (r >= 0x3400 && r <= 0x4dbf) ||
      (r >= 0xac00 && r <= 0xd7af) ||
      (r >= 0x3040 && r <= 0x30ff && r != 0x30fb))
    return C::CJK;
  if (r == 0xa0 || r == 0x3000 || (r >= 0x2000 && r <= 0x200a) || r == 0x2028 ||
      r == 0x2029)
    return C::Space;
  if (r >= 0xff00 && r <= 0xff65)
    return C::Other;
  constexpr SpaceRange latin[] = {
      {0xaa, 0xba, 16},    {0xc0, 0xd6},     {0xd8, 0xf6},
      {0xf8, 0x2b8},       {0x2e0, 0x2e4},   {0x370, 0x373},
      {0x375, 0x377},      {0x37a, 0x37d},   {0x37f, 0x384, 5},
      {0x386, 0x388, 2},   {0x389, 0x38a},   {0x38c, 0x38e, 2},
      {0x38f, 0x3a1},      {0x3a3, 0x3e1},   {0x3f0, 0x484},
      {0x487, 0x52f},      {0x1e00, 0x1f15}, {0x1f18, 0x1f1d},
      {0x1f20, 0x1f45},    {0x1f48, 0x1f4d}, {0x1f50, 0x1f57},
      {0x1f59, 0x1f5f, 2}, {0x1f60, 0x1f7d}, {0x1f80, 0x1fb4},
      {0x1fb6, 0x1fc4},    {0x1fc6, 0x1fd3}, {0x1fd6, 0x1fdb},
      {0x1fdd, 0x1fef},    {0x1ff2, 0x1ff4}, {0x1ff6, 0x1ffe}};
  if (in_space_ranges(r, latin))
    return C::Latin;
  constexpr SpaceRange cjk[] = {
      {0x2ea, 0x2eb},     {0x1100, 0x11ff},       {0x2e80, 0x2e99},
      {0x2e9b, 0x2ef3},   {0x2f00, 0x2fd5},       {0x3005, 0x3007, 2},
      {0x3021, 0x3029},   {0x302e, 0x302f},       {0x3038, 0x303b},
      {0x3105, 0x312f},   {0x3131, 0x318e},       {0x31a0, 0x31bf},
      {0x31f0, 0x31ff},   {0x3200, 0x321e},       {0x3260, 0x327e},
      {0x32d0, 0x32fe},   {0x3300, 0x3357},       {0xa960, 0xa97c},
      {0xd7b0, 0xd7c6},   {0xd7cb, 0xd7fb},       {0xf900, 0xfa6d},
      {0xfa70, 0xfad9},   {0xff66, 0xff6f},       {0xff71, 0xff9d},
      {0xffa0, 0xffbe},   {0xffc2, 0xffc7},       {0xffca, 0xffcf},
      {0xffd2, 0xffd7},   {0xffda, 0xffdc},       {0x16fe2, 0x16fe3},
      {0x16ff0, 0x16ff6}, {0x1aff0, 0x1aff3},     {0x1aff5, 0x1affb},
      {0x1affd, 0x1affe}, {0x1b000, 0x1b122},     {0x1b132, 0x1b150, 30},
      {0x1b151, 0x1b152}, {0x1b155, 0x1b164, 15}, {0x1b165, 0x1b167},
      {0x1f200, 0x1f200}, {0x20000, 0x2a6df},     {0x2a700, 0x2b81d},
      {0x2b820, 0x2cead}, {0x2ceb0, 0x2ebe0},     {0x2ebf0, 0x2ee5d},
      {0x2f800, 0x2fa1d}, {0x30000, 0x3134a},     {0x31350, 0x33479}};
  return in_space_ranges(r, cjk) ? C::CJK : C::Other;
}
inline bool space_boundary(SpaceClass a, SpaceClass b) {
  using C = SpaceClass;
  return (a == C::CJK && (b == C::Latin || b == C::Digit)) ||
         (b == C::CJK && (a == C::Latin || a == C::Digit));
}
inline bool space_joins(uint32_t r) {
  if (r < 0x300)
    return false;
  // Besides the common combining blocks, conservatively discard history
  // for unfamiliar scripts/symbols. A grapheme deletion must never expose
  // a guessed boundary. The final character's class remains known.
  return r == 0x200d || r == 0x20e3 || (r >= 0x300 && r <= 0x36f) ||
         (r >= 0x483 && r <= 0x489) || (r >= 0x1ab0 && r <= 0x1aff) ||
         (r >= 0x1dc0 && r <= 0x1dff) || (r >= 0x20d0 && r <= 0x20ff) ||
         (r >= 0x302a && r <= 0x302f) || r == 0x3099 || r == 0x309a ||
         (r >= 0xfe00 && r <= 0xfe2f) || (r >= 0x1f3fb && r <= 0x1f3ff) ||
         (r >= 0x1f1e6 && r <= 0x1f1ff) || (r >= 0xe0020 && r <= 0xe007f) ||
         (r >= 0x16ff0 && r <= 0x16ff6);
}
inline uint32_t space_decode(std::string_view &text) {
  if (text.empty())
    return 0;
  auto first = uint8_t(text[0]);
  size_t n = first < 0x80                     ? 1
             : first >= 0xc2 && first < 0xe0  ? 2
             : first < 0xf0 && first >= 0xe0  ? 3
             : first >= 0xf0 && first <= 0xf4 ? 4
                                              : 0;
  if (!n || text.size() < n) {
    text.remove_prefix(1);
    return 0xfffd;
  }
  uint32_t r = first & (n == 1 ? 0x7f : (1u << (7 - n)) - 1);
  for (size_t i = 1; i < n; ++i) {
    if ((uint8_t(text[i]) & 0xc0) != 0x80) {
      text.remove_prefix(1);
      return 0xfffd;
    }
    r = r * 64 + (uint8_t(text[i]) & 63);
  }
  if ((n == 2 && r < 0x80) || (n == 3 && r < 0x800) ||
      (n == 4 && r < 0x10000) || r > 0x10ffff || (r >= 0xd800 && r <= 0xdfff)) {
    text.remove_prefix(1);
    return 0xfffd;
  }
  text.remove_prefix(n);
  return r;
}
// Apartment-local, allocation-free counterpart of internal/client/spacer.go.
// Readers are invoked only inside a TSF edit session, at a possible boundary.
struct Spacer {
  using C = SpaceClass;
  struct Entry {
    C cl = C::Unknown;
    bool ours = false;
  };
  std::array<Entry, 16> history{};
  size_t top = 0, count = 0;
  bool on = false, muted = false, unreadable = false;
  C before = C::Unknown;
  unsigned strikes = 0;
  uint64_t last = 0;
  void clear() {
    count = 0;
    muted = false;
    before = C::Unknown;
  }
  void focus() {
    clear();
    on = false;
    unreadable = false;
    strikes = 0;
  }
  void tick(uint64_t t) {
    if (t - last > 30000000000ull)
      clear();
    last = t;
  }
  C left() const { return count ? history[top].cl : C::Unknown; }
  void push(C cl, bool ours = false) {
    top = (top + 1) % history.size();
    history[top] = {cl, ours};
    if (count < history.size())
      ++count;
  }
  void strike() {
    if (++strikes >= 2)
      unreadable = true;
  }
  template <class Reader> bool prefix(C next, Reader &reader) {
    if (!on || muted ||
        (next != C::CJK && next != C::Latin && next != C::Digit))
      return false;
    auto cl = left();
    if (cl != C::Unknown && !space_boundary(cl, next))
      return false;
    uint32_t r = 0;
    bool ok = false;
    if (!unreadable) {
      auto t = reader.now();
      ok = reader.read(r);
      if (!ok)
        unreadable = true;
      else if (reader.now() - t > 2000000)
        strike();
    }
    if (cl == C::Unknown) {
      if (!ok)
        return false;
      auto got = r ? space_class(r) : C::Space;
      push(got);
      return space_boundary(got, next);
    }
    if (!ok || space_class(r) == cl)
      return true;
    if (!r) {
      unreadable = true;
      return true;
    }
    if (space_class(r) == before) {
      strike();
      return true;
    }
    clear();
    push(space_class(r));
    return false;
  }
  bool needs_key_read(uint32_t sym, uint32_t mods) const {
    auto cl = space_class(sym);
    return on && !muted && !(mods & (kCommand | kRelease)) &&
           ((sym >= 0x20 && sym < 0x7f) || (sym >= 0xa0 && sym < 0x100)) &&
           (cl == C::Latin || cl == C::Digit) &&
           (left() == C::Unknown || space_boundary(left(), cl));
  }
  template <class Reader>
  bool key(uint32_t sym, uint32_t mods, Reader &reader,
           bool allow_space = true) {
    if (mods & kRelease)
      return false;
    if (mods & kCommand) {
      if ((mods & (kCommand | kShift)) != kSuper || (sym != 'c' && sym != 's'))
        clear();
      return false;
    }
    if (sym == 0xff08) {
      muted = count && history[top].ours;
      if (count) {
        top = (top + history.size() - 1) % history.size();
        --count;
      }
      before = C::Unknown;
      return false;
    }
    if (sym == 0xff0d || sym == 0xff8d) {
      clear();
      push(C::Space);
      return false;
    }
    if (sym >= 0xffe1 && sym <= 0xffee)
      return false;
    if (!((sym >= 0x20 && sym < 0x7f) || (sym >= 0xa0 && sym < 0x100))) {
      clear();
      return false;
    }
    auto cl = space_class(sym);
    bool space =
        allow_space && (cl == C::Latin || cl == C::Digit) && prefix(cl, reader);
    before = left();
    muted = false;
    if (space)
      push(C::Space, true);
    push(cl);
    return space;
  }
  void inserted(std::string_view text, bool space) {
    before = left();
    muted = false;
    if (space)
      push(C::Space, true);
    bool joined = false;
    auto cl = C::Unknown;
    while (!text.empty()) {
      auto r = space_decode(text);
      joined |=
          space_joins(r) ||
          (space_class(r) == C::Other && r >= 0x530 &&
           !(r >= 0x2000 && r <= 0x206f) && !(r >= 0x20a0 && r <= 0x20cf) &&
           !(r >= 0x2300 && r <= 0x2bff) && !(r >= 0x3000 && r <= 0x303f) &&
           !(r >= 0xff00 && r <= 0xff65) && !(r >= 0x1f000 && r <= 0x1faff));
      cl = space_class(r);
      push(cl);
    }
    if (joined) {
      clear();
      push(cl);
    }
  }
};
} // namespace shixin
