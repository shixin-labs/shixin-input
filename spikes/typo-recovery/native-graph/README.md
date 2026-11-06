# Scratch native spelling-graph experiment

This code is **not shipped** and is not a production patch. The experimental
library runs only in isolated capture workers and opt-in native/Go tests, with
validated warm schemas and private user directories. There are no shadow sessions or alternative-string
replays in the candidate pipeline. No installed IME or live dictionary is used.

Apply `librime.patch` to a scratch copy of librime commit
`3438a3b860cd3525f25ee2198c35ea49a3f2d85d`. Copy `edit_trace.h`, `prefix_edits.h` and
`bounded_corrector.inc`, `subtree_bounds.h`, `subtree_bounds_io.h` and
`initial_cache.h`, `mirror_cache.h` and `hypothesis_policy.h` to its `src/rime/dict/`, and
`protected_translation.inc`, `recovery_translation.inc`, `hypothesis_translation.inc`, `correction_filters.h`, `correction_registry.h` and `search_diagnostics.h` to `src/rime/gear/`, and `spelling_boundaries.h`
to `src/rime/algo/`. Copy `correction_api.h` and `correction_api.inc` into
`src/`. Build with Shixin's statically
merged native plugin and matching dependency headers/libraries, with no Lua,
external plugin loader or cgo. The prototype changes native candidate metadata;
all native plugins must be rebuilt together. This is not a stable ABI extension.

In a **copy** of a validated warm generation's `rime_ice_warm.schema.yaml`, add
under `translator`:

```yaml
enable_correction: true
shixin_spike_protected: true
shixin_spike_bounded: true
shixin_spike_initial_cache_file: /absolute/path/to/rime_ice.table.bin.initial3
```

Keep all user dictionaries disabled and reranking off. Use the existing
menu-capture tool with explicit engine/library/data/prebuilt/generation paths.
Baseline controls use the unchanged warm generation with the same library.

### Ranked spelling hypotheses (opt-in native implementation)

The combined `librime.patch` now includes the per-spelling policy. It is off
unless `translator/shixin_spike_hypothesis_model` names a valid model and
`shixin_spike_separate: true` is set. Export the small prior model offline:

```sh
CGO_ENABLED=0 go run ./spikes/typo-hypotheses -mode export-model -root build/typo-recovery/hypothesis-native-v1
```

The output directory must already exist. Set the model option to its absolute
`forms.tsv` path in a **private warm generation**. Keep the verified existing
initial/mirror cache options. An invalid configured model disables correction.
No model is loaded on a key, and no installed configuration is changed.

The policy ranks one-edit spelling hypotheses before dictionary lookup. The best
corrected spelling's segmentation chooses an eight-spelling allowance for full
pinyin or sixteen for abbreviated/mixed input. This is an intent heuristic.
Clear three-initial input prefers three-initial repairs over mixed interpretations
with a two-log-unit penalty; alternatives remain eligible. Apostrophes are hard
boundaries and cannot be edited or crossed. The planner can propose cross-syllable
swaps, but the native one-edge adapter only realizes supported within-edge edits.

Each spelling's native translation is consumed at most ten times, including
partials and entries later rejected for provenance. No refill beyond ten; no
eleventh entry is requested. Three-initial exact lookup uses the existing cache's
at-most-eight entries per spelling, so that lane can return fewer than ten.
Planning and all lookup stages share 32,768 work units, 2,048 nodes/chunks and
the original 900 µs cooperative deadline. An incomplete spelling's batch is
discarded; completed earlier batches survive. Finalization moves at most 160
candidate pointers and starts no lookup after exhaustion.

One candidate per ranked spelling precedes second candidates. The existing
bounded correction filter deduplicates and publishes **eight display entries**;
the candidate ABI and AI export are not enlarged. Native `Phrase` objects and
raw-span edit provenance are preserved. This does not replay alternative input
strings or create shadow sessions. User-dictionary selection/learning remains
native. The planner allocates no heap memory after model initialization; native
graphs/candidates still allocate. A ten-entry consumption cap does **not** bound
dictionary chunk collection/sorting to ten.

