#include <gtest/gtest.h>
#include <cstdlib>
#include <rime/algo/syllabifier.h>
#include <rime/dict/dictionary.h>
#include <rime/dict/prism.h>
#include <rime/dict/table.h>
#include "bounded_corrector.inc"
TEST(SpikeMirror, NativeIdentityLifetimeAndMissingArtifact) {
  const char *prism_file = std::getenv("SHIXIN_SPIKE_PRISM");
  const char *mirror_file = std::getenv("SHIXIN_SPIKE_MIRROR");
  if (!prism_file || !mirror_file)
    GTEST_SKIP();
  auto prism = rime::New<rime::Prism>(rime::path(std::string(prism_file)));
  std::string table_file = prism_file;
  auto suffix = table_file.find("prism.bin");
  ASSERT_NE(suffix, std::string::npos);
  table_file.replace(suffix, 9, "table.bin");
  auto table = rime::New<rime::Table>(rime::path(table_file));
  rime::Dictionary dict("rime_ice", {}, {table}, prism);
  ASSERT_TRUE(dict.Load());
  ASSERT_TRUE(table->PrepareSpikeInitialCache());
  ASSERT_TRUE(table->PrepareSpikeMirrorCache(mirror_file));
  ShixinSpikeCorrector corrector;
  corrector.SpikeAbbreviated(true);
  rime::Syllabifier syllabifier("'", true, false);
  syllabifier.EnableCorrection(&corrector);
  rime::SyllableGraph graph;
  graph.spike_budget_carried = true;
  auto reset = [&] {
    graph.spike_deadline =
        std::chrono::steady_clock::now() + std::chrono::seconds(1);
    graph.spike_exhausted = false;
    graph.spike_work_left = 32768;
    graph.spike_nodes_left = 2048;
    graph.spike_chunks_left = 2048;
  };
  reset();
  syllabifier.BuildSyllableGraph("xgr", *prism, &graph);
  ASSERT_TRUE(graph.spike_graph_complete);
  rime::TableQueryResult direct;
  ASSERT_TRUE(table->Query(graph, 0, &direct));
  ASSERT_EQ(direct.count(3), 1u);
  ASSERT_FALSE(direct[3].empty());
  const auto first = direct[3].front();
  ASSERT_EQ(first.cached_text(), "中国人");
  ASSERT_EQ(first.index_code().size(), 3u);
  EXPECT_EQ(table->GetSyllableById(first.index_code()[0]), "zhong");
  EXPECT_EQ(table->GetSyllableById(first.index_code()[1]), "guo");
  EXPECT_EQ(table->GetSyllableById(first.index_code()[2]), "ren");
  ASSERT_TRUE(table->PrepareSpikeMirrorCache(mirror_file));
  EXPECT_FALSE(
      table->PrepareSpikeMirrorCache(std::string(mirror_file) + ".missing"));
  reset();
  rime::TableQueryResult again;
  ASSERT_TRUE(table->Query(graph, 0, &again));
  EXPECT_EQ(first.entry(), again[3].front().entry());
  EXPECT_EQ(first.cached_text().data(), again[3].front().cached_text().data());
  reset();
  auto entries = dict.Lookup(graph, 0, nullptr, false);
  ASSERT_TRUE(entries);
  auto group = entries->find(3);
  ASSERT_NE(group, entries->end());
  auto entry = group->second.Peek();
  ASSERT_TRUE(entry);
  EXPECT_EQ(entry->text, "中国人");
  EXPECT_EQ(entry->code, first.index_code());
  auto missing = rime::New<rime::Table>(rime::path(table_file));
  ASSERT_TRUE(missing->Load());
  ASSERT_TRUE(missing->PrepareSpikeInitialCache());
  EXPECT_FALSE(
      missing->PrepareSpikeMirrorCache(std::string(mirror_file) + ".missing"));
  reset();
  rime::TableQueryResult none;
  EXPECT_FALSE(missing->Query(graph, 0, &none));
  EXPECT_TRUE(none.empty());
  EXPECT_TRUE(graph.spike_exhausted);
}
