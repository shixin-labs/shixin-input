# Identify why a correction stage stops

Status: scratch native diagnostics, not a product feature; 2026-11-04.

The intermittent `niaho` miss now has a captured cause. In the first trace of
the final Release native matrix, the preferred pass completed at anchor byte 3.
The alternate-boundary pass built its graph and completed table traversal, but
expired during dictionary chunk sorting. Its pre-sort deadline check passed;
the post-sort check failed. The stage was discarded, as required. On the next
trace it completed with the same remaining operation counts.

| Alternate-boundary stage | First trace | Next trace |
|---|---|---|
| Graph complete | Yes | Yes |
| Stage accepted | No | Yes |
| Stop reason | Deadline | None |
| Phase at stop/end | Sort | Ready |
| Work remaining | 31,025 | 31,025 |
| Nodes remaining | 1,948 | 1,948 |
| Chunks remaining | 1,957 | 1,957 |

This observation rules out work/node/chunk exhaustion for **this miss**. It does
not prove every earlier miss had the same cause. It also does not identify page
faults versus scheduling or other first-use effects. The diagnostics intentionally
do not collect page-fault counters or add I/O to the key path.

`DictEntryIterator::Sort` partially sorts chunk heads. Its comparator reads each
head entry's weight from the native table. Combined with the earlier residency
profiles, this makes head-weight access a concrete next optimization target.
It does not justify increasing the deadline, retaining an incompletely sorted
stage, restricting abbreviation candidates, or warming the entire dictionary.

## What changed

The scratch `SyllableGraph` records the first stop reason: deadline, work, nodes,
chunks, or other. Budget carry preserves that reason; later failures do not
overwrite it. Existing checks, clock-check cadence, limits and rejection paths
remain the same. The `Other` bucket includes existing cache/search validation
failures whose underlying cause is not yet separately classified.

The graph also tracks a phase: graph construction, lookup, before-sort, sort,
user lookup, sentence generation or ready. `BeforeSort` is separate from `Sort`
so an expired pre-sort check cannot be misreported as sorting cost. These are
operational markers, not per-phase timing measurements.

`search_diagnostics.h` defines a fixed snapshot of at most 128 bytes. Recording
is disabled by default and enabled only by the native test on its engine thread.
The thread-local snapshot describes the last protected translator query and two
stages; it contains input length, guessed boundary, flags and counters, **no raw
input or candidate text**. Reading returns a copy. Enable/disable clears it.
It is not a per-session history, client protocol extension, AI payload or stable
production ABI. The test prints diagnostics after key processing for its public
fixture; the library does no logging or I/O on key handling.

The final availability check can notice deadline expiry without mutating the
graph. Snapshot recording classifies that rejection separately from an already
recorded first failure. Recording adds a clock read only for such unclassified
rejections when explicitly enabled. Diagnostic runs therefore should not be
treated as unchanged timing experiments.

## Validation and remaining work

Four focused tests cover unbounded graphs, each first-stop reason, sticky failure
and carry, the existing 64-work-unit deadline cadence, successful spending, and
snapshot reset. These tests and the recovery/filter/registry/native-selection
tests pass in Release and ASan/UBSan. Both native CTest suites pass. The expanded
80-trace native matrix still validates the swap span and unchanged ordinary
menus; its first trace may fall back, while the next trace must recover the
fixture target. No missing-result assertion was removed.

With recording disabled, the 2,280-key engine replay passes total latency gates:
p50 72 µs, p99 1.28 ms, p99.9 1.536 ms, maximum 7.799 ms. The maximum is reported
rather than hidden by percentiles; this is not a hard real-time guarantee.
The standard engine key/allocation check passes; short fixture benchmarks report
22.67 µs/key and 22.12 µs/key for normal and English-punctuation cases, both
0 B/op and 0 Go allocations/op. Those standard fixtures do not prove the native
correction pipeline allocation-free, nor do they measure correction-only cost.

The missing cold correction is diagnosed, not fixed. The next experiment must
reduce head-weight access/sorting cost while preserving all dictionary entries,
abbreviation alternatives and the bounded fallback. Full resource gates,
conversion expansion, production integration and broader retrieval remain open.

[Test logs and source hashes](typo-search-diagnostics/). Apply the retained
scratch patch and copy the new `search_diagnostics.h` into `src/rime/gear/`, as
listed in the native spike README. Rebuild all native tests after the graph
layout change; old C++ binaries are not ABI compatible.
