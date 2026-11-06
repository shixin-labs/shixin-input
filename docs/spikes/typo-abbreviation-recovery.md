# Bounded abbreviation-aware recovery: prototype and failed gates

Date: 2026-11-04 · Offline C++/Go experiment · Not implemented in the IME

**Result:** bounded syllable search recovers some abbreviated typos, but this
prototype is **not ready for product integration**. It exhausts its queue on
400/830 evaluated examples, does not improve clean initials recall, and AI with
a spelling penalty still displaces six clean test targets from page one. A
first-page guard prevents those losses by construction, with reduced access to
newly recovered candidates. No new neural model was trained or selected.

Design: [typo-tolerant pinyin](../typo-tolerant-pinyin.md).
Previous control: [abbreviation baseline](typo-abbreviation.md).
Code/reproduction: [recovery spike](../../spikes/typo-recovery/README.md#bounded-abbreviation-recovery-prototype).
Evidence: [preparation](typo-abbreviation-recovery/preparation.json),
[summary](typo-abbreviation-recovery/summary.json),
[completion/model manifest](typo-abbreviation-recovery/manifest.json),
[initials by length](typo-abbreviation-recovery/initials-lengths.json),
[paired clean results](typo-abbreviation-recovery/clean-paired.json),
[search memory/time](typo-abbreviation-recovery/search-stats.txt),
[final source and run binary hashes](typo-abbreviation-recovery/final-source-run-binary-sha256.txt).

## What changed in the experiment

The index contains 887,781 explicit reading/text pairs from the same public
rime-ice sources. Labels never enter it. Unannotated Tencent readings are not
inferred; only Han text and alphabetic segmented readings fitting the 32-byte
full-spelling bound are admitted. The trie stores **syllable edges once** and
recognizes full syllables, one-letter initials and supported `zh/ch/sh` initials
while searching. It does not materialize all abbreviation masks or typo forms.

A single shared edit budget permits one adjacent-key substitution or adjacent
swap in the typed form. A pending-character state handles swaps across two typed
syllable forms, including initials. This is a lexical search experiment, not the
raw-span graph/partial-selection implementation required by the design. Missing
letters, repeated letters, fuzzy paths and shuangpin are not added here.

Search uses a fixed best-first queue, dictionary-prior bounds, first-letter edge
ranges and remaining-length bounds. The last two eliminate impossible paths
before queueing. There are 2,048 expanded-state, 8,192 examined-edge, 1,024 queue
and 4,096 dedup-slot limits. Any overflow discards the entire proposal batch;
ordinary native translation remains available. Separate quotas admit **four
zero-edit and four typo text candidates**, so exact abbreviation paths cannot
consume every correction slot. Abbreviation itself incurs no edit penalty.

Within each quota, the fixed ordering is
`0.3 * log1p(source frequency) - 0.2 * edit_cost`, then text/code; swaps cost 8,
neighbor substitutions 10. These are diagnostic constants inherited from the
prior channel experiment, not calibrated human error probabilities. Four
highest-prior words per canonical reading suffice for a four-slot category;
this truncation also limits which homophones the model can ever see.

The run reuses the baseline's 830 examples / 731 unique raw inputs and evaluates
all their prefixes for search stress: **2,616 unique queries**. These are the
same synthetic dev/test families, not an untouched final benchmark. Development
iterations improved impossible-path pruning; no parameters were fitted to target
accuracy. The published results describe the final bounded prototype only.

## Native verification and ranking controls

The final searches propose 557 distinct canonical spellings on evaluated inputs.
91 have compatible prior captures; 466 were captured in four independent scratch
engines. Every proposal used in the evaluated rows passed whole-input acceptance
for its Chinese text under the canonical code. New captures inspect the first
20 candidates. Some cached captures contain 50, but no accepted proposal depended
on a rank beyond 20. This is not proof that selecting the original typo through
a future graph will preserve raw spans or learning.

The merge preserves the original five entries, adds up to eight unique verified
proposals, then fills from the ordinary menu to 50. Existing full-span candidates
retain zero error cost; duplicate hypotheses retain the minimum available cost.
The offline AI comparisons use the same 50-candidate ceiling with partial entries
frozen, approximate source-frequency priors, and the pinned shipped ERNIE model.
This is the previous **wide diagnostic** control, not its native-window arm or
an exact reproduction of native phrase weights and the 20-item mixed export.

Arms: ordinary/merged menus without AI; ordinary/merged menus with ERNIE; merged
ERNIE with the earlier fixed spelling penalty; and a guard preserving the
ordinary AI first page before admitting the augmented tail. All 1,062
condition/example rows completed. Useful/stale context remains the same small
matched subset; there is no new context selection or model training.

## Retrieval and ranking results

Test split; recovery is the intended text appearing in the bounded merged pool,
not merely an inferred spelling. The AI column uses empty context and the fixed
penalty. Denominators differ after the original duplicate/collision exclusions.

| Form and error | Original pool recall | Recovered pool recall | AI + penalty top-5 |
|---|---:|---:|---:|
| Full, neighboring key | 0/35 | 20/35 | 14/35 |
| Full, swap | 7/35 | 22/35 | 14/35 |
| Initials, neighboring key | 0/35 | 3/35 | 2/35 |
| Initials, swap | 0/33 | 0/33 | 0/33 |
| Mixed even, neighboring key | 0/34 | 4/34 | 3/34 |
| Mixed even, swap | 6/35 | 9/35 | 6/35 |
| Mixed odd, neighboring key | 0/35 | 11/35 | 5/35 |
| Mixed odd, swap | 7/35 | 17/35 | 10/35 |

Additional digraph-initial forms recover 1/13 neighboring-key targets and 0/13
swaps; neither slice gains a top-5 target. All three recovered initials-neighbor
targets have four or more letters. The 2–3-letter initials typo slices recover
none. Longer initials are not automatically safe/easy: 8/11 long neighbor cases
and all 11 long swap cases exhaust the queue.

For example, synthetic `bqfdxd` (intended `bqfdxx`) retrieves 不区分大小写 and
ranks it #1 with ERNIE plus the penalty; the guarded version places it #6.
Likewise `oldyy` (intended `kldyy`) retrieves 克罗地亚语 at #1, or #6 guarded.
These are public synthetic examples, not reported user typos.

Clean initials retrieval remains **16/34**, unchanged from the baseline. Clean
mixed-even stays 23/35; mixed-odd improves 22/35 → 25/35. This prototype therefore
does not resolve the clean abbreviation retrieval gap identified previously.

### Clean-page regressions persist

Across 152 clean test form examples with empty context, ordinary AI has 70
labeled targets in its first page; augmented AI with the penalty has 66. This
net difference hides **six paired losses and two gains**. Examples:

- `qes` → 秦二世: ordinary AI #5, augmented/penalized AI #11.
- `ruancwu` → 阮崇武: ordinary AI #1, augmented/penalized AI #6.
- `jiejjiez` → 戒骄戒躁: ordinary AI #1, augmented/penalized AI #6.

These are target-retention regressions against the same AI control, not a claim
that every other homophone is invalid. Multiple forms of a phrase are correlated;
the counts are not independent human compositions or a release-grade false
correction rate. The first-page guard records zero such losses by construction.
When the ordinary menu has fewer than five entries, added candidates can still
fill its remaining first-page positions; otherwise recovery starts on page two.

The guard is a **counterfactual diagnostic**, using two separately computed
orders offline. Do not implement it by adding a second model pass or hold. A
production protection policy must work within one existing hold, preserve native
protected candidates, and be evaluated with the actual native export/pruning.
A zero-loss count for this constructed guard does not certify a model.

## Performance and memory

On the 2,616 warmed prefix/final queries, C++ lookup had zero intercepted `new`
allocations, p50 **0.0495 ms**, p99 **0.1489 ms**, p99.9 **0.1881 ms**, and maximum
0.2267 ms. The fixed workspace is **45,064 bytes**. Results reproduced exactly
on a second run after excluding timing fields. These are standalone search
measurements, excluding native graph construction, capture, merge, IPC and AI.

**1,134/2,616 queries and 400/830 evaluation examples exhaust the queue.** Every
final exhaustion is at the 1,024-state queue bound; the observed maximum popped
states is 1,439 and examined edges 3,686. Fast bounded failure does not establish
useful coverage. The initial full-child scan and weaker bounds failed more
often; retaining the hard caps exposes the remaining broad-search problem.

There are 1,308,655 nodes and 1,308,654 edges, with 866,195 retained terminal
words. Major retained arrays/string capacities total about **77.5 MiB**, excluding
small containers/allocator overhead. Whole-process peak RSS during construction
and lookup is **272,072,704 bytes** (~259.5 MiB), including builder scratch space.
The smaller representation improves on the earlier dense-character prototype,
but is still an additional dictionary-scale index in this experiment. It is not
an approved product memory increase. Production should reuse the native dictionary
and spelling structures, or separately justify measured incremental memory.
No extra neural model was loaded.

Penalized scorer p99 is 21.73 ms for empty context, with 30/830 calls over 16 ms;
source/stale subsets have p99 13.70/13.96 ms and no scorer-only overruns. Prefill
is separate; these timings include trivial windows, use fixed arm order, and do
not enforce the live hold. A timed-out result must use the deterministic safe
menu, never an unsafe augmented order or a later menu update.

## Validation and decision

ASan/UBSan passes 10,000 single-word and 2,500 multi-word randomized comparisons
against an independent exhaustive abbreviation/edit reference, plus explicit
swap/neighbor, invalid-input, cap and top-K fixtures. Go tests check native
acceptance filtering, minimum edit cost and first-page preservation. The 42
experiment/data/capture/rerank tests and vet pass. No production input code,
installed IME, model weights or live user dictionary changed.

**Do not ship this prototype or select a replacement model from these results.**
The next search experiment should constrain syllable/edit paths before broad
lexical expansion and reuse native dictionary structures, keeping the current
operation/memory bounds. It must improve both clean initials coverage and typo
coverage without relying on a larger queue. Then export real prior/provenance
and prove raw-span partial selection and learning. Re-evaluate correction on/off
under the same single-hold ranking policy; clean-page retention and fallback
remain release-blocking requirements. The existing ERNIE stays the baseline.

Follow-up: [radix compression](typo-radix.md) reduces retained index storage by
10.31% while preserving all measured search behavior. Its memory saving does
not change the failed coverage or clean-page gates above.
