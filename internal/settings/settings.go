// Package settings is the user's settings model and the compiler that turns
// it into Rime config patches (DESIGN §5.3). Users never edit YAML.
//
// The compiler owns default.custom.yaml, <schema>.custom.yaml and the
// custom phrase table in Shixin's own Rime user dir. Among other things it
// writes every schema's full engine lists, so no rime-ice lua_* component
// ever reaches a deployed schema (DESIGN §3.8, R9).
//
// Some settings are for the fronts only (candidate window layout and
// appearance), and the engine applies its per-app rules live: they change
// no Rime file, and EngineChanged tells a front whether the engine must
// reload.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Settings are the user's choices, stored as settings.json. Fields only
// ever get added; a missing field keeps its default.
type Settings struct {
	Version int `json:"version"`

	// General (常用).

	// Scheme is the Rime schema id: rime_ice (全拼) or a double pinyin
	// variant.
	Scheme string `json:"scheme"`
	// FullPinyinFallback also accepts full spellings in double-pinyin
	// schemas. It is off by default and doesn't change the full-pinyin schema.
	FullPinyinFallback bool `json:"full_pinyin_fallback"`
	// PageSize is the number of candidates per page, 3–10.
	PageSize int `json:"page_size"`
	// Layout is the candidate window's layout, "horizontal" or "vertical".
	// Fronts only.
	Layout string `json:"layout"`
	// Shift is what tapping Shift does: "commit_code" (switch 中/英 and
	// commit the typed letters as English, Sogou's default),
	// "commit_text", "clear", or "off" (Shift doesn't switch).
	Shift string `json:"shift"`
	// InitialMode is the mode of a new input session, "chinese" or
	// "english".
	InitialMode string `json:"initial_mode"`
	// Punctuation is the starting punctuation style for a new session,
	// "chinese" or "english". The punctuation shortcut toggles it for that
	// session without changing this default.
	Punctuation string `json:"punctuation"`

	// Appearance (外观). Fronts only.

	// Theme is "system", "light" or "dark".
	Theme string `json:"theme"`
	// Skin is a skin id (Skins).
	Skin string `json:"skin"`
	// FontFamily is the candidate font family. Empty uses the system font;
	// a family unavailable on this machine falls back without changing it.
	FontFamily string `json:"font_family"`
	// FontSize is the candidate text size in points, 12–28.
	FontSize int `json:"font_size"`

	// Input (输入).

	// Fuzzy lists enabled fuzzy-pinyin pairs (FuzzyPairs), applied before
	// spelling conversion in double-pinyin schemes.
	Fuzzy []string `json:"fuzzy"`
	// SelectChar is the key pair for 以词定字 (commit the first or last
	// character of the candidate): a KeyPairs id or "off".
	SelectChar string `json:"select_char"`
	// PageKeys lists the KeyPairs ids that page the candidates, besides
	// Page Up/Down and the arrow keys. "tab" pages with Shift+Tab/Tab.
	PageKeys []string `json:"page_keys"`
	// MixedEnglish offers English words among the candidates (中英混输).
	MixedEnglish bool `json:"mixed_english"`
	// Emoji offers emoji among the candidates.
	Emoji bool `json:"emoji"`
	// Traditional outputs traditional characters (繁体).
	Traditional bool `json:"traditional"`
	// AutoSpace inserts a space where Chinese meets English or digits
	// while typing (DESIGN §7.1).
	AutoSpace bool `json:"auto_space"`
	// Rerank is 智能排序 (DESIGN §7.2): "off", "cautious" (保守) or
	// "standard" (标准). It needs shixin-ai and its model; it doesn't
	// reach the Rime configuration.
	Rerank string `json:"rerank"`

	// Shortcuts (快捷键): HotkeyActions ids → a Rime key name or "" for
	// none.
	Hotkeys map[string]string `json:"hotkeys"`
	// CandidateKeys chooses optional shortcuts for the second and third
	// candidates on the current page: "off" or a CandidateKeyPairs id.
	CandidateKeys string `json:"candidate_keys"`

	// Dictionary (词库).

	// Phrases are the user's custom phrases (自定义短语): typing Code
	// offers Text first.
	Phrases []Phrase `json:"phrases"`

	// Apps (应用).

	// Apps are per-app default modes, applied when the app gets focus.
	Apps []AppRule `json:"apps"`
	// RememberAppMode keeps each app's last 中/英 mode. Off, the mode is
	// shared by all apps (as in Sogou), except for apps in Apps.
	RememberAppMode bool `json:"remember_app_mode"`
	// NoAutoSpace lists the apps (ids as in AppRule) without auto spacing:
	// by default the developer tools, where a space can change meaning.
	NoAutoSpace []string `json:"no_auto_space"`
}

