# Bounded correction registry

Date: 2026-11-04. Standalone native prototype, not connected to engine export/UI.
Parent: [explicit selection](typo-explicit-selection.md) and
[separate corrections](typo-separate-corrections.md).

The registry retains up to eight native choices outside the ordinary menu. A
choice is read through a bounded snapshot and selected through the explicit
native selection hook. No ordinary index is inserted, removed or reassigned.

## Lifetime and selection contract

Each token contains engine-generation, session-incarnation and composition-
revision identifiers, a publication sequence and a slot. These are C++ fixture
types, **not wire structs**. The future bridge must use versioned serialization
and Go/C++ compatibility vectors, not memcpy their padding.

The engine must supply authoritative current scope values independently of the
client token, never reuse session incarnations, and advance/invalidate revision
on relevant composition/focus/generation changes. The registry is bound to its
native Context pointer and raw input snapshot. These caller responsibilities are
not yet wired to engine lifecycle; span matching alone is insufficient.

Publication validates all entries before retaining them. Reads and selection
check current scope, raw bytes, active segment, stored raw spans, native Phrase
identity, displayed text and canonical code. Changed content is rejected. Valid
selection invalidates the whole registry before the synchronous native selection
notification, preventing reuse during a callback.

A sealed snapshot rejects same-revision replacement, including replacing a
displayed empty list. New revisions can publish fresh snapshots. Before sealing,
each publication increments its sequence so old slot tokens cannot alias a new
choice. Publication-counter exhaustion fails closed without wraparound. Clearing
releases retained candidates; a registry must be cleared before its native
session/generation and library are destroyed.

## Bounds and provenance

| Item | Bound |
|---|---:|
| Choices | 8 |
| Raw input snapshot | 32 bytes |
| Displayed text per choice | 256 bytes |
| Canonical code per choice | 32 syllable IDs |
| Fixed registry storage on this build | 3,696 bytes |

Oversized values are rejected, not silently truncated. Views contain fixed-buffer
text, spans, edit metadata and tokens; they do not expose native pointers. A view
is valid only until the next registry mutation and must be copied by an exporter.

Typed edit metadata carries kind, raw-byte edit span and finite nonnegative cost.
Validation checks span shape (for example, a swap consumes two adjacent raw
bytes). It does **not** prove that a proposed spelling is correct, that keys are
neighbors or that costs are calibrated. Actual recovery must provide provenance
from its selected spelling path; the current mixed translator does not yet feed
that information into this registry. Missing/extra edit enum values do not
implement missing/extra-letter recovery.

Fixed storage excludes the heap retained through Phrase, filters and syllabifier
objects. Eight retained pointers do not establish an upper bound on whole-engine
memory. The same native objects must also survive downstream conversion and
selection without replaying input or bypassing learning.

## Tests and measurements

Nine standalone tests pass in Release and against the full native library under
ASan/UBSan. They cover:

- Ordinary-menu identity and selection invalidation before callbacks.
- Wrong scope, context, publication, slot, raw input and segment span.
- Frozen shown lists and empty snapshots; new-revision publication.
- Mutation of native text, canonical code or consumed span.
- Eight/nine-choice limits, invalid edit metadata and retained-object release.
- Exact raw/text/code bounds and absolute spans after a prior selection.
- Publication counter overflow without token reuse.
- Warm publication/read/selection allocation and timing.

The final Release run records zero intercepted C++ `new`/`new[]` calls across
1,000 cycles, each publishing eight choices, reading all eight, sealing and
selecting one. Mean time is 4,627 ns per cycle. Native notification machinery is
warmed beforehand. This is a single-thread fixture microbenchmark, not an engine
key-path, malloc-family or full retained-memory guarantee; sanitizer timings are
not performance evidence.

The registry is not yet a live clean-input fix. Recovery extraction, typed
provenance, filter parity, lifecycle invalidation, native export, persistent
learning, AI deadlines, client protocol and keyboard/UI behavior remain to be
connected and tested. Until ordinary and correction streams are actually
separated, the mixed prototype's later-page displacement remains.

Source: `spikes/typo-recovery/native-graph/correction_registry.h` and
`correction_registry_test.cc`. Evidence:
[typo-correction-registry/](typo-correction-registry/).

Follow-up: [native edit provenance and registry selection](typo-edit-provenance.md)
connects actual recovery candidates to registry selection in a scratch native
test, with apostrophe-anchor and nested-wrapper fixes. Product publication
and learning-persistence checks remain open.

Follow-up: [context-owned native publication](typo-context-publication.md)
adds engine-driven snapshots and revision/incarnation invalidation. The real
worker-generation bridge and client protocol remain outstanding.
