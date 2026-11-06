# Offline typo-recovery spike

This is experiment code, not an input-method feature. It does not modify the
native pipeline, settings compiler, client IPC or live user dictionaries.

The Go driver samples the prepared CC-CEDICT dev/test sets and builds an index
**only from explicitly annotated rime-ice entries**. Labels never enter the
index. The C++17 prototype searches whole-word codes for one consecutive-letter
swap or QWERTY-neighbor substitution. It supports crossing a syllable boundary
because its index has no segmentation; this does not solve native partial-span
selection. Missing/repeated letters are negative capability controls.

Search order is fixed before evaluation: swaps cost 8, substitutions 10;
ascending cost, descending maximum dictionary frequency for the code, then
lexical code order. This is an uncalibrated channel ordering, not learned error
probabilities. Each query has a separate exact walk of ≤32 transitions, at most
2,048 recursive search states, and eight corrected-code outputs. State exhaustion
atomically drops corrections; the exact result survives. Short prefixes are
included for stress/timing, even though the proposed product would suppress
correction on very short input.

The prototype's dense trie is intentionally simple and large; report its memory
instead of presenting it as a shippable structure. Initialization allocates;
lookup uses fixed stack/results and reports intercepted C++ `new` allocations.
Timing excludes trie loading, all Rime work, IPC, output serialization and merge.
No claim about integrated engine latency follows from those numbers.

## Run

Prepare the corpus with [tools/typo-data](../../tools/typo-data/README.md).
Create a **private** runtime snapshot at `build/typo-recovery/` containing:

- `rime-data/`: the public rime-ice source used to build the generation;
- `lib/` and `share/opencc/`: matching built native library and OpenCC resources;
- `prebuilt/`: optional compiled dictionary resources, never a user database;
- `generation/`: builder-produced warm-only generation;
- `bin/`: this checkout's engine, builder and capture tool.

For the first experiment, settings were full pinyin, five candidates/page,
AI off, emoji off, traditional off, mixed English off and no fuzzy pairs.
Everything else retains compiled defaults. The capture worker disables learning
and creates its own scratch user directory; it never promotes its generation.

```sh
mkdir -p build/typo-recovery/bin
CGO_ENABLED=0 go build -o build/typo-recovery/bin/ \
  ./cmd/shixin-engine ./cmd/shixin-builder ./tools/menu-capture
# After placing public source/native snapshots as listed above:
build/typo-recovery/bin/shixin-builder \
  -lib "$PWD/build/typo-recovery/lib/librime.1.dylib" \
  -data "$PWD/build/typo-recovery/rime-data" \
  -prebuilt "$PWD/build/typo-recovery/prebuilt" \
  -settings "$PWD/build/typo-recovery/settings.json" \
  -output "$PWD/build/typo-recovery/generation"
clang++ -std=c++17 -O3 -Wall -Wextra -Werror \
  spikes/typo-recovery/native/search.cc -o build/typo-recovery/bin/search
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode prepare \
  -root build/typo-recovery/run-v1 -families 100
build/typo-recovery/bin/search \
  build/typo-recovery/run-v1/lexicon.tsv \
  build/typo-recovery/run-v1/queries.txt \
  > build/typo-recovery/run-v1/search.jsonl \
  2> build/typo-recovery/run-v1/search-stats.txt
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode plan \
  -root build/typo-recovery/run-v1
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode profile \
  -root build/typo-recovery/run-v1
build/typo-recovery/bin/menu-capture \
  -engine "$PWD/build/typo-recovery/bin/shixin-engine" \
  -generation "$PWD/build/typo-recovery/generation" \
  -data "$PWD/build/typo-recovery/rime-data" \
  -prebuilt "$PWD/build/typo-recovery/prebuilt" \
  -lib "$PWD/build/typo-recovery/lib/librime.1.dylib" \
  -profile "$(cat build/typo-recovery/run-v1/profile.id)" \
  -input build/typo-recovery/run-v1/capture-codes.jsonl \
  -output build/typo-recovery/run-v1/menus.jsonl -limit 20 -timeout 30m
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode report \
  -root build/typo-recovery/run-v1
```

