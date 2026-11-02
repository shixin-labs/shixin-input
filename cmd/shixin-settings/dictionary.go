package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shixin/internal/dictionary"
	"shixin/internal/i18n"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/settings"
)

// mergeDictionary preserves current preferences and existing phrase/pin order.
// Validate the whole merge before any learned data is imported.
func mergeDictionary(s settings.Settings, b dictionary.Backup) (settings.Settings, error) {
	s = clone(s)
	phrases, pins := map[settings.Phrase]bool{}, map[settings.Pin]bool{}
	for _, p := range s.Phrases {
		phrases[p] = true
	}
	for _, p := range s.Pins {
		pins[p] = true
	}
	for _, p := range b.Phrases {
		if !phrases[p] {
			s.Phrases = append(s.Phrases, p)
			phrases[p] = true
		}
	}
	for _, p := range b.Pins {
		if !pins[p] {
			s.Pins = append(s.Pins, p)
			pins[p] = true
		}
	}
	return s, s.Validate()
}

func dictionaryRequest(action, path string) error {
	dir, err := ipc.RuntimeDir()
	if err != nil {
		return err
	}
	return lifecycle.Dictionary(ipc.EnginePath(dir), action, path)
}

func (a *app) dictionaryReady() bool {
	return !a.dictionaryBusy && !a.applying && reflect.DeepEqual(a.s, a.saved) && a.status != statusError
}

func (a *app) dictionaryDone(err error, message string) {
	update(a, func() {
		a.dictionaryBusy, a.dictionaryError = false, err != nil
		a.dictionaryMessage = message
		if err != nil {
			a.dictionaryMessage = T("s.d.transfer.failed") + ": " + err.Error()
		}
	})
}

func (a *app) importDictionary() {
	if !a.dictionaryReady() {
		return
	}
	a.dictionaryBusy, a.dictionaryMessage = true, ""
	snapshot := clone(a.s)
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: T("s.d.import"), Filters: []mygo.FileFilter{{Name: T("s.d.files"), Extensions: []string{"scel", "txt", "json"}}}})
		if err != nil || len(paths) == 0 {
			a.dictionaryDone(err, "")
			return
		}
		b, err := dictionary.ReadFile(paths[0])
		if err == nil {
			_, err = mergeDictionary(snapshot, b)
		}
		if err != nil {
			a.dictionaryDone(err, "")
			return
		}
		if len(b.Words) > 0 {
			// Only normalized portable entries reach the coordinator. The
			// selected external file may disappear after parsing.
			f, makeErr := os.CreateTemp(filepath.Dir(a.path), ".dictionary-import-")
			if makeErr != nil {
				a.dictionaryDone(makeErr, "")
				return
			}
			defer os.Remove(f.Name())
			err = dictionary.WriteTable(f, b.Words)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = dictionaryRequest("import", f.Name())
			}
		}
		if err != nil {
			a.dictionaryDone(err, "")
			return
		}
		update(a, func() {
			merged, mergeErr := mergeDictionary(a.s, b)
			if mergeErr != nil {
				a.dictionaryBusy = false
				a.dictionaryError = true
				a.dictionaryMessage = T("s.d.transfer.failed")
				return
			}
			a.s = merged
			a.dictionaryBusy, a.dictionaryError = false, false
			a.dictionaryMessage = fmt.Sprintf(T("s.d.imported"), len(b.Words), len(b.Phrases), len(b.Pins))
		})
	}()
}

func (a *app) exportDictionary() {
	if !a.dictionaryReady() {
		return
	}
	a.dictionaryBusy, a.dictionaryMessage = true, ""
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, Title: T("s.d.export"), DefaultPath: "Shixin-dictionary.json", Filters: []mygo.FileFilter{{Name: T("s.d.backup.file"), Extensions: []string{"json"}}, {Name: T("s.d.table.file"), Extensions: []string{"txt"}}}})
		if err != nil || path == "" {
			a.dictionaryDone(err, "")
			return
		}
		if filepath.Ext(path) == "" {
			path += ".json"
		}
		err = dictionaryRequest("export", path)
		a.dictionaryDone(err, T("s.d.exported"))
	}()
}

func (a *app) clearDictionary() {
	if !a.dictionaryReady() {
		return
	}
	a.dictionaryBusy, a.dictionaryMessage = true, ""
	go func() { a.dictionaryDone(dictionaryRequest("clear", ""), T("s.d.cleared")) }()
}