See [native results and remaining gates](../../../docs/spikes/typo-hypothesis-native.md).
The older two-stage algorithm described below remains the control when the model
option is absent. The earlier head-weight sidecar experiment is not included in
the combined patch or this tested build.

The optional `shixin_spike_canonical_table_file` points to an offline-built,
trusted auxiliary native Table (see `compact_table_test.cc`). The scratch
translator verifies matching syllable IDs and warms the table at initialization.
Non-three-initial correction lookup uses its entries and owning text decoder;
ordinary lookup and the existing three-initial mirror keep their original table.
Failure to prepare a requested table disables correction for that translator.
This extra artifact is not covered by the mirror snapshot preparer's four-file
manifest. The experiment uses only locally built data; production activation,
coverage and combined memory gates are still open. See
[`typo-canonical-cache.md`](../../../docs/spikes/typo-canonical-cache.md).
The truncated auxiliary replacement is rejected after the broader capture lost
eighteen longer targets. `shixin_spike_warm_original: true` instead touches the
original table's mapped pages during schema initialization, without a second
word table. This is a separate experiment; it does not guarantee residency or
change the correction deadline. Leave the canonical-table option unset for it.
`shixin_spike_warm_strings: true` is the smaller follow-up: it touches only the
compressed string table during initialization. Leave both full-table warming
and the canonical-table option unset when measuring it. See
[`typo-selective-warming.md`](../../../docs/spikes/typo-selective-warming.md).
The complete-table layout experiment uses `repack_table_test.cc` and fresh
initial/mirror sidecars bound by a new verified snapshot. With that layout,
`shixin_spike_warm_indexes` can be combined with string warming; it touches
index arrays only during initialization. It is expensive on the old interleaved
layout. Neither option guarantees residency or fixes the first-run miss yet.
See [`typo-repacked-table.md`](../../../docs/spikes/typo-repacked-table.md).
`shixin_spike_warm_short_entries` additionally warms index arrays and short-entry
payloads. The measured follow-up still misses the first `cnjw` query and increases
RSS, so it is an experiment control rather than a recommended configuration.

The bounded generator enumerates swaps and neighboring substitutions of raw
prefixes up to eight bytes, probing the existing prism; it adds no word index.
A suffix permits at most 256 probes and 64 matched spellings. The legacy
Corrections container and new native graph still allocate; zero-allocation
claims apply only to the pure prefix enumerator, not the integration.

The protected translator runs ordinary and corrected lookup on the same raw
input in the same session. It deduplicates the ordinary translation before
counting a page, preserves native candidates, scans at most 64 recovery entries,
and offers at most eight, at most six for the full input and two per shorter
consumed span. Three-letter input tries the cached initial-reading stage first
(two full-span slots), then specific spelling repairs (four full-span slots).
Other lengths retain specific-then-broad lookup. The stages share at most 64
scanned entries, remaining work counters and the original deadline; the second
stage cannot restart the budget. Both use the same raw input and session.
The additional retained graph
is a memory cost that remains to be measured. A correction-count
check follows the same first alignment as the native syllabifier, with a
1,024-visit cap, and accepts only one corrected edge. This is conservative and
can reject another valid alignment. Native correction candidates retain their
Phrase identity through a shadow wrapper. A native metadata flag, rather than
comment text, allows the uniquifier to keep identical Chinese text with distinct
consumed spans when one candidate is corrected.

The latest version uses a shared 32,768-unit work budget, separate 2,048-node
and 2,048-chunk caps, and a 900 µs cooperative deadline checked every 64 work
units. These are experimental limits, not a hard bound on the whole key path.
It chooses one correction position using the ordinary graph and canonical
syllables (a normal spelling can itself be a typo alias). It skips corrected
word prediction and additional sentence construction when a full-span dictionary
phrase already exists. The single-position heuristic can miss other errors.
Incomplete bounded dictionary lookups discard their partial collectors for all
input lengths. An incomplete first stage leaves ordinary translation; an
incomplete optional second stage cannot invalidate a completed first stage.
`shixin_spike_work_cap` (0–32,768, default 32,768) forces deterministic budget
exhaustion in scratch tests. It never increases the normal work budget.
A completed cache stage may remain when the optional specific stage exhausts;
discard that incomplete second stage. Exact
forward/backward prism reachability rejects correction spans that cannot join
an unchanged prefix and suffix. It does not currently support corrected prefixes
followed by unfinished non-exact spelling. The old NearSearchCorrector is kept
unchanged unless the explicit spike option selects the new component.

