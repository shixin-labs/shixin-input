# Native implementation: ten entries per spelling

Status: implemented in the opt-in native experiment, tested 2026-11-06. Not
enabled in shipping settings or clients. The user accepted the measured **12.9 MiB
incremental sampled RSS**; the earlier proposed 8 MiB ceiling no longer blocks
this implementation. The broader paused goal was not resumed.

## Implementation

The [combined scratch librime patch](../../spikes/typo-recovery/native-graph/librime.patch)
now supports ranked spelling hypotheses through
`translator/shixin_spike_hypothesis_model`, with separate corrections enabled.
The old two-stage algorithm remains the control when that option is absent.
The new code is in [the fixed-workspace planner](../../spikes/typo-recovery/native-graph/hypothesis_policy.h),
[targeted corrector](../../spikes/typo-recovery/native-graph/bounded_corrector.inc),
and [bounded candidate pool](../../spikes/typo-recovery/native-graph/hypothesis_translation.inc).

1. Load a small, validated syllable-prior model during translator initialization.
   The offline Go exporter produces sorted spelling/score/abbreviation records.
   There is no inference, file access or model initialization during typing.
2. Rank single-swap and neighboring-key spelling hypotheses in fixed arrays.
   The best corrected spelling's parse chooses up to **eight** hypotheses for
   full pinyin or **sixteen** for abbreviated/mixed input. This is a heuristic
   inference of input form, not knowledge of the user's intent.
3. Bind each native correction graph to that hypothesis's edit. Raw input,
   candidate spans, edit alignment and native `Phrase` identity are preserved.
   No shadow session or alternative-string replay is used in the pipeline.
4. Consume at most **ten native entries per spelling**, including partial entries
   and entries later rejected by provenance checks. Never request an eleventh
   entry or refill after filtering. The existing compact three-initial cache has
   at most eight entries per exact spelling; that lane can return fewer than ten.
5. Offer the first phrase from each ranked spelling before second phrases.
   The existing correction filter deduplicates and publishes its bounded **eight
   display entries**. The retrieval pool can contain up to 160 entries; the
   current correction ABI and AI export have not been expanded to that size.

Planning and all spelling lookups share 32,768 work units, 2,048 nodes/chunks and
one 900 µs cooperative deadline. Failed or incomplete spelling batches are
discarded. Completed earlier batches survive later exhaustion; finalization
moves at most 160 pointers and starts no further lookup. Ordinary input remains
available. This fixes an initial integration bug that discarded completed batches
when a later spelling exhausted the budget.

Apostrophes are hard boundaries: edits cannot change or cross them. The planner
may propose swaps across inferred syllable boundaries, but the native adapter
only realizes edits representable in one correction edge. Three-initial input
prefers repairs that retain that form, with a two-log-unit penalty for mixed
interpretations; those alternatives remain eligible. Exact initial-cache lookup
avoids searching all neighboring spellings again for each selected hypothesis.
This fixed the observed `xgr` → `中国人` regression during integration.

The ten-entry cap limits translation consumption, **not** all internal dictionary
chunk collection/sorting to ten. Those operations remain under the shared work,
node/chunk and deadline limits. Native graphs and candidates still allocate.

## Validation and measurements

| Check | Result |
|---|---|
| Native policy tests | Five tests pass in Release and ASan/UBSan: model rejection, deterministic edit examples, input-form limits, exact initial bucket lookup, exhaustion, per-spelling caps, fair publication and idempotence. Includes 256 malformed-model inputs and 1,000 randomized input traces. |
| Planner allocation check | Zero C++ heap allocations during the measured query after loading the model. This does not assert zero allocations for native graphs/candidates. |
| Real dictionary integration | 80 traces pass in Release and ASan/UBSan, comparing up to 100 ordinary entries. Swap, neighbor, apostrophe and three-initial correction selection, traditional conversion, emoji, native identity and stale-token rejection pass. |
| Learning persistence | Five private writer/exporter cases pass: neighbor, swap, existing native alias, emoji and unselected suggestions. Eight selections persist the intended native reading; cancellation learns nothing. |
| Final engine latency | 2,280 keys: p50 **104 µs**, p99 **1.28 ms**, p99.9 **1.28 ms**, maximum **1.346 ms**. Existing total-engine gates pass. Earlier implementation sample: p50 112 µs, p99 1.408 ms, p99.9 1.443 ms. |
| Go engine allocation baseline | `BenchmarkKey`: **21.97 µs/key**, 0 B/op, 0 allocations; English punctuation: **21.75 µs/key**, 0 B/op, 0 allocations. Correction-off Go baseline, not a native-allocation claim. |
| Sampled process RSS | Same 1,500-composition workload: policy maximum **65,765,376 bytes**, ordinary maximum **52,215,808 bytes**; difference **13,549,568 bytes (12.92 MiB)**. Accepted by the user for this measured workload. Not a transient-peak or full session-pool measurement. |
| Reproduction | Applying the retained patch to pinned librime `3438a3b860cd3525f25ee2198c35ea49a3f2d85d` reproduces all 27 patched native source files exactly. The unrelated head-weight sidecar experiment is excluded. |