// Modes (InitialMode, AppRule.Mode) and punctuation styles (Punctuation).
const (
	ModeChinese = "chinese"
	ModeEnglish = "english"
)

// 智能排序 strengths (Rerank).
const (
	RerankOff      = "off"
	RerankCautious = "cautious" // 保守
	RerankStandard = "standard" // 标准
)

// DevApps are terminals and code editors: offered as English-mode rules in
// one click, and without auto spacing by default.
var DevApps = []string{
	"com.apple.Terminal", "com.googlecode.iterm2", "com.mitchellh.ghostty", "dev.warp.Warp-Stable",
	"com.microsoft.VSCode", "com.apple.dt.Xcode", "dev.zed.Zed", "com.todesktop.230313mzl4w4u92",
}

// Phrase is a custom phrase.
type Phrase struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// AppRule is an app's default mode.
type AppRule struct {
	ID   string `json:"id"`   // bundle id (macOS), executable name (Windows), app id (Linux)
	Name string `json:"name"` // display name, as found when the rule was added
	Mode string `json:"mode"` // "chinese" or "english"
}

// CurrentVersion is the settings format version.
const CurrentVersion = 1

// Default returns the settings of a new user.
func Default() Settings {
	return Settings{
		Version:       CurrentVersion,
		Scheme:        "rime_ice",
		PageSize:      5,
		Layout:        "horizontal",
		Shift:         "commit_code",
		InitialMode:   ModeChinese,
		Punctuation:   ModeChinese,
		Theme:         "system",
		Skin:          "shixin",
		FontSize:      17,
		Fuzzy:         []string{},
		SelectChar:    "brackets",
		PageKeys:      []string{"minus_equal"},
		MixedEnglish:  true,
		Emoji:         true,
		Hotkeys:       DefaultHotkeys(),
		CandidateKeys: "off",
		Rerank:        RerankOff,
		Phrases:       []Phrase{},
		Apps:          []AppRule{},
		NoAutoSpace:   slices.Clone(DevApps),
	}
}

// DefaultHotkeys returns the default shortcut for each action.
func DefaultHotkeys() map[string]string {
	m := map[string]string{}
	for _, a := range HotkeyActions {
		m[a.ID] = a.Choices[0]
	}
	return m
}

// Choice is an option of a setting: its stored value and its names.
type Choice struct{ ID, Name, NameEn string }

// Schemes lists the supported schemas, in menu order.
var Schemes = []Choice{
	{"rime_ice", "全拼", "Full pinyin"},
	{"double_pinyin", "自然码双拼", "Ziranma Shuangpin"},
	{"double_pinyin_flypy", "小鹤双拼", "Xiaohe Shuangpin"},
	{"double_pinyin_mspy", "微软双拼", "Microsoft Shuangpin"},
	{"double_pinyin_sogou", "搜狗双拼", "Sogou Shuangpin"},
	{"double_pinyin_abc", "智能 ABC 双拼", "Intelligent ABC Shuangpin"},
	{"double_pinyin_ziguang", "紫光双拼", "Ziguang Shuangpin"},
	{"double_pinyin_jiajia", "拼音加加双拼", "Pinyin Jiajia Shuangpin"},
}

// Skins lists the built-in candidate window skins; internal/ui/skin has
// their colors.
var Skins = []Choice{
	{"shixin", "拾新", "Shixin"},
	{"mist", "雾", "Mist"},
	{"ink", "墨", "Ink"},
	{"jade", "青瓷", "Celadon"},
	{"sakura", "樱", "Sakura"},
	{"amber", "琥珀", "Amber"},
}