Sparse dictionary levels now intersect the smaller of the native child set
and spelling graph, without allocating another index. `prism_graph_test.cc`
compares complete dictionary-entry multisets with the original traversal on
four fixtures, using only read-only Prism/Table mappings and relaxed diagnostic
budgets. It creates no Rime session and never opens a user dictionary.

Native TableQuery bookkeeping uses three fixed arrays (maximum index depth
three), and queue removal moves the remaining code vector. Other native
allocations and the traversal queue remain. `SHIXIN_SPIKE_TIMING=1` enables
a 50-sample warm timing diagnostic in `prism_graph_test.cc`; budgets are relaxed
only in that read-only diagnostic, never in the engine configuration.

Recognized symbol/number/Unicode/calculator/email/URL and other special tags,
and configured date-trigger codes, bypass correction. The paired corpus exposed
symbol displacement at `vlz`; the focused engine test now protects that menu.
The corpus evaluator is `go run ./spikes/typo-recovery -mode native-evaluate`;
see the report for paths and the pre-fix captured-library identity.

Corrected dictionary lookup streams completed chunks during depth-first traversal.
Only a completed spelling graph is eligible; an explicit flag rejects incomplete
graphs. Lookup exhaustion can retain valid materialized entries, skips additional
sentence work, and never extends the deadline. Ordinary traversal remains breadth
first. Exhaustive multiset/subset tests cover this distinction. The graph, queue,
chunks and candidates still allocate; sampled process RSS is not an allocation
count or a proof of the proposed memory budget.

This prototype is AI-off only. It does not yet prove every native path bounded,
prove clean-page retention across every filter/setting, support cross-boundary
swaps, prove learning persistence, or meet the engine latency/allocation gates.
The two-per-span allocation is experimental and can reduce whole-input recovery.
Do not enable it in product defaults or treat passing smoke tests as readiness.

Tests:

```sh
clang++ -std=c++17 -O1 -g -fsanitize=address,undefined \
  -fno-omit-frame-pointer spikes/typo-recovery/native-graph/prefix_edits_test.cc \
  -o build/typo-recovery/bin/prefix-edits-test
build/typo-recovery/bin/prefix-edits-test
SHIXIN_TYPO_SPIKE_ROOT="$PWD/build/typo-recovery" CGO_ENABLED=0 \
  go test -v ./tools/menu-capture -run '^TestTypoNativeSpikeEditing$' -count=1
# Run latency tests alone, sequentially for ordinary and protected variants.
SHIXIN_TYPO_SPIKE_ROOT="$PWD/build/typo-recovery" \
  SHIXIN_TYPO_SPIKE_VARIANT=ordinary CGO_ENABLED=0 \
  go test -v ./tools/menu-capture -run '^TestTypoNativeSpikeLatency$' -count=1
```

The latency test asserts the repository's engine p50/p99/p99.9 budgets. The
unbounded prototype fails. The latest bounded version passes nine diagnostic
editing fixtures, including `njh → 你好`, `xgr → 中国人` and symbol protection,
and the expanded timing trace with paging and Backspace. These finite diagnostics
do not replace full release validation. Normal tool tests skip the opt-in native
tests without the explicit runtime variable.


Latest checkpoint: the original nine engine editing cases pass, but the added
`zhr → 中国人` case fails. The expanded suite is intentionally not reported as
passing. All-position, prefix-order and width-32 beam alternatives failed to fix
it; the [last rejected snapshot](rejected-beam/README.md) is preserved separately.
The main patch is restored to depth-first lookup. The completed 830-row paired
capture and its clean later-page losses are in the report.

`SHIXIN_SPIKE_SANITIZER_TEST=1` is a compile-time functional-test override requiring
ASan. It changes only the initial correction clock allowance to one minute;
operation caps and carried deadlines are unchanged. Never define it in Release
or cite its editing pass as latency evidence. For the Go editing test binary,
set both `DYLD_INSERT_LIBRARIES` and `SHIXIN_DYLD_INSERT` to the compiler's ASan
runtime; the test restores the preload variable for its child scratch engine.


