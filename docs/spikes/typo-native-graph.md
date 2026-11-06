# Native spelling-graph recovery: bounded selection, abbreviation gap

Current follow-up: [native initial cache](typo-initial-cache.md) replaces startup
scanning with offline reading buckets. Warm editing passes; cold recovery still
misses. The results below remain historical evidence for their named versions.

Date: 2026-11-04 · Scratch-only integration; **not ready to ship**

The standalone word index cannot reproduce native spelling aliases and sentence
composition. This experiment probes librime's existing prism and translates the
original raw input through a corrected graph in the same session. It does not
load the separate 51.93 MiB word index, replay alternative strings, or use a
second Rime session. This is a concrete integration prototype, not a measured
51.93 MiB reduction in total engine RSS: native graph allocation adds other costs.

Code and reproduction: [native-graph spike](../../spikes/typo-recovery/native-graph/README.md).
Design: [typo-tolerant pinyin](../typo-tolerant-pinyin.md).
[Source/library/schema hashes](typo-native-graph/sha256.txt) identify the latest
restored depth-first snapshot and the saved unbounded library; historical captures below identify
intermediate stages and must not be attributed to the latest work-capped build.

## Bugs found and fixed in the scratch integration

**Visible first-page protection must follow ordinary deduplication.** Counting
five raw translator entries protected only three visible candidates in one
fixture because duplicate entries disappeared later. Wrapping the ordinary
translation in its normal deduplicator before counting fixes this case.

**Identical text can consume different raw spans.** Ordinary 尝试错误 is a partial
candidate for `changshicuowj`, leaving `j`. The corrected 尝试错误 consumes the
whole input. Text-only deduplication hid the useful correction. A native
correction metadata flag now survives shadow/uniquified candidates, allowing
these distinct spans to remain separately selectable. The flag is not inferred
from user-visible comments. Native Phrase identity and the syllabifier remain
attached, rather than manufacturing a generic text candidate.

The [ordinary](typo-native-graph/ordinary-menus.jsonl) and
[protected-v2](typo-native-graph/protected-v2-menus.jsonl) captures preserve all
five first-page texts for all [12 smoke inputs](typo-native-graph/smoke-codes.jsonl).
These are AI-off fixtures with the existing warm profile; this does not prove
retention across every conversion/filter/pin setting. Corrected 你好 for `nihso`
is selectable at rank 7; corrected full-span 尝试错误 for `changshicuowj` at rank 6.
The capture tool actually selects candidates in private sessions to establish
whole-input acceptance.

**Longer corrected prefixes can consume the entire recovery pool.** A following
prototype limits the eight extras to two per consumed span while scanning at
most 64 entries. That makes corrected 你好 available for partial selection from
`nihsozhongguo`; the remaining preedit is exactly `你好zhong guo`, with no lost or
duplicated ASCII letters. Full-span selection, raw Backspace and literal Return
also pass in the [uncapped editing run](typo-native-graph/editing-v3-test.log).
This allocation is provisional: it can reduce whole-input abbreviation recovery
and has not passed a broad comparative evaluation.

## Latency exposed the next real blocker

The corrected generator itself is small: an eight-byte edit buffer, a fixed
keyboard table, at most 256 prism probes and 64 matched spellings per suffix.
It proposes swaps and neighboring-key substitutions without a queue or new
word index. But this does **not** bound all dictionary and sentence lookup
triggered by the resulting graph. Nor does an eight-candidate output limit.

Actual engine histogram measurements, run sequentially without concurrent
captures, use the same eight input traces over 20 measured repetitions after
warm-up: 1,340 key samples, including Escape. These are diagnostic traces, not
the full repository latency corpus. Quantiles are histogram upper bounds.

| Version | p50 | p99 | p99.9 / max | Result |
|---|---:|---:|---:|---|
| Ordinary control | 22 µs | 640 µs | 655 / 655 µs | Pass |
| Unbounded protected recovery | 256 µs | 130.622 ms | 130.622 / 130.622 ms | **Fail** |
| First shared-work-cap version | 88 µs | 960 µs | 1.024 / 1.104 ms | Latency passes; recovery fails |

