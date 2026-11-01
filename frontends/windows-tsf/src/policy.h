// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "wire.h"
namespace shixin {
enum Disposition : uint8_t { Pass = 0, Notify = 1, Eat = 2, Ask = 3 };
struct Policy {
  uint16_t id = 0, composing = 0;
  uint8_t notify = 0, special_default = 0, command_default = 0;
  std::array<uint8_t, 95> printable{};
  struct Special {
    uint32_t sym;
    uint8_t shift, action;
  };
  struct Hotkey {
    uint32_t sym, mods;
  };
  std::array<Special, 128> special{};
  std::array<Hotkey, 128> hotkeys{};
  uint16_t specials = 0, hotkey_count = 0;
  bool decode(std::string_view bytes) {
    Reader r(bytes);
    id = r.u16();
    composing = r.u16();
    notify = r.u8();
    special_default = r.u8();
    command_default = r.u8();
    for (auto &a : printable)
      a = r.u8();
    specials = r.u16();
    if (specials > special.size())
      return false;
    uint64_t previous = 0;
    for (size_t i = 0; i < specials; ++i) {
      auto &s = special[i];
      s.sym = r.u32();
      s.shift = r.u8();
      s.action = r.u8();
      uint64_t current = uint64_t(s.sym) * 2 + !!s.shift;
      if (i && current < previous)
        return false;
      previous = current;
    }
    hotkey_count = r.u16();
    if (hotkey_count > hotkeys.size())
      return false;
    previous = 0;
    for (size_t i = 0; i < hotkey_count; ++i) {
      auto &h = hotkeys[i];
      h.sym = r.u32();
      h.mods = r.u32();
      uint64_t current = uint64_t(h.sym) << 32 | h.mods;
      if (i && current < previous)
        return false;
      previous = current;
    }
    return r.good;
  }
  uint8_t lookup(uint32_t sym, uint32_t mods) const {
    if (sym == 0xffe1 || sym == 0xffe2)
      return notify & 1 ? Notify : Pass;
    if (sym == 0xffe3 || sym == 0xffe4)
      return notify & 2 ? Notify : Pass;
    if (mods & kRelease)
      return Pass;
    if (mods & kCommand) {
      auto m = mods & (kShift | kCommand);
      for (size_t i = 0; i < hotkey_count; ++i)
        if (hotkeys[i].sym == sym && hotkeys[i].mods == m)
          return Ask | 2 << 2;
      return command_default;
    }
    if (sym >= 0x20 && sym <= 0x7e)
      return printable[sym - 0x20];
    for (size_t i = 0; i < specials; ++i)
      if (special[i].sym == sym &&
          bool(special[i].shift) == bool(mods & kShift))
        return special[i].action;
    return special_default;
  }
};
} // namespace shixin
