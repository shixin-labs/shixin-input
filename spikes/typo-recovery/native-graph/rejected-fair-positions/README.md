# Rejected correction-position scheduling

Scratch-only diagnostic; not the active patch. Apply this `librime.patch` to a
fresh copy of pinned librime `3438a3b860cd3525f25ee2198c35ea49a3f2d85d` and
use the parent README's build instructions and includes, replacing its
`bounded_corrector.inc` with this copy. Never apply both patches.

The broad stage considers abbreviation repairs at all raw positions. A LIFO
frontier for each correction position, plus an unchanged-prefix frontier, is
visited round-robin under the existing shared caps/deadline. Native vectors and
Code objects still allocate. It misses `zhr → 中国人` and regresses `xgr → 中国人`;
the active source has been restored. See the
[report](../../../../docs/spikes/typo-subtree-bounds.md).
