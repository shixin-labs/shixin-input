# Typo recovery spike: bounded search works, first-page insertion is unsafe

Date: 2026-11-04 · Apple M4, macOS 26.7.1 · Status: offline experiment only

Design: [typo-tolerant pinyin](../typo-tolerant-pinyin.md).
Data: [preparation report](typo-data-preparation.md).
Code and reproduction: [spikes/typo-recovery](../../spikes/typo-recovery/README.md).
Follow-up: [retest with the existing ERNIE reranker](typo-rerank.md). Useful
context recovers the displaced clean targets in a small subset; this first
AI-off experiment does not establish that contextual reranking fails.
Machine-readable [results](typo-recovery/results.json),
[preparation](typo-recovery/preparation.json) and
[artifact checksums](typo-recovery/artifact-sha256.txt).

## Decision

Continue with bounded native spelling recovery, but **reject the tested merge
policy**. Recovery made the intended Chinese available on page one for most
sampled swaps and neighboring-key errors. Keeping the original #1 did not protect
clean input: inserting corrections after it displaced 20 of 128 clean targets
from the first page. Protect an entire exact-input first page before considering
more aggressive contextual promotion.

A new neural model is not required to recover these spellings. Whether a joint
correction/ranking model improves selection remains unanswered: this corpus has
no preceding context, and homophones do not have a uniquely identifiable intended
selection. The next bottlenecks are exact-path protection, lexical coverage,
compact native graph integration and contextual/real-typo evaluation data.

## Experiment fixed before evaluation

- Sampled 100 connected families each from the prepared dev and test splits,
  by the lowest hashes of `recovery-v1/family_id`. All their available variants
  were retained: **1,568 labeled examples**, including 266 clean controls.
  Test contains 128 clean records; variants share families and are not independent
  observations. No model fitting, threshold tuning or sample rejection by outcome.
- Built the spelling index **only from rime-ice's explicit pronunciations** in
  8105/base/ext/others/Tencent source files. Neither CC-CEDICT targets nor their
  canonical spellings were inserted into this index. Tencent's unannotated entries
  and other unsupported rows are omitted: 984,778 of 1,873,509 source rows skipped.
  This deliberately underrepresents Rime's sentence-composition capability.
- The index has **785,119 distinct codes**. Search makes one adjacent-character
  swap (cost 8) or QWERTY-neighbor substitution (cost 10), across the whole code.
  Sort by cost, then maximum dictionary frequency of the code, then lexical code.
  Weights are fixed experimental heuristics, not calibrated probabilities.
- Each query performs one exact walk (≤32 transitions) plus at most 2,048 search
  states, returning at most eight corrected spellings. State exhaustion drops
  all corrections while retaining the exact result. No insertion/deletion search.
- Captured **2,051 distinct real Rime menus**, including clean/raw codes and all
  recovered spellings, up to **20 raw candidates** each. Every candidate is selected
  in a fresh scratch session to test whole-input acceptance. Full-span is an
  observed commit/clear property, not introspected native graph alignment.
- The offline merge preserves baseline #1, adds up to four new corrected Chinese
  candidates, then fills from baseline to a total of 20. It considers codes in
  fixed search order, candidates in Rime order, and only the contiguous verified
  full-span prefix of each corrected menu. Baseline ranks retain all raw entries,
  including partial candidates. No model participates.

The prototype searches whole-word spellings rather than the proposed syllable
graph. Its cross-boundary results do **not** prove safe partial selection or
learning across transpositions. Offline replay of corrected codes is solely an
evaluation method; it is not the proposed runtime implementation.

## Held-out test results

“First page” means target rank ≤5 and verified whole-input acceptance. The
clean-code control is target recall in its top 20, not total dictionary coverage.
The union diagnostic is the target appearing in baseline or any recovered-code
menu's verified prefix; it is **not** a bounded 20-candidate pool.

