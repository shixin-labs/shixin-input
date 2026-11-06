# Typo-tolerant pinyin: first-version decision for review

Status: [experimental settings option](typo-experimental-option.md) implemented
locally, off by default, not released.
This document consolidates the experiments. The broader goal remains paused;
the separately requested [ten-per-spelling experiment](spikes/typo-hypothesis-caps.md)
and [authorized native implementation](spikes/typo-hypothesis-native.md) are now
complete. They do not resume the broader implementation goal.

Latest retrieval recommendation: cap each corrected spelling at ten native entries,
evaluate eight spellings for full pinyin and up to sixteen for abbreviated/mixed
input under one shared budget. This replaces the earlier eight-total retrieval
assumption. Display and AI export limits remain separate. Native sampled latency
passes; the user accepted the measured 12.9 MiB incremental sampled RSS.

## Recommended approach

Use bounded native dictionary recovery for within-syllable swaps and adjacent-key
substitutions. Keep the ordinary first page and raw-input commit behavior unchanged. Per the
user’s final choice, insert labelled corrections after that page with normal
paging, numeric shortcuts and clicks. Later ordinary candidates move back;
short ordinary menus suppress corrections. Never silently rewrite input or
rearrange a displayed menu. Discard incomplete correction work on exhaustion and use ordinary input
for that update. Do not permanently disable the feature after one exhausted search.

Start with full pinyin. Preserve clean initials and mixed input as intentional
spellings, not deletion errors. Do not remove existing abbreviation recovery to
meet memory targets; test abbreviated and mixed input separately. Missing/extra
letters, cross-syllable swaps and shuangpin typo recovery are later scope, not
promised first-version behavior.

**No new model initially.** Use a small weighted error channel for recovery and
reuse ERNIE-4.5-0.3B-Base as the optional contextual ranking baseline. It can rank
recovered phrases but cannot retrieve phrases absent from the candidate pool.
A joint typo/ranking neural model remains an ablation candidate, not a prerequisite.
Base correction must work without a model download. Any AI ordering must fit the
existing hold and must not change the ordinary list or a menu already displayed.

## What the evidence supports

| Finding | Evidence and limit |
|---|---|
| Public data preparation is usable for experiments | [153,421 synthetic examples](spikes/typo-data-preparation.md); no verified human raw-pinyin typo corpus acquired, no model trained. |
| Native correction selection and learning work in fixtures | `nihso` / `niaho` → `你好`; [80 native traces](spikes/typo-alternate-boundaries.md) compare up to 100 ordinary candidates. [Learning](spikes/typo-learning-persistence.md) survives a fresh-process export; cancelling suggestions learns nothing. Not production client integration. |
| Context helps but is not a clean-input safeguard | [ERNIE retest](spikes/typo-rerank.md) improved useful-context results; empty/stale context still regressed. Separate correction presentation is still required. |
| Total engine latency passes the sampled replay | [2,280 keys](spikes/typo-search-diagnostics.md): p50 72 µs, p99 1.28 ms, p99.9 1.536 ms; maximum 7.799 ms. Correction-only cost and full native allocation bounds remain unproven. |
| Latest native policy improves overall coverage | [Paired 830-label traces](spikes/typo-hypothesis-native.md): 313 → 341 and 313 → 337, with 15/17 individual losses. Ordinary pages match apart from clock values. Full-pool offline recall is not the eight-entry native display result. |
| Current memory cost is accepted | The user accepted [12.9 MiB additional sampled RSS](spikes/typo-hypothesis-native.md) for the measured workload. This replaces the proposed 8 MiB target as a blocker. First-use/deadline misses and broader coverage remain review items. |

The 830-label development corpus has been reused repeatedly; it is not a fresh
acceptance set. Separate-suggestion target availability is not top-1 accuracy.
Sanitizer functional runs use an extended deadline and prove no Release timing claim.

Rejected directions: inserting corrections into ordinary menus, restricting
abbreviation search to save memory, truncating the dictionary, and adopting
table repacking or additional warming without demonstrated benefit. The latest
head-weight sidecar experiment builds, but its runtime integration is unvalidated
and is not included in the retained reproduction patch. Do not treat it as selected.

## One proposed implementation milestone

Deliver an **opt-in macOS VM vertical slice**: type a typo, see a separate
suggestion, select it, preserve the remaining raw input, and persist canonical
learning. Include a settings toggle, explicit keyboard and click selection,
worker/session freshness, and safe fallback for clients without the extension.
Choose and review the keyboard interaction before coding it; existing shortcuts
must retain their behavior. Windows shipping support is a later milestone, but
shared protocol changes must remain versioned and compatible with old clients.

These are proposed review targets, not approvals or claims of completion:

| Gate | Required evidence before enabling the slice |
|---|---|
| Clean input | Zero ordinary-list/order/span/selection changes in paired clean full, initials and mixed traces, including paging, pins, custom phrases and native triggers; normalize only known clock values. |
| Recovery | Freeze a new phrase/source-disjoint set before tuning. Proposed target: ≥80% full-span target availability for dictionary-supported single-edit full-pinyin cases, reported separately for swaps and neighbors. Evaluate the revised ten-per-spelling retrieval cap separately from the still-undecided display limit. Publish cold/warm misses and all abbreviation losses; synthetic results do not substitute for volunteered real examples. |
| Latency | Existing total p50 ≤0.3 ms, p99 ≤2 ms, p99.9 ≤8 ms; incremental correction p99 ≤0.25 ms, p99.9 ≤1 ms. Test AI off/on; no extra hold or retry of the same update. |
| Memory/allocations | User accepted the current measured 12.9 MiB incremental sampled RSS; do not continue optimizing toward the earlier 8 MiB proposal. Record session-pool and peak/startup memory separately. New workspace target ≤256 KiB/session; zero Go/client key allocations. The new planner allocates zero heap memory per query, but native graph/candidate allocation compliance remains unproven. |
| Selection/lifecycle | Exact VM commits; partial selection, Backspace, conversion, learning/forgetting across restart; stale-token, focus, reconnect, hot-swap and old-client checks. Sanitizers and parser fuzzing pass. |

Stop at the end-to-end review checkpoint. If a gate fails, present the failure
and a bounded next experiment rather than opening another indefinite optimization
cycle. Do not add neural training to compensate for missing dictionary candidates.

The [detailed design](typo-tolerant-pinyin.md) and linked spike reports retain
technical reasoning and historical measurements. This brief is the current
scope/decision entry point; it does not override repository hard rules.
