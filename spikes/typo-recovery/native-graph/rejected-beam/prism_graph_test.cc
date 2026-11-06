#include <cstdlib>
#include <gtest/gtest.h>
#include <iostream>
#include <rime/algo/syllabifier.h>
#include <rime/dict/prism.h>
TEST(SpikePrism, InspectOrdinaryGraph) {
  const char *file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!file)
    GTEST_SKIP();
  rime::Prism prism{rime::path(std::string(file))};
  ASSERT_TRUE(prism.Load());
  for (const std::string input : {"nihso", "changshicuowj", "nihsozhongguo"}) {
    rime::Syllabifier syllabifier("'", true, false);
    rime::SyllableGraph graph;
    syllabifier.BuildSyllableGraph(input, prism, &graph);
    std::cout << "INPUT " << input << " " << graph.input_length << " "
              << graph.interpreted_length << "\n";
    for (const auto &start : graph.edges)
      for (const auto &edge : start.second) {
        int type = 9;
        for (const auto &syllable : edge.second)
          type = std::min(type, int(syllable.second.type));
        std::cout << start.first << "->" << edge.first << " type=" << type
                  << "\n";
      }
  }
}

#include "bounded_corrector.inc"
#include <rime/dict/corrector.h>
#include <rime/dict/dictionary.h>
#include <rime/dict/table.h>
TEST(SpikePrism, InspectAnchoredLookupWork) {
  const char *file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!file)
    GTEST_SKIP();
  auto prism = rime::New<rime::Prism>(rime::path(std::string(file)));
  std::string table_file = file;
  table_file.replace(table_file.find("prism.bin"), 9, "table.bin");
  auto table = rime::New<rime::Table>(rime::path(table_file));
  rime::Dictionary dictionary("rime_ice", {}, {table}, prism);
  ASSERT_TRUE(dictionary.Load());
  const char *raw = std::getenv("SHIXIN_SPIKE_RAW");
  const std::string input = raw ? raw : "changshicuowj";
  rime::Syllabifier ordinary_syllabifier("'", true, false);
  rime::SyllableGraph ordinary_graph;
  ordinary_syllabifier.BuildSyllableGraph(input, *prism, &ordinary_graph);
  for (const auto &start : ordinary_graph.edges)
    for (const auto &end : start.second)
      for (const auto &spelling : end.second)
        if (spelling.second.type == rime::kNormalSpelling &&
            table->GetSyllableById(spelling.first) ==
                input.substr(start.first, end.first - start.first))
          std::cout << "CANONICAL " << start.first << " " << end.first << " "
                    << table->GetSyllableById(spelling.first) << "\n";
  ShixinSpikeCorrector corrector;
  const char *anchor = std::getenv("SHIXIN_SPIKE_ANCHOR");
  corrector.SpikeAnchor(anchor ? std::strtoul(anchor, nullptr, 10)
                        : raw  ? size_t(-1)
                               : 11);
  if (std::getenv("SHIXIN_SPIKE_ABBREVIATED"))
    corrector.SpikeAbbreviated(true);
  rime::Syllabifier syllabifier("'", true, false);
  syllabifier.EnableCorrection(&corrector);
  rime::SyllableGraph graph;
  syllabifier.BuildSyllableGraph(input, *prism, &graph);
  std::cout << "BUILD left=" << graph.spike_work_left
            << " exhausted=" << graph.spike_exhausted << "\n";
  // Warm repeated lookup timing is diagnostic only, with no deadline relief
  // applied to the engine. Includes lookup result destruction.
  if (std::getenv("SHIXIN_SPIKE_TIMING")) {
    std::vector<long long> table_times, dictionary_times;
    auto reset = [&]() {
      graph.spike_work_left = 10000000;
      graph.spike_nodes_left = 10000000;
      graph.spike_chunks_left = 10000000;
      graph.spike_exhausted = false;
      graph.spike_deadline =
          std::chrono::steady_clock::now() + std::chrono::hours(1);
    };
    for (int n = 0; n < 51; n++) {
      reset();
      auto began = std::chrono::steady_clock::now();
      {
        rime::TableQueryResult result;
        table->Query(graph, 0, &result);
      }
      auto table_us = std::chrono::duration_cast<std::chrono::microseconds>(
                          std::chrono::steady_clock::now() - began)
                          .count();
      reset();
      began = std::chrono::steady_clock::now();
      {
        auto result = dictionary.Lookup(graph, 0, nullptr, false);
      }
      auto dictionary_us =
          std::chrono::duration_cast<std::chrono::microseconds>(
              std::chrono::steady_clock::now() - began)
              .count();
      if (n) {
        table_times.push_back(table_us);
        dictionary_times.push_back(dictionary_us);
      }
    }
    std::sort(table_times.begin(), table_times.end());
    std::sort(dictionary_times.begin(), dictionary_times.end());
    std::cout << "WARM_MEDIAN_US table=" << table_times[25]
              << " dictionary=" << dictionary_times[25] << " samples=50\n";
  }
  // Diagnostic only: measure demand instead of changing the engine's cap.
  graph.spike_work_left = 1000000;
  graph.spike_nodes_left = 1000000;
  graph.spike_chunks_left = 1000000;
  graph.spike_exhausted = false;
  graph.spike_deadline =
      std::chrono::steady_clock::now() + std::chrono::hours(1);
  if (std::getenv("SHIXIN_SPIKE_RECOVERY_ONLY")) graph.spike_corrected_entries_only = true;
  if (std::getenv("SHIXIN_SPIKE_CAPPED")) {
    graph.spike_work_left = 32768;
    graph.spike_nodes_left = graph.spike_chunks_left = 2048;
  }
  for (const auto &start : graph.edges) {
    size_t before = graph.spike_work_left;
    size_t nodes = graph.spike_nodes_left, chunks = graph.spike_chunks_left;
    auto began = std::chrono::steady_clock::now();
    auto result = dictionary.Lookup(graph, start.first, nullptr, false);
    std::cout << "LOOKUP start=" << start.first << " us="
              << std::chrono::duration_cast<std::chrono::microseconds>(
                     std::chrono::steady_clock::now() - began)
                     .count()
              << " work=" << before - graph.spike_work_left
              << " nodes=" << nodes - graph.spike_nodes_left
              << " chunks=" << chunks - graph.spike_chunks_left
              << " groups=" << (result ? result->size() : 0)
              << " exhausted=" << graph.spike_exhausted << "\n";
    if (result && (start.first == 0 || start.first == 8 || start.first == 11)) {
      auto it = result->find(input.size());
      if (it != result->end())
        for (int n = 0; n < 20000 && !it->second.exhausted();
             n++, it->second.Next()) {
          auto entry = it->second.Peek();
          if (n < 8 || entry->text == "中国人") std::cout << "WORD rank=" << n+1 << " " << start.first << " " << entry->text << " "
                    << entry->weight << "\n";
        }
    }
  }
}

