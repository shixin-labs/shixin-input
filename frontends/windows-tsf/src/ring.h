// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <array>
#include <atomic>
#include <cstddef>
namespace shixin {
// One producer and one consumer. A slot remains owned by its consumer
// until pop(), so overlapped I/O may retain a pointer without copying it.
template <class T, std::size_t N> class Ring {
  std::array<T, N> items_{};
  alignas(64) std::atomic<std::size_t> read_{0};
  alignas(64) std::atomic<std::size_t> write_{0};

public:
  T *reserve() {
    auto w = write_.load(std::memory_order_relaxed);
    if (w - read_.load(std::memory_order_acquire) == N)
      return nullptr;
    return &items_[w % N];
  }
  void publish() { write_.fetch_add(1, std::memory_order_release); }
  T *front() {
    auto r = read_.load(std::memory_order_relaxed);
    if (r == write_.load(std::memory_order_acquire))
      return nullptr;
    return &items_[r % N];
  }
  void pop() { read_.fetch_add(1, std::memory_order_release); }
};
} // namespace shixin