Raw logs: [ordinary](typo-native-graph/ordinary-latency.log),
[unbounded](typo-native-graph/protected-latency.log),
[work-capped](typo-native-graph/bounded-latency.log).
A repeated [ordinary control with the budget hooks compiled in](typo-native-graph/bounded-ordinary-latency.log)
remains p50 22 µs / p99 640 µs, max 699 µs.

The work-capped graph shares 4,096 units across graph vertex/property visits,
table traversal, chunk collection and long-code matching. Table traversal also
rejects a second correction in its indexed prefix; candidate alignment checks
remain necessary for full-code/sentence correctness. Exhaustion drops recovery
and keeps the untouched ordinary translation. This bounds several previously
unbounded stages, not every possible native path or user-dictionary workload.

The cap loses the full `changshicuowj` recovery and the partial-selection target
([failing editing gate](typo-native-graph/bounded-editing.log)). It is therefore
**not a successful fix**, despite passing latency. A later necessary-condition
prune computes exact forward/backward prism reachability and rejects corrections
without an unchanged spellable prefix and suffix. It still
[fails those two cases](typo-native-graph/boundary-editing.log). The next iteration below has separate measurements; do not transfer the
first capped run's numbers to it.

## Canonical-aware position selection and avoiding redundant sentence work

A later iteration restores the original five editing fixtures. The ordinary
prism labels `hso` as a normal spelling alias for `shou`, so checking spelling
type alone does not locate the error in `nihso`. The prototype now compares
raw spans to canonical syllables and chooses the first noncanonical span along
a path minimizing noncanonical bytes. This is a heuristic, not a complete
search over all typo positions; all-canonical paths default to the first span.

The diagnostic [lookup trace](typo-native-graph/anchor-work-v2.log) shows that
native lookup already returns full-span 尝试错误. Subsequent sentence construction
was spending the remaining budget and discarding it. Recovery now retains a
full-span dictionary match without constructing additional sentences. Corrected
word prediction is also disabled; the ordinary stream is unchanged.

The shared limits are now 32,768 cheap work units, 2,048 table nodes, 2,048
collected chunks, and a cooperative 900 µs deadline checked every 64 work units.
This separates cheap visits from allocation-producing work; it does not prove
that every native operation respects the deadline or is allocation-free.

The [expanded editing gate](typo-native-graph/anchor-expanded-editing.log) passes
six of seven fixtures: full `nihso`, existing native swap alias `nihoa`, full
`changshicuowj`, raw Backspace, literal Return, and partial selection followed
by an exact `你好中国` commit. It **fails `njh → 你好`**. The swap fixture accepts
an ordinary candidate and verifies whole-input consumption; it does not falsely
require new correction metadata on a candidate already handled by librime.

The same build's expanded timing trace includes two PageDown keys and Backspace
per input, for 1,820 measured keys after warm-up:

| Variant | p50 | p99 | p99.9 | Max |
|---|---:|---:|---:|---:|
| [Ordinary](typo-native-graph/anchor-ordinary-latency.log) | 20 µs | 640 µs | 765 µs | 765 µs |
| [Protected](typo-native-graph/anchor-paging-latency.log) | 52 µs | 1.792 ms | 2.304 ms | 3.016 ms |

These pass the diagnostic latency gate, not the full repository performance gate.
Trying all correction positions on short input still failed abbreviation recovery;
a relaxed read-only diagnostic consumed 171,784 lookup work units for `njh`
([trace](typo-native-graph/abbr-work.log)). This relaxation is diagnostic only;
it is never enabled in the engine. Increasing the full-span candidate quota
from two to four also failed three repeated editing runs, so that change was
reverted. Depending on budget completion, the two-candidate quota can hide
你好 behind 你会/你回, or recovery can be discarded entirely. Both search cost
and candidate allocation need evaluation; a larger quota alone is insufficient.

## Sparse dictionary intersection

The next lookup change keeps the same correction paths and output limits.
At native dictionary levels one and two, it chooses the smaller of the actual
dictionary child set and the expanded spelling set. Sparse nodes probe the
graph only for dictionary children that exist, instead of testing every expanded
syllable against that node. Root and tail handling are unchanged. This uses
existing mapped dictionary nodes and adds no duplicate index or candidate queue.

