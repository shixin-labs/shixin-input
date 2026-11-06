# Typo recovery retest with the existing ERNIE reranker

Date: 2026-11-04 · Apple M4 / Metal · Offline experiment, not a shipped change

Prior experiment: [bounded recovery](typo-recovery.md).
Code: [spikes/typo-recovery](../../spikes/typo-recovery/README.md).
Evidence: [summary](typo-rerank/summary.json),
[model/policy manifest](typo-rerank/manifest.json),
[context-selection manifest](typo-rerank/contexts-manifest.json),
[diagnostic fixtures](typo-rerank/fixtures.json) and
[source/runtime hashes](typo-rerank/source-runtime-sha256.txt).

## Result

**Context-aware reranking does help.** With `这个孩子很` preceding clean `taoren`,
the shipped ERNIE scorer moved 讨人 from the merged menu's rank 6 to rank 1.
On the small source-context test subset, all five clean targets displaced from
page one by the original merge returned to page one: **14/19 → 19/19**.

That does not establish safe unconditional correction. Without context, AI
reduced the number of displaced clean test targets from 20 to 11, but did not
eliminate it. A fixed typo penalty reduced that number to five. Stale context
also produced regressions. The earlier result rejected blind insertion, not
contextual reranking; this retest supports keeping the existing model as the
first contextual baseline rather than assuming a new model is necessary.

## What was actually run

