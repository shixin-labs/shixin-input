# M2 spike: zero-shot models for context-aware candidate reranking

Date: 2026-10-27 · Machine: Apple M4, 16 GB · PyTorch 2.14.1 (MPS) ·
coremltools 9.0 · laya 0.4.15

Design: DESIGN §7.2. Code: `spikes/laya-zeroshot`, in Python. It is not
in this repository, which stays Go + C++; it is to move to a separate
training and evaluation repository with the fine-tuning work. The commands
below record how the results were made (downloads and the venv in
`build/laya`):

```
uv venv build/laya/venv && VIRTUAL_ENV=build/laya/venv uv pip install laya jieba datasets coremltools
export HF_HOME=$PWD/build/laya/hf
build/laya/venv/bin/python -I spikes/laya-zeroshot/make_eval.py build/rime-data build/laya/eval.jsonl 1500
build/laya/venv/bin/python -I spikes/laya-zeroshot/run_laya.py build/laya/eval.jsonl build/laya/out mps
build/laya/venv/bin/python -I spikes/laya-zeroshot/run_electra.py build/laya/eval.jsonl build/laya/out mps
build/laya/venv/bin/python -I spikes/laya-zeroshot/bench_coreml.py build/laya/out
build/laya/venv/bin/python -I spikes/laya-zeroshot/run_qwen.py build/laya/eval.jsonl build/laya/out mps fp32
build/laya/venv/bin/python -I spikes/laya-zeroshot/fuse_prior.py build/rime-data build/laya/out/qwen_fp32.jsonl
VIRTUAL_ENV=build/laya/venv uv pip install mlx-lm
build/laya/venv/bin/python -I spikes/laya-zeroshot/bench_qwen.py build/laya/eval.jsonl mps fp16 300
build/laya/venv/bin/python -I spikes/laya-zeroshot/bench_mlx.py build/laya/eval.jsonl build/laya/out/qwen_fp32.jsonl 300 8
build/laya/venv/bin/python -I spikes/laya-zeroshot/bench_table.py build/laya/eval.jsonl 400 8 16 64
```

## Question

Can a small open decision/encoder model, without fine-tuning, pick the
written word among its same-pinyin rivals from the left context better
than rime-ice's context-free order, and fast enough on the GPU/ANE?

## Evaluation set

1,500 points in simplified-Chinese FineWeb-2 (`cmn_Hani`) web documents,
at most 3 per document; documents with common traditional-only
characters are skipped. jieba segments the text. A word becomes an item when
it has one reading in rime-ice (8105, base, ext, tencent, others; the first
table's weight wins), at least one same-pinyin rival, and is on the first
page (5) of those homophones by weight. Context is up to 60 characters
before the word, within its line. Options are in rime-ice weight order, an
approximation of librime's context-free menu for that span (no user
dictionary, no sentence candidate). Labels: 532 single characters, 958
two-character words, 10 longer.

**rime-ice order is right 85.8% of the time** (213 errors). That is the
ceiling for any reranker on this set.

## Laya multilingual (322M, mmBERT-base), zero-shot: no

Choice question with the options as criteria, English and Chinese
instructions. Top-1 alone is 59%. Promoting Laya's pick only when its
probability beats rime's #1 by τ never beats rime:

| τ | top-1 | fixes | breaks |
|---|---|---|---|
| 0.5 | 80.4% | 52 | 133 |
| 0.9 | 85.7% | 19–21 | 21–22 |

The Chinese prompt was no better than the English one. MPS latency was
19 ms p50 per item. It may still learn the task with fine-tuning, but it
brings no usable signal as shipped.

## Chinese ELECTRA-small discriminator (12.3M), zero-shot: yes

`hfl/chinese-electra-180g-small-discriminator` (Apache-2.0). Each option is
appended to the context, and all options run as one batch. The score is the sum of
log P(original) over the option's tokens from the replaced-token head
("cand"). Scoring every token ("all") was worse.

