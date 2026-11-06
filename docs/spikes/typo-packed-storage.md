# Packed nodes, pooled strings and a prebuilt search image

Date: 2026-11-04 · Offline storage experiment, not an IME implementation

**This saves more than radix compression:** packed leaf/internal records, compact
edge arrays and pooled live strings reduce major retained buffers from
**77.48 MiB to 51.93 MiB (33.0%)**. Loading a prebuilt image in a fresh search-only
process peaks at **54.97 MiB RSS**, instead of 259.58 MiB when constructing the
ordinary trie in that process. Construction still costs memory in the separate
offline process; it is not eliminated. The model and actual engine are not part
of these memory measurements.

All 2,616 queries have identical candidates, costs, priors, state/edge counts,
queue peaks and exhaustion flags in every variant. This is a storage improvement,
not an accuracy improvement or a fix for the search's failed gates.

Design: [typo-tolerant pinyin](../typo-tolerant-pinyin.md).
Controls: [bounded recovery](typo-abbreviation-recovery.md),
[radix compression](typo-radix.md).
Evidence: [summary](typo-packed-storage/summary.json),
[input/image/output hashes](typo-packed-storage/input-output-sha256.txt),
[source/run-binary hashes](typo-packed-storage/source-binary-sha256.txt).

## Alternatives tested

**Live string pooling:** preserve word IDs, order and floating-point priors;
remove text not referenced by retained terminal words, share identical Chinese
text across readings, and copy canonical spellings into a right-sized buffer.
The canonical strings were already shared by reading, so their saving is mostly
unused buffer capacity. A sorted permutation of word IDs avoids large temporary
interning hash tables. No text, pronunciation or prior is quantized or dropped
beyond the parent experiment's existing four-word terminal quota.

**Packed node records:** a leaf stores only its word offset/count in four bytes.
An internal node uses a 16-byte record instead of the ordinary 24-byte node.
Its total prior bound is reconstructed exactly from its unchanged descendant
bound and terminal prior. A ranked bit vector maps the original node IDs to
leaf/internal arrays. Edge children and syllable IDs use separate arrays,
removing padding: six bytes per edge instead of eight. There are 646,813 leaves
and 661,842 internal records. This is independent of radix compression; these
runs do not combine the two.

**Prebuilt image:** write the combined representation once, then start a new
process that reads the packed arrays directly. That process never constructs
the ordinary trie or compaction maps. This is ordinary buffered file loading,
not `mmap`, and the filesystem cache was not flushed. It demonstrates
fresh-process memory use, not cold-disk startup performance.

## Measurements

Same 887,781 reading/text pairs and 2,616 query/prefix inputs, same compiler and
final executable. Each variant warms the queries before timing. Runs are
sequential; small timing differences are observations, not a stable ranking of
algorithms. Memory totals include major buffer capacities but exclude small
containers/allocator overhead. RSS is a separate whole-process peak counter.

| Variant | Retained buffers | Peak process RSS | Lookup p99 |
|---|---:|---:|---:|
| Ordinary syllable trie | 77.48 MiB | 259.58 MiB | 0.1520 ms |
| String pooling only | 71.58 MiB | 273.39 MiB | 0.1476 ms |
| Packed nodes/edges only | 57.83 MiB | 258.53 MiB | 0.1593 ms |
| Packed + pooled, built in process | 51.93 MiB | 281.88 MiB | 0.1585 ms |
| Packed + pooled, loaded in fresh process | 51.93 MiB | **54.97 MiB** | 0.1568 ms |

The separate [image-building process](typo-packed-storage/build-image-process.txt)
peaks at 281.91 MiB. Moving construction out of the runtime process accounts for
the large RSS reduction; record packing alone does not produce that reduction.
A first hash-table pooling attempt peaked near 396 MiB; sorting IDs reduced the
construction overhead before the final comparison.

The prebuilt file is **54,374,582 bytes** (~51.86 MiB). The fresh-load process's
0.37-second wall time includes loading, validation, query warm-up, measurement
and JSON output; it is not an isolated startup or first-key latency number.
[Loaded-process evidence](typo-packed-storage/loaded-process.txt) and
[plain-process evidence](typo-packed-storage/plain-process.txt) retain the raw
OS counters. No ERNIE or native Rime sessions ran during these measurements.

