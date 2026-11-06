#include <gtest/gtest.h>
#include <rime/algo/syllabifier.h>
#include "search_diagnostics.h"

TEST(SearchDiagnostics, UnboundedIgnoresBudgets) {
  rime::SyllableGraph graph;
  graph.spike_work_left = graph.spike_nodes_left = graph.spike_chunks_left = 0;
  EXPECT_TRUE(graph.SpendSpikeWork());
  EXPECT_TRUE(graph.SpendSpikeNode());
  EXPECT_TRUE(graph.SpendSpikeChunk());
  EXPECT_TRUE(graph.CheckSpikeDeadline());
  EXPECT_EQ(graph.spike_stop_reason, rime::SpikeStopReason::None);
}

TEST(SearchDiagnostics, IdentifiesFirstExhaustionAndCarriesIt) {
  for (auto reason : {rime::SpikeStopReason::Deadline, rime::SpikeStopReason::Work,
                      rime::SpikeStopReason::Nodes, rime::SpikeStopReason::Chunks,
                      rime::SpikeStopReason::Other}) {
    rime::SyllableGraph graph;
    graph.spike_bounded = true;
    graph.spike_deadline = std::chrono::steady_clock::time_point::max();
    switch (reason) {
    case rime::SpikeStopReason::Deadline:
      graph.spike_deadline = std::chrono::steady_clock::time_point::min();
      EXPECT_FALSE(graph.CheckSpikeDeadline());
      break;
    case rime::SpikeStopReason::Work:
      graph.spike_work_left = 0;
      EXPECT_FALSE(graph.SpendSpikeWork());
      break;
    case rime::SpikeStopReason::Nodes:
      graph.spike_nodes_left = 0;
      EXPECT_FALSE(graph.SpendSpikeNode());
      break;
    case rime::SpikeStopReason::Chunks:
      graph.spike_chunks_left = 0;
      EXPECT_FALSE(graph.SpendSpikeChunk());
      break;
    default: EXPECT_FALSE(graph.FailSpike(reason)); break;
    }
    EXPECT_TRUE(graph.spike_exhausted);
    EXPECT_EQ(graph.spike_stop_reason, reason);
    graph.FailSpike(rime::SpikeStopReason::Other);
    EXPECT_EQ(graph.spike_stop_reason, reason);
    rime::SyllableGraph next;
    next.spike_bounded = true;
    next.CarrySpikeBudgetFrom(graph);
    EXPECT_EQ(next.spike_stop_reason, reason);
    EXPECT_EQ(next.spike_deadline, graph.spike_deadline);
    EXPECT_FALSE(next.SpendSpikeWork());
    EXPECT_FALSE(next.CheckSpikeDeadline());
  }
}

TEST(SearchDiagnostics, DeadlineCheckCadenceAndSuccessfulSpending) {
  rime::SyllableGraph graph;
  graph.spike_bounded = true;
  graph.spike_deadline = std::chrono::steady_clock::time_point::max();
  graph.spike_work_left = 65;
  EXPECT_TRUE(graph.SpendSpikeNode());
  EXPECT_EQ(graph.spike_work_left, 64u);
  EXPECT_EQ(graph.spike_nodes_left, 2047u);
  graph.spike_deadline = std::chrono::steady_clock::time_point::min();
  EXPECT_FALSE(graph.SpendSpikeWork());
  EXPECT_EQ(graph.spike_work_left, 64u);
  EXPECT_EQ(graph.spike_stop_reason, rime::SpikeStopReason::Deadline);
}

TEST(SearchDiagnostics, EnableAndDisableClearCopiedSnapshot) {
  rime::EnableSpikeSearchDiagnostics(true);
  auto empty = rime::ReadSpikeSearchDiagnostics();
  EXPECT_EQ(empty.input_bytes, 0u);
  EXPECT_FALSE(empty.stages[0].evaluated);
  rime::EnableSpikeSearchDiagnostics(false);
  EXPECT_FALSE(rime::ReadSpikeSearchDiagnostics().stages[1].evaluated);
}
