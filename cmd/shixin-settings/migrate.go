package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"

	"shixin/internal/dictionary"
)

// ---------------------------------------------------------------- 迁移

// Settings looks for the dictionaries of input methods installed on this
// computer (internal/dictionary.FindSources) and lists them in the guide and
// on the Dictionary page, each with an Import button. What was imported is
// kept in dictionary-sources.json beside settings.json; it is not a setting.

const sourceImported = "imported"

func (a *app) sourceStatePath() string {
	return filepath.Join(filepath.Dir(a.path), "dictionary-sources.json")
}

func loadSourceState(path string) map[string]string {
	state := map[string]string{}
	if b, err := os.ReadFile(path); err == nil && len(b) < 64<<10 {
		json.Unmarshal(b, &state)
	}
	if state == nil { // the file held null
		state = map[string]string{}
	}
	return state
}

// saveSourceState writes the state on the UI thread's copy; it is small, and
// losing it only means asking again.
func (a *app) saveSourceState() {
	b, err := json.MarshalIndent(a.sourceState, "", "  ")
	if err != nil {
		return
	}
	tmp := a.sourceStatePath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, a.sourceStatePath())
	}
}

// findSources runs once at startup, off the UI thread.
func (a *app) findSources() {
	home, _ := os.UserHomeDir()
	sources := dictionary.FindSources(dictionary.Env{GOOS: runtime.GOOS, Home: home, AppData: os.Getenv("APPDATA"), Own: rimeDir()})
	state := loadSourceState(a.sourceStatePath())
	update(a, func() { a.sourcesLoaded(sources, state) })
}

func (a *app) sourcesLoaded(sources []dictionary.Source, state map[string]string) {
	a.sources, a.sourceState, a.sourcesFound = sources, state, true
}

func sourceIME(s dictionary.Source) string { return T("s.d.ime." + s.IME) }

// sourceTitle and sourceDesc describe a source in a row.
func sourceTitle(s dictionary.Source) string {
	if s.Cell {
		return fmt.Sprintf(T("s.d.src.cell"), sourceIME(s), s.Name)
	}
	return sourceIME(s)
}

func sourceDesc(s dictionary.Source) string {
	switch {
	case s.Rime:
		return fmt.Sprintf(T("s.d.src.rime"), s.Name)
	case s.Cell:
		return fmt.Sprintf(T("s.d.src.cell.desc"), s.Words)
	}
	return fmt.Sprintf(T("s.d.src.user"), s.Words)
}

// importSources imports each source in turn on a background goroutine.
func (a *app) importSources(sources []dictionary.Source) {
	if !a.dictionaryReady() || len(sources) == 0 {
		return
	}
	a.dictionaryBusy, a.dictionaryMessage = true, ""
	go func() { update(a, a.runImport(sources)) }()
}

// runImport imports off the UI thread, stopping at the first failure, and
// returns what to show on it. File sources are parsed again: the other
// input method may have learned more words since startup.
func (a *app) runImport(sources []dictionary.Source) func() {
	var done []dictionary.Source
	var err error
	for _, s := range sources {
		if s.Rime {
			err = a.requestDictionary("import-rime", s.Path)
		} else {
			var b dictionary.Backup
			if b, err = dictionary.ReadFile(s.Path); err == nil {
				err = a.importWords(b.Words)
			}
		}
		if err != nil {
			break
		}
		done = append(done, s)
	}
	return func() {
		names := make([]string, len(done))
		for i, s := range done {
			names[i] = sourceTitle(s)
			a.sourceState[s.ID] = sourceImported
		}
		if len(done) > 0 {
			a.saveSourceState()
		}
		a.dictionaryBusy, a.dictionaryError = false, err != nil
		if err != nil {
			a.dictionaryMessage = T("s.d.transfer.failed") + ": " + err.Error()
		} else {
			a.dictionaryMessage = fmt.Sprintf(T("s.d.src.imported"), strings.Join(names, T("s.d.src.and")))
		}
	}
}

// sourceRows lists what can be imported from other input methods, for the
// guide and the Dictionary page; the guide also shows the outcome, which
// the Dictionary page shows under 迁移与备份.
func (a *app) sourceRows(c *ui.Context, message bool) {
	ready := a.dictionaryReady()
	switch {
	case a.dictionaryBusy:
		block(c, func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, T("s.d.busy")).FontSize(12.5)
			})
		})
		return
	case !a.sourcesFound:
		row(c, T("s.d.src.finding"), "", func() { ui.Spinner(c).Size(14, 14) })
	case len(a.sources) == 0:
		row(c, T("s.d.src.none"), T("s.d.src.none.desc"), nil)
	}
	for _, s := range a.sources {
		row(c.Key(s.ID), sourceTitle(s), sourceDesc(s), func() {
			if a.sourceState[s.ID] == sourceImported {
				quiet(c, T("s.d.src.done"), iconCheck)
			}
			label := T("s.d.src.import")
			if a.sourceState[s.ID] == sourceImported {
				label = T("s.d.src.again")
			}
			ui.Button(c, label).Disabled(!ready).OnClick(func() { a.importSources([]dictionary.Source{s}) })
		})
	}
	row(c, T("s.d.import"), T("s.d.import.desc"), func() {
		ui.Button(c, T("s.d.import.button")).Disabled(!ready).OnClick(a.importDictionary)
	})
	if message && a.dictionaryMessage != "" {
		block(c, func() {
			color := colors(c).muted
			if a.dictionaryError {
				color = c.Theme().Danger
			}
			ui.Text(c, a.dictionaryMessage).FontSize(12).TextColor(color)
		})
	}
}