A [complete-entry comparison](typo-native-graph/intersection-verification.log)
checks text, consumed span, canonical code, weight and duplicate multiplicity
against the original traversal on the same corrected graph, with diagnostic
budgets relaxed on both sides. All four fixtures match: `njh` (4,830 entries),
`nihso` (36), `changshicuowj` (1,593), and `nihsozhongguo` (39). These are four
fixtures, not a proof for every graph. In the all-position `njh` diagnostic,
root lookup falls from 171,784 to 38,828 counted work visits; node/chunk counts
remain 1,551/1,927. The engine still uses one anchor and the original bounded
limits. Relaxed diagnostic timing is not engine timing.

The [same-build expanded latency trace](typo-native-graph/intersection-latency.log)
passes: p50 52 µs, p99 1.792 ms, p99.9 2.816 ms, maximum 5.594 ms across 1,820
keys. This sample does not demonstrate a latency improvement over the prior
version. The [native suites](typo-native-graph/intersection-native-tests.log) pass.
The [editing gate](typo-native-graph/intersection-final-editing.log)
still fails abbreviation recovery. A separate attempt to require a correction
in initial dictionary paths also failed and was reverted; it must not silently
remove unchanged word pieces needed for sentence composition.

Next investigate allocation-producing node/chunk work and how to retain useful
full-span candidates under the shared budget. Increasing the visit cap or the
candidate quota alone is not validated as a fix. Clean-menu, native learning,
AI-enabled integration and broader performance gates remain open.

## Fixed-size traversal bookkeeping and deadline isolation

A read-only, warmed `njh` lookup diagnostic (anchor zero, 50 samples, relaxed
budgets only in the diagnostic) measured median table traversal at 533 µs out
of 586 µs for complete dictionary lookup. Each queued native TableQuery copied
three vectors of accumulated credibility, quality and raw positions, although
the index depth cannot exceed three. These are now fixed-size arrays, and
removing a query from the queue moves its remaining code vector instead of
copying it. The queue and other native allocations still exist. This change
also affects ordinary traversal in the scratch library; no allocation-free
claim follows from it.

The same diagnostic after this change measures 461 µs for table traversal and
524 µs for dictionary lookup. Both measurements include result destruction;
they exclude graph construction and are not per-key latency measurements.
See [before](typo-native-graph/query-baseline-timing.log) and
[after](typo-native-graph/fixed-state-timing.log).

The [native suites](typo-native-graph/fixed-state-native-tests.log) and
[four complete-entry comparisons](typo-native-graph/fixed-state-equivalence.log)
pass. The comparison exercises both traversals with the new bookkeeping; it
is not an independent oracle for every historical weight calculation.
The 1,820-key [protected trace](typo-native-graph/fixed-state-latency.log) passes
at p50 52 µs, p99 1.152 ms, p99.9 2.048 ms, maximum 2.286 ms. The corresponding
[ordinary control](typo-native-graph/fixed-state-ordinary-latency.log) also passes.
These are diagnostic samples, not full release performance validation.

The bounded [editing gate](typo-native-graph/fixed-state-editing.log) still fails
`njh → 你好`. Increasing the internal deadline to 1,200 µs and full-span quota
to four [also fails](typo-native-graph/fixed-quota-editing.log). A controlled
20 ms deadline with four slots [passes all seven fixtures](typo-native-graph/deadline-diagnostic-editing.log),
confirming that the target can be selected when time and output room permit.
That is **not** an acceptable shipping configuration or a latency success.
The scratch source has restored the 900 µs deadline and two-per-span quota.

The next experiment should prioritize specific spelling repairs (`nj → ni`
followed by the ordinary `h` abbreviation) before expanding broad corrected
initials. It must retain a bounded second search for initial-only errors,
share one composition-wide work/deadline/output budget, and compare coverage
against the existing search. Simply disabling abbreviation corrections would
change the requested feature and is not an acceptable fix.

## Specific repairs before broad initials

The latest experiment separates recovery into two native translations over the
same raw input and session. First, corrected edges must resolve to normal
spellings; unchanged abbreviations remain legal, so `nj → ni` plus the unchanged
`h` abbreviation can recover 你好. Second, the existing broader corrected-initial
search runs only if budget remains. It inherits the first stage's remaining work,
node and chunk counts and its original deadline. Failure of the second stage
no longer discards a completed first-stage recovery.

