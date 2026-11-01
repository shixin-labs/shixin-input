// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <msctf.h>
#include <windows.h>
namespace shixin {
inline constexpr GUID kDisplayInput = {
    0x67987f13,
    0x1ba3,
    0x4bcb,
    {0x87, 0xb5, 0xb5, 0x73, 0x80, 0x9d, 0x2e, 0xcc}};
inline constexpr GUID kDisplayTarget = {
    0xe856e4d0,
    0x5841,
    0x44dd,
    {0xbb, 0x71, 0xb1, 0x1f, 0xe5, 0x4c, 0xc4, 0x89}};
HRESULT enum_display_attributes(IEnumTfDisplayAttributeInfo **);
HRESULT display_attribute_info(REFGUID, ITfDisplayAttributeInfo **);
void clear_composition_attributes(TfEditCookie, ITfComposition *);
HRESULT end_composition(TfEditCookie, ITfComposition *);
} // namespace shixin
