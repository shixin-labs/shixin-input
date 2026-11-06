#include <gtest/gtest.h>
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <sys/resource.h>
#include <rime/algo/syllabifier.h>
#include <rime/dict/dictionary.h>
#include <rime/dict/prism.h>
#include <rime/dict/table.h>
#include "bounded_corrector.inc"

// Read-only native diagnostic. Run one raw code per fresh process to distinguish
// first-query costs from repeated-query costs. No Rime session or user dictionary.
// The normal Release deadline is unchanged; this is not an engine latency gate.
TEST(SpikeCacheStages, FirstAndRepeatedQueries) {
  const char* file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!file) GTEST_SKIP();
  const char* requested = std::getenv("SHIXIN_SPIKE_RAW");
  const std::string raw = requested ? requested : "xgr";
  auto prism = rime::New<rime::Prism>(rime::path(std::string(file)));
  std::string table_file = file;
  auto suffix = table_file.find("prism.bin");
  ASSERT_NE(suffix, std::string::npos);
  table_file.replace(suffix, 9, "table.bin");
  auto table = rime::New<rime::Table>(rime::path(table_file));
  rime::Dictionary dict("rime_ice", {}, {table}, prism);
  ASSERT_TRUE(dict.Load());
  ASSERT_TRUE(table->PrepareSpikeInitialCache());
  if (std::getenv("SHIXIN_SPIKE_WARM_STRINGS"))
    ASSERT_TRUE(table->WarmSpikeStrings());
  if (std::getenv("SHIXIN_SPIKE_WARM_INDEXES"))
    ASSERT_TRUE(table->WarmSpikeIndexes());
  if (std::getenv("SHIXIN_SPIKE_WARM_ENTRIES"))
    ASSERT_TRUE(table->WarmSpikeIndexes(true));
  if (std::getenv("SHIXIN_SPIKE_WARM_ALL"))
    ASSERT_TRUE(table->WarmSpikePages());
  if (const char* mirror = std::getenv("SHIXIN_SPIKE_MIRROR")) {
    ASSERT_TRUE(table->PrepareSpikeMirrorCache(mirror));
    ASSERT_TRUE(table->PrepareSpikeMirrorCache(mirror));
  }
  ShixinSpikeCorrector corrector;
  corrector.SpikeAnchor(size_t(-1));
  corrector.SpikeAbbreviated(true);
  if (const char* anchor = std::getenv("SHIXIN_SPIKE_ANCHOR"))
    corrector.SpikeAnchor(std::stoul(anchor));
  if (std::getenv("SHIXIN_SPIKE_SPECIFIC")) {
    corrector.SpikeAbbreviated(false);
    corrector.SpikeSpecific(true);
  }
  rime::Syllabifier syllabifier("'", true, false);
  syllabifier.EnableCorrection(&corrector);
  using Clock = std::chrono::steady_clock;
  auto us = [](Clock::duration elapsed) {
    return std::chrono::duration_cast<std::chrono::microseconds>(elapsed).count();
  };
  for (int attempt = 0; attempt < 6; ++attempt) {
    rime::SyllableGraph graph;
    auto begin = Clock::now();
    syllabifier.BuildSyllableGraph(raw, *prism, &graph);
    auto built = Clock::now();
    const bool graph_exhausted = graph.spike_exhausted;
    const auto graph_work = graph.spike_work_left;
    size_t groups = 0;
    rusage before_lookup{}, after_lookup{};
    ASSERT_EQ(getrusage(RUSAGE_SELF, &before_lookup), 0);
    rime::an<rime::DictEntryCollector> found;
    if (std::getenv("SHIXIN_SPIKE_TABLE_ONLY")) {
      rime::TableQueryResult result;
      table->Query(graph, 0, &result);
      groups = result.size();
    } else {
      found = dict.Lookup(graph, 0, nullptr, false);
      groups = found ? found->size() : 0;
    }
    auto looked_up = Clock::now();
    ASSERT_EQ(getrusage(RUSAGE_SELF, &after_lookup), 0);
    size_t materialized = 0, text_bytes = 0;
    if (found) {
      for (auto& group : *found) {
        while (materialized < 64) {
          auto entry = group.second.Peek();
          if (!entry) break;
          ++materialized;
          text_bytes += entry->text.size();
          group.second.Next();
        }
      }
    }
    auto decoded = Clock::now();
    size_t spellings = 0;
    for (const auto& start : graph.edges)
      for (const auto& end : start.second) spellings += end.second.size();
    std::cout << "CACHE_STAGES raw=" << raw << " attempt=" << attempt
              << " graph_us=" << us(built - begin)
              << " lookup_us=" << us(looked_up - built)
              << " lookup_minor_faults=" << after_lookup.ru_minflt-before_lookup.ru_minflt
              << " lookup_major_faults=" << after_lookup.ru_majflt-before_lookup.ru_majflt
              << " decode_us=" << us(decoded - looked_up)
              << " decoded=" << materialized << " text_bytes=" << text_bytes
              << " graph_spellings=" << spellings
              << " graph_work=" << (32768 - graph_work)
              << " lookup_work=" << (graph_work - graph.spike_work_left)
              << " graph_complete=" << graph.spike_graph_complete
              << " graph_exhausted=" << graph_exhausted
              << " lookup_exhausted=" << graph.spike_exhausted
              << " groups=" << groups << '\n';
  }
}