| Test slice | N | Baseline first page | Merged first page | Merged top 20 | Union diagnostic | Clean-code top 20 |
|---|---:|---:|---:|---:|---:|---:|
| Within-syllable swap | 128 | 35 (27.3%) | 108 (84.4%) | 108 (84.4%) | 108 | 110 |
| Cross-boundary swap | 128 | 0 | 105 (82.0%) | 105 (82.0%) | 105 | 110 |
| Adjacent-key substitution | 128 | 0 | 100 (78.1%) | 100 (78.1%) | 105 | 110 |
| Missing letter, unsupported control | 118 | 18 (15.3%) | 17 (14.4%) | 19 (16.1%) | 19 | 102 |
| Repeated letter, unsupported control | 128 | 0 | 1 (0.8%) | 1 (0.8%) | 1 | 110 |
| Clean | 128 | 110 (85.9%) | **90 (70.3%)** | 110 (85.9%) | 110 | 110 |

For each supported error type, the canonical spelling was recovered in
**115/128 cases (89.8%)**, exactly the 115 whose spelling existed in the explicit
index. Conditional on that lexical coverage, recall was 115/115 for this small
sample. This is not evidence of 100% recall on arbitrary input. The proposed
≥95% eligible synthetic recovery gate is **not met unconditionally**.

Top-1 remains weak under the deliberately conservative merge: within-syllable
swaps improve from 26/128 to 37/128; neighboring substitutions from 0 to 3/128;
cross-boundary swaps remain 0. Where a raw menu existed its first entry never
moved; changes occur when the raw menu was empty. Improved first-page availability
usually means selecting a later candidate, not automatic correction with Space.

Dev results point in the same direction: first-page recall goes from 48/138 to
108/138 for within-syllable swaps, 0/134 to 96/134 for neighboring substitutions,
and 0/137 to 105/137 across boundaries. Clean dev recall falls from 110/138 to
85/138. Full counts, including top-1 and displaced candidates, are in the JSON.
These are descriptive synthetic results, not a confidence-backed real-user gain.

## What fails and why

**Clean-page protection fails.** Test #1 is unchanged in all 128 clean cases,
but 79 first pages change and 20 targets move out of the first five. No sampled
clean target is lost from top 20. The existing “exact first candidate wins” idea
is insufficient; preserve the exact first page, with correction alternatives
later, unless an independently validated policy permits promotion. Do not fix
this by merely raising confidence thresholds on the same held-out sample.

**Whole-word indexing misses valid compositions.** In dev, `changshicuowu` can
produce 尝试错误 at canonical rank 1, yet its whole spelling is absent from this
index, so neither its swap nor neighboring-key corruption recovers it. The
syllable/phrase graph can cover combinations that an explicit-word trie cannot.
An absent target in the clean top 20 also occurs (for example 白眶鸦雀); that is a
separate coverage/control failure, not proof that correction search is wrong.

**Pronunciation normalization still needs work.** The prepared CC-CEDICT reading
for 撒欢儿 yields `sahuanr`; it is not accepted as the same target in the captured
clean menu. Erhua representation must be reconciled with supported Rime spelling
rules. Do not quietly rewrite test labels or attribute such misses to a model.

**A recovered spelling can still lose in pooling.** Test neighboring-key union
recall is 105 while the bounded merge returns 100. In dev, `tongtou` recovers
`tongyou`, whose clean menu includes 通邮 at rank 4, but the final merge misses it.
A four-candidate correction quota and code-first ordering can starve competing
hypotheses. Better pooling/ranking may help after search, but cannot supply words
that were never recovered.

**Unsupported edits stay unsupported.** The repeat slice's single recovered
Chinese target does not mean insertion/deletion support: its canonical spelling
was never recovered. Alternative readings/segmentations can produce incidental
hits. Keep per-operation spelling recall separate from Chinese text coincidence.

## Lookup latency, allocations and memory

