# Dictionary subtree bounds: three-initial recovery

Current follow-up: [native initial cache](typo-initial-cache.md) replaces startup
scanning with offline reading buckets. Warm editing passes; cold recovery still
misses. The results below remain historical evidence for their named versions.

Date: 2026-11-04 · Read-only search experiment, **not integrated or shipped**

Parent: [typo design](../typo-tolerant-pinyin.md),
[native graph experiment](typo-native-graph.md).
Code: [search workspace](../../spikes/typo-recovery/native-graph/subtree_bounds.h)
and [exhaustive test](../../spikes/typo-recovery/native-graph/subtree_bounds_test.cc).

The native prototype misses `zhr → 中国人`; exploring all correction positions,
prefix-frequency ordering and a small beam did not fix it. A further experiment
round-robins native search stacks by correction position. It still misses `zhr`
and regresses `xgr` under the original deadline; its
[log](typo-subtree-bounds/rejected-fair-positions.log) and
[rejected patch](../../spikes/typo-recovery/native-graph/rejected-fair-positions/README.md)
are retained. The active native patch remains the earlier depth-first prototype.

## What changed

Prefix-word frequency is not a bound on complete phrases below that prefix.
Instead, preprocess the existing dictionary's one- and two-syllable prefixes:
each stores the maximum weight of a reachable **three-syllable** entry. A
best-first search can then stop once no remaining branch can improve the eighth
candidate. Equal-weight branches must still be visited for deterministic ties.
An empty subtree uses negative infinity, not zero: the exhaustive reference
caught a bug that otherwise omitted legitimate zero-weight entries.

The experimental search accepts exactly three lowercase initials and exactly
one adjacent-key substitution. It does not claim to implement mixed pinyin,
digraph initials, swaps, missing letters or arbitrary-length abbreviation.
For `zhr`, it explicitly tests the `z + h + r` interpretation, yielding the
`z + g + r` reading of 中国人. Ordinary `zh + r` interpretations must remain
available through the normal translator. This is an additional retrieval path,
not a replacement for existing pinyin segmentation.

A 4,096-state fixed heap and fixed eight-entry result array replace the first
prototype's dynamic heap and strings. Candidates retain native mapped entry
pointers and their three canonical syllable IDs. No text decoding occurs inside
this search. Existing sorted dictionary children are intersected with validated
ranges of syllable IDs sharing an initial. Once the edit is spent, unrelated
initials are not probed. This reduced `xgr` from 36,705 probes to 5,579 without
changing its exhaustive top eight or increasing the cap. Non-lowercase syllables
are excluded from this diagnostic contract; noncontiguous lowercase ID ranges
make initialization fail rather than silently omitting candidates.

## Evidence and memory accounting

The [Release run](typo-subtree-bounds/release.log) loads the pinned compiled
rime-ice table without creating any Rime session or opening a user dictionary.
It enumerates all 461,449 three-syllable entries, producing 6,110,291
entry/corruption combinations as an independent exhaustive reference. For all
**17,576** lowercase three-letter codes, search returns exactly the same top
eight native entries and canonical readings as that reference, with no budget
exhaustion. Each returned entry is also checked against native `QueryPhrases`.
This finite-domain result is not real-user typo accuracy or full-pinyin recall.

| Resource or check | Result |
|---|---:|
| One-syllable IDs / two-syllable prefixes | 440 / 91,335 |
| Bound floats alone | 367,100 bytes (358.5 KiB) |
| C++ metadata payload, including prefix records and initial bytes | 383,380 bytes (374.4 KiB) |
| Reusable search object | 66,432 bytes (64.9 KiB) |
| Maximum queued states over all codes | 2,746 / 4,096 |
| Maximum expanded states over all codes | 1,628 |
| Maximum probes over all codes | 9,451 / 32,768 |
| Measured search allocations | 0, including every code in the sweep |

Metadata payload excludes allocator bookkeeping, mapped dictionary pages and
process/runtime overhead. The result array and small function stack are additional
to the search-object size. Shared metadata must be tied to dictionary generation;
it must not be duplicated per session. The 256 KiB proposed workspace target
cannot be claimed as a total process-memory result. No separate phrase-text index
or neural model is loaded, but the bounds **are additional index metadata**.

The final isolated timing run has warm medians of 23.75 µs for `xgr` and 21.959 µs
for `zhr`, over fifty measurements each. Both return 中国人 first. Earlier runs
varied, and these measurements exclude graph construction, text decoding, native
candidate creation, user dictionaries, filters, AI and IPC. The first dynamic
prototype's first query took about 27 ms, while later fixed searches were much
faster. That is not a controlled cold-start improvement: preprocessing already
touches dictionary pages, and string decoding was removed from the timed search.
The latest metadata preprocessing takes about 128 ms. Startup/RSS and actual
cold engine key behavior must be measured before integration can pass.

The [ASan/UBSan run](typo-subtree-bounds/asan.log) repeats the exhaustive test and
passes. The new search and test are instrumented; the read-only loaded librime
library is the Release build. This is not a new full-library sanitizer claim.
Leak detection is disabled. The test intercepts ordinary C++ `new`/`new[]` around
search, outside metadata preparation and reference construction; no allocation
claim applies to the complete candidate pipeline.

## Exhaustion and integration decision

The API returns `complete=false` for invalid input, probe exhaustion, a full
frontier, or an expired deadline. Tests force zero/one-probe exhaustion, a
one-slot frontier and an already-expired deadline. The optional clock guard is
checked every 64 probes. A probe here is not the same accounting unit as the
native graph's work budget; heap comparisons are bounded by the fixed frontier
but are not charged as dictionary probes. A caller must share the original
composition deadline rather than grant this path a new wait.

For initial integration, an incomplete result contributes **no extras** and the
ordinary menu remains available. This is per-update fallback, not a persistent
feature toggle. Retry only after input changes. Do not enlarge buffers, restart
the deadline, launch a late replacement menu or change the user's setting.
A later policy may retain validated completed candidates, but only after native
span/provenance and selection tests establish safety. Repeated actual latency
overruns can suspend correction temporarily; normal bounded-search exhaustion
alone is not a reason to disable it globally.

Next steps remain substantive:

1. Connect mapped entry/code results to native Phrase/Sentence construction and
   one-edit alignment in the existing session; preserve original raw spans,
   partial selection, Backspace and learning identity. This diagnostic never
   fabricates a selectable candidate or replays alternate raw strings.
2. Prepare and validate bounds before key processing, preferably offline with
   dictionary identity/version checks. Measure shared metadata and mapped-page
   costs across the session pool, including cold keys and generation swaps.
3. Run the ten-case native editing and latency gates on that same integration;
   `zhr` is still failing in the current engine. Recheck clean first-page
   protection and existing clean later-page losses.
4. Extend the measured approach to other abbreviation lengths, mixed forms and
   swaps. Then evaluate fresh examples separately from this exhaustive but
   narrowly defined dictionary test. Do not call the overall feature fixed from
   the three-initial result.

No model decision changes: the result supports improving bounded retrieval
before adding a new neural scorer. Existing ERNIE or a jointly conditioned model
still needs candidate-pool, ranking, clean-input and deadline ablations.


[Artifact hashes](typo-subtree-bounds/sha256.txt) pin the table, search sources,
library and final logs. After restoring the native depth-first patch, the
[native control](typo-subtree-bounds/native-control.log) still passes its original
nine editing cases and fails `zhr`. The isolated search has not been silently
substituted for that failing end-to-end test.