`prepare` requires a new directory. Other modes create named experiment outputs;
use a new run directory when changing inputs/configuration. `plan` creates only
unique answer-free codes. `profile` hashes runtime/data/generation files, including
prebuilt assets and OpenCC. Record compiler/OS/CPU separately. Dynamic library
system dependencies are not recursively fingerprinted.

For faster capture, independent processes may each use a subset of codes: each
capture owns a different scratch dictionary. To use the provided four-way split,
rename a stopped, valid partial `menus.jsonl` to `menus-partial.jsonl` (or create
an empty latter file for a fresh run), run `-mode shard`, and invoke menu-capture
for each `shards/codes-N.jsonl` → `shards/menus-N.jsonl`, N=0..3. Wait for **all**
four commands to succeed before `-mode collect`. Collection preserves full capture
records, verifies profile/completeness, and rejects missing or duplicate codes.
An interrupted capture can retain complete records, but its unfinished input
set must never be reported as a complete experiment. Run latency benchmarks alone,
after all capture processes exit.

## Interpretation

For each synthetic error type and split, report:

- Corrected spelling in the top-eight code set, and whether the clean spelling
  exists in the explicit-code index (unannotated Tencent entries are omitted).
- Target acceptance at ranks 1/5/20 in baseline menus, versus the offline merge.
- The clean-code menu's top-20 target recall as a control; absence here is not
  proof that the word is absent from the entire dictionary.
- An uncapped union diagnostic: baseline candidates plus the verified full-span
  prefix of each recovered-code menu. This is an oracle recall ceiling over up
  to eight menus, **not** a bounded export pool or a measured product menu.
- Clean #1/page-1 changes, targets displaced from top-5/top-20, state exhaustion,
  output truncation, lookup timing and memory.

The conservative offline merge keeps baseline #1, inserts up to four new full-span
Chinese candidates after it, then fills from baseline to 20 entries. It walks
recovered codes in search order, preserves each Rime order, deduplicates full-span
text, and stops at the first unverified candidate within each recovered menu.
This can crowd out baseline entries and starve later correction hypotheses;
those losses belong in the report. Baseline recall uses the original raw ranks,
including intervening partial candidates; those are not counted as target hits.

Offline replay of corrected strings is an experiment shortcut, explicitly
forbidden as the product integration strategy. Native raw-span alignment,
learning, protected trigger/custom-phrase precedence and responsiveness still
need the graph integration described in the design. The sample has no context
or private learning history. Do not treat same-pinyin target top-1 as unambiguous
accuracy, or the constructed exact-first policy as proof of model safety.

## Checks

```sh
CGO_ENABLED=0 go test ./spikes/typo-recovery ./tools/typo-data ./tools/menu-capture
CGO_ENABLED=0 go vet ./spikes/typo-recovery ./tools/typo-data
clang++ -std=c++17 -O1 -g -fsanitize=address,undefined \
  -fno-omit-frame-pointer spikes/typo-recovery/native/search_test.cc \
  -o build/typo-recovery/bin/search-test
build/typo-recovery/bin/search-test
```

The C++ tests compare 10,000 deterministic randomized queries to an independent
brute-force edit oracle, and cover cap exhaustion, ranking truncation, malformed
input and the main example edits. Go tests ensure labels do not enter the index,
check whole-input verification and merge behavior, and reject missing captures.

## Retest with the shipped ERNIE scorer

The [rerank report](../../docs/spikes/typo-rerank.md) records the results and
native-export limitations. This uses the actual `internal/ai` scorer offline;
it does not modify the engine or train a model. Provide the matching llama.cpp
libraries and the pinned q8_0 model in `build/typo-recovery/rerank-runtime/`.
The model hash is checked against `internal/models.ERNIE`.

```sh
# Reads the downloaded public Duyu CSV; creates a new contexts.jsonl.
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode contexts \
  -root build/typo-recovery/run-v1
# Requires a new rerank-v1/ directory under the existing recovery run.
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode rerank \
  -root build/typo-recovery/run-v1 \
  -llama build/typo-recovery/rerank-runtime/lib \
  -model build/typo-recovery/rerank-runtime/model.gguf
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode rerank-summary \
  -root build/typo-recovery/run-v1
```

