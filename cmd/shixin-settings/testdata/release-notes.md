拾新 0.2.2：从其他输入法导入词库，修复切换应用后的首键和中文标点问题。
Shixin 0.2.2 imports dictionaries from other input methods and fixes the first key after an app switch and Chinese punctuation in some apps.

## 下载 / Downloads

| 系统 / System | 安装包 / Installer |
|---|---|
| Apple silicon，macOS 13 或更新版本 | `Shixin.dmg`（Developer ID 签名并经 Apple 公证 / signed and notarized） |
| Intel / AMD x64，Windows 10 或更新版本 | `ShixinSetup-amd64-unsigned-preview.exe` |
| ARM64，Windows 11 或更新版本 | `ShixinSetup-arm64-unsigned-preview.exe` |

Windows 包仍为未签名预览版，安装需要管理员批准，暂不支持 32 位应用；macOS DMG 已签名并公证。两端版本均为 **0.2.2**。
Windows packages remain unsigned previews, require administrator approval and do not support 32-bit applications; the macOS DMG is signed and notarized. Both platforms are version **0.2.2**.

## 新功能 / New

- **从其他输入法导入**（设置 → 词库 → 从其他输入法导入，也出现在「开始使用」引导中）：自动找到本机的搜狗输入法 Mac 版学过的词和已启用的细胞词库，以及鼠须管、小狼毫、fcitx5-rime、ibus-rime 的 `rime_ice`、`luna_pinyin`、`pinyin_simp` 用户词库，一键导入。只读取来源，不修改。选择文件导入现在也支持搜狗导出的 `.bin` 备份。
  **Import from other input methods** (Settings → Dictionary → Import from other input methods, also in the Get started guide): finds Sogou for Mac's learned words and enabled cell dictionaries, and the `rime_ice`, `luna_pinyin` and `pinyin_simp` user dictionaries of Squirrel, Weasel, fcitx5-rime and ibus-rime, and imports them in one step. Sources are only read. Importing a file now also accepts Sogou's `.bin` backups.
- **macOS：** 从其他输入法切换到拾新后，在「拾」标记旁显示当前的中/英模式。
  **macOS:** after switching to Shixin from another input source, the current 中/英 mode appears beside the 拾 badge.

## 修复 / Fixes

- 切换应用后立即输入的第一个字母，在中文模式下不再被当作英文字母直接输入。
  The first letter typed right after switching apps is no longer inserted as a literal letter in Chinese mode.
- 在 Codex、微信 4 等应用中输入中文标点时，不再出现「,，」这样的重复标点。
  Chinese punctuation no longer comes out doubled (such as ",，") in apps like Codex and WeChat 4.
- 微信 4 中输入拼音时，第一个字母不再残留在消息里（例如「z 这个」）。
  In WeChat 4, the first letter of a composition no longer stays in the message (such as "z 这个").
- 修复导入真实搜狗 `.scel` 细胞词库时的读取错误。
  Fixed reading real Sogou `.scel` cell dictionaries.
- macOS：候选窗先移动到光标处再显示，不再在上一个应用的位置闪现。
  macOS: the candidate window moves to the caret before it appears, instead of flashing where the last composition was.
- macOS：按住一侧 Shift 或 Control 时松开另一侧，现在能正确识别为松开。
  macOS: releasing Shift or Control while the other side is held is now recognized as a release.

## SHA-256

- `Shixin.dmg`: `2ae749bf6f84ff72a6efe69826169097c960a504fb31a571a0dc000b7950ebca`
- `ShixinSetup-amd64-unsigned-preview.exe`: `787fce60ab7307dbba6714fed4d8638ad92a859e5419d19f333f34dbbaa5502c`
- `ShixinSetup-arm64-unsigned-preview.exe`: `7f1cfb1ed5b91a2088b7655e2ebe4e9704301477bbb98aa918ad68b124df962a`
