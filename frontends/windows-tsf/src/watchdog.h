// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <cstdint>
namespace shixin {
// The I/O worker checks this only when no ready pipe work remains. It does
// not wait on an app thread. Legitimate reranking holds are at most 16 ms.
struct ReplyWatchdog {
  static constexpr uint64_t kTimeoutMS = 100;
  uint32_t ctx = 0, sent = 0, received = 0;
  uint64_t progress = 0;
  void request(uint32_t context, uint32_t seq, uint64_t now) {
    if (context != ctx) {
      ctx = context;
      sent = received = 0;
    }
    if (!seq || seq <= sent)
      return;
    if (sent == received)
      progress = now;
    sent = seq;
  }
  void result(uint32_t context, uint32_t seq, uint64_t now) {
    if (context != ctx || !seq || seq <= received || seq > sent)
      return;
    received = seq;
    progress = now;
  }
  bool expired(uint64_t now) const {
    return sent > received && now - progress >= kTimeoutMS;
  }
};
} // namespace shixin
