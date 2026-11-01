// SPDX-License-Identifier: GPL-3.0-or-later
#include "transport.h"
#include <algorithm>
#include <bcrypt.h>
#include <cstdio>
#include <cstring>
#include <memory>
#include <new>
#include <sddl.h>
#include <shlobj.h>
#include <string>
#include <vector>
namespace shixin {
namespace {
struct Handle {
  HANDLE value = INVALID_HANDLE_VALUE;
  explicit Handle(HANDLE h = INVALID_HANDLE_VALUE) : value(h) {}
  ~Handle() {
    if (value != INVALID_HANDLE_VALUE && value)
      CloseHandle(value);
  }
  Handle(const Handle &) = delete;
  Handle &operator=(const Handle &) = delete;
};
std::string utf8(std::wstring_view text) {
  int n = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, text.data(),
                              int(text.size()), nullptr, 0, nullptr, nullptr);
  if (!n)
    return {};
  std::string out(size_t(n), '\0');
  WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, text.data(),
                      int(text.size()), out.data(), n, nullptr, nullptr);
  return out;
}
std::wstring wide(std::string_view text) {
  int n = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, text.data(),
                              int(text.size()), nullptr, 0);
  if (!n)
    return {};
  std::wstring out(size_t(n), L'\0');
  MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, text.data(),
                      int(text.size()), out.data(), n);
  return out;
}
std::wstring user_sid(HANDLE process) {
  HANDLE token = nullptr;
  if (!OpenProcessToken(process, TOKEN_QUERY, &token))
    return {};
  Handle guard(token);
  DWORD n = 0;
  GetTokenInformation(token, TokenUser, nullptr, 0, &n);
  std::vector<char> b(n);
  if (!GetTokenInformation(token, TokenUser, b.data(), n, &n))
    return {};
  LPWSTR sid = nullptr;
  if (!ConvertSidToStringSidW(
          reinterpret_cast<TOKEN_USER *>(b.data())->User.Sid, &sid))
    return {};
  std::wstring out(sid);
  LocalFree(sid);
  return out;
}
std::wstring pipe_name(std::wstring path, const std::wstring &sid,
                       DWORD session) {
  // Go's filepath.Abs/Clean output is already published by the coordinator.
  // Fold each Unicode code point with the invariant Windows casing table.
  if (path.empty())
    return {};
  std::wstring folded(path.size(), L'\0');
  if (!LCMapStringEx(LOCALE_NAME_INVARIANT, LCMAP_LOWERCASE, path.data(),
                     int(path.size()), folded.data(), int(folded.size()),
                     nullptr, nullptr, 0))
    return {};
  path = std::move(folded);
  auto bytes = utf8(path);
  std::array<UCHAR, 32> digest{};
  if (BCryptHash(BCRYPT_SHA256_ALG_HANDLE, nullptr, 0,
                 reinterpret_cast<PUCHAR>(bytes.data()), ULONG(bytes.size()),
                 digest.data(), ULONG(digest.size())) < 0)
    return {};
  const wchar_t hex[] = L"0123456789abcdef";
  std::wstring suffix;
  for (size_t i = 0; i < 16; ++i) {
    suffix += hex[digest[i] >> 4];
    suffix += hex[digest[i] & 15];
  }
  return L"\\\\.\\pipe\\shixin-" + std::to_wstring(session) + L"-" + sid +
         L"-" + suffix;
}
bool trusted(HANDLE pipe, const std::wstring &directory,
             const std::wstring &sid, DWORD session, bool presentation) {
  ULONG pid = 0;
  DWORD actual = 0;
  if (!GetNamedPipeServerProcessId(pipe, &pid) ||
      !GetNamedPipeServerSessionId(pipe, &actual) || actual != session)
    return false;
  Handle p(OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid));
  if (p.value == INVALID_HANDLE_VALUE || !p.value || user_sid(p.value) != sid)
    return false;
  std::array<wchar_t, 32768> path{};
  DWORD n = DWORD(path.size());
  if (!QueryFullProcessImageNameW(p.value, 0, path.data(), &n))
    return false;
  return _wcsicmp(path.data(),
                  (directory +
                   (presentation ? L"\\Shixin.exe" : L"\\shixin-engine.exe"))
                      .c_str()) == 0;
}
std::wstring endpoint(const std::wstring &runtime) {
  auto path = runtime + L"\\engine.sock";
  Handle f(CreateFileW(path.c_str(), GENERIC_READ,
                       FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
                       nullptr, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, nullptr));
  if (f.value == INVALID_HANDLE_VALUE)
    return {};
  std::array<char, 32768> b{};
  DWORD n = 0;
  if (!ReadFile(f.value, b.data(), DWORD(b.size()), &n, nullptr) || n == 0 ||
      n == b.size())
    return {};
  auto target = wide({b.data(), n});
  // A published worker must be an absolute sibling g-* endpoint, never a
  // remote path or a recursive discovery record.
  auto prefix = runtime + L"\\g-";
  if (target.compare(0, prefix.size(), prefix) != 0 ||
      target.find_first_of(L"\\/", prefix.size()) != std::wstring::npos)
    return {};
  return target;
}
} // namespace
std::wstring module_directory(HMODULE module) {
  std::array<wchar_t, 32768> b{};
  DWORD n = GetModuleFileNameW(module, b.data(), DWORD(b.size()));
  if (!n || n == b.size())
    return {};
  std::wstring path(b.data(), n);
  auto i = path.find_last_of(L"\\/");
  return i == std::wstring::npos ? std::wstring{} : path.substr(0, i);
}
Transport::Transport(HWND w, std::wstring directory, bool presentation)
    : window_(w), presentation_(presentation), directory_(std::move(directory)),
      outgoing_(presentation ? kLargeRing : kSmallRing),
      incoming_(presentation ? kSmallRing : kLargeRing) {
  stop_ = CreateEventW(nullptr, TRUE, FALSE, nullptr);
  wake_ = CreateEventW(nullptr, FALSE, FALSE, nullptr);
  arrived_ = CreateEventW(nullptr, FALSE, FALSE, nullptr);
}
Transport::~Transport() {
  if (stop_)
    CloseHandle(stop_);
  if (wake_)
    CloseHandle(wake_);
  if (arrived_)
    CloseHandle(arrived_);
}
bool Transport::start() {
  if (!stop_ || !wake_ || !arrived_ || !outgoing_.ok() || !incoming_.ok())
    return false;
  struct Worker {
    std::shared_ptr<Transport> self;
    HMODULE pin;
  };
  HMODULE pin = nullptr;
  if (!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS,
                          reinterpret_cast<LPCWSTR>(&module_directory), &pin))
    return false;
  auto worker = new (std::nothrow) Worker{shared_from_this(), pin};
  if (!worker) {
    FreeLibrary(pin);
    return false;
  }
  ++objects;
  HANDLE thread = CreateThread(
      nullptr, 0,
      [](LPVOID data) -> DWORD {
        auto state = static_cast<Worker *>(data);
        auto self = std::move(state->self);
        auto pin = state->pin;
        delete state;
        try {
          self->run();
        } catch (...) {
          self->online_.store(false);
          SetEvent(self->arrived_);
          PostMessageW(self->window_, kWake, 0, 0);
        }
        self.reset();
        --objects;
        // Drop the module pin atomically with exiting, so COM cannot unload
        // this DLL between a worker's last reference and its return address.
        FreeLibraryAndExitThread(pin, 0);
      },
      worker, 0, nullptr);
  if (!thread) {
    --objects;
    delete worker;
    FreeLibrary(pin);
    return false;
  }
  CloseHandle(thread);
  return true;
}
void Transport::stop() {
  if (stop_)
    SetEvent(stop_);
}
bool Transport::send(const Frame &frame) {
  if (!frame.good || drain_.load())
    return false;
  auto p = outgoing_.reserve(frame.size);
  if (!p)
    return false;
  std::memcpy(p, frame.data.data(), frame.size);
  outgoing_.publish(frame.size);
  SetEvent(wake_);
  return true;
}
void Transport::run() {
  auto sid = user_sid(GetCurrentProcess());
  DWORD session = 0;
  if (sid.empty() || !ProcessIdToSessionId(GetCurrentProcessId(), &session))
    return;
  PWSTR local = nullptr;
  // A packaged app's default LocalAppData points inside its sandbox. The
  // discovery record belongs to the desktop user's host, not that package.
  if (FAILED(SHGetKnownFolderPath(FOLDERID_LocalAppData,
                                  KF_FLAG_NO_PACKAGE_REDIRECTION |
                                      KF_FLAG_DONT_VERIFY,
                                  nullptr, &local)))
    return;
  std::wstring runtime =
      std::wstring(local) + L"\\Shixin\\run-" + std::to_wstring(session);
#ifdef SHIXIN_TSF_TRACE
  PWSTR low = nullptr;
  SHGetKnownFolderPath(FOLDERID_LocalAppDataLow, KF_FLAG_DONT_VERIFY, nullptr,
                       &low);
  std::wstring logdir = std::wstring(low ? low : local) + L"\\Shixin";
  CoTaskMemFree(low);
  CreateDirectoryW(logdir.c_str(), nullptr);
  auto logfile = logdir + L"\\tsf-" + std::to_wstring(GetCurrentProcessId()) +
                 L"-" + std::to_wstring(GetCurrentThreadId()) + L".log";
  Handle log(CreateFileW(logfile.c_str(), FILE_APPEND_DATA,
                         FILE_SHARE_READ | FILE_SHARE_WRITE, nullptr,
                         OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr));
  auto connection_trace = [&](DWORD error) {
    char line[128];
    int len = std::snprintf(line, sizeof(line), "connect-error %lu\n",
                            (unsigned long)error);
    DWORD wrote = 0;
    if (len > 0 && len < int(sizeof(line)))
      WriteFile(log.value, line, DWORD(len), &wrote, nullptr);
  };
  auto flush_trace = [&] {
    for (size_t n; auto p = traces_.front(n); traces_.pop()) {
      Trace t{};
      std::memcpy(&t, p, sizeof(t));
      char line[256];
      int len = std::snprintf(line, sizeof(line), "%llu %s %llx %llx %llx\n",
                              (unsigned long long)t.tick, t.event,
                              (unsigned long long)t.a, (unsigned long long)t.b,
                              (unsigned long long)t.c);
      DWORD wrote = 0;
      if (len > 0 && len < int(sizeof(line)))
        WriteFile(log.value, line, DWORD(len), &wrote, nullptr);
    }
  };
#else
  auto flush_trace = [] {};
  auto connection_trace = [](DWORD) {};
#endif
  CoTaskMemFree(local);
  auto arrived = [&] {
    SetEvent(arrived_);
    PostMessageW(window_, kWake, 0, 0);
  };
  // The first frame of every connection. Built once, in a temporary Frame.
  std::string initial;
  {
    auto f = std::make_unique<Frame>();
    hello(*f, 2, GetCurrentProcessId(), utf8(directory_));
    if (!f->good)
      return;
    initial.assign(f->view());
  }
  // Reconnects back off while connections keep ending quickly (an engine
  // migration refusing new clients, a full or mismatched candidate host):
  // 20 ms doubling to 2 s, reset by a connection that lasted 2 s.
  DWORD backoff = 20;
  while (WaitForSingleObject(stop_, 0) != WAIT_OBJECT_0) {
    flush_trace();
    // Discard keys from the previous connection before opening a new one.
    for (size_t n; outgoing_.front(n);)
      outgoing_.pop();
    drop_.store(false);
    drain_.store(false);
    auto name =
        pipe_name(presentation_ ? runtime + L"\\ui.sock" : endpoint(runtime),
                  sid, session);
    Handle pipe(name.empty()
                    ? INVALID_HANDLE_VALUE
                    : CreateFileW(name.c_str(),
                                  FILE_GENERIC_READ | FILE_WRITE_DATA, 0,
                                  nullptr, OPEN_EXISTING,
                                  FILE_FLAG_OVERLAPPED | SECURITY_SQOS_PRESENT |
                                      SECURITY_IDENTIFICATION,
                                  nullptr));
    if (pipe.value == INVALID_HANDLE_VALUE ||
        !trusted(pipe.value, directory_, sid, session, presentation_)) {
      connection_trace(GetLastError());
      WaitForSingleObject(stop_, 500);
      continue;
    }
    Handle read_event(CreateEventW(nullptr, TRUE, FALSE, nullptr)),
        write_event(CreateEventW(nullptr, TRUE, FALSE, nullptr));
    if (!read_event.value || !write_event.value)
      return;
    OVERLAPPED read{}, write{};
    read.hEvent = read_event.value;
    write.hEvent = write_event.value;
    online_.store(true);
    ULONGLONG opened = GetTickCount64();
    // Incoming frames are read straight into the ring: the length first,
    // then the rest into the slot reserved for it.
    char header[4], eof = 0, *slot = nullptr;
    const char *output = initial.data();
    size_t output_size = initial.size();
    bool has_output = true;
    bool initial_write = true, reading = false, writing = false, failed = false,
         half_closed = false, eof_write = false;
    size_t have = 0, need = 4;
    auto received = [&](DWORD n) {
      if (!n) {
        failed = true;
        return;
      }
      have += n;
      if (!slot && have == 4) {
        uint32_t length = 0;
        if (!frame_size({header, 4}, length) ||
            length + 4 > incoming_.max_frame()) {
          failed = true;
          return;
        }
        need = length + 4;
        slot = incoming_.reserve(need);
        if (!slot) {
          failed = true;
          return;
        }
        std::memcpy(slot, header, 4);
      }
      if (slot && have == need) {
        incoming_.publish(need);
        arrived();
        slot = nullptr;
        have = 0;
        need = 4;
      }
    };
    ULONGLONG drain_started = 0;
    while (!failed && WaitForSingleObject(stop_, 0) != WAIT_OBJECT_0 &&
           !drop_.load()) {
      if (drain_.load()) {
        auto tick = GetTickCount64();
        if (!drain_started)
          drain_started = tick;
        if (tick - drain_started > 2000) {
          failed = true;
          break;
        }
      }
      flush_trace();
      bool progress = false;
      if (!reading) {
        ResetEvent(read.hEvent);
        DWORD n = 0;
        if (ReadFile(pipe.value, (slot ? slot : header) + have,
                     DWORD(need - have), &n, &read)) {
          received(n);
          ResetEvent(read.hEvent);
          progress = true;
        } else if (GetLastError() == ERROR_IO_PENDING)
          reading = true;
        else
          failed = true;
      }
      if (!writing) {
        if (!has_output && !half_closed) {
          if (auto next = outgoing_.front(output_size)) {
            output = next;
            has_output = true;
          }
        }
        if (!has_output && !half_closed && drain_.load()) {
          output = &eof;
          output_size = 0;
          has_output = true;
          eof_write = true;
          half_closed = true;
        }
        if (has_output) {
          ResetEvent(write.hEvent);
          DWORD n = 0;
          if (WriteFile(pipe.value, output, DWORD(output_size), &n, &write)) {
            ResetEvent(write.hEvent);
            progress = true;
            if (n != output_size)
              failed = true;
            if (initial_write)
              initial_write = false;
            else if (!eof_write)
              outgoing_.pop();
            has_output = false;
          } else if (GetLastError() == ERROR_IO_PENDING)
            writing = true;
          else
            failed = true;
        }
      }
      if (failed)
        break;
      if (progress)
        continue;
      // Completion events remain signaled until the next operation starts.
      HANDLE events[] = {stop_, wake_, read.hEvent, write.hEvent};
      DWORD which = WaitForMultipleObjects(4, events, FALSE, 50);
      if (which == WAIT_OBJECT_0)
        break;
      if (reading && WaitForSingleObject(read.hEvent, 0) == WAIT_OBJECT_0) {
        DWORD n = 0;
        reading = false;
        ResetEvent(read.hEvent);
        if (!GetOverlappedResult(pipe.value, &read, &n, FALSE))
          failed = true;
        else
          received(n);
      }
      if (writing && WaitForSingleObject(write.hEvent, 0) == WAIT_OBJECT_0) {
        DWORD n = 0;
        writing = false;
        ResetEvent(write.hEvent);
        if (!GetOverlappedResult(pipe.value, &write, &n, FALSE) ||
            n != output_size)
          failed = true;
        if (initial_write)
          initial_write = false;
        else if (!eof_write)
          outgoing_.pop();
        has_output = false;
      }
    }
    online_.store(false);
    CancelIoEx(pipe.value, nullptr);
    // OVERLAPPED and buffers stay alive until cancellation has completed.
    DWORD ignored;
    if (reading)
      GetOverlappedResult(pipe.value, &read, &ignored, TRUE);
    if (writing)
      GetOverlappedResult(pipe.value, &write, &ignored, TRUE);
    // A local frame tells the apartment the connection ended.
    const char disconnected[5] = {1, 0, 0, 0, char(kDisconnected)};
    if (auto p = incoming_.reserve(sizeof disconnected)) {
      std::memcpy(p, disconnected, sizeof disconnected);
      incoming_.publish(sizeof disconnected);
      arrived();
    }
    if (GetTickCount64() - opened >= 2000)
      backoff = 20;
    WaitForSingleObject(stop_, backoff);
    backoff = backoff < 1000 ? backoff * 2 : 2000;
  }
}
} // namespace shixin