| Policy | top-1 | fixes | breaks |
|---|---|---|---|
| rime-ice order | 85.8% | — | — |
| ELECTRA alone | 78.7% | 150 | 256 |
| promote if log-score margin ≥ 0.5 | 89.8% | 101 | 41 |
| margin ≥ 1.0 | 90.1% | 75 | 11 |
| rank prior β = 0.5, margin ≥ 0.5 | 89.4% | 60 | 6 |
| rank prior β = 1, margin 0 | 89.1% | 55 | 6 |

The score is `log p_i − β·rank_i`, and the model's pick replaces #1 only if
it leads by the margin. Choosing (β, margin) on one half and testing on the
other picked β = 0, margin 0.75 both times. Held out, that gave 89.9% vs
85.2% (43 fixes, 8 breaks) and 90.7% vs 86.4% (46 fixes, 14 breaks), about
**+4.5 points**, or a third of rime's errors fixed.

Fixes are what context should fix: 结婚|生子 (not 绳子), GDDR5 显存 (not
现存), 性能瓶颈 (not 平静), 版权声明 (not 生命). Breaks lean toward rare words
(荡涤 for 当地, 作伪 for 作为, 从命 for 聪明); a stronger rank prior removes
most of them at the cost of fixes. On 12 hand-written cases
(`hand.jsonl`) it ranked 9 correctly, but 他/她/它 and 的/地/得 only by
probability gaps under 0.02, which no margin acts on: zero-shot ELECTRA
barely separates them.

### Weighing rime-ice's order more

`fuse_prior.py` (`… fuse_prior.py build/rime-data build/laya/out/electra_cand.jsonl`)
adds rime-ice's weights to the score: `log p_i + β·log((w_i+1)/(w_0+1))`.
It also tries a guard, under which an option can only be promoted if
`w_i ≥ ratio·w_0`. Best full-set setting under a break budget:

| Breaks allowed | Rank prior | Weight prior / guard |
|---|---|---|
| ≤ 3 | 88.4% (+42/−3) | 89.7% (+62/−3), guard ratio 0.1, margin 1.0 |
| ≤ 10 | 89.7% (+66/−7) | 90.3% (+77/−9), β 0.2, margin 0.5, ratio 0.1 |
| ≤ 20 | 90.1% (+75/−11) | 90.6% (+87/−15), β 0.05, margin 0.75 |

Held out, with settings chosen on one half under a cap of ≤ 2 breaks:

- Rank prior: 88.3% (+26/−3) and 88.0% (+12/−0).
- Weight prior / guard: 88.9% (+31/−3) and 90.4% (+35/−5).

With a looser cap the two converge at about 90.7%. Weights add fixes at a
cautious setting. They do not raise peak accuracy.

The 当地/荡涤 breaks are gone in the settings above. The weights are
500,655 against 5,565, so the guard (ratio 0.011) blocks 荡涤, and at β 0.2
the fused lead drops below the margin. The breaks that remain are plain
model errors between words of similar frequency: 抓住|好→毫, 几个→及格,
收益→兽医, 放在→防灾, 而→二. No prior fixes those; fine-tuning has to.

## Speed: Core ML on the ANE

Converted with coremltools 9.0 at FP16, fixed shape 5 options × 64 tokens,
300 timed predictions after 20 warm-ups:

| Compute units | p50 | p95 | max \|Δlogit\| vs PyTorch FP32 |
|---|---|---|---|
| CPU + ANE | **1.20 ms** | 1.34 ms | 0.137 |
| CPU + GPU | 3.33 ms | 3.70 ms | 0.188 |
| CPU only | 3.56 ms | 3.91 ms | 0.297 |

Conversion needs a wrapper: precomputed additive attention mask, explicit
position and token-type ids (transformers 5.21's mask and buffer slicing do
not trace through coremltools). The package is ~25 MB at FP16. The ANE run
did not confirm the per-op placement (`MLComputePlan`); the 3× gap to CPU
suggests the encoder ran there.

## Qwen3-0.6B-Base (596M, causal LM), zero-shot: best by far

