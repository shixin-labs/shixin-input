# Native three-initial cache experiment

Date: 2026-11-04. Status: isolated experiment, not shipped; cold recovery remains incomplete.

This follows the [subtree-bound search](typo-subtree-bounds.md) and
[native graph prototype](typo-native-graph.md). It changes only the scratch
library patch and offline fixtures, not product settings or the installed IME.

## Decision and mechanism

Scanning the native dictionary to build subtree bounds at engine startup recovered
the ten editing fixtures, but sampled process RSS reached 94 MiB versus 50 MiB
ordinary. An offline bounds file avoided the scan, but cold traversal still
exhausted the 900 µs shared deadline. Those are rejected as the current integration.

The replacement prepares the eight highest-weight native entries for each clean
three-letter initial code offline. Each record stores the native table offset,
three canonical syllable IDs and weight, without copying Chinese text. At query
time, enumerate one keyboard-neighbor substitution at each of the three positions,
read the corresponding buckets and merge their top eight into fixed storage.
The top eight of the union must occur among the top eight of its constituent
buckets under the same total ordering. This avoids a dictionary traversal queue
for this narrow lane; it does not replace spelling graphs or native candidate work.

The cache has 77,447 records and a 1,309,460-byte payload (about 1.25 MiB).
The search object occupies 676 bytes, excluding result storage and caller stack.
All 17,576 lowercase input codes match an independent exhaustive reference over
461,449 three-syllable entries, including canonical readings and native offsets.
The largest query uses 144 candidate probes. This is exact top-eight retrieval
under dictionary weight for one neighboring substitution, not a language-accuracy
claim or coverage of arbitrary abbreviations, mixed forms or swaps.

Loading happens during schema initialization, never on a key. The loader validates
format, length, table identity, bucket bounds/order, finite weights, canonical
initials and offset ranges. Selected entries are additionally checked against their
native reading's entry list and weight before native Phrase creation. A production
builder, authenticated generation manifest and compatibility lifecycle remain open.

Three-letter recovery tries the cache stage first (two full-span output slots),
then the specific-spelling stage (four). Both share the original deadline and
remaining operation budgets; the overall recovery limit remains eight, including
at most six full-span entries. Other lengths retain the earlier search strategy.
Ordinary first-page candidates remain protected. Later pages can still lose targets
within a finite capture window; this is not a broad clean-input safety result.

## Exhaustion contract

An incomplete cache stage contributes nothing and suppresses subsequent recovery
for that update. Missing/invalid cache data also returns ordinary translation.
A fully completed cache stage can survive exhaustion of the optional later stage;
the incomplete later stage is discarded. No partially validated entry is exposed,
no deadline is restarted and no shown menu is revised. This does not change the
persistent feature setting. Six forced-zero-budget and missing-cache menus match
ordinary text, order, comments, preedit and verified consumed spans exactly.
See [fallback evidence](typo-initial-cache/fallback/comparison.json).

## Evidence and remaining failures

| Check | Observed result |
|---|---|
| Current Release editing, three repetitions | First run misses `njh` and `xgr`; subsequent two pass all ten |
| Full-library ASan/UBSan editing | All ten pass with a relaxed functional-test clock; not a latency result |
| Exhaustive cache/reference comparison | All 17,576 codes match; no C++ new/new[] observed inside search |
| Cache parser ASan/UBSan | Round trip, wrong identity and seven malformed variants pass |
| Native component and merge tests | Pass, including deterministic exhaustion and stage quotas |
| Warm engine trace, 2,100 keys | p50 56 µs, p99 1.024 ms, p99.9 1.408 ms, max 1.785 ms |
| Sampled RSS, 1,350 compositions, one context | Ordinary 50.59 MiB; protected 54.34 MiB |

The Release editing suite is **not green overall**. Warm success and sanitizer
functional success do not resolve cold misses. RSS samples are not transient peak,
private-dirty memory, all-session memory or incremental allocation measurements.
The legacy graph/candidate pipeline still allocates. The paired corpus evaluation is complete: 731 distinct codes cover 830 reused
synthetic examples. Typo top-13 recall is 43/553 ordinary versus 101/553 protected
(60 gains, two losses). Clean top-13 recall is 182/277 versus 173/277 (two gains,
eleven losses); clean top-five recall remains 153/277. These aggregates happen
to equal the preceding depth-first prototype, but are independently captured
with the frozen cache library. The two prefix differences are the live time codes
`ts` and `sj`. This is reused tuning data, not a fresh holdout. See
[aggregate metrics](typo-initial-cache/evaluation/aggregate.json) and
[full evaluator output](typo-initial-cache/evaluation/summary.json).

Logs and negative trials are in the evidence directory. The
[capture manifest](typo-initial-cache/capture-sha256.txt) identifies the pre-fix
library used for the paired corpus; the [current manifest](typo-initial-cache/sha256.txt)
identifies the later deadline repair. Do not attribute the earlier corpus capture
to that later library. In particular,
[editing repetitions](typo-initial-cache/cache-stage-quotas-editing.log),
[oracle](typo-initial-cache/initial-cache-oracle-validation.log),
[parser sanitizer](typo-initial-cache/initial-cache-parser-asan.log),
[latency](typo-initial-cache/initial-cache-latency.log) and
[RSS](typo-initial-cache/initial-cache-protected-rss.log) delimit the claims above.
Reproduction and cache generation are described in the
[spike README](../../spikes/typo-recovery/native-graph/README.md).