The combined stream protects the ordinary page, scans at most 64 entries
(up to 32 from each stage), and offers at most eight extras. It allows four
full-input extras and two per shorter span. This quota is provisional: a specific
stage can consume all four full-span slots before broader candidates are offered.
Both native translation objects retain their spelling graphs for selection;
this adds retained graph state and has not passed memory/allocation gates.

The original seven editing fixtures, including `njh → 你好`, now
[pass](typo-native-graph/staged-editing.log), also in
[three repeated runs](typo-native-graph/staged-editing-repeat.log), with the
900 µs search deadline unchanged. The same build's
[1,820-key latency sample](typo-native-graph/staged-latency.log) passes at p50
60 µs, p99 1.152 ms, p99.9 1.28 ms, maximum 2.401 ms. This is the first bounded
version to pass those seven fixtures and that diagnostic latency trace together.

A [budget and graph test](typo-native-graph/staged-budget-test.log) checks that
the second stage retains abbreviation correction edges, inherits counters,
spends the remaining work, and does not restart an expired deadline. It also
checks that the specific stage excludes corrected abbreviation edges while
retaining normal correction edges.

A new pure-initial fixture, `xgr → zgr → 中国人`,
[still fails](typo-native-graph/staged-initial-editing.log). The first stage fills
the full-span quota with repairs such as 虚弱 and 虚荣. Search exhaustion and
cross-stage candidate allocation remain concerns; do not interpret seven passing
fixtures as broad abbreviation support. The expanded editing gate is still red.
The paired native corpus capture below is complete; it measures this stage
before the subsequent merge and trigger fixes.

## Paired native corpus results and symbol-trigger fix

The frozen 830-example corpus has 731 distinct raw codes. Both captures finished
using the same library, with AI and learning off, five-candidate pages and a
13-candidate window. The capture tool verifies whole-input commits by selection.
[Ordinary menus](typo-native-graph/staged-evaluation/ordinary.jsonl),
[protected menus](typo-native-graph/staged-evaluation/protected.jsonl),
[summary](typo-native-graph/staged-evaluation/summary.json), and
[audit](typo-native-graph/staged-evaluation/audit.json) preserve the evidence.
The [library hash](typo-native-graph/staged-evaluation/library.sha256) identifies
this pre-fix staged build, not the current scratch binary.

| Metric | Ordinary | Protected |
|---|---:|---:|
| Verified target in first 5, all 830 rows | 188 | 198 |
| Verified target in first 13, all 830 rows | 225 | 275 |
| Clean target in first 13, 277 rows | 182 | 178 |
| Typo target in first 13, 553 rows | 43 | 97 |

There are 57 gains and seven losses in the 13-candidate window: six losses are
clean inputs and one is a swap. Inserting extras after page one can displace
later ordinary targets beyond this window; first-page protection does not mean
all clean ranks are preserved. No real-user accuracy claim follows from these
synthetic dictionary examples. Initial-only typo recall remains particularly
poor: just one adjacent-key example is recovered across the initial cohorts;
the digraph-initial and initial-swap cohorts have no target recovery here.

Three rows differ in the ordinary-prefix comparison. `sj` and `ts` invoke live
time/timestamp translators, so captures at different instants are not a valid
text-equality control. `vlz` is a real regression: the native symbols ʫ and ɮ
were replaced by corrections 绿洲 and 驴子. The current scratch translator now
bypasses correction for recognized native/special tags and configured date
trigger codes. Those codes are cached at initialization; there is no per-key
configuration read. A new [engine fixture](typo-native-graph/native-trigger-editing.log)
asserts the exact `vlz` menu and selects ʫ successfully. The other seven original
fixtures also pass; `xgr → 中国人` still fails. The entire 731-code capture has
not been repeated after this fix, so broad clean-prefix retention remains unproven.

An independent [merge test](typo-native-graph/merge-before-test.log) reproduced
another bug: four specific full-span candidates blocked all broad full-span
candidates. The current merger reserves two of eight slots for a present broad
stage, permits at most four full-span entries per stage and six in total, and
retains the two-per-shorter-span limit. Four output-policy tests pass
[ASan/UBSan](typo-native-graph/merge-after-asan.log), including bounded duplicate
scanning and same-text/different-span retention. This instruments the merger test,
not the entire librime binary. The real initial-only fixture still fails after
the quota fix, so search completion is a separate blocker.

