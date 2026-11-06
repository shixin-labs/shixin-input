# Selective dictionary warming

Date: 2026-11-04. Scratch native experiment, not shipped.
Parent: [canonical-cache experiments](typo-canonical-cache.md).

The truncated auxiliary dictionary lost eighteen longer typo targets. Warming
the complete original dictionary retained its entries but cost about 45 MiB more
sampled RSS than the mirror baseline. Neither result justifies a default change.

## Measure the layout first

`table_pages_test.cc` scans the native mapping and marks pages containing
metadata, index arrays, one-to-three-syllable entries and compressed strings.
It checks every marked range against the mapped file size. This is logical page
coverage, not incremental RSS: the diagnostic scan itself faults pages.

| Page size | Total file pages | Index pages | String pages | Metadata/index/string union | Union including short entries |
|---|---:|---:|---:|---:|---:|
| 4,096 bytes | 14,809 | 11,164 | 1,798 | 12,961 | 14,752 |
| 16,384 bytes | 3,703 | 3,161 | 451 | 3,611 | 3,703 |

This host reports 16,384-byte pages. The native builder interleaves index and
payload allocations: warming all indexes and strings would cover almost the
entire mapping. Calling it selective would not make it inexpensive.

## String-only experiment

The scratch schema option `shixin_spike_warm_strings: true` touches only the
7,359,696-byte compressed string table during translator initialization. It
validates the range, includes the last byte, and performs no new warming during
key handling. It keeps the complete original dictionary and existing initial
mirror; neither the truncated canonical option nor full-table warming is set.
No duplicate word index or text pool is allocated by this operation.

This does not promise resident index pages or eliminate page faults. The existing
bounded search and deadline fallback remain necessary. A future compact layout
could separate indexes from payload without deleting words, but is not part of
this experiment.

## Focused verification

All ten editing fixtures pass, including the longer phrase, partial selection,
Backspace and raw Return. The 2,100-key warm engine trace measured p50 56 µs,
p99 832 µs, p99.9 1.018 ms and maximum 1.018 ms. A separate fresh engine sampled
maximum RSS of 62,750,720 bytes (59.84 MiB) over 1,350 compositions in one
context. The preceding same-workload mirror run sampled 58,195,968 bytes
(55.50 MiB), a 4.34 MiB difference. These sequential samples are not a multi-
context bound or an estimate of OS-wide private memory.

Three native tests pass in Release and with the full native library under
ASan/UBSan. They cover unopened/closed-table warming, stable table metadata,
auxiliary ownership and candidate text decoding. The full-library sanitizer
engine run also passes all ten editing fixtures. Sanitizer tests use the existing
relaxed diagnostic deadline, so they provide functional safety evidence, not
latency evidence. General native lookup still allocates; this work does not
establish a zero-allocation candidate pipeline.

The fresh-worker gate did **not** pass: both `cnjw` and `ddegn` lacked their
targets in the first capture. Captures two and three found 纯净物 at rank seven
as a correction and 吊灯 at rank eleven as an ordinary candidate. String warming
is therefore not demonstrated to fix these misses. No full corpus run was started
for this option after that result. Do not infer a recovery gain from memory or
focused-test results. Clean
later-page displacement and native learning verification remain separate open
issues.

## Isolate the remaining cost

The read-only `cache_stages_test.cc` diagnostic now supports explicit string
warming, correction mode and anchor, and separately times graph construction,
dictionary lookup and decoding up to 64 returned entries. With specific correction
and anchor zero, in separate fresh processes:

| Input | First graph | First dictionary lookup | First decode | Lookup exhausted |
|---|---:|---:|---:|---|
| `cnjw` | 332 µs | 5,035 µs | 270 µs | yes |
| `ddegn` | 335 µs | 4,005 µs | 75 µs | yes |

Later lookup attempts fell to 164 and 185 µs respectively. Both graphs completed
without exhausting their budget. This localizes the observed cost to dictionary
lookup in this diagnostic, despite warmed strings. It is not an engine replay:
anchor zero is explicit, ordinary lookup is omitted, no user dictionary is open,
and the diagnostic decodes completed chunks even after lookup exhaustion.
The cooperative deadline cannot interrupt a slow individual memory access or
native operation; these results must not be described as a hard 900 µs bound.

Next inspect the lookup's page access and exhaustion handling. Repacking the
complete index separately from payload is a possible layout experiment; dropping
longer words or increasing the typing deadline is not justified by these results.

Sources: `spikes/typo-recovery/native-graph/table_pages_test.cc`,
`canonical_native_test.cc` and `librime.patch`. Evidence is saved under
[typo-selective-warming/](typo-selective-warming/). Use the ordinary mirror
generation plus the one string-warming option for reproduction; leave both
`shixin_spike_canonical_table_file` and `shixin_spike_warm_original` unset.
