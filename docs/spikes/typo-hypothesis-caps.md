# Ten candidates per corrected spelling

Status: completed offline policy experiment, 2026-11-06. Not product activation.
The broader goal remains paused; this is the separately requested bounded test.

Follow-up: the user authorized [native implementation](typo-hypothesis-native.md),
which is now tested, and accepted its measured 12.9 MiB incremental sampled RSS.
The results below remain the original offline policy comparison.

**The user's proposal helps coverage:** rank corrected spellings first, retain
more spellings, and take at most ten native entries from each. Eight spellings
are sufficient for the full-pinyin coverage observed here; sixteen improve
abbreviated and mixed-input coverage. Ten entries per spelling lose some
abbreviation targets compared with twenty. This does not yet establish a
latency or memory improvement in the native correction pipeline.

## Results

Reused synthetic development set: 830 labels, 731 distinct raw strings, including
553 swap/neighbor typo labels and 277 clean labels. No new model or AI scoring.
The table counts a target if it appears in the ordinary first ten or the verified
full-input correction pool. Ordinary input alone covers 39/553 typo labels.

| Ranked spellings | Entries per spelling | Typo targets available | Targets lost versus 20 per spelling | Mean distinct correction phrases per typo |
|---|---:|---:|---:|---:|
| 2 | 10 | 227/553 (41.0%) | 18 | 9.8 |
| 4 | 10 | 265/553 (47.9%) | 25 | 18.2 |
| 8 | 10 | 309/553 (55.9%) | 28 | 32.4 |
| 16 | 10 | 343/553 (62.0%) | 29 | 51.1 |

| Input form, typo labels only | Ordinary first ten | 8 spellings × 10 | 16 spellings × 10 | 16 spellings × 20 |
|---|---:|---:|---:|---:|
| Full pinyin | 15/128 | 106/128 | 106/128 | 106/128 |
| Single-letter initials | 0/123 | 24/123 | 41/123 | 51/123 |
| Digraph initials | 0/48 | 24/48 | 28/48 | 30/48 |
| Mixed, even positions abbreviated | 13/127 | 81/127 | 86/127 | 97/127 |
| Mixed, odd positions abbreviated | 11/127 | 74/127 | 82/127 | 88/127 |

At the same maximum output count of 160 entries, **16 × 10 finds 343 targets
versus 337 for 8 × 20**: 34 gained and 28 lost, not a strict improvement for every
input. Equal output counts do not imply equal lookup work or memory use.

The intended corrected spelling survives ranking in 354, 439, 506 and 548 of
553 typo labels at 2, 4, 8 and 16 spellings respectively. Thus at sixteen, the
remaining gap is mostly downstream of spelling recovery: dictionary/menu
availability and the per-spelling candidate cap. This heuristic alone cannot
resolve the ambiguity of abbreviated input.

## Method and bounds

The [standalone Go experiment](../../spikes/typo-hypotheses/README.md) enumerates
one adjacent transposition or QWERTY neighboring-key substitution. A dynamic
program scores legal full/initial/mixed segmentations using dictionary-derived
syllable priors, an abbreviation penalty and edit cost. It uses no target label
to rank hypotheses. This simple prior favors fewer syllables and is an experimental
baseline, not a calibrated typo probability model.

The planner retains at most sixteen spellings, with a 32,768-transition limit
and 900 µs deadline. All 731 queries completed: maximum 6,251 transitions;
observed planner-only p50 10.0 µs, p99 75.5 µs, maximum 93.3 µs. These timings
exclude native dictionary retrieval, session replay, selection verification, AI
and IPC. They are **not end-to-end key latency measurements**. The experiment
allocates Go memory and is not suitable for direct use on the engine key path.

For each spelling, take the first ten raw native menu entries; count only phrases
verified by exact whole-input commit with no remaining preedit. Partial candidates
consume slots, with no deeper refill. Deduplicate Chinese text across spellings.
The twenty-entry control uses the same captured prefix. This is an upper cap of
ten qualifying phrases, not a guarantee of ten. No unlimited candidate refill.

Whole-string edit generation includes swaps across inferred syllable boundaries,
so this is broader than the proposed first-version within-syllable scope. It does
not prove correct raw-span alignment or learning when selecting from typo input.
Explicit apostrophes, shuangpin, missing/extra letters and multiple edits are not
tested. These synthetic development families have been reused; percentages are
not held-out or real-user accuracy.

## Capture and validation

Captured all 10,077 unique raw/hypothesis strings using the original stable scratch
library, learning-disabled warm schema and a private temporary user directory.
All librime calls stay on one locked OS thread. The final capture reuses a session,
clearing composition and draining commits before each replay. Candidate selection
is verified individually. It matches candidate text/order and full-span flags for
all 105 IPC-capture rows and all 123 fresh-session native rows cross-checked.

An initial IPC attempt omitted the prebuilt dictionary path, producing empty
menus; that output was rejected and excluded. The corrected IPC capture and
fresh-session native capture were stopped after collecting cross-check rows,
because repeatedly constructing sessions made the full batch unnecessarily slow.

Three focused tests pass: swap/neighbor generation and exhaustion fallback;
ten-per-spelling limits, deduplication and no refill; bounded ordered hypotheses
and invalid-input rejection. `go vet` passes. No production hot path changed;
no production latency/allocation or RSS result is claimed.

Evidence: [preparation](typo-hypothesis-caps/preparation.json),
[cohort counts and per-case losses](typo-hypothesis-caps/summary.json),
[cross-check counts](typo-hypothesis-caps/crosschecks.json),
[equal-output comparison](typo-hypothesis-caps/equal-output-budget.json),
[source/output hashes](typo-hypothesis-caps/inputs.sha256) and
[runtime hashes](typo-hypothesis-caps/runtime.sha256).
Large local captures remain under `build/typo-recovery/hypotheses-ten-v1/`.

## Recommendation

Keep **ten per corrected spelling** as the next native experiment's cap. Evaluate
eight spellings for full pinyin and up to sixteen for initial/mixed input, subject
to one shared operation/deadline budget. Do not increase the deadline to reach
the requested count; exhaustion keeps ordinary input available for that update.

This replaces the earlier eight-total-suggestions assumption for retrieval policy
evaluation. The number presented to the user and optional AI export remain separate
bounded decisions; the existing AI export must not silently expand to 160 rows.
Ordinary candidates stay separate and unchanged by construction here. This is not
a UI regression test or evidence that automatic top-1 promotion is safe.

The next implementation question is whether the native dictionary can stop work
early for each spelling, including chunk collection/sorting. Taking ten *after*
expensive expansion would not address the observed exhaustion or memory footprint.
Validate that cost before adopting this policy; no further implementation is part
of this completed test.
