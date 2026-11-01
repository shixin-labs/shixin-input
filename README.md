# 拾新输入法 Shixin Input

拾新是一款基于 [Rime](https://rime.im/) 引擎的中文输入法，目标是像搜狗拼音一样好用：
装上就能打字，所有选项都在设置程序里，无需编辑 YAML，而且打字永不卡顿。
目前支持 macOS；Windows x64 与 ARM64 提供未签名预览版，Linux 在计划中。

Shixin is a Chinese pinyin input method built on
[librime](https://github.com/rime/librime) with the
[rime-ice](https://github.com/iDvel/rime-ice) dictionary. It aims to be as
easy to use as Sogou Pinyin: install it, choose options in a real settings
app, and type. Nobody edits YAML, and **typing never stalls**: the app you
type in never waits on the engine, whatever the engine is doing.

**Status:** macOS works and is in daily use. Windows x64 and ARM64 are available as
unsigned previews; Linux is planned
([milestones](docs/DESIGN.md#10-milestones)).

Windows x64 and ARM64 development builds include a TSF frontend and
candidate/tray host, with the same settings GUI. The 0.2.0 downloads are
unsigned previews. See
[Windows development](docs/windows.md).

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

## Install (Windows unsigned preview)

Requires administrator approval and Windows 10 or later on x64, or
Windows 11 on ARM64. 32-bit applications are not supported.

| Windows architecture | Installer |
|---|---|
| Intel/AMD x64 | `ShixinSetup-amd64-unsigned-preview.exe` |
| ARM64 | `ShixinSetup-arm64-unsigned-preview.exe` |

1. Download the matching installer above and its `.sha256` file
   from [0.2.0](https://github.com/shixin-labs/shixin-input/releases/tag/v0.2.0).
2. Use `Get-FileHash <installer-file> -Algorithm SHA256` and compare
   with the published checksum. The checksum detects corruption; it does
   not authenticate the publisher.
3. Open the installer and follow its guide. The installer is **unsigned**:
   Windows cannot verify its publisher, and some security policies block it.
   Setup requests UAC approval, preserves settings and learned words during
   upgrades, and may request a restart.
4. Choose 拾新 with Windows-Space. Updates are manual; Settings → About
   opens the release list. Uninstall through Windows Installed Apps.

Windows 安装：需要 x64 版 Windows 10 或更新版本，或 ARM64 版 Windows 11，
以及管理员批准。从
[0.2.0](https://github.com/shixin-labs/shixin-input/releases/tag/v0.2.0)
下载对应架构的安装包并核对 SHA-256，再打开安装包完成安装。安装包未签名，
Windows 无法验证发布者；部分安全策略会阻止运行。更新需手动下载安装，
设置和学到的词语会保留。暂不支持 32 位应用。

智能排序 is off by default. The Vulkan runtime and unavailable-GPU fallback
are tested, but Windows GPU scoring and latency still need hardware validation.

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
