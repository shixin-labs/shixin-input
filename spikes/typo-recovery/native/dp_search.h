#pragma once
#include "abbreviation_search.h"

namespace abbr {
// Each dictionary node has one parent. Carry all legal input alignments in one
// entry rather than enqueueing the same node independently for every alignment.
// Bits are raw byte positions, including the end position (maximum 32).
struct DPState {
  uint32_t node = 0;
  float bound = 0;
  uint64_t clean = 0, swap = 0, neighbor = 0, pending = 0;
};
struct DPWorkspace {
  std::array<DPState, 1024> heap{};
  size_t size = 0;
};
inline bool dpHigher(const DPState &a, const DPState &b) {
  return a.bound != b.bound ? a.bound > b.bound : a.node < b.node;
}
inline unsigned dpFirst(uint64_t bits) {
  assert(bits);
  unsigned pos = 0;
  while (!(bits & 1)) {
    bits >>= 1;
    pos++;
  }
  return pos;
}
inline Result queryDP(const Search &index, std::string_view raw, DPWorkspace &w,
                      int clean_limit = 4, int state_cap = 2048,
                      int work_cap = 8192) {
  assert(clean_limit >= 0 && clean_limit <= 8);
  assert(index.radix_removed.bits.empty());
  Result r;
  r.clean_limit = clean_limit;
  w.size = 0;
  if (!typo::Search::valid(raw) ||
      (index.nodes.empty() && !index.packed_logical_nodes))
    return r;
  const auto charge = [&]() {
    if (r.edges == work_cap) {
      r.exhausted = true;
      return false;
    }
    r.edges++;
    return true;
  };
  const auto push = [&](DPState s) {
    const auto n = index.nodeAt(s.node);
    auto trim = [&](uint64_t bits, bool pending) {
      uint64_t kept = 0;
      while (bits) {
        auto p = dpFirst(bits);
        bits &= bits - 1;
        int left = int(raw.size() - p) + int(pending);
        if (left >= n.min_tail && left <= n.max_tail)
          kept |= uint64_t(1) << p;
      }
      return kept;
    };
    s.clean = trim(s.clean, false);
    s.swap = trim(s.swap, false);
    s.neighbor = trim(s.neighbor, false);
    s.pending = trim(s.pending, true);
    if (!(s.clean | s.swap | s.neighbor | s.pending))
      return;
    float prior = n.bound;
    s.bound = .3f * prior - (s.clean ? 0 : (s.swap || s.pending ? 1.6f : 2.f));
    bool clean_done = !s.clean || !r.clean_limit ||
                      (r.clean == r.clean_limit &&
                       .3f * prior < index.score(r.hits[r.clean_limit - 1]));
    bool typo_done = r.clean_limit == 8 || (r.corrected == 8 - r.clean_limit &&
                                            s.bound < index.score(r.hits[7]));
    if (clean_done && typo_done) {
      r.pruned_bound++;
      return;
    }
    if (w.size == w.heap.size()) {
      r.exhausted = true;
      return;
    }
    size_t i = w.size++;
    while (i) {
      size_t parent = (i - 1) / 2;
      if (!dpHigher(s, w.heap[parent]))
        break;
      w.heap[i] = w.heap[parent];
      i = parent;
    }
    w.heap[i] = s;
    r.peak_queue = std::max(r.peak_queue, int(w.size));
  };
  push({0, 0, 1, 0, 0, 0});
  while (w.size && !r.exhausted) {
    if (r.states == state_cap) {
      r.exhausted = true;
      break;
    }
    DPState s = w.heap[0], last = w.heap[--w.size];
    if (w.size) {
      size_t i = 0;
      while (2 * i + 1 < w.size) {
        size_t child = 2 * i + 1;
        if (child + 1 < w.size && dpHigher(w.heap[child + 1], w.heap[child]))
          child++;
        if (!dpHigher(w.heap[child], last))
          break;
        w.heap[i] = w.heap[child];
        i = child;
      }
      w.heap[i] = last;
    }
    r.states++;
    const auto n = index.nodeAt(s.node);
    const uint64_t end = uint64_t(1) << raw.size();
    for (int i = 0; i < n.words; i++) {
      if (s.clean & end)
        index.insert(r, {n.word + uint32_t(i), 0});
      if (s.swap & end)
        index.insert(r, {n.word + uint32_t(i), 8});
      if (s.neighbor & end)
        index.insert(r, {n.word + uint32_t(i), 10});
    }
    s.clean &= ~end;
    s.swap &= ~end;
    s.neighbor &= ~end;
    uint32_t allowed = 0;
    for (uint64_t bits = s.clean | s.swap | s.neighbor; bits;
         bits &= bits - 1) {
      unsigned p = dpFirst(bits);
      allowed |= uint32_t(1) << (raw[p] - 'a');
      if (s.clean & (uint64_t(1) << p)) {
        for (char c = 'a'; c <= 'z'; c++)
          if (index.adjacent(raw[p], c))
            allowed |= uint32_t(1) << (c - 'a');
        if (p + 1 < raw.size())
          allowed |= uint32_t(1) << (raw[p + 1] - 'a');
      }
    }
    for (uint64_t bits = s.pending; bits; bits &= bits - 1) {
      unsigned p = dpFirst(bits);
      assert(p >= 2);
      allowed |= uint32_t(1) << (raw[p - 2] - 'a');
    }
    allowed &= index.initial_mask;
    for (char first = 'a'; first <= 'z' && !r.exhausted; first++) {
      if (!(allowed & (uint32_t(1) << (first - 'a'))))
        continue;
      int lo = 0, hi = n.edges;
      while (lo < hi) {
        if (!charge())
          break;
        int mid = (lo + hi) / 2;
        if (index.syllables[index.edgeAt(n, mid).syllable][0] < first)
          lo = mid + 1;
        else
          hi = mid;
      }
      for (int edge = lo; edge < n.edges && !r.exhausted; edge++) {
        if (!charge())
          break;
        auto e = index.edgeAt(n, edge);
        std::string_view syl = index.syllables[e.syllable];
        if (syl[0] != first)
          break;
        DPState next;
        next.node = e.child;
        std::array<size_t, 3> lengths{syl.size(), 1, 2};
        int forms = (syl.size() > 2 &&
                     (syl.substr(0, 2) == "zh" || syl.substr(0, 2) == "ch" ||
                      syl.substr(0, 2) == "sh"))
                        ? 3
                        : 2;
        if (syl.size() == 1)
          forms = 1;
        for (uint64_t bits = s.clean | s.swap | s.neighbor | s.pending;
             bits && !r.exhausted; bits &= bits - 1) {
          if (!charge())
            break;
          unsigned p = dpFirst(bits);
          uint64_t bit = uint64_t(1) << p;
          for (int form_index = 0; form_index < forms; form_index++) {
            size_t len = lengths[form_index];
            auto form = syl.substr(0, len);
            if ((s.pending & bit) && form[0] == raw[p - 2] &&
                p + len - 1 <= raw.size() &&
                form.substr(1) == raw.substr(p, len - 1))
              next.swap |= uint64_t(1) << (p + len - 1);
            if (p + len > raw.size())
              continue;
            auto part = raw.substr(p, len);
            int begin = -1, last = -1, mismatches = 0;
            for (size_t j = 0; j < len; j++)
              if (part[j] != form[j]) {
                if (begin < 0)
                  begin = int(j);
                last = int(j);
                mismatches++;
              }
            uint64_t dest = uint64_t(1) << (p + len);
            if (!mismatches) {
              if (s.clean & bit)
                next.clean |= dest;
              if (s.swap & bit)
                next.swap |= dest;
              if (s.neighbor & bit)
                next.neighbor |= dest;
            } else if (s.clean & bit) {
              if (mismatches == 1 && index.adjacent(part[begin], form[begin]))
                next.neighbor |= dest;
              if (mismatches == 2 && last == begin + 1 &&
                  form[begin] == part[last] && form[last] == part[begin])
                next.swap |= dest;
            }
            if ((s.clean & bit) && p + len < raw.size() &&
                raw[p + len - 1] != raw[p + len] &&
                form[len - 1] == raw[p + len] &&
                form.substr(0, len - 1) == part.substr(0, len - 1))
              next.pending |= uint64_t(1) << (p + len + 1);
          }
        }
        if (!r.exhausted)
          push(next);
      }
    }
  }
  if (r.exhausted) {
    r.clean = 0;
    r.corrected = 0;
  }
  return r;
}
static_assert(sizeof(DPWorkspace) < 256 * 1024, "workspace budget");
} // namespace abbr
