#pragma once
#include "search.h"
#include <cassert>
#include <limits>
#include <unordered_map>

namespace abbr {
// Compact syllable edges; no materialized combinations of abbreviation masks.
struct Node {
  uint32_t edge = 0, word = 0;
  uint16_t edges = 0, words = 0;
  float bound = 0;
  float descendant_bound = 0;
  uint8_t min_tail = 255, max_tail = 0;
};
struct Edge {
  uint32_t child = 0;
  uint16_t syllable = 0;
};
struct Word {
  uint32_t text = 0, code = 0;
  uint16_t text_len = 0, code_len = 0;
  float prior = 0;
};
struct State {
  uint32_t node = 0;
  uint8_t pos = 0, cost = 0;
  int8_t pending = -1;
  float bound = 0;
};
struct Hit {
  uint32_t word = 0;
  uint8_t cost = 0;
};
struct Result {
  std::array<Hit, 8> hits{};
  int clean_limit = 4;
  int clean = 0, corrected = 0, states = 0, edges = 0, peak_queue = 0;
  bool exhausted = false;
  int pruned_bound = 0, pruned_prefix = 0, lookahead_checks = 0;
};
struct Workspace {
  std::array<State, 1024> heap{};
  std::array<uint64_t, 4096> seen{};
  int size = 0;
  int length = 0;
  std::string_view raw;
  int edge_cap = 8192;
  bool constrained = false;
};
// A radix label is a sequence of syllables. Original logical IDs are retained
// solely to keep heap ties, deduplication and operation caps comparable.
struct RadixChain {
  uint32_t endpoint = 0, start = 0, original = 0;
  uint8_t count = 0;
};
struct RankedBits {
  std::vector<uint64_t> bits;
  std::vector<uint32_t> ranks;
  static unsigned popcount(uint64_t x) {
    x -= (x >> 1) & 0x5555555555555555ull;
    x = (x & 0x3333333333333333ull) + ((x >> 2) & 0x3333333333333333ull);
    x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0full;
    return unsigned((x * 0x0101010101010101ull) >> 56);
  }
  void resize(size_t n) { bits.resize((n + 63) / 64); }
  void set(uint32_t n) { bits[n / 64] |= uint64_t(1) << (n % 64); }
  bool get(uint32_t n) const { return (bits[n / 64] >> (n % 64)) & 1; }
  void freeze() {
    ranks.resize(bits.size() + 1);
    for (size_t i = 0; i < bits.size(); i++)
      ranks[i + 1] = ranks[i] + popcount(bits[i]);
  }
  uint32_t before(uint32_t n) const {
    uint32_t block = n / 64, offset = n % 64;
    return ranks[block] +
           (offset ? popcount(bits[block] & ((uint64_t(1) << offset) - 1)) : 0);
  }
  size_t bytes() const {
    return bits.capacity() * sizeof(uint64_t) +
           ranks.capacity() * sizeof(uint32_t);
  }
};
// Leaf nodes have no edge/bound/tail fields. Internal nodes derive their total
// bound from the unchanged terminal prior and descendant bound.
struct PackedBranch {
  uint32_t edge_and_count = 0, word_and_count = 0;
  float descendant_bound = 0;
  uint8_t min_tail = 255, max_tail = 0;
  uint16_t reserved = 0;
};
static_assert(sizeof(PackedBranch) == 16, "packed branch size");
class Search {
  struct Build {
    std::vector<Edge> children;
    std::vector<uint32_t> words;
    uint32_t code = 0;
    uint16_t code_len = 0;
  };
  std::vector<Build> build_{1};
  std::unordered_map<std::string, uint16_t> ids_;
  typo::Search
      keyboard_; // Only its fixed adjacency table and empty root are used.
public:
  std::vector<Node> nodes;
  std::vector<Edge> edges;
  std::vector<Word> words;
  std::vector<std::string> syllables;
  std::string text_blob, code_blob;
  RankedBits radix_removed, radix_starts;
  std::vector<uint16_t> radix_labels;
  std::vector<RadixChain> radix_chains;
  static constexpr uint32_t radix_flag = uint32_t(1) << 31;
  RankedBits packed_leaf_bits;
  std::vector<uint32_t> packed_leaves, packed_children;
  std::vector<uint16_t> packed_syllables;
  std::vector<PackedBranch> packed_branches;
  uint32_t packed_logical_nodes = 0;
  uint32_t initial_mask = 0;