// KeyPairs are the key pairs for 以词定字 and paging: {first/previous,
// last/next} as Rime key names.
var KeyPairs = []struct {
	ID   string
	Keys [2]string
	Name string // as shown
}{
	{"minus_equal", [2]string{"minus", "equal"}, "- ="},
	{"comma_period", [2]string{"comma", "period"}, ", ."},
	{"brackets", [2]string{"bracketleft", "bracketright"}, "[ ]"},
	{"tab", [2]string{"Shift+Tab", "Tab"}, "Shift+Tab / Tab"},
}

// HotkeyAction is a shortcut the settings offer.
type HotkeyAction struct {
	ID      string
	Toggle  string   // the Rime option toggled; "" for menu/editor actions
	Choices []string // Rime key names; the first is the default, "" is none
}

// HotkeyActions are the shortcuts the settings offer, each with the Rime
// key names it can be set to; the first is the default, "" is none.
var HotkeyActions = []HotkeyAction{
	{"punct", "ascii_punct", []string{"Control+period", "Control+Shift+3", ""}},
	{"traditional", "traditionalization", []string{"Control+Shift+F", "Control+Shift+4", "Control+slash", ""}},
	// No Shift+space (Sogou's): a binding on a printable key makes every
	// such key wait for the engine (an ASK), even without Shift.
	{"full_shape", "full_shape", []string{"", "Control+Shift+5", "Control+comma"}},
	{"switcher", "", []string{"Control+grave", "F4", ""}},
	{"forget", "", []string{"Control+Shift+BackSpace", "Control+Delete", ""}},
}

// CandidateKeyPairs are optional second/third-candidate shortcuts. Modifiers
// leave plain semicolon available to double-pinyin spelling, and apostrophe
// available as a syllable delimiter. They apply only while a menu is visible.
var CandidateKeyPairs = []struct {
	ID   string
	Keys [2]string
}{
	{"control", [2]string{"Control+semicolon", "Control+apostrophe"}},
	{"alt", [2]string{"Alt+semicolon", "Alt+apostrophe"}},
}

// CandidateKeyPair returns a preset's second- and third-candidate shortcuts.
// The off setting and unknown ids return false.
func CandidateKeyPair(id string) ([2]string, bool) {
	for _, pair := range CandidateKeyPairs {
		if pair.ID == id {
			return pair.Keys, true
		}
	}
	return [2]string{}, false
}

// FuzzyPairs are the fuzzy-pinyin options, each with the spelling algebra
// it derives in both directions.
var FuzzyPairs = []struct {
	ID    string
	Rules []string
}{
	{"z=zh", []string{`derive/^zh/z/`, `derive/^z([^h])/zh$1/`}},
	{"c=ch", []string{`derive/^ch/c/`, `derive/^c([^h])/ch$1/`}},
	{"s=sh", []string{`derive/^sh/s/`, `derive/^s([^h])/sh$1/`}},
	{"n=l", []string{`derive/^n/l/`, `derive/^l/n/`}},
	{"f=h", []string{`derive/^f/h/`, `derive/^h/f/`}},
	{"r=l", []string{`derive/^r/l/`, `derive/^l/r/`}},
	{"an=ang", []string{`derive/([^iu])an$/$1ang/`, `derive/([^iu])ang$/$1an/`, `derive/^an$/ang/`, `derive/^ang$/an/`}},
	{"en=eng", []string{`derive/en$/eng/`, `derive/eng$/en/`}},
	{"in=ing", []string{`derive/in$/ing/`, `derive/ing$/in/`}},
	{"ian=iang", []string{`derive/ian$/iang/`, `derive/iang$/ian/`}},
	{"uan=uang", []string{`derive/uan$/uang/`, `derive/uang$/uan/`}},
}

func hasChoice(cs []Choice, id string) bool {
	return slices.ContainsFunc(cs, func(c Choice) bool { return c.ID == id })
}

// KeyPair returns the keys of a KeyPairs id.
func KeyPair(id string) ([2]string, bool) {
	for _, p := range KeyPairs {
		if p.ID == id {
			return p.Keys, true
		}
	}
	return [2]string{}, false
}

