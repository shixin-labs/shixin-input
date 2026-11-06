# Experimental typo-correction option

Status: implemented locally, off by default; not a published release.

In Settings → General → Typing, full-pinyin users can enable **容错拼音（实验性） /
Typo-tolerant pinyin (experimental)**. The persisted key is `typo_correction`.
It applies to the full-pinyin schema, including initials and mixed spellings;
shuangpin schemas are unchanged. Changing it rebuilds a private generation and
uses the existing safe engine handoff.

The user's final display choice is **labelled corrections in the normal menu,
after its first page**. Ordinary translators, AI, conversion and pins finish
before insertion. Page one keeps its ordinary order. Up to eight deduplicated,
converted suggestions follow, labelled `纠错 →`, followed by the ordinary tail.
They use existing paging, number keys and candidate clicks. No client, IPC or
candidate-window extension is needed. Menus with fewer than a full first page
suppress corrections; there are no invisible slots or corrections on page one.
Later ordinary candidates can move to later pages. This is intentional and
supersedes the previously proposed separate correction area.

The retrieval policy is the [tested native policy](spikes/typo-hypothesis-native.md):
rank corrected spellings first, inspect at most ten native entries per spelling,
and consider up to eight full-pinyin or sixteen abbreviated/mixed hypotheses.
The heuristic classification and fixed first-three-initial cache are unchanged.
The shared search caps are 32,768 work units, 2,048 nodes, 2,048 chunks and a
900 µs cooperative deadline. Incomplete spelling batches are discarded; completed
batches can still contribute. Exhaustion affects that update, not the preference.
Ten entries is not a bound on all internal dictionary chunk expansion.
Correction retrieval uses the system dictionary only: user-dictionary traversal
is unbounded and is excluded from recovery. Ordinary user-dictionary lookup
and explicit-selection learning remain enabled.

No new neural model is required. A frozen, dictionary-derived spelling-prior
TSV and weighted keyboard channel drive retrieval. Existing ERNIE reranks the
ordinary stream; this option does **not** jointly rerank corrections with it.
A neural correction/ranking model remains a separate evaluation decision.

The offline builder produces generation-local initial and native-entry/text
caches from the compiled system dictionary. A versioned SHA-256 manifest binds
them to that table and the spelling model; validation rejects missing or changed
resources. No user dictionary is read by this builder. Resource loading occurs
at schema initialization, never during a key. Ordinary operation needs no model
download, GPU, network or private typing collection. Missing native builder
support fails the build rather than activating a half-built generation.

## Validation and limits

Validation on Apple M4 with the product library and private warm sessions
([Release menu log](spikes/typo-product-menu/release-menu.log),
[ASan/UBSan menu log](spikes/typo-product-menu/asan-menu.log),
[engine benchmark](spikes/typo-product-menu/engine-bench.log),
[short Go suite](spikes/typo-product-menu/short-go.log)):

- Native paging tests: exact/full/short first pages, deduplication, candidate
  identity, and ordinary tail continuation.
- Native policy tests: per-spelling quotas and fairness, exhaustion, invalid
  models, 256 malformed-model inputs and 1,000 randomized spelling inputs.
- Real menus: `nihso` and `niaho` offer labelled `你好` on page two; both clicks
  and numeric selection commit the expected text. Converted emoji retain labels.
- Paired enabled/disabled menus: every typed prefix of twenty illustrative
  clean, typo, initial, mixed, apostrophe and special-input strings preserves
  first-page text/comments. This is focused regression coverage, not a universal
  clean-input accuracy claim.
- 2,700 sampled native key+context reads: p50 **0.165 ms**, p99 **0.969 ms**,
  p99.9 **1.417 ms**, max **3.599 ms**; zero warm Go allocations per tested trace.
  Ordinary Go engine benchmark: **22.58 µs/key, 0 B/op, 0 allocs/op**.
- Release and ASan/UBSan native suites and the real-menu test pass. All short
  Go tests, changed-package vet and the no-cgo build pass. The settings control
  saves/applies in both languages and both themes.
- Fresh generation and off→on→off generation reuse build successfully;
  resource tampering is rejected in manifest tests.

The user accepted the prior policy's **12.9 MiB incremental sampled RSS**.
That measurement predates this menu adapter; it is not a new whole-application
memory measurement. Existing librime graphs and native candidates allocate;
zero Go allocations does not imply allocation-free native recovery.

Earlier 313→337–341/830 availability results describe the independent eight-item
experiment. They are **not** a new normal-menu accuracy result. Recovery remains
incomplete, especially pure-initial swaps and some inferred-boundary cases.
Only supported native edit alignments are selectable; missing/extra letters,
shuangpin correction and a dedicated neural model are not promised here.
Broader real-user accuracy, full corpus replay with this display policy,
platform UI VM checks and product-path learning persistence remain follow-ups.
Prior scratch learning tests are evidence for the shared native identity path,
not replacements for those product checks. This is why the option is labelled
experimental and defaults off.

Reproduce the product menu test with a builder-generated enabled generation,
an optional disabled baseline, matching library and prepared public data root:

```sh
SHIXIN_LIBRIME=/path/to/librime.1.dylib \
SHIXIN_TYPO_GENERATION=/path/to/enabled-generation \
SHIXIN_TYPO_BASELINE=/path/to/disabled-generation \
SHIXIN_TYPO_DATA_ROOT=/path/to/public-data-root \
CGO_ENABLED=0 go test -v ./internal/rime -run '^TestTypoProductMenu$' -count=1
```

The data root contains `rime-data`, `prebuilt` and `share/opencc`. All sessions
use fresh private user directories. Native checks use the standard librime
Release and ASan/UBSan builds.
