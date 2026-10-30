# Offline menu capture

This tool feeds explicit public corpus codes through an isolated
`shixin-engine -capture`. It uses the existing binary IPC protocol; librime
continues to run only on the engine's locked thread. No user typing, installed
IME, live settings or live dictionary is read.

Build the helpers and a private generation first:

```sh
CGO_ENABLED=0 go build -o build/bin/ ./cmd/shixin-engine ./cmd/shixin-builder ./tools/menu-capture
build/bin/shixin-builder -lib "$SHIXIN_LIBRIME" -data build/rime-data \
  -settings /path/to/public-settings.json -output /tmp/shixin-capture-generation
build/bin/menu-capture -engine build/bin/shixin-engine \
  -generation /tmp/shixin-capture-generation -data build/rime-data \
  -lib "$SHIXIN_LIBRIME" -profile PROFILE_SHA256 \
  -input codes.jsonl -output menus.jsonl -limit 50
```

Input is one `{"code":"tw"}` per JSONL row. Codes must be unique lowercase
ASCII letters, 1–32 bytes. The collector rejects answer-bearing extra fields,
so gold words never affect retrieval. Output must be new; a partial file after
failure is not approved data. The training project snapshots settings,
dictionaries, the runtime and compiled generation and assigns `PROFILE_SHA256`.

The private worker allocates its own scratch user directory and socket. It
validates warm-only schemas with every translator's user dictionary disabled,
never promotes, and never runs deploys. Core OpenCC resources are copied into
the scratch user directory. The worker exits when the collector closes stdin,
on a signal, or when the job deadline is reached; scratch files are removed on
normal shutdown. No Lua loader is enabled. Capture does not modify any typing
hot path or binary IPC schema.

Each output row contains `schema_version`, `profile_id`, actual `schema_id`
(normally `rime_ice_warm`), `code`, `preedit`, and ordered `candidates`. Each
entry retains text/comment, zero-based page/index and one-based raw menu rank.
`complete` means the requested raw prefix was captured successfully.
`last_page` means that prefix exhausted the menu; `truncated` means the limit
was reached while more raw entries remained. The limit is at most 50 **raw**
entries, including partial-span, emoji, English and native translator results.

For each candidate a fresh IPC connection/session replays the code and page.
Every typed key must commit nothing, the final state must still compose, and
the candidate at that page/index must match the original text. A selection that
commits exactly that text and clears composition confirms **whole-input
acceptance**. Only then are `full_span=true`, `span_start=0` and
`span_end=len(code)` recorded. This is a conservative acceptance probe, not
introspection of Rime's internal candidate start/end. Unconfirmed candidates
have no invented span fields. A candidate that changed during the probe is
retained with `span_verification=candidate_changed` and no confirmed span.

Commit history is session-local; each probe therefore needs a fresh connection.
Clear/FocusOut would retain it. Disabled user dictionaries ensure probes cannot
teach later sessions. Candidate ordering is verified again during replay.

`captured_at`, `timezone` and `timezone_offset_seconds` accompany every row.
Date/time translator output can change between captures even for the same
profile. Such records are timestamped observations, not a claim of bitwise
reproducibility. The collector does not infer dynamic component types or
dictionary priors; the ordinary UI IPC does not expose them.

The UI menu is after emoji/traditional/uniquifier filters. The planned native
ranker export is earlier in that pipeline and takes a contiguous prefix with
the first candidate's span. For a conservative whole-input benchmark, require
the first entry to verify and stop at the first unverified entry. Do not scan
past it to gather later full-input candidates or claim exact native-export
equivalence. Gold labels remain outside capture and missing targets remain
retrieval failures.

Run protocol tests with `go test ./tools/menu-capture`. For an actual isolated
generation using this repository's public rime-ice data and built librime:

```sh
CGO_ENABLED=0 SHIXIN_CAPTURE_INTEGRATION=1 \
  go test -v ./tools/menu-capture -run TestCaptureRealGeneration
```