`contexts.jsonl` preserves source line and sentence hashes, sends only preceding
text, and uses deterministic selection before any scoring. `rerank-v1/rows.jsonl`
contains all per-example ranks and times; the manifest is the completion marker.
Raw source-derived context rows remain local under ignored `build/`, rather than
being redistributed as a provenance-approved training corpus.

The test compares original/augmented menus with AI off/on and a fixed error
penalty. It uses approximate source-frequency priors because the captures lack
native phrase weights; partial candidates are fixed. Correct, empty and stale
context conditions remain separate. Handwritten diagnostic examples never enter
the aggregate benchmark. No live deadline is enforced; measure score/prefill
separately and retain fallback behavior when reasoning about deployment.

## Abbreviation baseline

`abbr-prepare` samples existing dev/test families by a new fixed family hash,
never by model performance or availability of context. It reads the original
syllable annotations, emits clean full/initial/digraph/mixed forms, then one
hash-selected swap and one neighbor substitution on each typed form. Duplicate
forms are omitted; cross-split raw-code collisions are excluded from both splits.
Original full-spelling deletions are not relabeled as abbreviations.

```sh
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode abbr-prepare \
  -root build/typo-recovery/abbr-v1 -families 25 \
  -source-run build/typo-recovery/run-v1
```

Use the capture command above with `abbr-v1` paths and **`-limit 50`**. The
same `shard`/`collect` modes support four private capture processes. Wait until
all captures complete before scoring; do not benchmark while they run. The
parent run's profile/runtime must be unchanged, and its source-derived contexts
are reused without model-dependent selection.

```sh
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode abbr-evaluate \
  -root build/typo-recovery/abbr-v1 \
  -llama build/typo-recovery/rerank-runtime/lib \
  -model build/typo-recovery/rerank-runtime/model.gguf
```

Evaluation requires a new `evaluation/` directory and writes its completion
manifest last. It compares ordinary retrieval against the existing ERNIE scorer,
without new typo expansion or penalties. `ai` uses up to 50 captured candidates;
`window_ai` uses the contiguous whole-input prefix, capped at 50 for raw codes
without `aeiouv`, otherwise 20. The tail retains its original order. This
approximates the native window but lacks native candidate types/weights and
segment identity. A partial first candidate produces an empty movable window.
Whole-input commit probes establish acceptance, not the exact spelling-graph
path (abbreviation, completion or other derivation).

The summary keeps form/error/split/context conditions separate. `recall50` is
retrieval coverage, while `window_recall` measures availability in the smaller
movable pool. Zero means missing from this capture, not absent from the entire
Rime dictionary. No live hold is enforced. Use per-row score times to analyze
late-order fallback, and keep context prefill timing separate. Raw contexts and
per-example source-derived data stay under ignored `build/`.

## Bounded abbreviation recovery prototype

The native `abbreviation_search` stores compact syllable-trie edges, not
expanded abbreviation combinations. Its transitions accept full syllables,
one-letter initials and `zh/ch/sh` initials. One keyboard-neighbor substitution
or adjacent swap can apply across the whole typed form; a pending-character
state handles swaps across typed syllable boundaries. This experiment does not
provide the raw-span alignment required for production partial selection.

A best-first queue uses a dictionary-prior upper bound, with remaining-length
bounds and first-letter edge ranges to avoid impossible paths. Limits are
2,048 popped states, 8,192 examined edges, 1,024 queued states and 4,096 dedup
slots. Scratch space is 45,064 bytes; any overflow discards all extra proposals.
There are four zero-edit and four typo candidate slots. Within each category,
order by `0.3*log1p(dictionary frequency) - 0.2*edit_cost`, then text/code. Costs
remain 8 for a swap and 10 for a neighbor. Only four terminal words per canonical
reading can enter a four-slot category, so the frozen index retains its top four.
No model or label affects search. These are experimental bounds, not shipping
parameters; see the [results](../../docs/spikes/typo-abbreviation-recovery.md).

```sh
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode abbr-recovery-prepare \
  -root build/typo-recovery/abbr-recovery-v1 \
  -source-run build/typo-recovery/abbr-v1
clang++ -std=c++17 -O3 -Wall -Wextra -Werror \
  spikes/typo-recovery/native/abbreviation_search.cc \
  -o build/typo-recovery/bin/abbreviation-search
build/typo-recovery/bin/abbreviation-search \
  build/typo-recovery/abbr-recovery-v1/syllable-lexicon.tsv \
  build/typo-recovery/abbr-recovery-v1/queries.txt \
  > build/typo-recovery/abbr-recovery-v1/search.jsonl \
  2> build/typo-recovery/abbr-recovery-v1/search-stats.txt
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode abbr-recovery-plan \
  -root build/typo-recovery/abbr-recovery-v1 \
  -source-run build/typo-recovery/run-v1
```

