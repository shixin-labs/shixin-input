# Consistent correction exhaustion fallback

Date: 2026-11-04. Scratch native correction path, not shipped.

The previous prototype discarded incomplete three-initial lookup results but
could retain partial dictionary collectors for longer inputs. It also allowed
some work counters to continue after another counter had marked the request
exhausted. This was inconsistent with the proposed per-request fallback policy.

The scratch patch now applies the same rules to all bounded correction graphs:

- Dictionary lookup discards its collector after exhaustion, and checks the
  deadline around sorting for every bounded path.
- An incomplete first correction stage is discarded. An incomplete second stage
  is discarded while an already completed first stage may remain available.
- Once exhausted, a carried budget rejects further work. A new request receives
  a fresh budget; no user setting or persistent feature state is disabled.
- Ordinary lookup remains independent of correction budgets. No new wait, model,
  deadline extension or post-display menu update is added.

The diagnostic schema option `shixin_spike_work_cap` defaults to the existing
32,768-unit cap and clamps to 0–32,768. It allows deterministic exhaustion in
scratch engine tests; it is not a supported product setting. The optional second
stage carries the remaining budget rather than resetting this cap.

## Verification

`exhaustion_test.cc` first confirms that a native lookup can return one completed
chunk before exhausting a one-chunk budget. The Dictionary-level call must then
return no partial collector. The test also covers an already-expired deadline,
a fresh successful budget and ordinary lookup. A second test checks that the
exhausted state remains set when carried to another graph.

Ten public codes were captured with the same library using ordinary configuration
and correction enabled with a zero work cap. All ten captures matched exactly
for preedit, the first thirteen candidates, comments, confirmed spans and paging
flags. Inputs include full pinyin, abbreviations, longer mixed input and partial
selection material: `nihao`, `nihso`, `nihoa`, `njh`, `xgr`, `zhr`, `cnjw`,
`ddegn`, `changshicuowj`, `nihsozhongguo`.

Both native tests pass in Release and with the full native library under
ASan/UBSan. All ten editing fixtures also pass in both configurations. The
sanitizer engine uses the existing relaxed functional-test deadline; it is not
performance evidence. The final Release 2,100-key trace measured p50 56 µs,
p99 960 µs, p99.9 1.152 ms and maximum 1.165 ms. No zero-allocation claim is
made for the native correction pipeline, which still allocates.

This proves deterministic initial exhaustion fallback for those engine cases;
the native test separately covers exhaustion after partial lookup progress.
It is not a broad clean-input regression result. Normal correction still inserts
entries into later pages, and the retrieval/layout problem remains open.

Evidence and hashes: [typo-exhaustion-fallback/](typo-exhaustion-fallback/).
Source: `spikes/typo-recovery/native-graph/exhaustion_test.cc` and `librime.patch`.
