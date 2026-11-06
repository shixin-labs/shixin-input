# Constrained abbreviation search and a single-pass safe merge

Date: 2026-11-04 · Offline experiment; no shipping IME changes

Prune impossible or already dominated states **before putting them in the
bounded queue**. Reusing the packed index and all previous operation caps
reduces exhausted examples from **400/830 to 181/830**. Verified target proposals
increase from **240/830 to 342/830**. A separate ranking change preserves the
ordinary first page using just one model call; it loses no existing first-page
target across 1,062 evaluated rows. Recovery is still incomplete, and this is
not a production latency, learning or clean-input acceptance result.

Design: [typo-tolerant pinyin](../typo-tolerant-pinyin.md).
Controls: [previous recovery](typo-abbreviation-recovery.md) and
[packed storage](typo-packed-storage.md).
Evidence: [metrics](typo-constrained-search/summary.json),
[run manifest](typo-constrained-search/manifest.json),
[preparation](typo-constrained-search/preparation.json),
[search process](typo-constrained-search/search-process.txt),
[source/binary/output hashes](typo-constrained-search/source-output-sha256.txt).
The manifest's `guard` describes the older two-order diagnostic; the new
`single_pass_ai` metrics implement the one-call policy described below.

## Search change

The dictionary remains the same immutable packed syllable trie: 887,781 explicit
reading/text pairs, four zero-edit and four typo proposals per query. Full
syllables, one-letter initials and zh/ch/sh initials are alternative spellings
of a syllable; abbreviation itself costs zero. The supported error is one
neighbor substitution or adjacent swap, including swaps across inferred syllable
boundaries. Missing/repeated letters, fuzzy spelling and shuangpin remain outside
this experiment. Arbitrary letter permutations are not one swap.

Before enqueuing a state, reject it if its descendant score bound cannot beat
the filled result quotas, or if no next syllable initial can match the raw
input. The allowed initials account for an unused adjacent-key error, the next
raw letter in a possible swap, or the pending letter of a boundary-crossing
swap. Intersect with the dictionary's actual initial alphabet. Inspect small
edge ranges directly; binary-search larger sorted ranges. This avoids exploring
branches that cannot produce a legal continuation, without choosing a smaller
beam or discarding low-frequency words heuristically. Strict bound comparisons
preserve ties.

Limits remain 2,048 popped states, 8,192 inspected edges, a 1,024-state queue
and 4,096 deduplication slots. Every extra initial-lookahead inspection counts
against the same edge limit; it is not hidden work outside the cap. Any overflow
still discards all extra proposals and uses the ordinary menu. New workspace
metadata adds 24 bytes: **45,088 bytes** total. The queue is still necessary for
alternative unfinished paths; the improvement is avoiding impossible paths
before they consume queue space.

| Measurement | Previous | Constrained |
|---|---:|---:|
| Exhausted queries, including prefixes | 1,134 / 2,616 | 397 / 2,616 |
| Exhausted labeled examples | 400 / 830 | 181 / 830 |
| Verified target proposals | 240 / 830 | 342 / 830 |

Of the remaining exhausted queries, 55 hit queue capacity and 342 hit edge work.
No previously completed query now exhausts. Whenever both searches finish,
proposal output is identical. The gains come from finishing additional queries,
not changing the scoring function. These are correlated synthetic variants,
not 830 independent real-user typos.

Native canonical-code verification covers 1,101 codes: 566 cached compatible
captures and 535 new scratch captures. No proposal is rejected. This verifies
whole-input dictionary acceptance, not raw-code graph alignment or selection.
The original baseline menu, split, contexts, dictionary, model and ranking
parameters are unchanged. No live user dictionary was opened.

## Reranking and clean-input safety

The safe arm computes the ordinary ERNIE order **once**, keeps its first five
entries, appends up to eight deduplicated verified proposals in channel order,
and fills from the remaining ordinary entries to a 50-entry ceiling. If the
ordinary menu has fewer than five entries, recovery starts immediately after
it. AI-off and timeout paths can apply the same operation to ordinary Rime order.
The offline runner also computes exploratory orders for comparison; the safe
arm does not consume those scores or require a second inference call.

