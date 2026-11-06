#pragma once
// Scratch short-list head weights. Structural validation is not authentication;
// a builder-verified manifest must bind the exact table and sidecar bytes.
#include <algorithm>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <limits>
#include <vector>

namespace shixin_spike {
static_assert(sizeof(float) == 4 && std::numeric_limits<float>::is_iec559);
struct HeadWeight {
  uint32_t offset;
  float weight;
};
struct HeadWeightIdentity {
  uint32_t checksum, entries;
  uint64_t bytes;
};
inline bool HeadRead32(std::istream& in, uint32_t* value) {
  unsigned char b[4];
  if (!in.read(reinterpret_cast<char*>(b), 4)) return false;
  *value = uint32_t(b[0]) | uint32_t(b[1]) << 8 | uint32_t(b[2]) << 16 | uint32_t(b[3]) << 24;
  return true;
}
inline bool HeadWrite32(std::ostream& out, uint32_t value) {
  const char b[] = {char(value), char(value >> 8), char(value >> 16), char(value >> 24)};
  return bool(out.write(b, 4));
}
inline bool ValidHeadWeights(const std::vector<HeadWeight>& weights, uint64_t bytes) {
  if (weights.empty() || weights.size() > 1000000 || bytes < 8) return false;
  uint32_t previous = 0;
  for (const auto& entry : weights) {
    if (entry.offset <= previous || entry.offset % 4 || entry.offset > bytes - 8 ||
        !std::isfinite(entry.weight)) return false;
    previous = entry.offset;
  }
  return true;
}
inline bool SaveHeadWeights(const std::filesystem::path& path, HeadWeightIdentity id,
                            const std::vector<HeadWeight>& weights) {
  if (std::filesystem::exists(path) || weights.size() > id.entries ||
      !ValidHeadWeights(weights, id.bytes)) return false;
  std::ofstream out(path, std::ios::binary);
  out.write("SXHEAD01", 8);
  for (uint32_t x : {id.checksum, id.entries, uint32_t(id.bytes), uint32_t(id.bytes >> 32),
                     uint32_t(weights.size()), uint32_t(0)})
    if (!HeadWrite32(out, x)) return false;
  for (const auto& entry : weights) {
    uint32_t bits;
    static_assert(sizeof(bits) == sizeof(entry.weight));
    std::memcpy(&bits, &entry.weight, 4);
    if (!HeadWrite32(out, entry.offset) || !HeadWrite32(out, bits)) return false;
  }
  out.close();
  return bool(out);
}
inline bool LoadHeadWeights(const std::filesystem::path& path, HeadWeightIdentity id,
                            std::vector<HeadWeight>* result) {
  if (!result) return false;
  std::error_code ec;
  const auto size = std::filesystem::file_size(path, ec);
  if (ec || size < 32 || size > 8000032) return false;
  std::ifstream in(path, std::ios::binary);
  char magic[8];
  uint32_t fields[6];
  if (!in.read(magic, 8) || std::memcmp(magic, "SXHEAD01", 8)) return false;
  for (auto& x : fields) if (!HeadRead32(in, &x)) return false;
  const uint64_t bytes = uint64_t(fields[2]) | uint64_t(fields[3]) << 32;
  if (fields[0] != id.checksum || fields[1] != id.entries || bytes != id.bytes ||
      fields[5] || fields[4] > id.entries || fields[4] > 1000000 ||
      size != 32 + uint64_t(fields[4]) * 8) return false;
  std::vector<HeadWeight> weights(fields[4]);
  for (auto& entry : weights) {
    uint32_t bits;
    if (!HeadRead32(in, &entry.offset) || !HeadRead32(in, &bits)) return false;
    std::memcpy(&entry.weight, &bits, 4);
  }
  if (in.peek() != std::char_traits<char>::eof() || !ValidHeadWeights(weights, bytes)) return false;
  *result = std::move(weights);
  return true;
}
inline bool FindHeadWeight(const std::vector<HeadWeight>& weights, uint32_t offset, float* value) {
  if (!value) return false;
  auto it = std::lower_bound(weights.begin(), weights.end(), offset,
                            [](const HeadWeight& entry, uint32_t key) { return entry.offset < key; });
  if (it == weights.end() || it->offset != offset) return false;
  *value = it->weight;
  return true;
}
} // namespace shixin_spike
