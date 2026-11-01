// SPDX-License-Identifier: GPL-3.0-or-later
#include "service.h"
#include <cstdlib>
#include <iostream>
using namespace shixin;
#define CHECK(x)                                                               \
  do {                                                                         \
    if (!(x)) {                                                                \
      std::cerr << __LINE__ << ": " #x "\n";                                   \
      std::abort();                                                            \
    }                                                                          \
  } while (0)
#define UNKNOWN                                                                \
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID, void **p) override {        \
    *p = nullptr;                                                              \
    return E_NOINTERFACE;                                                      \
  }                                                                            \
  ULONG STDMETHODCALLTYPE AddRef() override { return 2; }                      \
  ULONG STDMETHODCALLTYPE Release() override { return 1; }
// A text store that keeps processing app selection changes while deferring
// our edit callback. Nothing connects to an engine or registers an IME.
struct Range final : ITfRange {
  UNKNOWN
  std::wstring text = L"document";
  bool selected = false;
  unsigned writes = 0;
  HRESULT STDMETHODCALLTYPE SetText(TfEditCookie, DWORD, const WCHAR *s,
                                    LONG n) override {
    if (selected)
      text.assign(s, n);
    else
      text.append(s, n);
    ++writes;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE Collapse(TfEditCookie, TfAnchor) override {
    selected = false;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetText(TfEditCookie, DWORD, WCHAR *, ULONG,
                                    ULONG *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetFormattedText(TfEditCookie,
                                             IDataObject **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetEmbedded(TfEditCookie, REFGUID, REFIID,
                                        IUnknown **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE InsertEmbedded(TfEditCookie, DWORD,
                                           IDataObject *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE ShiftStart(TfEditCookie, LONG, LONG *,
                                       const TF_HALTCOND *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE ShiftEnd(TfEditCookie, LONG, LONG *,
                                     const TF_HALTCOND *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE ShiftStartToRange(TfEditCookie, ITfRange *,
                                              TfAnchor) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE ShiftEndToRange(TfEditCookie, ITfRange *,
                                            TfAnchor) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE ShiftStartRegion(TfEditCookie, TfShiftDir,
                                             BOOL *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE ShiftEndRegion(TfEditCookie, TfShiftDir,
                                           BOOL *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE IsEmpty(TfEditCookie, BOOL *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE IsEqualStart(TfEditCookie, ITfRange *, TfAnchor,
                                         BOOL *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE IsEqualEnd(TfEditCookie, ITfRange *, TfAnchor,
                                       BOOL *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE CompareStart(TfEditCookie, ITfRange *, TfAnchor,
                                         LONG *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE CompareEnd(TfEditCookie, ITfRange *, TfAnchor,
                                       LONG *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE AdjustForInsert(TfEditCookie, ULONG,
                                            BOOL *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetGravity(TfGravity *, TfGravity *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE SetGravity(TfEditCookie, TfGravity,
                                       TfGravity) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE Clone(ITfRange **) override { return E_NOTIMPL; }
  HRESULT STDMETHODCALLTYPE GetContext(ITfContext **) override {
    return E_NOTIMPL;
  }
};
struct Context final : ITfContext {
  UNKNOWN
  Range range;
  ITfEditSession *waiting = nullptr;
  BOOL own = FALSE;
  bool read_only = false;
  HRESULT STDMETHODCALLTYPE RequestEditSession(TfClientId, ITfEditSession *e,
                                               DWORD,
                                               HRESULT *result) override {
    CHECK(!waiting);
    waiting = e;
    e->AddRef();
    *result = TF_S_ASYNC;
    return S_OK;
  }
  void complete() {
    CHECK(waiting);
    auto e = waiting;
    waiting = nullptr;
    own = TRUE;
    CHECK(SUCCEEDED(e->DoEditSession(1)));
    own = FALSE;
    e->Release();
  }
  HRESULT STDMETHODCALLTYPE InWriteSession(TfClientId, BOOL *out) override {
    *out = own;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetSelection(TfEditCookie, ULONG, ULONG,
                                         TF_SELECTION *out, ULONG *n) override {
    *out = {&range, {TF_AE_NONE, FALSE}};
    range.AddRef();
    *n = 1;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE SetSelection(TfEditCookie, ULONG,
                                         const TF_SELECTION *) override {
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetStatus(TF_STATUS *s) override {
    *s = {};
    s->dwStaticFlags = read_only ? TF_SD_READONLY : 0;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetStart(TfEditCookie, ITfRange **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetEnd(TfEditCookie, ITfRange **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetActiveView(ITfContextView **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE EnumViews(IEnumTfContextViews **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetProperty(REFGUID, ITfProperty **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetAppProperty(REFGUID,
                                           ITfReadOnlyProperty **) override {
    return E_FAIL;
  }
  HRESULT STDMETHODCALLTYPE TrackProperties(const GUID **, ULONG, const GUID **,
                                            ULONG,
                                            ITfReadOnlyProperty **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE EnumProperties(IEnumTfProperties **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetDocumentMgr(ITfDocumentMgr **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE CreateRangeBackup(TfEditCookie, ITfRange *,
                                              ITfRangeBackup **) override {
    return E_NOTIMPL;
  }
};
struct Changed final : ITfEditRecord {
  UNKNOWN
  HRESULT STDMETHODCALLTYPE GetSelectionStatus(BOOL *changed) override {
    *changed = TRUE;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetTextAndPropertyUpdates(
      DWORD, const GUID **, ULONG, IEnumTfRanges **) override {
    return E_NOTIMPL;
  }
};
namespace shixin {
HMODULE module = nullptr;
std::atomic<long> objects{0};
struct ServiceTest {
  static void prepare(Service &s, Context &c) {
    s.active_ = true;
    s.context_ = &c;
    c.AddRef();
    s.epoch_ = 1;
    s.core_.secure = false;
    s.core_.ctx = 1;
    s.transport_ = std::make_shared<Transport>(nullptr, L"");
    s.recovering_ = true;
    s.fallback_[0] = 'n';
    s.fallback_size_ = 1;
    s.schedule();
    CHECK(c.waiting);
  }
  static void run() {
    // Ctrl+A and mouse selections invalidate an already-requested callback.
    for (bool keyboard : {false, true}) {
      Context c;
      auto owner = std::make_unique<Service>();
      auto &s = *owner;
      prepare(s, c);
      if (keyboard) {
        BOOL eaten;
        s.handle(&c, VK_CONTROL, 0, false, &eaten);
        CHECK(!eaten);
      }
      c.range.selected = true;
      Changed changed;
      s.OnEndEdit(&c, 1, &changed);
      c.complete();
      CHECK(c.range.text == L"document" && c.range.writes == 0);
    }
    // Own edits must not be mistaken for a user's selection change.
    {
      Context c;
      auto owner = std::make_unique<Service>();
      auto &s = *owner;
      prepare(s, c);
      c.own = TRUE;
      Changed changed;
      s.OnEndEdit(&c, 1, &changed);
      c.own = FALSE;
      CHECK(s.recovering_);
      c.complete();
      CHECK(c.range.text == L"documentn");
    }
    // New literal keys stay ordered, and test/delivery callbacks deduplicate.
    {
      Context c;
      auto owner = std::make_unique<Service>();
      auto &s = *owner;
      prepare(s, c);
      BOOL eaten;
      s.OnTestKeyDown(&c, 'I', 0, &eaten);
      CHECK(eaten);
      s.OnKeyDown(&c, 'I', 0, &eaten);
      CHECK(eaten);
      c.complete();
      CHECK(c.range.text == L"documentni");
    }
    // Exhaustion invalidates the delayed callback before passing text.
    {
      Context c;
      auto owner = std::make_unique<Service>();
      auto &s = *owner;
      prepare(s, c);
      s.fallback_size_ = s.fallback_.size();
      BOOL eaten;
      s.handle(&c, 'A', 0, false, &eaten);
      CHECK(!eaten);
      c.range.selected = true;
      c.complete();
      CHECK(c.range.writes == 0);
    }
    // Focus loss invalidates both the callback and its literal tail.
    {
      Context c, other;
      auto owner = std::make_unique<Service>();
      auto &s = *owner;
      prepare(s, c);
      s.focus(&other);
      c.complete();
      CHECK(c.range.writes == 0 && other.range.writes == 0);
    }
    // Security is checked again when the delayed callback finally runs.
    {
      Context c;
      auto owner = std::make_unique<Service>();
      auto &s = *owner;
      prepare(s, c);
      c.read_only = true;
      c.complete();
      CHECK(c.range.writes == 0 && s.core_.secure && s.fallback_size_ == 0);
    }
    // A full edit queue cannot advance received past an unretained reply.
    {
      Context c;
      auto owner = std::make_unique<Service>();
      auto &s = *owner;
      prepare(s, c);
      c.complete();
      s.core_.connected = true;
      s.core_.secure = false;
      s.core_.ctx = 1;
      s.core_.installed[1] = true;
      s.core_.sent = 33;
      for (uint32_t seq = 1; seq <= 33; ++seq) {
        s.core_.pending[seq] = {seq, 'a', false, true};
        Frame f;
        f.begin(ResultType);
        f.u32(1);
        f.u32(seq);
        f.u16(0);
        f.u16(1);
        f.string("");
        f.string("");
        f.u32(0);
        f.u32(0);
        f.u32(0);
        f.u16(0);
        f.u16(0);
        f.u16(0);
        f.string("");
        f.end();
        s.receive_result(f.view());
      }
      CHECK(s.core_.received == 32 && s.fallback_size_ == 1 &&
            s.fallback_[0] == 'a');
      CHECK(s.edit_write_ - s.edit_read_ == 32);
    }
  }
};
} // namespace shixin
int main() {
  ServiceTest::run();
  CHECK(objects == 0);
  std::cout << "deferred recovery and edit overflow passed\n";
}
