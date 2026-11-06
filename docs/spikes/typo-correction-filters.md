# Conversion and deduplication of separate corrections

Status: scratch integration, not shipped; 2026-11-04.

The detached correction stream now has a private filter history. It no longer
requires publishing raw, unconverted dictionary candidates. The engine builds
and finishes this snapshot during translation, before a registry consumer can
read it. Subsequent duplicate merging cannot mutate a published list.

Filters must explicitly opt in. Deterministic Simplifier instances and the
Uniquifier opt in; reranking, exact-code pins, and other ordinary-menu filters
do not. A random Simplifier does not opt in, avoiding additional calls into
shared RNG state. The supported settings compiler does not enable that random
mode. Segment tag applicability and conversion options still apply.

Ordinary filters keep their existing pipeline and candidate history. The
correction pipeline uses a separate candidate vector, reserves eight entries,
and retains at most eight output candidates. A wrapper limits values pulled
through each filter to 64. These are presentation/scan bounds: stopping after a
window of individually valid corrections does not invalidate an ordinary menu.
Incomplete spelling recovery still uses the existing bounded fallback.

The OpenCC objects are initialized during schema construction when the scratch
separate-mode flag is set, rather than loading their files on the first
correction key. Existing weak-cache sharing of converters remains. This moves
resource work to initialization and can increase startup time and resident
memory even when a conversion option is initially off. It is not enabled in
product settings or ordinary-only schemas.

## Verification

The native real-dictionary test now exercises nine inputs twice in each of four
option combinations: simplified/traditional Chinese crossed with emoji off/on
(72 traces). It compares up to 100 ordinary candidates against a correction-off
control, including text, comments and consumed spans. They agree and remain
unchanged after reading the independent corrections. This is still a bounded
comparison, not proof of equality on every later page.

With traditional conversion enabled, a recovered `xgr` candidate displays and
commits `中國人` while retaining native Phrase text `中国人`. With emoji enabled,
`nihso` and `ni'hso` can explicitly commit `👋` while retaining native Phrase
text `你好`. Each selection goes through the registry, checks the original raw
edit trace, asserts exact committed text and rejects token reuse. Ordinary
selection keys are not redirected to corrections. This is a native test, not
a client UI or persistent-learning test.

Two new filter-stage tests verify independent duplicate history, exclusion of
unaudited filters, the eight-output snapshot and the 64-value duplicate scan
cap. Together with the five extraction tests, ten registry tests and native
integration test, all focused tests pass in Release and ASan/UBSan. Both native
CTest suites and all ten legacy mixed-mode engine editing fixtures also pass
in both configurations.

The separate-mode warm engine trace of 2,100 keys reports p50 72 µs,
p99 1.024 ms, p99.9 1.149 ms and maximum 1.149 ms. It now includes building the
filtered correction snapshot. Conversion options in this timing trace are off;
the functional conversion matrix is not a conversion-latency benchmark. Future
client publication and AI costs are not included. Sanitizer runs use relaxed
functional deadlines and are not timing evidence.

In two fresh-worker, one-context replays of 1,350 compositions, maximum sampled
RSS was 52,969,472 bytes (50.52 MiB) for ordinary input and 58,884,096 bytes
(56.16 MiB) for the separate correction prototype: a 5.64 MiB difference. This
compares the whole prototype against ordinary input, not the filter's isolated
cost. Samples do not bound transient peaks, many contexts, conversion-enabled
workloads or AI memory. No separate typo model is loaded.

[Logs and source hashes](typo-correction-filters/).

## Limits and remaining work

The 64-value wrapper cannot interrupt work performed inside a single OpenCC
conversion. OpenCC's existing word-conversion routine can construct multiple
intermediate forms before returning. Its internal expansion, temporary storage
and per-call latency are not bounded by this new wrapper. Resolve that before
claiming production memory/latency guarantees; do not confuse eight retained
choices with bounded internal conversion memory. The native pipeline also
still allocates filter, vector and candidate objects.

Next, bind publication to actual engine generation/session/revision ownership,
add versioned client export and explicit selection, and verify persistent
learning. Conversion-aware AI, complete ordinary paging, all supported settings,
Windows behavior and the allocation/resource gates remain open.

For reproduction, additionally copy `correction_filters.h` into the scratch
librime `src/rime/gear/` directory and rebuild the complete native library and
plugins after the Filter interface change. The native test uses a fresh private
user directory with a symlink to the scratch runtime's immutable OpenCC data;
the engine capture tool already copies those resources into its private user
directory. Neither path uses the installed IME or live user dictionary.

Follow-up: [context-owned native publication](typo-context-publication.md)
adds engine-driven snapshots and revision/incarnation invalidation. The real
worker-generation bridge and client protocol remain outstanding.