On 152 clean test rows with empty context, the ordinary model has 70 target
hits in its first five. The safe merge retains all 70. Across all 1,062 rows
(empty, source and stale context), no ordinary first-page target is lost.
This follows structurally from retaining the existing entries, and does not
mean every clean input is correct. Tail entries can be displaced by the
50-entry experimental cap; whole-menu retention is not claimed.

By contrast, reranking the expanded pool with the existing fixed error penalty
loses **14** previously first-page clean test targets with empty context. More
recovery makes unrestricted reranking a greater regression risk here. Context
cannot be relied on to resolve ambiguous correct spellings or abbreviations.

The cost of protection is extra candidate navigation:

| Empty-context test subset | Rows | Ordinary AI top-5 | Safe top-5 | Safe top-20 |
|---|---:|---:|---:|---:|
| Full-pinyin neighbor typo | 35 | 0 | 1 | 23 |
| Full-pinyin swap | 35 | 7 | 10 | 24 |
| Initials neighbor typo | 35 | 0 | 0 | 3 |
| Initials swap | 33 | 0 | 0 | 4 |

Initials recovery is still weak. The experiment uses approximate frequency
priors instead of native Phrase weights, a 50-candidate diagnostic pool instead
of the normal full/mixed export limit, and previously examined dev/test families.
Source-context test coverage remains just five target phrases. These results
do not justify training or selecting a new model, removing the guard, or
claiming real-typo accuracy. A jointly trained typo/ranking model remains a later
ablation after retrieval and native integration are reliable.

## Performance and verification

Standalone warm search measured p50 **0.0662 ms**, p99 **0.1905 ms**, p99.9
**0.2166 ms**, maximum **0.2316 ms**, with zero intercepted C++ `new` allocations.
The same packed index retains about **51.93 MiB** of major buffers; the fresh
search process peaks at **55.05 MiB RSS**. These exclude Rime and model memory.
The earlier packed-load p99 was 0.1568 ms: stronger pruning is not a demonstrated
speed improvement. Measurements were sequential, before native captures/model
scoring; integrated per-key budgets remain untested.

ASan/UBSan passes 10,000 randomized comparisons against independently expanded
full/abbreviated spellings and a brute-force one-edit oracle. Fixtures exercise
boundary swaps, low work budgets and unsupported apostrophes. The packed-image
suite checks constrained and ordinary searches across plain/packed/reloaded
representations and malformed images. Go tests and vet pass for the spike and
data tool. A merge test covers ordinary menu lengths 0–50, duplicate proposals
and partial candidates, asserting exact retention of the first five entries.

## Next gate and reproduction

Before production, retain an unchanged ordinary native translation stream and
a separate bounded recovery stream within the same session. Preserve candidate
identity, raw spans, partial selection, learning, pins and conversion behavior.
Do not replay alternative strings in shadow sessions. Use the configured page
size and the existing single ≤16 ms AI hold; display only the final assembled
menu. Native allocation/latency and scratch-engine learning tests remain needed.
The duplicate 51.93 MiB index and 181 exhausted examples are unresolved gates.

The [spike README](../../spikes/typo-recovery/README.md) documents preparation,
private captures and scorer configuration. Use a new output root and the previous
packed image:

```sh
clang++ -std=c++17 -O3 -Wall -Wextra -Werror \
  spikes/typo-recovery/native/abbreviation_search.cc \
  -o build/typo-recovery/bin/abbreviation-constrained-search
build/typo-recovery/bin/abbreviation-constrained-search \
  build/typo-recovery/storage-v2/packed.bin \
  build/typo-recovery/abbr-recovery-v1/queries.txt load-constrained
clang++ -std=c++17 -O1 -g -fsanitize=address,undefined \
  -fno-omit-frame-pointer spikes/typo-recovery/native/constrained_search_test.cc \
  -o build/typo-recovery/bin/constrained-search-test
build/typo-recovery/bin/constrained-search-test
```

Save search output as `search.jsonl` alongside the unchanged parent selected
rows, menus, queries and contexts. Run `abbr-recovery-plan`, isolated capture,
`collect` and `abbr-recovery-evaluate` as in the parent experiment. The evaluator
now emits `single_pass_ai_rank` alongside its historical comparison arms.