// Compare complete entry multisets, including raw span and canonical code.
// No candidate truncation, user dictionary or Rime session is involved.
TEST(SpikePrism, SparseIntersectionPreservesCandidates) {
  const char *file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!file)
    GTEST_SKIP();
  auto prism = rime::New<rime::Prism>(rime::path(std::string(file)));
  std::string table_file = file;
  table_file.replace(table_file.find("prism.bin"), 9, "table.bin");
  auto table = rime::New<rime::Table>(rime::path(table_file));
  rime::Dictionary dict("rime_ice", {}, {table}, prism);
  ASSERT_TRUE(dict.Load());
  for (const std::string input :
       {"njh", "nihso", "changshicuowj", "nihsozhongguo"}) {
    ShixinSpikeCorrector corrector;
    corrector.SpikeAnchor(input == "changshicuowj" ? 11
                          : input == "njh"         ? 0
                                                   : 2);
    rime::Syllabifier syllabifier("'", true, false);
    syllabifier.EnableCorrection(&corrector);
    rime::SyllableGraph graph;
    syllabifier.BuildSyllableGraph(input, *prism, &graph);
    ASSERT_FALSE(graph.spike_exhausted);
    auto capture = [&](bool bounded) {
      graph.spike_bounded = bounded;
      graph.spike_work_left = 10000000;
      graph.spike_nodes_left = 10000000;
      graph.spike_chunks_left = 10000000;
      graph.spike_deadline =
          std::chrono::steady_clock::now() + std::chrono::hours(1);
      std::vector<std::string> rows;
      auto result = dict.Lookup(graph, 0, nullptr, false);
      if (result)
        for (auto &group : *result) {
          for (auto &it = group.second; !it.exhausted(); it.Next()) {
            auto entry = it.Peek();
            std::string row = std::to_string(group.first) + ":" + entry->text +
                              ":" + std::to_string(entry->weight);
            for (auto code : entry->code)
              row += ":" + std::to_string(code);
            rows.push_back(row);
          }
        }
      std::sort(rows.begin(), rows.end());
      return rows;
    };
    auto ordinary = capture(false);
    auto sparse = capture(true);
    ASSERT_FALSE(graph.spike_exhausted);
    ASSERT_FALSE(ordinary.empty());
    EXPECT_EQ(ordinary, sparse) << input;
    std::cout << "EQUIVALENT " << input << " entries=" << ordinary.size()
              << "\n";
  }
}

