# Repack the complete native table

Date: 2026-11-04. Offline layout experiment and scratch engine tests; not shipped.
Parent: [selective warming](typo-selective-warming.md).

The original table interleaves indexes and payloads across nearly all mapped
pages. This experiment groups indexes first, followed by the compressed string
table, short-entry lists and long-entry payloads. It uses the existing native
format and preserves all entries rather than retaining only shorter words.

## Preservation and layout

The reopened output preserves all 440 syllable IDs, 609,764 one-to-three-syllable
entries, 1,276,287 longer entries and 2,065,654 extra-code syllables. The verifier
compares every index key, entry order, text ID, weight and extra-code sequence,
and the complete compressed string table bytes against the source. Total entries:
**1,886,051**. Output size remains **60,656,260 bytes**. This is a layout change,
not a smaller dictionary or a second runtime word table.

Release and ASan/UBSan builds produce byte-identical files. The offline builder
sampled 200,982,528 bytes maximum RSS; its temporary arrays are not engine memory.
This tool assumes a trusted, valid native source artifact and is not an untrusted
binary importer.

On 16 KiB pages, indexes occupy 1,046 pages instead of 3,161. The metadata/index/
string union falls from 3,611 to 1,495 pages; adding short entries raises it to
1,793 pages, versus all 3,703 pages in the original layout. The index region ends
at byte 17,127,976, followed by 7,359,696 bytes of compressed strings. Page counts
measure address coverage, not incremental process RSS.

## Rebuild dependent artifacts

Repacking changes entry offsets. The initial cache and mirror were rebuilt and
their three-initial search compared against exhaustive results for all 17,576
codes. The engine uses a new four-artifact verified snapshot, with the unchanged
prism and new table/cache/mirror hashes. The existing prebuilt fallback directory
still supplies unrelated English and radical dictionaries.

The legacy cache identity tuple (dictionary checksum, counts and file size) is
unchanged by this repack. It cannot distinguish these two layouts. Do not reuse
old sidecars or treat that tuple as authentication; the reviewed manifest must
bind exact artifact bytes. Equal-weight offset tie breaks in correction cache
construction may also change, so content preservation does not prove identical
correction ranking. The ordinary entries retain their original order.

## Engine results and limit

Both repack configurations pass all ten editing fixtures. The un-warmed repack
measures p99 704 µs and maximum 1.096 ms on the 2,100-key trace; sampled maximum
RSS over 1,350 compositions is 59,031,552 bytes (56.30 MiB).

With `shixin_spike_warm_indexes` and `shixin_spike_warm_strings` enabled at schema
initialization, p99 is 704 µs, p99.9 1.024 ms and maximum 1.031 ms. Sampled maximum
RSS is 73,121,792 bytes (69.73 MiB). Neither setting performs warming on key
handling. These are sequential focused runs, not cold-storage latency guarantees
or a multi-context memory bound.

The three native lifetime/warming/decoding tests and all ten engine editing
fixtures also pass with the full native library under ASan/UBSan. The sanitizer
engine uses the existing relaxed functional deadline; its timing is not a gate.

**The known first-run miss remains:** in each configuration's first two-code
capture, `cnjw` lacks 纯净物. Two later fresh workers recover it at rank seven.
`ddegn` contains 吊灯 at rank seven as an ordinary candidate in all three runs;
that is not proof of typo recovery. An additional control with the original
complete table fully warmed finds 纯净物 at rank seven on its first capture.
That single control supports investigating remaining payload accesses but does
not establish causality or a universal benefit.

Do not promote this layout based on preserved contents or focused timings alone.
The payload follow-up below also leaves the first-use gap open. The full paired
corpus, ordinary-menu regression, learning,
multi-context memory and allocation gates remain open. Exhaustion continues to
discard incomplete stages; the deadline is unchanged.

## Payload follow-up: extra warming is not recommended

The offline stage diagnostic now records process minor/major page-fault deltas
around dictionary lookup. In three fresh processes with index/string warming,
first lookup times for `cnjw` were 859, 211 and 157 µs; the first exhausted its
budget and recorded five major faults. Three subsequent fully warmed processes
took 118, 112 and 118 µs with no major faults. Runs were sequential, not randomized;
the process-wide counters cannot identify a particular file or prove causality.
The timing also includes the diagnostic's pre-lookup `getrusage` call. These are
not engine latency measurements or physical-cache-flush tests.

The scratch option `shixin_spike_warm_short_entries` adds one-to-three-syllable
payloads to index warming. It preserves all longer entries and does no new
warming during typing. Combined with string warming on the repacked table, the
engine still lacks 纯净物 on the first `cnjw` capture and recovers it in the next
two fresh workers. The offline diagnostic similarly exhausts on its first run
(946 µs lookup, six major faults), then completes in 98 and 102 µs. Thus warming
the short payloads does not establish a fix; further reads or other first-use
costs remain.