Measured standalone C++17 `-O3` on Apple M4, after touching all query paths.
The 6,653 unique queries include all incremental prefixes of the sampled raw
codes, not just final inputs. Measurements exclude dictionary parsing/loading,
Rime lookup, IPC, serialization, merging, rendering and session initialization.

| Lookup measurement | Initial run | Independent repeat after capture exited |
|---|---:|---:|
| p50 | 0.208 µs | 0.250 µs |
| p99 | 0.500 µs | 0.625 µs |
| p99.9 | 0.750 µs | 1.041 µs |
| Maximum | 3.333 µs | 2.042 µs |
| Intercepted C++ `new` allocations during queries | 0 | 0 |

Both runs returned identical results after removing timing fields. Search states
have p50 30, p99 67 and maximum 92. No query exhausted the 2,048-state cap; eight
prefix queries had more than eight corrected spellings and were truncated. The
cap fallback was separately tested, since this corpus did not exercise it.
After moving/formatting the native source, a fresh build also returned identical
non-timing results for every query; its binary hash is recorded separately in
[`final-source-binary-sha256.txt`](typo-recovery/final-source-binary-sha256.txt).

Memory is unacceptable as a shipping design: **3,546,925 dense trie nodes occupy
383,067,900 bytes**, before vector capacity, word strings and initialization
peaks. The repeated process's maximum RSS was **820,232,192 bytes (~782 MiB)**.
Its entire parse/build/warm/search/write run took 0.39 seconds, a different
measurement from warm lookup latency. See [raw timing](typo-recovery/native-time.txt).
A compact shared representation or reuse of Rime's existing prism is required.
These microbenchmarks do not establish integrated §3 key latency or zero native
candidate-allocation overhead; the shipping hot path was not changed.

## Reproducibility and isolation

The run lives in ignored `build/typo-recovery/run-v1/`. Checked-in summaries include
[source checksums](typo-recovery/experiment-source-sha256.txt),
[runtime file checksums](typo-recovery/runtime-files.json),
[profile identity](typo-recovery/profile.id) and [host details](typo-recovery/host.txt).
Go executables were built from this worktree. The local librime 1.17.0 binary and
prebuilt resources were copied and hashed; their native source lineage was not
independently rebuilt for this spike. Runtime system dependencies are not fully
captured by the file manifest.

Settings: full pinyin, five candidates/page, AI/emoji/traditional/mixed English
off, no fuzzy pairs; other compiler defaults unchanged. The ordinary baseline
schema has no enabled corrector. Builder validation keeps deployed schemas
warm-only and every translator's user dictionary disabled. Each capture process
owns a private scratch user directory and socket. No installed IME was touched.

An initial incomplete capture lacked the copied core OpenCC directory and was
aborted/excluded. The complete runtime includes `share/opencc`. A later valid
partial capture was resumed through four independent scratch processes; all
finished successfully, and collection verified all 2,051 expected codes with
matching profile IDs, no duplicates and no incomplete records. Capture concurrency
was not used for the repeated latency measurement.

Validation: Go tests/vet passed for experiment/data tools; menu-capture tests
passed. C++ tests passed ASan/UBSan and checked 10,000 deterministic randomized
queries against a brute-force oracle, malformed inputs, state exhaustion and
output-cap ordering. No model training, GPU inference or product integration.

## Next experiment

1. Revise the proposed selection policy to protect exact first pages, using
   native exact-path provenance (including sentence paths), not merely membership
   in the whole-word index. Measure typo availability and clean-page stability
   separately on a new evaluation iteration; retain this v1 result unchanged.
2. Prototype compact, syllable-aware recovery with raw-span alignment. Address
   missing/repeated letters and erhua normalization as distinct changes, with
   independent tests; do not infer support from incidental Chinese matches.
3. Resolve and prepare a contextual data source and a separately held-out real
   raw-typo set. Compare weighted channel + ERNIE against a jointly trained scorer
   only when those data exist. The current result supports recovery, not a neural
   model choice or immediate implementation of a free-generating IME.
