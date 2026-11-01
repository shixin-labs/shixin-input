// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "core.h"
#include "ids.h"
#include "spacer.h"
#include "transport.h"
#include <inputscope.h>
#include <memory>
#include <msctf.h>
#include <windows.h>
namespace shixin {
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
  bool recovery = false;
  uint32_t spacing_key = 0, spacing_mods = 0;
  bool spacing_done = false, spacing_inserted = false, cancelled = false;
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
  friend struct ServiceTest;
  std::atomic<ULONG> refs_{1};
  ITfThreadMgr *manager_ = nullptr;
  ITfContext *context_ = nullptr;
  ITfComposition *composition_ = nullptr;
  bool composition_spacing_ = true;
  ITfCompartment *disabled_ = nullptr;
  TfClientId client_ = 0;
  DWORD manager_cookie_ = TF_INVALID_COOKIE, text_cookie_ = TF_INVALID_COOKIE;
  HWND window_ = nullptr;
  uint32_t epoch_ = 0, test_epoch_ = 0;
  bool active_ = false, probe_needed_ = false, inside_ = false,
       pumping_ = false, focus_pending_ = false, draining_ = false,
       finalize_needed_ = false;
  bool recovering_ = false;
  Spacer spacer_;
  bool passed_edit_ = false;
  ITfRange *passed_selection_ = nullptr;
  std::array<char, 128> fallback_{};
  size_t fallback_size_ = 0;
  unsigned key_depth_ = 0;
  ITfContext *next_context_ = nullptr;
  bool tested_ = false, test_up_ = false, test_eaten_ = false;
  WPARAM test_vk_ = 0;
  LPARAM test_lparam_ = 0;
  Core core_;
  std::shared_ptr<Transport> transport_, ui_;
  Edit edit_;
  Frame scratch_;
  std::array<char, 4096> app_{};
  size_t app_size_ = 0;
  // Results waiting for an edit session, applied in order: their frames in a
  // byte ring (a result is a few KB, at most kFrameCapacity), and what Core
  // decided about each beside it. Both are bounded; when either is full the
  // service degrades. Only the apartment thread touches them.
  static constexpr size_t kEditSlots = 32;
  struct EditMeta {
    uint32_t epoch = 0, repair = 0;
    bool notify = false;
  };
  FrameRing edit_frames_{kLargeRing};
  std::array<EditMeta, kEditSlots> edit_meta_{};
  size_t edit_read_ = 0, edit_write_ = 0;
  void edit_pop();
  void edit_clear();
  void receive_result(std::string_view);
  std::array<wchar_t, 16385> wide_{};
  LARGE_INTEGER frequency_{};
  HANDLE timer_ = nullptr; // high-resolution deadline for wait_until
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
  struct SpacingReader {
    Service &service;
    TfEditCookie cookie;
    ITfRange *range;
    bool read(uint32_t &);
    uint64_t now() { return service.now(); }
  };
  bool passed_key(uint32_t, uint32_t);
  bool same_passed_selection(TfEditCookie);
  static bool matches_character(uint32_t, WPARAM, LPARAM);
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