func (a *app) dictionaryTools(c *ui.Context) {
	ready := a.dictionaryReady()
	group(c, T("s.d.transfer"), T("s.d.transfer.note"), func() {
		row(c, T("s.d.import"), T("s.d.import.desc"), func() { ui.Button(c, T("s.d.import.button")).Disabled(!ready).OnClick(a.importDictionary) })
		row(c, T("s.d.export"), T("s.d.export.desc"), func() { ui.Button(c, T("s.d.export.button")).Disabled(!ready).OnClick(a.exportDictionary) })
		row(c, T("s.d.clear"), T("s.d.clear.desc"), func() {
			ui.Button(c, T("s.d.clear.button")).Disabled(!ready).OnClick(func() { a.confirmClearDictionary = true })
		})
		row(c, T("s.d.recovery"), T("s.d.recovery.desc"), func() {
			ui.Button(c, T("s.open")).OnClick(func() { go mygo.Shell.OpenPath(filepath.Join(rimeDir(), "backups")) })
		})
		if a.dictionaryMessage != "" {
			block(c, func() {
				color := colors(c).muted
				if a.dictionaryError {
					color = c.Theme().Danger
				}
				ui.Text(c, a.dictionaryMessage).FontSize(12).TextColor(color)
			})
		}
	})
	if ui.AlertDialog(c, &a.confirmClearDictionary, T("s.d.clear.confirm"), T("s.d.clear.confirm.desc"), T("s.cancel"), T("s.d.clear.button")) == 1 {
		a.clearDictionary()
	}
}

func (a *app) candidatePins(c *ui.Context) {
	s := &a.s
	pin := settings.Pin{Scheme: s.Scheme, Code: strings.ToLower(strings.TrimSpace(a.newPinCode)), Text: strings.TrimSpace(a.newPinText)}
	trial := clone(*s)
	trial.Pins = append(trial.Pins, pin)
	canAdd := trial.Validate() == nil
	group(c, T("s.d.pins"), T("s.d.pins.note"), func() {
		row(c, T("s.d.pin.layout"), "", func() {
			for _, layout := range settings.Schemes {
				if layout.ID == s.Scheme {
					name := layout.Name
					if i18n.Current() == i18n.English {
						name = layout.NameEn
					}
					ui.Text(c, name).FontSize(12).TextColor(colors(c).muted)
				}
			}
		})
		block(c, func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c.Key("pin-code"), &a.newPinCode).Placeholder(T("s.d.pin.code")).Width(130)
				ui.TextInput(c.Key("pin-text"), &a.newPinText).Placeholder(T("s.d.pin.text")).Grow(1)
				ui.Button(c, T("s.d.pin.add")).Disabled(!canAdd).OnClick(func() { s.Pins = append(s.Pins, pin); a.newPinCode, a.newPinText = "", "" })
			})
			if (pin.Code != "" || pin.Text != "") && !canAdd {
				ui.Text(c, T("s.d.pin.invalid")).FontSize(11.5).TextColor(c.Theme().Danger)
			}
		})
		for i, p := range s.Pins {
			if p.Scheme != s.Scheme {
				continue
			}
			previous := -1
			for j := 0; j < i; j++ {
				if s.Pins[j].Scheme == p.Scheme && s.Pins[j].Code == p.Code {
					previous = j
				}
			}
			ui.Row(c.Key(p)).Padding(8, 14).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.Code).FontSize(12).Font("monospace").Width(110)
				ui.Text(c, p.Text).FontSize(13).Grow(1).SingleLine().Ellipsis("…")
				ui.Button(c, "↑").Label(T("s.d.pin.up")).Disabled(previous < 0).OnClick(func() { movePinUp(s.Pins, i, previous, p) })
				iconButton(c, iconTrash, T("s.d.pin.remove")).OnClick(func() { s.Pins = slices.DeleteFunc(s.Pins, func(x settings.Pin) bool { return x == p }) })
			})
		}
	})
}

// movePinUp swaps the pins at i and previous. The indexes come from the
// rendered frame; ignore the click if the list changed since.
func movePinUp(pins []settings.Pin, i, previous int, p settings.Pin) {
	if previous < 0 || i >= len(pins) || previous >= i || pins[i] != p {
		return
	}
	pins[i], pins[previous] = pins[previous], pins[i]
}
