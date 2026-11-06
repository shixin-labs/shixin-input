# Native selection outside the ordinary menu

Date: 2026-11-04. Scratch fork hook and native tests; no separate UI yet.
Parent: [separate correction suggestions](typo-separate-corrections.md).

A correction stored outside the ordinary menu cannot be selected through an
ordinary index. Inserting it temporarily changes menu state; committing its text
directly bypasses native phrase identity. This experiment adds a selected-candidate
override to Segment and a Context hook using the existing selection notification.

## Hook contract

`Context::SelectExplicitCandidate` accepts a retained native candidate for an
active, not-yet-selected segment. Its raw span must start at the segment start,
be nonempty, and fit within the segment and input. The hook stores the same
object, marks the segment selected and invokes the normal selection notifier
once. It does not modify the menu, rewrite input or directly commit Chinese text.

This is an **internal hook, not a stale-token validator**. Its caller must validate
session/generation/composition revision and ownership. Matching spans cannot prove
that a candidate belongs to current input. No client protocol exposes this hook.

## Selection-state audit

| Consumer | Scratch handling |
|---|---|
| Segment selected candidate | Resolve the explicit object before the ordinary index |
| Segment close | Existing partial-span split uses the same native candidate |
| Segment reopen | Same-caret reopening retains the selection; changed-caret invalidation clears it |
| Segment clear / engine menu regeneration | Release the override |
| Ordinary selection | Validate the index, then clear the override |
| Ordinary highlight | Clear and notify even when the numeric index stays unchanged |
| Delete current selection | Notify with the explicit candidate |
| Indexed deletion | Validate first; invalid indexes preserve explicit selection |
| Punctuator/switcher direct index writes | Clear the override |
| Selector Home | Return to ordinary highlight even when the saved index is already zero |
| Memory selection/commit | Existing genuine-candidate unwrapping reaches native Phrase metadata |
| C API page/highlight export | Still ordinary-index based; correction-selection state remains unimplemented |

Reopening must not silently replace a chosen phrase with ordinary top one.
Returning to an ordinary row must clear the override even if its numeric index
did not change. Segment's internal layout changes, so native plugins and the
scratch library are rebuilt together. This is not a stable ABI extension.

## Evidence and limits

Five standalone tests use native Context, Segment, Menu and Phrase objects:

- Select a phrase absent from a 16-entry menu; verify every ordinary object and
  position is unchanged and phrase code/language/weight survive.
- Close and reopen a selected prefix, retaining the untouched `zhongguo` suffix;
  then return to ordinary highlighting.
- Reject invalid spans/null candidates; exercise explicit deletion, invalid
  indexed deletion and ordinary selection afterward.
- Observe normal Context commit notification text and release after clearing.
- Keep a filter wrapper's displayed text while unwrapping the original Phrase,
  matching the access used by native Memory consumers.

These are fixture Phrases, not dictionary-generated recovery candidates. They
do not prove real selection integration, persistent learning, stale-token
rejection, filter parity or keyboard/UI behavior. The bounded registry, export
bridge and ordinary-stream separation remain missing. The current mixed-list
prototype therefore still has later-page displacement.

Before the final Home adjustment, all ten existing engine editing fixtures passed.
The 2,100-key trace measured p50 56 µs, p99 960 µs, p99.9 1.152 ms and maximum
1.259 ms. This exercises the existing mixed-list flow, not side-list selection.
Allocation and learning gates remain open; the extra shared pointer per Segment
is also a memory cost.

After that adjustment, the final Release repeat passes all ten fixtures and
measures p50 56 µs, p99 704 µs, p99.9 1.152 ms and maximum 1.273 ms.

All five standalone tests pass in Release and with the full native library under
ASan/UBSan. Both native CTest suites pass under both configurations. All ten
existing engine editing fixtures also pass with the sanitizer library, using
the relaxed functional deadline; that run is not latency evidence.

Source: `spikes/typo-recovery/native-graph/explicit_selection_test.cc` and
`librime.patch`. Evidence: [typo-explicit-selection/](typo-explicit-selection/).
