# Context-owned correction publication

Status: native scratch integration, not shipped; 2026-11-04.

The registry is now owned by the native Context rather than by the test. Once
explicitly configured, the engine publishes its finished correction snapshot
at the end of Compose. Read and Select obtain the current scope from Context;
they do not accept an allegedly current scope copied from a client token.

## Ownership and invalidation

`ConfigureSpikeCorrections(generation)` creates the fixed registry once, before
key handling. Unconfigured contexts keep it disabled and allocate no registry.
Each Context gets a process-local, monotonically assigned incarnation number
that is independent of its memory address or Rime session pointer. Generation
and incarnation zero are rejected; counters do not wrap into reusable identities.

The generation argument must be supplied by the engine owner and must be fresh
across worker/library lifetimes. A dictionary checksum or a constant is not
sufficient. The native test supplies a fixed fixture generation and exercises
a change to another fixture generation. The production Go bridge has not yet
been added, so cross-worker generation safety is a requirement, not a verified
product behavior. The incarnation counter alone is process-local.

Input/caret updates, clearing, commit/selection/deletion, option/property changes
and relevant composition-reset methods invalidate the registry before their
synchronous native notifications. Compose also invalidates before rebuilding.
A later callback can publish a new revision, but it cannot make the prior token
valid again. Same-text edits still advance the revision. Explicit invalidation
is available for the future focus/epoch bridge. Direct arbitrary mutation through
a mutable composition reference is not automatically intercepted; engine paths
must Compose or invalidate rather than bypass these boundaries.

Publication copies validated native traces and candidate identities from the
finished filter snapshot, then seals the registry. It does not consume the
snapshot's enumeration cursor. Republishing the same sealed revision is rejected,
including an empty publication. Selection clears the registry before invoking
the native selection hook and its callbacks. Context destruction releases its
registry; recreated objects at the same address have a different incarnation.

These are native methods and C++ types, not a client wire format or stable ABI.
The Context layout/constructor changes require rebuilding the library, plugins
and native tests together. No client protocol or installed IME is changed.

## Verification

The real-dictionary conversion fixture now configures the native Context once
and relies on automatic engine publication. Its 72 traces still compare ordinary
menus, validate raw edit metadata and select through the registry in all four
simplified/traditional/emoji combinations. It reads and selects through the new
Context API, not a test-owned registry. Live emoji-option changes invalidate old
tokens without changing raw input; generation changes and explicit invalidation
also reject the previous token.

Four lifecycle tests add checks for invalidation before callbacks, frozen
publication, generation changes, cross-context rejection, destruction/recreation
at the same address, and reuse of warmed publication storage. Along with the
existing ten registry tests, seven extraction/filter tests and real-dictionary
fixture, they pass in Release and full-library ASan/UBSan. Both native CTest
suites and ten legacy mixed-mode engine editing fixtures also pass in both
configurations.

A warm one-choice fixture performed 1,000 invalidate/publish/read cycles with
zero intercepted C++ new/new[] allocations and a Release mean of 743 ns per
cycle. This excludes initial configuration, candidate generation/conversion,
malloc-family calls and whole-engine costs. It is not an eight-choice benchmark
or a claim that the native key path is allocation-free.

The existing Go capture client's 2,100-key separate-stream replay reports
p50 72 µs, p99 1.024 ms, p99.9 1.152 ms and maximum 4.128 ms. That client does
not yet configure the Context registry: this checks compatibility and disabled-
registry overhead, not end-to-end publication cost. The native functional fixture
exercises enabled publication but does not establish an engine latency gate.
Sanitizer deadlines are relaxed for functional testing only.

[Logs and source hashes](typo-context-publication/).

## Remaining boundary work

Add a versioned native C bridge and purego binding, supply the real worker
generation identity, and connect app focus/session disposal and hot-swap
invalidation. Add versioned client export/selection, then verify stale frames
across reconnects and old-client fallback. Persistent learning, AI integration,
OpenCC internal expansion, full paging, and complete memory/allocation/latency
gates remain unverified. This native lifecycle work does not close those gates.

Reproduction additionally requires copying `correction_registry.h` to the scratch
librime `src/rime/gear/` directory. The existing native fixture initializes only
private warm sessions, with user dictionaries disabled; no live dictionary or
installed input method is touched.

Follow-up: [the versioned native/purego bridge](typo-native-bridge.md)
verifies copied snapshots, token selection and zero Go allocations with reusable
buffers. Product worker/client integration and learning persistence remain open.