The latest [engine timing sample](typo-native-graph/native-trigger-latency.log)
passes at p50 60 µs, p99 1.152 ms, p99.9 1.28 ms, maximum 2.324 ms (1,820 keys).
The [native suites](typo-native-graph/native-trigger-ctest.log) pass. Next work
must reduce broad search cost and evaluate whether valid completed candidates
can survive budget exhaustion without exposing incomplete graph state; it must
also assess the observed clean later-page losses.

Reproduce corpus evaluation with:

```sh
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode native-evaluate \
  -root build/typo-recovery/native-graph/staged-evaluation \
  -source-run build/typo-recovery/constrained-v1
```

The evaluator rejects missing/duplicate/incomplete captures, counts only verified
whole-input targets, and compares text, comments, rank and verified span in the
ordinary prefix. It reports time-dependent differences rather than hiding them.

## Retaining completed entries with depth-first recovery

The next change publishes completed native dictionary chunks as traversal finds
them, avoiding the intermediate table-accessor collection for corrected lookup.
Exhaustion stops traversal but does not discard already completed chunks. A new
explicit graph-completion flag rejects dictionary lookup on an unfinished graph;
ScriptTranslation also rejects graph-build exhaustion before dictionary lookup.
When root lookup exhausts, it skips additional user-dictionary/sentence work and
retains only materialized entries. Native candidate alignment still uses the
completed graph. This is incomplete *search coverage*, not a partially formed
candidate or an asynchronously changing menu.

Incremental collection alone [does not fix the initial fixture](typo-native-graph/streaming-editing.log).
Corrected table traversal now uses depth-first order, completing phrase branches
before visiting all sibling prefixes. Ordinary traversal retains breadth-first
order. This adds neither a word index nor a model, but still uses native dynamic
containers and is not an allocation-free production implementation.

The [invariant tests](typo-native-graph/streaming-invariants.log) compare complete
entry multisets against ordinary exhaustive traversal for four graphs. Another
test exhausts lookup on `xgr` at five work limits: 0, 93, 685, 2,885 and 3,116
entries survive out of 5,431 exhaustive entries. Every partial multiset is a
subset of the exhaustive result, including text, span, weight, canonical code
and multiplicity. An incomplete graph yields no entries. Shared-stage budgets
and abbreviation-edge preservation also pass. These finite fixtures do not
replace broader fuzzing and full-library sanitizer validation.

The original nine editing fixtures, including `xgr → 中国人`,
[pass](typo-native-graph/depth-first-editing.log), also in
[three repeated runs](typo-native-graph/depth-first-editing-repeat.log).
The timing trace now includes `xgr`, increasing it to 1,960 measured keys:

| Variant | p50 | p99 | p99.9 | Maximum |
|---|---:|---:|---:|---:|
| [Ordinary](typo-native-graph/depth-first-ordinary-latency.log) | 20 µs | 448 µs | 573 µs | 573 µs |
| [Protected](typo-native-graph/depth-first-latency.log) | 60 µs | 1.152 ms | 1.664 ms | 5.311 ms |

Both diagnostic latency gates and the [native suites](typo-native-graph/depth-first-ctest.log)
pass. The paired 731-code capture on this build is now complete; its results
and the newly failing tenth fixture are recorded below. This is a new capture
of the reused corpus, not a fresh holdout. Earlier corpus results must not be
attributed to this build.

### Process memory sample

An opt-in macOS diagnostic types 1,350 compositions over nine codes, reusing one
scratch context, and samples the engine PID's RSS at startup and after each
135-composition batch. It samples while a composition remains active and includes
native allocations and mapped pages; it is separate from latency measurement.
The [ordinary trace](typo-native-graph/depth-first-ordinary-rss.log) reaches a
sampled maximum of 50.27 MiB and ends there. The
[protected trace](typo-native-graph/depth-first-protected-rss.log) reaches 55.02 MiB
and ends at 49.08 MiB, with a drop during the run. Different GC/allocator and
mapping behavior prevents treating the lower endpoint as negative overhead.
Neither trace shows sustained growth in this short repeated-input experiment.

