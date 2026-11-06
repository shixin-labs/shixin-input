#pragma once
// Scratch-only, engine-thread diagnostics. No raw text or candidate text.
#include <rime/common.h>
#include <rime/algo/syllabifier.h>
namespace rime {
struct SpikeStageDiagnostics {
  bool evaluated = false, found = false, available = false, graph_complete = false;
  SpikeStopReason stop = SpikeStopReason::None;
  SpikeSearchPhase phase = SpikeSearchPhase::Graph;
  size_t work_left = 0, nodes_left = 0, chunks_left = 0;
};
struct SpikeSearchDiagnostics {
  size_t input_bytes = 0, preferred_anchor = 0;
  SpikeStageDiagnostics stages[2];
};
static_assert(sizeof(SpikeSearchDiagnostics) <= 128, "bounded diagnostic snapshot");
RIME_DLL void EnableSpikeSearchDiagnostics(bool enabled);
RIME_DLL SpikeSearchDiagnostics ReadSpikeSearchDiagnostics();
} // namespace rime
