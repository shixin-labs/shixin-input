// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include <array>
#include <cstddef>
#include <cstdint>
#include <cstring>
#include <string_view>

namespace shixin {
constexpr uint32_t kMaxFrame = 1 << 20;
// The largest frame the DLL builds, queues or accepts: what a 128 KiB
// FrameRing (ring.h) is guaranteed to hold. The protocol allows more
// (kMaxFrame); the DLL treats a longer frame as a protocol error and
// reconnects.
constexpr size_t kFrameCapacity = (64 << 10) - 4;
constexpr uint16_t kVersion = 1;
enum Type : uint8_t {
  Hello = 1,
  Key = 2,
  FocusIn = 3,
  FocusOut = 4,
  Select = 5,
  ChangePage = 6,
  Clear = 7,
  SetOption = 8,
  StatsRequest = 9,
  CaretType = 10,
  Welcome = 64,
  ResultType = 65,
  PolicyType = 66,
  Unavailable = 67,
  Migrate = 68,
  Stats = 69
};
constexpr uint32_t kShift = 1, kLock = 2, kControl = 4, kAlt = 8,
                   kSuper = 1u << 26, kRelease = 1u << 30;
constexpr uint32_t kCommand = kControl | kAlt | kSuper;
constexpr uint16_t kHandled = 1, kComposing = 2, kAutoSpace = 1 << 7;

struct Reader {
  std::string_view bytes;
  bool good = true;
  explicit Reader(std::string_view b) : bytes(b) {}
  std::string_view take(size_t n) {
    if (!good || n > bytes.size()) {
      good = false;
      return {};
    }
    auto s = bytes.substr(0, n);
    bytes.remove_prefix(n);
    return s;
  }
  uint8_t u8() {
    auto b = take(1);
    return b.empty() ? 0 : static_cast<uint8_t>(b[0]);
  }
  uint16_t u16() {
    auto a = u8();
    return static_cast<uint16_t>(a | uint16_t(u8()) << 8);
  }
  uint32_t u32() {
    auto a = u16();
    return a | uint32_t(u16()) << 16;
  }
  std::string_view string() {
    auto n = u32();
    return take(n);
  }
};

// A whole frame: its u32 length, type byte and payload.
inline uint8_t frame_type(std::string_view frame) {
  return frame.size() >= 5 ? uint8_t(frame[4]) : 0;
}
inline std::string_view frame_payload(std::string_view frame) {
  return frame.size() >= 5 ? frame.substr(5) : std::string_view{};
}
struct Frame {
  std::array<char, kFrameCapacity> data{};
  size_t size = 0;
  bool good = true;
  void raw(std::string_view s) {
    if (!good || s.size() > data.size() - size) {
      good = false;
      return;
    }
    if (!s.empty())
      std::memcpy(data.data() + size, s.data(), s.size());
    size += s.size();
  }
  void u8(uint8_t n) {
    char b = static_cast<char>(n);
    raw({&b, 1});
  }
  void u16(uint16_t n) {
    u8(uint8_t(n));
    u8(uint8_t(n >> 8));
  }
  void u32(uint32_t n) {
    u16(uint16_t(n));
    u16(uint16_t(n >> 16));
  }
  void string(std::string_view s) {
    u32(static_cast<uint32_t>(s.size()));
    raw(s);
  }
  void begin(uint8_t type) {
    size = 0;
    good = true;
    u32(0);
    u8(type);
  }
  bool end() {
    if (!good || size < 5)
      return false;
    auto n = static_cast<uint32_t>(size - 4);
    for (size_t i = 0; i < 4; ++i)
      data[i] = char(n >> (8 * i));
    return true;
  }
  void copy(const Frame &other) {
    size = other.size;
    good = other.good;
    if (size <= data.size())
      std::memcpy(data.data(), other.data.data(), size);
    else {
      good = false;
      size = 0;
    }
  }
  std::string_view view() const { return {data.data(), size}; }
  std::string_view payload() const { return frame_payload(view()); }
  uint8_t type() const { return frame_type(view()); }
};

inline bool frame_size(std::string_view header, uint32_t &size) {
  Reader r(header);
  size = r.u32();
  return r.good && size >= 1 && size <= kMaxFrame;
}
inline void hello(Frame &b, uint8_t kind, uint32_t pid, std::string_view app) {
  b.begin(Hello);
  b.u16(kVersion);
  b.u8(kind);
  b.u32(pid);
  b.string(app);
  b.end();
}
inline void key(Frame &b, uint32_t ctx, uint32_t seq, uint32_t sym,
                uint32_t mods, uint8_t flags) {
  b.begin(Key);
  b.u32(ctx);
  b.u32(seq);
  b.u32(sym);
  b.u32(mods);
  b.u8(flags);
  b.end();
}
inline void focus_in(Frame &b, uint32_t ctx, bool secure,
                     std::string_view app) {
  b.begin(FocusIn);
  b.u32(ctx);
  b.u8(secure ? 1 : 0);
  b.string(app);
  b.end();
}
inline void focus_out(Frame &b, uint32_t ctx) {
  b.begin(FocusOut);
  b.u32(ctx);
  b.end();
}
inline void select(Frame &b, uint32_t ctx, uint32_t seq, uint32_t index) {
  b.begin(Select);
  b.u32(ctx);
  b.u32(seq);
  b.u32(index);
  b.end();
}
inline void caret(Frame &b, uint32_t ctx, int32_t left, int32_t top,
                  int32_t right, int32_t bottom) {
  b.begin(CaretType);
  b.u32(ctx);
  b.u32(uint32_t(left));
  b.u32(uint32_t(top));
  b.u32(uint32_t(right));
  b.u32(uint32_t(bottom));
  b.end();
}
struct Candidate {
  std::string_view text, comment;
};
struct Result {
  uint32_t ctx = 0, seq = 0, cursor = 0, start = 0, end = 0;
  uint16_t flags = 0, policy = 0, page = 0, highlight = 0, count = 0;
  std::string_view commit, preedit, labels;
  std::array<Candidate, 128> candidates{};
  bool decode(std::string_view b) {
    Reader r(b);
    ctx = r.u32();
    seq = r.u32();
    flags = r.u16();
    policy = r.u16();
    commit = r.string();
    preedit = r.string();
    cursor = r.u32();
    start = r.u32();
    end = r.u32();
    page = r.u16();
    highlight = r.u16();
    count = r.u16();
    if (count > candidates.size())
      return false;
    for (size_t i = 0; i < count; ++i) {
      candidates[i].text = r.string();
      candidates[i].comment = r.string();
    }
    labels = r.string();
    return r.good && cursor <= preedit.size() && start <= end &&
           end <= preedit.size();
  }
};
inline void display(Frame &b, const Result &r) {
  b.begin(ResultType);
  b.u32(r.ctx);
  b.u32(r.seq);
  b.u16(r.flags);
  b.u16(r.policy);
  b.string({});
  b.string(r.preedit);
  b.u32(r.cursor);
  b.u32(r.start);
  b.u32(r.end);
  b.u16(r.page);
  b.u16(r.highlight);
  b.u16(r.count);
  for (size_t i = 0; i < r.count; ++i) {
    b.string(r.candidates[i].text);
    b.string(r.candidates[i].comment);
  }
  b.string(r.labels);
  b.end();
}
} // namespace shixin
