# Canonical correction cache: retention and compressed storage

Date: 2026-11-04. Isolated native experiment; truncated replacement rejected.
Parent: [typo design](../typo-tolerant-pinyin.md).

**Decision:** do not replace general correction lookup with the one-to-three-
syllable auxiliary table. The complete 731-code capture loses eighteen longer
typo targets relative to the previous mirror capture and gains only two.

The three-initial mirror fixes focused first-access cases, but longer mixed
inputs still miss their deadline. This experiment tests whether a shared cache
indexed by canonical readings can cover those inputs without duplicating large
decoded strings. It does not demonstrate typo-search recall or ranking gains.

## Retention before choosing a layout

Deduplicating the reused tuning families gives 64 reading/text pairs. Only 43
have two or three syllables; 34 of these exist under their labeled native reading.
The other 21 lengths and nine missing dictionary readings are not successes.

| Layout | Retained native-present labels | Projected flat payload |
|---|---:|---:|
| Two-syllable top four + existing three-initial cache | 31/34 | 8,439,285 bytes |
| Two/three-syllable canonical top one | 24/34 | 23,317,730 bytes |
| Two/three-syllable canonical top two | 31/34 | 25,214,556 bytes |
| Two/three-syllable canonical top four | 34/34 | 26,113,287 bytes |

The smaller hybrid loses 番荔枝, 鼓点子 and 建始县 despite their presence in
the native dictionary. It is therefore not a complete substitute. These are
reused synthetic tuning labels, not independent evaluation data.

The projections charge 20 bytes per entry, 16 per reading group, 16 per prefix
node and a shared UTF-8 text pool. They are neither implemented layouts nor RSS.

## Reuse librime's compressed table

The offline builder retains the top eight entries per canonical reading of one
to three syllables, by native weight and original offset for ties. All 440
syllable IDs are preserved. A normal librime Table provides the index and
compressed string storage; no new radix format or parser is needed.

The actual output is **15,072,644 bytes (14.37 MiB)**, including a 1,868,560-byte
compressed string table. It contains 601,322 entries in 487,252 reading groups.
Every retained entry's code, text, weight and order was checked against the
source selection after reopening the file. Four-and-longer phrase entries are
absent; this remains a material coverage limitation, not a full dictionary.

One separate process loaded the table, touched each mapped page, then queried
你好, 吊灯 and 纯净物. Load/warm took 48.14 ms; the three exact queries took
23.375, 1.916 and 4.458 microseconds. Maximum process RSS was 18,350,080 bytes
(17.50 MiB). This is a standalone process, not incremental engine RSS, a
spelling-search benchmark or a cold-storage guarantee. The offline builder's
303,529,984-byte peak RSS is temporary preparation memory, not typing memory.

## Integration decision and remaining checks

The scratch native patch now prepares an auxiliary table during translator
initialization, verifies all syllable IDs and touches its mapped pages. Bounded
correction graphs other than the three-initial lane use this table, with the
auxiliary owner propagated into dictionary chunks for text decoding. The
three-initial lane retains the existing original-table mirror. This experiment
therefore retains both caches; the file size alone understates its memory cost.

The first engine run passes all ten editing fixtures, including partial
selection, Backspace and literal Return. A native test verifies repeated
initialization preserves the owner and entry pointers, rejects a changed path,
and keeps missing-artifact initialization failed. A warm 2,100-key engine trace
measured p50 52 µs, p99 896 µs, p99.9 1.024 ms and maximum 1.027 ms. These
are focused results, not a broad latency or coverage guarantee.

A two-code capture recovers 纯净物 for `cnjw` at rank seven as a correction.
For `ddegn`, 吊灯 appears at rank thirteen as an **ordinary** candidate; the
recovery entries differ from the prior mirror run. This is not evidence that
the correction path fixed both misses.

### Full capture rejects the truncated replacement

All 731 inputs completed. On the same 553 reused typo labels, target availability
within thirteen candidates fell from 99 in the previous mirror capture to 83:
eighteen losses and two gains. Every lost target has four or five syllables,
including 平行公设, 保加利亚 and 克罗地亚语. Sentence composition from shorter
entries did not preserve these longer dictionary phrases. Clean-label availability
was 174/277 versus 173/277 in the previous mirror capture; this does not establish
ordinary-menu preservation.

