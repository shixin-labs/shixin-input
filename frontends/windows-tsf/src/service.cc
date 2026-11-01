// SPDX-License-Identifier: GPL-3.0-or-later
#include "service.h"
#include "keys.h"
#include "recovery.h"
#include "scope.h"
#include <algorithm>
#include <appmodel.h>
#include <cstring>
#include <new>
#include <shobjidl.h>
namespace shixin {
namespace {
template <class T> void release(T *&p) {
  if (p) {
    p->Release();
    p = nullptr;
  }
}
// Focus changes may finalize the previous context asynchronously. This
// object is allocated only on focus loss, never for a key or its result.
class Finish final : public ITfEditSession {
  ULONG refs_ = 1;
  ITfComposition *composition_;

public:
  explicit Finish(ITfComposition *c) : composition_(c) { ++objects; }
  ~Finish() {
    composition_->Release();
    --objects;
  }
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID iid, void **p) override {
    if (!p)
      return E_POINTER;
    *p = nullptr;
    if (iid != IID_IUnknown && iid != IID_ITfEditSession)
      return E_NOINTERFACE;
    *p = static_cast<ITfEditSession *>(this);
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
  HRESULT STDMETHODCALLTYPE DoEditSession(TfEditCookie ec) override {
    return end_composition(ec, composition_);
  }
};
void finish(ITfContext *context, ITfComposition *&composition,
            TfClientId client) {
  if (!composition)
    return;
  auto edit = new (std::nothrow) Finish(composition);
  if (!edit) {
    release(composition);
    return;
  }
  composition = nullptr;
  HRESULT result;
  context->RequestEditSession(client, edit,
                              TF_ES_ASYNCDONTCARE | TF_ES_READWRITE, &result);
  edit->Release();
}
} // namespace
void Edit::prepare(Service *s, ITfContext *c, uint32_t e, bool p) {
  owner = s;
  context = c;
  epoch = e;
  probe = p;
  recovery = false;
  spacing_key = spacing_mods = 0;
  spacing_done = spacing_inserted = cancelled = false;
  refs_ = 1;
  owner->AddRef();
  context->AddRef();
}
HRESULT Edit::QueryInterface(REFIID iid, void **p) {
  if (!p)
    return E_POINTER;
  *p = nullptr;
  if (iid != IID_IUnknown && iid != IID_ITfEditSession)
    return E_NOINTERFACE;
  *p = static_cast<ITfEditSession *>(this);
  AddRef();
  return S_OK;
}
ULONG Edit::Release() {
  auto n = --refs_;
  if (!n) {
    refs_ = 1;
    auto s = owner;
    auto c = context;
    context = nullptr;
    auto old = ending;
    ending = nullptr;
    if (old)
      old->Release();
    if (c)
      c->Release();
    refs_ = 0;
    s->edit_released();
    s->Release();
  }
  return n;
}
HRESULT Edit::DoEditSession(TfEditCookie ec) {
  return owner->execute(*this, ec);
}
#ifndef CREATE_WAITABLE_TIMER_HIGH_RESOLUTION
#define CREATE_WAITABLE_TIMER_HIGH_RESOLUTION 0x00000002
#endif
Service::Service() {
  ++objects;
  QueryPerformanceFrequency(&frequency_);
  timer_ = CreateWaitableTimerExW(nullptr, nullptr,
                                  CREATE_WAITABLE_TIMER_HIGH_RESOLUTION,
                                  TIMER_ALL_ACCESS);
  edit_.owner = this;
}
Service::~Service() {
  Deactivate();
  release(passed_selection_);
  if (timer_)
    CloseHandle(timer_);
  --objects;
}
HRESULT Service::QueryInterface(REFIID iid, void **p) {
  if (!p)
    return E_POINTER;
  *p = nullptr;
  if (iid == IID_IUnknown || iid == IID_ITfTextInputProcessor ||
      iid == kProcessorEx)
    *p = static_cast<ProcessorEx *>(this);
  else if (iid == IID_ITfKeyEventSink)
    *p = static_cast<ITfKeyEventSink *>(this);
  else if (iid == IID_ITfThreadMgrEventSink)
    *p = static_cast<ITfThreadMgrEventSink *>(this);
  else if (iid == IID_ITfCompositionSink)
    *p = static_cast<ITfCompositionSink *>(this);
  else if (iid == IID_ITfTextEditSink)
    *p = static_cast<ITfTextEditSink *>(this);
  else if (iid == IID_ITfDisplayAttributeProvider)
    *p = static_cast<ITfDisplayAttributeProvider *>(this);
  else
    return E_NOINTERFACE;
  AddRef();
  return S_OK;
}
LRESULT CALLBACK Service::window_proc(HWND w, UINT m, WPARAM wp, LPARAM lp) {
  auto self = reinterpret_cast<Service *>(GetWindowLongPtrW(w, GWLP_USERDATA));
  if (m == WM_NCCREATE) {
    self = static_cast<Service *>(
        reinterpret_cast<CREATESTRUCTW *>(lp)->lpCreateParams);
    SetWindowLongPtrW(w, GWLP_USERDATA, reinterpret_cast<LONG_PTR>(self));
  }
  if (m == kWake && self) {
    self->pump();
    return 0;
  }
  return DefWindowProcW(w, m, wp, lp);
}
HRESULT Service::ActivateEx(ITfThreadMgr *m, TfClientId id, DWORD flags) {
  if (active_)
    return E_UNEXPECTED;
  if (!m || flags & TF_TMAE_SECUREMODE)
    return E_ACCESSDENIED;
  // Warm the application identity once. The shared settings picker uses
  // AppUserModelIDs for packaged/explicitly identified Windows apps.
  std::array<wchar_t, 32768> app{};
  UINT32 count = UINT32(app.size());
  if (GetCurrentApplicationUserModelId(&count, app.data()) != ERROR_SUCCESS) {
    PWSTR explicit_id = nullptr;
    if (SUCCEEDED(GetCurrentProcessExplicitAppUserModelID(&explicit_id)) &&
        explicit_id) {
      wcsncpy_s(app.data(), app.size(), explicit_id, _TRUNCATE);
      CoTaskMemFree(explicit_id);
    } else
      GetModuleFileNameW(nullptr, app.data(), DWORD(app.size()));
  }
  int app_bytes = WideCharToMultiByte(
      CP_UTF8, 0, app.data(), int(wcsnlen(app.data(), app.size())), app_.data(),
      int(app_.size()), nullptr, nullptr);
  app_size_ = size_t(std::max(0, app_bytes));
  manager_ = m;
  manager_->AddRef();
  client_ = id;
  active_ = true;
  // Register display atoms once, before input can arrive. Styling remains
  // optional when an application's text store does not support properties.
  ITfCategoryMgr *category = nullptr;
  if (SUCCEEDED(CoCreateInstance(CLSID_TF_CategoryMgr, nullptr,
                                 CLSCTX_INPROC_SERVER, IID_ITfCategoryMgr,
                                 reinterpret_cast<void **>(&category)))) {
    category->RegisterGUID(kDisplayInput, &input_atom_);
    category->RegisterGUID(kDisplayTarget, &target_atom_);
    category->Release();
  }
  WNDCLASSW cls{};
  cls.lpfnWndProc = window_proc;
  cls.hInstance = module;
  cls.lpszClassName = L"Shixin.TSF.Dispatch.v1";
  if (!RegisterClassW(&cls) && GetLastError() != ERROR_CLASS_ALREADY_EXISTS) {
    Deactivate();
    return E_FAIL;
  }
  window_ = CreateWindowExW(0, cls.lpszClassName, L"", 0, 0, 0, 0, 0,
                            HWND_MESSAGE, nullptr, module, this);
  if (!window_) {
    Deactivate();
    return E_FAIL;
  }
  try {
    transport_ = std::make_shared<Transport>(window_, module_directory(module));
    ui_ = std::make_shared<Transport>(window_, module_directory(module), true);
  } catch (...) {
    Deactivate();
    return E_OUTOFMEMORY;
  }
  if (!transport_->start() || !ui_->start()) {
    Deactivate();
    return E_FAIL;
  }
  ITfSource *source = nullptr;
  ITfKeystrokeMgr *keys = nullptr;
  HRESULT hr = manager_->QueryInterface(IID_ITfSource,
                                        reinterpret_cast<void **>(&source));
  if (SUCCEEDED(hr)) {
    hr = source->AdviseSink(IID_ITfThreadMgrEventSink,
                            static_cast<ITfThreadMgrEventSink *>(this),
                            &manager_cookie_);
    source->Release();
  }
  if (SUCCEEDED(hr))
    hr = manager_->QueryInterface(IID_ITfKeystrokeMgr,
                                  reinterpret_cast<void **>(&keys));
  if (SUCCEEDED(hr)) {
    hr = keys->AdviseKeyEventSink(client_, static_cast<ITfKeyEventSink *>(this),
                                  TRUE);
    keys->Release();
  }
  if (FAILED(hr)) {
    Deactivate();
    return hr;
  }
  transport_->trace("activate", flags, hr, client_);
  ITfDocumentMgr *doc = nullptr;
  if (SUCCEEDED(manager_->GetFocus(&doc)) && doc) {
    OnSetFocus(doc, nullptr);
    doc->Release();
  }
  return S_OK;
}
HRESULT Service::Deactivate() {
  if (!active_)
    return S_OK;
  active_ = false;
  core_.reset();
  focus(nullptr);
  if (manager_) {
    ITfKeystrokeMgr *keys = nullptr;
    if (SUCCEEDED(manager_->QueryInterface(IID_ITfKeystrokeMgr,
                                           reinterpret_cast<void **>(&keys)))) {
      keys->UnadviseKeyEventSink(client_);
      keys->Release();
    }
    ITfSource *source = nullptr;
    if (manager_cookie_ != TF_INVALID_COOKIE &&
        SUCCEEDED(manager_->QueryInterface(
            IID_ITfSource, reinterpret_cast<void **>(&source)))) {
      source->UnadviseSink(manager_cookie_);
      source->Release();
    }
  }
  manager_cookie_ = TF_INVALID_COOKIE;
  if (transport_) {
    transport_->stop();
    transport_.reset();
  }
  if (ui_) {
    ui_->stop();
    ui_.reset();
  }
  if (window_) {
    DestroyWindow(window_);
    window_ = nullptr;
    UnregisterClassW(L"Shixin.TSF.Dispatch.v1", module);
  }
  release(manager_);
  return S_OK;
}
void Service::focus(ITfContext *c) {
  if (transport_)
    transport_->trace("focus", uintptr_t(c), uintptr_t(context_), inside_);
  if (inside_) {
    release(next_context_);
    next_context_ = c;
    if (c)
      c->AddRef();
    focus_pending_ = true;
    return;
  }
  if (c == context_)
    return;
  spacer_.focus();
  passed_edit_ = false;
  release(passed_selection_);
  release(attributes_);
  mode_until_ = 0;
  if (recovering_) {
    recovering_ = false;
    fallback_size_ = 0;
    fallback_.fill(0);
    core_.reset();
  }
  if (context_) {
    if (transport_ && core_.connected) {
      focus_out(scratch_, core_.ctx);
      transport_->send(scratch_);
    }
    ITfSource *source = nullptr;
    if (text_cookie_ != TF_INVALID_COOKIE &&
        SUCCEEDED(context_->QueryInterface(
            IID_ITfSource, reinterpret_cast<void **>(&source)))) {
      source->UnadviseSink(text_cookie_);
      source->Release();
    }
    finish(context_, composition_, client_);
  }
  text_cookie_ = TF_INVALID_COOKIE;
  release(disabled_);
  release(context_);
  context_ = c;
  if (c)
    c->AddRef();
  ++epoch_;
  if (!epoch_)
    ++epoch_;
  core_.focus(context_id(epoch_), true);
  ui_focus(true);
  probe_needed_ = c != nullptr;
  edit_clear();
  if (c) {
    ITfSource *source = nullptr;
    if (SUCCEEDED(c->QueryInterface(IID_ITfSource,
                                    reinterpret_cast<void **>(&source)))) {
      source->AdviseSink(IID_ITfTextEditSink,
                         static_cast<ITfTextEditSink *>(this), &text_cookie_);
      source->Release();
    }
    ITfCompartmentMgr *cm = nullptr;
    if (SUCCEEDED(c->QueryInterface(IID_ITfCompartmentMgr,
                                    reinterpret_cast<void **>(&cm)))) {
      cm->GetCompartment(GUID_COMPARTMENT_KEYBOARD_DISABLED, &disabled_);
      cm->Release();
    }
    schedule();
  }
}
bool Service::flush_focus() {
  if (!focus_pending_)
    return false;
  focus_pending_ = false;
  auto next = next_context_;
  next_context_ = nullptr;
  focus(next);
  release(next);
  return true;
}
HRESULT Service::OnSetFocus(ITfDocumentMgr *doc, ITfDocumentMgr *) {
  ITfContext *c = nullptr;
  if (doc)
    doc->GetTop(&c);
  focus(c);
  release(c);
  return S_OK;
}
HRESULT Service::OnPopContext(ITfContext *) {
  ITfDocumentMgr *doc = nullptr;
  if (manager_)
    manager_->GetFocus(&doc);
  OnSetFocus(doc, nullptr);
  release(doc);
  return S_OK;
}
HRESULT Service::OnSetFocus(BOOL foreground) {
  if (!foreground)
    focus(nullptr);
  else if (manager_) {
    ITfDocumentMgr *doc = nullptr;
    manager_->GetFocus(&doc);
    OnSetFocus(doc, nullptr);
    release(doc);
  }
  return S_OK;
}
HRESULT Service::OnCompositionTerminated(TfEditCookie ec, ITfComposition *c) {
  if (transport_)
    transport_->trace("terminated", uintptr_t(c), uintptr_t(composition_));
  if (c == composition_) {
    clear_composition_attributes(ec, c);
    release(composition_);
    secure_changed(core_.secure);
  }
  return S_OK;
}
HRESULT Service::OnEndEdit(ITfContext *c, TfEditCookie ec,
                           ITfEditRecord *record) {
  if (c != context_ || inside_)
    return S_OK;
  inside_ = true;
  bool secure = password(ec, c);
  inside_ = false;
  if (flush_focus())
    return S_OK;
  if (secure != core_.secure)
    secure_changed(secure);
  else if (spacer_.on || recovering_ || core_.sent != core_.applied ||
           (ui_ && GetTickCount64() < mode_until_)) {
    // Own write sessions also notify this sink after execute() returns.
    // Ask TSF who owns the lock instead of retaining a callback marker.
    BOOL own = FALSE;
    if (FAILED(c->InWriteSession(client_, &own)) || !own) {
      BOOL selection = TRUE;
      IEnumTfRanges *updates = nullptr;
      ITfRange *range = nullptr;
      ULONG count = 0;
      bool unknown = !record || FAILED(record->GetSelectionStatus(&selection));
      bool changed = unknown || selection;
      unknown |= !record ||
                 FAILED(record->GetTextAndPropertyUpdates(
                     TF_GTP_INCL_TEXT, nullptr, 0, &updates)) ||
                 !updates;
      if (updates)
        unknown |= FAILED(updates->Next(1, &range, &count));
      changed |= unknown || count != 0;
      release(range);
      release(updates);
      // A passed printable/Backspace has a known effect. Keep its history
      // through its own text-edit notification; selection-only changes,
      // unknown edits and application commands always reset it. Verification
      // at the next boundary catches autocorrection in that same app edit.
      bool observed = passed_edit_ && !unknown && count != 0;
      if (observed && (spacer_.on || (ui_ && GetTickCount64() < mode_until_))) {
        // Chromium sends a text edit followed by a separate selection
        // notification for the SAME caret after Backspace. Remember its
        // position so that only that redundant notification is ignored.
        release(passed_selection_);
        TF_SELECTION selected{};
        ULONG n = 0;
        if (SUCCEEDED(
                c->GetSelection(ec, TF_DEFAULT_SELECTION, 1, &selected, &n)) &&
            n && selected.range) {
          BOOL empty = FALSE;
          if (SUCCEEDED(selected.range->IsEmpty(ec, &empty)) && empty)
            selected.range->Clone(&passed_selection_);
          selected.range->Release();
        }
      } else if (!unknown && !count && selection && passed_selection_) {
        observed = same_passed_selection(ec);
      }
      if (transport_)
        transport_->trace("spacing-edit",
                          (observed ? 1 : 0) | (unknown ? 2 : 0) |
                              (selection ? 4 : 0) | (passed_edit_ ? 8 : 0),
                          count, spacer_.count);
      passed_edit_ = false;
      if (changed && !observed)
        spacer_.clear();
      if (changed && !observed)
        release(passed_selection_);
      if (changed && !observed && ui_) {
        mode_until_ = 0;
        shixin::caret(scratch_, core_.ctx, 1, 0, 0, 0);
        ui_->send(scratch_);
      }
      if (changed && !observed &&
          (recovering_ || core_.sent != core_.applied)) {
        // The application moved/replaced text while our edit was delayed.
        // Invalidate queued edits and late replies before they can touch its
        // new selection. Already-marked text may only be finalized.
        secure_changed(secure);
        finalize_needed_ = composition_ != nullptr;
      }
    }
  }
  return S_OK;
}
bool Service::disabled() const {
  if (!disabled_)
    return false;
  VARIANT v;
  VariantInit(&v);
  bool off = FAILED(disabled_->GetValue(&v)) ||
             (v.vt != VT_EMPTY && (v.vt != VT_I4 || v.lVal != 0));
  VariantClear(&v);
  return off;
}
bool Service::password(TfEditCookie ec, ITfContext *c) {
  if (disabled())
    return true;
  ITfContextView *view = nullptr;
  if (SUCCEEDED(c->GetActiveView(&view))) {
    HWND hwnd = nullptr;
    if (SUCCEEDED(view->GetWnd(&hwnd)) && hwnd) {
      wchar_t name[64]{};
      GetClassNameW(hwnd, name, int(std::size(name)));
      bool edit =
          _wcsicmp(name, L"Edit") == 0 || _wcsnicmp(name, L"RichEdit", 8) == 0;
      if (edit && (GetWindowLongPtrW(hwnd, GWL_STYLE) & ES_PASSWORD)) {
        view->Release();
        return true;
      }
    }
    view->Release();
  }
  TF_STATUS status{};
  if (FAILED(c->GetStatus(&status)) || (status.dwStaticFlags & TF_SD_READONLY))
    return true;
  TF_SELECTION sel{};
  ULONG count = 0;
  if (FAILED(c->GetSelection(ec, TF_DEFAULT_SELECTION, 1, &sel, &count)) ||
      !count || !sel.range)
    return true;
  ITfReadOnlyProperty *property = nullptr;
  HRESULT hr = c->GetAppProperty(kInputScopeProperty, &property);
  if (transport_)
    transport_->trace("app-property", uint32_t(hr), uintptr_t(property));
  VARIANT v;
  VariantInit(&v);
  if (SUCCEEDED(hr) && property) {
    hr = property->GetValue(ec, sel.range, &v);
    property->Release();
  }
  sel.range->Release();
  if (transport_)
    transport_->trace("scope", uint32_t(hr), v.vt);
  bool secure = !absent_input_scope(uint32_t(hr), v.vt) &&
                (FAILED(hr) || v.vt != VT_UNKNOWN || !v.punkVal);
  if (SUCCEEDED(hr) && v.vt == VT_UNKNOWN && v.punkVal) {
    ITfInputScope *scope = nullptr;
    if (FAILED(v.punkVal->QueryInterface(kInputScope,
                                         reinterpret_cast<void **>(&scope))))
      secure = true;
    else {
      InputScope *values = nullptr;
      UINT n = 0;
      hr = scope->GetInputScopes(&values, &n);
      secure = FAILED(hr) || n > 256 || (n && !values);
      if (SUCCEEDED(hr) && n <= 256 && (!n || values))
        for (UINT i = 0; i < n; ++i)
          if (values[i] == IS_PASSWORD || values[i] == IS_PRIVATE ||
              values[i] == IS_NUMERIC_PASSWORD || values[i] == IS_NUMERIC_PIN ||
              values[i] == IS_ALPHANUMERIC_PIN ||
              values[i] == IS_ALPHANUMERIC_PIN_SET)
            secure = true;
      CoTaskMemFree(values);
      scope->Release();
    }
  }
  VariantClear(&v);
  return secure;
}
void Service::secure_changed(bool secure) {
  spacer_.focus();
  passed_edit_ = false;
  release(passed_selection_);
  mode_until_ = 0;
  if (recovering_) {
    recovering_ = false;
    fallback_size_ = 0;
    fallback_.fill(0);
    core_.reset();
  }
  if (transport_ && core_.connected) {
    focus_out(scratch_, core_.ctx);
    transport_->send(scratch_);
  }
  ++epoch_;
  if (!epoch_)
    ++epoch_;
  core_.focus(context_id(epoch_), secure);
  ui_focus(secure);
  edit_clear();
  if (secure && context_)
    finish(context_, composition_, client_);
  send_focus();
}
void Service::ui_focus(bool hide) {
  if (!ui_)
    return;
  focus_in(scratch_, core_.ctx,
           hide || core_.secure || core_.migrating || !core_.connected ||
               !context_,
           {});
  ui_->send(scratch_);
}
void Service::choose(uint32_t index) {
  if (core_.secure || !core_.connected || core_.migrating || !core_.known ||
      index >= 128)
    return;
  if (core_.sent != core_.applied)
    return;
  if (core_.sent - core_.applied >= core_.pending.size() - 1 ||
      core_.sent == UINT32_MAX) {
    degraded();
    return;
  }
  auto seq = core_.sent + 1;
  select(scratch_, core_.ctx, seq, index);
  if (!transport_->send(scratch_)) {
    degraded();
    return;
  }
  core_.sent = seq;
  core_.pending[seq % core_.pending.size()] = {seq, 0, false, false};
  core_.known = false;
}
void Service::caret(TfEditCookie ec) {
  if (!ui_ || !context_)
    return;
  ITfRange *range = nullptr;
  ITfContextView *view = nullptr;
  RECT rect{};
  BOOL clipped = FALSE;
  TF_SELECTION selected{};
  ULONG count = 0;
  if (SUCCEEDED(context_->GetSelection(ec, TF_DEFAULT_SELECTION, 1, &selected,
                                       &count)) &&
      count && selected.range) {
    range = selected.range;
    range->Collapse(ec, TF_ANCHOR_END);
    if (SUCCEEDED(context_->GetActiveView(&view))) {
      if (SUCCEEDED(view->GetTextExt(ec, range, &rect, &clipped)) && !clipped) {
        HWND hwnd = nullptr;
        if (SUCCEEDED(view->GetWnd(&hwnd)) && hwnd) {
          POINT a{rect.left, rect.top}, b{rect.right, rect.bottom};
          if (LogicalToPhysicalPointForPerMonitorDPI(hwnd, &a) &&
              LogicalToPhysicalPointForPerMonitorDPI(hwnd, &b))
            rect = {a.x, a.y, b.x, b.y};
        }
        shixin::caret(scratch_, core_.ctx, rect.left, rect.top, rect.right,
                      rect.bottom);
        ui_->send(scratch_);
      }
      view->Release();
    }
    range->Release();
  }
}
void Service::send_focus() {
  if (!context_ || !transport_ || !core_.connected)
    return;
  focus_in(scratch_, core_.ctx, core_.secure, {app_.data(), app_size_});
  if (!transport_->send(scratch_))
    degraded();
  ui_focus();
}
bool Service::send(uint32_t ctx, uint32_t seq, uint32_t sym, uint32_t mods,
                   uint8_t flags) {
  key(scratch_, ctx, seq, sym, mods, flags);
  return transport_ && transport_->send(scratch_);
}
void Service::degraded() {
  spacer_.focus();
  passed_edit_ = false;
  release(passed_selection_);
  mode_until_ = 0;
  if (!recovering_) {
    fallback_size_ =
        core_.secure ? 0 : core_.fallback(fallback_.data(), fallback_.size());
    recovering_ = fallback_size_ || edit_read_ != edit_write_ || composition_;
  }
  if (recovering_) {
    // Retain already-received replies until their edit sessions finish.
    // New keys pass, and the transport's later replies are discarded.
    core_.migrating = true;
    if (edit_read_ != edit_write_)
      core_.connected = true;
  } else {
    core_.reset();
  }
  if (window_)
    PostMessageW(window_, kWake, 0, 0);
  ui_focus(true);
  core_.known = false;
  if (transport_)
    transport_->disconnect();
}
uint64_t Service::now() {
  LARGE_INTEGER t;
  QueryPerformanceCounter(&t);
  return uint64_t(t.QuadPart / frequency_.QuadPart) * 1000000000ull +
         uint64_t(t.QuadPart % frequency_.QuadPart) * 1000000000ull /
             uint64_t(frequency_.QuadPart);
}
// wait_until waits for a result or the deadline, whichever comes first.
// A plain millisecond timeout rounds up to the system tick (~15.6 ms by
// default), twice the 8 ms budget, so the deadline is a high-resolution
// waitable timer when Windows has them (10 1803+).
void Service::wait_until(uint64_t deadline) {
  if (!transport_)
    return;
  uint64_t t = now();
  if (t >= deadline)
    return;
  uint64_t left = deadline - t;
  if (timer_) {
    LARGE_INTEGER due;
    due.QuadPart = -LONGLONG((left + 99) / 100); // relative, in 100 ns
    if (SetWaitableTimer(timer_, &due, 0, nullptr, nullptr, FALSE)) {
      HANDLE both[2] = {transport_->arrived(), timer_};
      WaitForMultipleObjects(2, both, FALSE, INFINITE);
      CancelWaitableTimer(timer_);
      return;
    }
  }
  WaitForSingleObject(transport_->arrived(), DWORD((left + 999999) / 1000000));
}
void Service::pump() {
  if (!transport_ || pumping_)
    return;
  pumping_ = true;
  if (ui_)
    for (auto frame = ui_->front(); !frame.empty(); frame = ui_->front()) {
      if (frame_type(frame) == Welcome)
        ui_focus(!core_.connected);
      else if (frame_type(frame) == Select) {
        Reader r(frame_payload(frame));
        auto ctx = r.u32();
        auto seq = r.u32();
        auto index = r.u32();
        if (r.good && ctx == core_.ctx && seq == core_.received)
          choose(index);
      }
      ui_->pop();
    }
  if (draining_ && edit_read_ == edit_write_ && edit_.idle()) {
    core_.reset();
    finalize_needed_ = true;
    draining_ = false;
  }
  while (!draining_) {
    auto frame = transport_->front();
    if (frame.empty())
      break;
    if (recovering_ && frame_type(frame) == Welcome)
      break; // Finish the local tail before adopting a new connection.
    switch (frame_type(frame)) {
    case Welcome: {
      Reader r(frame_payload(frame));
      auto version = r.u16();
      r.u32();
      if (!r.good || version != kVersion) {
        degraded();
        break;
      }
      core_.reset();
      spacer_.focus();
      core_.connected = true;
      core_.focus(context_id(epoch_), core_.secure);
      send_focus();
      break;
    }
    case PolicyType:
      if (!core_.install(frame_payload(frame)))
        degraded();
      break;
    case ResultType: {
      if (recovering_)
        break;
      receive_result(frame);
      break;
    }
    case Migrate:
      ui_focus(true);
      core_.migrating = true;
      transport_->migrate();
      break;
    case kDisconnected:
      ui_focus(true);
      if (recovering_) {
        break;
      } else if (core_.migrating && core_.received == core_.sent)
        draining_ = true;
      else
        degraded();
      break;
    case Unavailable:
      degraded();
      break;
    default:
      break;
    }
    transport_->pop();
  }
  schedule();
  pumping_ = false;
}
void Service::receive_result(std::string_view frame) {
  Result result;
  if (!result.decode(frame_payload(frame))) {
    degraded();
    return;
  }
  auto slot = edit_write_ - edit_read_ == kEditSlots
                  ? nullptr
                  : edit_frames_.reserve(frame.size());
  if (!slot) {
    degraded();
    return;
  }
  // Advance received only after retaining room for this reply's edit.
  auto complete = core_.result(result);
  if (!complete.accept)
    return;
  std::memcpy(slot, frame.data(), frame.size());
  edit_frames_.publish(frame.size());
  auto &edit = edit_meta_[edit_write_ % kEditSlots];
  edit.epoch = epoch_;
  edit.repair = complete.repair;
  edit.notify = complete.notify;
  ++edit_write_;
}
void Service::schedule() {
  if (!context_ || !active_ || !edit_.idle())
    return;
  if (finalize_needed_ && !composition_)
    finalize_needed_ = false;
  if ((finalize_needed_ || recovering_) && key_depth_)
    return;
  bool finishing = finalize_needed_ && !recovering_;
  const bool recovery = recovering_ && edit_read_ == edit_write_;
  if (!finishing && !recovery && !probe_needed_ && edit_read_ == edit_write_)
    return;
  bool probe = !finishing && !recovering_ && probe_needed_;
  if (probe)
    probe_needed_ = false;
  edit_.prepare(this, context_, epoch_, probe);
  edit_.recovery = recovery;
  if (finishing) {
    edit_.ending = composition_;
    composition_ = nullptr;
    finalize_needed_ = false;
  }
  HRESULT result = E_FAIL;
  HRESULT hr = context_->RequestEditSession(
      client_, &edit_,
      TF_ES_ASYNCDONTCARE | (probe ? TF_ES_READ : TF_ES_READWRITE), &result);
  if (transport_)
    transport_->trace("edit-request", uint32_t(hr), uint32_t(result), probe);
  if (FAILED(hr) || FAILED(result)) {
    edit_clear();
    probe_needed_ = false;
    core_.secure = true;
    if (recovering_) {
      // An app refusing the recovery edit must not create an unbounded
      // retry loop on its apartment. Secure/focus guards discard the tail.
      recovering_ = false;
      fallback_size_ = 0;
      fallback_.fill(0);
      core_.reset();
      transport_->disconnect();
    } else {
      degraded();
    }
  }
  edit_.Release();
}
void Service::edit_released() {
  if (window_)
    PostMessageW(window_, kWake, 0, 0);
}
HRESULT Service::execute(Edit &edit, TfEditCookie ec) {
  // This request only finalizes already-marked text. It never copies new
  // text into a context, even if focus changed while the session waited.
  if (edit.ending)
    return end_composition(ec, edit.ending);
  if (!active_ || edit.epoch != epoch_ || edit.context != context_)
    return S_OK;
  if (edit.spacing_key) {
    if (edit.cancelled)
      return S_OK;
    inside_ = true;
    bool secure = password(ec, context_);
    HRESULT hr = S_OK;
    if (!secure && !focus_pending_) {
      TF_SELECTION sel{};
      ULONG count = 0;
      if (SUCCEEDED(context_->GetSelection(ec, TF_DEFAULT_SELECTION, 1, &sel,
                                           &count)) &&
          count && sel.range) {
        SpacingReader reader{*this, ec, sel.range};
        edit.spacing_done = true;
        BOOL empty = TRUE;
        bool empty_selection =
            SUCCEEDED(sel.range->IsEmpty(ec, &empty)) && empty;
        if (!empty_selection)
          spacer_.clear();
        if (spacer_.key(edit.spacing_key, edit.spacing_mods, reader,
                        empty_selection) &&
            !focus_pending_) {
          // One insertion makes a single undo step. No engine notification
          // is added: it already saw this key if its policy requested one.
          wchar_t text[] = {L' ', wchar_t(edit.spacing_key)};
          hr = sel.range->SetText(ec, 0, text, 2);
          if (SUCCEEDED(hr)) {
            sel.range->Collapse(ec, TF_ANCHOR_END);
            hr = context_->SetSelection(ec, 1, &sel);
            // SetText consumed the key even if setting the caret failed.
            edit.spacing_inserted = true;
          }
          if (FAILED(hr))
            spacer_.clear();
        }
        sel.range->Release();
      }
    }
    inside_ = false;
    if (secure)
      secure_changed(true);
    flush_focus();
    return hr;
  }
  if (edit.recovery) {
    // Drained results can carry the previous spacing flag. Literal outage
    // recovery preserves the accepted keys exactly, without a new prefix.
    spacer_.focus();
    inside_ = true;
    const bool secure = password(ec, context_);
    HRESULT hr = S_OK;
    if (composition_) {
      auto old = composition_;
      composition_ = nullptr;
      hr = end_composition(ec, old);
      old->Release();
    }
    if (SUCCEEDED(hr) && !secure && fallback_size_) {
      Result literal;
      literal.commit = {fallback_.data(), fallback_size_};
      hr = apply_text(ec, literal, 0);
    }
    fallback_size_ = 0;
    fallback_.fill(0);
    recovering_ = false;
    finalize_needed_ = false;
    probe_needed_ = false;
    core_.reset();
    core_.secure = secure;
    inside_ = false;
    flush_focus();
    return hr;
  }
  inside_ = true;
  bool secure = password(ec, context_);
  if (transport_)
    transport_->trace("execute", secure, edit.probe, epoch_);
  inside_ = false;
  if (flush_focus())
    return S_OK;
  if (edit.probe) {
    core_.secure = secure;
    core_.known = false;
    if (!secure && !attributes_) {
      inside_ = true;
      context_->GetProperty(GUID_PROP_ATTRIBUTE, &attributes_);
      inside_ = false;
      if (flush_focus())
        return S_OK;
    }
    send_focus();
    return S_OK;
  }
  if (edit_read_ == edit_write_)
    return S_OK;
  auto &pending = edit_meta_[edit_read_ % kEditSlots];
  size_t size = 0;
  auto bytes = edit_frames_.front(size);
  if (!bytes) {
    edit_clear();
    return S_OK;
  }
  // The frame stays in the ring, and so valid, until edit_pop().
  Result result;
  if (!result.decode(frame_payload({bytes, size}))) {
    degraded();
    return E_FAIL;
  }
  if (secure) {
    secure_changed(true);
    return S_OK;
  }
  if (!core_.connected || pending.epoch != epoch_) {
    edit_pop();
    return S_OK;
  }
  HRESULT hr = S_OK;
  inside_ = true;
  // Passed keys aren't translated while spacing is disabled. On enabling,
  // learn the current boundary again rather than trusting another layout's
  // unverified history. Known text inserted below is tracked normally.
  if ((result.flags & kAutoSpace) && !spacer_.on)
    spacer_.clear();
  spacer_.on = (result.flags & kAutoSpace) != 0;
  spacer_.tick(now());
  bool ascii = (result.flags & kASCII) != 0;
  bool mode_changed = mode_known_ && mode_ascii_ != ascii;
  mode_known_ = true;
  mode_ascii_ = ascii;
  if (mode_changed || !result.seq)
    mode_until_ = GetTickCount64() + 900;
  if (result.count)
    mode_until_ = 0;
  // Notify keys reach the app too, but their results still apply: a Shift
  // tap's commit_code commits the typed letters and ends the composition,
  // as on macOS. A result that changes nothing is a no-op in apply_text,
  // and only eaten keys carry a repair.
  if (ui_) {
    if (result.count)
      caret(ec);
    display(scratch_, result);
    ui_->send(scratch_);
  }
  hr = apply_text(ec, result, pending.notify ? 0 : pending.repair);
  if (SUCCEEDED(hr) && (result.count || mode_changed || !result.seq))
    caret(ec);
  inside_ = false;
  if (flush_focus())
    return hr;
  if (SUCCEEDED(hr))
    core_.acknowledge(result.seq);
  else
    degraded();
  edit_pop();
  return hr;
}
void Service::edit_pop() {
  if (edit_read_ == edit_write_)
    return;
  ++edit_read_;
  edit_frames_.pop();
}
void Service::edit_clear() {
  while (edit_read_ != edit_write_)
    edit_pop();
}
HRESULT Service::apply_text(TfEditCookie ec, Result &result, uint32_t repair) {
  if (!composition_ && result.commit.empty() && result.preedit.empty() &&
      !repair)
    return S_OK;
  ITfRange *range = nullptr;
  if (composition_) {
    if (FAILED(composition_->GetRange(&range)))
      return E_FAIL;
  } else {
    TF_SELECTION sel{};
    ULONG n = 0;
    if (FAILED(context_->GetSelection(ec, TF_DEFAULT_SELECTION, 1, &sel, &n)) ||
        !n || !sel.range)
      return E_FAIL;
    range = sel.range;
  }
  HRESULT hr = S_OK;
  bool allow_spacing = !composition_ || composition_spacing_;
  if (!composition_) {
    BOOL empty = TRUE;
    if (FAILED(range->IsEmpty(ec, &empty)) || !empty) {
      spacer_.clear();
      allow_spacing = false;
    }
  }
  auto set = [&](std::string_view text, bool commit = false) {
    if (text.size() >= wide_.size())
      return E_FAIL;
    SpacingReader reader{*this, ec, range};
    auto start = text;
    bool space = allow_spacing && commit && !text.empty() &&
                 spacer_.prefix(space_class(space_decode(start)), reader);
    if (focus_pending_)
      return TF_E_DISCONNECTED;
    int n = text.empty()
                ? 0
                : MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS,
                                      text.data(), int(text.size()),
                                      wide_.data() + (space ? 1 : 0),
                                      int(wide_.size()) - (space ? 1 : 0));
    if (!text.empty() && !n)
      return E_FAIL;
    if (space)
      wide_[0] = L' ';
    auto result = range->SetText(ec, 0, wide_.data(), n + (space ? 1 : 0));
    if (SUCCEEDED(result) && commit && !text.empty())
      spacer_.inserted(text, space);
    else if (FAILED(result))
      spacer_.clear();
    return result;
  };
  if (!result.commit.empty() || repair) {
    hr = set(result.commit, true);
    if (SUCCEEDED(hr) && repair) {
      range->Collapse(ec, TF_ANCHOR_END);
      char ch = char(repair);
      hr = set({&ch, 1}, true);
    }
    if (composition_) {
      auto old = composition_;
      composition_ = nullptr;
      end_composition(ec, old);
      old->Release();
    }
    range->Collapse(ec, TF_ANCHOR_END);
    allow_spacing = true;
  }
  if (focus_pending_)
    hr = TF_E_DISCONNECTED;
  if (SUCCEEDED(hr) && !result.preedit.empty()) {
    if (!composition_) {
      ITfContextComposition *cc = nullptr;
      hr = context_->QueryInterface(IID_ITfContextComposition,
                                    reinterpret_cast<void **>(&cc));
      if (SUCCEEDED(hr)) {
        composition_spacing_ = allow_spacing;
        hr = cc->StartComposition(
            ec, range, static_cast<ITfCompositionSink *>(this), &composition_);
        cc->Release();
      }
    }
    if (SUCCEEDED(hr))
      hr = set(result.preedit);
    if (SUCCEEDED(hr) && !focus_pending_)
      mark_attributes(ec, range, result);
    if (focus_pending_)
      hr = TF_E_DISCONNECTED;
    // Convert the UTF-8 cursor prefix, not the byte offset, to UTF-16.
    if (SUCCEEDED(hr)) {
      LONG prefix = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS,
                                        result.preedit.data(),
                                        int(result.cursor), nullptr, 0);
      ITfRange *caret = nullptr;
      hr = range->Clone(&caret);
      if (SUCCEEDED(hr)) {
        caret->Collapse(ec, TF_ANCHOR_START);
        LONG moved = 0;
        caret->ShiftEnd(ec, prefix, &moved, nullptr);
        caret->Collapse(ec, TF_ANCHOR_END);
        TF_SELECTION selected{caret, {TF_AE_NONE, FALSE}};
        hr = context_->SetSelection(ec, 1, &selected);
        caret->Release();
      }
    }
  } else if (SUCCEEDED(hr)) {
    if (result.commit.empty() && !repair)
      hr = set({});
    if (composition_) {
      auto old = composition_;
      composition_ = nullptr;
      end_composition(ec, old);
      old->Release();
    }
    range->Collapse(ec, TF_ANCHOR_END);
    TF_SELECTION selected{range, {TF_AE_NONE, FALSE}};
    if (SUCCEEDED(hr))
      hr = context_->SetSelection(ec, 1, &selected);
  }
  range->Release();
  return hr;
}
void Service::mark_attributes(TfEditCookie ec, ITfRange *range,
                              const Result &result) {
  if (!attributes_ || input_atom_ == TF_INVALID_GUIDATOM)
    return;
  VARIANT value;
  VariantInit(&value);
  value.vt = VT_I4;
  value.lVal = LONG(input_atom_);
  if (FAILED(attributes_->SetValue(ec, range, &value)) ||
      target_atom_ == TF_INVALID_GUIDATOM || result.end <= result.start)
    return;
  auto prefix = [&](uint32_t bytes) -> LONG {
    return bytes ? MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS,
                                       result.preedit.data(), int(bytes),
                                       nullptr, 0)
                 : 0;
  };
  LONG start = prefix(result.start), end = prefix(result.end), moved = 0;
  if (end <= start)
    return;
  ITfRange *selected = nullptr;
  if (SUCCEEDED(range->Clone(&selected)) && selected) {
    if (SUCCEEDED(selected->Collapse(ec, TF_ANCHOR_START)) &&
        SUCCEEDED(selected->ShiftEnd(ec, end, &moved, nullptr)) &&
        moved == end &&
        SUCCEEDED(selected->ShiftStart(ec, start, &moved, nullptr)) &&
        moved == start && !focus_pending_) {
      value.lVal = LONG(target_atom_);
      attributes_->SetValue(ec, selected, &value);
    }
    selected->Release();
  }
}
bool Service::SpacingReader::read(uint32_t &r) {
  if (!range)
    return false;
  // A selection replacement isn't a known boundary. A composition range
  // is allowed: its start is precisely the character before marked text.
  BOOL empty = FALSE;
  if (!service.composition_ &&
      (FAILED(range->IsEmpty(cookie, &empty)) || !empty))
    return false;
  ITfRange *left = nullptr;
  if (FAILED(range->Clone(&left)) || !left)
    return false;
  LONG moved = 0;
  wchar_t text[2]{};
  ULONG n = 0;
  bool ok = SUCCEEDED(left->Collapse(cookie, TF_ANCHOR_START)) &&
            SUCCEEDED(left->ShiftStart(cookie, -2, &moved, nullptr));
  if (ok && moved)
    ok = SUCCEEDED(left->GetText(cookie, 0, text, 2, &n)) && n > 0;
  left->Release();
  if (!ok)
    return false;
  r = n ? uint32_t(text[n - 1]) : 0;
  if (r >= 0xdc00 && r <= 0xdfff) {
    if (n != 2 || text[0] < 0xd800 || text[0] > 0xdbff)
      return false;
    r = 0x10000 + (uint32_t(text[0]) - 0xd800) * 1024 + r - 0xdc00;
  } else if (r >= 0xd800 && r <= 0xdbff)
    return false;
  return true;
}
bool Service::passed_key(uint32_t sym, uint32_t mods) {
  if (mods & kRelease)
    return false;
  auto epoch = epoch_;
  bool inserted = false, done = false;
  const bool drained =
      core_.applied == core_.sent ||
      (core_.sent == core_.applied + 1 && core_.received == core_.applied &&
       core_.pending[core_.sent % core_.pending.size()].notify);
  // No new barrier or wait: if TSF defers this optional edit, cancel it
  // before passing the original key. A late callback must insert nothing.
  if (spacer_.needs_key_read(sym, mods) && edit_.idle() &&
      edit_read_ == edit_write_ && drained && !composition_) {
    edit_.prepare(this, context_, epoch_, false);
    edit_.spacing_key = sym;
    edit_.spacing_mods = mods;
    HRESULT result = E_FAIL;
    context_->RequestEditSession(
        client_, &edit_, TF_ES_ASYNCDONTCARE | TF_ES_READWRITE, &result);
    done = edit_.spacing_done;
    inserted = edit_.spacing_inserted;
    edit_.cancelled = true;
    edit_.Release();
  }
  if (core_.secure || !context_ || epoch_ != epoch)
    return inserted;
  if (!done) {
    SpacingReader reader{*this, 0, nullptr};
    spacer_.key(sym, mods, reader, false);
  }
  // After an ordering timeout, a passed key must still invalidate older
  // engine writes. Only drained work (or this key's own notify) is observed.
  passed_edit_ = drained && !inserted && !(mods & kCommand) &&
                 ((sym >= 0x20 && sym < 0x7f) || sym == 0xff08 ||
                  sym == 0xff0d || sym == 0xff8d);
  if (transport_)
    transport_->trace("spacing-key", spacer_.count, uint32_t(spacer_.left()),
                      spacer_.muted);
  return inserted;
}
bool Service::same_passed_selection(TfEditCookie ec) {
  TF_SELECTION selected{};
  ULONG count = 0;
  if (FAILED(context_->GetSelection(ec, TF_DEFAULT_SELECTION, 1, &selected,
                                    &count)) ||
      !count || !selected.range)
    return false;
  BOOL start = FALSE, end = FALSE;
  bool same = SUCCEEDED(selected.range->IsEqualStart(
                  ec, passed_selection_, TF_ANCHOR_START, &start)) &&
              start &&
              SUCCEEDED(selected.range->IsEqualEnd(ec, passed_selection_,
                                                   TF_ANCHOR_END, &end)) &&
              end;
  selected.range->Release();
  return same;
}
bool Service::matches_character(uint32_t sym, WPARAM vk, LPARAM lp) {
  BYTE state[256]{};
  wchar_t text[4]{};
  // Flag 4 leaves the keyboard's dead-key state untouched.
  return GetKeyboardState(state) &&
         ToUnicodeEx(UINT(vk), UINT(lp >> 16) & 255, state, text, 4, 4,
                     GetKeyboardLayout(0)) == 1 &&
         uint32_t(text[0]) == sym;
}
HRESULT Service::handle(ITfContext *c, WPARAM vk, LPARAM lp, bool up,
                        BOOL *eaten) {
  if (!eaten)
    return E_POINTER;
  *eaten = FALSE;
  if (transport_)
    transport_->trace("handle", uintptr_t(c), uintptr_t(context_),
                      (core_.secure ? 1 : 0) | (core_.connected ? 2 : 0) |
                          (core_.known ? 4 : 0));
  if (inside_ || c != context_ || !active_ || !transport_ || core_.secure ||
      disabled())
    return S_OK;
  uint32_t mods = 0;
  if (GetKeyState(VK_SHIFT) & 0x8000)
    mods |= kShift;
  if (GetKeyState(VK_CONTROL) & 0x8000)
    mods |= kControl;
  if (GetKeyState(VK_MENU) & 0x8000)
    mods |= kAlt;
  if (GetKeyState(VK_LWIN) & 0x8000 || GetKeyState(VK_RWIN) & 0x8000)
    mods |= kSuper;
  if (GetKeyState(VK_CAPITAL) & 1)
    mods |= kLock;
  if (up)
    mods |= kRelease;
  auto sym = windows_key(uint32_t(vk), uint32_t(lp >> 16) & 255,
                         (lp & (1u << 24)) != 0, mods);
  if (!up) {
    spacer_.tick(now());
    passed_edit_ = false;
    release(passed_selection_);
  }
  if (sym) {
    if (!transport_->online())
      degraded();
    auto recover_key = [&] {
      if (!recovering_ || (mods & kRelease) || sym == 0xffe1 || sym == 0xffe2)
        return;
      if (append_literal(fallback_.data(), fallback_size_, fallback_.size(),
                         sym, mods)) {
        *eaten = TRUE;
      } else {
        // Navigation, shortcuts or a full tail must cancel delayed writes
        // before the application receives the key and changes selection.
        secure_changed(core_.secure);
        finalize_needed_ = composition_ != nullptr;
      }
    };
    if (recovering_) {
      recover_key();
      return S_OK;
    }
    ++key_depth_;
    *eaten = core_.key(sym, mods, *this) ? TRUE : FALSE;
    --key_depth_;
    if (!*eaten)
      recover_key(); // pump() can discover a disconnect inside this key.
    if (!*eaten && !recovering_ && !core_.secure && core_.connected &&
        !core_.migrating) {
      // Validate passed text before BOTH tracking and inserting it: another
      // layout's shifted digit may actually be punctuation. Tracking the US
      // class would invent a boundary in an app without surrounding text.
      bool same = true;
      if (spacer_.on && sym >= 0x20 && sym < 0x7f &&
          !(mods & (kCommand | kRelease)))
        same = matches_character(sym, vk, lp);
      if (same)
        *eaten = passed_key(sym, mods) ? TRUE : FALSE;
      else
        spacer_.clear();
    }
    if (composition_ && c == context_ &&
        finish_before_shortcut(sym, mods, *eaten != FALSE)) {
      // End the marked range before the application handles a focus-moving
      // shortcut. Use the pooled, nonwaiting edit request; pending engine
      // replies belong to the old epoch and must never reinsert its preedit.
      secure_changed(core_.secure);
      finalize_needed_ = true;
      schedule();
    }
  } else if (!up) {
    spacer_.clear();
  }
  return S_OK;
}
HRESULT Service::OnTestKeyDown(ITfContext *c, WPARAM vk, LPARAM lp,
                               BOOL *eaten) {
  if (transport_)
    transport_->trace("test-down", vk, lp, epoch_);
  auto hr = handle(c, vk, lp, false, eaten);
  tested_ = eaten && *eaten;
  test_up_ = false;
  test_vk_ = vk;
  test_lparam_ = key_message(uint32_t(lp));
  test_eaten_ = tested_;
  test_epoch_ = epoch_;
  return hr;
}
HRESULT Service::OnTestKeyUp(ITfContext *c, WPARAM vk, LPARAM lp, BOOL *eaten) {
  if (transport_)
    transport_->trace("test-up", vk, lp, epoch_);
  auto hr = handle(c, vk, lp, true, eaten);
  tested_ = eaten && *eaten;
  test_up_ = true;
  test_vk_ = vk;
  test_lparam_ = key_message(uint32_t(lp));
  test_eaten_ = tested_;
  test_epoch_ = epoch_;
  return hr;
}
HRESULT Service::OnKeyDown(ITfContext *c, WPARAM vk, LPARAM lp, BOOL *eaten) {
  if (transport_)
    transport_->trace("key-down", vk, lp, tested_);
  if (!eaten)
    return E_POINTER;
  if (tested_ && !test_up_ && test_vk_ == vk &&
      test_lparam_ == key_message(uint32_t(lp))) {
    tested_ = false;
    *eaten =
        test_epoch_ == epoch_ && c == context_ && !core_.secure && !disabled()
            ? test_eaten_
            : FALSE;
    return S_OK;
  }
  return handle(c, vk, lp, false, eaten);
}
HRESULT Service::OnKeyUp(ITfContext *c, WPARAM vk, LPARAM lp, BOOL *eaten) {
  if (!eaten)
    return E_POINTER;
  if (tested_ && test_up_ && test_vk_ == vk &&
      test_lparam_ == key_message(uint32_t(lp))) {
    tested_ = false;
    *eaten =
        test_epoch_ == epoch_ && c == context_ && !core_.secure && !disabled()
            ? test_eaten_
            : FALSE;
    return S_OK;
  }
  return handle(c, vk, lp, true, eaten);
}
} // namespace shixin