All ten normal editing fixtures pass. The 2,100-key trace measures p50 56 µs,
p99 704 µs, p99.9 1.024 ms and maximum 1.036 ms. Sampled maximum RSS reaches
75,743,232 bytes (72.23 MiB) over 1,350 compositions. This additional eager
residency is not recommended without a demonstrated recovery benefit.

The updated warming/ownership native tests and all ten engine editing fixtures
also pass under full-library ASan/UBSan, using the relaxed functional deadline.

Stop extending the warming set solely to make these two inputs pass. The
per-request exhaustion fallback is a required behavior, not a promise to recover
every cold query. Keep storage experiments separate from the unresolved normal
menu displacement problem; the next presentation work must preserve ordinary
candidate positions while keeping corrections explicitly selectable.

Source: `spikes/typo-recovery/native-graph/repack_table_test.cc`,
`table_pages_test.cc` and `librime.patch`. Evidence is saved in
[typo-repacked-table/](typo-repacked-table/). The repacker accepts
`SHIXIN_SPIKE_TABLE` and a new `SHIXIN_SPIKE_REPACK_TABLE`; if output exists,
it verifies preservation rather than overwriting it.

## Re-evaluation with separate corrections and alternate boundaries

A later run tests this exact complete-table repack with the retained broader
alternate-boundary policy and separate correction stream. The four repacked
artifacts were rechecked against their SHA-256 manifest before constructing a
new isolated `repack-separate-generation-v2`; no additional warming is enabled.
The original separate generation remains the default. The Go bridge, corpus and
learning tests, plus the native separate-stream fixture, now accept
`SHIXIN_TYPO_SPIKE_GENERATION` to compare explicitly prepared scratch generations.

Two paired fresh-process corpus trials use the same 731 codes / 830 development
labels, in original/repack then repack/original order. Each code gets two traces.
Availability means ordinary first five plus up to eight full-span separate
corrections, with no AI or learning. It is not fixed-menu-size or top-1 accuracy.

| Trial / trace | Original layout targets | Repacked targets | Lost targets |
|---|---:|---:|---:|
| 0 / first | 310 | 311 | 0 |
| 0 / second | 311 | 311 | 0 |
| 1 / first | 313 | 313 | 0 |
| 1 / second | 313 | 313 | 0 |

The only paired gain is `abojialiya` → `保加利亚` on the first trace of trial 0.
All ordinary first-five text lists match except `sj`/`ts` time/timestamp values.
This is evidence against a regression in this reused development sample, not a
proof of identical ranking, exhaustive recall, or an improvement from layout.
Timing variability still changes outcomes between trials.

The expanded 80-trace native menu/selection matrix passed on a standalone run.
An initial run failed because both `niaho` traces yielded no corrections; its
failure log is retained. No reason counter was captured, so attributing that
failure specifically to page faults or deadline expiry would be speculative.
The broader policy with the original layout has also shown this intermittent
failure. The five private learning/persistence cases pass with the repack,
including the genuine swap case, and pass against the ASan/UBSan library.
Sanitizer functional tests use the existing extended deadline. Go vet passes.
The initial native runner stopped before its ASan matrix, so this follow-up
claims sanitizer learning coverage, not a completed sanitizer matrix.

The unprofiled 2,280-key replay passes total engine gates: p50 72 µs, p99 1.28 ms,
p99.9 1.664 ms, maximum 1.794 ms. It excludes production correction publication,
client transport and AI. **There is no observed memory improvement:** maximum
sampled RSS over 1,500 compositions is 67,665,920 bytes (64.53 MiB), versus the
original-layout broader-policy run's 67,010,560 bytes (63.91 MiB). These sequential
single runs do not establish a statistically meaningful regression either.

A separate VM-map profile after clearing composition finds 21.2 MiB resident in
the repacked main dictionary and 22.2 MiB resident MALLOC_SMALL, compared with
20.7 MiB and 22.2 MiB in the earlier original-layout broad-policy profile. The
repacked file still spans roughly 57.9 MiB and has zero dirty bytes in that
mapping. Grouping indexes alone did not reduce touched pages on this workload.

**Decision:** do not adopt this layout as the memory fix. Preserve the original
complete table and abbreviation support. The next search/storage experiments
need to reduce actual pages visited while retaining candidate coverage; do not
add warming to conceal the failed memory result. The missing cold recovery,
per-session allocation bounds, internal conversion limits and production
integration remain unresolved.

[Follow-up captures, profiles, logs and hashes](typo-repacked-table/separate-stream/).
The 830 evaluation labels are the same as
[the memory-attribution comparison](typo-memory-attribution/labels.tsv).
