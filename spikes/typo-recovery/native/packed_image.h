#pragma once
#include "abbreviation_search.h"
#include <cstring>
#include <fstream>

namespace abbr {
// Experiment-only, little/native-endian POD image. The marker rejects an
// incompatible byte order; this is not a product generation/IPC format.
static_assert(sizeof(Word) == 16 && sizeof(PackedBranch) == 16,
              "image record sizes");
static_assert(std::numeric_limits<float>::is_iec559, "IEEE float image");
template <class T> bool writeArray(std::ostream &out, const std::vector<T> &v) {
  return bool(out.write(reinterpret_cast<const char *>(v.data()),
                        v.size() * sizeof(T)));
}
template <class T>
bool readArray(std::istream &in, std::vector<T> &v, size_t n) {
  v.resize(n);
  return bool(in.read(reinterpret_cast<char *>(v.data()), n * sizeof(T)));
}
inline bool savePackedImage(const Search &s, const char *path) {
  if (!s.packed_logical_nodes)
    return false;
  std::ofstream out(path, std::ios::binary);
  if (!out)
    return false;
  const char magic[8] = {'S', 'X', 'A', 'B', 'R', '0', '0', '1'};
  out.write(magic, 8);
  uint32_t header[] = {0x01020304,
                       s.packed_logical_nodes,
                       static_cast<uint32_t>(s.packed_leaves.size()),
                       static_cast<uint32_t>(s.packed_branches.size()),
                       static_cast<uint32_t>(s.packed_children.size()),
                       static_cast<uint32_t>(s.words.size()),
                       static_cast<uint32_t>(s.syllables.size()),
                       static_cast<uint32_t>(s.text_blob.size()),
                       static_cast<uint32_t>(s.code_blob.size())};
  out.write(reinterpret_cast<const char *>(header), sizeof(header));
  if (!writeArray(out, s.packed_leaf_bits.bits) ||
      !writeArray(out, s.packed_leaves) ||
      !writeArray(out, s.packed_branches) ||
      !writeArray(out, s.packed_children) ||
      !writeArray(out, s.packed_syllables) || !writeArray(out, s.words))
    return false;
  for (const auto &syllable : s.syllables) {
    uint8_t length = static_cast<uint8_t>(syllable.size());
    out.put(static_cast<char>(length));
    out.write(syllable.data(), length);
  }
  out.write(s.text_blob.data(), s.text_blob.size());
  out.write(s.code_blob.data(), s.code_blob.size());
  out.flush();
  return bool(out);
}
inline bool loadPackedImage(Search &s, const char *path) {
  // Bounds before allocation, then graph/index validation before any query.
  if (s.packed_logical_nodes || !s.nodes.empty() || !s.words.empty())
    return false;
  std::ifstream in(path, std::ios::binary | std::ios::ate);
  if (!in)
    return false;
  auto size = in.tellg();
  if (size < 44 || size > 128 * 1024 * 1024)
    return false;
  in.seekg(0);
  char magic[8];
  uint32_t h[9];
  in.read(magic, 8);
  in.read(reinterpret_cast<char *>(h), sizeof(h));
  if (!in || std::memcmp(magic, "SXABR001", 8) || h[0] != 0x01020304)
    return false;
  uint64_t n = h[1], leaves = h[2], branches = h[3], edges = h[4], words = h[5],
           syllables = h[6], texts = h[7], codes = h[8];
  if (!n || n > 4000000 || leaves + branches != n || edges != n - 1 ||
      words > 4000000 || syllables > 1024 || texts > 128 * 1024 * 1024 ||
      codes > 128 * 1024 * 1024)
    return false;
  uint64_t blocks = (n + 63) / 64;
  uint64_t minimum = 44 + blocks * 8 + leaves * 4 + branches * 16 + edges * 6 +
                     words * 16 + syllables * 2 + texts + codes;
  if (minimum > static_cast<uint64_t>(size) ||
      minimum + syllables * 7 < static_cast<uint64_t>(size))
    return false;
  s.packed_logical_nodes = static_cast<uint32_t>(n);
  if (!readArray(in, s.packed_leaf_bits.bits, blocks) ||
      !readArray(in, s.packed_leaves, leaves) ||
      !readArray(in, s.packed_branches, branches) ||
      !readArray(in, s.packed_children, edges) ||
      !readArray(in, s.packed_syllables, edges) ||
      !readArray(in, s.words, words))
    return false;
  if (n % 64 && (s.packed_leaf_bits.bits.back() >> (n % 64)))
    return false;
  s.packed_leaf_bits.freeze();
  if (s.packed_leaf_bits.before(static_cast<uint32_t>(n)) != leaves)
    return false;
  for (size_t i = 0; i < syllables; i++) {
    int length = in.get();
    if (length < 1 || length > 8)
      return false;
    std::string value(length, ' ');
    in.read(value.data(), length);
    if (!in || !typo::Search::valid(value))
      return false;
    s.initial_mask |= uint32_t(1) << (value[0] - 'a');
    s.syllables.push_back(std::move(value));
  }
  s.text_blob.resize(texts);
  s.code_blob.resize(codes);
  in.read(s.text_blob.data(), texts);
  in.read(s.code_blob.data(), codes);
  if (!in || in.peek() != std::char_traits<char>::eof())
    return false;
  for (unsigned char c : s.text_blob)
    if (c < 32 || c == '"' || c == '\\')
      return false;
  for (char c : s.code_blob)
    if (c < 'a' || c > 'z')
      return false;
  for (const auto &w : s.words)
    if (!w.text_len || !w.code_len || w.code_len > 32 ||
        uint64_t(w.text) + w.text_len > texts ||
        uint64_t(w.code) + w.code_len > codes || !std::isfinite(w.prior) ||
        w.prior < 0)
      return false;
  const auto validWords = [&](uint32_t packed, bool leaf) {
    uint32_t count = packed >> 29, at = packed & ((uint32_t(1) << 29) - 1);
    return count <= 4 && (!leaf || count) && uint64_t(at) + count <= words;
  };
  for (auto leaf : s.packed_leaves)
    if (!validWords(leaf, true))
      return false;
  for (auto b : s.packed_branches) {
    uint32_t count = b.edge_and_count >> 23,
             at = b.edge_and_count & ((uint32_t(1) << 23) - 1);
    if (b.reserved || !validWords(b.word_and_count, false) ||
        uint64_t(at) + count > edges || !std::isfinite(b.descendant_bound) ||
        b.descendant_bound < 0 || b.max_tail > 32 ||
        (b.min_tail > 32 && b.min_tail != 255))
      return false;
  }
  std::vector<uint8_t> incoming(n);
  for (uint32_t id = 0; id < n; id++) {
    auto node = s.nodeAt(id);
    std::string_view prev;
    for (int j = 0; j < node.edges; j++) {
      auto e = s.edgeAt(node, j);
      if (e.child <= id || e.child >= n || e.syllable >= syllables ||
          incoming[e.child]++)
        return false;
      const auto &syllable = s.syllables[e.syllable];
      if (j && prev >= syllable)
        return false;
      prev = syllable;
    }
  }
  for (uint32_t id = 1; id < n; id++)
    if (incoming[id] != 1)
      return false;
  for (uint32_t id = static_cast<uint32_t>(n); id-- > 0;) {
    auto node = s.nodeAt(id);
    int min = node.words ? 0 : 255, max = 0;
    float descendant = 0;
    for (int j = 0; j < node.edges; j++) {
      auto edge = s.edgeAt(node, j);
      auto child = s.nodeAt(edge.child);
      min = std::min(min, int(child.min_tail) + 1);
      max = std::max(max,
                     int(child.max_tail + s.syllables[edge.syllable].size()));
      descendant = std::max(descendant, child.bound);
    }
    if (node.min_tail != min || node.max_tail != max ||
        node.descendant_bound != descendant)
      return false;
  }
  return true;
}
} // namespace abbr
