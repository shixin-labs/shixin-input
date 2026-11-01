// SPDX-License-Identifier: GPL-3.0-or-later
#include "service.h"
#include <cstdio>
namespace shixin {
namespace {
constexpr LANGID kLanguage = 0x0804;
HRESULT step(const char *name, HRESULT hr) {
#ifndef SHIXIN_TSF_TRACE
  if (SUCCEEDED(hr))
    return hr;
#endif
  char line[192];
  int n = std::snprintf(line, sizeof(line), "TSF %s: HRESULT 0x%08lx\n", name,
                        static_cast<unsigned long>(hr));
  DWORD written = 0;
  if (n > 0 && n < int(sizeof(line)))
    WriteFile(GetStdHandle(STD_ERROR_HANDLE), line, DWORD(n), &written,
              nullptr);
  return hr;
}
std::wstring class_key() {
  wchar_t guid[40]{};
  StringFromGUID2(kClass, guid, 40);
  return std::wstring(L"Software\\Classes\\CLSID\\") + guid;
}
HRESULT value(const std::wstring &path, const wchar_t *name,
              const std::wstring &text) {
  HKEY key = nullptr;
  auto status = RegCreateKeyExW(HKEY_CURRENT_USER, path.c_str(), 0, nullptr, 0,
                                KEY_WRITE, nullptr, &key, nullptr);
  if (status != ERROR_SUCCESS)
    return HRESULT_FROM_WIN32(status);
  status = RegSetValueExW(key, name, 0, REG_SZ,
                          reinterpret_cast<const BYTE *>(text.c_str()),
                          DWORD((text.size() + 1) * sizeof(wchar_t)));
  RegCloseKey(key);
  return HRESULT_FROM_WIN32(status);
}
struct Com {
  HRESULT status = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
  ~Com() {
    if (SUCCEEDED(status))
      CoUninitialize();
  }
  bool good() const {
    return SUCCEEDED(status) || status == RPC_E_CHANGED_MODE;
  }
};
} // namespace
} // namespace shixin
STDAPI DllRegisterServer() {
  using namespace shixin;
  Com com;
  if (!com.good())
    return com.status;
  std::array<wchar_t, 32768> path{};
  auto n = GetModuleFileNameW(module, path.data(), DWORD(path.size()));
  if (!n || n == path.size())
    return E_FAIL;
  auto key = class_key();
  HKEY previous = nullptr;
  bool existing =
      RegOpenKeyExW(HKEY_CURRENT_USER, (key + L"\\InprocServer32").c_str(), 0,
                    KEY_READ, &previous) == ERROR_SUCCESS;
  if (existing) {
    std::array<wchar_t, 32768> old{};
    DWORD bytes = DWORD(old.size() * sizeof(wchar_t)), type = 0;
    auto status =
        RegQueryValueExW(previous, nullptr, nullptr, &type,
                         reinterpret_cast<BYTE *>(old.data()), &bytes);
    RegCloseKey(previous);
    old.back() = L'\0';
    if (status != ERROR_SUCCESS || type != REG_SZ ||
        bytes > old.size() * sizeof(wchar_t) ||
        _wcsicmp(old.data(), path.data()) != 0)
      return E_ACCESSDENIED;
  }
  HRESULT hr = value(key, nullptr, L"拾新输入法 Shixin Input");
  if (SUCCEEDED(hr))
    hr =
        value(key + L"\\InprocServer32", nullptr, std::wstring(path.data(), n));
  if (SUCCEEDED(hr))
    hr = value(key + L"\\InprocServer32", L"ThreadingModel", L"Apartment");
  ITfInputProcessorProfiles *profiles = nullptr;
  ITfCategoryMgr *categories = nullptr;
  if (SUCCEEDED(hr))
    hr = CoCreateInstance(CLSID_TF_InputProcessorProfiles, nullptr,
                          CLSCTX_INPROC_SERVER, IID_ITfInputProcessorProfiles,
                          reinterpret_cast<void **>(&profiles));
  if (SUCCEEDED(hr))
    hr =
        step("ITfInputProcessorProfiles::Register", profiles->Register(kClass));
  if (SUCCEEDED(hr)) {
    constexpr wchar_t name[] = L"拾新输入法";
    hr = step("ITfInputProcessorProfiles::AddLanguageProfile",
              profiles->AddLanguageProfile(kClass, kLanguage, kProfile, name,
                                           UINT(std::size(name) - 1),
                                           path.data(), n, 0));
  }
  if (SUCCEEDED(hr))
    hr = step(
        "ITfInputProcessorProfiles::EnableLanguageProfile",
        profiles->EnableLanguageProfile(kClass, kLanguage, kProfile, TRUE));
  if (SUCCEEDED(hr))
    hr = CoCreateInstance(CLSID_TF_CategoryMgr, nullptr, CLSCTX_INPROC_SERVER,
                          IID_ITfCategoryMgr,
                          reinterpret_cast<void **>(&categories));
  if (SUCCEEDED(hr))
    hr = step(
        "ITfCategoryMgr::RegisterCategory",
        categories->RegisterCategory(kClass, GUID_TFCAT_TIP_KEYBOARD, kClass));
  if (FAILED(hr) && !existing) {
    if (categories)
      categories->UnregisterCategory(kClass, GUID_TFCAT_TIP_KEYBOARD, kClass);
    if (profiles) {
      profiles->RemoveLanguageProfile(kClass, kLanguage, kProfile);
      profiles->Unregister(kClass);
    }
    RegDeleteTreeW(HKEY_CURRENT_USER, key.c_str());
  }
  if (categories)
    categories->Release();
  if (profiles)
    profiles->Release();
  return hr;
}
STDAPI DllUnregisterServer() {
  using namespace shixin;
  Com com;
  if (!com.good())
    return com.status;
  ITfInputProcessorProfiles *profiles = nullptr;
  auto hr = CoCreateInstance(
      CLSID_TF_InputProcessorProfiles, nullptr, CLSCTX_INPROC_SERVER,
      IID_ITfInputProcessorProfiles, reinterpret_cast<void **>(&profiles));
  if (SUCCEEDED(hr)) {
    // Unregister removes every language profile and category. Removing the
    // category afterwards returns E_FAIL even after a successful uninstall.
    hr = step("ITfInputProcessorProfiles::Unregister",
              profiles->Unregister(kClass));
    profiles->Release();
  }
  // Keep the COM class usable if TSF removal failed (for example because the
  // machine TIP needs elevation). Never leave a registered TIP without its DLL.
  if (FAILED(hr))
    return hr;
  auto status = RegDeleteTreeW(HKEY_CURRENT_USER, class_key().c_str());
  if (status != ERROR_SUCCESS && status != ERROR_FILE_NOT_FOUND)
    return HRESULT_FROM_WIN32(status);
  return S_OK;
}
