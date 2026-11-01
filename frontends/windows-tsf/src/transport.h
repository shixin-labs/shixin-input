// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "ring.h"
#include "wire.h"
#include <atomic>
#include <memory>
#include <string>
#include <windows.h>
namespace shixin {
constexpr UINT kWake = WM_APP + 0x31;
constexpr uint8_t kDisconnected = 250;
// Pipes are opened, verified, read and written only by this worker. All
// apartment-facing operations are bounded queue operations and SetEvent.
extern std::atomic<long> objects;
class Transport : public std::enable_shared_from_this<Transport> {
  HWND window_;
  bool presentation_;
  std::wstring directory_;
  HANDLE stop_ = nullptr, wake_ = nullptr, arrived_ = nullptr;
  std::atomic<bool> online_{false};
  std::atomic<bool> drop_{false}, drain_{false};
  Ring<Frame, 64> outgoing_, incoming_;
  void run();

public:
  Transport(HWND window, std::wstring directory, bool presentation = false);
  ~Transport();
  bool start();
  void stop();
  bool send(const Frame &frame);
  Frame *front() { return incoming_.front(); }
  void pop() { incoming_.pop(); }
  void migrate() {
    drain_.store(true);
    SetEvent(wake_);
  }
  void disconnect() {
    drop_.store(true);
    SetEvent(wake_);
  }
  bool online() const { return online_.load(); }
  HANDLE arrived() const { return arrived_; }
};
std::wstring module_directory(HMODULE module);
} // namespace shixin