## Read-only subtree-bound experiment

`subtree_bounds.h` retains the read-only bounds-search alternative. The current
native patch instead uses `initial_cache.h` for three-initial/one-neighbor search. It uses immutable bounds prepared from the existing table
and a fixed reusable workspace. The test enumerates all three-letter codes and
compares exact top-eight entries/readings with an independent exhaustive oracle.
See the [report](../../../docs/spikes/typo-subtree-bounds.md) for scope, allocation,
memory, cold-path and integration limitations. Incomplete results must be treated
as fallback by the first native integration; they never toggle a persistent setting.

After preparing the scratch source, dependencies and Release library above:

```sh
SHIXIN_SPIKE_BOOST_ROOT=$(sed -n 's/^BOOST_ROOT:PATH=//p' build/typo-recovery/native-graph/out/CMakeCache.txt)
clang++ -std=c++17 -O2 -DBOOST_DLL_USE_STD_FS \
  -DGLOG_EXPORT= -DGLOG_NO_EXPORT= -DGLOG_DEPRECATED= \
  -Ibuild/typo-recovery/native-graph/out/src \
  -Ibuild/typo-recovery/native-graph/src/src \
  -Ibuild/typo-recovery/native-graph/src/include -I"$SHIXIN_SPIKE_BOOST_ROOT" \
  spikes/typo-recovery/native-graph/subtree_bounds_test.cc \
  build/typo-recovery/native-graph/src/lib/libgtest_main.a \
  build/typo-recovery/native-graph/src/lib/libgtest.a \
  -Lbuild/typo-recovery/native-graph/out/lib -lrime \
  -Wl,-rpath,"$PWD/build/typo-recovery/native-graph/out/lib" \
  -o build/typo-recovery/bin/subtree-bounds-test
SHIXIN_SPIKE_TABLE="$PWD/build/typo-recovery/prebuilt/rime_ice.table.bin" \
  build/typo-recovery/bin/subtree-bounds-test
```

For the search sanitizer run, replace `-O2` with
`-O1 -g -fsanitize=address,undefined -fno-sanitize=vptr -fno-omit-frame-pointer`,
use a distinct output binary, and run with
`ASAN_OPTIONS=detect_leaks=0:halt_on_error=1 UBSAN_OPTIONS=halt_on_error=1`.
This instruments the new header/test, not the linked Release librime library.
Timing from that sanitizer run is not a performance result.

## Offline initial cache

The same read-only test builds the top eight native readings per clean three-letter
initial code. Generate a new file before starting the scratch engine (the writer
refuses to overwrite an existing file):

```sh
SHIXIN_SPIKE_TABLE="$PWD/build/typo-recovery/prebuilt/rime_ice.table.bin" \
SHIXIN_SPIKE_CACHE_OUTPUT="$PWD/build/typo-recovery/prebuilt/rime_ice.table.bin.initial3" \
  build/typo-recovery/bin/subtree-bounds-test
```

The loader defaults to the table filename plus `.initial3`; the explicit schema
path above is preferable for reproducible captures. A missing/invalid file causes
ordinary per-update fallback for the cache lane. Set
`shixin_spike_bound_probe_cap: 0` only in a copied test generation to force that
fallback. No cache building or file I/O happens on a key. See the
[current report](../../../docs/spikes/typo-initial-cache.md) for memory, cold
failures, exactness scope and current evidence. Never rebuild a library or replace
its cache/schema while a capture using that artifact is live.

`cache_stages_test.cc` is a read-only stage diagnostic: compile it with the same
include/link arguments as `subtree_bounds_test.cc`, using a distinct binary name.
Set `SHIXIN_SPIKE_PRISM` to the matching `rime_ice.prism.bin` and
`SHIXIN_SPIKE_RAW` to a three-letter code. Run each code in a fresh process; it
reports graph and lookup time separately over six repetitions with the normal
Release deadline. Process-first-query timing is not OS-cold-cache timing, and
this diagnostic does not include ScriptTranslation or engine costs. Run it only
after deadline-sensitive corpus captures finish.

