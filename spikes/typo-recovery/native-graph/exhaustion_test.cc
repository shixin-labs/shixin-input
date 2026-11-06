#include <gtest/gtest.h>
#include <chrono>
#include <cstdlib>
#include <rime/algo/syllabifier.h>
#include <rime/dict/dictionary.h>
#include <rime/dict/prism.h>
#include <rime/dict/table.h>

TEST(SpikeExhaustion, ExhaustionStaysSetUntilANewBudget) {
  rime::SyllableGraph graph;
  graph.spike_bounded = true;
  graph.spike_deadline =
      std::chrono::steady_clock::now() + std::chrono::seconds(1);
  graph.spike_chunks_left = 0;
  EXPECT_FALSE(graph.SpendSpikeChunk());
  EXPECT_TRUE(graph.spike_exhausted);
  const auto work = graph.spike_work_left;
  EXPECT_FALSE(graph.SpendSpikeWork());
  EXPECT_EQ(graph.spike_work_left, work);
  rime::SyllableGraph second;
  second.spike_bounded = true;
  second.CarrySpikeBudgetFrom(graph);
  EXPECT_FALSE(second.SpendSpikeWork());
  graph.spike_bounded = false;
  EXPECT_TRUE(graph.SpendSpikeWork());
}

TEST(SpikeExhaustion, GeneralLookupDiscardsPartialChunksAndCanRecover) {
  const char *source = std::getenv("SHIXIN_SPIKE_TABLE");
  const char *prism_file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!source || !prism_file)
    GTEST_SKIP();
  auto table = rime::New<rime::Table>(rime::path(std::string(source)));
  auto prism = rime::New<rime::Prism>(rime::path(std::string(prism_file)));
  rime::Dictionary dictionary("rime_ice", {}, {table}, prism);
  ASSERT_TRUE(dictionary.Load());
  rime::Syllabifier syllabifier("'", true, false);
  rime::SyllableGraph graph;
  syllabifier.BuildSyllableGraph("nihao", *prism, &graph);
  auto reset = [&](size_t chunks) {
    graph.spike_bounded = true;
    graph.spike_graph_complete = true;
    graph.spike_exhausted = false;
    graph.spike_work_left = 32768;
    graph.spike_nodes_left = 2048;
    graph.spike_chunks_left = chunks;
    graph.spike_deadline =
        std::chrono::steady_clock::now() + std::chrono::seconds(1);
  };
  reset(1);
  size_t accepted = 0;
  auto visitor = [](void *context, size_t, rime::TableAccessor &) {
    auto &state =
        *static_cast<std::pair<size_t *, rime::SyllableGraph *> *>(context);
    if (!state.second->SpendSpikeChunk())
      return false;
    ++*state.first;
    return true;
  };
  std::pair<size_t *, rime::SyllableGraph *> context{&accepted, &graph};
  rime::TableQueryResult result;
  table->Query(graph, 0, &result, visitor, &context);
  EXPECT_EQ(accepted, 1u);
  ASSERT_TRUE(graph.spike_exhausted);
  reset(1);
  EXPECT_FALSE(dictionary.Lookup(graph, 0, nullptr, false));
  EXPECT_TRUE(graph.spike_exhausted);
  reset(2048);
  graph.spike_deadline =
      std::chrono::steady_clock::now() - std::chrono::seconds(1);
  EXPECT_FALSE(dictionary.Lookup(graph, 0, nullptr, false));
  EXPECT_TRUE(graph.spike_exhausted);
  reset(2048);
  auto recovered = dictionary.Lookup(graph, 0, nullptr, false);
  ASSERT_TRUE(recovered);
  ASSERT_NE(recovered->find(5), recovered->end());
  ASSERT_TRUE(recovered->at(5).Peek());
  EXPECT_EQ(recovered->at(5).Peek()->text, "你好");
  EXPECT_FALSE(graph.spike_exhausted);
  graph.spike_bounded = false;
  graph.spike_exhausted = true;
  graph.spike_work_left = 0;
  auto ordinary = dictionary.Lookup(graph, 0, nullptr, false);
  ASSERT_TRUE(ordinary);
  ASSERT_NE(ordinary->find(5), ordinary->end());
  ASSERT_TRUE(ordinary->at(5).Peek());
  EXPECT_EQ(ordinary->at(5).Peek()->text, "你好");
}
