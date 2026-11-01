// SPDX-License-Identifier: GPL-3.0-or-later
#include "service.h"
#include <new>
namespace shixin {
HMODULE module = nullptr;
std::atomic<long> objects{0};
class Factory final : public IClassFactory {
  std::atomic<ULONG> refs_{1};

public:
  Factory() { ++objects; }
  ~Factory() { --objects; }
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid, void **p) override {
    if (!p)
      return E_POINTER;
    *p = nullptr;
    if (iid != IID_IUnknown && iid != IID_IClassFactory)
      return E_NOINTERFACE;
    *p = static_cast<IClassFactory *>(this);
    AddRef();
    return S_OK;
  }
  ULONG STDMETHODCALLTYPE AddRef() override { return ++refs_; }
  ULONG STDMETHODCALLTYPE Release() override {
    auto n = --refs_;
    if (!n)
      delete this;
    return n;
  }
  HRESULT STDMETHODCALLTYPE CreateInstance(IUnknown *outer, REFIID iid,
                                           void **p) override {
    if (!p)
      return E_POINTER;
    *p = nullptr;
    if (outer)
      return CLASS_E_NOAGGREGATION;
    auto s = new (std::nothrow) Service;
    if (!s)
      return E_OUTOFMEMORY;
    auto hr = s->QueryInterface(iid, p);
    s->Release();
    return hr;
  }
  HRESULT STDMETHODCALLTYPE LockServer(BOOL lock) override {
    if (lock)
      ++objects;
    else
      --objects;
    return S_OK;
  }
};
} // namespace shixin
extern "C" BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID) {
  // No COM, windows, pipes, threads, files, or library loading under the
  // loader lock. The worker starts only during TSF activation.
  if (reason == DLL_PROCESS_ATTACH)
    shixin::module = instance;
  return TRUE;
}
extern "C" HRESULT __declspec(dllexport) WINAPI DllGetClassObject(REFCLSID cls,
                                                                  REFIID iid,
                                                                  void **p) {
  if (!p)
    return E_POINTER;
  *p = nullptr;
  if (cls != shixin::kClass)
    return CLASS_E_CLASSNOTAVAILABLE;
  auto factory = new (std::nothrow) shixin::Factory;
  if (!factory)
    return E_OUTOFMEMORY;
  auto hr = factory->QueryInterface(iid, p);
  factory->Release();
  return hr;
}
extern "C" HRESULT __declspec(dllexport) WINAPI DllCanUnloadNow() {
  return shixin::objects.load() == 0 ? S_OK : S_FALSE;
}
