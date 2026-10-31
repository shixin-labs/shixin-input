# 拾新输入法 Shixin Input

拾新是一款基于 [Rime](https://rime.im/) 引擎的中文输入法，目标是像搜狗拼音一样好用：
装上就能打字，所有选项都在设置程序里，无需编辑 YAML，而且打字永不卡顿。
目前支持 macOS，Windows 与 Linux 在计划中。

Shixin is a Chinese pinyin input method built on
[librime](https://github.com/rime/librime) with the
[rime-ice](https://github.com/iDvel/rime-ice) dictionary. It aims to be as
easy to use as Sogou Pinyin: install it, choose options in a real settings
app, and type. Nobody edits YAML, and **typing never stalls**: the app you
type in never waits on the engine, whatever the engine is doing.

**Status:** pre-release. macOS works and is in daily use; Windows and Linux
are planned ([milestones](docs/DESIGN.md#10-milestones)).

## Install (macOS)

Requires macOS 13 or later on Apple silicon.

1. Download `Shixin.dmg` from the
   [latest release](https://github.com/shixin-labs/shixin-input/releases/latest).
2. Open it and double-click **Shixin** (拾新). There is nothing to drag:
   it installs itself into `~/Library/Input Methods`, or upgrades an
   older copy in place.
3. Follow the 开始使用 guide that opens: allow 拾新 when macOS asks, add
   it under System Settings → Keyboard → Input Sources (+ → Chinese,
   Simplified → 拾新), and switch to it with Control-Space.

安装：从 [Releases](https://github.com/shixin-labs/shixin-input/releases/latest)
下载 `Shixin.dmg`，打开后双击「拾新」即可安装（无需拖到“应用程序”），再按
「开始使用」向导在系统设置中添加拾新。

## Features (macOS)

- **Pinyin:** full pinyin and seven shuangpin layouts (自然码, 小鹤, 微软,
  搜狗, 智能 ABC, 紫光, 拼音加加) with keyboard charts, optional full pinyin
  inside shuangpin, and fuzzy pinyin.
- **Candidates:** rime-ice's dictionary with English words, emoji and
  traditional characters; 以词定字; paging keys; optional second/third
  candidate shortcuts; forgetting a learned word; custom phrases; dates,
  amounts in 大写, Unicode and a calculator (`rq`, `R123`, `U62fe`, `cC1+2`).
- **Typing comfort:** Shift switches 中/英 and commits the typed letters;
  per-app default modes (terminals and editors start in English); automatic
  spaces between Chinese and English or digits; password fields are always
  English.
- **Look:** six skins, horizontal or vertical candidates, any installed
  font, light and dark.
- **智能排序 (optional):** reorders candidates using what you just wrote,
  with a small language model running on your Mac's GPU. Off by default;
  turning it on downloads the model once. A key's result never waits more
  than one frame for it.
- **Private by default:** no telemetry. Nothing leaves your Mac except the
  智能排序 model download you ask for.

## How it works

The input method itself never runs Rime. Each keystroke is classified
locally against a small key table and answered at once; Rime runs in a
separate engine process, and its results are applied when they arrive,
typically within a millisecond. Settings changes are compiled and warmed
up in the background, then swapped in without interrupting typing. Rime's
Lua extensions are never loaded: their features are rebuilt as native
components. See [`docs/DESIGN.md`](docs/DESIGN.md).

The code is Go (no cgo; librime and llama.cpp are bound with
[purego](https://github.com/ebitengine/purego)), with C++ only inside
librime's candidate pipeline. The UI uses [MyGo](https://github.com/egoist/mygo).

## Building on macOS

Requirements: macOS 13 or later (developed on macOS 26, Apple silicon),
Xcode Command Line Tools, Go (version in `go.mod`), CMake and Ninja
(`brew install cmake ninja`).

```sh
git clone --recurse-submodules https://github.com/shixin-labs/shixin-input
cd shixin-input
scripts/fetch-rime-data.sh   # rime-ice into build/rime-data
scripts/build-librime.sh     # our librime with librime-shixin, without Lua
scripts/build-llama.sh       # optional: llama.cpp for 智能排序
LIBRIME=$PWD/build/librime/out/lib/librime.1.dylib scripts/bundle-macos.sh
```

This produces `build/macos/Shixin.app`. Opening it installs it as above;
opening a newer build later upgrades it in place. `scripts/package-macos.sh`
makes the signed and notarized `Shixin.dmg` for a release (see
[`AGENTS.md`](AGENTS.md)).

## Contributing

Read [`AGENTS.md`](AGENTS.md) first: it has the rules that keep typing
fast (performance is a correctness requirement here), the architecture
boundaries, every build and test command, and the conventions. Run
`scripts/ci.sh go` before sending a change. Known problems and deferred
work are in [`docs/KNOWN-ISSUES.md`](docs/KNOWN-ISSUES.md).

## License

Shixin is free software under the GNU General Public License v3.0 or
later; see [LICENSE](LICENSE). It builds on librime (BSD-3-Clause), rime-ice
(GPL-3.0), llama.cpp (MIT), purego (Apache-2.0), MyGo (MIT), and the
ERNIE-4.5-0.3B-Base model (Apache-2.0), downloaded only when 智能排序 is
turned on.
