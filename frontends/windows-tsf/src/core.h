// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "policy.h"
namespace shixin {
// All calls are on the TSF apartment. The transport is a bounded SPSC queue;
// Host never opens pipes, allocates, or waits except in wait_until.
struct Host {
  virtual ~Host() = default;
  virtual bool send(uint32_t ctx, uint32_t seq, uint32_t sym, uint32_t mods,
                    uint8_t flags) = 0;
  virtual void pump() = 0;
  virtual uint64_t now() = 0; // monotonic nanoseconds
  virtual void wait_until(uint64_t deadline) = 0;
  virtual void degraded() = 0;
};
struct Completion {
  bool accept = false, notify = false;
  uint32_t repair = 0;
};
struct Core {
  static constexpr uint64_t kBudget = 8000000;
  std::array<Policy, 256> policies{};
  std::array<bool, 256> installed{};
  struct Pending {
    uint32_t seq = 0, sym = 0;
    bool notify = false, eaten = false;
  };
  std::array<Pending, 128> pending{};
  uint32_t ctx = 0, sent = 0, received = 0, applied = 0;
  uint64_t cycle = 0;
  uint16_t policy = 0;
  bool connected = false, secure = true, known = false, migrating = false;
  uint32_t answer_seq = 0;
  bool answer_handled = false;
  void reset() {
    ++cycle;
    migrating = false;
    connected = false;
    known = false;
    installed.fill(false);
    sent = received = applied = 0;
    answer_seq = 0;
  }
  void focus(uint32_t id, bool password) {
    ctx = id;
    secure = password;
    known = false;
    received = applied = sent;
  }
  bool install(std::string_view b) {
    // Decode into temporary storage; a malformed frame must not corrupt a
    // policy still in use. This stack object contains no owning strings.
    Policy p;
    if (!p.decode(b) || p.id >= policies.size() ||
        p.composing >= policies.size())
      return false;
    policies[p.id] = p;
    installed[p.id] = true;
    return true;
  }
  Completion result(const Result &r) {
    if (!connected || secure || r.ctx != ctx || r.seq > sent ||
        (r.seq && r.seq <= received) || r.policy >= installed.size() ||
        !installed[r.policy])
      return {};
    if (r.seq && pending[r.seq % pending.size()].seq != r.seq)
      return {};
    // Intermediate replies carry commits, but cannot replace the policy
    // predicted for later keys that are already in flight.
    if (r.seq == sent || (!r.seq && received == sent)) {
      policy = r.policy;
      known = true;
    }
    if (r.seq)
      received = r.seq;
    Completion out;
    out.accept = true;
    if (r.seq) {
      auto p = pending[r.seq % pending.size()];
      if (p.seq != r.seq)
        return {};
      out.notify = p.notify;
      if (p.eaten && !(r.flags & kHandled) && p.sym >= 0x20 && p.sym <= 0x7e)
        out.repair = p.sym;
      answer_seq = r.seq;
      answer_handled = (r.flags & kHandled) != 0;
    }
    return out;
  }
  void acknowledge(uint32_t seq) {
    if (seq > applied && seq <= received)
      applied = seq;
  }
  bool fail(Host &h) {
    connected = false;
    known = false;
    h.degraded();
    return false;
  }
  bool barrier(Host &h, uint64_t deadline, bool text) {
    auto context = ctx;
    auto connection = cycle;
    while (connected && (received < sent || (text && applied < sent))) {
      if (h.now() >= deadline)
        return fail(h);
      h.pump();
      if (ctx != context || cycle != connection || secure || migrating)
        return false;
      if (h.now() > deadline)
        return fail(h);
      if (received >= sent && (!text || applied >= sent))
        return true;
      if (h.now() >= deadline)
        return fail(h);
      h.wait_until(deadline);
    }
    return connected;
  }
  bool key(uint32_t sym, uint32_t mods, Host &h) {
    if (secure || !connected || migrating)
      return false;
    uint64_t deadline = h.now() + kBudget;
    if (!known && (!barrier(h, deadline, false) || !known))
      return false;
    auto a = policies[policy].lookup(sym, mods);
    auto disposition = Disposition(a & 3);
    bool modifier = sym >= 0xffe1 && sym <= 0xffe4;
    if (disposition == Pass || disposition == Notify) {
      if (!modifier && sent > applied) {
        if (!barrier(h, deadline, true))
          return false;
        a = policies[policy].lookup(sym, mods);
        disposition = Disposition(a & 3);
      }
    }
    if (disposition == Pass || disposition == Notify) {
      if (disposition == Pass &&
          !((mods & kShift && policies[policy].notify & 1) ||
            (mods & kControl && policies[policy].notify & 2)))
        return false;
      // Passed shortcuts while Shift is held cancel Rime's Shift tap.
      if (!modifier && (mods & kRelease))
        return false;
      disposition = Notify;
    }
    if (disposition == Ask && !barrier(h, deadline, true))
      return false;
    if (disposition == Eat && ((a >> 2) & 3) == 1 &&
        !installed[policies[policy].composing])
      return fail(h);
    if (sent - applied >= pending.size() - 1 || sent == UINT32_MAX)
      return fail(h);
    auto seq = sent + 1;
    auto flags = uint8_t(disposition == Ask      ? 1
                         : disposition == Notify ? 2
                                                 : 0);
    if (!h.send(ctx, seq, sym, mods, flags))
      return fail(h);
    sent = seq;
    pending[seq % pending.size()] = {seq, sym, disposition == Notify,
                                     disposition == Eat};
    if (disposition == Notify) {
      known = false;
      return false;
    }
    if (disposition == Ask) {
      if (!barrier(h, deadline, false) || answer_seq != seq)
        return false;
      return answer_handled;
    }
    switch ((a >> 2) & 3) {
    case 1:
      if (!installed[policies[policy].composing])
        return fail(h);
      policy = policies[policy].composing;
      break;
    case 2:
      known = false;
      break;
    default:
      break;
    }
    return true;
  }
};
} // namespace shixin
