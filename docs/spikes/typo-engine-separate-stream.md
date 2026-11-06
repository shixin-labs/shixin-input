# Separate recovery stream in the scratch engine

Status: experimental native integration, not a shipped feature; 2026-11-04.

With `translator/shixin_spike_protected: true` and
`translator/shixin_spike_separate: true`, the script translator now returns an
envelope whose ordinary Peek/Next operations never enumerate corrections. The
engine takes its independent recovery stream before checking whether the ordinary
translation is exhausted, and stores it on the current Segment. Consequently,
even a recovery-only envelope can transfer suggestions without inserting them
into the ordinary menu. The existing mixed adapter remains the default scratch
control; product settings and defaults are unchanged.

This is the first actual engine separation experiment, following the
[independent extractor](typo-recovery-stream.md) and
[native explicit-selection hook](typo-explicit-selection.md).

## Native ownership

`Translation::TakeSpikeCorrections()` transfers ownership once. The engine
accepts only the first auxiliary stream per segment; the current generated
schemas have one script translator. Multiple streams are not silently merged
into unbounded storage. Segment clearing, replacement translation and reopening
release the segment's stream. Actual stale-selection protection still requires
the authoritative session/revision registry integration; a retained native
pointer alone is not a client selection capability.

The detached stream is raw: it has not passed through conversion, emoji,
uniquifier or pin filters. Ordinary translations still pass through their normal
filter chain. Do not present this stream as final UI text or run stateful filters
a second time without a reviewed integration. Registry publication still needs
edit provenance from the selected spelling path, not inferred candidate comments.

The internal Translation virtual interface and Segment layout change. Rebuild
the entire scratch library, statically linked plugins and native test binaries
together. This is not a stable native ABI or an IPC extension.

## Verified behavior

`separate_stream_test.cc` initializes the real library on a single test thread,
using the prepared warm schemas and a newly created private user directory. It
does not deploy data or use a live user dictionary. One session uses the separate
mode and a control session disables correction. Both disable dictionary learning.

For `nihao`, `nihoa`, `nihso`, `njh`, `zhongguo`, `shijie`, `nh`, and `xgr`,
two typing traces compare ordinary text, comments and raw consumed spans before
and after draining recovery. The snapshots agree with the control and remain
unchanged by recovery enumeration. The test checks up to 100 candidates; this
is not proof of complete equality beyond that boundary. Four inputs hit that
boundary (`njh`, `shijie`, `nh`, `xgr`). All inputs have nonempty ordinary menus,
so this comparison is not vacuous.

On the warmed `nihso` trace, the test finds recovered `你好`, preserves its
genuine native Phrase, selects it through the native hook and asserts the exact
commit text and cleared composition. Clearing composition also releases the
segment-owned recovery stream. This verifies a real dictionary-backed selection,
not learning persistence, client token validation or keyboard/UI selection.

The real-dictionary test passes in Release and full-library ASan/UBSan. Five
extractor/envelope tests also pass in both configurations, including one-shot
transfer when ordinary results are absent. Both native CTest suites pass in
Release and ASan/UBSan. The existing ten mixed-mode engine editing fixtures pass
with the rebuilt Release library.

A separate-mode scratch-engine trace of 2,100 keys reports p50 52 µs,
p99 1.024 ms, p99.9 1.28 ms, and maximum 3.739 ms. It exercises typing, ordinary
paging and Backspace, but the current client does not request or display the
remaining correction stream. These figures therefore do not establish the
future complete correction-publication cost. Native allocation and full memory
gates remain open. Sanitizer deadlines are relaxed for functional testing only.

Logs and source hashes: [typo-engine-separate-stream/](typo-engine-separate-stream/).

## Reproduction and next work

Apply the maintained scratch patch and copy both translation `.inc` files as
described in the native spike README. Copy the existing verified mirror
generation to `build/typo-recovery/native-graph/separate-generation-v1`; set
`translator/shixin_spike_separate: true` in its built warm schema. Clone that
schema as `rime_ice_control_warm.schema.yaml`, change its schema ID accordingly,
and set `translator/shixin_spike_protected` and `translator/enable_correction`
to false. Keep the existing warm-only default schema list, read-only dictionary
resources and disabled user dictionaries. Compile `separate_stream_test.cc`
against the matching scratch library and gtest, then run with
`SHIXIN_TYPO_SPIKE_ROOT` pointing at the absolute scratch runtime root.

The next integration must attach typed edit provenance, convert and deduplicate
corrections, bind publication to authoritative generation/session/revision
identity, and expose versioned client selection. Recheck clean ordinary menus
with actual AI, pins, conversions and complete paging; test persisted learning
in an isolated writable fixture. Until these pass, the native separation result
is useful evidence, not a claim that the whole clean-input problem is solved.

Follow-up: [native edit provenance and registry selection](typo-edit-provenance.md)
connects actual recovery candidates to registry selection in a scratch native
test, with apostrophe-anchor and nested-wrapper fixes. Product publication
and learning-persistence checks remain open.

Follow-up: [separate correction conversion and deduplication](typo-correction-filters.md)
verifies traditional/emoji display and registry commits while preserving the
compared ordinary menus. Internal conversion budgets and production publication
remain open.
