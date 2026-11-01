// SPDX-License-Identifier: GPL-3.0-or-later
#include "service.h"
#include <aclapi.h>
#include <cstdio>
namespace shixin {
namespace {
constexpr LANGID kLanguage = 0x0804;
// FACILITY_ITF: the TIP is gone, but COM cleanup failed. Paired with the
// elevated helper's exit 73 in register_windows.go; user rollback is unsafe.
constexpr HRESULT kUnregisterCleanupFailure = static_cast<HRESULT>(0x8004a001u);
// Advertise only capabilities implemented by this DLL. In particular, do
// not advertise UIElement/search integration: candidates live in our host.
const GUID kCategories[] = {GUID_TFCAT_TIP_KEYBOARD,
                            GUID_TFCAT_TIPCAP_IMMERSIVESUPPORT};
HRESULT allow_sandbox_load(const wchar_t *path) {
  BYTE sid[SECURITY_MAX_SID_SIZE]{};
  DWORD size = sizeof(sid);
  if (!CreateWellKnownSid(WinBuiltinAnyPackageSid, nullptr, sid, &size))
    return HRESULT_FROM_WIN32(GetLastError());
  PACL old = nullptr, acl = nullptr;
  PSECURITY_DESCRIPTOR sd = nullptr;
  auto status =
      GetNamedSecurityInfoW(path, SE_FILE_OBJECT, DACL_SECURITY_INFORMATION,
                            nullptr, nullptr, &old, nullptr, &sd);
  if (status != ERROR_SUCCESS)
    return HRESULT_FROM_WIN32(status);
  EXPLICIT_ACCESSW entry{};
  entry.grfAccessPermissions = FILE_GENERIC_READ | FILE_GENERIC_EXECUTE;
  entry.grfAccessMode = GRANT_ACCESS;
  entry.Trustee.TrusteeForm = TRUSTEE_IS_SID;
  entry.Trustee.ptstrName = reinterpret_cast<LPWSTR>(sid);
  status = SetEntriesInAclW(1, &entry, old, &acl);
  if (status == ERROR_SUCCESS)
    status = SetNamedSecurityInfoW(const_cast<LPWSTR>(path), SE_FILE_OBJECT,
                                   DACL_SECURITY_INFORMATION, nullptr, nullptr,
                                   acl, nullptr);
  LocalFree(acl);
  LocalFree(sd);
  return HRESULT_FROM_WIN32(status);
}
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
HRESULT machine_tip_exists(bool &exists) {
  wchar_t clsid[40]{};
  StringFromGUID2(kClass, clsid, 40);
  HKEY tip = nullptr;
  auto status = RegOpenKeyExW(
      HKEY_LOCAL_MACHINE,
      (std::wstring(L"Software\\Microsoft\\CTF\\TIP\\") + clsid).c_str(), 0,
      KEY_READ, &tip);
  exists = status == ERROR_SUCCESS;
  if (tip)
    RegCloseKey(tip);
  return status == ERROR_FILE_NOT_FOUND ? S_OK : HRESULT_FROM_WIN32(status);
}
HRESULT check_class(HKEY root, const wchar_t *path, bool &existing) {
  HKEY key = nullptr;
  auto status = RegOpenKeyExW(root, (class_key() + L"\\InprocServer32").c_str(),
                              0, KEY_READ, &key);
  existing = status == ERROR_SUCCESS;
  if (status == ERROR_FILE_NOT_FOUND)
    return S_OK;
  if (status != ERROR_SUCCESS)
    return HRESULT_FROM_WIN32(status);
  std::array<wchar_t, 32768> old{};
  DWORD bytes = DWORD(sizeof(old)), type = 0;
  status = RegQueryValueExW(key, nullptr, nullptr, &type,
                            reinterpret_cast<BYTE *>(old.data()), &bytes);
  RegCloseKey(key);
  if (status != ERROR_SUCCESS || type != REG_SZ || bytes > sizeof(old) ||
      old.back() != L'\0' || _wcsicmp(old.data(), path) != 0)
    return E_ACCESSDENIED;
  return S_OK;
}
HRESULT value(const std::wstring &path, const wchar_t *name,
              const std::wstring &text) {
  HKEY key = nullptr;
  auto status = RegCreateKeyExW(HKEY_LOCAL_MACHINE, path.c_str(), 0, nullptr, 0,
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
  bool existing = false;
  HRESULT hr = check_class(HKEY_LOCAL_MACHINE, path.data(), existing);
  if (FAILED(hr))
    return hr;
  bool existing_tip = false;
  hr = machine_tip_exists(existing_tip);
  if (FAILED(hr))
    return hr;
  hr = step("DLL sandbox read/execute access", allow_sandbox_load(path.data()));
  if (SUCCEEDED(hr))
    hr = value(key, nullptr, L"拾新输入法 Shixin Input");
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
  if (SUCCEEDED(hr)) {
    ITfInputProcessorProfileMgr *manager = nullptr;
    hr = profiles->QueryInterface(IID_ITfInputProcessorProfileMgr,
                                  reinterpret_cast<void **>(&manager));
    constexpr wchar_t name[] = L"拾新输入法";
    if (SUCCEEDED(hr)) {
      // A machine registration must not implicitly enable the keyboard for
      // every account. The original caller enables its own profile later.
      hr = step("ITfInputProcessorProfileMgr::RegisterProfile",
                manager->RegisterProfile(kClass, kLanguage, kProfile, name,
                                         UINT(std::size(name) - 1), path.data(),
                                         n, 0, nullptr, 0, FALSE, 0));
      manager->Release();
    }
  }
  if (SUCCEEDED(hr))
    hr = CoCreateInstance(CLSID_TF_CategoryMgr, nullptr, CLSCTX_INPROC_SERVER,
                          IID_ITfCategoryMgr,
                          reinterpret_cast<void **>(&categories));
  for (const auto &category : kCategories) {
    if (FAILED(hr))
      break;
    hr = step("ITfCategoryMgr::RegisterCategory",
              categories->RegisterCategory(kClass, category, kClass));
  }
  if (FAILED(hr) && !existing) {
    if (profiles && !existing_tip) {
      profiles->Unregister(kClass);
    }
    RegDeleteTreeW(HKEY_LOCAL_MACHINE, key.c_str());
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
  std::array<wchar_t, 32768> path{};
  auto n = GetModuleFileNameW(module, path.data(), DWORD(path.size()));
  if (!n || n == path.size())
    return E_FAIL;
  bool existing = false;
  auto check = check_class(HKEY_LOCAL_MACHINE, path.data(), existing);
  if (FAILED(check))
    return check;
  ITfInputProcessorProfiles *profiles = nullptr;
  bool tip = false;
  auto hr = machine_tip_exists(tip);
  if (SUCCEEDED(hr) && tip)
    hr = CoCreateInstance(CLSID_TF_InputProcessorProfiles, nullptr,
                          CLSCTX_INPROC_SERVER, IID_ITfInputProcessorProfiles,
                          reinterpret_cast<void **>(&profiles));
  if (SUCCEEDED(hr) && tip) {
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
  auto status = RegDeleteTreeW(HKEY_LOCAL_MACHINE, class_key().c_str());
  if (status != ERROR_SUCCESS && status != ERROR_FILE_NOT_FOUND) {
    step("machine COM cleanup after TIP removal", HRESULT_FROM_WIN32(status));
    SetLastError(status);
    return kUnregisterCleanupFailure;
  }
  return S_OK;
}

// Called in the invoking user's process: enable after machine registration,
// disable before machine removal, or query to support uninstall rollback.
// InstallLayoutOrTip updates Windows' language settings as well as TSF;
// EnableLanguageProfile alone leaves a keyboard absent from Settings.
STDAPI DllInstall(BOOL install, LPCWSTR command) {
  using namespace shixin;
  const bool query = command && wcscmp(command, L"user-query") == 0;
  const bool cleanup = command && wcscmp(command, L"user-cleanup") == 0;
  if (!command || (!query && !cleanup && wcscmp(command, L"user") != 0))
    return E_INVALIDARG;
  Com com;
  if (!com.good())
    return com.status;
  if (query) {
    // A retry after a post-removal cleanup failure has no TIP left to query.
    // Do not mistake that for an enabled profile or prevent COM cleanup.
    bool tip = false;
    auto status = machine_tip_exists(tip);
    if (FAILED(status) || !tip)
      return FAILED(status) ? status : S_FALSE;
    ITfInputProcessorProfiles *profiles = nullptr;
    auto hr = CoCreateInstance(
        CLSID_TF_InputProcessorProfiles, nullptr, CLSCTX_INPROC_SERVER,
        IID_ITfInputProcessorProfiles, reinterpret_cast<void **>(&profiles));
    BOOL enabled = FALSE;
    if (SUCCEEDED(hr)) {
      hr = step("ITfInputProcessorProfiles::IsEnabledLanguageProfile",
                profiles->IsEnabledLanguageProfile(kClass, kLanguage, kProfile,
                                                   &enabled));
      profiles->Release();
    }
    return FAILED(hr) ? hr : (enabled ? S_OK : S_FALSE);
  }
  std::array<wchar_t, 32768> current{};
  auto n = GetModuleFileNameW(module, current.data(), DWORD(current.size()));
  if (!n || n == current.size())
    return E_FAIL;
  bool legacy = false;
  auto check = check_class(HKEY_CURRENT_USER, current.data(), legacy);
  if (FAILED(check))
    return check;
  if (cleanup) {
    if (!legacy)
      return S_OK;
    return HRESULT_FROM_WIN32(
        RegDeleteTreeW(HKEY_CURRENT_USER, class_key().c_str()));
  }
  HMODULE input =
      LoadLibraryExW(L"input.dll", nullptr, LOAD_LIBRARY_SEARCH_SYSTEM32);
  if (!input)
    return HRESULT_FROM_WIN32(GetLastError());
  auto apply = reinterpret_cast<BOOL(WINAPI *)(LPCWSTR, DWORD)>(
      GetProcAddress(input, "InstallLayoutOrTip"));
  HRESULT hr = E_NOINTERFACE;
  if (apply) {
    wchar_t clsid[40]{}, profile[40]{}, tip[96]{};
    StringFromGUID2(kClass, clsid, 40);
    StringFromGUID2(kProfile, profile, 40);
    // Unlike the language-list UI's ID, this API parses the language as a
    // number and needs the 0x prefix. A bare "0804" can return success while
    // leaving the intended user's keyboard list unchanged.
    swprintf_s(tip, L"0x%04x:%s%s;", kLanguage, clsid, profile);
    hr =
        step("InstallLayoutOrTip", apply(tip, install ? 0 : 1) ? S_OK : E_FAIL);
  }
  FreeLibrary(input);
  // Remove only this copy's legacy user COM entry. Otherwise it shadows the
  // machine class, including in restricted apps, after upgrading a dev build.
  if (SUCCEEDED(hr) && install && legacy) {
    // A rollback may be restoring an older HKCU-only installation. Keep
    // that class until a matching machine class can take over its work.
    bool machine = false;
    auto match = check_class(HKEY_LOCAL_MACHINE, current.data(), machine);
    if (FAILED(match))
      return match;
    if (!machine)
      return hr;
    auto status = RegDeleteTreeW(HKEY_CURRENT_USER, class_key().c_str());
    if (status != ERROR_SUCCESS && status != ERROR_FILE_NOT_FOUND)
      hr = HRESULT_FROM_WIN32(status);
  }
  return hr;
}