These are sampled RSS values, not transient peaks, per-key allocation counts,
private dirty memory or a bound across the session pool and full dictionary.
They do not prove the proposed memory target. No separate word index or neural
model is loaded for base correction. Reproduce with the same scratch runtime:

```sh
SHIXIN_TYPO_SPIKE_ROOT="$PWD/build/typo-recovery" \
  SHIXIN_TYPO_SPIKE_VARIANT=protected CGO_ENABLED=0 \
  go test -v ./tools/menu-capture -run '^TestTypoNativeSpikeResidentMemory$' -count=1
```

Run `ordinary` separately as the control; do not run these concurrently with
latency benchmarks.

## Verification and remaining requirements

Pure prefix enumeration and exact-boundary tests pass ASan/UBSan. Prefix tests
intercept `new` and observe zero allocations; this claim does not cover native
graphs, the legacy correction map, translated candidates or both translation
objects. The boundary oracle covers 10,000 generated one-edit middle syllables,
apostrophes and length bounds. Exact suffix reachability currently postpones
correction when the remaining input is only an unfinished spelling.

The first boundary patch accidentally changed the legacy corrector's multi-error
behavior. The new policy is now selected only through an explicit spike
capability/configuration flag. The [native CTest run](typo-native-graph/isolated-native-tests.log)
then passes both suites, including all 106 enabled librime tests and the Shixin
suite. The later [native CTest run](typo-native-graph/anchor-native-tests.log) also
passes both suites. The expanded opt-in editing test continues to expose the
abbreviation failure. Full-library sanitizer results are recorded below;
learning-persistence and AI-enabled integration gates remain missing.

The production engine, settings compiler, client IPC and installed IME are
unchanged. The librime submodule is clean at its pinned commit; changes are a
reviewable patch applied only to the ignored scratch build. All captures use
warm schemas with user dictionaries disabled.

Next work must make useful native correction paths fit the budget, rather than
relaxing the latency gate or claiming fast fallback as recovery. Prioritize
correction positions/spans and avoid rebuilding irrelevant dictionary branches;
then require the expanded editing and latency gates on the same build. Abbreviation
coverage, cross-boundary swaps, missing letters, fresh data, AI export protection,
allocation accounting and native learning remain open. No new model is selected.


## Completed depth-first corpus and later-initial counterexample

The [paired capture summary](typo-native-graph/depth-first-evaluation/summary.json)
contains 830 labeled examples and 731 unique codes, with the same library for the
ordinary and protected runs. Its [library identity](typo-native-graph/depth-first-evaluation/library.sha256)
belongs to the depth-first capture, not the subsequent rejected experiments.
Raw captures and input hashes are retained beside the summary. AI and learning
are off. Repeated use of this corpus for tuning means its original split labels
must not be described as a fresh blind test.

| Cohort | Examples | Ordinary top 13 | Protected top 13 | Gains / losses |
|---|---:|---:|---:|---:|
| Clean | 277 | 182 | 173 | 2 / 11 |
| Synthetic typos | 553 | 43 | 101 | 60 / 2 |
| All | 830 | 225 | 274 | 62 / 13 |

Clean top-five target recall is unchanged at 153/277. The prefix audit finds two
changes, `sj` and `ts`, whose live clock/timestamp outputs change between runs.
The previously displaced `vlz` symbol prefix is retained. This establishes no
observed static first-page change in this capture, **not** zero clean regression:
eleven clean targets fall out of the thirteen-candidate window. Full-pinyin rows
improve from 68 to 106 top-13 hits; initials fall from 24 to 22, digraph initials
stay at 14, mixed-even falls from 62 to 57, and mixed-odd rises from 57 to 75.
These form totals include both clean and corrupted rows.

A positional audit of the 64 initial-neighbor examples finds one recovery among
20 first-position errors and none among 44 later-position errors. The intended
clean code already retrieves the target within thirteen candidates in 7 and 17
of those rows, respectively. This separates spelling-search failure from missing
readings for that subset. Initial-swap recovery is also still zero.