TEST(SpikePrism, StagesShareBudgetAndKeepInitialCorrections) {
  const char *file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!file)
    GTEST_SKIP();
  rime::Prism prism{rime::path(std::string(file))};
  ASSERT_TRUE(prism.Load());
  ShixinSpikeCorrector corrector;
  corrector.SpikeAnchor(0);
  rime::Syllabifier syllabifier("'", true, false);
  syllabifier.EnableCorrection(&corrector);
  rime::SyllableGraph budget;
  budget.spike_work_left = 100000;
  budget.spike_deadline =
      std::chrono::steady_clock::now() + std::chrono::hours(1);
  rime::SyllableGraph specific;
  specific.CarrySpikeBudgetFrom(budget);
  corrector.SpikeSpecific(true);
  syllabifier.BuildSyllableGraph("njh", prism, &specific);
  ASSERT_FALSE(specific.spike_exhausted);
  auto count = [](const rime::SyllableGraph &graph, bool abbreviated) {
    size_t n = 0;
    for (const auto &start : graph.edges)
      for (const auto &end : start.second)
        for (const auto &spelling : end.second)
          if (spelling.second.is_correction &&
              (spelling.second.type == rime::kAbbreviation) == abbreviated)
            ++n;
    return n;
  };
  EXPECT_GT(count(specific, false), 0);
  EXPECT_EQ(count(specific, true), 0);
  rime::SyllableGraph broad;
  broad.CarrySpikeBudgetFrom(specific);
  EXPECT_EQ(broad.spike_work_left, specific.spike_work_left);
  EXPECT_EQ(broad.spike_nodes_left, specific.spike_nodes_left);
  EXPECT_EQ(broad.spike_chunks_left, specific.spike_chunks_left);
  corrector.SpikeSpecific(false);
  syllabifier.BuildSyllableGraph("njh", prism, &broad);
  EXPECT_GT(count(broad, true), 0);
  EXPECT_LT(broad.spike_work_left, specific.spike_work_left);
  EXPECT_EQ(broad.spike_deadline, budget.spike_deadline);
  rime::SyllableGraph exhausted;
  budget.spike_work_left = 7;
  budget.spike_deadline =
      std::chrono::steady_clock::now() - std::chrono::seconds(1);
  exhausted.CarrySpikeBudgetFrom(budget);
  syllabifier.BuildSyllableGraph("njh", prism, &exhausted);
  EXPECT_TRUE(exhausted.spike_exhausted);
  EXPECT_EQ(exhausted.spike_deadline, budget.spike_deadline);
  EXPECT_LE(exhausted.spike_work_left, 7);
  EXPECT_EQ(budget.spike_work_left, 7);
}

TEST(SpikePrism, BudgetedLookupOnlyRetainsCompletedEntries) {
  const char *file = std::getenv("SHIXIN_SPIKE_PRISM");
  if (!file)
    GTEST_SKIP();
  auto prism = rime::New<rime::Prism>(rime::path(std::string(file)));
  std::string table_file = file;
  table_file.replace(table_file.find("prism.bin"), 9, "table.bin");
  auto table = rime::New<rime::Table>(rime::path(table_file));
  rime::Dictionary dict("rime_ice", {}, {table}, prism);
  ASSERT_TRUE(dict.Load());
  ShixinSpikeCorrector corrector;
  corrector.SpikeAnchor(0);
  rime::Syllabifier syllabifier("'", true, false);
  syllabifier.EnableCorrection(&corrector);
  rime::SyllableGraph graph;
  graph.spike_budget_carried = true;
  graph.spike_deadline =
      std::chrono::steady_clock::now() + std::chrono::hours(1);
  syllabifier.BuildSyllableGraph("xgr", *prism, &graph);
  ASSERT_TRUE(graph.spike_graph_complete);
  auto capture = [&](size_t work) {
    graph.spike_work_left = work;
    graph.spike_nodes_left = 1000000;
    graph.spike_chunks_left = 1000000;
    graph.spike_exhausted = false;
    std::vector<std::string> rows;
    auto result = dict.Lookup(graph, 0, nullptr, false);
    if (result)
      for (auto &group : *result)
        for (auto &it = group.second; !it.exhausted(); it.Next()) {
          auto entry = it.Peek();
          std::string row = std::to_string(group.first) + ":" + entry->text +
                            ":" + std::to_string(entry->weight);
          for (auto code : entry->code)
            row += ":" + std::to_string(code);
          rows.push_back(row);
        }
    std::sort(rows.begin(), rows.end());
    return rows;
  };
  auto full = capture(1000000);
  ASSERT_FALSE(full.empty());
  ASSERT_FALSE(graph.spike_exhausted);
  size_t useful_partial = 0;
  for (size_t cap : {1, 32, 128, 512, 2048}) {
    auto partial = capture(cap);
    EXPECT_TRUE(std::includes(full.begin(), full.end(), partial.begin(),
                              partial.end()));
    EXPECT_TRUE(graph.spike_exhausted);
    if (!partial.empty())
      ++useful_partial;
    std::cout << "PARTIAL cap=" << cap << " entries=" << partial.size()
              << " full=" << full.size() << "\n";
  }
  EXPECT_GT(useful_partial, 0);
  graph.spike_graph_complete = false;
  EXPECT_TRUE(capture(1000000).empty());
}
