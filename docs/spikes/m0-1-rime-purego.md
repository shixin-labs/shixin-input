# M0 spike 1: Go → librime via purego

Date: 2026-10-26 · Machine: Apple Silicon Mac, macOS 26.7 · librime 1.17.0
(Homebrew) · rime-ice nightly · Go 1.27.3, `CGO_ENABLED=0`

Code: `internal/rime` (binding), `spikes/m0-rime` (benchmark).
Run with `scripts/fetch-rime-data.sh && go run ./spikes/m0-rime`.

## Result: works, well inside budget

- purego binding over `rime_get_api()` works without cgo. Typing and
  candidates are correct (`nihao` → 你好 👋, `hello` → hello).
- Passing `modules = {"default"}` keeps librime's `plugins` loader off, so
  Homebrew's librime-lua is never loaded (`rq` gives plain pinyin, no dates).
- Measured per key: `process_key` + `get_commit` + `get_context` +
  `get_status`, 29 corpus words (full pinyin sentences and English) × 20, all
  committed with space, n = 6,180 keys, after one warm-up pass:

| | No Lua (Shixin) | rime-ice with Lua | Budget (§3.7) |
|---|---|---|---|
| p50 | 49 µs | 259 µs | ≤ 300 µs |
| mean | 77 µs | 260 µs | |
| p99 | 494 µs | 697 µs | ≤ 2 ms |
| p99.9 | 965 µs | 1.25 ms | ≤ 8 ms |
| max | 1.11 ms | 1.48 ms | |
| Go heap allocs per key | 0.001 | 0.001 | 0 |

  A second no-Lua run gave p50 40 µs, p99 356 µs, max 917 µs. Expect
  run-to-run noise of this size.

- Cold costs, to be hidden by warm-up (§3.4): first `create_session` 30 ms,
  first key 6.8 ms. Deploying rime-ice from scratch takes 7.9 s; this is
  builder work, never on the engine thread.

## Findings

1. **purego allocates on calls with literal arguments.** `SyscallN(fn, a, b)`
   heap-allocates its variadic array: 1 allocation per call, ~6 per key.
   Passing a slice of a preallocated array (`Lib.call`) brings it to 0.
   `TestCallDoesNotAllocate` guards this.
2. **Lua costs 3–5× at the median.** That supports R9.
3. **rime-ice without Lua shows pinyin spelling hints** as candidate
   comments (`你好(［ni hao］)`), because one of its Lua filters hides them.
   The settings compiler must set the translator's hint/comment options.
4. librime 1.17 warns `config does not conform to schema` for rime-ice's
   `conversion_chain` (OpenCC emoji config). It's harmless; track it upstream.
5. C `Bool` returns must be read as `int32(r1)`; the upper 32 bits of the
   register are undefined on arm64.

## Not covered yet

- Engine thread priority, the IPC hop, and the full replay corpus. Those
  arrive with the engine (M1).
- Windows and Linux. purego supports both; to be re-measured in M3/M4.