These are sequential captures compared with the previous mirror run, not a new
paired ordinary baseline or independent holdout. The losses are sufficient to
reject replacing general lookup with this truncated dictionary. The next
experiment warms the original complete table at schema initialization, retaining
all phrase lengths and avoiding an additional word table. Residency and deadline
behavior still need measurement.

This remains an isolated auxiliary-table experiment, not a product default.
Initialization must verify and warm an immutable generation before key handling.
The four-artifact version-one mirror manifest does not cover this new table.
The offline preparer now accepts an explicit version-two canonical manifest
with five required artifacts and separate canonical-builder provenance. Tests
reject missing artifacts, changed bytes and invalid version/role mixtures,
and verify source changes do not affect copied bytes. Existing engine captures
used the locally built auxiliary table directly; the new manifest path has not
been exercised by those captures and is not product activation infrastructure.

Canonical syllable IDs can remain stable, but rebuilt **string IDs and entry
offsets are different**. Each returned dictionary chunk must retain the owning
auxiliary Table; interpreting its entry through the original table is invalid.
Ordinary lookup continues to use the original dictionary. Native candidate
selection, user learning and partial raw spans still require integration tests.

The three-initial cache's search cannot simply be pointed at this file: its
offsets belong to the original table. Rebuild its index against the auxiliary
table or verify bounded native traversal independently. Preserve the composition
work/deadline limits; exhaustion falls back for that request, not a permanent
feature disable. Warming reduces first-access risk but cannot guarantee residency.

Before adopting this layout, measure the actual engine RSS delta, repeat fresh
workers for longer mixed inputs, run the full paired corpus, and verify sanitizer,
learning and clean-input behavior. Separate correction suggestions remain an
independent unresolved presentation change.

## Full-table warming follow-up

The scratch `shixin_spike_warm_original` option touches the complete original
mapping at initialization, retaining the existing mirror and all dictionary
phrases without loading the auxiliary table. All ten editing fixtures passed
three runs. A 2,100-key trace measured p50 56 µs, p99 704 µs, p99.9 1.020 ms
and maximum 1.020 ms. The combined performance invocation initially omitted the
required RSS variant and failed that diagnostic setup; the separate corrected
RSS runs below completed. Neither is an allocation or sanitizer result.

Sequential fresh-worker runs with the same library and the same 1,350-composition,
single-context trace sampled these maximum RSS values:

| Configuration | Sampled maximum RSS |
|---|---:|
| Ordinary | 53,198,848 bytes (50.73 MiB) |
| Existing mirror | 58,195,968 bytes (55.50 MiB) |
| Truncated auxiliary plus mirror | 71,680,000 bytes (68.36 MiB) |
| Original full-table warming plus mirror | 105,758,720 bytes (100.86 MiB) |

Full-table warming adds about 45.36 MiB over the mirror run. Do not recommend it
as the default based on the focused latency pass: it eagerly makes a large
dictionary mapping resident. These are sampled process RSS values, not private
heap sizes, OS-wide physical memory costs or multi-context upper bounds. Pages
can subsequently be evicted. The next experiment should distinguish index/string
pages from long-phrase tail pages and test selective initialization warming,
preserving the complete dictionary and existing exhaustion fallback. Broad recall
and memory must both pass; a smaller truncated dictionary has already failed.

## Reproduction and evidence

Sources: `spikes/typo-recovery/canonical_cache_plan.go`,
`native-graph/canonical_cache_size_test.cc` and `native-graph/compact_table_test.cc`
under that directory. Run `canonical-cache-plan` against the preserved
`constrained-v1` source, then set `SHIXIN_SPIKE_TABLE` and
`SHIXIN_SPIKE_CANONICAL_LABELS` for the sizing test. The compact test takes
`SHIXIN_SPIKE_TABLE` and a new `SHIXIN_SPIKE_COMPACT_TABLE`; existing output is
verified rather than overwritten. Run `CompactTable.FreshLoadAndWarm` separately
with `SHIXIN_SPIKE_COMPACT_ONLY=1` and only the compact output path.

Saved logs, label provenance and hashes are in [typo-canonical-cache/](typo-canonical-cache/).
