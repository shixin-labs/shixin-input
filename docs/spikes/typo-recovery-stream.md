# Independent recovery extraction

Status: scratch refactor, not enabled in the product; 2026-11-04.

The recovery extractor no longer requires an ordinary translation.
`SpikeRecoveryTranslation` in
`spikes/typo-recovery/native-graph/recovery_translation.inc` consumes only the
two recovery stages and an optional fixed snapshot of up to ten exclusions.
It retains genuine native Phrase identity through the existing correction
ShadowCandidate. It does not consume, reorder or inspect an ordinary cursor.

The legacy `SpikeProtectedTranslation` now delegates correction enumeration to
this extractor. It still interleaves corrections after the first ordinary page
for comparison with previous captures. This adapter still causes later-page
displacement; extracting the stream is not itself the clean-input fix.

## Bounds and semantics

The existing limits remain: at most 64 inspected entries, 32 per phase, eight
accepted corrections, six full-span choices and two per shorter span. The first
phase full-span quota is supplied by the caller (four normally, two for the
three-initial experiment); the second phase allows four. Only candidates with
one correction according to the source translation qualify. Duplicate text and
raw span are suppressed against accepted corrections and the exclusion snapshot.
Repeated Peek calls return the same candidate without advancing either source.
Null or non-positive-span candidates are rejected.

Exclusions do not constitute a complete ordinary-menu deduplication guarantee:
looking through all ordinary pages would defeat independent bounded extraction.
The separate presentation design must specify duplicate handling without
mutating or exhausting ordinary candidates. Neither this refactor nor the
legacy graph provides the typed edit alignment required by the registry yet.

The legacy adapter allocates a separate extractor object, and correction wrappers
still allocate. No native zero-allocation or whole-engine memory claim follows
from the registry's separate allocation test. Product integration must resolve
this ownership/storage issue before meeting the allocation gate.

## Verification

Four standalone tests pass in Release and ASan/UBSan:

- Drain correction results while a 40-candidate ordinary source retains its
  cursor, candidate identity and complete ordering; preserve the genuine Phrase.
- Suppress duplicates, zero/multiple-edit candidates and enforce phase/span quotas.
- Stop at 32 inspections per phase, handle a missing first stage and empty sources.
- Preserve ordinary relative order and the old insertion point in the legacy adapter.

The deterministic recovery source in these tests substitutes only dictionary
lookup and correction counting; the actual extractor and adapter are included.
These tests do not prove dictionary recall, edit alignment or UI behavior.

Both rebuilt native libraries pass all ten scratch-engine editing fixtures.
The Release 2,100-key trace reports p50 52 µs, p99 832 µs, p99.9 1.259 ms and
maximum 1.259 ms. This is one warm fixture trace, not a broad latency claim or
an allocation measurement. Sanitizer functional runs use the existing relaxed
time deadline and are not latency evidence. The maintained librime patch still
applies to its pinned base; reproduction now requires copying both `.inc` files.

Evidence is stored in [typo-recovery-stream/](typo-recovery-stream/).

## Remaining integration

Route the independent stream into a session-scoped native store instead of the
ordinary menu, carry authoritative edit provenance, apply appropriate conversion
and deduplication, and publish through the bounded correction registry. Engine
revision/lifetime invalidation, versioned client export/selection, learning
persistence and actual complete ordinary-page comparisons remain unverified.
Do not treat this refactor as release-ready or disable the bounded fallback.

Follow-up: [the native engine separation experiment](typo-engine-separate-stream.md) now
stores recovery outside the ordinary menu and verifies real native selection.
Client publication, conversion and provenance remain outstanding.
