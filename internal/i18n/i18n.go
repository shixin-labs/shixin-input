// Package i18n is the catalog of user-facing strings: Chinese first, with
// English available (AGENTS.md). Views look strings up by key instead of
// hard-coding them.
package i18n

import (
	"os"
	"strings"
)

// Lang is a supported UI language.
type Lang int

const (
	Chinese Lang = iota
	English
)

var current = detect()

// detect picks English only when the user's preferred language is English;
// everyone else gets Chinese.
func detect() Lang {
	for _, v := range []string{os.Getenv("SHIXIN_LANG"), os.Getenv("LC_ALL"), os.Getenv("LANG")} {
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "en") {
			return English
		}
		return Chinese
	}
	return Chinese
}

// Current returns the UI language.
func Current() Lang { return current }

// SetLang overrides the detected language.
func SetLang(l Lang) { current = l }

// T returns the string for key in the current language, or key itself if
// the catalog lacks it.
func T(key string) string {
	e, ok := catalog[key]
	if !ok {
		return key
	}
	if current == English && e[1] != "" {
		return e[1]
	}
	return e[0]
}

// catalog maps keys to {Chinese, English}.
var catalog = map[string][2]string{
	"app.name":          {"拾新", "Shixin"},
	"mode.chinese":      {"中", "中"},
	"mode.english":      {"英", "En"},
	"mode.indicator":    {"输入模式", "Input mode"},
	"mode.offline":      {"拾新（引擎未运行）", "Shixin (engine not running)"},
	"menu.toggle":       {"切换中 / 英（Shift）", "Switch 中 / English (Shift)"},
	"menu.settings":     {"设置…", "Settings…"},
	"menu.mode.chinese": {"当前：中文", "Mode: Chinese"},
	"menu.mode.english": {"当前：英文", "Mode: English"},
	"menu.mode.offline": {"引擎未运行", "Engine not running"},
	"menu.about":        {"关于拾新", "About Shixin"},
	"menu.version":      {"版本 %s", "Version %s"},

	"settings.title":             {"拾新设置", "Shixin Settings"},
	"settings.scheme":            {"输入方案", "Input scheme"},
	"settings.page_size":         {"每页候选词个数", "Candidates per page"},
	"settings.shift":             {"Shift 键", "Shift key"},
	"settings.shift.commit_code": {"切换中英文，已输入的字母上屏", "Switch, commit typed letters"},
	"settings.shift.commit_text": {"切换中英文，候选词上屏", "Switch 中/英, commit candidate"},
	"settings.shift.clear":       {"切换中英文，清除输入", "Switch 中/英, discard input"},
	"settings.shift.off":         {"不切换", "Doesn't switch"},
	"settings.fuzzy":             {"模糊音", "Fuzzy pinyin"},
	"settings.save":              {"保存", "Save"},
}
