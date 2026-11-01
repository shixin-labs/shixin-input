# Sogou Mac settings compared with Shixin

This compares the settings and menu entries of Sogou Pinyin for Mac
(搜狗输入法 6.25.3, inspected 2026-10-26 in a macOS 26.6 VM) with what
Shixin's settings app supports. It records gaps; it doesn't make every
Sogou feature a Shixin requirement. It compares the Mac versions only, not
prediction quality, latency, or Windows/Linux support.

A Sogou entry means its control was seen, not that its behavior was tested
end to end. No account was used, and cloud, account, Contacts,
accessibility, microphone and screen-capture functions weren't exercised.
Shixin support means behavior available through the app and its validated
settings; a feature present only in upstream Rime data doesn't count.

## Comparison

✅ marks completed Shixin functionality within the listed scope. Remaining
gaps and testing qualifications are recorded in the last column.

| Feature | Sogou Mac: observed control | Shixin: current support | Gap or qualification |
|---|---|---|---|
| ✅ Full pinyin and shuangpin | 全拼 and seven 双拼 layouts | 全拼 and seven 双拼 layouts | The shared layouts are 自然码, 小鹤, 微软, 搜狗, 智能 ABC, 紫光, 拼音加加. |
| ✅ 自然码双拼 | Listed in the 双拼 selector | Available as 自然码双拼 / Ziranma Shuangpin | Uses rime-ice's `double_pinyin` schema, with compiler/deploy, fuzzy-pinyin and prebuilt packaging checks. |
| ✅ Shuangpin keyboard chart | Chart shown in 双拼方案设置 | Chinese/English charts for all seven layouts, opened from General | Initials, finals, semicolon and vowel-only spellings; every displayed mapping is checked against librime with rime-ice's spelling rules. |
| ✅ Full pinyin while using shuangpin | 双拼下同时使用全拼 | General → Also accept full pinyin, off by default, for all seven layouts | Accepts full/double spelling within a phrase, fuzzy spelling and common ü aliases. Ambiguous spellings may add candidates or change ranking. |
| Fuzzy pinyin | Eleven explicit pairs and 开启智能模糊音 | Eleven explicit pairs, full pinyin and shuangpin | No intelligent-fuzzy option. |
| Chinese/English, simplified/traditional, full/half width | Defaults and switching shortcuts | Mode, traditional and punctuation defaults; switching shortcuts | Initial full/half-width has no app control. |
| ✅ Default punctuation | Chinese/English punctuation defaults and switching shortcuts | General → Default punctuation: Chinese `，。` or English `,.` | New sessions use the preference; the shortcut temporarily toggles the current session. Both styles keep ordinary typing asynchronous. |
| ✅ Candidate paging | Comma/period, minus/equal, brackets, Tab, arrows | Configurable paging pairs, Tab, standard navigation | Core paging is covered. |
| ✅ Second/third candidate shortcuts | 二三候选 shortcut selector | Optional Ctrl or Option/Alt + semicolon/apostrophe presets, off by default | Selects items 2 and 3 on the current page while candidates are shown; preserves unmodified spelling keys. |
| Candidate filtering | 开启 Tab 筛选模式 | No corresponding app option | Sogou's filtering behavior was not exercised. |
| ✅ Per-app English mode | 自动切换到英文状态 with an app list | Per-app default modes and remembered modes | Core app-level behavior is covered. |
| English in browser address bars | 浏览器地址栏内默认启用英文 | No address-bar-specific detection | Existing per-app rules affect the app as a whole. |
| Hide inline pinyin | 隐藏内嵌拼音串 with an app list | Missing | Keep candidate display while optionally suppressing inline preedit. |
| ✅ Chinese/English auto spacing | 中文与英文/数字间加入空格 | Implemented on macOS, with per-app exclusions | Falls back to tracking when an app's text cannot be read reliably. |
| Punctuation customization | Symbol replacement editor, 中文标点配对, per-app English punctuation | Built-in mappings, a Chinese/English punctuation default and toggle | No replacement editor, automatic pairing option, or per-app punctuation setting. |
| Punctuation around numbers | 智能调整数字后标点, with explanatory details | Inherited numeric separator behavior for comma, period, colon | Partially covered by rime-ice; exact equivalence was not tested. |
| ✅ Candidate font | Font-family picker | Search installed families, system-default fallback, live preview | Fonts are prepared outside key handling. Missing fonts keep their saved preference. |
| Candidate appearance | Font size, layout, additional candidate styles, system/light/dark | Font size, horizontal/vertical layout, system/light/dark | Extra display-style controls are not built. |
| Skins | Skin marketplace and random skin switching | Six built-in skins | Skin editing/import and richer skin behavior are not built. |
| Status indicators | Configurable menu-bar icons, floating status bar and opacity | Input menu and a temporary caret mode bubble | No persistent floating bar or comparable icon controls. |
| ✅ Custom phrases | 自定义短语设置 | Add, search, and delete custom phrases | Core custom phrases are covered; format compatibility was not tested. |
| ✅ Forget learned words | Candidate forgetting was not exercised in the audit | Highlight with arrow keys, then Ctrl+Shift+Backspace (configurable) | Removes learned entries/ranking without committing text. Bundled words may still appear; custom phrases are deleted in Dictionary. |
| User dictionary management | Chinese dictionary import/export/clear; separate English and URL/email clearing | Automatic word learning, forget a highlighted word, open the data folder | Import/export/clear controls are not built. |
| Additional dictionaries | Enable/manage 细胞词库, add/remove, download more | No dictionary import UI | `.scel` and text import are deferred. |
| English input mode | Separate 英文输入法, translation and automatic word-space options | English words in Chinese candidates; English pass-through mode | No separate English completion/translation UI. |
| Typing statistics | Today's count, total count, typing-speed indicator | Engine latency and performance page | Character counts and typing-speed statistics are not built. |
| Cloud candidates | 云计算候选 | Not built | Opt-in cloud input is planned for M5. |
| Account/configuration sync | Account login for dictionary and configuration sync | Not built | Folder/WebDAV sync is planned; a Sogou-style account service is not a v1 goal. |
| Cross-device clipboard | 跨设备同步 page advertises copying on a computer and pasting on a phone | Missing | Login required; no transfer was tested. |
| Voice and handwriting | Input-menu entries | Missing | Both are outside Shixin's v1 scope. |
| Screenshots and OCR | Screenshot menu entry and 图片转文字 shortcut | Missing | Permission-dependent workflows were not exercised. |
| Stickers and custom emoticons | 斗图 menu and 自定义表情 controls | Emoji candidates and symbol data | No comparable sticker/custom-emoticon management UI. |
| Contacts suggestions and Touch Bar | Contacts-related controls and 搜狗 TouchBar settings | Missing | Contacts access was denied; Touch Bar was not tested. |

## Suggested follow-ups

Dictionary import/export/clear is a larger, already planned gap. Punctuation
customization needs clear rules and app compatibility tests. New key
bindings and preedit behavior must preserve the latency and allocation
requirements in [DESIGN §3](DESIGN.md#3-no-stall-architecture).

Cloud, sync, and multimedia tools should be evaluated against product
scope separately. Existing plans and deferred implementation details are
in [DESIGN §7](DESIGN.md#7-features-sogou-parity-map) and
[KNOWN-ISSUES.md](KNOWN-ISSUES.md).

## Reproducing

Sogou came from its [official Mac download page](https://pinyin.sogou.com/mac/)
(`sogou_mac_625b_new_guanwang.zip`, SHA-256
`581cc88c07fd2ef7e48d42a8bbd1adaba43e471bc5e64b2fc614385e48e4ef51`) and was
installed only in the test VM, never on a development Mac. Its settings
open from the input menu, or in the guest with:

```sh
"/Library/Input Methods/SogouInput.app/Contents/SogouPreference.app/Contents/MacOS/SogouPreference" -SGPreferences 0
```