The plan reuses matching-profile captures and creates four capture shards under
`abbr-recovery-v1/verification/`. Run the capture command from above for each
shard with `-limit 20`, fresh outputs and separate scratch engines. All four must
succeed before collection and model timing:

```sh
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode collect \
  -root build/typo-recovery/abbr-recovery-v1/verification
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode abbr-recovery-evaluate \
  -root build/typo-recovery/abbr-recovery-v1
clang++ -std=c++17 -O1 -g -fsanitize=address,undefined \
  -fno-omit-frame-pointer spikes/typo-recovery/native/abbreviation_search_test.cc \
  -o build/typo-recovery/bin/abbreviation-search-test
build/typo-recovery/bin/abbreviation-search-test
```

A proposal must pass whole-input text acceptance in its canonical-code capture.
That proves lexical acceptance, not selection of the original raw abbreviated
code through a corrected native graph. The offline merge keeps the five ordinary
entries, appends up to eight deduplicated proposals, then fills to 50 from the
ordinary menu. This diagnostic uses the same 50-candidate ceiling for both AI
arms, not the native full/mixed limit of 20. A separate guard uses the ordinary
AI first page as a counterfactual control; computing two orders offline is not
a proposal to add an inference pass or another hold to the product.

### Optional radix storage

Append `radix` to the native search command to compress nonterminal single-child
chains after building the index. Internal syllable boundaries remain traversable;
terminals, original logical IDs, search caps and result ordering are preserved.
Ranked bit vectors replace per-node/per-label mapping arrays. This is a storage
experiment; it does not prune extra hypotheses or change the queue. See the
[radix report](../../docs/spikes/typo-radix.md) for retained-buffer savings,
startup-memory limitations, equivalence checks and sanitizer commands.

### Packed records and prebuilt image

Additional native modes are `strings` (pool/right-size live payloads), `packed`
(specialized leaf/internal records and compact edge arrays), and `packed-strings`
(both). They preserve logical IDs, floats, candidate ordering and operation caps.
`save-packed` takes a source TSV and an image output path; `load-packed` takes
that image and the query list, avoiding in-process construction. These modes do
not combine with radix compression. The prototype loader validates bounded
sizes, graph structure and bounds before querying, but is not a production
artifact format. See the [storage report](../../docs/spikes/typo-packed-storage.md)
for commands, sanitizer/corruption tests, file identity and separated build/runtime
memory measurements.

### Constrained search and single-pass first-page retention

`load-constrained` takes the same packed image and queries as `load-packed`.
It rejects dominated/impossible-next-initial states before enqueueing, charges
lookahead to the existing edge budget, and retains all operation caps. See the
[constrained-search report](../../docs/spikes/typo-constrained-search.md) for
measurements and the independent sanitizer oracle. The recovery evaluator adds
`single_pass_ai_rank`: score only the original menu, retain its first five,
then append verified proposals in channel order. This arm does not use a second
model call; the older two-order guard remains a separate diagnostic.

### Failure audit

`-mode abbr-audit -root <completed-run> -source-run <lexicon-run>` creates
`<completed-run>/audit/` without overwriting it. It joins labeled readings to
the explicit lexicon and separates absence, exhaustion and completed-search
quota losses. See the [failure audit](../../docs/spikes/typo-failure-audit.md)
for interpretation, tie limitations and the next controlled ablations.

### Recovery quota and combined-alignment experiments

`load-constrained [0..8]` optionally assigns that many of the eight result slots
to zero-edit paths; the default remains four. `load-dp [0..8]` is a separate,
currently rejected bitset-alignment prototype. Both reuse the same packed image,
including its four-word-per-reading truncation. `abbr-quota-report` compares
saved search outputs against ordinary native captures without invoking inference.
See the [quota report](../../docs/spikes/typo-quota-allocation.md) for results,
limitations and exact input naming.
