# Separate correction suggestions: layout analysis and proposed contract

Date: 2026-11-04. Counterfactual evaluation and design, not implemented UI.

The [explicit native selection experiment](typo-explicit-selection.md) now
tests selecting a retained Phrase outside the ordinary menu. The registry,
stream separation, protocol, persistent learning and UI gates remain open.

A [bounded registry prototype](typo-correction-registry.md) now checks scoped
tokens, frozen snapshots and candidate identity in native fixtures. It has not
yet been connected to live recovery, lifecycle invalidation or client export.

The [native mirror capture](typo-native-mirror.md) preserves ordinary page one,
but eleven clean targets originally at ranks 7–13 fall outside the captured
13-candidate window when corrections are inserted. Examples include `qers → 秦二世`
(original rank 7) and `qyuan → 请愿` (rank 12). These losses do not require the model
to choose a wrong first candidate: adding extra items shifts later ordinary items.

A finite list cannot preserve every ordinary rank while inserting new entries
before those ranks. Protecting thirteen instead of five ordinary positions merely
moves that boundary and delays useful corrections. The recommended next design
experiment is therefore an independent bounded correction list, leaving ordinary
candidate ordering and pagination under their existing pipeline.

## Counterfactual results

The analysis keeps the saved ordinary menus intact and extracts the native spike's
marked correction candidates from its paired capture, preserving their order.
It performs no new retrieval or model scoring. Only verified full-input commits
count as targets; partial candidates still consume a correction slot. Labels are
reused synthetic dictionary examples. Capture comments are acceptable analysis
labels here, but are not a production provenance protocol.

For 553 typo examples, ordinary recall is 35 within five candidates and 43 within
thirteen. The mixed native menu finds 99 within thirteen. The following uses
**extra positions**, so it is not a fixed-size ranking comparison:

| Separate correction slots | Additional targets beyond ordinary 13 | Available in ordinary 13 + corrections | Available in ordinary 5 + corrections |
|---|---:|---:|---:|
| 1 | 43 | 86/553 | 78/553 |
| 2 | 50 | 93/553 | 85/553 |
| 4 | 56 | 99/553 | 91/553 |
| 8 | 59 | 102/553 | 94/553 |

For 277 clean examples, keeping ordinary menus intact retains their 153 top-five
and 182 top-thirteen targets by construction. Corrections additionally expose two
labeled targets absent from those windows. That does not prove correct automatic
correction or native/UI safety. The eleven interleaving losses disappear in the
counterfactual because the ordinary list is never modified, not because a learned
classifier identifies clean input. A displayed correction can still be wrong.

Two initially visible suggestions plus an explicitly opened list of up to eight
is a useful next prototype: most of the observed added availability occurs in
the first two slots. This introduces screen area and an explicit selection action;
first-screen availability is not selection accuracy, saved keystrokes or usability.
The normal five plus two extra positions must not be reported as top-five recall.

Evidence: [metrics and displaced examples](typo-native-mirror/evaluation/separate-lanes.json),
[analysis tests](typo-native-mirror/evaluation/lane-analysis-tests.log),
[source/input identities](typo-native-mirror/evaluation/analysis-sha256.txt).
Reproduce with `go run ./spikes/typo-recovery -mode native-lanes -root <paired-capture> -source-run <selected-labels>`.

## Proposed behavior

Ordinary Space/number selection, pins, page keys, highlight and raw Return/Shift
behavior keep their existing meanings. Corrections appear in a separately labeled
area, initially up to two entries, with up to eight available through an explicit
correction-list action. Selection must be deliberate; simply having a high
correction score does not change ordinary top one. When the ordinary list is
empty, the raw-input policy still applies rather than implicitly committing a
suggestion. A conflict-free keyboard entry/exit action must be tested and exposed
through supported settings before shipping; a mouse-only design is insufficient.
No new shortcut is assigned by this document.

Ordinary paging advances only the ordinary list. Opening/paging corrections does
not consume ordinary positions. Recovery exhaustion omits that update's suggestions;
it does not persistently disable the feature. All entries for a shown result are
frozen: no late AI reply revises either list. Reuse the existing candidate window
and warmed UI resources, with bounded rows and screen-edge placement tests.

Ordinary AI ranking runs as before. A channel model, existing ERNIE fusion or a
joint typo/ranking scorer may order the separate correction list without competing
for ordinary first-page positions. They share at most the existing single AI hold;
late correction scores fall back to channel order. This reduces clean-displacement
risk without claiming context is correct or solving missing candidates.

## Native selection and protocol requirements

Keep separate ordinary and correction streams in the same native session. Export
ordinary pages after their normal translators, conversion, deduplication, pins and
ranking. Do not partition an already interleaved page and call that unchanged
paging: that would retain the displacement bug. Keep a bounded native correction
candidate registry with raw consumed spans, canonical code, edit provenance,
composition revision and generation/session identity. Never replay corrected input.

The client receives opaque correction selection tokens, not native pointers or
indices copied from a previous captured menu. A selection must match the current
composition/generation and retained candidate, otherwise it is rejected without
committing. Backspace, new input, focus changes and generation changes invalidate
stale tokens. Ordinary selection messages retain their current interpretation.
Add capability negotiation and append-only/versioned result and selection extensions
with shared Go/C++ vectors; older clients receive ordinary candidates only.

Existing `Context::Select(index)` and `Segment::GetSelectedCandidate()` resolve
through the ordinary menu. They cannot directly select a candidate held only in a
side registry. A maintained-fork hook must retain an explicit selected native
candidate and drive the normal selection notification/commit path. Audit all
selected-index consumers, highlight, reopening, deletion and clearing; merely
committing the candidate's text bypasses canonical learning and partial selection.
Retain Phrase/Sentence identity and the original raw-byte span. Selection of a
corrected prefix must leave exactly the untouched suffix. Keep native calls on
the engine's locked thread and bound registry lifetime with the composition.

## Acceptance evidence still needed

Compare the entire ordinary stream and its page boundaries with correction off,
including native symbols, English, abbreviations, pins, conversion and AI on/off.
Verify direct selection of hidden native candidates, partial selection, Backspace,
raw bypass, stale clicks, focus/generation changes and learning persistence in
scratch dictionaries. Exercise keyboard access and both platform UIs in disposable
VMs. Measure additional export work, allocations, window placement and memory.
The current counterfactual is not a substitute for those tests.

This proposal changes presentation, not the unresolved recovery algorithm. The
longer-form cold misses `cnjw` and `ddegn`, broad coverage, missing letters and
cross-boundary swaps remain separate work. Do not relax those goals or advertise
all clean-input regression as solved based on this layout analysis.

The [independent recovery extractor](typo-recovery-stream.md) now isolates
recovery iteration from the ordinary cursor. The scratch engine still uses the
legacy interleaving adapter until separate publication is connected.

Follow-up: [the native engine separation experiment](typo-engine-separate-stream.md) now
stores recovery outside the ordinary menu and verifies real native selection.
Client publication, conversion and provenance remain outstanding.
