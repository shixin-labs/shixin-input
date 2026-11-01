// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <msctf.h>
#include <windows.h>
namespace shixin {
// The extended interface ABI is also declared here for SDKs (including
// MinGW) that expose only ITfTextInputProcessor. IID from Microsoft's SDK.
inline constexpr IID kProcessorEx = {
    0x6e4e2102,
    0xf9cd,
    0x433d,
    {0xb4, 0x96, 0x30, 0x3c, 0xe0, 0x3a, 0x65, 0x07}};
struct ProcessorEx : ITfTextInputProcessor {
  virtual HRESULT STDMETHODCALLTYPE ActivateEx(ITfThreadMgr *, TfClientId,
                                               DWORD) = 0;
};
inline constexpr CLSID kClass = {
    0xaac1a491,
    0xe694,
    0x4610,
    {0x96, 0xb5, 0xa4, 0x76, 0xa1, 0x44, 0x12, 0x24}};
} // namespace shixin