The stage diagnostic also accepts `SHIXIN_SPIKE_TABLE_ONLY=1` to exclude dictionary
collector sorting. `SpikeCacheStages.RejectsExpiryDuringNativeVisitor` injects a
slow test callback, checks expiry rejection and tests a subsequent reset. The
current patch checks the deadline around native cache lookup/publication and
collector sorting; it still cannot preempt an individual native table operation.
The current manifest and historical corpus manifest are distinct in the report.

`cache_access_test.cc` uses the same Release compile/link command, with a separate
binary. Set `SHIXIN_SPIKE_TABLE` to the matching native table and optionally
`SHIXIN_SPIKE_RAW` (default `xgr`). It separates reading lookup, membership, weight
and text access. `SHIXIN_SPIKE_CACHE_SIZE=1` instead verifies all cached readings
and measures a possible pooled text payload offline. The latter uses temporary
construction containers and must never run on the engine key path. Its projected
record layouts are not an implemented replacement cache or an RSS measurement.

## Standalone compact mirror

Compile `mirror_cache_test.cc` with the same include/link arguments as the
read-only bounds test, using `build/typo-recovery/bin/mirror-cache-test` as output.
Build/verify the artifact offline, then load it in a separate process:

```sh
SHIXIN_SPIKE_TABLE="$PWD/build/typo-recovery/prebuilt/rime_ice.table.bin" \
SHIXIN_SPIKE_MIRROR_OUTPUT="$PWD/build/typo-recovery/native-graph/compact-mirror-v1.bin" \
  build/typo-recovery/bin/mirror-cache-test
SHIXIN_SPIKE_MIRROR_ONLY=1 \
SHIXIN_SPIKE_INITIAL_CACHE="$PWD/build/typo-recovery/prebuilt/rime_ice.table.bin.initial3" \
SHIXIN_SPIKE_MIRROR_OUTPUT="$PWD/build/typo-recovery/native-graph/compact-mirror-v1.bin" \
  /usr/bin/time -l build/typo-recovery/bin/mirror-cache-test --gtest_filter=MirrorCache.FreshImageOnly
```

Use a new output filename when changing format/data. Existing image files are
loaded and compared, not overwritten; the accompanying test identity/initial
fixtures are refreshed. The fresh-load test opens no native table. For ASan/UBSan,
use the sanitizer compiler flags above and a separate binary. This instruments the
mirror header/test; the linked Release library prepares the native reference only.
The standalone test does not activate the mirror in an engine. The native patch
now has a separate opt-in mirror path described below. Its handoff fixtures are
not production authentication. See the
[report](../../../docs/spikes/typo-compact-mirror.md) for measured scope and next gates.

## Verified snapshot and native mirror

`spikes/typo-recovery/prepare-mirror` is an offline Go helper, not a product engine
hook. A reviewed version-1 JSON manifest identifies builder provenance and four
artifacts by absolute source path, exact byte count and SHA-256: `table`, `prism`,
`initial`, `mirror`. See the [experiment manifest](../../../docs/spikes/typo-native-mirror/input-manifest.json)
for its format. Native reading/text verification must precede manifest creation;
the helper checks copied bytes against that trusted manifest, not the truth of
arbitrary builder claims.

```sh
CGO_ENABLED=0 go test ./spikes/typo-recovery/prepare-mirror
CGO_ENABLED=0 go run ./spikes/typo-recovery/prepare-mirror \
  -manifest /absolute/path/to/reviewed-manifest.json \
  -output /absolute/path/to/new-private-snapshot
```

Only after successful preparation, use a copy of the validated warm generation.
Its staging `build/rime_ice.table.bin` and `build/rime_ice.prism.bin` must resolve
to the private copies (the experiment uses absolute symlinks). Keep the original
prebuilt directory as fallback for English/radical resources. In that copied warm
schema, set `translator/shixin_spike_initial_cache_file` to the snapshot's
`rime_ice.table.bin.initial3` and `translator/shixin_spike_mirror_cache_file` to
`rime_ice.table.bin.mirror3`. Do not modify an active generation or cache.