All variants have zero intercepted C++ `new` allocations per search and the same
45,064-byte search workspace. Loaded p99.9 is recorded in the summary; integrated
engine latency remains unmeasured. Files are opened and arrays allocated only
before queries, never from the typing/search path.

## Correctness and image boundaries

Normalized JSON output, excluding only nanosecond timings, matches the parent
recovery run byte for byte in all five variants. The image preserves original
logical IDs and exact floats. No new candidate capture or model scoring was
needed: the previous recall, six clean-page losses and 400/830 exhausted examples
remain unchanged. Prefix stress still exhausts 1,134/2,616 queries.

ASan/UBSan passes 10,000 randomized three-way comparisons (ordinary, packed and
image-loaded), node/edge reconstruction and word text/code/prior checks across
100 small dictionaries. Explicit prefix words, duplicate Chinese text under
multiple readings, and low operation caps are covered. One hundred corruption
cases include bad magic, truncation and oversized counts; other mutated images
are either rejected or safely queried when structurally valid. Existing search
oracle and radix-equivalence suites also passed after the shared accessor changes.

The prototype image has a version magic, byte-order marker and bounded counts.
The loader rejects files over 128 MiB, checks section sizes before allocation,
validates string spans, finite priors, bit ranks, sorted edges, single-parent
acyclic topology, terminal ranges and recomputed search bounds before querying.
It is an experiment-specific POD format, **not** a production generation/IPC
contract. Failed loads are discarded. Not every content change is corruption:
structurally valid changed words can pass; file identity is recorded separately
by SHA-256 in this experiment.

Production adoption still needs builder/profile identity, checksums, cross-platform
format validation and generation hot-swap integration. Packed field bounds must
be validated in the builder, and invalid/missing artifacts must disable extra
recovery while ordinary Rime translation continues. The artifact must be warmed
before activation; never move file loading into the key path.

## Design consequence

Prefer a **builder-produced compact immutable index** over constructing and
compacting a second dictionary in the engine. Leaf specialization and payload
compaction currently save more than radix compression in this corpus. The native
librime dictionary still exists, so even 51.93 MiB is an incremental duplicate
index in this experiment; native structure reuse remains worth investigating.

This result addresses search-index storage and runtime construction overhead.
It does not remove abbreviation ambiguity, the queue, model memory, or the
clean-input regression gate. Do not ship this feature on the strength of the
memory result alone.

## Reproduction

Use the parent's prepared lexicon/query files and fresh output paths:

```sh
clang++ -std=c++17 -O3 -Wall -Wextra -Werror \
  spikes/typo-recovery/native/abbreviation_search.cc \
  -o build/typo-recovery/bin/abbreviation-storage-search
# Compare plain, strings, packed, and packed-strings; use separate outputs.
build/typo-recovery/bin/abbreviation-storage-search \
  build/typo-recovery/abbr-recovery-v1/syllable-lexicon.tsv \
  build/typo-recovery/abbr-recovery-v1/queries.txt packed-strings
# In save mode the second path is a new image output, not a query file.
build/typo-recovery/bin/abbreviation-storage-search \
  build/typo-recovery/abbr-recovery-v1/syllable-lexicon.tsv \
  build/typo-recovery/storage-v2/packed.bin save-packed
# A separate process loads that image and performs queries.
build/typo-recovery/bin/abbreviation-storage-search \
  build/typo-recovery/storage-v2/packed.bin \
  build/typo-recovery/abbr-recovery-v1/queries.txt load-packed
clang++ -std=c++17 -O1 -g -fsanitize=address,undefined \
  -fno-omit-frame-pointer spikes/typo-recovery/native/packed_storage_test.cc \
  -o build/typo-recovery/bin/packed-storage-test
build/typo-recovery/bin/packed-storage-test \
  build/typo-recovery/storage-v2/test-image.bin
```

No production engine/settings code, installed IME, live dictionaries, model or
ranking parameters changed. Binary/data artifacts remain under ignored `build/`.
