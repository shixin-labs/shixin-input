# Memory attribution and a rejected abbreviation restriction

Status: experiment, 2026-11-04. The retained native policy is still the broader
alternate-boundary search. The selective policy described below was reverted.

## Where the extra resident memory goes

`TestTypoNativeSpikeResidentMemory` now optionally runs macOS `vmmap -wide
-noCoalesce` before replay, after 1,500 compositions, and after Escape clears
the final composition. It only profiles its private scratch worker. Profiling
is outside key processing and must not be enabled for latency measurements:
`vmmap` can suspend the process. These observations are process snapshots, not
allocation counts, peak residency guarantees or pooled-session measurements.

The same workload includes `niaho`. The previous preferred-anchor policy was
temporarily restored and rebuilt for a control, then the retained policy was
restored. Rounded values below are read from the post-clear VM maps.

| Policy | Main dictionary resident | Resident MALLOC_SMALL | Physical footprint |
|---|---:|---:|---:|
| Ordinary | 11.0 MiB | 18.5 MiB | 29.3 MiB |
| Previous preferred anchor | 13.5 MiB | 22.1 MiB | 33.7 MiB |
| Broader alternate boundaries | 20.7 MiB | 22.2 MiB | 32.5 MiB |
| Rejected selective policy | 17.1 MiB | 21.9 MiB | 28.6 MiB |

The main dictionary is a roughly 57.9 MiB read-only mapped file with zero dirty
bytes in these snapshots. Broader boundary search touched substantially more of
it; the two prototype policies had similar resident small-allocation heaps.
This supports targeting dictionary access locality rather than assuming the
entire RSS increase is a growing candidate queue. It does not establish a
byte-exact attribution: allocator state, OS reclamation and mapping residency
vary across fresh processes. Physical footprint and RSS are different metrics;
neither should be substituted for the other. Profiling-run maximum sampled RSS
was 66,994,176 bytes for broad and 62,799,872 for selective. The earlier unprofiled
paired RSS measurements remain in [the boundary report](typo-alternate-boundaries.md).

## Rejected experiment

The selective second pass allowed full-syllable edits at all valid prefix
boundaries, but allowed abbreviation edits only at the preferred boundary. It
kept the same candidate/work/deadline limits. `niaho` recovery and canonical
learning passed, as did the focused native matrix in Release and ASan/UBSan.
Its memory reduction was promising, but those tests were too narrow to accept it.

A new opt-in `TestTypoCorrectionCorpusSpike` captures the actual separate native
stream through the purego bridge. It uses 731 existing public codes representing
830 reused synthetic development labels, in sorted code order, twice per code.
It records the ordinary first five and up to eight **full-span** corrections.
It creates a private warm session, makes no selections, and does no AI scoring.
Both policies use the same data, budgets and code order in fresh processes.

Target availability on the second trace is:

| Cohort | Labels | Broad | Selective |
|---|---:|---:|---:|
| All | 830 | 312 | 321 |
| Clean | 277 | 154 | 155 |
| Swap | 275 | 69 | 74 |
| Neighbor | 278 | 89 | 92 |
| Initials-only neighbor (subset) | 64 | 3 | 1 |

Despite its higher total, selective gained 15 labels and lost six. The losses
were `gtianoxia` → `瓜田李下`, `rlwz` → `儒林外史`, `baoiliy` → `保加利亚`,
`rvhw` → `阮崇武`, `oldyy` → `克罗地亚语`, and `shadver` → `傻大个儿`.
They span mixed, initials and digraph-initial input. The first traces show the
same six losses, with 16 gains instead of 15. This is enough to reject the
restriction as a no-regression memory fix, not enough to establish stable
population-level accuracy. Deadlines and warming still affect recovery.

All ordinary first-five text lists agreed between the two captures except the
native time/timestamp triggers `sj` and `ts`, whose values changed with the clock.
The clean-row difference in the table is an additional separate suggestion,
not an ordinary top-1 change. Availability means the target occurs in the union
of two areas; these are **not** top-1, fixed-menu-size accuracy or AI results.
The corpus is reused development data, not a held-out benchmark or real typo set.

## Retained state and next work

The restriction is saved as `spikes/typo-recovery/native-graph/selective-abbreviation.patch`
for reproduction only, to apply after the existing scratch patch and copied
files. It is not part of `librime.patch` or the retained `bounded_corrector.inc`.
After reverting it, both native libraries were rebuilt and the focused native
tests passed again in Release and ASan/UBSan. A reconstruction check confirmed
the scratch native sources exactly match the retained broader-policy patch.
Go vet passes for the new diagnostic tests. No new production option is enabled.

The memory problem remains open. Next experiments should reduce dictionary
pages visited through search ordering or shared dictionary-index locality while
retaining abbreviated alternatives; they must rerun the paired corpus, not
just the full-pinyin fixtures. Do not count this rejected restriction as a
completed memory fix or compensate by increasing caches/warming.

## Reproduction

With the prepared private scratch runtime:

```sh
SHIXIN_TYPO_SPIKE_VARIANT=protected SHIXIN_TYPO_SPIKE_ROOT="$PWD/build/typo-recovery" SHIXIN_TYPO_SPIKE_GENERATION="$PWD/build/typo-recovery/native-graph/separate-generation-v1" SHIXIN_TYPO_SPIKE_VMMAP_DIR="$PWD/build/typo-memory-profile" build/typo-recovery/bin/menu-capture.test -test.v -test.run '^TestTypoNativeSpikeResidentMemory$'
CGO_ENABLED=0 SHIXIN_TYPO_SPIKE_ROOT="$PWD/build/typo-recovery" SHIXIN_TYPO_CORPUS_OUTPUT="$PWD/build/typo-corpus-new.jsonl" go test -tags shixin_typo_spike ./internal/rime -run '^TestTypoCorrectionCorpusSpike$' -count=1 -v
```

The corpus output must not already exist. [Evidence](typo-memory-attribution/)
includes complete capture rows, compact evaluation labels, comparison counts,
test logs, and VM-map excerpts retaining dictionary mapping rows and the full
summary. Original full VM-map SHA-256 hashes are included in the excerpts.

Follow-up: [repacked full-table re-evaluation](typo-repacked-table.md#re-evaluation-with-separate-corrections-and-alternate-boundaries)
keeps the broad search and tested coverage, but does not reduce observed memory.

Follow-up: [search stop diagnostics](typo-search-diagnostics.md) locate a captured
miss inside chunk sorting, where comparisons read mapped entry weights.
