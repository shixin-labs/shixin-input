// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "wire.h"
namespace shixin {
enum Disposition : uint8_t { Pass = 0, Notify = 1, Eat = 2, Ask = 3 };
struct Policy {
  struct Special {
    uint32_t sym;
    uint8_t shift, action;
  };
  struct Hotkey {
    uint32_t sym, mods;
  };
  static constexpr size_t kMaxSpecial = 128, kMaxHotkey = 128;
  // The sorted key tables of every installed policy live in one Tables, so a
  // Core holds 256 small headers, not 256 maximum-size policies. Tables is
  // bump-allocated and cleared with the connection.
  struct Tables {
    static constexpr size_t kSpecial = 6144, kHotkey = 2048;
    std::array<Special, kSpecial> special{};
    std::array<Hotkey, kHotkey> hotkeys{};
    size_t specials = 0, hotkey_count = 0;
    void clear() { specials = hotkey_count = 0; }
  };
  uint16_t id = 0, composing = 0;
  uint8_t notify = 0, special_default = 0, command_default = 0;
  std::array<uint8_t, 95> printable{};
  uint16_t specials = 0, hotkey_count = 0;
  uint32_t special_at = 0, hotkey_at = 0;
  // decode writes the tables after the used part of t but does not claim
  // them: a malformed policy leaves t untouched. commit claims them once the
  // policy is kept.
  bool decode(std::string_view bytes, Tables &t) {
    Reader r(bytes);
    id = r.u16();
    composing = r.u16();
    notify = r.u8();
    special_default = r.u8();
    command_default = r.u8();
    for (auto &a : printable)
      a = r.u8();
    specials = r.u16();
    if (specials > kMaxSpecial || specials > t.special.size() - t.specials)
      return false;
    special_at = uint32_t(t.specials);
    uint64_t previous = 0;
    for (size_t i = 0; i < specials; ++i) {
      auto &s = t.special[special_at + i];
      s.sym = r.u32();
      s.shift = r.u8();
      s.action = r.u8();
      uint64_t current = uint64_t(s.sym) * 2 + !!s.shift;
      if (i && current < previous)
        return false;
      previous = current;
    }
    hotkey_count = r.u16();
    if (hotkey_count > kMaxHotkey ||
        hotkey_count > t.hotkeys.size() - t.hotkey_count)
      return false;
    hotkey_at = uint32_t(t.hotkey_count);
    previous = 0;
    for (size_t i = 0; i < hotkey_count; ++i) {
      auto &h = t.hotkeys[hotkey_at + i];
      h.sym = r.u32();
      h.mods = r.u32();
      uint64_t current = uint64_t(h.sym) << 32 | h.mods;
      if (i && current < previous)
        return false;
      previous = current;
    }
    return r.good;
  }
  void commit(Tables &t) const {
    t.specials += specials;
    t.hotkey_count += hotkey_count;
  }
  uint8_t lookup(const Tables &t, uint32_t sym, uint32_t mods) const {
    if (sym == 0xffe1 || sym == 0xffe2)
      return notify & 1 ? Notify : Pass;
    if (sym == 0xffe3 || sym == 0xffe4)
      return notify & 2 ? Notify : Pass;
    if (mods & kRelease)
      return Pass;
    if (mods & kCommand) {
      auto m = mods & (kShift | kCommand);
      for (size_t i = 0; i < hotkey_count; ++i) {
        auto &h = t.hotkeys[hotkey_at + i];
        if (h.sym == sym && h.mods == m)
          return Ask | 2 << 2;
      }
      return command_default;
    }
    if (sym >= 0x20 && sym <= 0x7e)
      return printable[sym - 0x20];
    for (size_t i = 0; i < specials; ++i) {
      auto &s = t.special[special_at + i];
      if (s.sym == sym && bool(s.shift) == bool(mods & kShift))
        return s.action;
    }
    return special_default;
  }
};
} // namespace shixin