#include <thread>
TEST(SpikeCacheStages, RejectsExpiryDuringNativeVisitor) {
  const char* file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!file) GTEST_SKIP();
  auto prism = rime::New<rime::Prism>(rime::path(std::string(file)));
  std::string table_file = file;
  auto suffix = table_file.find("prism.bin");
  ASSERT_NE(suffix, std::string::npos);
  table_file.replace(suffix, 9, "table.bin");
  auto table = rime::New<rime::Table>(rime::path(table_file));
  rime::Dictionary dict("rime_ice", {}, {table}, prism);
  ASSERT_TRUE(dict.Load());
  ASSERT_TRUE(table->PrepareSpikeInitialCache());
  if (const char* mirror = std::getenv("SHIXIN_SPIKE_MIRROR")) {
    ASSERT_TRUE(table->PrepareSpikeMirrorCache(mirror));
    ASSERT_TRUE(table->PrepareSpikeMirrorCache(mirror));
  }
  ShixinSpikeCorrector corrector;
  corrector.SpikeAbbreviated(true);
  rime::Syllabifier syllabifier("'", true, false);
  syllabifier.EnableCorrection(&corrector);
  rime::SyllableGraph graph;
  graph.spike_budget_carried = true;
  graph.spike_deadline = std::chrono::steady_clock::now() + std::chrono::seconds(1);
  syllabifier.BuildSyllableGraph("xgr", *prism, &graph);
  ASSERT_TRUE(graph.spike_graph_complete);
  bool called = false;
  auto visitor = [](void* context, size_t, rime::TableAccessor&) {
    *static_cast<bool*>(context) = true;
    std::this_thread::sleep_for(std::chrono::milliseconds(110));
    return true;
  };
  graph.spike_deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(100);
  rime::TableQueryResult result;
  EXPECT_FALSE(table->Query(graph, 0, &result, visitor, &called));
  EXPECT_TRUE(called);
  EXPECT_TRUE(graph.spike_exhausted);
  EXPECT_TRUE(result.empty());
  // The same graph can be queried on a later update with reset budgets.
  graph.spike_exhausted = false;
  graph.spike_deadline = std::chrono::steady_clock::now() + std::chrono::seconds(1);
  EXPECT_TRUE(table->Query(graph, 0, &result));
  EXPECT_FALSE(result.empty());
  EXPECT_FALSE(graph.spike_exhausted);
}