The new engine fixture `zhr → 中国人` illustrates the later-position problem:
`h` is adjacent to intended `g`, while both `zh + r` and `z + h + r` are valid
abbreviated analyses. The fixture searches ten pages and requires a full-span
native correction. It fails; the original nine editing fixtures still pass on
the restored depth-first implementation. Do not report the expanded suite green.

The following scratch alternatives were tested under the unchanged 900 µs
cooperative deadline and original work/output caps:

| Trial | Result |
|---|---|
| Broad corrections at all positions | Still misses `zhr` |
| Disjoint specific/broad paths at the first anchor | Still misses `zhr` |
| Skip unchanged short entries in recovery lookup | Still misses `zhr` |
| Visit unchanged prefixes first | Misses `zhr`; regresses `xgr` |
| Prefer shorter consumed spans; abbreviation-only second stage | Still misses `zhr` |
| Use mapped prefix-entry weights to order depth-first siblings | Misses `zhr`; regresses `xgr` |
| Width-32 beam per indexed syllable depth | Misses `zhr`; regresses `xgr` |

These are sequential diagnostic trials, not controlled corpus ablations. Their
[editing logs](typo-native-graph/beam-editing.log) and intermediate logs are saved.
The final [rejected beam snapshot](../../spikes/typo-recovery/native-graph/rejected-beam/README.md)
is reproducible separately; it is not the default scratch patch. Its width bound
applies after pruning and does not establish a fixed peak workspace size.

With abbreviation-only corrections, relaxed diagnostic time and unchanged
operation caps, the [read-only lookup](typo-native-graph/abbreviated-stage-diagnostic.log)
finds 中国人 at rank one. This is evidence that the dictionary can supply it;
it is **not** an engine latency pass. The native deadline still prevents useful
retrieval. Prefix-entry weight is only a search heuristic, not an admissible
bound on descendant phrases. A better experiment must allocate search across
repair positions and segmentations, preserve ordinary visible candidates, and
measure coverage on a fresh corpus before adopting a new ordering or model.

## Full-library sanitizer checkpoint

Both native suites pass the [ASan/UBSan CTest run](typo-native-graph/asan-ctest.log).
The [nine-fixture functional editing run](typo-native-graph/asan-functional-editing.log)
also passes with the instrumented library preloaded in the scratch engine.
Sanitizer overhead made the 900 µs retrieval deadline miss `xgr`; therefore this
functional run explicitly uses `SHIXIN_SPIKE_SANITIZER_TEST`, which requires ASan
and gives the initial correction graph a one-minute clock allowance. Deterministic
work/node/chunk caps remain unchanged, and a carried deadline is never reset.
Release timing tests retain 900 µs. These sanitizer results establish neither
performance nor `zhr` recovery. Leak detection is disabled, as in repository CI;
this is not a leak-check result. The macro and preload setup are retained for
reproduction, and cannot silently apply to the Release build.


The restored-build [editing run](typo-native-graph/restored-editing.log) passes
nine of ten cases and fails only `zhr`. Its expanded ten-input
[latency trace](typo-native-graph/restored-latency.log) measures 2,100 engine key
samples: p50 60 µs, p99 1.28 ms, p99.9 1.664 ms, maximum 1.701 ms. This passes
the sampled engine limits but does not turn the `zhr` fallback into recovery.
Both [native CTest suites](typo-native-graph/restored-ctest.log) pass. The three
Go spike/data/capture packages pass 24 default tests; native opt-in results must
be reported separately, including the known editing failure.


The restored [read-only graph tests](typo-native-graph/restored-prism-tests.log)
also pass: exhaustive candidate-multiset equivalence, shared-stage counters and
deadlines, and validity of retained chunks under five work limits. Both saved
patches pass `git apply --check` against the pinned clean librime submodule.


## Follow-up: measured subtree bounds

Round-robin correction-position scheduling also failed the expanded editing
fixture and regressed `xgr`; that alternative was rejected and the depth-first
patch restored. A separate [subtree-bound search](typo-subtree-bounds.md) now
recovers both cases in isolation and matches exhaustive top-eight results across
all 17,576 three-letter codes with one neighboring substitution. Native `zhr`
selection remains unimplemented. The report separates its fixed workspace and
metadata costs from the unresolved process/RSS and full key-path budgets.
