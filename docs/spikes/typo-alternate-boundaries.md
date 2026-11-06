# Recovery beyond the guessed error boundary

Status: scratch correction change; not enabled in the product; 2026-11-04.

The previous learning experiment could not recover `niaho` → `你好`. The intended
reading is `ni hao`; the raw `ah` at byte span `[2,4)` is a within-syllable swap.
Both native correction stages were restricted to the same preferred error
boundary derived from the ordinary syllable graph. An alternative segmentation
can make that guess wrong. Repeating it in the second stage prevents recovery
at other valid prefix boundaries.

The experimental second stage now permits all exact-prefix boundaries. The first
stage still tries the preferred anchor. Both stages still share the original
composition work budget and 900 µs Release deadline; this adds neither a third
pass nor a larger deadline. Candidate quotas, one-edit trace validation and
ordinary-menu separation remain in place. The change is in the scratch
`librime.patch`, in `ScriptTranslator::Query`; the maintained submodule is clean.

## Verification

- The native matrix adds `niaho` to the existing inputs: 10 inputs × two traces ×
  four traditional/emoji combinations, 80 traces total. It compares up to 100
  ordinary candidates (text, comments and raw spans) with correction disabled,
  before and after consuming the separate stream. These comparisons pass.
- Warm `niaho` recovery is asserted to carry `Swap`, span `[2,4)`, bytes `ah` →
  `ha`, heuristic cost 8, and native phrase `你好`. Explicit selection commits
  `你好` or its configured emoji form. Token reuse remains rejected.
- The writable-dictionary test now includes a genuine new swap recovery case,
  separate from the existing ordinary `nihoa` spelling alias. Eight selections
  persist exactly `你好 / ni hao / 8` in a fresh-process export. Neighbor, emoji,
  native-alias and unselected controls still pass.
- Focused native tests and both CTest suites pass in Release and ASan/UBSan.
  The five learning cases also pass in both builds; Go vet passes.
- An expanded engine replay including `niaho` records 2,280 keys: p50 80 µs,
  p99 1.28 ms, p99.9 1.536 ms, maximum 1.838 ms. This passes the existing engine
  timing gates on this machine. It includes separate-stream generation but not
  configured worker publication, client selection transport or AI.

One initial Release native run returned zero `niaho` corrections on both traces;
a standalone rerun and the subsequent complete matrix passed. No exhaustion
reason was captured in that failed run, so a deadline explanation is plausible
but not proven. This is not a guarantee of cold or per-trace recall. The tests
retain their warm recovery assertions; they were not changed to accept missing
results. ASan uses the existing extended functional deadline, not the Release
deadline, and supplies no timing evidence.

## Memory tradeoff: still unresolved

The same 1,500-composition, one-context RSS workload now includes `niaho`.
Measurements are sampled process RSS, not peak allocation or a pooled-session
bound. They include all existing prototype components.

| Configuration | Maximum sampled RSS |
|---|---:|
| Ordinary input | 53,002,240 bytes (50.55 MiB) |
| Previous preferred-anchor policy, same workload | 62,652,416 bytes (59.75 MiB) |
| Alternate-boundary second stage | 67,010,560 bytes (63.91 MiB) |

For the policy control, only the second-stage anchor line was restored, the
Release library rebuilt, and the identical workload run. The changed policy was
then restored and rebuilt; learning and the native matrix were rechecked.
This run attributes about 4.16 MiB additional sampled RSS to the broader policy,
with normal process/startup variation still possible. The complete prototype is
about 13.36 MiB above ordinary input on this workload. Earlier smaller-workload
RSS figures must not be substituted for these results.

The recovery gain therefore is not a completed resource fix. Keep this policy
experimental while measuring dictionary pages versus retained search objects and
evaluating bounded alternate-boundary ordering. Do not increase warming, caches,
search limits or AI holds to hide the tradeoff. Missing/extra letters, broader
abbreviation recall, OpenCC internal expansion, full allocation gates and
production worker/client integration also remain open.

[Logs and source hashes](typo-alternate-boundaries/). Reproduction uses the native
matrix described in [the bridge report](typo-native-bridge.md), the extended
`TestTypoLearningPersistenceSpike`, and `TestTypoNativeSpikeLatency` /
`TestTypoNativeSpikeResidentMemory` with the separate scratch generation.

Follow-up: [VM attribution and the rejected abbreviation restriction](typo-memory-attribution.md)
measures resident dictionary pages and evaluates 830 labels using actual separate
correction snapshots. The restriction was reverted because of abbreviation losses.
