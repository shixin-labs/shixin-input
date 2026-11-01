// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "core.h"
#include "transport.h"
#include <inputscope.h>
#include <memory>
#include <msctf.h>
#include <windows.h>
namespace shixin {
// The extended interface ABI is also declared here for SDKs (including
// MinGW) that expose only ITfTextInputProcessor. IID from Microsoft's SDK.
inline constexpr IID kProcessorEx = {
    0x6e4e2102,
    0xf9cd,
    0x433d,
    {0xb4, 0x96, 0x30, 0x3c, 0xe0, 0x3a, 0x65, 0x07}};
struct ProcessorEx : ITfTextInputProcessor {
  virtual HRESULT STDMETHODCALLTYPE ActivateEx(ITfThreadMgr *, TfClientId,
                                               DWORD) = 0;
};
inline constexpr GUID kInputScope = {
    0xfde1eaee,
    0x6924,
    0x4cdf,
    {0x91, 0xe7, 0xda, 0x38, 0xcf, 0xf5, 0x55, 0x9d}};
inline constexpr GUID kInputScopeProperty = {
    0x1713dd5a,
    0x68e7,
    0x4a5b,
    {0x9a, 0xf6, 0x59, 0x2a, 0x59, 0x5c, 0x77, 0x8d}};
inline constexpr CLSID kClass = {
    0xaac1a491,
    0xe694,
    0x4610,
    {0x96, 0xb5, 0xa4, 0x76, 0xa1, 0x44, 0x12, 0x24}};
inline constexpr GUID kProfile = {
    0xe887d942,
    0xb23a,
    0x408e,
    {0x81, 0x32, 0x15, 0x02, 0x4a, 0x35, 0x99, 0xc1}};
extern HMODULE module;
class Service;
class Edit : public ITfEditSession {
  std::atomic<ULONG> refs_{0};

public:
  Service *owner = nullptr;
  ITfContext *context = nullptr;
  ITfComposition *ending = nullptr;
  uint32_t epoch = 0;
  bool probe = false;
  bool idle() const { return refs_.load() == 0; }
  void prepare(Service *, ITfContext *, uint32_t, bool);
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID, void **) override;
  ULONG STDMETHODCALLTYPE AddRef() override { return ++refs_; }
  ULONG STDMETHODCALLTYPE Release() override;
  HRESULT STDMETHODCALLTYPE DoEditSession(TfEditCookie) override;
};
class Service final : public ProcessorEx,
                      public ITfKeyEventSink,
                      public ITfThreadMgrEventSink,
                      public ITfCompositionSink,
                      public ITfTextEditSink,
                      public Host {
  std::atomic<ULONG> refs_{1};
  ITfThreadMgr *manager_ = nullptr;
  ITfContext *context_ = nullptr;
  ITfComposition *composition_ = nullptr;
  ITfCompartment *disabled_ = nullptr;
  TfClientId client_ = 0;
  DWORD manager_cookie_ = TF_INVALID_COOKIE, text_cookie_ = TF_INVALID_COOKIE;
  HWND window_ = nullptr;
  uint32_t epoch_ = 0, test_epoch_ = 0;
  bool active_ = false, probe_needed_ = false, inside_ = false,
       pumping_ = false, focus_pending_ = false, draining_ = false,
       finalize_needed_ = false;
  unsigned key_depth_ = 0;
  ITfContext *next_context_ = nullptr;
  bool tested_ = false, test_up_ = false, test_eaten_ = false;
  WPARAM test_vk_ = 0;
  LPARAM test_lparam_ = 0;
  Core core_;
  std::shared_ptr<Transport> transport_, ui_;
  Edit edit_;
  Frame scratch_;
  std::array<char, 32768> app_{};
  size_t app_size_ = 0;
  struct Display {
    Frame frame;
    uint32_t epoch = 0, repair = 0;
    bool notify = false;
  };
  std::array<Display, 32> edits_{};
  size_t edit_read_ = 0, edit_write_ = 0;
  std::array<wchar_t, 32768> wide_{};
  LARGE_INTEGER frequency_{};
  static LRESULT CALLBACK window_proc(HWND, UINT, WPARAM, LPARAM);
  void focus(ITfContext *);
  bool flush_focus();
  void send_focus();
  void secure_changed(bool);
  void ui_focus(bool hide = false);
  void choose(uint32_t);
  void caret(TfEditCookie);
  void schedule();
  bool password(TfEditCookie, ITfContext *);
  bool disabled() const;
  HRESULT apply_text(TfEditCookie, Result &, uint32_t repair);
  HRESULT handle(ITfContext *, WPARAM, LPARAM, bool, BOOL *);

public:
  Service();
  ~Service();
  HRESULT STDMETHODCALLTYPE QueryInterface(REFIID, void **) override;
  ULONG STDMETHODCALLTYPE AddRef() override { return ++refs_; }
  ULONG STDMETHODCALLTYPE Release() override {
    auto n = --refs_;
    if (!n)
      delete this;
    return n;
  }
  HRESULT STDMETHODCALLTYPE Activate(ITfThreadMgr *m, TfClientId id) override {
    return ActivateEx(m, id, 0);
  }
  HRESULT STDMETHODCALLTYPE ActivateEx(ITfThreadMgr *, TfClientId,
                                       DWORD) override;
  HRESULT STDMETHODCALLTYPE Deactivate() override;
  HRESULT STDMETHODCALLTYPE OnSetFocus(BOOL foreground) override;
  HRESULT STDMETHODCALLTYPE OnTestKeyDown(ITfContext *, WPARAM, LPARAM,
                                          BOOL *) override;
  HRESULT STDMETHODCALLTYPE OnTestKeyUp(ITfContext *, WPARAM, LPARAM,
                                        BOOL *) override;
  HRESULT STDMETHODCALLTYPE OnKeyDown(ITfContext *, WPARAM, LPARAM,
                                      BOOL *) override;
  HRESULT STDMETHODCALLTYPE OnKeyUp(ITfContext *, WPARAM, LPARAM,
                                    BOOL *) override;
  HRESULT STDMETHODCALLTYPE OnPreservedKey(ITfContext *, REFGUID,
                                           BOOL *eaten) override {
    if (!eaten)
      return E_POINTER;
    *eaten = FALSE;
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE OnInitDocumentMgr(ITfDocumentMgr *) override {
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE OnUninitDocumentMgr(ITfDocumentMgr *) override {
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE OnSetFocus(ITfDocumentMgr *,
                                       ITfDocumentMgr *) override;
  HRESULT STDMETHODCALLTYPE OnPushContext(ITfContext *c) override {
    focus(c);
    return S_OK;
  }
  HRESULT STDMETHODCALLTYPE OnPopContext(ITfContext *) override;
  HRESULT STDMETHODCALLTYPE OnCompositionTerminated(TfEditCookie,
                                                    ITfComposition *) override;
  HRESULT STDMETHODCALLTYPE OnEndEdit(ITfContext *, TfEditCookie,
                                      ITfEditRecord *) override;
  bool send(uint32_t, uint32_t, uint32_t, uint32_t, uint8_t) override;
  void pump() override;
  uint64_t now() override;
  void wait_until(uint64_t) override;
  void degraded() override;
  HRESULT execute(Edit &, TfEditCookie);
  void edit_released();
};
} // namespace shixin
