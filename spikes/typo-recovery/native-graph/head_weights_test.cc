#include <gtest/gtest.h>
#include <cstdlib>
#include <functional>
#include <iostream>
#include <rime/dict/table.h>
#include "head_weights.h"

class HeadWeightTable : public rime::Table {
 public:
  using Table::Table;
  using Table::index_;
  using rime::MappedFile::address;
};

TEST(HeadWeights, OfflineEveryShortListHeadMatchesNativeBits) {
  auto source = std::getenv("SHIXIN_SPIKE_TABLE");
  auto output = std::getenv("SHIXIN_SPIKE_HEAD_WEIGHTS");
  if (!source || !output) GTEST_SKIP();
  HeadWeightTable table{rime::path(std::string(source))};
  ASSERT_TRUE(table.Load());
  std::vector<shixin_spike::HeadWeight> expected;
  auto collect = [&](const rime::List<rime::table::Entry>& list) {
    if (!list.size) return;
    const auto offset = reinterpret_cast<const char*>(list.at.get()) - table.address();
    ASSERT_GT(offset, 0);
    ASSERT_LE(uint64_t(offset) + sizeof(rime::table::Entry), table.file_size());
    ASSERT_LE(uint64_t(offset), UINT32_MAX);
    expected.push_back({uint32_t(offset), list.at.get()->weight});
  };
  std::function<void(const rime::table::TrunkIndex*, int)> visit;
  visit = [&](const auto* index, int depth) {
    for (size_t i = 0; i < index->size; ++i) {
      collect(index->at[i].entries);
      if (depth < 3 && index->at[i].next_level) visit(&index->at[i].next_level->trunk(), depth + 1);
    }
  };
  for (size_t i = 0; i < table.index_->size; ++i) {
    collect(table.index_->at[i].entries);
    if (table.index_->at[i].next_level) visit(&table.index_->at[i].next_level->trunk(), 2);
  }
  std::sort(expected.begin(), expected.end(), [](const auto& a, const auto& b) { return a.offset < b.offset; });
  const shixin_spike::HeadWeightIdentity id{table.dict_file_checksum(), table.metadata()->num_entries, table.file_size()};
  ASSERT_TRUE(shixin_spike::ValidHeadWeights(expected, id.bytes));
  if (!std::filesystem::exists(output)) ASSERT_TRUE(shixin_spike::SaveHeadWeights(output, id, expected));
  std::vector<shixin_spike::HeadWeight> loaded;
  ASSERT_TRUE(shixin_spike::LoadHeadWeights(output, id, &loaded));
  ASSERT_EQ(loaded.size(), expected.size());
  for (size_t i = 0; i < loaded.size(); ++i) {
    ASSERT_EQ(loaded[i].offset, expected[i].offset);
    ASSERT_EQ(std::memcmp(&loaded[i].weight, &expected[i].weight, 4), 0);
    float found = 0;
    ASSERT_TRUE(shixin_spike::FindHeadWeight(loaded, expected[i].offset, &found));
    ASSERT_EQ(std::memcmp(&found, &expected[i].weight, 4), 0);
  }
  std::cout << "short_list_heads=" << loaded.size() << " payload_bytes=" << loaded.size()*sizeof(loaded[0])
            << " file_bytes=" << std::filesystem::file_size(output) << "\n";
}

TEST(HeadWeights, RejectInvalidRecordsAndMissingOffsets) {
  EXPECT_FALSE(shixin_spike::ValidHeadWeights({}, 100));
  EXPECT_FALSE(shixin_spike::ValidHeadWeights({{0, 1}}, 100));
  EXPECT_FALSE(shixin_spike::ValidHeadWeights({{5, 1}}, 100));
  EXPECT_FALSE(shixin_spike::ValidHeadWeights({{96, 1}}, 100));
  EXPECT_FALSE(shixin_spike::ValidHeadWeights({{4, INFINITY}}, 100));
  EXPECT_FALSE(shixin_spike::ValidHeadWeights({{4, NAN}}, 100));
  EXPECT_FALSE(shixin_spike::ValidHeadWeights({{8, 1},{4, 2}}, 100));
  EXPECT_FALSE(shixin_spike::ValidHeadWeights({{4, 1},{4, 2}}, 100));
  const std::vector<shixin_spike::HeadWeight> values{{4, -1},{12, 2}};
  EXPECT_TRUE(shixin_spike::ValidHeadWeights(values, 100));
  float result = 99;
  EXPECT_FALSE(shixin_spike::FindHeadWeight(values, 8, &result));
  EXPECT_EQ(result, 99);
  EXPECT_FALSE(shixin_spike::FindHeadWeight(values, 4, nullptr));
}
