# Typo-tolerant pinyin: internet sources and first prepared corpus

Date: 2026-11-04 · Status: data preparation; no model training or IME changes

Design: [Typo-tolerant pinyin](../typo-tolerant-pinyin.md).
Tools and reproduction: [tools/typo-data](../../tools/typo-data/README.md).

Follow-up: [abbreviation pilot](typo-abbreviation.md), 830 clean/corrupted
full, initials and mixed examples from existing dev/test families, with native
menu capture and ERNIE evaluation. It remains synthetic evaluation data.

## What is ready

Downloaded the public CC-CEDICT release dated **2026-11-03 00:50:19 UTC** and
prepared **153,421 examples from 20,000 connected phrase/reading families**.
The 73 MiB output is local in `build/typo-data/cedict-v1/`; data files are ignored
by git. The [recorded manifest](../../tools/typo-data/manifests/cedict-v1.json)
preserves attribution, source checksum/header, counts, seed and output hashes.
Two complete runs produced identical output hashes.

Also downloaded and audited the fixed revision of Duyu/Pinyin-Hanzi: **1,646,554
headerless CSV records**, 1,583,561 distinct pairs, 62,993 duplicate pairs and
1,574,764 distinct Chinese targets. It remains separate raw research data under
`build/typo-data/raw/duyu/`, not mixed into the prepared corpus.

**No verified public raw-pinyin human-typo corpus was acquired in this search.**
The prepared corruptions are synthetic. This is useful data for spelling-search
recall and deterministic channel experiments, not enough evidence to choose or
ship an end-to-end model. No typing logs, live user dictionaries or private
messages were read.

## Internet source inventory