`Qwen/Qwen3-0.6B-Base` (Apache-2.0), scored with `run_qwen.py`. Each option
gets log P(option | context): the context (last 64 tokens) and the option
are tokenized separately and concatenated, and the option's token
log-probabilities are summed. Same-pinyin options have the same character
count but not the same token count, and the summed probability compares
them fairly without length normalization.

| Policy | top-1 | fixes | breaks |
|---|---|---|---|
| rime-ice order | 85.8% | — | — |
| Qwen alone | 94.9% | 186 | 49 |
| promote if probability lead ≥ 0.6 | 95.9% | 168 | 17 |
| weight prior β 0.5, margin 2 (log) | 95.4% | 153 | 9 |
| weight prior β 0.4, margin 1 (log) | 96.0% | 168 | 15 |

Choosing the setting on one half and testing on the other:

- 95.6% vs rime 85.2% (+85/−7)
- 96.1% vs rime 86.4% (+86/−13)

That is about **+10 points**, or about 80% of rime's errors fixed. On the 12
hand cases it got 11, with large gaps: 我妈妈今天很累，|她 0.97,
小狗很可爱，|它 0.75, 网上|散布 0.997. It missed only 慢慢|地 (得 0.40,
地 0.37).

### Speed on the M4 GPU

Context changes only on commit, so the realistic cost splits into two
parts:

- **Prefill:** the context once per commit, keeping its KV cache and the
  next-token distribution.
- **Per key:** single-token options are read straight from that
  distribution. Multi-token options run one batched step over their
  remaining tokens on top of the cache.

`bench_qwen.py` (PyTorch) and `bench_mlx.py` (MLX) compute it this way.
The cached scores pick the same option as the full forward for 290 of 290
items. 31% of items have only single-token options, which need no step at
all.

| Runtime | Prefill (per commit) p50 | Per key with a step, p50 / p95 | Same top-1 as FP32 |
|---|---|---|---|
| PyTorch MPS FP16 | 41.9 ms | 27.0 / 30.2 ms | 290/290 |
| MLX 16-bit | 26.9 ms | 19.3 / 25.6 ms | 290/290 |
| MLX 8-bit | 20.0 ms | 13.2 / 33.8 ms | 290/290 |
| MLX 4-bit | 19.1 ms | 9.8 / 33.0 ms | 280/290 |

For comparison, the full uncached forward of 5 rows took 85.7 ms on MPS.
4-bit changes 3.4% of the choices; 8-bit is lossless here.

A small step is bound by memory bandwidth: every step reads all the
weights, and the base M4 GPU reads about 120 GB/s, which puts the floor
near 10 ms at 16-bit. The p95 values are noisy. Other GPU load on the
machine was not controlled.

**Per-commit table (`bench_table.py`, MLX 8-bit).** After a commit, also
run one batched step for the top-K first tokens. Per key, an option's score
is exact if it is one token, or two tokens whose first is in the top K.
Otherwise its first-token log-probability is an upper bound. That decides
the choice exactly whenever the best exact score beats every bound. Then
the per-key work is a lookup, synchronous, with no late reorder.

| K | decided exactly | top-1 (undecided → rime order) |
|---|---|---|
| 16 | 88.2% | 94.1% vs rime 85.6% (+39/−6) |
| 64 | 89.7% | 94.4% |

The table step is unexpectedly slow in MLX from Python:

- prefill 25 ms, plus 49–65 ms for a 16-row step, so 73 ms p50 per commit
  at K = 16
- a single batched 16 × 31-token pass is slower still: 183 ms

