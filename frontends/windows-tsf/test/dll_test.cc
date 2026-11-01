// SPDX-License-Identifier: GPL-3.0-or-later
#include "service.h"
#include <iostream>
#define CHECK(x)                                                               \
  do {                                                                         \
    if (!(x)) {                                                                \
      std::cerr << __LINE__ << ": " #x "\n";                                   \
      return 1;                                                                \
    }                                                                          \
  } while (0)
int wmain(int argc, wchar_t **argv) {
  CHECK(argc == 2);
  // Never activate or register a profile here: tests must not connect to a
  // live engine or change the machine's input sources.
  HMODULE dll = LoadLibraryExW(argv[1], nullptr,
                               LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR |
                                   LOAD_LIBRARY_SEARCH_SYSTEM32);
  CHECK(dll);
  auto get = reinterpret_cast<HRESULT(WINAPI *)(REFCLSID, REFIID, void **)>(
      GetProcAddress(dll, "DllGetClassObject"));
  auto unload = reinterpret_cast<HRESULT(WINAPI *)()>(
      GetProcAddress(dll, "DllCanUnloadNow"));
  CHECK(get && unload);
  CHECK(unload() == S_OK);
  IClassFactory *factory = nullptr;
  CHECK(SUCCEEDED(get(shixin::kClass, IID_IClassFactory,
                      reinterpret_cast<void **>(&factory))));
  CHECK(unload() == S_FALSE);
  ITfTextInputProcessor *tip = nullptr;
  CHECK(factory->CreateInstance(factory, IID_ITfTextInputProcessor,
                                reinterpret_cast<void **>(&tip)) ==
        CLASS_E_NOAGGREGATION);
  CHECK(SUCCEEDED(factory->CreateInstance(nullptr, IID_ITfTextInputProcessor,
                                          reinterpret_cast<void **>(&tip))));
  shixin::ProcessorEx *extended = nullptr;
  CHECK(SUCCEEDED(tip->QueryInterface(shixin::kProcessorEx,
                                      reinterpret_cast<void **>(&extended))));
  CHECK(extended->ActivateEx(nullptr, 1, TF_TMAE_SECUREMODE) == E_ACCESSDENIED);
  extended->Release();
  ITfKeyEventSink *keys = nullptr;
  CHECK(SUCCEEDED(tip->QueryInterface(IID_ITfKeyEventSink,
                                      reinterpret_cast<void **>(&keys))));
  IUnknown *a = nullptr;
  IUnknown *b = nullptr;
  CHECK(SUCCEEDED(
      tip->QueryInterface(IID_IUnknown, reinterpret_cast<void **>(&a))));
  CHECK(SUCCEEDED(
      keys->QueryInterface(IID_IUnknown, reinterpret_cast<void **>(&b))));
  CHECK(a == b);
  a->Release();
  b->Release();
  for (int i = 0; i < 10000; ++i) {
    BOOL eaten = TRUE;
    CHECK(SUCCEEDED(keys->OnTestKeyDown(nullptr, 'A', 0, &eaten)));
    CHECK(!eaten);
  }
  CHECK(keys->OnKeyDown(nullptr, 'A', 0, nullptr) == E_POINTER);
  CHECK(tip->Deactivate() == S_OK);
  keys->Release();
  tip->Release();
  factory->Release();
  CHECK(unload() == S_OK);
  CHECK(FreeLibrary(dll));
  std::cout << "COM factory and disconnected key routing passed\n";
  return 0;
}
