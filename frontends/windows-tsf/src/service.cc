// SPDX-License-Identifier: GPL-3.0-or-later
#include "service.h"
#include "keys.h"
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
    return composition_->EndComposition(ec);
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
HRESULT Service::OnCompositionTerminated(TfEditCookie, ITfComposition *c) {
  if (transport_)
    transport_->trace("terminated", uintptr_t(c), uintptr_t(composition_));
  if (c == composition_) {
    release(composition_);
    degraded();
  }
  return S_OK;
}
HRESULT Service::OnEndEdit(ITfContext *c, TfEditCookie ec, ITfEditRecord *) {
  if (c != context_ || inside_)
    return S_OK;
  inside_ = true;
  bool secure = password(ec, c);
  inside_ = false;
  if (flush_focus())
    return S_OK;
  if (secure != core_.secure)
    secure_changed(secure);
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
  finalize_needed_ = true;
  if (window_)
    PostMessageW(window_, kWake, 0, 0);
  ui_focus(true);
  core_.connected = false;
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
      Result result;
      if (!result.decode(frame_payload(frame))) {
        degraded();
        break;
      }
      auto complete = core_.result(result);
      if (!complete.accept)
        break;
      auto slot = edit_write_ - edit_read_ == kEditSlots
                      ? nullptr
                      : edit_frames_.reserve(frame.size());
      if (!slot) {
        degraded();
        break;
      }
      std::memcpy(slot, frame.data(), frame.size());
      edit_frames_.publish(frame.size());
      auto &edit = edit_meta_[edit_write_ % kEditSlots];
      edit.epoch = epoch_;
      edit.repair = complete.repair;
      edit.notify = complete.notify;
      ++edit_write_;
      break;
    }
    case Migrate:
      ui_focus(true);
      core_.migrating = true;
      transport_->migrate();
      break;
    case kDisconnected:
      ui_focus(true);
      if (core_.migrating)
        draining_ = true;
      else {
        core_.reset();
        finalize_needed_ = true;
      }
      break;
    case Unavailable:
      finalize_needed_ = true;
      ui_focus(true);
      core_.reset();
      transport_->disconnect();
      break;
    default:
      break;
    }
    transport_->pop();
  }
  schedule();
  pumping_ = false;
}
void Service::schedule() {
  if (!context_ || !active_ || !edit_.idle())
    return;
  if (finalize_needed_ && !composition_)
    finalize_needed_ = false;
  if (finalize_needed_ && key_depth_)
    return;
  bool finishing = finalize_needed_;
  if (!finishing && !probe_needed_ && edit_read_ == edit_write_)
    return;
  bool probe = !finishing && probe_needed_;
  if (probe)
    probe_needed_ = false;
  edit_.prepare(this, context_, epoch_, probe);
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
    degraded();
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
    return edit.ending->EndComposition(ec);
  if (!active_ || edit.epoch != epoch_ || edit.context != context_)
    return S_OK;
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
  if (SUCCEEDED(hr) && result.count)
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
  auto set = [&](std::string_view text) {
    if (text.size() > wide_.size())
      return E_FAIL;
    int n = text.empty() ? 0
                         : MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS,
                                               text.data(), int(text.size()),
                                               wide_.data(), int(wide_.size()));
    if (!text.empty() && !n)
      return E_FAIL;
    return range->SetText(ec, 0, wide_.data(), n);
  };
  if (!result.commit.empty() || repair) {
    hr = set(result.commit);
    if (SUCCEEDED(hr) && repair) {
      range->Collapse(ec, TF_ANCHOR_END);
      wchar_t ch = wchar_t(repair);
      hr = range->SetText(ec, 0, &ch, 1);
    }
    if (composition_) {
      auto old = composition_;
      composition_ = nullptr;
      old->EndComposition(ec);
      old->Release();
    }
    range->Collapse(ec, TF_ANCHOR_END);
  }
  if (SUCCEEDED(hr) && !result.preedit.empty()) {
    if (!composition_) {
      ITfContextComposition *cc = nullptr;
      hr = context_->QueryInterface(IID_ITfContextComposition,
                                    reinterpret_cast<void **>(&cc));
      if (SUCCEEDED(hr)) {
        hr = cc->StartComposition(
            ec, range, static_cast<ITfCompositionSink *>(this), &composition_);
        cc->Release();
      }
    }
    if (SUCCEEDED(hr))
      hr = set(result.preedit);
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
      old->EndComposition(ec);
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
  if (!transport_->online()) {
    degraded();
    return S_OK;
  }
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
  if (sym) {
    ++key_depth_;
    *eaten = core_.key(sym, mods, *this) ? TRUE : FALSE;
    --key_depth_;
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
  test_lparam_ = lp;
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
  test_lparam_ = lp;
  test_eaten_ = tested_;
  test_epoch_ = epoch_;
  return hr;
}
HRESULT Service::OnKeyDown(ITfContext *c, WPARAM vk, LPARAM lp, BOOL *eaten) {
  if (transport_)
    transport_->trace("key-down", vk, lp, tested_);
  if (!eaten)
    return E_POINTER;
  if (tested_ && !test_up_ && test_vk_ == vk && test_lparam_ == lp) {
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
  if (tested_ && test_up_ && test_vk_ == vk && test_lparam_ == lp) {
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
