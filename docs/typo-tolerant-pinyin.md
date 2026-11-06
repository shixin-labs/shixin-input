# Typo-tolerant pinyin — proposed design

Date: 2026-11-03 · Updated: 2026-11-04 · Status: proposed product, not shipped

Start with the [first-version decision brief](typo-v1-decision.md) for the current
recommendation, evidence limits and proposed review gates. The experiment summaries
below are historical; their outstanding-work statements describe their own revisions.
The [ten-per-spelling experiment](spikes/typo-hypothesis-caps.md) led to an
[opt-in native implementation](spikes/typo-hypothesis-native.md), now tested for
sampled latency, memory, clean-menu preservation and learning. The user accepted
its measured 12.9 MiB incremental sampled RSS; individual coverage losses remain
documented. The [experimental settings option](typo-experimental-option.md) now
places labelled corrections after page one using normal selection; it defaults
off. The broader model design below remains proposed.

Parent: [DESIGN §3](DESIGN.md#3-no-stall-architecture) and
[§7.2 智能排序](DESIGN.md#72-context-aware-reranking-智能排序).
Unless explicitly linked to measured experiment results, limits and acceptance
thresholds below are proposed targets. This document authorizes no change to
shipped defaults.

Use bounded native dictionary search for typo recovery, with a weighted keyboard
error model as the baseline. Reuse ERNIE for optional context ranking before
considering a new jointly trained correction/ranking scorer. Base recovery must
work without a neural-model download; free-form Chinese generation is not the
initial architecture.

The [native mirror experiment](spikes/typo-native-mirror.md) passes the focused
first-run editing fixtures with a compact shared cache, but the broader corpus
still has only 99/553 typo targets within thirteen candidates and eleven clean
later-page displacements. Longer mixed inputs also retain first-access misses.
These results justify neither shipping nor a claim that a new model solves retrieval.

The [canonical-cache experiment](spikes/typo-canonical-cache.md) verifies a
14.37 MiB compressed auxiliary dictionary containing 601,322 retained entries,
but rejects it as a general replacement: the broader capture loses eighteen
four/five-syllable typo targets and gains only two relative to the mirror run.
[Selective warming](spikes/typo-selective-warming.md) retains the complete
dictionary and warms only its compressed strings. Focused tests pass at 59.84 MiB
sampled engine RSS, but first-worker mixed-input misses persist. A read-only
diagnostic localizes substantial first-access cost to dictionary lookup; broad
recovery and clean-input validation remain open.

[Exhaustion fallback](spikes/typo-exhaustion-fallback.md) now discards incomplete
general correction lookups as well as three-initial lookups in the scratch path.
Forced exhaustion matches ordinary menus in ten deterministic engine cases;
this does not resolve normal correction's later-page displacement.

The [complete-table repack](spikes/typo-repacked-table.md) preserves all 1,886,051
entries while grouping indexes into fewer pages. It passes focused tests, but
index/string warming still leaves a first-run miss; it is not a recovery fix yet.

The next presentation experiment is [separate correction suggestions](spikes/typo-separate-corrections.md):
preserve the entire ordinary candidate stream and expose bounded explicit choices
separately. Its counterfactual availability is promising, but native selection,
keyboard/UI behavior, protocol compatibility and learning must be verified before
it replaces the mixed-list baseline. Experiment history is collected in §10.

The [native selection hook](spikes/typo-explicit-selection.md) retains an
explicitly chosen Phrase without inserting it into the ordinary menu. Fixture
tests pass; separate recovery streams, tokens, learning and UI remain unverified.

The [standalone correction registry](spikes/typo-correction-registry.md) adds
bounded snapshots and stale-selection checks. Engine integration and client
protocol validation remain open; this is not yet a clean-input regression fix.

## 1. Product decision

Help users select the Chinese they intended when they transpose letters or
hit a neighboring key. Start in the full-pinyin input scheme, including its
ordinary initials and mixed full/abbreviated input. The user explicitly identified
order errors and adjacent-key errors; their `pinin` also illustrates a missing
`y`. No personal typo corpus or preferred input layout has been supplied.
Examples below are illustrative, not measured user behavior.

The proposed foundation is **bounded, dictionary-backed spelling search**.
It works offline without a model download. Compare three ranking choices on
the same recovered menus before selecting the production AI architecture:

1. A weighted keyboard-error channel plus dictionary/user priors.
2. That channel plus the existing ERNIE contextual scorer.
3. A jointly trained model that sees raw pinyin, candidate spelling, Chinese
   candidate and preceding context, and scores correction and ranking together.

A separate neural model is **not a prerequisite**. A joint model is a serious
candidate for replacing the existing scorer, not automatically an additional
model running after it. Unconstrained pinyin-to-Chinese generation is an
experimental alternative, not the proposed first shipping path (§7).

The key distinction is candidate recall versus ranking: today's ERNIE can
choose among supplied Chinese strings but cannot select a word missing from
the spelling graph. Training a better reranker cannot fix that search failure.
Conversely, more tolerant search alone cannot reliably decide whether valid
pinyin was intentional. Evaluate both problems separately and jointly.

## 2. User behavior and scope

| Raw input | Intended spelling | Treatment |
|---|---|---|
| `nihoa` | `nihao` → 你好 | One adjacent transposition within `hao` |
| `nihso` | `nihao` → 你好 | One adjacent-key substitution, `s` for `a` |
| `pinin` | `pinyin` → 拼音 | One missing letter, `y` |
| `nihaoo` | `nihao` → 你好 | One extra/repeated letter |
| `nihao` | `nihao` → 你好 | Clean input; preserve ordinary translation |
| `nhi ao` (raw `nhiao`) | `ni hao` | Swap `h/i` across the intended syllable boundary; deferred initially |
| `hao` | `gao` → 高 | Both valid pinyin; `h/g` are adjacent keys. Do not assume correction is intended |

Only adjacent character swaps count as one transposition. Arbitrary letter
permutations are not one edit. Keyboard adjacency uses a versioned QWERTY
geometry table including diagonal neighbors, with distinct costs; it does not
claim to model every physical layout. Unsupported layouts still get swaps,
missing and repeated letters, without an asserted physical-neighbor advantage.

Initial correction scope: one edit per active composition, full-pinyin lexical paths,
including within-syllable swaps, neighboring substitutions, missing letters
and extra letters. Search after at least four raw letters; shorter exact
inputs are protected. Incomplete trailing syllables continue to use ordinary
completion; completion is distinguished from inserting a missing letter.
Explicit apostrophes are hard boundaries, never inserted, removed or crossed
by a typo operation. Existing `v`/ü normalization precedes comparison while
retaining raw offsets. Arbitrary substitutions, multiple edits and swaps
across inferred syllable boundaries are later experiments with separate gates.

Proposed Settings → Input control: **拼音纠错 / Pinyin typo correction**,
boolean `pinyin_typo_correction`, initially off for existing and new profiles.
Help text: “Recover common full-pinyin typing mistakes. Works without 智能排序.”
Intelligent sorting remains separately optional; its existing status explains
whether contextual ranking is active. No model choice or numerical thresholds
are exposed as user settings. Compiler validation, portable settings migration,
Chinese/English UI and snapshots accompany a future implementation.

Keep the literal preedit. Mark a corrected candidate with a compact comment
such as `纠错 → nihao`; preserve existing comments where space permits. Do not
rewrite the composition in place. Space selects the displayed first candidate;
number keys and clicks select the displayed item. Return retains literal-input
commit semantics. Shift retains the configured mode-switch/commit behavior;
this feature must not redefine it as a new correction command. Backspace edits
the raw letters, not a hidden normalized spelling. No automatic text replacement
after commit and no automatic correction acceptance merely because typing paused.

## 3. Protect intentional input

Always generate the ordinary translation first and preserve it as fallback.
Do not enable correction only when that translation is empty: `hao`/`gao`-style
collisions exist, including in longer phrases. Eligible longer exact inputs may
get correction alternatives, but availability and permission to become #1 are
different decisions.

- Exact-code pins and custom phrases keep their existing precedence. Never
  transfer a pin for corrected spelling to the raw typo code.
- English/ASCII mode, secure fields, recognizer/native translator triggers
  (date, number, calculator, Unicode, symbols), punctuation and literal custom
  codes are outside correction. Identify these from segment/translator metadata,
  not a new client heuristic. Exact English candidates retain their priority;
  do not infer “Chinese typo” solely because the letters form an English word.
- Clean initials-only and mixed full/abbreviated paths are first-class supported
  input and must retain ordinary translation and contextual reranking. Typo
  edits on abbreviated paths require the separate evaluation below; shuangpin
  receives no typo expansion in v1.
  Fuzzy-pinyin paths continue to work but cannot also spend typo edits initially.
  Never multiply fuzzy, abbreviation, mixed-layout and typo expansions together.
- Preserve the entire ordinary first page whenever a zero-typo full-pinyin,
  abbreviated or mixed lexical/sentence path exists in the first rollout.
  Abbreviation is not a spelling error. Correction alternatives remain
  available after that page and protected entries; never evict a pin to force
  visibility. Native path provenance must distinguish exact spelling from
  abbreviation/completion: whole-input acceptance or a whole-word index lookup
  alone cannot establish it. The first offline experiment showed that preserving
  #1 alone still displaces clean targets from the first page.
- If there is no zero-typo lexical candidate (including abbreviation), a correction may become #1 only above
  a calibrated confidence/margin threshold; otherwise keep ordinary completion
  or literal fallback first. Reserve an abstain outcome.
- A later opt-in experiment may let corrected candidates beat an exact lexical
  #1, using both a stronger score margin and the clean-input gate in §8. The
  same guard applies whether AI is on or off. Context alone cannot bypass it.

Context is a hint, not document truth: unobserved mouse movements and edits can
leave the current per-app context stale. Evaluate with empty, stale and unrelated
context as well as accurate context. The conservative base policy survives all
AI failures and remains useful without an accelerator.

### Abbreviation is intentional input, not deletion noise

Illustrative inputs for 你好 include `nihao`, initials `nh`, and mixed `nih`
or `nhao`. Which paths the current schema actually accepts must be established
by native menu capture; a synthetic abbreviation is not automatically supported.
Treat the scheme's initial spellings (including any supported `zh/ch/sh` forms)
as spelling rules, not a universal first-character shortcut.

Search must compose **canonical syllables → allowed typed forms → keyboard
errors**, retaining separate abbreviation and typo provenance. For example,
`ni hao → ni h → nih` uses no typo edit. A hypothetical `njh → nih` changes
`j` to adjacent `i` once on that mixed path. `hn → nh` swaps two initials,
not two letters inside a syllable. The latter is a separate, especially
ambiguous operation and is excluded from initial typo expansion. Many short
codes already have legitimate interpretations; neither example authorizes
automatic correction or invents a target absent from the native dictionary.

Keep full, abbreviated, mixed and incomplete-prefix interpretations concurrently
within one bounded search budget. Do not classify an entire raw string as one
mode using length or whether a full-pinyin parse exists. `nih` may be an
intentional abbreviation or a prefix of unfinished `nihao`; preserve both
ordinary behaviors. The missing-letter operation is charged only relative to
the selected allowed typed form, never against every omitted canonical vowel.

Initially preserve existing abbreviation retrieval unchanged. Before enabling
typo edits on mixed paths, add provenance-aware graph transitions for an exact
abbreviated syllable plus a corrected fully typed syllable. They share the same
composition edit/state/output caps; no Cartesian product of all abbreviated
phrase strings, no second dictionary/model and no expanded per-app cache.
Correcting an abbreviated initial itself is a later ablation with separate
short-code safety gates. Exhaustion returns the ordinary abbreviated menu.

The existing ERNIE scorer can rank retrieved abbreviation candidates using
Chinese/context, but does not know which letters were intentionally omitted.
A joint scorer should receive raw code, canonical syllables and the chosen
typed-form alignment, with separate abbreviation/typo features. Never impose
the full-spelling deletion penalty on an otherwise clean abbreviation. Compare
against the existing abbreviation reranker at its same bounded export limit;
report candidates lost to that limit separately from spelling-search failures.

The original recovery and ERNIE reports cover full-spelling synthetic errors.
The [abbreviation baseline](spikes/typo-abbreviation.md) now adds 830 examples
and native retrieval/scoring: clean test initials retrieve 16/34 targets in
the first 50, while useful context can promote an available target (`xj` → 休假,
#45 → #1). That baseline did not add recovery. The subsequent
[bounded syllable-search prototype](spikes/typo-abbreviation-recovery.md) adds
recovery and native verification, but exhausts its queue on 400/830 examples
and still loses six clean test targets from page one with the error penalty.
Its 45,064-byte workspace is bounded; the separate dictionary-scale index is
not a product memory commitment. The counterfactual AI-page guard is diagnostic,
not permission for a second inference pass. Data and evaluation must address
the following **before the next model decision**:

- Keep the existing phrase/reading family splits; generate full, initials and
  mixed forms only after splitting. Sample a bounded number of mixed masks per
  family rather than enumerating all combinations. Verify forms against native
  spelling rules; retain unsupported forms as an explicit coverage count.
- Save canonical syllables, allowed typed form, abbreviation mask/alignment,
  raw code and keyboard edits separately. Add clean abbreviations first, then
  corrupt the typed form. Include adjacent substitutions on initials, swaps
  between initials, and edits inside fully typed mixed syllables as separate
  challenge slices. Do not relabel existing synthetic deletions as intentional
  abbreviations or move previously tested families into training.
- Capture ordinary native menus with AI off/on, then compare recovered menus
  with AI off/on and the error channel. Evaluate empty, useful and stale context
  on identical examples; prefixes, Backspace and partial selection must preserve
  remaining raw letters. Deduplicate ambiguous raw codes without losing their
  multiple legitimate targets; split-conflicting codes need exclusion or an
  explicitly shared-code evaluation, not contradictory training labels.
- Report recall, top-1/top-5 retention, added keystrokes, cap exhaustion, latency
  and memory separately for full, initials and mixed input and short-code length
  buckets. Apply the clean-regression gate to each enabled category, not only
  to the combined average. Until these pass, typo expansion on abbreviated
  paths stays disabled while ordinary abbreviation and its reranking remain on.

## 4. Search and librime integration

### Current constraints

Source inspection at librime submodule commit
`3438a3b860cd3525f25ee2198c35ea49a3f2d85d` found:

- `src/rime/dict/corrector.{h,cc}` has a corrector interface. Its factory returns
  `NearSearchCorrector`; the edit-distance implementation is behind `#if 0`.
  The latter includes transpositions but allocates dynamic work arrays. Neither
  simply enabling that branch nor the existing neighbor search meets this design.
- `src/rime/algo/syllabifier.cc` invokes correction for suffixes with tolerance
  5 and assigns a fixed correction credibility. `Corrections` is keyed only by
  syllable ID, which can discard alternatives consuming different raw lengths.
  Weighted path costs and composition-wide edit limits are not preserved by
  merely adding a new implementation of that interface.
- `src/rime/gear/script_translator.cc` requests the literal component name
  `corrector` when `translator/enable_correction` is set and caps correction
  candidates at four. Registering `shixin_corrector` alone will not connect it.
- Shixin's [compiler](../internal/settings/compile.go) places `shixin_rerank`
  first among filters and `shixin_pin_filter` last. Its existing
  [rerank export](../internal/rerank/slot.go) has prior/flags but no edit cost
  or correction provenance.

### Proposed ownership and algorithm

Use a native `shixin_` corrector in `plugins/librime-shixin` plus small generic
hooks in the maintained librime fork: configurable component selection,
bounded correction output carrying rich edges, graph path edit accounting,
and translator candidate provenance. Default hooks preserve existing behavior
when disabled. Document and test the fork changes independently; do not put
Shixin policy in upstream-style generic code.

At builder time, derive a compact lexical full-pinyin syllable trie and keyboard
cost table from the supported schema. Do not expand every typo into the prism:
that increases generation size and multiplies fuzzy/shuangpin combinations.
Version/checksum these artifacts with dictionary and compiler identity. Warm
and validate them before activating a generation; corrupt/missing data disables
correction without disabling ordinary typing. No live dictionary I/O is added.

The [radix experiment](spikes/typo-radix.md) compresses nonterminal single-child
syllable chains while retaining their internal spelling/edit boundaries. It
reduces major retained index buffers by 10.31% with identical results, at a small
observed lookup cost; it does not fix queue exhaustion or prove a startup RSS
reduction. Keep storage compression separate from search-pruning decisions.

The [packed-storage experiment](spikes/typo-packed-storage.md) does better on
this corpus: specialized leaf/internal records, compact edge arrays and pooled
live strings reduce major retained buffers to 51.93 MiB. Loading a builder-made
image in a fresh search-only process peaks at 54.97 MiB RSS; construction still
costs memory in its separate process. Prefer an immutable prebuilt artifact if
a separate index is adopted, with checksummed generation identity, validated
field limits and startup warm-up. This is not approval for duplicating the
native dictionary or changing the unresolved search/safety gates.

The [cold-access follow-up](spikes/typo-initial-cache.md#cold-access-breakdown-and-proposed-compact-mirror)
finds that native text decoding as well as reading lookup can exceed the recovery
budget on first access. Evaluate a bounded shared mirror of the selected native
cache entries, rather than a second full dictionary: measured entry/text counts
project 2.87–3.17 MiB of immutable payload for the three-initial lane. This is an
explicit experimental exception to avoiding duplicated text, not a per-app cache
or an approved memory budget. Builder-verified code/text/weight identity,
versioned table/prism/cache digests and validation before generation activation
are prerequisites for removing per-key native lookup. Keep native candidate and
learning identity; measure actual cold latency and pooled-session RSS before adoption.

On the engine's existing locked OS thread, traverse the trie against raw input
with fixed-capacity edit states. Enumerate exact matches first and then bounded
one-edit matches. Each correction edge contains raw `[start,end)` byte offsets,
canonical syllable ID, edit operation/alignment, fixed-point channel cost and
edit count. Keep alternatives distinguished by consumed span and budget state,
not just syllable ID. Use stable deterministic tie-breaking.

The graph combines edges under **one shared edit budget for the composition**,
not one edit per syllable. Maintain that budget across partial selections until
the original composition is finished or cleared; selecting a corrected prefix
must not reset it and enable unlimited corrections in the remaining suffix.
Recompute from retained alignment when raw input is edited. Reject paths whose
alignment or budget cannot be represented; retain exact translation.

Preserve native Phrase/Sentence and user-phrase identity, dictionary references
and selection/learning behavior. Do not manufacture a detached Chinese string,
open a shadow session, or replay alternate input into Rime. Learn the selected
Chinese under its canonical reading; do not create permanent typo-code aliases.
Learning must not make typo paths eligible to override protected exact input.
Deduplicate identical text **and raw span** by the best legal path, retaining
provenance; equal text with different consumption is not interchangeable.

Partial selection consumes exactly the selected raw span and retains the raw
suffix. This is a release-blocking invariant for deletion/insertion edges,
caret edits, paging and shorter-prefix AI selection. Initial support only permits
prefix cuts at monotonic alignment boundaries. For future cross-boundary swaps,
represent a two-syllable edge with a non-splittable swapped region; disallow a
partial cut through it. Until that representation is implemented and tested,
`nhiao` → `nihao` is explicitly unsupported, not silently approximated.

### Work caps and exhaustion

Starting limits for the spike, fixed per composition update across all suffixes:

| Resource | Proposed cap |
|---|---:|
| Raw correction input | 64 ASCII bytes; longer inputs use ordinary translation |
| Edits | 1 total |
| Expanded trie/edit states | 2,048 |
| Additional correction edges | 128 |
| Correction phrase candidates admitted | 8 |
| Additional reusable workspace | 256 KiB per warmed session |
| Incremental correction cost | p99 ≤ 0.25 ms; p99.9 ≤ 1 ms |

Operation caps are the primary bound; a measured time guard aborts at 1 ms to
limit surprises but is not a hard real-time guarantee under OS scheduling.
Reject the entire correction batch if graph/alignment construction is incomplete.
The native spike supports evaluating a revised rule for later dictionary-search
exhaustion: retain only fully materialized dictionary candidates backed by that
completed graph, mark search coverage incomplete internally, and stop further
lookup/sentence work. Subset checks against exhaustive search must prove that
retained candidates are valid; this does not justify exposing partially formed
candidates. Keep ordinary translation unchanged and never restart the deadline.
For the new initial-cache path, any incomplete cache stage adds no corrections
for that update. A fully completed cache stage may remain when the optional later
stage exhausts; discard that incomplete later stage. This is not a persistent feature toggle;
retry only after input changes. Repeated actual latency overruns may temporarily
suspend correction, while ordinary bounded exhaustion is expected and should not
disable the feature globally. The breaker thresholds/recovery interval remain
experiment parameters, not implemented product behavior.
Do not retry the same update, enlarge buffers, continue asynchronously or revise
an already displayed menu. Counters record exhaustion without input text. The proposed
incremental cost/workspace targets still need separate validation; passing the
sampled total engine latency gate does not establish them.
Warming multiplied by the existing session pool must fit a measured startup
and memory budget; reduce caps if it does not.

Use fixed arenas for new search work and metadata. Existing librime graph and
candidate construction already allocate; do not describe the whole native
pipeline as allocation-free. Measure that baseline separately, bound additional
candidate allocations, and require zero new common-path search allocations plus
zero Go/client per-key allocations. All §3 engine/client latency budgets still
apply; these incremental targets do not increase the total engine budget.

## 5. Ranking contract

For raw input `x`, canonical spelling `s`, candidate `y` and context `c`, the
non-neural baseline uses dictionary/user prior and `log P(x | s)` (negative
weighted edit cost). The ERNIE alternative adds `log P(y | c)` with calibrated
weights and the existing length/prefix policy. Margins and abstention are tuned
on development data, then frozen before test evaluation.

Keep the unmodified dictionary prior separate from channel cost. Charge the
channel once per path. If graph credibility already includes the error penalty,
the exported prior must expose its unpenalized value, or the scoring contract
must explicitly subtract that term before fusion. Audit sentence weights as
well as Phrase::weight; do not assume they exclude graph costs.

The mixed-list experiment leaves the ordinary candidate export unchanged and
scores it once with the existing reranker. It merges a separate bounded recovery
stream after the resulting first page. With AI off or timed out, perform the
same merge after the ordinary Rime first page. Recovered candidates retain
channel order; they are not context-ranked in this phase. The offline retest
protects five entries; production must use the configured page size. Preserve
the original translation stream, weights and pruning: a late filter cannot
restore an ordinary page already changed by correction inside the translator.
This requires a separate recovery stream/sidecar in the same native session,
not shadow sessions or replaying altered input. Scratch native integration is
measured; production lifecycle and learning integration remain unproven.

“Ordinary first page” means the visible combined menu, including other native
translators, not five entries from script_translator alone. The native spike's
`vlz` symbol displacement demonstrates that a per-translator merge is insufficient.
The production merge must occur after ordinary translation merging and ordinary
ranking/conversion/deduplication, while preserving the explicit pin policy, or
prove equivalent behavior across those stages. Deduplication must retain ordinary
provenance when ordinary and corrected entries share text and span. Trigger
bypasses are required but do not replace this end-to-end invariant. Later ordinary
entries must remain selectable in their original relative order; inserting
corrections can increase their page/rank, which must be reported separately from
first-page retention rather than advertised as no clean regression.

A later joint-ranking experiment may address correction starvation in the
first-N export. For that experiment only, for the normal 20-candidate budget,
reserve up to four slots for the best legal corrected same-span candidates,
fill the rest with exact candidates, and return unused correction slots to
exact candidates. Do not scan an unbounded translation to fill a quota. Preserve
protected entries and ordinary paging. Corrections excluded by the search caps
are not promised to reappear on later pages. Track search recall and export
recall separately. Shorter-prefix candidates remain subject to the existing
opt-in policy and raw-span validation.

Provenance must survive simplification, emoji conversion, deduplication and
pins. Keep genuine native candidate types; introducing a `correction` type
could make today's phrase/sentence/user_phrase allowlist treat it as fixed.
Test the full filter chain, not just the generator in isolation.

Extend native slot ABI and AI protocol together if needed, with explicit
version/capability negotiation and size checks: raw input, spelling/alignment
identity, raw span, edit cost and provenance must match the export revision.
Freshness includes session ownership, input/caret revision, context revision,
generation/configuration and export identity. Old helpers or incompatible
libraries fail closed to deterministic ranking. No stale reply applies after
Backspace, focus changes, settings changes or partial selection.

AI gets the **existing single ≤16 ms hold**, never another correction hold.
Late/unavailable inference uses the ordinary Rime page plus the bounded recovery
tail. Assemble once before display; never update an already shown menu.
ASK/commit/other never-held keys remain never held. Prefer the
existing candidate-comment client protocol; any necessary client extension is
append-only and versioned with Go/C++ golden vectors.

### Separate suggestions as the next presentation experiment

The mixed-list baseline cannot preserve later ordinary ranks when it inserts
corrections. Evaluate an independent correction list, initially two visible
suggestions with up to eight available through an explicit action. Ordinary
candidate ordering, page boundaries, number/Space selection and raw bypass remain
under their existing pipeline. Do not obtain this list by splitting an already
interleaved page. Recovery remains bounded, and neither list changes after display.

On the reused 553-typo corpus, ordinary five plus two correction positions expose
85 targets, versus 35 in ordinary five alone; these are seven positions, not an
improvement in top-five ranking. Ordinary thirteen plus two correction positions
expose 93 targets, versus 43 in ordinary thirteen. Clean ordinary targets retain
their positions by construction in this offline comparison, not by a tested UI.

A native side registry and explicit selection hook must retain real Phrase identity
and raw spans, with revision-checked opaque tokens and append-only capability-gated
client messages. Existing menu-index selection is insufficient for candidates held
outside the ordinary menu. A conflict-free keyboard path, partial selection,
learning, stale-click handling and VM UI tests are release gates. The detailed
[contract and counterfactual](spikes/typo-separate-corrections.md) is a proposed next
experiment; no binding, UI, protocol or shipped default has changed.

## 6. Shuangpin and settings lifecycle

Full-pinyin-first is an explicit scope choice, not evidence that shuangpin users
do not make typos. In shuangpin, swapping two keys can change an entire syllable;
physical-key errors must be modeled over each layout's encoding, not over the
expanded full-pinyin string. Many wrong codes are valid, so false corrections
are particularly dangerous.

Initially disable the control for pure shuangpin and for shuangpin with optional
full-pinyin fallback. The mixed prism does not yet provide a sufficiently safe
layout-provenance contract for typo expansion. Preserve fallback behavior as-is.
A later phase needs layout-tagged graph paths, seven-layout confusion/evaluation
sets, precedence for exact shuangpin, and no extra independent expansion budget.

A future option change goes through settings validation → compiler → builder →
warmed-generation hot swap. Change compiler compatibility identity and reuse
rules whenever correction resources or graph semantics change. No deployment,
file loading, inference or network activity occurs on the engine key path;
clients gain no new synchronous query or policy assumption.

## 7. Which model, including end-to-end correction and ranking?

“End-to-end” can mean joint training, or removing the dictionary/search pipeline.
Those are separate choices. Joint training is compatible with bounded native
search and is the most promising neural experiment here.

| Alternative | What it can learn/do | Main limitation | Proposed role |
|---|---|---|---|
| Fixed weighted channel + priors | Swaps, physical neighbors, missing/repeated letters; inspectable fallback | Limited semantic disambiguation | Required baseline and no-download mode |
| Learned confusion table or small linear channel | Error frequencies conditioned on key/position; still fixed-cost native scoring | Cannot resolve all context-sensitive collisions | First low-cost trained experiment |
| Existing ERNIE-4.5-0.3B + channel | Reuses shipped Chinese-context scorer and infrastructure | ERNIE does not currently see raw pinyin; independent fusion can miss interactions | Reference AI baseline, not a proven typo solution |
| Joint discriminative model | Scores `(raw pinyin, canonical spelling, Chinese, context)` including “keep exact” | Recall still limited by search; needs labeled negatives and a deployable runtime | Preferred specialized-model experiment |
| Joint autoregressive model, initialized from existing ERNIE | Fine-tuned conditional scoring of supported Chinese given raw pinyin/context, or constrained decoding | Tokenization of pinyin, extra prefill and prefix changes may hurt latency | Compare with the small discriminator; possible replacement for current scorer |
| Free-generating pinyin/context → Chinese model | Can propose candidates absent from dictionary; learns correction and conversion together | Hallucination, rare names, uncertain spans, decoding cost, lost native learning semantics | Offline research challenger only initially |

For the discriminative experiment, investigate a character/byte-aware encoder
in the **10–30M parameter range**, scoring a batch of at most 20 candidates.
This is a search range, not a selected architecture or latency claim. Include
canonical readings for polyphones, explicit candidate boundaries and a null/
abstain option. Train listwise or pairwise on complete candidate menus with
clean and corrupted inputs, including unchanged exact candidates as hard
negatives. A model that only classifies “is this spelling plausible?” is not a
joint reranker. It must also rank ordinary clean/abbreviated/fuzzy menus if it is
to replace ERNIE for all intelligent sorting.

For a joint ERNIE experiment, fine-tune conditional candidate scoring with raw
pinyin and context rather than expecting the existing Chinese continuation
objective to understand typos. Compare against simple channel fusion under the
same candidate and timing budget. Do not assume a new prompt alone solves it.

A free-generating contender must return multiple hypotheses with canonical
readings and verifiable raw alignment. Validate against native lexical paths
before exposing a selectable candidate, or design and prove a separate learning
contract. Such validation restricts its claimed out-of-dictionary advantage.
Constrained decoding over dictionary candidates is effectively a joint scorer
and can share the safer integration above. Long autoregressive generation may
miss the existing hold; no architecture gets permission to extend that hold.

**Provisional recommendation:** build/evaluate bounded recovery first; use the
weighted channel as guaranteed fallback. Compare current ERNIE fusion against
a jointly trained discriminator and jointly conditioned ERNIE. Select one AI
scorer, not sequential correction-model and rerank-model passes. A winning
joint model should replace the scorer when it also preserves clean, abbreviated,
fuzzy and short-prefix quality; otherwise remain an experimental route only
for corrected menus with explicit memory/load costs measured. No new runtime
or second resident model is approved by this design. Any shipped model must
fit the existing process boundary, licensed distribution, download verification
and accelerator/failure lifecycle; unsupported operators/runtime requirements
are a separate architecture decision.

Go/no-go for a specialized joint model: on held-out real volunteered typo cases,
≥3 percentage points absolute top-1 improvement over the best simpler baseline,
with a positive paired 95% confidence interval, unchanged clean-input safety
gate and total scorer p95 ≤15 ms on each supported reference device. It must
also show a meaningful advantage for transpositions and adjacent substitutions,
not just synthetic deletions. To replace ERNIE globally, clean/abbreviation/fuzzy
first-page accuracy must regress by no more than 0.2 points in each slice.
If data is insufficient or no candidate meets these gates, ship no new model.

## 8. Evaluation and release gates

Build a versioned corpus of public/licensed Chinese phrases and their accepted
readings, plus explicitly volunteered typo examples. Never harvest private
composition text, raw keystrokes or context into logs/telemetry. Evaluation tools
use scratch user dictionaries and never open the live user database.

Split by source/document and phrase family **before** generating corruptions;
keep their clean and noisy variants together to avoid leakage. Hold out real
examples separately from synthetic training/development data. Include polyphones,
rare names, English/code-switching, custom codes, abbreviations, fuzzy settings,
apostrophes, long compositions and unsupported edits. Report outcomes for each
operation and for valid-pinyin collisions. Generate every incremental typing
prefix, Backspace/caret edits and partial selection traces, not just final words.

Compare correction off, existing librime correction as a diagnostic baseline,
bounded channel, learned channel, ERNIE fusion, joint discriminative scoring,
and joint generative/conditional scoring. Same dictionaries, search caps and
candidate sets for ranking ablations; separately report expanded recall for a
generative contender. Use oracle ranking to expose search failures.

| Measurement | Proposed release gate/report |
|---|---|
| Intended candidate in recovered pool | ≥95% on eligible single-edit synthetic cases, each priority error type; report real recall separately |
| First page, AI off | ≥90% on eligible synthetic cases; report real top-1/top-page and corrections requiring extra selection |
| Real benefit | Paired improvement over correction-off with positive 95% CI; insufficient data means no claim of real-world accuracy |
| Clean false correction | Upper 95% confidence bound ≤0.1% of clean compositions promoted to a wrong corrected #1; ≥10,000 clean held-out compositions |
| Protected paths | Zero changed precedence/behavior in deterministic pin/custom/English/native-trigger regression fixtures |
| Corrected promotion over valid exact input | Disabled initially; same false-correction gate required before considering enablement |
| Latency and memory | §4 incremental caps plus DESIGN §3 totals, AI off/on; report p50/p95/p99/p99.9, timeout/exhaustion rates and warmed pool RSS |
| Allocations | Zero Go/client key allocations; zero new common-path correction-workspace allocations; native baseline/candidate deltas reported separately |
| Text integrity | Zero lost/duplicated bytes, wrong-span commits or stale reorder in trace and VM suites |

A zero false-promotion count with the initial exact-first policy is not proof
that a neural model is safe to override exact input. Also report first-page
pollution, clean top-1 changes, corrections selected at the wrong time and extra
keystrokes. Include unsupported/short cases in an all-input coverage report,
not only in the denominator of a favorable eligible subset. Publish seeds,
licenses, manifests and split hashes with the spike results.

Required implementation verification: C++ unit/property tests for edit alignment,
caps and deterministic fallback; ASan/UBSan and fuzzing for bounded graph/search
and malformed artifacts; scratch engine integration for native identity,
learning/forgetting and persistence across restart; generation/handoff tests;
rerank freshness/ABI compatibility tests; latency and allocation replays with AI
off/on. macOS Tart and Windows VM tests assert exact committed text for typing,
Backspace, Return, configured Shift, paging, candidate clicks, focus loss and
partial selection. No host IME installation is required or authorized.

## 9. Delivery sequence and open decisions

1. **Baseline and design-validation spike.** Build the corpus/evaluator in Go
   and native search experiments in C++; measure existing correction, capacity
   and recall. Prove raw-span partial selection and learning before adopting the
   fork hooks. Deliver a report with failures, not product implementation by
   implication. Resolve dictionary-prior/cost accounting and startup memory.
2. **Bounded correction without AI.** Implement the hooks, native generator,
   settings/generation lifecycle and conservative merge. Pass safety/performance
   gates on both current platforms. Keep the setting opt-in. Cross-boundary
   swaps, shuangpin and multi-edit inputs remain explicit exclusions.
3. **Model comparison.** Reuse the same captured menus to compare learned
   channel, existing ERNIE fusion and jointly trained alternatives. Prototype
   free/constrained generation offline to measure whether its extra recall
   justifies integration complexity. Publish accuracy, clean regressions,
   timing, memory and distribution/license costs together. Select a model only
   after ablations, with the criteria in §7.
4. **Expansion and default decision.** Add cross-boundary swaps first if their
   measured frequency warrants it, then separately evaluate multiple edits and
   each shuangpin layout. Revisit exact-top-1 promotion and opt-out defaults only
   after real-example evidence meets the safety gate. Do not silently expand
   scope to rescue aggregate benchmarks.

Open risks: real error distribution is unknown; native sentence weight provenance
may require deeper fork work; bounded search may miss long/rare phrases; exported
corrections may compete with exact candidates in small pages; non-monotonic swap
alignments complicate partial commits; a small joint model may lose ERNIE's broad
contextual competence. These are experiment questions, not reasons to delay the
concrete design or to assume a new neural model will solve them.

## 10. Evidence and related work

### Experiment record

Data preparation started 2026-11-04: see the
[source inventory and first corpus report](spikes/typo-data-preparation.md).
The pilot contains labeled synthetic errors from downloaded CC-CEDICT data;
it does not establish real-typo accuracy or select a model.

The [first recovery experiment](spikes/typo-recovery.md) measured useful swap
and neighboring-key recall but rejected insertion immediately after exact #1:
it displaced clean targets from page one. The policy below now protects the
whole ordinary first page. The [constrained-search retest](spikes/typo-constrained-search.md)
now evaluates a single-pass merge that preserves that page after ordinary AI
ranking: zero first-page retention losses across 1,062 offline rows. This is
structural retention, not proof of real-user accuracy or native integration.
The [failure audit](spikes/typo-failure-audit.md) separates the remaining misses:
153 lack the labeled reading, 153 exhaust with a retained target, and 182 have
a completed-search quota explanation. Increasing queue capacity alone cannot
fix reading coverage or output quotas; test candidate allocation and
reading preparation separately before choosing another model.
The [quota experiment](spikes/typo-quota-allocation.md) finds a modest retrieval
gain from one zero-edit/seven typo slots, but this is not yet native-verified
and slightly increases exhaustion. A bitset-alignment alternative worsens
bounded retrieval and is rejected. Neither replaces the native integration gates.
The [native graph prototype](spikes/typo-native-graph.md) now demonstrates raw
selection and partial-selection behavior without the separate word index.
It fixes visible-page counting and distinct-span deduplication in a scratch
library. Canonical-aware position selection and skipping redundant sentence
construction restore the original editing fixtures within the diagnostic latency
gate. Staging specific repairs before broad initial corrections also recovers
`njh → 你好` within that gate. Incremental completed-entry collection and
depth-first recovery also restore `xgr → 中国人` in the original nine editing
fixtures. Adding a later-initial error, `zhr → 中国人`, exposes a remaining failure;
the expanded ten-case suite is not green. Native readiness still requires broader
recovery, allocation, memory, learning and AI validation. The completed paired
830-example depth-first capture raises verified typo-target recall within 13
candidates from 43/553 to 101/553, but clean-target recall in that window falls
from 182/277 to 173/277. Clean top-five recall remains 153/277. The earlier
native-symbol prefix regression is absent in this capture; the two remaining
prefix differences are live date/time outputs. This is a reused synthetic tuning
corpus, not a fresh holdout or evidence of broad clean-input safety.

Later-position search, prefix-frequency ordering and a simple width-32 beam
have been tried in the scratch engine and rejected as replacements: none fixes
`zhr`, and the latter two regress `xgr`. The design therefore retains a bounded
channel/search baseline while requiring repair-position and segmentation coverage
in its next experiment. A new neural model cannot compensate for candidates that
miss the native time-bounded export pool.

The [subtree-bound experiment](spikes/typo-subtree-bounds.md) now recovers both
abbreviation examples in an isolated three-initial search. Its exact top eight
match exhaustive enumeration for all 17,576 lowercase codes with one neighboring
substitution, using about 374 KiB of metadata payload and a 65 KiB workspace.
This does not fix the native engine yet and does not cover other lengths, mixed
forms or swaps. The next integration must preserve native candidate identity and
share the existing deadline; memory and latency figures exclude the full pipeline.

The [native initial-cache follow-up](spikes/typo-initial-cache.md) now integrates
a compact offline reading cache for this three-letter lane. It avoids the startup
dictionary scan and reduces sampled RSS to about 54 MiB versus 51 MiB ordinary.
All ten editing cases pass warm; cold misses remain. The report records exact
lookup validation, exhaustion fallback and the limits of these measurements.
Its independent corpus retest reproduces 101/553 typo top-13 recall and 173/277
clean top-13 recall, retaining the eleven clean losses. Read-only stage timing
locates first-query cost in native lookup (1.2–3.8 ms), not graph construction;
the cooperative deadline also needs checks around costly native operations.
Earlier native/corpus results above describe their respective historical versions.

The [standalone compact mirror](spikes/typo-compact-mirror.md) now demonstrates
exact native reading/text round trips and dictionary-free queries using 3.17 MiB
of retained payload. Its fresh-load measurements exclude the engine pipeline;
verified generation binding and native learning integration remain required.

The [native-mirror integration](spikes/typo-native-mirror.md) now passes the
focused first-run editing fixtures using verified private artifact copies. It
retains native candidate identity and bounded fallback, with measured RSS and
warm latency. Broader corpus, learning and product lifecycle gates remain open.

The [existing-ERNIE retest](spikes/typo-rerank.md) restored first-page target
recall from 14/19 to 19/19 on a small source-context clean test subset. Empty
and stale context still regressed, even with a fixed error penalty. These
offline results use approximate dictionary priors; they support testing the
existing scorer before training another model, not removing deterministic
clean-input protection or the timeout fallback.

### Primary references

Repository evidence: [main design](DESIGN.md),
[existing model experiment](spikes/m2-rerank-models.md),
[settings compiler](../internal/settings/compile.go),
[native reranker](../plugins/librime-shixin/src/rerank.cc) and
[export ABI](../internal/rerank/slot.go). The librime paths in §4 refer to the
pinned submodule source; this worktree may need submodule initialization to
browse them locally.

Tan et al., ACL 2022,
[Exploring and Adapting Chinese GPT to Pinyin Input Method](https://aclanthology.org/2022.acl-long.133/),
reports degradation from perfect to abbreviated pinyin and investigates adapting
GPT with pinyin information and homophone discrimination. This motivates testing
input-aware models; it is not evidence of typo accuracy or Shixin latency.
Jia and Zhao, ACL 2014,
[A Joint Graph Model for Pinyin-to-Chinese Conversion with Typo Correction](https://aclanthology.org/P14-1142/),
is historical related work by title; no performance or implementation claims
from its full paper are used here. Neither source selects the production model:
Shixin's real menus, raw-span semantics and no-stall gates decide that.

Implementation experiment: [independent recovery extraction](spikes/typo-recovery-stream.md)
removes the ordinary-cursor dependency from enumeration; separate engine/client
publication remains outstanding. This is not a shipped clean-input fix.

Follow-up: [the native engine separation experiment](spikes/typo-engine-separate-stream.md) now
stores recovery outside the ordinary menu and verifies real native selection.
Client publication, conversion and provenance remain outstanding.

Follow-up: [native edit provenance and registry selection](spikes/typo-edit-provenance.md)
connects actual recovery candidates to registry selection in a scratch native
test, with apostrophe-anchor and nested-wrapper fixes. Product publication
and learning-persistence checks remain open.

Follow-up: [separate correction conversion and deduplication](spikes/typo-correction-filters.md)
verifies traditional/emoji display and registry commits while preserving the
compared ordinary menus. Internal conversion budgets and production publication
remain open.

Follow-up: [context-owned native publication](spikes/typo-context-publication.md)
adds engine-driven snapshots and revision/incarnation invalidation. The real
worker-generation bridge and client protocol remain outstanding.

Follow-up: [the versioned native/purego bridge](spikes/typo-native-bridge.md)
verifies copied snapshots, token selection and zero Go allocations with reusable
buffers. Product worker/client integration and learning persistence remain open.

Follow-up: [private correction learning across process restart](spikes/typo-learning-persistence.md)
verifies canonical persistence after adjacent-key and emoji correction selections,
plus a no-selection negative control. Swap retrieval, partial learning and product
integration remain unverified.

Follow-up: [alternate-boundary recovery](spikes/typo-alternate-boundaries.md)
recovers and persists the within-syllable swap `niaho` → `你好` without changing
the compared ordinary menus. The same-workload control exposes an additional
RSS cost; the broader search remains experimental pending resource work.

Follow-up: [memory attribution and abbreviation regression](spikes/typo-memory-attribution.md)
identifies dictionary-page residency as a major cost. A selective-boundary
optimization was rejected after paired corpus capture lost six abbreviated/mixed
targets. The broader policy remains retained; the memory gate is still open.

Follow-up: [the complete-table layout re-evaluation](spikes/typo-repacked-table.md#re-evaluation-with-separate-corrections-and-alternate-boundaries)
finds no lost targets across two paired corpus trials, but no measured RSS
improvement. The original table remains the default; layout repacking is not
accepted as the memory fix.

Follow-up: [bounded search diagnostics](spikes/typo-search-diagnostics.md) capture
a `niaho` miss at the post-sort deadline check with ample operation budget.
The next optimization target is dictionary head-weight access, not a larger
queue or deadline. Cold recovery is still unresolved.