The step should cost about the same as a 5-row per-key step. Native
runtimes (llama.cpp's Metal backend, mlx-c, a Core ML stateful model) were
not tried in this spike; llama.cpp was adopted afterwards (DESIGN §7.2).

Size: 1.2 GB at FP16, about 0.6 GB at 8-bit. Not tried on the ANE.

## ERNIE-4.5-0.3B-Base (361M), zero-shot: better and faster than Qwen

`baidu/ERNIE-4.5-0.3B-Base-PT` (Apache-2.0, Llama-style, native in
transformers, no remote code), scored exactly as Qwen:
`MODEL=baidu/ERNIE-4.5-0.3B-Base-PT NAME=ernie … run_qwen.py`.

| | Qwen3-0.6B-Base | ERNIE-4.5-0.3B-Base |
|---|---|---|
| alone | 94.9% (+186/−49) | **95.5%** (+185/−40) |
| ≤ 2 breaks (full set) | 92.4% (+101/−2) | **95.7%** (+150/−1) |
| ≤ 10 breaks (full set) | 95.4% (+153/−9) | **96.4%** (+165/−6) |
| held out (two halves) | 95.6% / 96.1% | **96.1% / 96.1%** (+86/−4, +81/−8) |
| hand cases | 11/12 | **12/12** (慢慢|地 by 0.462 to 0.459) |
| MLX 16-bit prefill / per key p50 | 26.9 / 19.3 ms | **16.1 / 9.6 ms** |
| MLX 8-bit prefill / per key p50 (p95) | 20.0 / 13.2 (33.8) ms | **11.0 / 6.3 (10.6) ms** |
| same top-1 as FP32, MLX 8-bit | 290/290 | 289/290 |

The cautious setting of 2 breaks is the default-relevant one. There ERNIE
fixes 150 errors against Qwen's 101. Its size is about 0.7 GB at 16-bit and
0.36 GB at 8-bit.

### Top 10 options instead of 5 (Qwen)

`PAGE=10 make_eval.py` builds a set with rime-ice's first 10 homophones
(rime top-1 84.0%). Only 22 of 1,500 labels (1.5%) are at ranks 6–10, which
caps the gain. Run on the same items:

- Choosing among all 10, held out: 94.3% / 95.7% (+85/−6, +95/−10).
- Choosing among the first 5: 93.5% / 94.7% (+79/−6, +86/−8).

That's about +1 point, with breaks barely changed. Scoring 10 options
costs little more, since most options are single tokens and are read from
the next-token distribution. Showing them means moving a page-2 word onto
page 1.

## Reordering the whole list, not just #1 (ERNIE)

The model scores every option in one pass. Sorting by the fused score gives
a whole new order at no extra cost. `PAGE=20 make_eval.py` builds a set
from rime-ice's first 20 homophones. In it, rime has the written word at
#1 for 1,256 items and on page 1 (top 5) for 1,474; only 26 items (1.7%)
have it at ranks 6–20.

`rank_eval.py` reports two numbers:
- **first word:** the written word is #1
- **first page:** the written word is in the top 5

Settings are chosen on one half and tested on the other, and the two
held-out halves are pooled:

| Model sees | Policy | First word | First page | Pushed off / pulled onto page 1 |
|---|---|---|---|---|
| — | rime-ice order | 83.7% | 98.3% | — |
| rime top 5 | sort, β 0.3 | 94.9% | 98.3% | 0 / 0 |
| rime top 10 | sort, β 0.3 | 95.8% | 99.5% | 0 / 18 |
| rime top 20 | sort, β 0.3 | **96.1%** | **99.9%** | 1 / 25 |
| rime top 20 | stable (below) | 96.0% | 99.9% | — |
| rime top 20 | Qwen3-0.6B, sort | 94.9% | 99.7% | 4 / 26 |

Scoring 20 options per key costs ERNIE about the same as scoring 5: in MLX
8-bit, 6.8 ms p50 and 14.0 ms p95 when a step is needed. The step is needed
in 90% of items, since most of the 20 lists contain a multi-token word.
Prefill stays at 10.8 ms per commit, and top-1 matches FP32 in 290/290.

"sort" puts all 20 options in fused-score order. "keep1" keeps rime's #1
unless the leader beats it by a margin; it scored the same within 0.1
point.

**Stability.** A full sort changes a lot of what's on screen:

| | #1 changed | Page-1 order unchanged | Page-1 slots changed (avg) | First word fixes / breaks |
|---|---|---|---|---|
| full sort | 15.9% | 31.9% | 2.07 | +202 / −17 |
| keep1, margin 2 | 12.7% | 32.3% | 2.03 | +174 / −4 |
| **stable** | 15.3% | **55.0%** | **1.18** | +197 / −13 |

Users who pick by number key rely on positions 2–5. The "stable" policy:

1. The model picks #1 (by fused score, with margin).
2. It picks the other four page-1 words: the best-scored ones of the top 20.
3. Those four are shown in rime-ice order.

It keeps the same accuracy and halves the churn on page 1.

## 简拼 and 模糊音 (ERNIE, up to rime's top 50)

Two more ways the input gets ambiguous. Sets come from `make_eval.py`:

- **简拼**, `ABBREV=1 PAGE=50`: multi-character words typed by their
  initials (台湾 → `tw`). The options are every word with those initials, in
  weight order. Every option is a multi-character word.
- **模糊音**, `FUZZY=1 PAGE=50`: words are grouped by pinyin equal under
  z=zh, c=ch, s=sh, n=l, an=ang, en=eng, in=ing. Those are seven of the
  pairs in Shixin's `FuzzyPairs`.

The sets only include points with the written word in the top 50. In the
scanned documents, before sampling, it was below 50 at 12.0% of 简拼 points
(20,110 of 167,629) and 0.5% of 模糊音 points.

Held out, pooled, with `NS=5,10,20,50 rank_eval.py`:

| Input | Model sees | First word | First page | Pushed off / pulled on |
|---|---|---|---|---|
| 简拼 | rime order | 29.4% | 61.3% | — |
| 简拼 | top 5, sort | 55.9% | 61.3% | 0 / 0 |
| 简拼 | top 10, sort | 67.4% | 76.7% | 5 / 236 |
| 简拼 | top 20, stable | 72.9% | 86.5% | 8 / 386 |
| 简拼 | top 50, sort | **79.0%** | **95.5%** | 11 / 524 |
| 模糊音 | rime order | 79.4% | 96.7% | — |
| 模糊音 | top 20, stable | 95.2% | 99.5% | 1 / 42 |
| 模糊音 | top 50, stable | **95.4%** | **99.8%** | 2 / 48 |

简拼 is where the model matters most. Its first word goes from 29% to 79%,
and the right word goes from missing page 1 in 39% of cases to 4.5%. That
takes the model seeing far more than one page.

**Cost.** MLX 8-bit, 50 简拼 options (all multi-token):

- prefill: 11.7 ms per commit
- per key: **23.4 ms p50, 112.8 ms p95**
- top-1 matches FP32: 286/290

That's too slow at p95. The run was on a machine with about 5 GB of swap
in use. Each key also copies the context's KV cache 50 times
(`mx.repeat`), which a runtime with shared-prefix sequences (llama.cpp
`seq_cp`) would avoid. Measure this in a native runtime before deciding
how many options to score.

The offline scorer (`run_qwen.py`) first ran out of memory on these sets:
the logits over the full 103k vocabulary for 50 rows × ~35 positions
filled about 10 GB of swap. It now computes vocabulary logits only at
option positions. Scores are unchanged (max |Δp| 3e-6). It still takes
about 410 ms per 简拼 item in FP32 without a cache, so building a 1,500-item
evaluation takes about 10 minutes.

## Smaller models on 简拼

Every model below is native in transformers, with no remote code.

| Model | Params | License | Full pinyin, held out | 简拼 top 50: first word | 简拼 top 50: first page | Per key, 50 options, MLX 8-bit p50 / p95 |
|---|---|---|---|---|---|---|
| ERNIE-4.5-0.3B-Base | 361M | Apache-2.0 | 96.1% / 96.1% | **79.0%** | **95.5%** | 23 / 113 ms |
| uer/gpt2-chinese-cluecorpussmall | 102M | **none stated** | 96.0% / 96.9% | 77.3% | 94.5% | 57 / 97 ms |
| IDEA-CCNL/Wenzhong-GPT2-110M | 110M | Apache-2.0 | 95.6% / 95.9% | 76.0% | 94.6% | 50 / 97 ms |
| uer/gpt2-distil-chinese-cluecorpussmall | 60M | **none stated** | 94.1% / 96.3% | 75.5% | 93.5% | 27 / 39 ms |
| tiiuae/Falcon-H1-Tiny-Multilingual-100M | 108M | Falcon LLM | 93.6% alone | — | — | — |
| jingyaogong/minimind-3 | 64M | Apache-2.0 | 83.1% alone (below rime) | — | — | — |
| jingyaogong/MiniMind2-Small | 26M | (Apache-2.0 per repo) | 77.1% alone (below rime) | — | — | — |

The uer weights have no license tag; Azure's catalog lists them as
"Unknown". CLUECorpusSmall, their training corpus, is meant for research,
so they can't ship. MiniMind is trained on a few GB of text and is too weak.

**Smaller is not faster here.** Per key, the cost is the batched step over
the options' remaining tokens, plus copying the context cache once per
option from Python. That is overhead, not compute.

The character-level tokenizers (uer: one token per hanzi; Wenzhong:
byte-level BPE) make every 简拼 option 2–4 tokens. ERNIE's 103k vocabulary
often holds the whole word as one token.

The MLX conversion for the GPT-2 models goes through
`build/laya/conv/<name>`: safetensors with the `transformer.` prefix
stripped. Rankings match FP32 for 287–290 of 290 items.

**A two-level table built per commit** (`bench_tree.py`, uer-distil) loses
too much:

- K = 64: 37 ms per commit, 64.9% / 82.3%
- K = 256: 166 ms per commit, 74.6% / 89.0%
- full scoring for comparison: 77.2% / 93.6%

No item was exact: with 50 options there is almost always a 3+ token word,
or a first token outside the top K.

**Pruning with first-token bounds** (`bench_prune.py`) works:

1. The prefill already gives log P(first token) for every option, free.
2. Fully score the M options with the best bounds, plus rime's top 5.
3. Sort those; the rest follow in bound order.

| Model | M | Per key p50 / p95 | First word | First page | Full scoring |
|---|---|---|---|---|---|
| ERNIE | 8 | **9.7 / 17.1 ms** | **79.5%** | 93.8% | 79.5% / 96.4% |
| ERNIE | 16 | 15.0 / 35.7 ms | 79.5% | 95.1% | |
| uer-distil | 8 | 10.3 / 22.0 ms | 73.8% | 86.4% | 77.2% / 93.6% |
| uer-distil | 16 | 15.4 / 32.4 ms | 76.2% | 91.0% | |

These use the first 390 items after warm-up, β 0.3. Pruning suits ERNIE:
with whole words often as one token, the bound is often the exact score.
It loses no first-word accuracy, about 2.6 points of first page, and cuts
per-key cost from 23 to about 10 ms.

## Caveats

- Options approximate librime's menu from dictionary weights. Real menus
  (user dictionary, sentence candidates, span mixing) need the engine.
- Web text is not casual typing; the per-word segmentation is jieba's, and
  users often type several words per composition, which has fewer
  homophones.
- 213 rime errors in total; differences of a few fixes are noise.
- Ranking accuracy of the converted FP16 model on the set was not rerun.

## Conclusion

**ERNIE-4.5-0.3B-Base is the pick** (DESIGN §7.2). Zero-shot, it fixes
about three quarters of rime-ice's context errors on full pinyin (96.1%
held out against 85.8%), with fewer breaks
than Qwen3-0.6B at cautious settings, and lifts 简拼's first word from 29%
to 79%. It is about twice as fast as Qwen: 6.3 ms per key and 11 ms per
commit in MLX 8-bit on the M4, and about 10 ms per key for 50 简拼 options
with first-token pruning. rime-ice's weights belong in the policy, as a
guard plus a small weight term, and the "stable" order keeps page 1 from
churning.

How the pick evolved: zero-shot Laya brought no usable signal;
ELECTRA-small fixed about a third of rime's errors in 1.2 ms on the ANE and
remains the fallback where a 0.3B model is too heavy; Qwen3-0.6B fixed far
more but was slower; ERNIE matched or beat it at half the cost.

Next: a native runtime with shared-prefix sequences (done: llama.cpp in
`shixin-ai`), evaluation on real engine menus (`tools/menu-capture`), and
fine-tuning, whose break rate decides the default margin.
