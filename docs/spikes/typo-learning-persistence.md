# Correction learning across process restart

Status: isolated experiment, not product activation; 2026-11-04.

The versioned correction bridge now has an opt-in writable-dictionary test in
`internal/rime/typo_learning_spike_test.go`. Each case gets a fresh private user
directory and a private compiled schema named `typo_learning`. Only its primary
translator enables learning, with the private database name `typo_learning`.
The original warm schemas and maintained librime submodule remain unchanged.

A writer process selects eight candidates, ends the final learning transaction
through the normal unhandled-Tab path, destroys its session and exits. Only after
that process exits does a second process initialize the deployer and export the
database, without creating a session. This verifies persisted database contents,
not just a candidate's native type or an in-memory ranking change.

| Case | Selection path | Fresh-process export |
|---|---|---|
| `nihso` → `你好` | Correction snapshot token | Exactly one entry: `你好`, code `ni hao`, count 8 |
| `nihso` → `👋` with emoji enabled | Converted correction snapshot token | Exactly the same original phrase/code/count; no emoji learned |
| `nihoa` → `你好` | Ordinary native spelling alias, control | Exactly the same original phrase/code/count |
| View `nihso` corrections eight times, then cancel each composition | No selection | Zero entries |

All four cases pass in Release and against the ASan/UBSan native library.
`go vet -tags shixin_typo_spike ./internal/rime` also passes. Tests use
`CGO_ENABLED=0`; no installed IME or live user dictionary is opened. Native ASan
uses the existing extended functional deadline, so its timings are not latency
evidence. [Recorded logs and source hashes](typo-learning-persistence/).

## Coverage discovery

An initial test incorrectly expected `nihoa` to expose `你好` as a new correction.
The compiled prism already supports that spelling through its `ao` → `oa`
derivation. It belongs in the ordinary-menu control, not a claim of new swap
recovery. A second attempted input, `niaho` (swapping `h` and `a` within intended
`hao`), did not expose `你好` through the separate correction snapshot after
three traces. That remains a retrieval failure to diagnose; this report does
not claim the new swap path passed a persistence test. It must not be hidden by
selecting an ordinary fallback or expanding the search deadline.

The correction cases allow up to three fresh traces before each selection to
accommodate a cold bounded-search fallback. No ordinary candidate substitutes
for a missing correction. This is a learning test, not cold-start recall or
latency evidence.

## Reproduction and limits

Use the scratch runtime from [the native bridge report](typo-native-bridge.md):

```sh
CGO_ENABLED=0 SHIXIN_TYPO_SPIKE_ROOT="$PWD/build/typo-recovery" go test -tags shixin_typo_spike ./internal/rime -run '^TestTypoLearningPersistenceSpike$' -count=1 -v
```

The result establishes canonical learning for the adjacent-key full-phrase
fixture and its emoji wrapper. It does not establish partial-selection learning,
new compound words, forgetting, abbreviation learning, crash durability,
production worker handoff, or a fresh engine's learned ranking. Those gates,
the missing swap recovery, client integration, AI integration and complete
resource limits remain open.

Follow-up: [alternate-boundary recovery](typo-alternate-boundaries.md) adds a
genuine `niaho` swap selection/persistence case. It passes after relaxing the
second stage’s repeated anchor restriction, with a measured memory tradeoff.
The four-case results above describe the earlier revision.
