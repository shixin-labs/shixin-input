#include <gtest/gtest.h>
#include <cstdlib>
#include <rime/algo/syllabifier.h>
#include <rime/dict/dictionary.h>
#include <rime/dict/prism.h>
#include <rime/dict/table.h>

TEST(SpikeCanonical, StringWarmingPreservesTableAndClosedState) {
  const char *source = std::getenv("SHIXIN_SPIKE_TABLE");
  if (!source)
    GTEST_SKIP();
  rime::Table table{rime::path(std::string(source))};
  EXPECT_FALSE(table.WarmSpikeStrings());
  EXPECT_FALSE(table.WarmSpikeIndexes());
  ASSERT_TRUE(table.Load());
  auto *metadata = table.metadata();
  const auto entries = metadata->num_entries;
  const auto size = table.file_size();
  const auto syllable = table.GetSyllableById(0);
  ASSERT_TRUE(table.WarmSpikeStrings());
  ASSERT_TRUE(table.WarmSpikeStrings());
  ASSERT_TRUE(table.WarmSpikeIndexes());
  ASSERT_TRUE(table.WarmSpikeIndexes());
  ASSERT_TRUE(table.WarmSpikeIndexes(true));
  EXPECT_EQ(table.metadata(), metadata);
  EXPECT_EQ(table.metadata()->num_entries, entries);
  EXPECT_EQ(table.file_size(), size);
  EXPECT_EQ(table.GetSyllableById(0), syllable);
  EXPECT_EQ(table.SpikeCanonicalTable(), nullptr);
  table.Close();
  EXPECT_FALSE(table.WarmSpikeStrings());
  EXPECT_FALSE(table.WarmSpikeIndexes());
}

TEST(SpikeCanonical, OwnerLifetimeAndMissingArtifact) {
  const char *source = std::getenv("SHIXIN_SPIKE_TABLE");
  const char *auxiliary = std::getenv("SHIXIN_SPIKE_COMPACT_TABLE");
  if (!source || !auxiliary)
    GTEST_SKIP();
  rime::Table original{rime::path(std::string(source))};
  ASSERT_TRUE(original.Load());
  ASSERT_TRUE(original.PrepareSpikeCanonicalTable(auxiliary));
  auto *owner = original.SpikeCanonicalTable();
  ASSERT_NE(owner, nullptr);
  rime::Code code;
  for (const auto *syllable : {"ni", "hao"}) {
    bool found = false;
    for (size_t i = 0; i < original.metadata()->num_syllables; ++i)
      if (original.GetSyllableById(i) == syllable) {
        code.push_back(i);
        found = true;
        break;
      }
    ASSERT_TRUE(found);
  }
  auto result = owner->QueryPhrases(code);
  ASSERT_FALSE(result.exhausted());
  auto *entry = result.entry();
  EXPECT_EQ(owner->GetEntryText(*entry), "你好");
  ASSERT_TRUE(original.PrepareSpikeCanonicalTable(auxiliary));
  EXPECT_EQ(owner, original.SpikeCanonicalTable());
  EXPECT_FALSE(
      original.PrepareSpikeCanonicalTable(std::string(auxiliary) + ".missing"));
  EXPECT_EQ(owner, original.SpikeCanonicalTable());
  EXPECT_EQ(entry, owner->QueryPhrases(code).entry());
  EXPECT_EQ(owner->GetEntryText(*entry), "你好");
  rime::Table missing{rime::path(std::string(source))};
  ASSERT_TRUE(missing.Load());
  EXPECT_FALSE(
      missing.PrepareSpikeCanonicalTable(std::string(auxiliary) + ".missing"));
  EXPECT_EQ(missing.SpikeCanonicalTable(), nullptr);
  EXPECT_FALSE(missing.PrepareSpikeCanonicalTable(auxiliary));
}

TEST(SpikeCanonical, DictionaryChunksDecodeWithAuxiliaryOwner) {
  const char *source = std::getenv("SHIXIN_SPIKE_TABLE");
  const char *auxiliary = std::getenv("SHIXIN_SPIKE_COMPACT_TABLE");
  const char *prism_file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!source || !auxiliary || !prism_file)
    GTEST_SKIP();
  auto table = rime::New<rime::Table>(rime::path(std::string(source)));
  auto prism = rime::New<rime::Prism>(rime::path(std::string(prism_file)));
  rime::Dictionary dictionary("rime_ice", {}, {table}, prism);
  ASSERT_TRUE(dictionary.Load());
  ASSERT_TRUE(table->PrepareSpikeCanonicalTable(auxiliary));
  rime::Syllabifier syllabifier("'", true, false);
  rime::SyllableGraph graph;
  syllabifier.BuildSyllableGraph("nihao", *prism, &graph);
  auto ordinary = dictionary.Lookup(graph, 0, nullptr, false);
  ASSERT_TRUE(ordinary);
  ASSERT_NE(ordinary->find(5), ordinary->end());
  ASSERT_TRUE(ordinary->at(5).Peek());
  EXPECT_EQ(ordinary->at(5).Peek()->text, "你好");
  graph.spike_bounded = true;
  graph.spike_graph_complete = true;
  graph.spike_work_left = 32768;
  graph.spike_nodes_left = 2048;
  graph.spike_chunks_left = 2048;
  graph.spike_deadline =
      std::chrono::steady_clock::now() + std::chrono::seconds(1);
  auto corrected = dictionary.Lookup(graph, 0, nullptr, false);
  ASSERT_TRUE(corrected);
  ASSERT_NE(corrected->find(5), corrected->end());
  auto &entries = corrected->at(5);
  size_t checked = 0;
  while (auto entry = entries.Peek()) {
    auto native = table->SpikeCanonicalTable()->QueryPhrases(entry->code);
    bool matched = false;
    while (!native.exhausted()) {
      if (table->SpikeCanonicalTable()->GetEntryText(*native.entry()) ==
          entry->text)
        matched = true;
      native.Next();
    }
    EXPECT_TRUE(matched) << entry->text;
    if (checked == 0)
      EXPECT_EQ(entry->text, "你好");
    ++checked;
    entries.Next();
  }
  EXPECT_GT(checked, 0u);
  EXPECT_LE(checked, 8u);
  EXPECT_FALSE(graph.spike_exhausted);
}