// check validates one part of Settings; fix makes that part valid, keeping
// what it can and taking the rest from the defaults.
type check struct {
	valid func(s *Settings) error
	fix   func(s *Settings)
}

func oneOf(field, v string, ok ...string) error {
	if !slices.Contains(ok, v) {
		return fmt.Errorf("settings: unknown %s %q", field, v)
	}
	return nil
}

// checks covers every field. Errors never quote phrase text, which may be
// private and ends up in logs (AGENTS.md, privacy).
var checks = []check{
	{func(s *Settings) error {
		if len(s.FontFamily) > 256 || !utf8.ValidString(s.FontFamily) || strings.TrimSpace(s.FontFamily) != s.FontFamily || strings.ContainsAny(s.FontFamily, ",\"'") || strings.ContainsFunc(s.FontFamily, unicode.IsControl) {
			return fmt.Errorf("settings: invalid font_family")
		}
		return nil
	}, func(s *Settings) { s.FontFamily = Default().FontFamily }},
	{func(s *Settings) error {
		if !hasChoice(Schemes, s.Scheme) {
			return fmt.Errorf("settings: unknown scheme %q", s.Scheme)
		}
		return nil
	}, func(s *Settings) { s.Scheme = Default().Scheme }},
	{func(s *Settings) error {
		if s.PageSize < 3 || s.PageSize > 10 {
			return fmt.Errorf("settings: page_size %d out of range 3–10", s.PageSize)
		}
		return nil
	}, func(s *Settings) { s.PageSize = Default().PageSize }},
	{func(s *Settings) error {
		if s.FontSize < 12 || s.FontSize > 28 {
			return fmt.Errorf("settings: font_size %d out of range 12–28", s.FontSize)
		}
		return nil
	}, func(s *Settings) { s.FontSize = Default().FontSize }},
	{func(s *Settings) error {
		if !hasChoice(Skins, s.Skin) {
			return fmt.Errorf("settings: unknown skin %q", s.Skin)
		}
		return nil
	}, func(s *Settings) { s.Skin = Default().Skin }},
	{func(s *Settings) error { return oneOf("layout", s.Layout, "horizontal", "vertical") },
		func(s *Settings) { s.Layout = Default().Layout }},
	{func(s *Settings) error {
		return oneOf("shift behavior", s.Shift, "commit_code", "commit_text", "clear", "off")
	}, func(s *Settings) { s.Shift = Default().Shift }},
	{func(s *Settings) error { return oneOf("initial_mode", s.InitialMode, ModeChinese, ModeEnglish) },
		func(s *Settings) { s.InitialMode = Default().InitialMode }},
	{func(s *Settings) error { return oneOf("punctuation", s.Punctuation, ModeChinese, ModeEnglish) },
		func(s *Settings) { s.Punctuation = Default().Punctuation }},
	{func(s *Settings) error { return oneOf("rerank", s.Rerank, RerankOff, RerankCautious, RerankStandard) },
		func(s *Settings) { s.Rerank = Default().Rerank }},
	{func(s *Settings) error { return oneOf("theme", s.Theme, "system", "light", "dark") },
		func(s *Settings) { s.Theme = Default().Theme }},
	{func(s *Settings) error {
		for _, f := range s.Fuzzy {
			if fuzzyRules(f) == nil {
				return fmt.Errorf("settings: unknown fuzzy pair %q", f)
			}
		}
		return nil
	}, func(s *Settings) {
		s.Fuzzy = slices.DeleteFunc(s.Fuzzy, func(f string) bool { return fuzzyRules(f) == nil })
	}},
	{func(s *Settings) error {
		if s.SelectChar == "off" {
			return nil
		}
		if _, ok := KeyPair(s.SelectChar); !ok || s.SelectChar == "tab" {
			return fmt.Errorf("settings: unknown select_char keys %q", s.SelectChar)
		}
		return nil
	}, func(s *Settings) { s.SelectChar = Default().SelectChar }},
	{func(s *Settings) error {
		seen := map[string]bool{}
		for _, k := range s.PageKeys {
			if _, ok := KeyPair(k); !ok {
				return fmt.Errorf("settings: unknown page keys %q", k)
			}
			if k == s.SelectChar {
				return fmt.Errorf("settings: page keys %q are also used by select_char", k)
			}
			if seen[k] {
				return fmt.Errorf("settings: page keys %q listed twice", k)
			}
			seen[k] = true
		}
		return nil
	}, func(s *Settings) {
		seen := map[string]bool{}
		s.PageKeys = slices.DeleteFunc(s.PageKeys, func(k string) bool {
			_, ok := KeyPair(k)
			bad := !ok || k == s.SelectChar || seen[k]
			seen[k] = true
			return bad
		})
	}},
	{func(s *Settings) error {
		for id, key := range s.Hotkeys {
			if err := validHotkey(id, key); err != nil {
				return err
			}
		}
		return nil
	}, func(s *Settings) {
		hk := DefaultHotkeys()
		for id, key := range s.Hotkeys {
			if validHotkey(id, key) == nil {
				hk[id] = key
			}
		}
		s.Hotkeys = hk
	}},
	{func(s *Settings) error {
		if s.CandidateKeys == "off" {
			return nil
		}
		if _, ok := CandidateKeyPair(s.CandidateKeys); !ok {
			return fmt.Errorf("settings: unknown candidate_keys %q", s.CandidateKeys)
		}
		return nil
	}, func(s *Settings) { s.CandidateKeys = Default().CandidateKeys }},
	{func(s *Settings) error {
		seen := map[Phrase]bool{}
		for i := range s.Phrases {
			if err := s.Phrases[i].Validate(); err != nil {
				return fmt.Errorf("settings: phrase %d: %w", i+1, err)
			}
			if seen[s.Phrases[i]] {
				return fmt.Errorf("settings: phrase %d is a duplicate", i+1)
			}
			seen[s.Phrases[i]] = true
		}
		return nil
	}, func(s *Settings) {
		seen := map[Phrase]bool{}
		s.Phrases = slices.DeleteFunc(s.Phrases, func(p Phrase) bool {
			bad := p.Validate() != nil || seen[p]
			seen[p] = true
			return bad
		})
	}},
	{func(s *Settings) error {
		seen := map[string]bool{}
		for i, a := range s.Apps {
			if err := a.validate(); err != nil {
				return fmt.Errorf("settings: app rule %d: %w", i+1, err)
			}
			if seen[a.ID] {
				return fmt.Errorf("settings: app %q has two rules", a.ID)
			}
			seen[a.ID] = true
		}
		return nil
	}, func(s *Settings) {
		seen := map[string]bool{}
		s.Apps = slices.DeleteFunc(s.Apps, func(a AppRule) bool {
			bad := a.validate() != nil || seen[a.ID]
			seen[a.ID] = true
			return bad
		})
	}},
	{func(s *Settings) error {
		seen := map[string]bool{}
		for _, id := range s.NoAutoSpace {
			if !validAppID(id) {
				return fmt.Errorf("settings: invalid app id %q in no_auto_space", id)
			}
			if seen[id] {
				return fmt.Errorf("settings: app %q listed twice in no_auto_space", id)
			}
			seen[id] = true
		}
		return nil
	}, func(s *Settings) {
		seen := map[string]bool{}
		s.NoAutoSpace = slices.DeleteFunc(s.NoAutoSpace, func(id string) bool {
			bad := !validAppID(id) || seen[id]
			seen[id] = true
			return bad
		})
	}},
}

