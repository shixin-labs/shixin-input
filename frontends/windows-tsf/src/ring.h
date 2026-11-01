// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <array>
#include <atomic>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <memory>
#include <new>
namespace shixin {
// One producer and one consumer. A slot remains owned by its consumer
// until pop(), so overlapped I/O may retain a pointer without copying it.
#ifdef _MSC_VER
// C4324: the alignas below pads the ring on purpose, putting the reader's and
// writer's counters on separate cache lines.
#pragma warning(push)
#pragma warning(disable : 4324)
#endif
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
// Variable-length frames in one power-of-two byte buffer, allocated once at
// construction. One producer and one consumer, with Ring's ownership rule:
// a frame stays valid until pop(), so overlapped I/O can use it in place.
// Frames are contiguous; one that would straddle the end of the buffer
// starts at its beginning instead, after a skip marker the consumer steps
// over. A frame of up to max_frame() bytes therefore always fits an empty
// ring, and nothing else is ever stored per frame but a 4-byte length.
class FrameRing {
  static constexpr size_t kHeader = 4;
  static constexpr uint32_t kSkip = UINT32_MAX;
  std::unique_ptr<char[]> data_;
  size_t mask_ = 0;
  alignas(64) std::atomic<size_t> read_{0};
  alignas(64) std::atomic<size_t> write_{0};
  size_t base_ = 0; // producer: where the reserved frame's header sits
  static size_t span(size_t n) { return kHeader + ((n + 3) & ~size_t(3)); }
  uint32_t length(size_t pos) const {
    uint32_t n;
    std::memcpy(&n, &data_[pos & mask_], sizeof n);
    return n;
  }

public:
  // capacity must be a power of two of at least 16. If the allocation fails
  // the ring is unusable: ok() is false and reserve() returns nullptr.
  explicit FrameRing(size_t capacity)
      : data_(new (std::nothrow) char[capacity]), mask_(capacity - 1) {
    if (!data_)
      mask_ = 0;
  }
  bool ok() const { return data_ != nullptr; }
  // The largest frame guaranteed to fit when the ring is empty.
  size_t max_frame() const { return data_ ? (mask_ + 1) / 2 - kHeader : 0; }
  // Producer: n contiguous bytes, or nullptr if there is no room now. The
  // frame belongs to the consumer only after publish().
  char *reserve(size_t n) {
    if (n == 0 || n > max_frame())
      return nullptr;
    auto w = write_.load(std::memory_order_relaxed);
    auto r = read_.load(std::memory_order_acquire);
    size_t capacity = mask_ + 1, off = w & mask_, tail = capacity - off;
    size_t skip = tail < span(n) ? tail : 0;
    if (capacity - (w - r) < skip + span(n))
      return nullptr;
    if (skip) {
      uint32_t marker = kSkip;
      std::memcpy(&data_[off], &marker, sizeof marker);
    }
    base_ = w + skip;
    return &data_[(base_ & mask_) + kHeader];
  }
  // Producer: makes the first n bytes of the reserved frame visible.
  void publish(size_t n) {
    auto header = static_cast<uint32_t>(n);
    std::memcpy(&data_[base_ & mask_], &header, sizeof header);
    write_.store(base_ + span(n), std::memory_order_release);
  }
  // Consumer: the oldest frame, or nullptr if there is none.
  const char *front(size_t &n) {
    auto r = read_.load(std::memory_order_relaxed);
    if (r == write_.load(std::memory_order_acquire))
      return nullptr;
    if (length(r) == kSkip) {
      r += mask_ + 1 - (r & mask_);
      read_.store(r, std::memory_order_release);
    }
    n = length(r);
    return &data_[(r & mask_) + kHeader];
  }
  // Consumer: releases the frame last returned by front().
  void pop() {
    auto r = read_.load(std::memory_order_relaxed);
    read_.store(r + span(length(r)), std::memory_order_release);
  }
  bool empty() const {
    return read_.load(std::memory_order_acquire) ==
           write_.load(std::memory_order_acquire);
  }
};
#ifdef _MSC_VER
#pragma warning(pop)
#endif
} // namespace shixin
