// SPDX-License-Identifier: GPL-3.0-or-later
#include "service.h"
#include <algorithm>
#include <chrono>
#include <cstdlib>
#include <functional>
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
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID, void **p) override {
    *p = nullptr;
    return E_NOINTERFACE;
  }
  ULONG refs = 1;
  ULONG STDMETHODCALLTYPE AddRef() override { return ++refs; }
  ULONG STDMETHODCALLTYPE Release() override {
    auto n = --refs;
    if (!n && root)
      delete this;
    return n;
  }
  Range *root = nullptr;
  ITfContext *context = nullptr;
  LONG start = 0, end = 0;
  bool readable = true;
  bool empty_failure = false;
  std::function<void()> on_read;
  std::wstring text = L"document";
  bool selected = false;
  LONG selected_start = 0;
  unsigned writes = 0;
  HRESULT STDMETHODCALLTYPE SetText(TfEditCookie, DWORD, const WCHAR *s,
                                    LONG n) override {
    if (selected)
      text.replace(size_t(selected_start), text.size() - size_t(selected_start),
                   s, size_t(n));
    else
      text.append(s, n);
    ++writes;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE Collapse(TfEditCookie, TfAnchor anchor) override {
    if (root) {
      if (anchor == TF_ANCHOR_START)
        end = start;
      else
        start = end;
    }
    selected = false;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetText(TfEditCookie, DWORD, WCHAR *out, ULONG cap,
                                    ULONG *n) override {
    if (!root || !root->readable)
      return E_NOTIMPL;
    if (root->on_read)
      root->on_read();
    *n = std::min(cap, ULONG(end - start));
    std::copy_n(root->text.data() + start, *n, out);
    return S_OK;
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
  HRESULT STDMETHODCALLTYPE ShiftStart(TfEditCookie, LONG shift, LONG *moved,
                                       const TF_HALTCOND *) override {
    if (!root || !root->readable)
      return E_NOTIMPL;
    auto next = std::clamp(start + shift, LONG(0), LONG(root->text.size()));
    *moved = next - start;
    start = next;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE ShiftEnd(TfEditCookie, LONG shift, LONG *moved,
                                     const TF_HALTCOND *) override {
    if (!root)
      return E_NOTIMPL;
    auto next = std::clamp(end + shift, start, LONG(root->text.size()));
    *moved = next - end;
    end = next;
    return S_OK;
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
  HRESULT STDMETHODCALLTYPE IsEmpty(TfEditCookie, BOOL *empty) override {
    if (empty_failure || (root && root->empty_failure))
      return E_FAIL;
    *empty = root ? start == end : !selected;
    return S_OK;
  }
  LONG first() const {
    return root ? start : selected ? selected_start : LONG(text.size());
  }
  LONG last() const { return root ? end : LONG(text.size()); }
  HRESULT STDMETHODCALLTYPE IsEqualStart(TfEditCookie, ITfRange *other,
                                         TfAnchor anchor,
                                         BOOL *equal) override {
    auto p = static_cast<Range *>(other);
    *equal = first() == (anchor == TF_ANCHOR_START ? p->first() : p->last());
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE IsEqualEnd(TfEditCookie, ITfRange *other,
                                       TfAnchor anchor, BOOL *equal) override {
    auto p = static_cast<Range *>(other);
    *equal = last() == (anchor == TF_ANCHOR_START ? p->first() : p->last());
    return S_OK;
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
  HRESULT STDMETHODCALLTYPE Clone(ITfRange **out) override {
    auto range = new Range;
    range->root = root ? root : this;
    range->start = first();
    range->end = root ? end : LONG(text.size());
    *out = range;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetContext(ITfContext **out) override {
    *out = root ? root->context : context;
    if (!*out)
      return E_NOTIMPL;
    (*out)->AddRef();
    return S_OK;
  }
};
struct Property final : ITfProperty {
  UNKNOWN
  struct Mark {
    LONG start, end, atom;
  };
  std::array<Mark, 16> marks{};
  unsigned sets = 0, clears = 0;
  bool active = false;
  HRESULT STDMETHODCALLTYPE GetType(GUID *out) override {
    *out = GUID_PROP_ATTRIBUTE;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE EnumRanges(TfEditCookie, IEnumTfRanges **,
                                       ITfRange *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetValue(TfEditCookie, ITfRange *,
                                     VARIANT *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE GetContext(ITfContext **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE FindRange(TfEditCookie, ITfRange *, ITfRange **,
                                      TfAnchor) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE SetValueStore(TfEditCookie, ITfRange *,
                                          ITfPropertyStore *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE SetValue(TfEditCookie, ITfRange *span,
                                     const VARIANT *value) override {
    CHECK(value->vt == VT_I4 && sets < marks.size());
    auto range = static_cast<Range *>(span);
    marks[sets++] = {range->first(), range->last(), value->lVal};
    active = true;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE Clear(TfEditCookie, ITfRange *) override {
    ++clears;
    active = false;
    return S_OK;
  }
};
struct Composition final : ITfComposition {
  UNKNOWN
  Range *range = nullptr;
  HRESULT STDMETHODCALLTYPE GetRange(ITfRange **out) override {
    *out = range;
    range->AddRef();
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE ShiftStart(TfEditCookie, ITfRange *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE ShiftEnd(TfEditCookie, ITfRange *) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE EndComposition(TfEditCookie) override {
    range->selected = false;
    return S_OK;
  }
};
struct Context final : ITfContext, ITfContextComposition {
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid, void **out) override {
    *out = nullptr;
    if (iid != IID_ITfContextComposition)
      return E_NOINTERFACE;
    *out = static_cast<ITfContextComposition *>(this);
    AddRef();
    return S_OK;
  }
  ULONG STDMETHODCALLTYPE AddRef() override { return 2; }
  ULONG STDMETHODCALLTYPE Release() override { return 1; }
  Range range;
  Property property;
  Context() { range.context = static_cast<ITfContext *>(this); }
  Composition composition;
  std::function<void()> on_status, on_selection;
  HRESULT STDMETHODCALLTYPE StartComposition(TfEditCookie, ITfRange *,
                                             ITfCompositionSink *,
                                             ITfComposition **out) override {
    if (!range.selected)
      range.selected_start = LONG(range.text.size());
    range.selected = true;
    composition.range = &range;
    *out = &composition;
    composition.AddRef();
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE
  EnumCompositions(IEnumITfCompositionView **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE FindComposition(
      TfEditCookie, ITfRange *, IEnumITfCompositionView **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE TakeOwnership(TfEditCookie, ITfCompositionView *,
                                          ITfCompositionSink *,
                                          ITfComposition **) override {
    return E_NOTIMPL;
  }
  ITfEditSession *waiting = nullptr;
  BOOL own = FALSE;
  bool read_only = false;
  bool immediate = false, refuse = false;
  HRESULT STDMETHODCALLTYPE RequestEditSession(TfClientId, ITfEditSession *e,
                                               DWORD,
                                               HRESULT *result) override {
    CHECK(!waiting);
    if (refuse) {
      *result = TS_E_READONLY;
      return S_OK;
    }
    if (immediate) {
      own = TRUE;
      *result = e->DoEditSession(1);
      own = FALSE;
      return S_OK;
    }
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
    if (on_selection)
      on_selection();
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
    if (on_status)
      on_status();
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
  HRESULT STDMETHODCALLTYPE GetProperty(REFGUID id,
                                        ITfProperty **out) override {
    if (id != GUID_PROP_ATTRIBUTE)
      return E_NOTIMPL;
    *out = &property;
    property.AddRef();
    return S_OK;
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
struct Updates final : IEnumTfRanges {
  UNKNOWN
  Range range;
  bool text = false;
  HRESULT STDMETHODCALLTYPE Next(ULONG, ITfRange **out, ULONG *n) override {
    *n = text ? 1 : 0;
    if (text) {
      *out = &range;
      range.AddRef();
    }
    return text ? S_OK : S_FALSE;
  }
  HRESULT STDMETHODCALLTYPE Clone(IEnumTfRanges **) override {
    return E_NOTIMPL;
  }
  HRESULT STDMETHODCALLTYPE Skip(ULONG) override { return E_NOTIMPL; }
  HRESULT STDMETHODCALLTYPE Reset() override { return S_OK; }
};
struct Changed final : ITfEditRecord {
  UNKNOWN
  Updates updates;
  HRESULT STDMETHODCALLTYPE GetSelectionStatus(BOOL *changed) override {
    *changed = TRUE;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE GetTextAndPropertyUpdates(
      DWORD, const GUID **, ULONG, IEnumTfRanges **out) override {
    *out = &updates;
    updates.AddRef();
    return S_OK;
  }
};
namespace shixin {
HMODULE module = nullptr;
std::atomic<long> objects{0};
struct ServiceTest {
  static void spacing_prepare(Service &s, Context &c) {
    s.active_ = true;
    s.context_ = &c;
    c.AddRef();
    s.epoch_ = 1;
    s.core_.ctx = 1;
    s.core_.secure = false;
    s.core_.connected = true;
    s.spacer_.on = true;
    c.immediate = true;
    c.range.text.clear();
  }
  static void commit(Service &s, std::string_view text) {
    Result result;
    result.commit = text;
    CHECK(SUCCEEDED(s.apply_text(1, result, 0)));
  }
  static void spacing() {
    // Even with spacing off, a mode hint survives Chromium's redundant
    // selection-only notification after an ordinary passed text edit.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      s->spacer_.on = false;
      s->ui_ = std::make_shared<Transport>(nullptr, L"", true);
      s->mode_until_ = GetTickCount64() + 900;
      CHECK(!s->passed_key('a', 0));
      c.range.text += L'a';
      Changed changed;
      changed.updates.text = true;
      s->OnEndEdit(&c, 1, &changed);
      changed.updates.text = false;
      s->OnEndEdit(&c, 1, &changed);
      CHECK(s->mode_until_ != 0);
    }
    // Native composition styling uses UTF-16 spans and is removed when the
    // marked text commits, including the target segment's underline.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      s->attributes_ = &c.property;
      c.property.AddRef();
      s->input_atom_ = 5;
      s->target_atom_ = 6;
      Result preedit;
      preedit.preedit = "你hao";
      preedit.cursor = 6;
      preedit.start = 3;
      preedit.end = 6;
      CHECK(SUCCEEDED(s->apply_text(1, preedit, 0)));
      CHECK(c.property.sets == 2 && c.property.active);
      CHECK(c.property.marks[0].atom == 5 && c.property.marks[1].atom == 6);
      CHECK(c.property.marks[1].start == 1 && c.property.marks[1].end == 4);
      commit(*s, "你好");
      CHECK(!c.property.active && c.property.clears == 1);
      CHECK(c.range.text == L"你好");
    }
    // A selected range stays a replacement throughout preedit updates and
    // commit. A later, independent composition may space normally again.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      c.range.text = L"A旧文";
      c.range.selected = true;
      c.range.selected_start = 1;
      for (auto text : {"ni", "ni hao"}) {
        Result preedit;
        preedit.preedit = text;
        preedit.cursor = uint32_t(preedit.preedit.size());
        CHECK(SUCCEEDED(s->apply_text(1, preedit, 0)));
      }
      commit(*s, "你好");
      CHECK(c.range.text == L"A你好");
      Result preedit;
      preedit.preedit = "hello";
      preedit.cursor = 5;
      CHECK(SUCCEEDED(s->apply_text(1, preedit, 0)));
      commit(*s, "hello");
      CHECK(c.range.text == L"A你好 hello");
    }
    // Focus can reenter from any COM call, including the surrounding-text
    // reader after the security probe. No optional write survives it.
    for (unsigned phase = 0; phase != 3; ++phase) {
      Context c, other;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      commit(*s, "中文");
      if (phase == 0)
        c.on_status = [&] { s->focus(&other); };
      unsigned selections = 0;
      if (phase == 1)
        c.on_selection = [&] {
          if (++selections == 2)
            s->focus(&other);
        };
      if (phase == 2)
        c.range.on_read = [&] { s->focus(&other); };
      CHECK(!s->passed_key('a', 0));
      CHECK(c.range.text == L"中文" && c.range.writes == 1 &&
            other.range.writes == 0);
      CHECK(s->context_ == &other);
    }
    // A failing selection query never licenses a space from cached history.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      commit(*s, "中文");
      c.range.empty_failure = true;
      CHECK(!s->passed_key('a', 0));
      c.range.text += L'a';
      commit(*s, "好");
      CHECK(c.range.text == L"中文a好");
    }
    // Spacing's known passed edits cannot exempt a key that passed after
    // the engine ordering barrier timed out: old replies must be invalidated.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      s->core_.sent = 1;
      s->core_.pending[1] = {1, 'n', false, true};
      auto epoch = s->epoch_;
      CHECK(!s->passed_key('x', 0));
      c.range.text += L'x';
      Changed changed;
      changed.updates.text = true;
      s->OnEndEdit(&c, 1, &changed);
      CHECK(s->epoch_ != epoch && s->spacer_.count == 0);
    }
    // Include the OS keyboard-layout check in the Windows key-path budget.
    // No text-store calls, waits or heap allocations occur in this helper.
    {
      std::array<int64_t, 10000> samples{};
      volatile bool matched = false;
      for (auto &sample : samples) {
        auto start = std::chrono::steady_clock::now();
        matched = Service::matches_character('a', 'A', 0x001e0001);
        sample = std::chrono::duration_cast<std::chrono::nanoseconds>(
                     std::chrono::steady_clock::now() - start)
                     .count();
      }
      (void)matched;
      std::sort(samples.begin(), samples.end());
      std::cout << "keyboard layout check: p50 " << samples[5000] << " ns, p99 "
                << samples[9900] << " ns\n";
    }
    // Passed Backspace notifications preserve removal of an inserted space;
    // an actual selection-only edit discards that history.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      commit(*s, "中文");
      CHECK(s->passed_key('a', 0));
      Changed r;
      r.updates.text = true;
      for (unsigned i = 0; i != 2; ++i) {
        CHECK(!s->passed_key(0xff08, 0));
        c.range.text.pop_back();
        s->OnEndEdit(&c, 1, &r);
        r.updates.text = false;
        s->OnEndEdit(&c, 1, &r);
        r.updates.text = true;
      }
      CHECK(!s->passed_key('b', 0));
      c.range.text += L'b';
      CHECK(c.range.text == L"中文b");
      commit(*s, "好");
      CHECK(c.range.text == L"中文b 好");
      c.range.readable = false;
      c.range.selected = true;
      r.updates.text = false;
      s->OnEndEdit(&c, 1, &r);
      CHECK(!s->passed_key('a', 0));
      CHECK(!s->spacer_.count || s->spacer_.left() != SpaceClass::CJK);
    }
    // Actual TSF ranges, one write per boundary, readable and fallback apps.
    for (bool readable : {false, true}) {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      c.range.readable = readable;
      commit(*s, "中文");
      CHECK(s->passed_key('a', 0));
      CHECK(c.range.text == L"中文 a" && c.range.writes == 2);
      CHECK(!s->passed_key('b', 0));
      c.range.text += L'b';
      commit(*s, "很好");
      CHECK(c.range.text == L"中文 ab 很好");
    }
    // Reading recovers an unknown boundary, including supplementary Han.
    for (auto text : {L"iPhone", L"\xd840\xdc00"}) {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      c.range.text = text;
      if (c.range.text == L"iPhone") {
        commit(*s, "拍照");
        CHECK(c.range.text == L"iPhone 拍照");
      } else {
        CHECK(s->passed_key('3', 0));
        CHECK(c.range.text == L"\xd840\xdc00 3");
      }
    }
    // Replacing a selection doesn't prefix the replacement with a space.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      commit(*s, "中文");
      c.range.selected = true;
      commit(*s, "hello");
      CHECK(c.range.text == L"hello");
      c.range.selected = true;
      CHECK(!s->passed_key('a', 0));
      CHECK(c.range.writes == 2);
    }
    // A deferred optional key edit is cancelled before the original passes.
    // Changing the selection, focus or security cannot revive that edit.
    for (unsigned change = 0; change != 4; ++change) {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      commit(*s, "中文");
      c.immediate = false;
      CHECK(!s->passed_key('a', 0) && c.waiting);
      c.range.text += L'a';
      if (change == 1) {
        c.range.selected = true;
        Changed r;
        s->OnEndEdit(&c, 1, &r);
      }
      if (change == 2)
        s->focus(nullptr);
      if (change == 3)
        c.read_only = true;
      c.complete();
      CHECK(c.range.text == L"中文a" && c.range.writes == 1);
    }
    // Refusal also passes the key, and leaves no queued write.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      commit(*s, "中文");
      c.refuse = true;
      CHECK(!s->passed_key('a', 0));
      CHECK(c.range.text == L"中文" && !c.waiting && s->edit_.idle());
    }
    // The local guard prevents reading or inserting in a newly secure field.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      commit(*s, "中文");
      c.read_only = true;
      CHECK(!s->passed_key('a', 0));
      CHECK(c.range.writes == 1 && !s->spacer_.on && s->core_.secure);
    }
    // ResultAutoSpace is the existing wire bit; applying results turns it
    // on/off live, before commits. Repair text uses the same spacing path.
    {
      Context c;
      auto s = std::make_unique<Service>();
      spacing_prepare(*s, c);
      s->core_.installed[1] = true;
      for (uint32_t seq = 1; seq != 4; ++seq) {
        s->core_.sent = seq;
        s->core_.pending[seq] = {seq, 'a', false, true};
        Frame f;
        f.begin(ResultType);
        f.u32(1);
        f.u32(seq);
        f.u16(seq == 2 ? 0 : kHandled | kAutoSpace);
        f.u16(1);
        f.string(seq == 1 ? "中文" : seq == 3 ? "好" : "");
        f.string("");
        f.u32(0);
        f.u32(0);
        f.u32(0);
        f.u16(0);
        f.u16(0);
        f.u16(0);
        f.string("");
        f.end();
        s->receive_result(f.view());
        s->schedule();
      }
      CHECK(c.range.text == L"中文a 好");
      CHECK(s->core_.applied == 3 && s->spacer_.on);
    }
  }
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
    // Queued engine results may have re-enabled spacing before recovery;
    // an outage's literal tail still preserves the original keys exactly.
    {
      Context c;
      auto s = std::make_unique<Service>();
      prepare(*s, c);
      c.range.text = L"中文";
      s->spacer_.on = true;
      s->spacer_.inserted("中文", false);
      c.complete();
      CHECK(c.range.text == L"中文n" && !s->spacer_.on);
    }
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
  ServiceTest::spacing();
  ServiceTest::run();
  CHECK(objects == 0);
  std::cout << "deferred recovery and edit overflow passed\n";
}