func validAppID(id string) bool { return id != "" && !strings.ContainsAny(id, "\n\t") }

func validHotkey(id, key string) error {
	i := slices.IndexFunc(HotkeyActions, func(a HotkeyAction) bool { return a.ID == id })
	if i < 0 {
		return fmt.Errorf("settings: unknown hotkey action %q", id)
	}
	if !slices.Contains(HotkeyActions[i].Choices, key) {
		return fmt.Errorf("settings: hotkey %q not offered for %s", key, id)
	}
	return nil
}

// Validate reports the first invalid field.
func (s *Settings) Validate() error {
	for _, c := range checks {
		if err := c.valid(s); err != nil {
			return err
		}
	}
	return nil
}

// repair resets the invalid fields of s and reports what was wrong.
func (s *Settings) repair() error {
	var errs []error
	for _, c := range checks {
		if err := c.valid(s); err != nil {
			errs = append(errs, err)
			c.fix(s)
		}
	}
	return errors.Join(errs...)
}

// Validate reports whether p can be written as a phrase table line. The
// error doesn't quote the phrase.
func (p *Phrase) Validate() error {
	if p.Text == "" || p.Text[0] == '#' || strings.ContainsAny(p.Text, "\t\r\n") {
		return errors.New("invalid text (empty, a tab or line break, or starting with #)")
	}
	if p.Code == "" {
		return errors.New("no code")
	}
	for _, r := range p.Code {
		if r < 'a' || r > 'z' {
			return errors.New("the code must be lowercase letters a–z")
		}
	}
	return nil
}

