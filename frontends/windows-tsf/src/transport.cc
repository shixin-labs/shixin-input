// SPDX-License-Identifier: GPL-3.0-or-later
#include "transport.h"
#include <algorithm>
#include <bcrypt.h>
#include <new>
#include <sddl.h>
#include <shlobj.h>
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
      !ProcessIdToSessionId(pid, &actual) || actual != session)
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
    : window_(w), presentation_(presentation),
      directory_(std::move(directory)) {
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
  if (!stop_ || !wake_ || !arrived_)
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
  auto p = outgoing_.reserve();
  if (!p)
    return false;
  p->copy(frame);
  outgoing_.publish();
  SetEvent(wake_);
  return true;
}
void Transport::run() {
  auto sid = user_sid(GetCurrentProcess());
  DWORD session = 0;
  if (sid.empty() || !ProcessIdToSessionId(GetCurrentProcessId(), &session))
    return;
  PWSTR local = nullptr;
  if (FAILED(SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &local)))
    return;
  std::wstring runtime =
      std::wstring(local) + L"\\Shixin\\run-" + std::to_wstring(session);
  CoTaskMemFree(local);
  auto publish = [&](const Frame &f) {
    auto p = incoming_.reserve();
    if (!p)
      return false;
    p->copy(f);
    incoming_.publish();
    SetEvent(arrived_);
    PostMessageW(window_, kWake, 0, 0);
    return true;
  };
  // Reconnects back off while connections keep ending quickly (an engine
  // migration refusing new clients, a full or mismatched candidate host):
  // 20 ms doubling to 2 s, reset by a connection that lasted 2 s.
  DWORD backoff = 20;
  while (WaitForSingleObject(stop_, 0) != WAIT_OBJECT_0) {
    // Discard keys from the previous connection before opening a new one.
    while (outgoing_.front())
      outgoing_.pop();
    drop_.store(false);
    drain_.store(false);
    auto name =
        pipe_name(presentation_ ? runtime + L"\\ui.sock" : endpoint(runtime),
                  sid, session);
    Handle pipe(name.empty()
                    ? INVALID_HANDLE_VALUE
                    : CreateFileW(name.c_str(), GENERIC_READ | GENERIC_WRITE, 0,
                                  nullptr, OPEN_EXISTING,
                                  FILE_FLAG_OVERLAPPED | SECURITY_SQOS_PRESENT |
                                      SECURITY_IDENTIFICATION,
                                  nullptr));
    if (pipe.value == INVALID_HANDLE_VALUE ||
        !trusted(pipe.value, directory_, sid, session, presentation_)) {
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
    Frame input, initial, eof;
    hello(initial, 2, GetCurrentProcessId(), utf8(directory_));
    Frame *output = &initial;
    bool initial_write = true, reading = false, writing = false, failed = false,
         half_closed = false, eof_write = false;
    size_t have = 0, need = 4;
    auto received = [&](DWORD n) {
      if (!n) {
        failed = true;
        return;
      }
      have += n;
      if (have == 4 && need == 4) {
        uint32_t length = 0;
        if (!frame_size({input.data.data(), 4}, length) ||
            length + 4 > input.data.size()) {
          failed = true;
          return;
        }
        need = length + 4;
      }
      if (have == need) {
        input.size = have;
        if (!publish(input))
          failed = true;
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
      bool progress = false;
      if (!reading) {
        ResetEvent(read.hEvent);
        DWORD n = 0;
        if (ReadFile(pipe.value, input.data.data() + have, DWORD(need - have),
                     &n, &read)) {
          received(n);
          ResetEvent(read.hEvent);
          progress = true;
        } else if (GetLastError() == ERROR_IO_PENDING)
          reading = true;
        else
          failed = true;
      }
      if (!writing) {
        if (!output && !half_closed)
          output = outgoing_.front();
        if (!output && !half_closed && drain_.load()) {
          output = &eof;
          output->size = 0;
          eof_write = true;
          half_closed = true;
        }
        if (output) {
          ResetEvent(write.hEvent);
          DWORD n = 0;
          if (WriteFile(pipe.value, output->data.data(), DWORD(output->size),
                        &n, &write)) {
            ResetEvent(write.hEvent);
            progress = true;
            if (n != output->size)
              failed = true;
            if (initial_write)
              initial_write = false;
            else if (!eof_write)
              outgoing_.pop();
            output = nullptr;
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
            n != output->size)
          failed = true;
        if (initial_write)
          initial_write = false;
        else if (!eof_write)
          outgoing_.pop();
        output = nullptr;
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
    Frame disconnected;
    disconnected.begin(kDisconnected);
    disconnected.end();
    publish(disconnected);
    if (GetTickCount64() - opened >= 2000)
      backoff = 20;
    WaitForSingleObject(stop_, backoff);
    backoff = backoff < 1000 ? backoff * 2 : 2000;
  }
}
} // namespace shixin
