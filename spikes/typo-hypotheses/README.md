# Offline per-spelling candidate-cap experiment

Not product code. Tests ranked spelling hypotheses with ten native menu entries
per spelling, against a twenty-entry control. No AI, user telemetry or live IME.

```sh
CGO_ENABLED=0 go test ./spikes/typo-hypotheses
CGO_ENABLED=0 go run ./spikes/typo-hypotheses -mode prepare -root build/typo-recovery/hypotheses-ten-v1
CGO_ENABLED=0 go run ./spikes/typo-hypotheses -mode capture -root build/typo-recovery/hypotheses-ten-v1
CGO_ENABLED=0 go run ./spikes/typo-hypotheses -mode report -root build/typo-recovery/hypotheses-ten-v1
```

Preparation requires the existing `constrained-v1/selected.jsonl` synthetic
development labels and `abbr-recovery-v1/syllable-lexicon.tsv`. Output directory
must be new. Capture requires the original stable scratch library, warm generation,
prebuilt dictionaries and OpenCC under `build/typo-recovery`. It uses a temporary
private user directory, learning-disabled schema, and one locked OS thread.
Capture output must not exist. Native capture reuses one warm session, clearing
composition and draining commits before every replay. It confirms each candidate
by selecting it and checking exact commit text and empty preedit. It does not test
selection against the original mistyped string or production worker integration.

The planner enumerates one swap or neighboring-key substitution and ranks legal
full/initial/mixed segmentations by syllable prior and edit cost. Whole-string swaps
include cross-boundary edits; this is broader than the proposed first-version
within-syllable scope. It keeps up to sixteen spellings under 32,768 DP transitions
and a 900 µs planner deadline. These budgets exclude native retrieval. Exhaustion
discards the planner output for that input. The Go experiment allocates and is not
an engine implementation.

Each spelling contributes its first ten **raw native entries**, with only verified
whole-input phrases counted. Partial candidates consume slots; no deeper refill.
Duplicate text across spellings is removed. The ordinary first ten are a separate
availability baseline. This evaluates availability, not top-1 accuracy, clean-input
UI safety, end-to-end latency, allocation compliance or resident memory.
