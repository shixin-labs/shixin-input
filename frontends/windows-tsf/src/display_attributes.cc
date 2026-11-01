// SPDX-License-Identifier: GPL-3.0-or-later
#include "display_attributes.h"
#include <array>
#include <atomic>
namespace shixin {
extern std::atomic<long> objects;
namespace {
// Metadata has process lifetime; handing it to TSF never allocates on a key
// callback. Outstanding references still pin the COM module through objects.
class Attribute final : public ITfDisplayAttributeInfo {
  std::atomic<ULONG> refs_{0};
  bool target_;

public:
  explicit Attribute(bool target) : target_(target) {}
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid, void **out) override {
    if (!out)
      return E_POINTER;
    *out = nullptr;
    if (iid != IID_IUnknown && iid != IID_ITfDisplayAttributeInfo)
      return E_NOINTERFACE;
    *out = static_cast<ITfDisplayAttributeInfo *>(this);
    AddRef();
    return S_OK;
  }
  ULONG STDMETHODCALLTYPE AddRef() override {
    auto n = ++refs_;
    if (n == 1)
      ++objects;
    return n;
  }
  ULONG STDMETHODCALLTYPE Release() override {
    auto n = --refs_;
    if (!n)
      --objects;
    return n;
  }
  HRESULT STDMETHODCALLTYPE GetGUID(GUID *out) override {
    if (!out)
      return E_POINTER;
    *out = target_ ? kDisplayTarget : kDisplayInput;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetDescription(BSTR *out) override {
    if (!out)
      return E_POINTER;
    *out = SysAllocString(target_ ? L"拾新：当前输入段" : L"拾新：拼音输入");
    return *out ? S_OK : E_OUTOFMEMORY;
  }
  HRESULT STDMETHODCALLTYPE
  GetAttributeInfo(TF_DISPLAYATTRIBUTE *out) override {
    if (!out)
      return E_POINTER;
    *out = {};
    out->crText.type = TF_CT_NONE;
    out->crBk.type = TF_CT_NONE;
    out->crLine.type = TF_CT_SYSCOLOR;
    out->crLine.nIndex = COLOR_WINDOWTEXT;
    out->lsStyle = target_ ? TF_LS_SOLID : TF_LS_DOT;
    out->bAttr = target_ ? TF_ATTR_TARGET_NOTCONVERTED : TF_ATTR_INPUT;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE
  SetAttributeInfo(const TF_DISPLAYATTRIBUTE *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE Reset() override { return S_OK; }
};
Attribute input(false), target(true);
class Attributes final : public IEnumTfDisplayAttributeInfo {
  std::atomic<ULONG> refs_{0};
  ULONG next_ = 0;

public:
  bool reserve(ULONG next) {
    ULONG empty = 0;
    if (!refs_.compare_exchange_strong(empty, 1))
      return false;
    next_ = next;
    ++objects;
    return true;
  }
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid, void **out) override {
    if (!out)
      return E_POINTER;
    *out = nullptr;
    if (iid != IID_IUnknown && iid != IID_IEnumTfDisplayAttributeInfo)
      return E_NOINTERFACE;
    *out = static_cast<IEnumTfDisplayAttributeInfo *>(this);
    AddRef();
    return S_OK;
  }
  ULONG STDMETHODCALLTYPE AddRef() override { return ++refs_; }
  ULONG STDMETHODCALLTYPE Release() override {
    auto n = --refs_;
    if (!n)
      --objects;
    return n;
  }
  HRESULT STDMETHODCALLTYPE Clone(IEnumTfDisplayAttributeInfo **) override;
  HRESULT STDMETHODCALLTYPE Next(ULONG n, ITfDisplayAttributeInfo **out,
                                 ULONG *fetched) override {
    if (!out || (!fetched && n != 1))
      return E_POINTER;
    ULONG count = 0;
    while (count < n && next_ < 2) {
      out[count] = next_++ ? &target : &input;
      out[count++]->AddRef();
    }
    if (fetched)
      *fetched = count;
    return count == n ? S_OK : S_FALSE;
  }
  HRESULT STDMETHODCALLTYPE Reset() override {
    next_ = 0;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE Skip(ULONG n) override {
    auto left = 2 - next_;
    next_ += n < left ? n : left;
    return n <= left ? S_OK : S_FALSE;
  }
};
std::array<Attributes, 16> enumerators;
HRESULT enumerator(ULONG next, IEnumTfDisplayAttributeInfo **out) {
  if (!out)
    return E_POINTER;
  *out = nullptr;
  for (auto &entry : enumerators)
    if (entry.reserve(next)) {
      *out = &entry;
      return S_OK;
    }
  return E_OUTOFMEMORY;
}
HRESULT Attributes::Clone(IEnumTfDisplayAttributeInfo **out) {
  return enumerator(next_, out);
}
} // namespace
HRESULT enum_display_attributes(IEnumTfDisplayAttributeInfo **out) {
  return enumerator(0, out);
}
HRESULT display_attribute_info(REFGUID id, ITfDisplayAttributeInfo **out) {
  if (!out)
    return E_POINTER;
  *out = id == kDisplayInput    ? &input
         : id == kDisplayTarget ? &target
                                : nullptr;
  if (!*out)
    return E_INVALIDARG;
  (*out)->AddRef();
  return S_OK;
}
void clear_composition_attributes(TfEditCookie ec,
                                  ITfComposition *composition) {
  ITfRange *range = nullptr;
  ITfContext *context = nullptr;
  ITfProperty *property = nullptr;
  if (SUCCEEDED(composition->GetRange(&range)) && range) {
    if (SUCCEEDED(range->GetContext(&context)) && context) {
      if (SUCCEEDED(context->GetProperty(GUID_PROP_ATTRIBUTE, &property)) &&
          property) {
        property->Clear(ec, range);
        property->Release();
      }
      context->Release();
    }
    range->Release();
  }
}
HRESULT end_composition(TfEditCookie ec, ITfComposition *composition) {
  clear_composition_attributes(ec, composition);
  return composition->EndComposition(ec);
}
} // namespace shixin
