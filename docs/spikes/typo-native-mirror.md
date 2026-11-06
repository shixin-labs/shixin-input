# Native compact-mirror integration

Date: 2026-11-04. Scratch engine experiment, not shipped.

The [standalone mirror](typo-compact-mirror.md) is now connected to the native
candidate pipeline in the isolated librime patch. The ten editing fixtures pass
on the first run and two repeats. `xgr`, `zhr` and `njh` also pass individually in
three fresh workers each. This resolves the observed focused first-run failures
in these runs; it does not prove all cold inputs, abbreviation forms or machines.
The broader paired corpus retest is complete with frozen artifacts; its results
and limits are below.

## Generation preparation and trust boundary

The offline native test verifies all 77,447 mirrored reading/text/weight records.
The new Go `spikes/typo-recovery/prepare-mirror` tool consumes a reviewed manifest
with explicit table, prism, initial-cache and mirror SHA-256 hashes and sizes,
format version and builder identity. It copies and hashes the same bytes into a
new private directory, then makes its files read-only. It never validates one
source and later asks the engine to reopen that mutable source. Invalid manifests,
changed files and unexpected sizes fail preparation; incomplete new directories
are removed and existing destinations are never replaced.

Tests cover each altered artifact, role/path/version/digest/size errors, trailing
JSON, source mutation after copying and preservation of existing destinations.
The builder hash records provenance; this helper does not authenticate an
untrusted manifest or independently attest the builder. The test manifest is
created from the verified offline run. Its trust derives from that controlled
workflow. Product builder/activation integration remains pending.

The scratch warm schema points to the private initial/mirror copies. Its staging
directory resolves the table and prism to those same verified copies. The ordinary
prebuilt directory still supplies English/radical resources, avoiding an unrelated
change to candidate coverage. Capture runs only after preparation succeeds and
uses a scratch user directory with learning disabled. No installed IME changes.
[Input manifest](typo-native-mirror/input-manifest.json) and
[prepared snapshot manifest](typo-native-mirror/snapshot-manifest.json).

## Native data and lifetime

Table loads the mirror during schema initialization. A new accessor carries an
optional cached-text view alongside the existing native entry, canonical code,
credibility and quality. Dictionary chunks retain that view, and normal DictEntry
materialization copies the cached UTF-8 text instead of decoding the native string
table. Diagnostic logging also uses the materialized text. Phrase/Sentence candidate
types, raw spans and selection behavior remain native; no alternative input strings
or shadow sessions are created. Actual user-dictionary learning persistence is
not tested by the capture worker and remains a separate gate.

The mirror is loaded once per shared Table. Repeated preparation with the same
path does not replace its buffers; a different path is rejected. A translator whose
requested mirror cannot load uses ordinary translation. Previously active accessors
retain their original pointers. Missing data and budget exhaustion contribute no
partial cache candidates. Component tests verify canonical Chinese reading,
DictEntry text/code, stable pointers, missing data, deadline expiry and reset.
All native components are rebuilt together because TableAccessor's internal layout
changes. This is not a stable ABI or a client protocol change.

## Measurements and checks

| Check | Result |
|---|---|
| Release editing | All ten cases pass in three consecutive runs, repeated after the final guard change |
| Individual fresh-worker `xgr`, `zhr`, `njh` | Each passes three runs |
| Full-library ASan/UBSan editing | All ten pass; functional-test clock relaxed, operation caps unchanged |
| Native identity/lifetime/expiry tests | Three tests pass in Release and full-library ASan/UBSan |
| Native ctest suites | Both pass in Release and sanitizer builds |
| Missing mirror / zero probe budget | Six menus each exactly match ordinary text, order, comments, preedit and verified spans |
| Final 2,100-key warm trace | p50 56 µs; p99 704 µs; p99.9 1.024 ms; maximum 1.529 ms |
| Earlier trace before final unavailable-mirror guard | p99 1.024 ms; p99.9 1.792 ms; maximum 4.733 ms |
| One-context RSS samples, 1,350 compositions | Protected 58,490,880 bytes (55.78 MiB); ordinary 52,576,256 bytes (50.14 MiB) |

The memory delta is approximately 5.64 MiB in this sample. It includes the native
pipeline and is not just the 3.17 MiB cache payload. The first snapshot startup took
1.153 seconds; later starts in the editing runs were 67–68 ms. No filesystem-cache
flush was performed. These are observed startup times, not a startup guarantee.
The memory run preceded the final translator failure flag; it does not measure
all-session memory or transient peak. The final warm latency trace passes the
existing percentile gates, but does not establish incremental correction budgets
or a hard 900 µs bound on every native operation. Full graph/candidate allocations
remain; standalone search's zero-allocation result is not a whole-engine claim.

See [final editing](typo-native-mirror/mirror-native-final-editing.log),
[latency](typo-native-mirror/mirror-native-final-latency.log),
[RSS](typo-native-mirror/mirror-native-protected-rss.log),
[fallback](typo-native-mirror/fallback/comparison.json),
[snapshot tests](typo-native-mirror/mirror-snapshot-tests.log),
[native sanitizer](typo-native-mirror/mirror-native-asan-components.log) and
[artifact hashes](typo-native-mirror/sha256.txt).

## Remaining work

The completed 731-code capture covers 830 reused synthetic examples. Typo
full-span top-13 recall is 43/553 ordinary versus 99/553 protected (59 gains,
three losses). Clean top-13 recall is 182/277 versus 173/277 (two gains, eleven
losses); clean top-five remains 153/277. The only first-page differences are
live time/timestamp values for `sj` and `ts`, inspected directly. These are
availability/ranking measurements on reused tuning data, not real-user accuracy.
[Aggregate](typo-native-mirror/evaluation/aggregate.json),
[full evaluation](typo-native-mirror/evaluation/summary.json).

Two targets present in the earlier offset-cache capture disappear here:
`cnjw → 纯净物` and `ddegn → 吊灯`. A focused interleaved retest with the same
current library finds both absent in the first offset and mirror workers, then
both present at ranks 7 and 11 in the next two workers of each variant. This
indicates first-access/budget sensitivity beyond the three-initial mirror lane;
it does not prove the exact timing cause or make the misses acceptable. Both
longer-form paths still depend on native lookup. Do not describe the mirror as
fixing all cold input or as giving exactly invariant engine recall under deadlines.
[Focused retest](typo-native-mirror/evaluation/delta-retest/summary.json).

The [separate-correction layout analysis](typo-separate-corrections.md) addresses
the eleven clean later-page displacements without claiming a ranking improvement
from additional screen positions. It is a design candidate, not an implemented UI.

Production generation lifecycle, learning persistence, all-session memory,
incremental allocation/latency, broader abbreviation and mixed-input coverage,
missing letters/cross-boundary swaps and AI integration remain open. Base recovery
still needs no neural model. These results support continuing the bounded native
retrieval design; they do not select or validate a joint neural correction/reranker.
