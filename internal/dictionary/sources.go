package dictionary

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Source is a dictionary another input method keeps on this computer, found
// without running or changing that input method.
type Source struct {
	// ID is stable across runs, so Settings can remember what the user
	// imported or declined.
	ID string `json:"id"`
	// IME is "sogou", "squirrel", "weasel", "fcitx5-rime" or "ibus-rime".
	IME string `json:"ime"`
	// Cell marks an optional Sogou cell dictionary (细胞词库) rather than
	// words the user typed.
	Cell bool `json:"cell,omitempty"`
	// Name is a cell dictionary's title or a Rime dictionary's name.
	Name string `json:"name,omitempty"`
	// Path is a file Read accepts, or a Rime LevelDB directory (Rime set),
	// which only the engine may convert from a private copy.
	Path string `json:"path"`
	Rime bool   `json:"rime,omitempty"`
	// Words counts the importable words of a file source; 0 for Rime.
	Words int `json:"words,omitempty"`
}

// Env says where to look. Settings passes the real directories; tests
// pass scratch ones.
type Env struct {
	GOOS    string
	Home    string
	AppData string // %APPDATA% on Windows
	// Own is Shixin's Rime user directory, never offered as a source.
	Own string
}

// RimePinyinDictionaries are the Rime dictionaries whose user databases use
// spaced full-pinyin codes, the only codes Shixin's dictionary accepts.
var RimePinyinDictionaries = []string{"rime_ice", "luna_pinyin", "pinyin_simp"}

// RimeSourceName returns the dictionary name of a Rime user database
// directory such as ".../luna_pinyin.userdb", or "" when it isn't one.
func RimeSourceName(path string) string {
	name, ok := strings.CutSuffix(filepath.Base(path), ".userdb")
	if !ok || !slices.Contains(RimePinyinDictionaries, name) {
		return ""
	}
	return name
}

// FindSources lists importable dictionaries. It reads and parses file
// sources, which takes a moment, so call it off the UI thread. Missing or
// unreadable sources are left out.
func FindSources(env Env) []Source {
	var out []Source
	if env.GOOS == "darwin" && env.Home != "" {
		sogou := filepath.Join(env.Home, "Library", "Application Support", "Sogou", "InputMethod", "SogouPY")
		if b, err := ReadFile(filepath.Join(sogou, "sgim_gd_usr.bin")); err == nil && len(b.Words) > 0 {
			out = append(out, Source{ID: "sogou:user", IME: "sogou", Path: filepath.Join(sogou, "sgim_gd_usr.bin"), Words: len(b.Words)})
		}
		cells, _ := filepath.Glob(filepath.Join(sogou, "scd", "*.scel"))
		slices.Sort(cells)
		for _, path := range cells {
			if s, ok := cellSource(path); ok {
				out = append(out, s)
			}
		}
	}
	var rime []struct{ ime, dir string }
	switch env.GOOS {
	case "darwin":
		if env.Home != "" {
			rime = append(rime, struct{ ime, dir string }{"squirrel", filepath.Join(env.Home, "Library", "Rime")})
		}
	case "windows":
		if env.AppData != "" {
			rime = append(rime, struct{ ime, dir string }{"weasel", filepath.Join(env.AppData, "Rime")})
		}
	case "linux":
		if env.Home != "" {
			rime = append(rime,
				struct{ ime, dir string }{"fcitx5-rime", filepath.Join(env.Home, ".local", "share", "fcitx5", "rime")},
				struct{ ime, dir string }{"ibus-rime", filepath.Join(env.Home, ".config", "ibus", "rime")})
		}
	}
	for _, r := range rime {
		if env.Own != "" && sameDir(r.dir, env.Own) {
			continue
		}
		for _, name := range RimePinyinDictionaries {
			path := filepath.Join(r.dir, name+".userdb")
			// A LevelDB directory always has CURRENT; Lstat keeps links out.
			if info, err := os.Lstat(path); err != nil || !info.IsDir() {
				continue
			}
			if info, err := os.Lstat(filepath.Join(path, "CURRENT")); err != nil || !info.Mode().IsRegular() {
				continue
			}
			out = append(out, Source{ID: r.ime + ":" + name, IME: r.ime, Name: name, Path: path, Rime: true})
		}
	}
	return out
}

func cellSource(path string) (Source, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return Source{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Source{}, false
	}
	b, err := Read(bytes.NewReader(raw))
	if err != nil || len(b.Words) == 0 {
		return Source{}, false
	}
	name := SCELName(raw)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".scel")
	}
	return Source{ID: "sogou:cell:" + filepath.Base(path), IME: "sogou", Cell: true, Name: name, Path: path, Words: len(b.Words)}, true
}

func sameDir(a, b string) bool {
	if x, err := filepath.EvalSymlinks(a); err == nil {
		a = x
	}
	if x, err := filepath.EvalSymlinks(b); err == nil {
		b = x
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