| Source and primary evidence | What it provides | Acquisition and use decision |
|---|---|---|
| [CC-CEDICT / MDBG](https://www.mdbg.net/chinese/dictionary?page=cc-cedict), [project wiki](https://cc-cedict.org/wiki/) | Community Chinese dictionary with traditional/simplified forms and pinyin; explicit CC BY-SA 4.0 notice | Downloaded 125,360-entry snapshot; used for this synthetic pilot. Preserve attribution and share-alike terms on derived data. No sentence context or human keyboard errors. |
| [Duyu/Pinyin-Hanzi](https://huggingface.co/datasets/Duyu/Pinyin-Hanzi), [fixed card](https://huggingface.co/datasets/Duyu/Pinyin-Hanzi/blob/9961d7d05910be7006f2dcfae21f4fc5ce4d560a/README.md) | Large Chinese/pinyin CSV, mixing phrases, names and sentences; publisher labels Apache-2.0 | Downloaded and audited only. Underlying text sources, reading-generation method and quality are undocumented in the short card. Resolve those before promoting it to a distributable training source. Not real typo data. |
| [Transformers4IME / PinyinGPT](https://github.com/VisualJoyce/Transformers4IME), [ACL 2022 paper](https://aclanthology.org/2022.acl-long.133/) | Reported 270K benchmark instances across 15 news domains, studying perfect/abbreviated pinyin with context | README links benchmark through Baidu Drive. Not downloaded; no dataset license was established from the inspected README/root listing. Valuable contextual evaluation lead, not verified raw typo data. |
| [READIN authors' repository](https://github.com/thunlp/READIN), [ACL 2023 paper](https://aclanthology.org/2023.acl-long.460/) | Human re-entry noise benchmark with keyboard and speech variants across four NLP tasks; README offers Google Drive/Tsinghua Cloud downloads | Not downloaded. Useful lead for realistic post-input robustness. The README does not establish that released data includes original Latin keystrokes; inspect schema and underlying benchmark terms before treating it as raw-pinyin correction supervision. Speech noise must remain separate. |
| [KNPTC paper, §4.1](https://arxiv.org/html/1805.00741) | Directly relevant raw pinyin/target supervision derived from Sogou input logs, including backspace/re-entry information | Research evidence, not acquired data: no public dataset release was located. Its logs are not available to this project merely because the paper describes them. Do not substitute synthetic examples and label them KNPTC real errors. |
| [phrase-pinyin-data README](https://github.com/mozillazg/phrase-pinyin-data), [repository license](https://github.com/mozillazg/phrase-pinyin-data/blob/master/LICENSE) | Pronunciation mappings and corrections; aggregate includes CC-CEDICT and other listed sources | Not downloaded. MIT repository license does not by itself resolve every listed upstream data source. Prefer the explicit CC-CEDICT snapshot for this first corpus. |
| [SimZhou/pinyin_autocorrection](https://github.com/SimZhou/pinyin_autocorrection) | Demonstration of edit-distance/probabilistic pinyin correction | Not selected as a dataset. README describes a learning/demo project; inspected repository metadata has no license, and no verified human typo benchmark was established. |

Search results for Chinese spelling correction and speech-recognition repair
are not interchangeable with raw input-method errors. A wrong Chinese character
can result from homophone selection with perfectly correct pinyin. Translating
that character back to pinyin does not recover what the user typed.

## Snapshot provenance

| Artifact | Recorded identity |
|---|---|
| CC-CEDICT gzip | SHA-256 `2ec9a065cee371c2ee6a75adda056deef979ee336a7ae45b3d25f2ef2e6d427f` |
| CC-CEDICT release | `2026-11-03T00:50:19Z`, publisher MDBG, 125,360 entries |
| Derived-data license | CC BY-SA 4.0; CC-CEDICT community/MDBG attribution and original header retained in manifest |
| Duyu revision | `9961d7d05910be7006f2dcfae21f4fc5ce4d560a` |
| Duyu CSV | SHA-256 `420af662df18e4c3730ef2555bd37f583f7f6fa193895387ce335924ef117862` |
| Duyu README | SHA-256 `37168686c51372f0f384791981235bb9926f37568af8f9a6a87884b02172e1d5` |

The Duyu web viewer presents the first real record as column headings and shows
1,646,553 rows. The downloaded file has **no header**; the audit counts all
1,646,554 records using Go's CSV parser. Of these, 1,528,987 have Han-only Chinese
text. No syllable-count mismatch was found in that Han-only subset; this checks
alignment count, **not pronunciation correctness**. Remaining mixed/punctuation
records are retained in raw data, not automatically considered invalid.

## Prepared split and slices

The source yielded 110,209 eligible records after filtering and duplicate removal,
forming 85,483 connected families. Sampled 20,000 families before corruption;
identical simplified/traditional text and untoned readings link into one family.
This guards exact phrase/reading leakage but does not guarantee semantic-family
or cross-corpus deduplication. The single dictionary is not a held-out-domain test.

| Slice | Train | Dev | Test |
|---|---:|---:|---:|
| Families | 16,103 | 2,000 | 1,897 |
| Clean | 20,694 | 2,586 | 2,478 |
| Within-syllable transposition | 20,678 | 2,574 | 2,471 |
| Cross-boundary transposition | 20,537 | 2,565 | 2,448 |
| Adjacent-key substitution | 20,609 | 2,531 | 2,434 |
| Missing letter | 20,375 | 2,393 | 2,302 |
| Repeated letter | 20,688 | 2,581 | 2,477 |

There are 4,086 corrupted examples whose raw code equals another eligible
lexicon entry's clean code. These are useful ambiguity probes, not a complete
set of valid-pinyin collisions. The generator excluded 951 cross-split raw-code
collisions and two examples above the capture length cap. Its 134,747 unique
capture inputs contain only `code`, never gold targets.

The operation mixture is deliberately balanced for coverage; it is not an
estimate of real typo prevalence. The keyboard geometry and corruption positions
are synthetic assumptions. Cross-boundary swaps are a challenge slice, beyond
the initial design's within-syllable support. Exact short codes, abbreviations,
fuzzy/shuangpin, apostrophes, mixed English, native triggers and UI interactions
still need separate fixtures. The tool preserves a reversible canonical-to-raw
edit but does not invent real incremental timing or backspace traces.

## Implications for model selection

1. **Start with candidate recall.** This corpus can reveal whether a bounded
   generator recovers target readings, especially swaps and neighboring keys.
   Keep missing targets as retrieval failures, including when evaluating a model.
2. **Do not overinterpret top-1.** There is no left context, and homophones can
   have multiple legitimate targets. A different same-code Chinese candidate is
   not necessarily a false correction. The dictionary does not provide typing
   frequency priors or a user's intended selection.
3. **Do not train on the pilot test set while calling it held out.** All model
   and channel tuning uses train/dev. The pilot's 2,478 clean test rows are below
   the design's 10,000-clean-example safety requirement and do not cover protected
   paths; this is not a release-gate dataset.
4. **Add contextual data before deciding between ERNIE and a joint model.**
   Resolve provenance/access for a sentence/context source, split by document,
   preserve polyphonic readings, and then synthesize errors. Do not fabricate
   context by joining unrelated dictionary words. Benchmark all alternatives on
   the same menus and a separately held-out real-typo collection.
5. **Real raw errors remain the main gap.** Verify READIN's downloadable schema
   as a research lead; otherwise obtain explicitly volunteered raw/corrected
   examples. No contact was sent to authors and no private logs were requested.

## Verification performed

- Tool tests and `go vet` passed; no new dependencies or runtime input code.
- Two full preparations yielded identical SHA-256 hashes for all four JSONL
  outputs and identical manifests.
- Full-artifact verification checks hashes, reversible edits, unique IDs,
  raw-code/family split separation and complete answer-free capture coverage.
- No engine capture, model training, latency benchmark or GUI test was run:
  those would not validate this data-only work. The IME feature remains proposed.
