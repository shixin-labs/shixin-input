# Recovery slot allocation and combined-alignment search

Date: 2026-11-04 · Offline experiments; the problem is not yet fixed

The [failure audit](typo-failure-audit.md) motivated two controlled experiments.
Allocating more of the same eight proposal slots to typo recovery improves
candidate availability modestly. Combining alignments at dictionary nodes does
not improve the bounded search in this prototype and is rejected for adoption.
Neither result establishes production correctness or real-user accuracy.

## Same eight slots, different allocation

The native spike now accepts a clean-recovery quota from 0 to 8; typo recovery
gets the remainder. The default remains 4/4. Bounds and insertion use the same
quota, with no increase in result capacity, queue capacity or work limits. The
same packed image still retains only four words per exact segmented reading;
these runs do not claim an exhaustive top-eight list from the original lexicon.

Development selection compared all nine allocations by the union of ordinary
native whole-input candidates and search proposals. The ordinary list is not
changed. Both 0/8 and 1/7 reach 244/375, versus 235/375 for 4/4; choose 1/7 to
retain one zero-edit recovery hypothesis. This choice was recorded before
examining the test aggregates, but the underlying test families have already
been examined in previous experiments, so this is not fresh held-out evidence.

| Metric | 4 clean / 4 typo | 1 clean / 7 typo |
|---|---:|---:|
| Development candidate union | 235 / 375 | 244 / 375 |
| Test candidate union | 213 / 455 | 219 / 455 |
| Development typo-row union | 127 | 135 |
| Test typo-row union | 113 | 119 |
| Exhausted examples, both splits | 181 / 830 | 185 / 830 |
| Exhausted queries including prefixes | 397 / 2,616 | 419 / 2,616 |
| Standalone warm search p99 | 0.1860 ms | 0.1848 ms |

These are **pre-verification union counts**, not final top-5, model accuracy or
native-corrected selection counts. Newly proposed canonical codes have not yet
been captured, the final 50-entry merge cap is not applied here, and no new
model run was needed. One result can replace another; aggregate gains do not
prove every old recovered target is retained. The ordinary first-page guard
remains required. Standalone C++ `new` interceptions remain zero; integrated
engine latency is still untested.

The default 4/4 output, including work counters and exhaustion flags, is
byte-identical to the preceding constrained run after excluding timings. This
controls for accidental behavior changes while parameterizing quotas.

## Alternative tested: combine input alignments per dictionary node

The experimental `load-dp` search stores four position bitsets at a node:
zero edits, a completed swap, a neighboring-key substitution, and a pending
boundary-crossing swap. Each node has one parent, so its parent can combine
all legal alignments before enqueuing the node once. A pending swap's carried
letter is derivable from the raw bytes at position minus two. This removes the
separate visited-state hash table and uses a **40,968-byte workspace** instead
of 45,088 bytes. Dictionary storage is unchanged.

The 1/7 DP run exhausts **441/830 examples** and **1,238/2,616 prefix queries**;
its candidate unions fall to 195/375 development and 184/455 test. Reject this
version. It has zero intercepted allocations and p99 0.1668 ms, but returning
fallback more often is not a useful latency improvement. This prototype charges
binary probes and alignment visits to its 8,192-unit work cap; the earlier
search counter counts matched edges and lookahead inspections. Both are bounded,
but their counters are not identical operations, so the experiment does not
prove all dynamic-programming approaches are inferior.

Sanitizer tests compare both implementations to independently expanded
full/abbreviated spellings and brute-force one-edit matching across 10,000
randomized queries and all quota sizes. Work exhaustion discards proposals;
zero work and unsupported apostrophes have explicit tests. The existing
reference search and packed/reloaded-image sanitizer suites also pass. Go tests
and vet pass. No production search component or settings changed.

## Reading coverage follow-up

The 153 absent-reading examples span 12 Chinese targets. Direct source checks
show that the qingyuan place-name entries (including 庆元 and 清远) are commented
out in the rime-ice base source. They were correctly excluded by preparation;
this is not a parser accidentally dropping active entries. Do not silently
uncomment dictionary source lines to fit the test labels.

Erhua is a separate mismatch: the corpus labels 大伙儿 as `da huo r`, while the
explicit dictionary has `da huo er`. Native canonical capture does recover
傻大个儿 under its corpus reading, illustrating that native spelling/composition
can cover cases absent from the standalone exact-reading index. Eleven of the
12 targets are not in the first 50 whole-input native candidates under their
labeled full code. That does not prove absence at every rank.

The next coverage work must distinguish spelling aliases, native sentence
composition and actual absent vocabulary. An independent duplicate word index
cannot be assumed to reproduce native retrieval. The design's native syllable
integration is therefore still necessary, alongside bounded search and candidate
selection. More queue memory or training the current reranker alone is not a
verified fix for these coverage gaps.

## Reproduction and evidence

[Comparison metrics and input hashes](typo-quota-allocation/comparison.json),
[development selection](typo-quota-allocation/selection.json),
[DP storage counters](typo-quota-allocation/dp-storage.json), and
[source/image/binary hashes](typo-quota-allocation/sha256.txt) retain evidence.
Source hashes include final formatting/test edits; binaries identify the actual
measurement executables. See the [spike README](../../spikes/typo-recovery/README.md)
for the shared preparation and packed-image build.

```sh
clang++ -std=c++17 -O3 -Wall -Wextra -Werror \
  spikes/typo-recovery/native/abbreviation_search.cc \
  -o build/typo-recovery/bin/quota-search
# Save each run separately; default quota remains four.
build/typo-recovery/bin/quota-search \
  build/typo-recovery/storage-v2/packed.bin \
  build/typo-recovery/constrained-v1/queries.txt load-constrained 1
# Use load-dp for the rejected combined-alignment alternative.
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode abbr-quota-report \
  -root build/typo-recovery/quota-v1 \
  -source-run build/typo-recovery/constrained-v1
```

The report expects `search-0.jsonl` through `search-8.jsonl`, plus `dp-1.jsonl`
and `dp-4.jsonl`, all from the same image/query inputs. It emits search-only
counts and labels their limitations; it does not invoke a model or native engine.