func (a *AppRule) validate() error {
	if !validAppID(a.ID) {
		return fmt.Errorf("invalid app id %q", a.ID)
	}
	return oneOf("app mode", a.Mode, ModeChinese, ModeEnglish)
}

func fuzzyRules(id string) []string {
	for _, p := range FuzzyPairs {
		if p.ID == id {
			return p.Rules
		}
	}
	return nil
}

// AppMode returns the default mode for app ("chinese", "english"), or ""
// when it has no rule.
func (s *Settings) AppMode(app string) string {
	for _, a := range s.Apps {
		if a.ID == app {
			return a.Mode
		}
	}
	return ""
}

// EngineChanged reports whether going from a to b needs the engine to
// reload: the generated Rime files differ. Appearance and layout changes
// don't, and the engine watches settings.json for its per-app rules and
// auto spacing.
func EngineChanged(a, b Settings) bool {
	fa, errA := Compile(a)
	fb, errB := Compile(b)
	if errA != nil || errB != nil || len(fa) != len(fb) {
		return true
	}
	for name, content := range fa {
		if !bytes.Equal(content, fb[name]) {
			return true
		}
	}
	return false
}

// Path returns the settings file for this user (DESIGN §5.3).
func Path() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// RimeUserDir returns Shixin's own Rime user dir, which keeps Squirrel's and
// Weasel's data untouched.
func RimeUserDir() (string, error) {
	return rimeUserDir()
}

// Load reads settings from path. A missing file yields the defaults; unset
// fields keep their defaults.
func Load(path string) (Settings, error) {
	s := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("settings: %w", err)
	}
	return Parse(b)
}

// Parse decodes settings.json content over the defaults. A field that is
// invalid (a hand edit, or a value from a newer version) is reset to its
// default, or the invalid entries of a list dropped; the rest is kept, and
// the error says what was reset. Only JSON that doesn't parse yields all
// defaults.
func Parse(b []byte) (Settings, error) {
	s := Default()
	s.Hotkeys = nil // a partial map in the file adds to the defaults below
	if err := json.Unmarshal(b, &s); err != nil {
		return Default(), fmt.Errorf("settings: %w", err)
	}
	hk := DefaultHotkeys()
	maps.Copy(hk, s.Hotkeys)
	s.Hotkeys = hk
	if s.Fuzzy == nil {
		s.Fuzzy = []string{}
	}
	if s.PageKeys == nil {
		s.PageKeys = []string{}
	}
	if s.Phrases == nil {
		s.Phrases = []Phrase{}
	}
	if s.Apps == nil {
		s.Apps = []AppRule{}
	}
	if s.NoAutoSpace == nil {
		s.NoAutoSpace = []string{}
	}
	return s, s.repair()
}

// Save writes settings to path atomically.
func Save(path string, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	s.Version = CurrentVersion
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'))
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Watch calls fn with the new settings whenever the file at path changes,
// checking every interval until stop is closed. It runs on the calling
// goroutine and only stats the file between changes. Invalid files are
// skipped.
func Watch(path string, interval time.Duration, stop <-chan struct{}, fn func(Settings)) {
	var last os.FileInfo
	if fi, err := os.Stat(path); err == nil {
		last = fi
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if last != nil && fi.ModTime().Equal(last.ModTime()) && fi.Size() == last.Size() {
			continue
		}
		last = fi
		if s, err := Load(path); err == nil {
			fn(s)
		}
	}
}