The opt-in native tests accept `SHIXIN_TYPO_SPIKE_GENERATION` to select this copied
generation, preserving the default offset-cache test setup. Optional
`SHIXIN_TYPO_SPIKE_PREBUILT` selects a different fallback directory; do not point
it to a snapshot lacking the unrelated dictionaries. `mirror_native_test.cc` and
`cache_stages_test.cc` can be compiled together using the native include/link
arguments above. Set `SHIXIN_SPIKE_PRISM` to the snapshot's prism and
`SHIXIN_SPIKE_MIRROR` to its mirror image. Use a separate ASan executable linked
to the fully instrumented native library for functional checks.

The Table loads one immutable mirror; repeated same-path preparation retains
accessor/text pointers. Other paths are rejected, and a translator whose mirror
cannot load offers ordinary translation. No production settings, generation
activation protocol, model or client IPC is changed by this spike. See
[native integration evidence](../../../docs/spikes/typo-native-mirror.md).

## Separate-list analysis

After a paired capture is complete, run `go run ./spikes/typo-recovery -mode
native-lanes -root <paired-capture> -source-run <selected-labels>`. This writes
`separate-lanes.json`, preserving ordinary positions counterfactually and counting
one/two/four/eight separately exposed correction slots. It does not implement a
UI, alter native selection or measure a fixed-size ranking gain. Tests:
`go test ./spikes/typo-recovery -run '^TestSeparateLane'`. See the
[design and analysis](../../../docs/spikes/typo-separate-corrections.md).

The recovery extractor now lives in `recovery_translation.inc`, independently
of the legacy mixed-menu adapter. It accepts recovery translations and a bounded
exclusion snapshot, never an ordinary translation. `recovery_translation_test.cc`
tests native identity, ordinary-cursor independence, phase quotas and scan caps.
This refactor does not yet publish a separate engine/client correction stream.
See [the extraction report](../../../docs/spikes/typo-recovery-stream.md).

`translator/shixin_spike_separate: true` (alongside the protected option) enables
the experimental separate native stream. It is stored on the current Segment
and is not exposed by the existing client protocol. Rebuild all native binaries
after this patch's Translation/Segment ABI changes.
`separate_stream_test.cc` compares ordinary menus against a correction-disabled
control and explicitly commits a real recovered phrase using scratch warm schemas.
See [setup, results and remaining work](../../../docs/spikes/typo-engine-separate-stream.md).

Typed native edit traces and a real registry-selection fixture are described in
[the provenance report](../../../docs/spikes/typo-edit-provenance.md). This also
fixes apostrophe-aware anchor selection and bounded native unwrapping through
correction/conversion layers; rebuild all native binaries after applying it.

[Separate conversion/deduplication](../../../docs/spikes/typo-correction-filters.md)
now uses private candidate history and opt-in filters. OpenCC is initialized
when the separate scratch schema is constructed. Its internal conversion
expansion and the native allocation gate remain unresolved.

[Context-owned publication](../../../docs/spikes/typo-context-publication.md)
now invalidates and publishes through the native Context. Configure with an
engine-owned non-reused generation identity before keys; the production Go/C
bridge is still missing. The fixture's constant generation is not suitable
for a cross-worker protocol.

[The versioned C/purego bridge](../../../docs/spikes/typo-native-bridge.md)
copies bounded pointer-free snapshots and validates selection tokens. Its
production worker and client integration remain absent; use the explicit
`shixin_typo_spike` test tag only with the prepared private runtime.

`selective-abbreviation.patch` is a rejected, optional experiment, not part of
the retained patch. It reduces alternate-boundary abbreviation search and loses
six previously recovered development-corpus targets; see
`docs/spikes/typo-memory-attribution.md`. Do not apply it for the default spike.

`search_diagnostics_test.cc` tests sticky first-stop reasons, budget carry and
recording reset. `separate_stream_test.cc` enables the fixed thread-local
diagnostic snapshot for public fixtures; recording is otherwise disabled. See
`docs/spikes/typo-search-diagnostics.md`. Rebuild all C++ test binaries when the
scratch `SyllableGraph` layout changes.
