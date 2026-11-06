# Versioned native correction bridge

Status: scratch C ABI and purego binding, not enabled in the product; 2026-11-04.

The native Context registry can now be configured, read, invalidated and selected
through exported C functions. Snapshot buffers contain copied text/metadata and
scoped tokens, never native candidate pointers or C++ object layouts. Calls use
the existing opaque Rime session handle and must run on the locked engine
thread. The binding borrows the loaded library's lifetime.

The implementation is in `spikes/typo-recovery/native-graph/correction_api.{h,inc}`
and `spikes/typo-recovery/bridge/`. No production binding, IPC codec, client UI or
settings default changes. The opt-in Go integration test lives behind the
`shixin_typo_spike` build tag in `internal/rime/typo_bridge_spike_test.go`.

## ABI v1

`shixin_spike_corrections_version` returns 1. The four `_v1_` exports configure
an engine-owned generation, read a snapshot, select a token and invalidate the
current snapshot. Configure/select/invalidate return 1 for success and 0 for
rejection. Read returns 0 for success (including an empty snapshot), -1 for
invalid buffer/capacity, -2 for an unknown session, and -3 for inconsistent
snapshot data. A valid-size output is cleared on read failure. A short buffer
is left untouched. No function retains caller buffer addresses.

Multibyte fields are little endian, independent of C++ struct alignment. The
full snapshot occupies 2,640 bytes: a 16-byte header and eight 328-byte records.
The header contains version, total byte size, count, and a zero reserved word.
Unused records and padding are zeroed. A record is:

| Offset | Bytes | Field |
|---|---:|---|
| 0 | 8 | Worker/generation identity |
| 8 | 8 | Native Context incarnation |
| 16 | 8 | Composition revision |
| 24 | 8 | Publication counter |
| 32 | 1 | Native slot |
| 33 | 7 | Reserved, zero |
| 40 | 4 | Raw consumed begin |
| 44 | 4 | Raw consumed end |
| 48 | 4 | Edit kind: swap 0, neighbor 1; missing 2/extra 3 reserved for existing metadata types |
| 52 | 4 | Raw edit begin |
| 56 | 4 | Raw edit end |
| 60 | 4 | IEEE-754 float32 heuristic cost |
| 64 | 4 | UTF-8 text byte length |
| 68 | 4 | Reserved, zero |
| 72 | 256 | Text followed by zero padding |

The selection token is the first 40 bytes. Selection never trusts text or edit
metadata sent back by a client; it resolves the token against the retained native
candidate. Reserved token bytes, wrong size, invalid slot/scope, stale revision
and reuse are rejected. Native records are slot ordered. Presentation may reorder
views while retaining their tokens; it must not rewrite native slots to match
display positions. This ABI is distinct from the eventual client IPC extension.

## Go binding and verification

Attach resolves all symbols once and checks the ABI version. Missing/incompatible
helpers return `ErrUnsupported`, allowing ordinary input to remain available.
The binding uses reusable arguments and caller-owned snapshots. Typed references
keep buffers alive during foreign calls and ensure stable heap storage; no native
address survives in exported record data. Failed Go reads also clear the buffer.
Record text aliases the snapshot and must be copied into another owned buffer
before that snapshot is reused asynchronously.

The decoder checks exact size/version, count, reserved bytes, nonzero identities,
consistent scope/publication across records, native slots, raw/edit spans, edit
shape, finite nonnegative cost, UTF-8 and padding. It does not treat the existing
8/10 costs as calibrated probabilities or add new correction operations.

The native conversion fixture now selects through the C ABI in all four
conversion-setting combinations. It verifies canaries around output buffers,
short/null buffers, zero generation, malformed size/reserved bytes/slots, wrong
generation, token reuse and reads after session destruction. It also exports a
public `nihso` snapshot consumed by the Go codec test. Those native tests pass in
Release and ASan/UBSan, together with the registry/filter tests and native CTest
suites.

The real purego test uses a fresh private warm session, generates a nonzero
worker-lifetime nonce before keys, types `nihso`, reads the native snapshot,
selects `你好`, asserts the exact commit and rejects reuse/invalidation. It
also checks missing-symbol fallback and cleared output for invalid sessions.
It passes with `CGO_ENABLED=0` against Release and ASan/UBSan libraries. This
proves the binding in an isolated session, not production worker lifecycle or
client reconnect behavior.

With reusable buffers, 1,000 native snapshot reads/first-record decodes and
1,000 stale-selection checks record zero Go allocations. A separate warm read/
decode sample reports p50 2 µs, p99 2.5 µs and maximum 9.208 µs. A one-record
codec benchmark reports 1,256 ns/op, 0 B/op and 0 allocations/op on the test
machine (macOS arm64, Apple M4). These are component measurements, excluding
candidate generation, UI, IPC and AI; sanitizer timings are not performance
evidence.

Codec tests and `go vet` pass. A short two-worker fuzz run completed 48,985
executions without failure. It is a smoke fuzz run, not exhaustive parser proof.
Windows/native cross-platform ABI execution remains untested.

[Logs and source hashes](typo-native-bridge/). The native byte fixture is
`spikes/typo-recovery/bridge/testdata/native-snapshot.bin`.

## Reproduction and remaining work

Copy `correction_api.h` and `correction_api.inc` into scratch librime `src/`
after applying the maintained patch, then rebuild both the library and tests.
The existing native fixture writes a snapshot when `SHIXIN_SPIKE_EXPORT_FILE`
is set. All data comes from private warm sessions; no live user dictionary or
installed IME is used.

```sh
CGO_ENABLED=0 go test ./spikes/typo-recovery/bridge -bench BenchmarkSnapshotDecode -benchmem
CGO_ENABLED=0 SHIXIN_TYPO_SPIKE_ROOT="$PWD/build/typo-recovery" go test -tags shixin_typo_spike ./internal/rime -run '^TestTypoNativeBridgeSpike$' -count=1 -v
```

Next, connect actual worker generation and focus/session ownership, add
capability-negotiated client export/selection, and verify reconnect/hot-swap
staleness and old-client fallback. Persistent learning, AI integration, full
ordinary-page comparisons and complete resource gates remain open. The internal
OpenCC expansion limit is still unresolved. Do not enable the product based on
these bridge tests alone.

Follow-up: [the private learning experiment](typo-learning-persistence.md) verifies
canonical persisted entries for adjacent-key correction selections and emoji
wrappers. Broader learning and product integration gates remain open.
