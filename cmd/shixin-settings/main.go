// Command shixin-settings is the settings app (DESIGN §6), a separate MyGo
// process so heavy pages can never affect the input method.
//
// Changes apply as they are made, as in System Settings: shortly after the
// last edit the app writes settings.json. A change to the Rime config or the
// engine's options builds and warms a replacement generation while the old
// engine keeps serving. Appearance changes reach the front through its
// settings.json watcher.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"maps"
	"os"
	"reflect"
	"slices"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"shixin/internal/i18n"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/settings"
	"shixin/internal/ui/fonts"
	shixupdate "shixin/internal/update"
)

// version is set with -ldflags "-X main.version=...".
var version = "dev"

// applyDelay is how long after the last edit settings are written, so a
// slider drag or typing doesn't start a build at every step.
const applyDelay = 700 * time.Millisecond

type status int

const (
	statusSaved    status = iota
	statusPending         // edited, not yet written
	statusApplying        // written; a replacement generation is building
	statusError
)

type app struct {
	win  *mygo.Window
	path string

	saved  settings.Settings // as written to settings.json
	s      settings.Settings // bound to the controls
	seen   settings.Settings // s as of the last frame, to time edits
	editAt time.Time

	status status
	err    string
	sticky bool               // err is about the file or the engine, not the edit: it stays until an apply succeeds
	failed *settings.Settings // the edit whose apply failed; not retried until edited again
	backup bool               // settings.json needed repair: keep the original before overwriting it

	applying   bool // a hot swap is in flight
	applyAgain bool // settings changed during it: apply once more after it

	// Hooks, replaced in tests.
	saveFile    func(settings.Settings) error
	applyEngine func(done func(error))

	page       string
	chartShown bool
	fuzzyShown bool     // 模糊音's details sheet
	quickShown bool     // 快捷输入's codes sheet
	fonts      []string // installed families, enumerated once at startup

	// 词库
	newCode, newText, phraseQuery string

	// 应用
	installed []mygo.InstalledApp
	icons     map[string]*ui.Bitmap // by app id
	loading   map[string]bool
	picking   bool
	pickSpace bool // the picker adds to NoAutoSpace, not a mode rule
	appQuery  string

	// 开始使用
	added      bool        // 拾新 is in the user's input sources
	startShown atomic.Bool // the guide is showing: pollAdded runs only then

	// 智能排序 (输入)
	model modelState

	// 性能, and 智能排序's status on 输入
	statsShown atomic.Bool
	stats      *ipc.Stats
	statsAt    time.Time
	resetHist  atomic.Bool

	// 关于
	confirmReset bool
	upd          updateState
	front        string                                                                 // Shixin.app's front, "" outside the bundle
	checkRelease func(ctx context.Context, current string) (*shixupdate.Release, error) // replaced in tests
}

func newApp(path string, s settings.Settings) *app {
	a := &app{path: path, saved: clone(s), s: clone(s), seen: clone(s), page: "general", icons: map[string]*ui.Bitmap{}, loading: map[string]bool{}}
	a.fonts = fonts.Families()
	a.saveFile = func(s settings.Settings) error { return settings.Save(a.path, s) }
	a.applyEngine = applyEngine
	a.checkRelease = shixupdate.Check
	a.front = plat.front()
	a.refreshModel()
	return a
}

// clone deep-copies s, so edits through the controls never alias saved.
func clone(s settings.Settings) settings.Settings {
	s.Fuzzy = slices.Clone(s.Fuzzy)
	s.PageKeys = slices.Clone(s.PageKeys)
	s.Phrases = slices.Clone(s.Phrases)
	s.Apps = slices.Clone(s.Apps)
	s.Hotkeys = maps.Clone(s.Hotkeys)
	return s
}

// sync runs at the end of every frame: it notes edits and applies them
// once the user pauses.
func (a *app) sync(c *ui.Context) {
	// Keys chosen for 以词定字 no longer page.
	a.s.PageKeys = slices.DeleteFunc(a.s.PageKeys, func(k string) bool { return k == a.s.SelectChar })
	now := c.Now()
	if !reflect.DeepEqual(a.s, a.seen) {
		a.seen = clone(a.s)
		a.editAt = now
	}
	if reflect.DeepEqual(a.s, a.saved) {
		// Back to what is saved: a pending edit or an invalid one is gone.
		if a.status == statusPending || a.status == statusError && !a.sticky {
			a.setIdle()
		}
		return
	}
	if a.failed != nil && reflect.DeepEqual(a.s, *a.failed) {
		return // shown as an error; retried once edited again
	}
	if wait := applyDelay - now.Sub(a.editAt); wait > 0 {
		a.status = statusPending
		c.After(wait)
		return
	}
	a.apply()
}

