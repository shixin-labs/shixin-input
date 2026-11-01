// SPDX-License-Identifier: GPL-3.0-or-later
#include "display_attributes.h"
#include "ids.h"
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
  auto install = reinterpret_cast<HRESULT(WINAPI *)(BOOL, LPCWSTR)>(
      GetProcAddress(dll, "DllInstall"));
  CHECK(install && install(FALSE, L"invalid") == E_INVALIDARG);
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
  ITfDisplayAttributeProvider *attributes = nullptr;
  CHECK(SUCCEEDED(tip->QueryInterface(IID_ITfDisplayAttributeProvider,
                                      reinterpret_cast<void **>(&attributes))));
  IEnumTfDisplayAttributeInfo *items = nullptr, *copy = nullptr;
  CHECK(SUCCEEDED(attributes->EnumDisplayAttributeInfo(&items)));
  ITfDisplayAttributeInfo *info[2]{};
  ULONG fetched = 0;
  CHECK(items->Next(2, info, &fetched) == S_OK && fetched == 2);
  for (unsigned i = 0; i < 2; ++i) {
    GUID id{};
    TF_DISPLAYATTRIBUTE style{};
    CHECK(info[i]->GetGUID(&id) == S_OK);
    CHECK(id == (i ? shixin::kDisplayTarget : shixin::kDisplayInput));
    CHECK(info[i]->GetAttributeInfo(&style) == S_OK);
    CHECK(style.lsStyle == (i ? TF_LS_SOLID : TF_LS_DOT));
    CHECK(style.bAttr == (i ? TF_ATTR_TARGET_NOTCONVERTED : TF_ATTR_INPUT));
    CHECK(style.crLine.type == TF_CT_SYSCOLOR &&
          style.crLine.nIndex == COLOR_WINDOWTEXT);
    info[i]->Release();
  }
  CHECK(items->Reset() == S_OK && items->Skip(1) == S_OK);
  CHECK(items->Clone(&copy) == S_OK);
  CHECK(copy->Next(1, info, &fetched) == S_OK && fetched == 1);
  info[0]->Release();
  copy->Release();
  items->Release();
  CHECK(attributes->GetDisplayAttributeInfo(shixin::kDisplayInput, &info[0]) ==
        S_OK);
  attributes->Release();
  // The metadata itself keeps the DLL loaded after its provider is released.
  CHECK(unload() == S_FALSE);
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
  CHECK(unload() == S_FALSE);
  info[0]->Release();
  CHECK(unload() == S_OK);
  CHECK(FreeLibrary(dll));
  std::cout << "COM factory and disconnected key routing passed\n";
  return 0;
}
