// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "keys.h"
namespace shixin {
// Keep later literal key-downs behind a delayed recovery edit. The caller
// invalidates recovery before passing a key this bounded tail cannot hold.
inline bool append_literal(char *tail, size_t &size, size_t capacity,
                           uint32_t sym, uint32_t mods) {
  if (sym < 0x20 || sym > 0x7e ||
      (mods & (kControl | kAlt | kSuper | kRelease)) || size == capacity)
    return false;
  tail[size++] = char(sym);
  return true;
}
} // namespace shixin
