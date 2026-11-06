# Compact native reading/text mirror

Follow-up: [native mirror integration](typo-native-mirror.md) now connects this
representation to the scratch engine. The standalone measurements below retain
their original scope; they are not engine measurements.

Date: 2026-11-04. Standalone experiment; not integrated or shipped.

The [initial-cache investigation](typo-initial-cache.md) found first-access costs
in native reading lookup, entry weights and compressed Chinese text decoding.
This experiment stores the selected native entries and decoded text in compact
buffers loaded before queries, preserving the existing bounded candidate set.
It does not add a model, expand all typos or copy the whole native dictionary.

## What was built

`mirror_cache.h` keeps the original initial-code index, plus one native entry and
one text span per retained reading, and a shared UTF-8 byte pool. Native entries
retain the original string ID and exact float weight; canonical syllable IDs and
native offsets remain in the original cache. The unchanged top-eight search
returns readings; a bounded scan of their clean-initial bucket resolves each to
its mirror record. Each bucket contains at most eight records. No hash table,
string construction or native table access occurs during these queries.

The offline test verifies every code/offset/weight against native `QueryPhrases`
and decodes its Chinese text before creating the mirror. Equal text shares one
pool span. The writer emits explicit little-endian fields, not native structs.
The loader checks version magic, exact size, identity, bounded record/pool counts,
record correspondence with the initial cache, span arithmetic, native string-ID
range, UTF-8 and terminators. Failed loads leave the prior output untouched.

The `.identity` and `.initials` files are test handoff fixtures. They are **not an
authentication mechanism**. Structural parsing alone cannot prove that text and
weights came from the claimed dictionary. Production use still requires the
builder-verified table/prism/cache digest contract described in the parent report.
The engine continues to use the prior offset cache and native verification.

## Results

| Check | Observed result |
|---|---|
| Native reading/weight/text round trip | All 77,447 records match |
| Query sweep after closing the native table | 17,576 codes, 132,183 results; all resolve to matching text/weight |
| Intercepted C++ allocations during search/resolution | Zero |
| Retained payload | 3,322,066 bytes (3.17 MiB) |
| Fresh-load retained vector capacities, including initials | 3,322,506 bytes |
| Query sweep timing | p99 1.167 µs; maximum 7 µs |
| Fresh process after loading images | `xgr → 中国人` 1.584 µs; `zhr → 中国人` 1.208 µs |
| Standalone fresh-load process peak RSS | 8,880,128 bytes (8.47 MiB) |
| Parser checks | Wrong table identity and eight malformed-file cases rejected |
| ASan/UBSan | Round-trip, query and parser checks pass |

The sweep reuses the already validated top-eight search; it proves serialization
and record/text resolution preserve that search, rather than independently proving
all spelling semantics again. The earlier exhaustive dictionary oracle supplies
that narrow three-initial/one-neighbor search evidence. Tests close the native table
before the sweep. The fresh-load test never opens it, loading only the two cache
images and bounded identity/initial fixtures. Its first process test is intentionally
skipped; the image-only test runs explicitly. Conversely, the offline construction
run skips the image-only test. Sanitizers instrument the new test/header, with the
Release library used only for offline native dictionary preparation.

Fresh-query timing excludes image loading. The process took about 0.01 seconds
wall time including loading and checks, with no filesystem-cache flush. This is
neither cold-disk startup timing nor engine latency. RSS is for a standalone test,
not incremental engine memory or a full session pool. Construction containers are
not included in the fresh-load process. No recovery accuracy gain is claimed: the
candidate set is deliberately unchanged, and clean later-page losses remain.

Evidence: [round trip](typo-compact-mirror/mirror-roundtrip.log),
[fresh process and RSS](typo-compact-mirror/mirror-fresh-load.log),
[sanitizer](typo-compact-mirror/mirror-asan.log),
[artifact identities](typo-compact-mirror/sha256.txt).

## Next integration gates

Bind the mirror to the validated generation before any runtime use. Add an explicit
cached-entry/text path to the native accessor and iterator while retaining native
Phrase identity, canonical code, consumed span, correction provenance and learning.
Keep the mirror immutable and shared with a lifetime covering every accessor.
Do not call the original compressed string decoder for mirrored entries, including
from diagnostic logging. Missing or mismatched artifacts use ordinary fallback.

Then repeat native editing, partial selection, learning persistence, cold and warm
latency, allocations and session-pool RSS. Preserve the same candidate ordering
when comparing caches so that storage changes are not mistaken for accuracy gains.
The specific-spelling stage used by `njh → 你好` is outside this three-initial
mirror and can still exhaust; this cannot be declared fixed from these tests.
Broader abbreviation coverage, clean-menu policy and AI integration remain open.
