// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "wire.h"
namespace shixin {
// IMM-backed controls can zero the repeat count between testing and delivery.
// Scan/extended/transition bits identify the message; the count does not.
inline bool finish_before_shortcut(uint32_t sym, uint32_t mods, bool eaten) {
  return !eaten && !(mods & kRelease) && (mods & (kControl | kAlt | kSuper)) &&
         !(sym >= 0xffe1 && sym <= 0xffee);
}
inline uint32_t key_message(uint32_t lparam) { return lparam & 0xffff0000u; }

inline uint32_t windows_key(uint32_t vk, uint32_t scan, bool extended,
                            uint32_t mods) {
  if (vk >= 'A' && vk <= 'Z')
    return ((mods & kShift) != 0) ^ ((mods & kLock) != 0) ? vk : vk + 32;
  if (vk >= '0' && vk <= '9') {
    constexpr char shifted[] = ")!@#$%^&*(";
    return mods & kShift ? uint32_t(shifted[vk - '0']) : vk;
  }
  // The keypad sends its characters, as on macOS, so a numpad digit picks
  // a candidate like the main row's (with Num Lock off Windows reports the
  // navigation keys instead).
  if (vk >= 0x60 && vk <= 0x69) // VK_NUMPAD0–9
    return '0' + (vk - 0x60);
  if (vk >= 0x70 && vk <= 0x87)
    return 0xffbe + (vk - 0x70);
  switch (vk) {
  case 8:
    return 0xff08;
  case 9:
    return 0xff09;
  case 13:
    return extended ? 0xff8d : 0xff0d;
  case 27:
    return 0xff1b;
  case 32:
    return ' ';
  case 33:
    return 0xff55;
  case 34:
    return 0xff56;
  case 35:
    return 0xff57;
  case 36:
    return 0xff50;
  case 37:
    return 0xff51;
  case 38:
    return 0xff52;
  case 39:
    return 0xff53;
  case 40:
    return 0xff54;
  case 45: // VK_INSERT
    return 0xff63;
  case 46:
    return 0xffff;
  case 0x6a: // VK_MULTIPLY
    return '*';
  case 0x6b: // VK_ADD
    return '+';
  case 0x6d: // VK_SUBTRACT
    return '-';
  case 0x6e: // VK_DECIMAL
    return '.';
  case 0x6f: // VK_DIVIDE
    return '/';
  case 16:
    return scan == 0x36 ? 0xffe2 : 0xffe1;
  case 17:
    return extended ? 0xffe4 : 0xffe3;
  case 0xa0:
    return 0xffe1;
  case 0xa1:
    return 0xffe2;
  case 0xa2:
    return 0xffe3;
  case 0xa3:
    return 0xffe4;
  default:
    break;
  }
  constexpr uint8_t codes[] = {0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf,
                               0xc0, 0xdb, 0xdc, 0xdd, 0xde};
  constexpr char plain[] = ";=,-./`[\\]'", shifted[] = ":+<_>?~{|}\"";
  for (size_t i = 0; i < sizeof(codes); ++i)
    if (vk == codes[i])
      return uint32_t((mods & kShift ? shifted : plain)[i]);
  return 0;
}
} // namespace shixin