Used the same 1,568 synthetic dev/test examples, captured Rime menus, eight-code
recovery limit and four-correction/20-candidate merge from the first experiment.
No new candidate retrieval, new training or parameter fitting was performed.
These examples do not evaluate intentional initials or mixed abbreviation.
Their treatment and the required next data slices are specified in the
[abbreviation design](../typo-tolerant-pinyin.md#abbreviation-is-intentional-input-not-deletion-noise).
The subsequent [abbreviation baseline](typo-abbreviation.md) measures those
forms separately; its results must not be pooled with this experiment.

The scorer is the actual `internal/ai.Scorer`, including its first-token pruning
and `internal/rerank.Order`, with the pinned **ERNIE-4.5-0.3B-Base q8_0** model.
Its SHA-256 matched `internal/models.ERNIE`. The existing standard policy was
used unchanged: beta 0.3, promotion margin 0.5, page size 5, short penalty 3.

Five arms are reported on each condition:

1. Original captured menu, AI off.
2. Typo-augmented menu, AI off.
3. Original menu + existing AI (necessary control for AI's own changes).
4. Typo-augmented menu + existing AI.
5. Typo-augmented menu + existing AI and a fixed spelling-error penalty.

The penalty was fixed **before scoring**: subtract 1.6 log-score units for a swap
or 2.0 for an adjacent-key substitution. It is applied once through the scorer's
prior term; exact candidates receive no error penalty. It is a diagnostic
ablation, not a selected production calibration. Both AI arms can promote a
correction over #1; the exact-first-page guard was deliberately not imposed, so
this experiment can test whether the model alone repairs the regression.

### Native-export limitation

The old UI captures do not contain native `Phrase::weight` or complete candidate
flags. This is a rerun of the **existing model/scoring policy on offline pools**,
not an exact reproduction of the live native export path:

- Prior proxy: `log(1 + dictionary frequency)` from the first matching rime-ice
  table; absent Chinese strings get prior 0. This differs from native phrase and
  sentence weights. Of 8,145 movable-candidate occurrences across conditions,
  937 (11.5%) lacked a source-frequency prior.
- Whole-input candidates are movable; partial candidates are frozen with
  `EntryFixed`. Shorter-prefix reranking and user learning are not evaluated.
- Candidate spans from corrected input are still offline acceptance probes,
  not mapped raw-input graph edges. The product integration remains unimplemented.
- Scoring runs directly through the existing Go package on a locked OS thread
  with Metal. It does not exercise engine IPC or the live 16 ms hold.

## Context conditions and leakage precautions

**Empty:** all original 1,568 rows; nothing preceding the candidate.

**Source-derived:** scan the downloaded Duyu/Pinyin-Hanzi CSV for source sentences
containing each target. Keep punctuation-bearing sentences of 12–200 runes with
at least six preceding Han characters. Select the smallest deterministic hash of
`context-v1/target/sentence`, without consulting model scores. Send only the last
64 runes *before the first target occurrence*; never send the target, the rest of
the sentence, or the expected answer as a prompt. This yielded prefixes for
**48/265 targets: 29 dev and 19 test**, and 281 total variant rows. The original
family splits are retained. The same target's variants share context.

These are substring-derived contexts from a secondary corpus, not observed IME
sessions. Matching a target selects an appropriate-context subset; it is not a
random sample of user contexts. Source provenance/annotation quality and model
pretraining overlap are not independently validated. Do not treat this as a
release benchmark or compare its percentages directly to the entire empty set.
Only its prefix and source-line/sentence hash are retained locally for audit.

**Stale:** the exact same 281-row subset, but replace every prefix with the fixed,
unrelated `系统设置已更新，请重新启动应用。`. This tests one stale-context scenario,
not all possible editing/focus histories.

Three hand-authored `taoren` prompts were selected before inference and reported
separately; none contributes to aggregate metrics. The source-derived prefix
for 讨人 was `小姑娘的脸蛋：`; that also promoted it from rank 6 to rank 1.

## Clean-input regression

Counts are intended target within the first five, with verified whole-input
acceptance. Multiple homophones can remain legitimate candidates; this measures
retention of the labeled target, not an infallible judgment of user intent.

| Test condition | N | Original | Merge, no AI | Original + AI | Merge + AI | Merge + AI + penalty |
|---|---:|---:|---:|---:|---:|---:|
| Empty context | 128 | 110 | 90 | 110 | **99** | **105** |
| Source-derived context | 19 | 19 | 14 | 19 | **19** | **19** |
| Stale context, same subset | 19 | 19 | 14 | 19 | **16** | **18** |

On source-derived test contexts, clean top-1 is 13/19 originally, 17/19 with
original-menu AI, 16/19 with augmented-menu AI, and 17/19 with the penalty.
Thus first-page recovery alone still hides a top-1 tradeoff in the unpenalized arm.
On empty-context clean inputs, the augmented AI arms have target top-1 of 60/128
and 66/128, versus 83/128 for original-menu AI. A better first-page retention
score does not mean a safer automatic Space commit.

Dev clean first-page counts agree directionally: with source-derived context,
merge alone retains 24/29, both AI variants 29/29. Empty-context dev retains
85/138 with merge alone, 93/138 with AI, and 100/138 with the penalty, versus
109/138 for original-menu AI. These small, correlated family samples do not
satisfy the design's clean-input false-correction gate.

## Typo selection with context

Held-out source-context subset; each supported slice has 19 rows. Values are
**top-1 / top-5**. The baseline-AI column isolates what a model can do without
recovering missing spellings.

| Error | Original + AI | Merge, no AI | Merge + AI | Merge + AI + penalty |
|---|---:|---:|---:|---:|
| Within-syllable swap | 6 / 6 | 5 / 19 | **14 / 19** | **14 / 19** |
| Cross-boundary swap | 0 / 0 | 0 / 19 | **11 / 19** | **11 / 19** |
| Adjacent-key substitution | 0 / 0 | 1 / 18 | **14 / 18** | **14 / 18** |

Recovery supplies candidates the original menu lacks; context then often moves
the intended one to #1. Without context, augmented-menu AI still improves test
top-1 to 70/128 for within-syllable swaps, 54/128 across boundaries, and 60/128
for neighboring-key substitutions, but it causes the clean regressions above.

Missing/repeated letters remain outside the generator's supported operations.
The contextual repeated-letter slice remains at zero target recall; reranking
cannot invent a missing candidate. No separate typo-specific model was tested.

## The concrete `taoren` example

| Prefix | After merge | Existing AI | AI + penalty |
|---|---:|---:|---:|
| None, target 讨人 | #6 | #6 | #5 |
| `小姑娘的脸蛋：`, source-derived | #6 | **#1** | **#1** |
| `这个孩子很`, diagnostic fixture | #6 | **#1** | **#1** |
| `医生在处方中写下了中药`, target 桃仁, diagnostic fixture | #1 | **#1** | **#1** |

This directly confirms the user's suggestion for a favorable context. It does
not prove that a model can recognize whether its stored context is trustworthy.

## Timing and validation

After a five-menu warm-up, scorer-only times for augmented pools:

| Condition | Calls | p50 | p95 | p99 | Calls exceeding 16 ms |
|---|---:|---:|---:|---:|---:|
| Empty | 1,568 | 5.90 ms | 7.79 ms | 8.91 ms | 4 |
| Source-derived | 281 | 6.10 ms | 7.98 ms | 9.34 ms | 1 |
| Stale | 281 | 6.00 ms | 8.07 ms | 18.66 ms | 3 |

Prefill is measured separately: source-context p95 8.30 ms, p99 15.84 ms, with
most repeated target variants reusing already-prefilled text. Do not add or omit
prefill blindly when estimating live latency: the engine normally prefills
committed context in advance. This harness applies neither the app's scheduling
priority nor IPC/hold logic. The score tables include all completed orders;
actual late responses must fall back. A scorer-only 16 ms fallback simulation
leaves the clean test first-page counts in the table unchanged, but is not an
end-to-end deadline test. Timing arms ran in a fixed order, so their small
latency differences are not evidence that one policy is faster.

All 2,130 condition/example rows completed using the pinned model. Tests cover
prefix-only context extraction and preserving exact-path zero cost/minimum
correction cost; experiment and rerank package tests plus vet pass. No input
hot-path code, model weights, installed IME or user dictionary was changed.

## What this changes

Keep the existing ERNIE scorer as the first model for further experiments.
Context plus an explicit input-error prior deserves testing before a separate
joint model. Retain a deterministic exact-input protection/fallback path:
empty or stale context still regresses, and a timeout cannot be allowed to expose
an unsafe merge. The next meaningful evaluation needs native priors/provenance,
more independently sourced contexts, real raw typos and the actual hold/fallback
path. The full exact-first-page policy remains a conservative proposed guard;
this run does not silently replace it with the experimental penalty.
