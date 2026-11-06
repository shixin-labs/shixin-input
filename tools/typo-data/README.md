# Typo data preparation

Offline data tools for the [proposed feature](../../docs/typo-tolerant-pinyin.md).
No correction engine or model is implemented here. Source research, limitations
and the first run are recorded in
[the preparation report](../../docs/spikes/typo-data-preparation.md).

## Prepare CC-CEDICT

Download the UTF-8 v1 gzip archive using the download link on
[the publisher's page](https://www.mdbg.net/chinese/dictionary?page=cc-cedict).
Keep the archive unchanged under `build/typo-data/raw/cc-cedict/cedict.txt.gz`.
The URL is mutable: record its hash and retain that snapshot. The recorded first
run requires SHA-256
`2ec9a065cee371c2ee6a75adda056deef979ee336a7ae45b3d25f2ef2e6d427f`.

```sh
mkdir -p build/typo-data
CGO_ENABLED=0 go run ./tools/typo-data \
  -input build/typo-data/raw/cc-cedict/cedict.txt.gz \
  -output build/typo-data/cedict-v1
```

Output must not already exist. `manifest.json` is the completion marker;
ordinary failures remove the tool-created partial directory. An interrupted
process can leave a directory without that marker; it is not an approved corpus.
The tool does not download anything or read an installed IME. Large source and
output files stay in ignored `build/`; the first manifest is checked in under
`manifests/` for reproducibility. This is a pilot, not a frozen final test set.

Defaults: seed `shixin-typos-2026-11-v1`, maximum 20,000 families. Family sampling
and 80/10/10 split assignment are deterministic hash operations. Actual row
proportions differ because family sizes differ. To reproduce hashes use the
same source bytes, seed, family cap and generator version.

## Data contract

- Parse only v1 dictionary entries: Han-only simplified/traditional text,
  2–10 Chinese characters, one numbered-tone syllable per character, 4–32
  normalized pinyin bytes. Lowercase, remove tone numbers, map `u:` to `v`.
  Unsupported entries are counted and rejected, not silently transliterated.
  The dictionary's readings are labels, not independently verified ground truth.
- Split connected components of identical Chinese text, traditional variants
  and identical untoned pinyin **before** making typos. This groups polyphonic
  readings and homophones; it does not discover all semantic near-duplicates.
  Within the single source, no phrase/reading family appears in multiple splits.
- Produce one clean row and up to one deterministic variant for each of five
  edit types: within-syllable transpose, boundary transpose, keyboard neighbor,
  missing letter, repeated letter. Boundary swaps are deliberately retained as
  a separate challenge slice despite being outside the initial feature scope.
- Keyboard neighbors use QWERTY key centers: row offsets 0, 0.25, 0.75 key widths,
  vertical spacing 1 and Euclidean distance ≤1.3. These are synthetic assumptions,
  not fitted human error probabilities. Only consecutive differing letters swap.
- Reject generated codes above the existing capture tool's 32-byte limit.
  Pre-reserve all selected clean codes to their split; reject noisy codes that
  would appear in another split. Resolve remaining noisy cross-split collisions
  in deterministic family order. The manifest counts this selection bias.
- Each row records its source line, Chinese target, reading, raw code, family,
  split, error operation and reversible **canonical-to-raw** byte edit. Clean
  rows use `origin=dictionary_clean`; corrupted rows use `origin=synthetic`.
  `context` is empty. No user key traces or context are invented.
- `lexicon_code_collision` means the corrupted code equals an eligible source
  entry's clean code. It is **not** a complete valid-pinyin recognizer, and says
  nothing about which interpretation a real user intended.

`train.jsonl`, `dev.jsonl`, `test.jsonl` contain labels.
`codes.jsonl` has only unique `{"code":"..."}` rows, suitable for
[menu-capture](../menu-capture/README.md); it carries no answers. Captures remain
future work and require an isolated warm generation. Do not feed label-bearing
rows directly to retrieval. Incremental traces can later type each byte of `raw`
in sequence, but the prepared files do not contain measured timing or edits.

Use target recall for the initial search study. Homophones without context make
unique top-1 labels ambiguous: another valid homophone is not automatically a
false correction. This dataset alone cannot validate contextual reranking,
actual keyboard-error frequency, clean-input safety or the proposed neural
model gates. In particular, the pilot test split has fewer than 10,000 clean
rows, no volunteered raw typos, and no committed context.

The derived dataset is **CC BY-SA 4.0**, attributed to the CC-CEDICT community
and MDBG. The manifest preserves the source header and license URL. Share those
notices and record these transformations with any redistributed derived data.
The tool source retains this repository's license; dataset licensing is separate.

## Audit the larger paired-text candidate

The Duyu CSV is headerless; its first line is a real sentence. Do not let a CSV
loader consume that line as column names (the Hugging Face preview does this).
Download a fixed revision, retain its README and keep it separate from CC-CEDICT:

```sh
curl -fL --retry 2 -o build/typo-data/raw/duyu/pinyin2hanzi.csv \
  'https://huggingface.co/datasets/Duyu/Pinyin-Hanzi/resolve/9961d7d05910be7006f2dcfae21f4fc5ce4d560a/pinyin2hanzi.csv?download=true'
CGO_ENABLED=0 go run ./tools/typo-data \
  -audit-pairs build/typo-data/raw/duyu/pinyin2hanzi.csv
```

Create the destination parent first. `-audit-pairs` only parses/counts/hashes;
it does not normalize, split or approve data for training. It reports duplicates
and Han-only alignment mismatches without printing source text. The publisher
labels the dataset Apache-2.0, but its short card does not document underlying
text sources or annotation methods. Keep that provenance question open.

Validation:

```sh
CGO_ENABLED=0 go test ./tools/typo-data
CGO_ENABLED=0 go vet ./tools/typo-data
CGO_ENABLED=0 go run ./tools/typo-data -verify build/typo-data/cedict-v1
```

Tests cover tone/ü normalization and rejected records, reversible edits,
keyboard geometry, boundary classification, polyphonic/traditional/homophone
split grouping, disjoint raw inputs, deterministic output, refusal to overwrite,
answer-free capture files, and headerless CSV auditing.
