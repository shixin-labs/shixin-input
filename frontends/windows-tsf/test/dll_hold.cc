// SPDX-License-Identifier: GPL-3.0-or-later
// Disposable-VM installer fixture: hold the public DLL without activating TSF.
#include <windows.h>
int wmain(int argc, wchar_t **argv) {
  if (argc != 4)
    return 2;
  HMODULE dll = LoadLibraryExW(argv[1], nullptr,
                              LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR |
                                  LOAD_LIBRARY_SEARCH_SYSTEM32);
  if (!dll)
    return 3;
  auto unload = reinterpret_cast<HRESULT(WINAPI *)()>(
      GetProcAddress(dll, "DllCanUnloadNow"));
  if (!unload || unload() != S_OK) {
    FreeLibrary(dll);
    return 4;
  }
  HANDLE ready = CreateFileW(argv[2], GENERIC_WRITE, FILE_SHARE_READ, nullptr,
                             CREATE_NEW, FILE_ATTRIBUTE_NORMAL, nullptr);
  if (ready == INVALID_HANDLE_VALUE) {
    FreeLibrary(dll);
    return 5;
  }
  CloseHandle(ready);
  const auto deadline = GetTickCount64() + 600000;
  while (GetFileAttributesW(argv[3]) == INVALID_FILE_ATTRIBUTES &&
         GetTickCount64() < deadline)
    Sleep(50);
  const bool released = GetFileAttributesW(argv[3]) != INVALID_FILE_ATTRIBUTES;
  FreeLibrary(dll);
  return released ? 0 : 6;
}
