# Why abbreviation recovery still misses targets

Date: 2026-11-04 · Offline diagnostic, no production changes

The [constrained-search run](typo-constrained-search.md) has three separate
limitations. More search work alone cannot fix its coverage or candidate-quota
losses. This audit changes neither search nor model parameters and does not
rerun inference. It examines the same 830 labeled synthetic examples, not a
fresh accuracy test.

| Outcome | Examples |
|---|---:|
| Target recovered | 342 |
| Labeled word/reading absent from explicit lexicon | 153 |
| Retained labeled target, but query exhausts its budget | 153 |
| Completed search; four same-category proposals strictly outscore labeled path | 181 |
| Completed search; boundary tie, manually checked | 1 |

These categories are ordered: recovered first, then absent reading, terminal
quota, exhaustion, and completed-search misses. Absence can overlap exhaustion;
28 of the 181 exhausted examples also lack the labeled reading. No missed
example in this audit is excluded solely by the per-reading four-word terminal
quota. Alternate readings may still exist: “labeled reading absent” does not
mean the Chinese word is absent from every dictionary or that native Rime
cannot compose it.

The audit matches exact segmented readings and Chinese text against the original
887,781-pair TSV. It reproduces float32 log-frequency ordering for terminal rank.
For completed searches it compares the labeled path's clean/swap/neighbor cost
against the four returned candidates in its category. A strict score margin
avoids treating rounded output or ties as proven strict losses. This explains
why the labeled path cannot enter the quota; it is not an exhaustive proof
about every alternate pronunciation or alignment of that word.

The remaining tie is clean initials `rcw` → 阮崇武. Its reading `ruan chong wu`
and the fourth clean proposal 如传闻 (`ru chuan wen`) both have frequency 100.
The native comparator resolves equal score by UTF-8 text order, placing 如传闻
first. Thus all 182 completed misses have a quota explanation for their labeled
path; none is evidence here that a larger queue is needed.

## Consequence for the next experiment

Keep the protected ordinary first page and current operation/memory caps.
Separate the next ablations:

1. **Candidate quota:** compare the current four exact-abbreviation/four typo
   slots with an adaptive allocation of the same eight slots. Deduplicate against
   the ordinary menu before consuming recovery slots where bounded integration
   permits it. First measure the recoverable ceiling; reallocating slots can
   sacrifice other useful hypotheses and does not solve ambiguity automatically.
2. **Coverage:** inspect missing labeled readings, including erhua/segmentation
   variants, against native canonical retrieval. Do not add evaluation targets
   directly to the dictionary or infer pronunciation for unannotated phrases
   without a separately validated preparation rule.
3. **Exhaustion:** investigate the 153 retained-target failures separately. More
   memory or a higher operation cap is not an explanation for the other misses.

Freeze any choice on development families before a new held-out evaluation.
These repeatedly inspected test families are diagnostic only. A model trained
on this tiny set would not establish real-user typo accuracy. The audit supports
improving what reaches the candidate pool before training a joint reranker;
it does not rule out a joint retrieval/ranking model as a later comparison.

## Evidence and reproduction

[Grouped counts and input hashes](typo-failure-audit/summary.json) retain
split/form/error breakdowns. [Source and row hashes](typo-failure-audit/sha256.txt)
identify the diagnostic; raw row output stays under ignored `build/`.
The one tie remains conservatively classified as `completed_retained_target_missing`
in machine output, with the manual source check explained above.

```sh
CGO_ENABLED=0 go run ./spikes/typo-recovery -mode abbr-audit \
  -root build/typo-recovery/constrained-v1 \
  -source-run build/typo-recovery/abbr-recovery-v1
CGO_ENABLED=0 go test ./spikes/typo-recovery
CGO_ENABLED=0 go vet ./spikes/typo-recovery
```

The command creates a new `audit/` directory and refuses to overwrite it.
Tests cover strict quota loss, score ties, category separation and an unfilled
quota. Tests and vet passed. No runtime performance claim follows from this
analysis tool; no hot-path code changed.