  void compactPayloads() {
    // Keep word IDs/order/priors unchanged. Pool repeated Chinese text and copy
    // only live strings. Canonical codes are already shared by spelling.
    // Sort an ID permutation rather than building two large hash tables.
    std::vector<uint32_t> order(words.size());
    for (uint32_t i = 0; i < words.size(); i++)
      order[i] = i;
    std::sort(order.begin(), order.end(),
              [&](uint32_t a, uint32_t b) { return text(a) < text(b); });
    std::string compact_text, compact_code;
    std::string_view previous;
    size_t text_size = 0;
    for (auto id : order) {
      auto value = text(id);
      if (value != previous) {
        text_size += value.size();
        previous = value;
      }
    }
    compact_text.reserve(text_size);
    previous = {};
    uint32_t offset = 0;
    for (auto id : order) {
      auto value = text(id);
      if (value != previous) {
        offset = static_cast<uint32_t>(compact_text.size());
        compact_text.append(value);
        previous = value;
      }
      words[id].text = offset;
    }
    std::sort(order.begin(), order.end(), [&](uint32_t a, uint32_t b) {
      return words[a].code < words[b].code;
    });
    uint32_t old_offset = std::numeric_limits<uint32_t>::max();
    size_t code_size = 0;
    for (auto id : order)
      if (words[id].code != old_offset) {
        old_offset = words[id].code;
        code_size += words[id].code_len;
      }
    compact_code.reserve(code_size);
    old_offset = std::numeric_limits<uint32_t>::max();
    for (auto id : order) {
      auto &word = words[id];
      if (word.code != old_offset) {
        old_offset = word.code;
        offset = static_cast<uint32_t>(compact_code.size());
        compact_code.append(code_blob.data() + word.code, word.code_len);
      }
      word.code = offset;
    }
    text_blob.swap(compact_text);
    code_blob.swap(compact_code);
    words.shrink_to_fit();
  }
  void packNodes() {
    assert(radix_removed.bits.empty() && packed_leaf_bits.bits.empty());
    packed_logical_nodes = static_cast<uint32_t>(nodes.size());
    packed_leaf_bits.resize(nodes.size());
    size_t leaves = 0;
    for (uint32_t i = 0; i < nodes.size(); i++)
      if (!nodes[i].edges && nodes[i].words) {
        packed_leaf_bits.set(i);
        leaves++;
      }
    packed_leaf_bits.freeze();
    packed_leaves.reserve(leaves);
    packed_branches.reserve(nodes.size() - leaves);
    for (uint32_t i = 0; i < nodes.size(); i++) {
      const auto &n = nodes[i];
      assert(n.word < (uint32_t(1) << 29) && n.words < 8);
      uint32_t word = n.word | (uint32_t(n.words) << 29);
      if (packed_leaf_bits.get(i)) {
        packed_leaves.push_back(word);
        continue;
      }
      assert(n.edge < (uint32_t(1) << 23) && n.edges < 512);
      packed_branches.push_back({n.edge | (uint32_t(n.edges) << 23), word,
                                 n.descendant_bound, n.min_tail, n.max_tail,
                                 0});
    }
    packed_children.reserve(edges.size());
    packed_syllables.reserve(edges.size());
    for (auto e : edges) {
      packed_children.push_back(e.child);
      packed_syllables.push_back(e.syllable);
    }
    std::vector<Node>().swap(nodes);
    std::vector<Edge>().swap(edges);
  }

