# Radix compression of the abbreviation-search index

Date: 2026-11-04 · Offline prototype · No product integration

**Yes: radix path compression saves retained index memory.** On the same
887,781 reading/text pairs, major retained buffers fall from **77.48 MiB to
69.49 MiB**, saving **7.99 MiB / 10.31%**. Search output and operation counts are
identical for all 2,616 queries. This does not fix queue exhaustion, and this
build-then-compress prototype does not reduce measured startup peak RSS.

Parent experiment: [bounded abbreviation recovery](typo-abbreviation-recovery.md).
Evidence: [summary](typo-radix/summary.json),
[plain buffer sizes](typo-radix/plain-memory.json),
[radix buffer sizes](typo-radix/radix-memory.json),
[plain process measurement](typo-radix/plain-process-stats.txt),
[radix process measurement](typo-radix/radix-process-stats.txt),
[input/output hashes](typo-radix/input-output-sha256.txt),
[source/binary hashes](typo-radix/source-binary-sha256.txt).

## Representation

A radix tree collapses paths with no branches. Here only **nonterminal nodes
with one child** are removed. Branches and word endpoints remain explicit, so
prefix words are not lost. Each compressed label retains its sequence of
syllable IDs: flattening it into one undifferentiated character string would
lose the boundaries needed for full, initial and mixed spelling choices.

The prototype removes 472,698 explicit nodes (36.12%), leaving 835,957 nodes.
There are 331,902 compressed chains. Many chains are short, and candidate text,
canonical strings and weights remain unchanged; consequently the overall saving
is smaller than the node-count reduction.

Queries can still stop at every logical syllable boundary, choose an initial,
or carry a cross-boundary swap. Remaining-length and prior bounds are recovered
from the chain endpoint and its remaining labels. Two compact bit vectors with
prefix counts map original logical IDs to packed nodes/chains. Original IDs are
preserved to keep heap tie-breaking, deduplication and budget behavior identical.
The builder's consecutive-ID invariant for nonterminal single-child chains is
asserted and tested; this is not a loader for arbitrary external radix files.

A first version used per-node/per-label mapping arrays and saved only 3.78%.
The final bit-vector bookkeeping yields the 10.31% result. No strings, weights,
dictionary entries or search quotas were changed to obtain this saving.

## Measured comparison

Both final variants use the same executable, compiler settings, input files and
fixed operation caps, with/without its `radix` argument. Each warms all queries
before measurement; runs are sequential, not randomized or a broad benchmark.

| Measurement | Plain syllable trie | Radix representation |
|---|---:|---:|
| Major retained buffer capacities | 81,247,270 B | 72,867,522 B |
| Fixed search workspace | 45,064 B | 45,064 B |
| Lookup p50 | 0.0514 ms | 0.0569 ms |
| Lookup p99 | 0.1541 ms | 0.1658 ms |
| Lookup maximum | 0.2399 ms | 0.2538 ms |
| Intercepted query `new` allocations | 0 | 0 |
| Exhausted queries | 1,134/2,616 | 1,134/2,616 |
| Whole-process peak RSS | 270,155,776 B | 270,237,696 B |

Retained capacity totals include node/edge/word arrays, text/code buffers and
radix metadata, but exclude small containers and allocator overhead. They are
not RSS. Startup peak RSS is essentially unchanged: the process builds the
ordinary trie, then compresses it, and construction scratch memory dominates.
A direct compressed builder or a serialized load path is a future experiment;
no startup/RSS saving is claimed from this run.

The ~7.6% p99 increase in this single pair is an observed decoding tradeoff,
not a stable performance estimate. Both p99 values are below the proposed
standalone-search target, but integrated engine cost remains unmeasured.

## Correctness and remaining limits

Removing timing fields produces byte-identical output for radix, the plain
variant of this executable, and the prior recovery run: candidate text, canonical
code, costs, priors, allocation counts, states, edges, queue peaks and fallback
flags all match. Therefore no new menu capture or model rerun was needed; the
previous accuracy results and failed clean-input gates remain applicable.

ASan/UBSan passes 10,000 randomized radix/plain query comparisons across 100
small dictionaries, explicit prefix-word/abbreviation/swap/invalid-input/cap
fixtures, topology reconstruction checks and bit-rank boundary checks. The
existing 12,500 exhaustive-reference search comparisons also pass after the
shared accessor changes. Compression allocates only during initialization;
search keeps the same fixed workspace.

A radix tree changes **dictionary storage**, while the priority queue holds
**competing interpretations of the current input**. Preserving those
interpretations deliberately preserves the queue pressure: 400/830 full
examples still exhaust, and clean-page regressions are unchanged. This is a
useful storage improvement, not a replacement for better constrained search or
safe ranking. The index still duplicates native dictionary information and is
not an approved product memory increase.

## Reproduction

Use the prepared input from the parent experiment and a new output directory.
The existing `abbreviation_search` executable accepts an optional `radix` flag:

```sh
clang++ -std=c++17 -O3 -Wall -Wextra -Werror \
  spikes/typo-recovery/native/abbreviation_search.cc \
  -o build/typo-recovery/bin/abbreviation-radix-compact
# Omit the last argument for the plain control; use separate output files.
build/typo-recovery/bin/abbreviation-radix-compact \
  build/typo-recovery/abbr-recovery-v1/syllable-lexicon.tsv \
  build/typo-recovery/abbr-recovery-v1/queries.txt radix
clang++ -std=c++17 -O1 -g -fsanitize=address,undefined \
  -fno-omit-frame-pointer spikes/typo-recovery/native/radix_search_test.cc \
  -o build/typo-recovery/bin/radix-search-test
build/typo-recovery/bin/radix-search-test
```

No production engine code, installed IME, user dictionaries, model or ranking
parameters changed. Compression remains optional in the offline prototype.

Follow-up: [packed records and prebuilt loading](typo-packed-storage.md) save
more retained storage on this corpus and separately demonstrate lower runtime
peak RSS by keeping construction in an offline process.