The [stage diagnostic](typo-initial-cache/initial-cache-stages.log) changes the
next investigation: graph construction took 64–251 µs on first queries, whereas
native lookup took 1.23–3.75 ms, then a few microseconds after repeats. This is
fresh-process read-only lookup, not an engine trace or guaranteed OS-cold-cache
measurement. In some cases lookup exceeded 900 µs without setting the exhaustion
flag because cooperative checks did not occur after the costly operation. The
translator's final clock check can reject those results, but cannot undo time
already spent. Do not treat the nominal deadline as a proven hard bound.

Next: separate native table access from dictionary result materialization, repair
the deadline-check gap, and reduce cold lookup cost without relaxing the budget.
Then address clean later-page losses and broader abbreviation/partial-input coverage
and learning.
AI fusion, clean later-page policy, generation lifecycle, allocation and session-pool
memory remain production gates. No model choice follows from this retrieval test.

## Deadline repair follow-up

The table-only diagnostic reproduces a 3.85 ms first lookup before any dictionary
collector sorting, narrowing the remaining cold cost to native table access.
The scratch patch now checks the clock before each cached candidate, after
`QueryPhrases`, after visitor publication, at cache completion and around collector
sorting. Incomplete direct table results are cleared. The three-initial dictionary
collector discards the batch when exhausted. This closes a gap where fewer than 64
work steps after a slow native operation could return without recording expiry.
It cannot preempt a native lookup already running and does not solve cold latency.

A new regression test expires the deadline during a visitor callback, verifies
rejection and empty direct results, then resets the budget and verifies recovery.
It passes in Release and full-library ASan/UBSan. Both native test suites pass;
all ten engine editing cases pass in the sanitizer functional configuration
(relaxed test clock, unchanged operation caps). Release editing across three runs
still misses `njh` in the first run and passes all ten in the next two.
The 2,100-key warmed trace is p50 56 µs, p99 1.024 ms, p99.9 1.152 ms and
maximum 1.452 ms. No whole-pipeline allocation or cold-performance claim follows.
Evidence and the exact follow-up patch are in [deadline repair logs](typo-initial-cache/deadline/cache-deadline-stages.log).

Next investigation remains reducing the native table page accesses needed to
validate cached entries without weakening reading identity or warming the entire
dictionary. The cache must remain dictionary-backed, and generation validation
must be explicit if any verification moves from query time to offline preparation.

## Cold access breakdown and proposed compact mirror

A fresh-process read-only diagnostic splits native access into reading lookup,
entry membership, weight access and Chinese text decoding. For the eight `xgr`
results, first reading lookups take 242–740 µs; some weight accesses take another
51–372 µs; text decoding takes 7–1,633 µs. Repeated steps are about 0–1 µs.
Membership iteration is one entry for these eight cases. Therefore eliminating
binary-search traversal alone cannot remove all cold costs: sparse native entry
and compressed string-table accesses remain. These are observed elapsed times,
not a controlled proof of physical disk faults or an engine latency measurement.
[Raw evidence](typo-initial-cache/access/cache-access.log).

An offline sizing pass verifies all 77,447 cached code/entry/weight associations
against native `QueryPhrases`, decodes their text, and interns equal text. It finds
77,340 distinct strings requiring 773,454 bytes including terminators. With the
existing 70,308-byte bucket index, proposed records of 28 or 32 bytes would give
3,012,278 or 3,322,066 bytes of payload (2.87 or 3.17 MiB). These are layout
projections from measured counts, not an implemented loader or measured engine
RSS. Offline construction uses temporary containers that would not be retained
in the engine. [Sizing evidence](typo-initial-cache/access/cache-mirror-size.log).

The next experiment should prepare one immutable compact mirror per native table
and generation, containing only the existing bounded cache's selected entries:
canonical code, native entry identity, weight and pooled UTF-8 text. It must not
expand all typos or copy the full dictionary. The native TableAccessor and
DictEntryIterator would need an explicit bounded cached-entry/text path, preserving
Phrase identity, canonical code, raw consumed span and normal selection/learning.
No special candidate type or alternative input replay is needed. A stable shared
buffer must outlive every accessor; it cannot move after schema activation.
The existing DictionaryComponent shares Table objects by dictionary name, but
multi-schema/generation lifetime and measured session-pool memory still require tests.

Moving per-key native validation offline requires a generation contract, not simply
trusting cache offsets. The builder must verify every reading/text/weight association
and emit versioned table/prism/cache digests plus compiler identity. A private
immutable generation must be validated before activation, with the verified cache
bound to that exact table and prism. Current generation code does not yet provide
this cache digest contract. Structural parsing, UTF-8/length checks and file/record
caps remain necessary even after digest verification. Missing/mismatched artifacts
must use ordinary fallback. Do not remove runtime validation from the current cache
until the replacement contract is implemented and tested.

Compare ordinary, current offset-cache and compact-mirror engines on identical
menus, fresh-process editing, partial selections/backspace, cold and warm latency,
allocations, startup and full session-pool RSS. Repeat exhaustive code/reading/text
agreement and corruption/mismatch tests. This proposal addresses sparse table access;
it does not solve short-code ambiguity, later-page displacement, missing readings,
other abbreviation lengths or model ranking by itself.

The [standalone compact-mirror follow-up](typo-compact-mirror.md) implements and
tests the proposed storage representation. It preserves all cached readings/texts
and measures fresh-load queries without the native table. Generation validation
and engine integration remain pending; the offset-cache engine is unchanged.
