# Native edit provenance and registry selection

Status: scratch integration, not shipped; 2026-11-04.

The bounded generator now emits a typed edit trace alongside each edited prism
prefix. It records adjacent swap or neighboring-key substitution, original and
replacement bytes, the edited raw span, and the existing heuristic cost (8 or
10). The old callback interface remains as an adapter for earlier experiments.
No raw input is logged or persisted by this mechanism.

The trace travels through Correction and graph edge properties. Graph building
adds the suffix's raw offset; candidate extraction adds the current composition
segment offset. Traces therefore refer to the original raw input, including
positions after explicit apostrophes. They do not expand abbreviated input into
full pinyin or infer edits from Chinese candidate text/comments.

Candidate extraction uses the same bounded, longest-edge-first graph search
that previously counted corrections. It chooses a compatible path for the
candidate's canonical code and consumed span, requires exactly one correction,
and copies that path's trace. This is a deterministic compatible-path witness,
not proof that the dictionary retained a unique derivation. The graph still
collapses some alternatives; preserving all distinct lengths/costs is unresolved.
Missing or unrepresentable provenance rejects the correction rather than
fabricating an alignment. The path audit uses a fixed 32-pointer array and
retains its 1,024-step limit. Other graph and candidate allocations remain.

The trace cost is metadata only. Graph credibility still uses the existing
correction penalty; this change does not calibrate channel probabilities or add
another ranking penalty. Missing/extra-key errors and cross-boundary swaps are
not added by this work.

## Two integration bugs found and fixed

The ordinary graph consumes trailing delimiters as part of an edge. Anchor
selection previously compared `ni'` directly with canonical `ni`, classified
that clean prefix as suspicious, and searched the wrong syllable. A stronger
`ni'hso → 你好` test failed. Anchor selection now excludes leading/trailing
delimiters when comparing canonical spelling and counting suspicious letters;
raw edit positions remain absolute. The strengthened test now selects the
intended result and checks `s → a` at bytes 4–5 (versus 3–4 for `nihso`).

A conversion shadow around a correction shadow exposed a second failure:
librime's original genuine-candidate helper removed only one shadow layer. The
scratch helper now unwraps up to eight shadow/first-uniquified layers and rejects
a deeper or empty chain. The plural helper preserves top-level alternatives
while resolving each through that bounded helper. Trace forwarding follows the
same representative through standard shadow and uniquified wrappers. This
retains native Phrase identity when displayed text changes, which is required
for learning integration; persisted learning itself remains untested.

These changes affect native class layouts/interfaces and genuine-candidate
resolution. Rebuild the library, plugins and native tests together. The patch
remains scratch-only and still applies to the pinned librime base.

## Registry bridge and verification

`ChoiceFromNativeTrace` validates edit shape, lower-case letters, nonzero
heuristic cost, raw bounds and the original bytes against the current input,
then creates the registry choice. It trusts native generation for keyboard
adjacency and dictionary-path membership. It does not accept a client-supplied
edit or infer one from a comment.

The real-dictionary test now publishes recovered candidates into the bounded
registry and selects through a single-use token. Its session ID comes from the
scratch session, its revision increases for each fixture trace, and its generation
ID is a fixed test generation. This connects the components in a native test;
the actual engine revision manager and client protocol are not yet wired.

Verification on the final source:

- Prefix enumerator tests pass in Release and ASan/UBSan. Applying each trace
  reproduces the emitted prefix, respects delimiters, and allocates no C++
  new/new[] storage in that enumerator fixture.
- Five extractor/envelope tests, ten registry tests and one real-dictionary
  integration test pass in both Release and full-library ASan/UBSan.
- The integration test covers nine inputs, including abbreviations and an
  explicit apostrophe, twice each. Every offered recovery validates against raw
  input. Up to 100 ordinary candidates retain text, comments and spans compared
  with correction-disabled control sessions and after recovery enumeration.
- Both `nihso` and `ni'hso` select `你好` through the registry, assert exact
  commit text, and reject reuse of the token. Wrapper tests preserve native
  identity through changed display text, and reject excessive unwrap depth.
- Both native CTest suites and all ten legacy mixed-mode engine editing fixtures
  pass in Release and ASan/UBSan.
- The warm separate-mode 2,100-key trace reports p50 52 µs, p99 960 µs,
  p99.9 1.152 ms and maximum 3.814 ms. It does not include future client
  publication or complete recovery draining on the engine key path. Sanitizer
  deadlines remain relaxed for functional testing and are not timing evidence.

[Logs and source hashes](typo-edit-provenance/). No broad recall or clean-input
accuracy improvement is claimed from these fixtures.

## Remaining work

Wire real engine generation/session/revision ownership and frozen publication;
apply the correct conversion/deduplication pipeline to the detached stream; add
versioned client export and explicit selection; verify learning persistence,
full ordinary paging, AI behavior, memory and allocation budgets. The registry
bridge in a native test is not a replacement for those product-level checks.

Follow-up: [separate correction conversion and deduplication](typo-correction-filters.md)
verifies traditional/emoji display and registry commits while preserving the
compared ordinary menus. Internal conversion budgets and production publication
remain open.
