// Offline auxiliary-table construction and fresh-process loading only.
#include <gtest/gtest.h>
#include <array>
#include <chrono>
#include <cmath>
#include <cstdlib>
#include <iostream>
#include <rime/dict/table.h>
class CompactInspectTable : public rime::Table {
public:
  using Table::Table;
  rime::table::Index *index() { return index_; }
  uint32_t Offset(const rime::table::Entry *e) const {
    return uint32_t(reinterpret_cast<const char *>(e) - address());
  }
  uint64_t Warm() const {
    volatile uint64_t sum = 0;
    for (size_t i = 0; i < file_size(); i += 4096)
      sum += uint8_t(address()[i]);
    return sum;
  }
};
TEST(CompactTable, BuildAndVerifyCanonicalReadings) {
  const char *source_path = std::getenv("SHIXIN_SPIKE_TABLE");
  const char *output = std::getenv("SHIXIN_SPIKE_COMPACT_TABLE");
  if (!source_path || !output || std::getenv("SHIXIN_SPIKE_COMPACT_ONLY"))
    GTEST_SKIP();
  CompactInspectTable source{rime::path(std::string(source_path))};
  ASSERT_TRUE(source.Load());
  auto root = source.index();
  rime::Syllabary syllabary;
  for (size_t i = 0; i < root->size; ++i)
    syllabary.insert(source.GetSyllableById(i));
  ASSERT_EQ(syllabary.size(), root->size);
  rime::Vocabulary vocabulary;
  size_t count = 0, groups = 0;
  auto collect = [&](const rime::List<rime::table::Entry> &entries,
                     const rime::Code &code) {
    std::array<const rime::table::Entry *, 8> best{};
    size_t n = 0;
    for (size_t i = 0; i < entries.size; ++i) {
      const auto *e = &entries.at[i];
      ASSERT_TRUE(std::isfinite(e->weight));
      size_t at = 0;
      while (at < n && (best[at]->weight > e->weight ||
                        (best[at]->weight == e->weight &&
                         source.Offset(best[at]) < source.Offset(e))))
        ++at;
      if (at == best.size())
        continue;
      if (n < best.size())
        ++n;
      for (size_t j = n - 1; j > at; --j)
        best[j] = best[j - 1];
      best[at] = e;
    }
    if (!n)
      return;
    ++groups;
    auto list = vocabulary.LocateEntries(code);
    ASSERT_TRUE(list);
    for (size_t i = 0; i < n; ++i) {
      auto entry = rime::New<rime::ShortDictEntry>();
      entry->code = code;
      entry->text = source.GetEntryText(*best[i]);
      entry->weight = best[i]->weight;
      list->push_back(entry);
      ++count;
    }
  };
  for (size_t a = 0; a < root->size; ++a) {
    rime::Code code;
    code.push_back(a);
    collect(root->at[a].entries, code);
    auto next = root->at[a].next_level.get();
    if (!next)
      continue;
    auto two = &next->trunk();
    for (size_t b = 0; b < two->size; ++b) {
      code.resize(1);
      code.push_back(two->at[b].key);
      collect(two->at[b].entries, code);
      auto more = two->at[b].next_level.get();
      if (!more)
        continue;
      auto three = &more->trunk();
      for (size_t c = 0; c < three->size; ++c) {
        code.resize(2);
        code.push_back(three->at[c].key);
        collect(three->at[c].entries, code);
      }
    }
  }
  CompactInspectTable compact{rime::path(std::string(output))};
  if (!std::filesystem::exists(rime::path(std::string(output)))) {
    ASSERT_TRUE(compact.Build(syllabary, vocabulary, count));
    ASSERT_TRUE(compact.Save());
    compact.Close();
  }
  ASSERT_TRUE(compact.Load());
  ASSERT_EQ(compact.metadata()->num_entries, count);
  ASSERT_EQ(compact.metadata()->num_syllables, root->size);
  for (size_t i = 0; i < root->size; ++i)
    ASSERT_EQ(compact.GetSyllableById(i), source.GetSyllableById(i));
  size_t checked = 0;
  auto verify = [&](const rime::Code &code) {
    auto expected = vocabulary.LocateEntries(code);
    if (expected->empty())
      return;
    auto actual = compact.QueryPhrases(code);
    for (const auto &e : *expected) {
      ASSERT_FALSE(actual.exhausted());
      EXPECT_EQ(compact.GetEntryText(*actual.entry()), e->text);
      EXPECT_EQ(actual.entry()->weight, float(e->weight));
      actual.Next();
      ++checked;
    }
    EXPECT_TRUE(actual.exhausted());
  };
  for (size_t a = 0; a < root->size; ++a) {
    rime::Code code;
    code.push_back(a);
    verify(code);
    auto next = root->at[a].next_level.get();
    if (!next)
      continue;
    auto two = &next->trunk();
    for (size_t b = 0; b < two->size; ++b) {
      code.resize(1);
      code.push_back(two->at[b].key);
      verify(code);
      auto more = two->at[b].next_level.get();
      if (!more)
        continue;
      auto three = &more->trunk();
      for (size_t c = 0; c < three->size; ++c) {
        code.resize(2);
        code.push_back(three->at[c].key);
        verify(code);
      }
    }
  }
  EXPECT_EQ(checked, count);
  std::cout << "COMPACT_TABLE groups=" << groups << " records=" << count
            << " verified=" << checked << " bytes=" << compact.file_size()
            << " string_table_bytes=" << compact.metadata()->string_table_size
            << '\n';
}
TEST(CompactTable, FreshLoadAndWarm) {
  const char *output = std::getenv("SHIXIN_SPIKE_COMPACT_TABLE");
  if (!output || !std::getenv("SHIXIN_SPIKE_COMPACT_ONLY"))
    GTEST_SKIP();
  auto began = std::chrono::steady_clock::now();
  CompactInspectTable compact{rime::path(std::string(output))};
  ASSERT_TRUE(compact.Load());
  auto checksum = compact.Warm();
  auto warmed = std::chrono::steady_clock::now();
  std::vector<std::string> syllables;
  for (size_t i = 0; i < compact.metadata()->num_syllables; ++i)
    syllables.push_back(compact.GetSyllableById(i));
  for (const auto &reading : std::vector<std::vector<std::string>>{
           {"ni", "hao"}, {"diao", "deng"}, {"chun", "jing", "wu"}}) {
    rime::Code code;
    for (const auto &syllable : reading) {
      auto it = std::find(syllables.begin(), syllables.end(), syllable);
      ASSERT_NE(it, syllables.end());
      code.push_back(it - syllables.begin());
    }
    auto start = std::chrono::steady_clock::now();
    auto result = compact.QueryPhrases(code);
    ASSERT_FALSE(result.exhausted());
    auto text = compact.GetEntryText(*result.entry());
    auto elapsed = std::chrono::steady_clock::now() - start;
    EXPECT_EQ(text, reading[0] == "ni"     ? "你好"
                    : reading[0] == "diao" ? "吊灯"
                                           : "纯净物");
    std::cout
        << "FRESH_COMPACT text=" << text << " query_ns="
        << std::chrono::duration_cast<std::chrono::nanoseconds>(elapsed).count()
        << '\n';
  }
  std::cout << "FRESH_COMPACT bytes=" << compact.file_size() << " load_warm_us="
            << std::chrono::duration_cast<std::chrono::microseconds>(warmed -
                                                                     began)
                   .count()
            << " warm_checksum=" << checksum << '\n';
}