The sanitizer build uses the existing expanded clock allowance for functional
checks; operation caps remain unchanged. Performance numbers above come from
Release with the real deadline. AI was off; no new hold was introduced.

## Paired corpus result

731 raw strings / 830 synthetic development labels, two traces each. Both arms
use the same library and dictionaries: the prior two-stage separate stream versus
the opt-in spelling policy. Target availability counts the ordinary first five
plus full-span candidates among the eight published corrections.

| Trace | Previous policy | New policy | Individual gains | Individual losses |
|---|---:|---:|---:|---:|
| First | 313/830 | **341/830** | 43 | 15 |
| Second | 313/830 | **337/830** | 41 | 17 |

The only ordinary-menu differences are the live time/timestamp outputs for `sj`
and `ts`; all other captured ordinary first pages match. Separate correction
availability improves overall, but the new policy is **not a strict coverage
superset**. The retained per-case loss list is the next ranking/recall diagnostic.
These repeatedly used synthetic labels are not real-user or held-out accuracy.

Do not compare these totals directly with the [offline policy experiment](typo-hypothesis-caps.md):
that test used the ordinary first ten, the entire 10×K correction pool, no shared
native retrieval deadline, and a slightly different input-form ranking heuristic.
The native result includes graph representability, exhaustion and the eight-entry
publication limit. Cold/warm success is not guaranteed merely by considering more
spellings.

## Reproduce and inspect

Follow the [native setup instructions](../../spikes/typo-recovery/native-graph/README.md#ranked-spelling-hypotheses-opt-in-native-implementation).
The model used here is retained as [forms.tsv](typo-hypothesis-native/forms.tsv).
Only private warm generations and scratch user dictionaries are used.

```sh
CGO_ENABLED=0 go test ./spikes/typo-hypotheses
CGO_ENABLED=0 go vet ./spikes/typo-hypotheses
CGO_ENABLED=0 go test -c -tags shixin_typo_spike -o build/typo-recovery/bin/hypothesis-rime.test ./internal/rime
CGO_ENABLED=0 go test -c -o build/typo-recovery/bin/hypothesis-capture.test ./tools/menu-capture
```

With `SHIXIN_TYPO_SPIKE_ROOT` set to the prepared runtime and
`SHIXIN_TYPO_SPIKE_GENERATION` to its `hypothesis-native-v1/generation`, run
`TestTypoLearningPersistenceSpike` in the first binary and
`TestTypoNativeSpikeLatency` in the second. The RSS test additionally takes
`SHIXIN_TYPO_SPIKE_VARIANT=protected` or `ordinary`. Corpus capture uses
`TestTypoCorrectionCorpusSpike` and a new `SHIXIN_TYPO_CORPUS_OUTPUT` path.
Native fixtures are `hypothesis_policy_test.cc` and `separate_stream_test.cc`,
linked against the matching Release or sanitizer library and headers.

Evidence: [corpus cohorts and losses](typo-hypothesis-native/corpus-summary.json),
[final latency](typo-hypothesis-native/latency-final.log),
[policy RSS](typo-hypothesis-native/memory-policy.log),
[ordinary RSS](typo-hypothesis-native/memory-control.log),
[learning](typo-hypothesis-native/learning.log),
[Go allocations](typo-hypothesis-native/go-allocation.log),
[native Release](typo-hypothesis-native/separate_stream-trace-out.log),
[native sanitizers](typo-hypothesis-native/separate_stream-trace-asan.log),
[policy/stress sanitizers](typo-hypothesis-native/hypothesis_policy-trace-asan.log)
and [artifact hashes](typo-hypothesis-native/sha256.txt).

The remaining product work is client/settings integration and deliberate review
of the coverage losses, not further memory optimization toward the old 8 MiB
proposal. No shipping configuration or client protocol was changed by this work.
