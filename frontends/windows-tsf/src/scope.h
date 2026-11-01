// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <cstdint>
namespace shixin {
// Some text stores (including Windows 11 Notepad and shell searches) expose
// the scope property but return E_FAIL/VT_EMPTY when no scope was supplied.
// Actual lock/disconnect errors and malformed supplied values remain guarded.
inline bool absent_input_scope(uint32_t hr, uint16_t variant) {
  return variant == 0 &&
         (hr == 0 || hr == 1 || hr == 0x80004001u || hr == 0x80004005u);
}

} // namespace shixin