// setIdle shows the state with nothing left to write.
func (a *app) setIdle() {
	a.err, a.sticky = "", false
	if a.applying {
		a.status = statusApplying
	} else {
		a.status = statusSaved
	}
}

func (a *app) fail(err error, sticky bool) {
	a.status, a.err, a.sticky = statusError, err.Error(), sticky
}

// apply writes the edited settings and requests a hot swap if needed.
func (a *app) apply() {
	a.failed = nil
	if err := a.s.Validate(); err != nil {
		a.fail(err, false)
		f := clone(a.s)
		a.failed = &f
		return
	}
	reload := settings.EngineChanged(a.saved, a.s)
	if a.backup {
		// The file had invalid parts that were reset on load: keep the
		// original next to it before replacing it.
		os.Rename(a.path, a.path+".bad")
		a.backup = false
	}
	if err := a.saveFile(a.s); err != nil {
		a.fail(err, true)
		f := clone(a.s)
		a.failed = &f
		return
	}
	a.saved = clone(a.s)
	if reload {
		a.requestApply()
		return
	}
	a.setIdle()
}

// requestApply serializes builds and follows with the latest saved edit
// when settings changed during an apply.
func (a *app) requestApply() {
	a.status, a.err, a.sticky = statusApplying, "", false
	if a.applying {
		a.applyAgain = true
		return
	}
	a.applying = true
	a.applyEngine(func(err error) {
		update(a, func() {
			a.applying = false
			if a.applyAgain {
				a.applyAgain = false
				a.requestApply()
				return
			}
			switch {
			case err != nil:
				a.fail(err, true)
			case a.status == statusApplying:
				a.status = statusSaved
			}
		})
	})
}

// update runs fn on the main thread and redraws; directly in tests.
func update(a *app, fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

// applyEngine waits for the coordinator's build, warm-up and activation.
// Build and warm-up errors leave the current generation serving.
func applyEngine(done func(error)) {
	go func() {
		dir, err := ipc.RuntimeDir()
		if err == nil {
			err = lifecycle.Apply(ipc.EnginePath(dir))
		}
		if err != nil {
			err = fmt.Errorf("%s: %w", i18n.T("s.err.apply"), err)
		}
		done(err)
	}()
}

// resetDefaults restores the default settings, keeping the user's phrases
// and app rules, which aren't preferences but data.
func (a *app) resetDefaults() {
	d := settings.Default()
	d.Phrases, d.Apps = a.s.Phrases, a.s.Apps
	a.s = clone(d)
}

func rimeDir() string {
	dir, _ := settings.RimeUserDir()
	return dir
}

func main() {
	path, err := settings.Path()
	if err != nil {
		log.Fatal(err)
	}
	welcome := flag.Bool("welcome", false, "open the first-run guide (Shixin.app does after installing)")
	pageFlag := flag.String("page", "", "open this page (the installer reopens 关于 after an update)")
	flag.Parse()
	s, loadErr := settings.Load(path)
	a := newApp(path, s)
	if *welcome {
		a.page = "start"
	}
	for _, pg := range pages {
		if *pageFlag != "" && pg.id == *pageFlag {
			a.page = pg.id
		}
	}
	if loadErr != nil {
		a.fail(fmt.Errorf("%s: %w", i18n.T("s.err.repaired"), loadErr), true)
		a.backup = true
	}
	mygo.App.SetName(i18n.T("settings.title"))
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:          i18n.T("settings.title"),
			Width:          880,
			Height:         640,
			MinWidth:       720,
			MinHeight:      480,
			StateKey:       "settings",
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: titleBarH,
			Vibrancy:       mygo.VibrancySidebar,
			Content:        ui.View(a.view),
		})
		go a.loadApps()
		go a.pollStats()
		go a.pollAdded()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
