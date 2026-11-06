# Rejected beam diagnostic

This is the final snapshot of a rejected scratch experiment, not the active
native prototype. Apply this directory's `librime.patch` to a **fresh** copy of
librime `3438a3b860cd3525f25ee2198c35ea49a3f2d85d`, then use the parent README's
build/setup instructions and parent include files, replacing
`bounded_corrector.inc` with this directory's copy. Do not apply both patches.

Changes combine all-position abbreviation-only recovery, omission of unchanged
short dictionary entries, and a width-32 beam ordered using existing prefix-entry
weights. The beam is pruned after sibling expansion, so 32 is not a peak memory
bound. Existing operation caps and the Release 900 µs deadline remain. The
snapshot misses `zhr → 中国人` and regresses `xgr → 中国人`; it is rejected.
No new dictionary index, model, shipped feature or product setting is added.
See the [report](../../../../docs/spikes/typo-native-graph.md) for logs and limits.