  // Compression is initialization-only. Terminals and branching nodes remain
  // explicit; nonterminal one-child chains become multi-syllable labels.
  void compressRadix() {
    assert(radix_removed.bits.empty());
    const auto compressible = [&](uint32_t id) {
      return id && !nodes[id].words && nodes[id].edges == 1;
    };
    size_t kept = 0, removed = 0, kept_edges = 0, chains = 0;
    radix_removed.resize(nodes.size());
    radix_starts.resize(nodes.size());
    for (uint32_t i = 0; i < nodes.size(); i++) {
      if (compressible(i)) {
        removed++;
        radix_removed.set(i);
        continue;
      }
      kept++;
      kept_edges += nodes[i].edges;
      for (int j = 0; j < nodes[i].edges; j++)
        if (compressible(edges[nodes[i].edge + j].child))
          chains++;
    }
    assert(chains < (radix_flag >> 5));
    radix_removed.freeze();
    radix_chains.reserve(chains);
    radix_labels.reserve(removed);
    std::vector<Node> packed;
    packed.reserve(kept);
    std::vector<Edge> packed_edges;
    packed_edges.reserve(kept_edges);
    for (uint32_t i = 0; i < nodes.size(); i++)
      if (!compressible(i)) {
        Node node = nodes[i];
        node.edge = static_cast<uint32_t>(packed_edges.size());
        for (int j = 0; j < nodes[i].edges; j++)
          packed_edges.push_back(edges[nodes[i].edge + j]);
        packed.push_back(node);
      }
    for (uint32_t i = 0; i < nodes.size(); i++)
      if (!compressible(i))
        for (int j = 0; j < nodes[i].edges; j++) {
          uint32_t next = edges[nodes[i].edge + j].child;
          if (!compressible(next))
            continue;
          RadixChain chain;
          chain.original = next;
          radix_starts.set(next);
          chain.start = static_cast<uint32_t>(radix_labels.size());
          while (compressible(next)) {
            assert(chain.count < 32);
            auto edge = edges[nodes[next].edge];
            // The builder allocates this nonterminal's sole child immediately
            // after it; preserving this invariant avoids a per-label ID array.
            assert(edge.child == next + 1);
            radix_labels.push_back(edge.syllable);
            chain.count++;
            next = edge.child;
          }
          chain.endpoint = next - radix_removed.before(next);
          radix_chains.push_back(chain);
        }
    std::sort(radix_chains.begin(), radix_chains.end(),
              [](const RadixChain &a, const RadixChain &b) {
                return a.original < b.original;
              });
    radix_starts.freeze();
    nodes.swap(packed);
    edges.swap(packed_edges);
  }
  Node nodeAt(uint32_t original) const {
    if (!packed_leaf_bits.bits.empty()) {
      Node n;
      uint32_t rank = packed_leaf_bits.before(original), word;
      if (packed_leaf_bits.get(original)) {
        word = packed_leaves[rank];
        n.min_tail = 0;
      } else {
        const auto &b = packed_branches[original - rank];
        word = b.word_and_count;
        n.edge = b.edge_and_count & ((uint32_t(1) << 23) - 1);
        n.edges = b.edge_and_count >> 23;
        n.descendant_bound = b.descendant_bound;
        n.min_tail = b.min_tail;
        n.max_tail = b.max_tail;
      }
      n.word = word & ((uint32_t(1) << 29) - 1);
      n.words = word >> 29;
      n.bound =
          std::max(n.descendant_bound, n.words ? words[n.word].prior : 0.f);
      return n;
    }
    if (radix_removed.bits.empty())
      return nodes[original];
    if (!radix_removed.get(original))
      return nodes[original - radix_removed.before(original)];
    uint32_t chain_id = radix_starts.before(original + 1) - 1;
    const auto &chain = radix_chains[chain_id];
    unsigned offset = original - chain.original;
    const auto &endpoint = nodes[chain.endpoint];
    Node n;
    n.edge = radix_flag | (chain_id << 5) | offset;
    n.edges = 1;
    n.bound = n.descendant_bound = endpoint.bound;
    n.min_tail = endpoint.min_tail + chain.count - offset;
    unsigned remaining = endpoint.max_tail;
    for (unsigned i = offset; i < chain.count; i++)
      remaining += syllables[radix_labels[chain.start + i]].size();
    n.max_tail = static_cast<uint8_t>(remaining);
    return n;
  }
  Edge edgeAt(const Node &node, int index) const {
    if (!packed_leaf_bits.bits.empty())
      return {packed_children[node.edge + index],
              packed_syllables[node.edge + index]};
    if (!radix_removed.bits.empty() && (node.edge & radix_flag)) {
      assert(index == 0);
      const auto &chain = radix_chains[(node.edge & ~radix_flag) >> 5];
      unsigned at = chain.start + (node.edge & 31);
      return {chain.original + (node.edge & 31) + 1, radix_labels[at]};
    }
    return edges[node.edge + index];
  }
  bool add(const std::vector<std::string> &parts, const std::string &text,
           double weight) {
    if (parts.empty() || text.empty() || text.size() > 65535 ||
        !std::isfinite(weight) || weight < 0)
      return false;
    std::string code;
    for (const auto &p : parts) {
      if (!typo::Search::valid(p) || p.size() > 8)
        return false;
      code += p;
    }
    if (!typo::Search::valid(code))
      return false;
    uint32_t n = 0;
    for (const auto &p : parts) {
      auto it = ids_.find(p);
      uint16_t sid;
      if (it == ids_.end()) {
        if (syllables.size() >= 65535)
          return false;
        sid = static_cast<uint16_t>(syllables.size());
        ids_[p] = sid;
        syllables.push_back(p);
        initial_mask |= uint32_t(1) << (p[0] - 'a');
      } else
        sid = it->second;
      uint32_t child = 0;
      for (auto e : build_[n].children)
        if (e.syllable == sid) {
          child = e.child;
          break;
        }
      if (!child) {
        child = static_cast<uint32_t>(build_.size());
        build_[n].children.push_back({child, sid});
        build_.emplace_back();
      }
      n = child;
    }
    if (build_[n].words.empty()) {
      build_[n].code = static_cast<uint32_t>(code_blob.size());
      build_[n].code_len = static_cast<uint16_t>(code.size());
      code_blob += code;
    }
    uint32_t id = static_cast<uint32_t>(words.size());
    words.push_back({static_cast<uint32_t>(text_blob.size()), build_[n].code,
                     static_cast<uint16_t>(text.size()), build_[n].code_len,
                     static_cast<float>(std::log1p(weight))});
    text_blob += text;
    build_[n].words.push_back(id);
    return true;
  }
  std::string_view text(uint32_t w) const {
    return {text_blob.data() + words[w].text, words[w].text_len};
  }
  std::string_view code(uint32_t w) const {
    return {code_blob.data() + words[w].code, words[w].code_len};
  }
  bool adjacent(char a, char b) const {
    return keyboard_.adjacent[a - 'a'][b - 'a'];
  }
  void freeze() {
    nodes.resize(build_.size());
    std::vector<Word> sorted;
    sorted.reserve(words.size());
    edges.reserve(build_.size() - 1);
    for (size_t i = 0; i < build_.size(); i++) {
      auto &b = build_[i];
      auto &n = nodes[i];
      n.edge = static_cast<uint32_t>(edges.size());
      n.edges = static_cast<uint16_t>(b.children.size());
      std::sort(b.children.begin(), b.children.end(), [&](Edge a, Edge z) {
        return syllables[a.syllable] < syllables[z.syllable];
      });
      edges.insert(edges.end(), b.children.begin(), b.children.end());
      std::sort(b.words.begin(), b.words.end(), [&](uint32_t a, uint32_t z) {
        if (words[a].prior != words[z].prior)
          return words[a].prior > words[z].prior;
        return text(a) < text(z);
      });
      n.word = static_cast<uint32_t>(sorted.size());
      n.words = static_cast<uint16_t>(std::min<size_t>(4, b.words.size()));
      // Only four entries from a single spelling can enter a four-slot
      // category.
      for (int j = 0; j < n.words; j++)
        sorted.push_back(words[b.words[j]]);
      if (n.words)
        n.bound = sorted[n.word].prior;
    }
    words.swap(sorted);
    for (size_t i = nodes.size(); i-- > 0;) {
      if (nodes[i].words)
        nodes[i].min_tail = 0;
      for (int j = 0; j < nodes[i].edges; j++) {
        nodes[i].bound = std::max(nodes[i].bound,
                                  nodes[edges[nodes[i].edge + j].child].bound);
        auto e = edges[nodes[i].edge + j];
        const auto &child = nodes[e.child];
        nodes[i].descendant_bound =
            std::max(nodes[i].descendant_bound, child.bound);
        nodes[i].min_tail =
            std::min<int>(nodes[i].min_tail, child.min_tail + 1);
        nodes[i].max_tail = std::max<int>(
            nodes[i].max_tail, child.max_tail + syllables[e.syllable].size());
      }
    }
    std::vector<Build>().swap(build_);
    std::unordered_map<std::string, uint16_t>().swap(ids_);
  }
  static bool higher(const State &a, const State &b) {
    if (a.bound != b.bound)
      return a.bound > b.bound;
    if (a.node != b.node)
      return a.node < b.node;
    if (a.pos != b.pos)
      return a.pos < b.pos;
    if (a.cost != b.cost)
      return a.cost < b.cost;
    return a.pending < b.pending;
  }
  float score(Hit h) const { return .3f * words[h.word].prior - .2f * h.cost; }
  bool better(Hit a, Hit b) const {
    if (score(a) != score(b))
      return score(a) > score(b);
    if (text(a.word) != text(b.word))
      return text(a.word) < text(b.word);
    return code(a.word) < code(b.word);
  }
  void insert(Result &r, Hit h) const {
    int start = h.cost ? r.clean_limit : 0;
    int limit = h.cost ? 8 - r.clean_limit : r.clean_limit;
    if (!limit)
      return;
    int &count = h.cost ? r.corrected : r.clean;
    for (int i = 0; i < count; i++)
      if (text(r.hits[start + i].word) == text(h.word)) {
        if (!better(h, r.hits[start + i]))
          return;
        for (int j = i; j + 1 < count; j++)
          r.hits[start + j] = r.hits[start + j + 1];
        count--;
        break;
      }
    int i = 0;
    while (i < count && !better(h, r.hits[start + i]))
      i++;
    if (i == limit)
      return;
    for (int j = std::min(count, limit - 1); j > i; j--)
      r.hits[start + j] = r.hits[start + j - 1];
    r.hits[start + i] = h;
    count = std::min(limit, count + 1);
  }
  bool push(Workspace &w, Result &r, State s) const {
    int remaining = w.length - s.pos + (s.pending >= 0 ? 1 : 0);
    const auto node = nodeAt(s.node);
    if (remaining < node.min_tail || remaining > node.max_tail)
      return true;
    float bound = remaining ? node.descendant_bound : words[node.word].prior;
    s.bound = .3f * bound - .2f * s.cost;
    if (w.constrained) {
      bool clean_done =
          s.cost ||
          (!r.clean_limit || (r.clean == r.clean_limit &&
                              s.bound < score(r.hits[r.clean_limit - 1])));
      bool typo_done =
          (!remaining && !s.cost) ||
          (r.clean_limit == 8 ||
           (r.corrected == 8 - r.clean_limit && s.bound < score(r.hits[7])));
      if (clean_done && typo_done) {
        r.pruned_bound++;
        return true;
      }
      if (remaining) {
        bool possible = false;
        uint32_t allowed_mask = 0;
        for (char c = 'a'; c <= 'z'; c++) {
          bool allowed =
              s.pending >= 0
                  ? c == s.pending
                  : (c == w.raw[s.pos] ||
                     (!s.cost &&
                      (adjacent(w.raw[s.pos], c) ||
                       (s.pos + 1 < w.raw.size() && c == w.raw[s.pos + 1]))));
          if (allowed)
            allowed_mask |= uint32_t(1) << (c - 'a');
        }
        allowed_mask &= initial_mask;
        if (node.edges <= 8) {
          for (int i = 0; allowed_mask && i < node.edges && !possible; i++) {
            if (r.edges == w.edge_cap) {
              r.exhausted = true;
              return false;
            }
            r.edges++;
            r.lookahead_checks++;
            possible =
                allowed_mask &
                (uint32_t(1) << (syllables[edgeAt(node, i).syllable][0] - 'a'));
          }
        } else {
          for (char initial = 'a'; initial <= 'z' && !possible; initial++) {
            if (!(allowed_mask & (uint32_t(1) << (initial - 'a'))))
              continue;
            int lo = 0, hi = node.edges;
            while (lo < hi) {
              if (r.edges == w.edge_cap) {
                r.exhausted = true;
                return false;
              }
              r.edges++;
              r.lookahead_checks++;
              int mid = (lo + hi) / 2;
              char c = syllables[edgeAt(node, mid).syllable][0];
              if (c == initial) {
                possible = true;
                break;
              }
              if (c < initial)
                lo = mid + 1;
              else
                hi = mid;
            }
          }
        }
        if (!possible) {
          r.pruned_prefix++;
          return true;
        }
      }
    }
    uint64_t key = (uint64_t(s.node) << 24) | (uint64_t(s.pos) << 16) |
                   (uint64_t(s.cost) << 8) | uint8_t(s.pending + 1);
    size_t slot = (key * 11400714819323198485ull) >> 52;
    for (size_t i = 0; i < w.seen.size();
         i++, slot = (slot + 1) % w.seen.size()) {
      if (w.seen[slot] == key)
        return true;
      if (w.seen[slot] == std::numeric_limits<uint64_t>::max()) {
        w.seen[slot] = key;
        break;
      }
      if (i + 1 == w.seen.size()) {
        r.exhausted = true;
        return false;
      }
    }
    if (w.size == static_cast<int>(w.heap.size())) {
      r.exhausted = true;
      return false;
    }
    int i = w.size++;
    while (i > 0) {
      int p = (i - 1) / 2;
      if (!higher(s, w.heap[p]))
        break;
      w.heap[i] = w.heap[p];
      i = p;
    }
    w.heap[i] = s;
    r.peak_queue = std::max(r.peak_queue, w.size);
    return true;
  }
  State pop(Workspace &w) const {
    State out = w.heap[0], last = w.heap[--w.size];
    if (w.size) {
      int i = 0;
      while (i * 2 + 1 < w.size) {
        int c = i * 2 + 1;
        if (c + 1 < w.size && higher(w.heap[c + 1], w.heap[c]))
          c++;
        if (!higher(w.heap[c], last))
          break;
        w.heap[i] = w.heap[c];
        i = c;
      }
      w.heap[i] = last;
    }
    return out;
  }
  Result query(std::string_view raw, Workspace &w, int state_cap = 2048,
               int edge_cap = 8192, bool constrained = false,
               int clean_limit = 4) const {
    Result r;
    assert(clean_limit >= 0 && clean_limit <= 8);
    r.clean_limit = clean_limit;
    w.size = 0;
    w.length = static_cast<int>(raw.size());
    w.raw = raw;
    w.edge_cap = edge_cap;
    w.constrained = constrained;
    w.seen.fill(std::numeric_limits<uint64_t>::max());
    if ((nodes.empty() && !packed_logical_nodes) || !typo::Search::valid(raw))
      return r;
    push(w, r, {});
    while (w.size && !r.exhausted) {
      if (r.states == state_cap) {
        r.exhausted = true;
        break;
      }
      auto s = pop(w);
      r.states++;
      // Bounds are conservative even for a not-yet-spent edit. Strict
      // comparison preserves deterministic lexical tie-breaking at equal
      // scores.
      bool clean_done =
          s.cost ||
          (!r.clean_limit || (r.clean == r.clean_limit &&
                              s.bound < score(r.hits[r.clean_limit - 1])));
      bool typo_done =
          r.clean_limit == 8 ||
          (r.corrected == 8 - r.clean_limit && s.bound < score(r.hits[7]));
      if (clean_done && typo_done)
        continue;
      const auto n = nodeAt(s.node);
      if (s.pos == raw.size() && s.pending < 0) {
        for (int i = 0; i < n.words; i++)
          insert(r, {n.word + uint32_t(i), s.cost});
        continue;
      }
      std::array<bool, 26> first{};
      if (s.pending >= 0)
        first[s.pending - 'a'] = true;
      else {
        first[raw[s.pos] - 'a'] = true;
        if (!s.cost) {
          for (char c = 'a'; c <= 'z'; c++)
            if (adjacent(raw[s.pos], c))
              first[c - 'a'] = true;
          if (s.pos + 1 < raw.size())
            first[raw[s.pos + 1] - 'a'] = true;
        }
      }
      for (char initial = 'a'; initial <= 'z' && !r.exhausted; initial++) {
        if (!first[initial - 'a'])
          continue;
        int lo = 0, hi = n.edges;
        while (lo < hi) {
          int mid = (lo + hi) / 2;
          if (syllables[edgeAt(n, mid).syllable][0] < initial)
            lo = mid + 1;
          else
            hi = mid;
        }
        for (int i = lo; i < n.edges && !r.exhausted; i++) {
          if (syllables[edgeAt(n, i).syllable][0] != initial)
            break;
          if (r.edges == edge_cap) {
            r.exhausted = true;
            break;
          }
          r.edges++;
          auto e = edgeAt(n, i);
          auto syl = std::string_view(syllables[e.syllable]);
          std::array<int, 3> lengths{static_cast<int>(syl.size()), 1, 2};
          int count = (syl.size() > 2 &&
                       (syl.substr(0, 2) == "zh" || syl.substr(0, 2) == "ch" ||
                        syl.substr(0, 2) == "sh"))
                          ? 3
                          : 2;
          if (syl.size() == 1)
            count = 1;
          for (int k = 0; k < count && !r.exhausted; k++) {
            int len = lengths[k];
            auto form = syl.substr(0, len);
            if (s.pending >= 0) {
              if (form[0] != s.pending || size_t(s.pos + len - 1) > raw.size())
                continue;
              if (form.substr(1) == raw.substr(s.pos, len - 1))
                push(w, r, {e.child, uint8_t(s.pos + len - 1), s.cost, -1, 0});
              continue;
            }
            if (size_t(s.pos + len) > raw.size())
              continue;
            auto part = raw.substr(s.pos, len);
            int first = -1, last = -1, mismatches = 0;
            for (int j = 0; j < len; j++)
              if (form[j] != part[j]) {
                if (first < 0)
                  first = j;
                last = j;
                mismatches++;
              }
            if (!mismatches)
              push(w, r, {e.child, uint8_t(s.pos + len), s.cost, -1, 0});
            else if (!s.cost) {
              if (mismatches == 1 && adjacent(part[first], form[first]))
                push(w, r, {e.child, uint8_t(s.pos + len), 10, -1, 0});
              if (mismatches == 2 && last == first + 1 &&
                  form[first] == part[last] && form[last] == part[first])
                push(w, r, {e.child, uint8_t(s.pos + len), 8, -1, 0});
            }
            // A swap may straddle two typed syllable forms. Carry the second
            // expected character; no second edit can occur while consuming it.
            if (!s.cost && size_t(s.pos + len) < raw.size() &&
                raw[s.pos + len - 1] != raw[s.pos + len] &&
                form[len - 1] == raw[s.pos + len] &&
                form.substr(0, len - 1) == part.substr(0, len - 1))
              push(w, r,
                   {e.child, uint8_t(s.pos + len + 1), 8,
                    int8_t(raw[s.pos + len - 1]), 0});
          }
        }
      }
    }
    if (r.exhausted) {
      r.clean = 0;
      r.corrected = 0;
    }
    return r;
  }
};
static_assert(sizeof(Workspace) < 256 * 1024, "workspace budget");
} // namespace abbr
