# Abbreviated pinyin: native retrieval and ERNIE baseline

Date: 2026-11-04 · Offline pilot, not a product change

Design: [abbreviation handling](../typo-tolerant-pinyin.md#abbreviation-is-intentional-input-not-deletion-noise).
Code and reproduction: [spikes/typo-recovery](../../spikes/typo-recovery/README.md#abbreviation-baseline).

**Result:** existing ERNIE can help abbreviated input: useful context promotes
休假 for `xj` from #45 to #1. Retrieval remains the main gap in this pilot:
only 16/34 clean test initials targets appear in the first 50, and none of the
sampled adjacent-key typo targets do. Prioritize bounded abbreviation-aware
candidate recovery before deciding whether to replace the scorer.

## Scope and data

This follows the [full-spelling rerank experiment](typo-rerank.md). It tests
existing native retrieval and the existing ERNIE scorer on intentional
abbreviations, then on errors applied to those abbreviated forms. **It does not
implement abbreviation typo recovery or train a model.** No gold spelling is
inserted into a candidate pool.

Selected 25 dev and 25 test families from the existing CC-CEDICT pilot by fixed
family hash, without consulting context availability or model outputs. They
contain 29 dev and 35 test phrase/readings. The source remains CC-CEDICT,
CC BY-SA 4.0; attribution, snapshot and original splits are documented in the
[data report](typo-data-preparation.md). This is another pilot on previously
examined families, not a new untouched final benchmark.

The compiled schema contains both `abbrev/^([a-z]).+$/$1/` and
`abbrev/^([zcs]h).+$/$1/`. Preparation generates:

- Full spelling as the control.
- First-letter initials; also `zh/ch/sh` initials when different.
- Two mixed masks: abbreviate alternate syllables, retaining the others in full.
- Clean forms plus one deterministic adjacent swap and one keyboard-neighbor
  substitution per form, chosen without menu/model feedback.

Each row retains canonical syllables, typed syllable pieces, the intended typed
form, raw code, and a reversible edit. Intentional omitted letters cost no typo
edits. Duplicate forms within a reading are removed. Eight rows with cross-split
raw-code collisions were excluded from both splits; same-split ambiguous codes
retain multiple possible targets. Final data: **830 rows / 731 unique raw codes**,
336 KiB of local JSONL. These are synthetic forms/errors, not human typing traces.
No arbitrary abbreviation masks or all combinations were enumerated.

Five dev targets and five test targets have source-derived context carried over
from the earlier deterministic Duyu prefix selection. Empty context covers every
row; useful and stale context use this same small matched subset. Stale context
is the fixed unrelated `系统设置已更新，请重新启动应用。`. No new prompts were chosen
in response to model scores. Duyu provenance limitations still apply; raw
context/data remain in ignored `build/typo-recovery/abbr-v1/`.

## Capture and scoring

All 731 menus are captured with the prior warm-only generation/profile, up to
50 candidates each. Four processes use separate scratch user dictionaries with
learning disabled. Each whole-input match requires successful candidate-selection
verification, not just matching displayed text. Whole-input acceptance does not
reveal whether Rime used abbreviation, completion or another spelling derivation;
a missing target does not prove the spelling rule itself is unsupported.

Scoring starts only after every capture process succeeds. The pinned
ERNIE-4.5-0.3B-Base q8_0 model and unchanged standard policy run through the actual
`internal/ai.Scorer` on Metal. The model hash is verified before loading. There
are two offline scoring arms:

1. **Window AI:** reorder only the contiguous verified whole-input prefix, capped
   at 50 when raw input contains no `aeiouv`, otherwise 20; preserve the tail.
   With a partial first candidate, leave the whole menu unchanged. This mirrors
   the raw-code cap and contiguous-span restriction, but cannot reproduce native
   candidate types, native priors, or exact segment identity.
2. **Wide AI:** diagnostic up-to-50 pool for every form, with partial candidates
   frozen. This can expose ranking opportunities beyond the smaller window;
   it is not the shipped export behavior.

Both use the prior experiment's approximate `log(1 + source frequency)` priors,
not native `Phrase::weight`. Neither gets raw pinyin as model input. No typo
penalties, extra candidate generation, learning, live IPC or 16 ms hold are
introduced. Original menus are the AI-off control. Record target ranks even
when the target falls outside the movable window, and distinguish retrieval
(top 50), window availability, and top-1/top-5 ranking.

## Results

Evidence: [preparation](typo-abbreviation/preparation.json),
[summary](typo-abbreviation/summary.json),
[completion/model manifest](typo-abbreviation/manifest.json),
[paired full-spelling control](typo-abbreviation/paired-full-control.json),
[deadline counts](typo-abbreviation/deadlines.json),
[fallback diagnostic](typo-abbreviation/clean-deadline-fallback.json),
[source/binary hashes](typo-abbreviation/source-binary-sha256.txt).
All 731 captures and 1,062 condition/example scoring rows completed.

### Clean input: retrieval is already a substantial constraint

Test split, empty context. Top-5 refers to the labeled target; it is not a claim
that other homophones are incorrect. Different denominators reflect duplicate
forms and cross-split-code exclusions, not model-dependent filtering.

| Form | N | Retrieved in first 50 | In movable window | Original top-5 | Window AI top-5 |
|---|---:|---:|---:|---:|---:|
| Full spelling | 35 | 26 | 26 | 26 | 26 |
| First-letter initials | 34 | 16 | 14 | 8 | 7 |
| `zh/ch/sh` initials, additional forms | 13 | 9 | 9 | 7 | 6 |
| Mixed, even syllables abbreviated | 35 | 23 | 22 | 16 | 15 |
| Mixed, odd syllables abbreviated | 35 | 22 | 20 | 15 | 16 |

Nine full-spelling targets are also absent from their first 50 candidates, so
not every abbreviation miss is attributable to abbreviation alone. Restricting
to targets retrieved by their full-spelling control still leaves **16/25**
initials targets retrieved, compared with **23/26** and **22/26** for the mixed
forms. Two retrieved initials targets are outside the approximated contiguous
movable window. Better ranking alone cannot fix either unavailable candidates
or candidates excluded from its window.

Empty-context clean top-1 for initials changes from 4/34 to 2/34; mixed changes
11/35 to 10/35 and 11/35 to 11/35. These are tradeoffs of existing reranking with
approximate priors, **not regressions caused by new typo expansion**, since none
is present in this run. A future correction-on comparison needs the same
correction-off AI policy as its control.

### Useful context helps when the candidate is available

Matched test context subset; the two mixed rows reuse the same five targets and
are not ten independent samples. The initials row has four after a shared-code
exclusion; no additional digraph form exists in this subset.

| Form | N | Original top-5 | Useful-context window AI | Stale-context window AI |
|---|---:|---:|---:|---:|
| Full spelling | 5 | 5 | 5 | 5 |
| First-letter initials | 4 | 0 | 1 | 0 |
| Mixed even | 5 | 3 | 5 | 2 |
| Mixed odd | 5 | 3 | 5 | 3 |

For clean `xj`, **休假 moves from #45 to #1** with its source-derived prefix.
For mixed `qingy`, **情愿 moves from #12 to #4**; `qyuan` moves #7 to #1.
By contrast, the intended targets for `sl` (思量), `cj` (从军) and `qy` (情愿)
are absent from their first 50, so this scorer cannot promote them.

The wider 50-candidate arm is not uniformly better: on each useful-context mixed
slice it retains 4/5 targets in the first page, versus 5/5 for window AI. The
extra distractors and changed scorer pruning make “just increase the pool” an
experiment to measure, not a safe default. This small corpus does not establish
that 20 is an optimal limit.

### Typos of abbreviated forms need candidate recovery

Test retrieval in the first 50, before ranking:

| Typed form | Adjacent-key substitution | Adjacent swap |
|---|---:|---:|
| Full spelling control | 0/35 | 7/35 |
| First-letter initials | 0/35 | 0/33 |
| Additional digraph initials | 0/13 | 0/13 |
| Mixed even | 0/34 | 6/35 |
| Mixed odd | 0/35 | 7/35 |

These swaps are over typed characters; full or mixed swaps can cross syllable
boundaries. They are not all within-syllable edits. Native retrieval happens to
admit some swapped forms, but we cannot identify their spelling paths from UI
captures. No dedicated typo generator was enabled. The zero neighbor recall
holds for this synthetic sample only; it is not a universal statement about
Rime or real typing. Context cannot improve a zero-recall pool.

## Timing, memory and checks

Window-scorer timing on Apple M4/Metal, after five warm-up menus, excluding
prefill and IPC:

| Condition | Calls | p50 | p95 | p99 | Over 16 ms |
|---|---:|---:|---:|---:|---:|
| Empty | 830 | 5.74 ms | 13.96 ms | 21.89 ms | 30 |
| Source-derived | 116 | 6.30 ms | 8.67 ms | 13.94 ms | 0 |
| Stale | 116 | 6.07 ms | 8.91 ms | 14.08 ms | 0 |

These distributions include empty/single-candidate windows: only 442/830 and
81/116 windows respectively have more than one candidate. Wide scoring always
runs before window scoring, so this is not an unbiased head-to-head timing
benchmark. Source-context prefill p95 is 7.33 ms and p99 16.01 ms; repeated
variants usually reuse the prefill. The actual engine normally pre-fills earlier.
A scorer-only late-order fallback simulation leaves the clean test top-5 table
unchanged, but cannot establish compliance with the live 16 ms deadline.

The [whole-process measurement](typo-abbreviation/process-time.txt) records
808,632,320 bytes peak RSS and 457,639,496 bytes peak memory footprint. These are
separate OS counters, not quantities to add. They include the existing model,
Metal buffers, dictionary-prior map and offline evaluation data; they are not a
measurement of incremental product correction memory. No extra model or expanded
abbreviation trie was loaded. This pilot does not justify materializing every
abbreviation of every dictionary entry in production.

Tests cover aligned forms, reversible edits, deterministic preparation,
cross-split exclusion and bounded contiguous windows. Experiment, data,
menu-capture and rerank tests pass (40 tests); experiment vet and whitespace
checks pass. Native capture uses only isolated scratch sessions; no host IME,
production input code, model weights or live user dictionary changed.

## Design consequence and next experiment

Keep abbreviation as a zero-typo spelling path and retain current ERNIE as the
contextual baseline. Before selecting a new neural model, prototype bounded
search over native syllable/abbreviation transitions with one shared typo
budget. Measure **clean abbreviation retrieval first**, then mixed-path edits;
only then evaluate corrections on very short initials with their own regression
gate. Do not generate an expanded table of all abbreviation masks.

Compare correction-on and correction-off using the same AI policy, dictionaries
and native candidate weights/provenance. The next integration spike must export
those weights and raw-span metadata, establish how the target enters the bounded
window, and exercise partial selection, prefix typing and timeout fallback.
The current offline priors and small context subset are insufficient to choose
a replacement model or ship automatic correction of initials.

Follow-up: the [bounded recovery prototype](typo-abbreviation-recovery.md)
implements and measures this search direction offline. It recovers some typos
but fails coverage and clean-page gates; it is not a shipped feature.
