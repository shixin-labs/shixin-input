// Command shixin-engine supervises private engine generations. The default
// coordinator never loads librime; -worker owns it on one locked thread.
package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"shixin/internal/engine"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/logx"
	"shixin/internal/settings"
)

var version = "dev"

type config struct{ lib, data, prebuilt, user, settings, socket, log, generation, builder, public string }

func main() {
	res := resources()
	var c config
	flag.StringVar(&c.lib, "lib", res.lib, "librime shared library")
	flag.StringVar(&c.data, "data", res.data, "shared Rime data (read-only)")
	flag.StringVar(&c.prebuilt, "prebuilt", res.prebuilt, "prebuilt dictionaries")
	flag.StringVar(&c.user, "user", "", "Rime user dir")
	flag.StringVar(&c.settings, "settings", "", "settings.json")
	flag.StringVar(&c.socket, "socket", "", "public engine socket")
	flag.StringVar(&c.log, "log", "", "log path (- for stderr; default: platform log directory)")
	flag.StringVar(&c.generation, "generation", "", "private generation (worker only)")
	flag.StringVar(&c.public, "public", "", "stable discovery endpoint (worker only)")
	flag.StringVar(&c.builder, "builder", "", "builder executable (default: sibling)")
	worker := flag.Bool("worker", false, "run a generation under a coordinator")
	capture := flag.Bool("capture", false, "serve a warm-only generation for offline menu capture (own scratch user/socket)")
	flag.Parse()
	if *capture {
		// Capture never opens the developer's ordinary log path.
		c.log = "-"
	} else {
		logSpecified := false
		flag.Visit(func(f *flag.Flag) { logSpecified = logSpecified || f.Name == "log" })
		if !logSpecified {
			c.log = logx.DefaultPath("engine.log")
		}
	}
	var logger *logx.Logger
	if c.log != "" && c.log != "-" {
		if l, err := logx.Open(c.log); err == nil {
			logger = l
		}
	}
	if logger == nil {
		logger = logx.New(os.Stderr)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	debug.SetGCPercent(400)
	debug.SetMemoryLimit(512 << 20)
	var err error
	if *capture {
		err = captureConfig(&c, *worker)
		if err == nil {
			err = runCapture(c, logger.Printf, sig, os.Stdin, os.Stdout)
		}
	} else if err = defaults(&c); err == nil {
		if *worker {
			err = runWorker(c, logger.Printf, sig)
		} else {
			err = runCoordinator(c, logger.Printf, sig)
		}
	}
	if err != nil {
		logger.Printf("engine: %v", err)
	}
	logger.Close()
	if err != nil {
		if errors.Is(err, errLocked) {
			os.Exit(lifecycle.ExitLocked)
		}
		os.Exit(1)
	}
}

func defaults(c *config) error {
	var err error
	if c.user == "" {
		c.user, err = settings.RimeUserDir()
		if err != nil {
			return err
		}
	}
	if c.settings == "" {
		c.settings, err = settings.Path()
		if err != nil {
			return err
		}
	}
	if c.socket == "" {
		dir, err := ipc.RuntimeDir()
		if err != nil {
			return err
		}
		c.socket = ipc.EnginePath(dir)
	}
	if c.builder == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		c.builder = filepath.Join(filepath.Dir(exe), "shixin-builder"+executableSuffix())
	}
	for _, p := range []*string{&c.data, &c.user, &c.settings, &c.socket, &c.builder} {
		*p, err = filepath.Abs(*p)
		if err != nil {
			return err
		}
	}
	if c.prebuilt != "" {
		c.prebuilt, err = filepath.Abs(c.prebuilt)
	}
	return err
}

// appModes converts the settings' per-app rules for the engine.
func appModes(s *settings.Settings) engine.AppModes {
	m := engine.AppModes{
		English:     map[string]bool{},
		Remember:    s.RememberAppMode,
		AutoSpace:   s.AutoSpace,
		NoAutoSpace: map[string]bool{},
	}
	for _, a := range s.Apps {
		m.English[a.ID] = a.Mode == settings.ModeEnglish
	}
	for _, id := range s.NoAutoSpace {
		m.NoAutoSpace[id] = true
	}
	return m
}

type paths struct{ lib, data, prebuilt string }

// resources finds librime and the Rime data: inside the app bundle when the
// engine runs from Shixin.app/Contents/Helpers, else the developer setup
// (Homebrew librime, scripts/fetch-rime-data.sh).
func resources() paths {
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		if runtime.GOOS == "windows" {
			dir := filepath.Dir(exe)
			p := paths{lib: filepath.Join(dir, "rime.dll"), data: filepath.Join(dir, "data")}
			if _, err := os.Stat(filepath.Join(dir, "prebuilt")); err == nil {
				p.prebuilt = filepath.Join(dir, "prebuilt")
			}
			return p
		}
		if contents := filepath.Dir(filepath.Dir(exe)); strings.HasSuffix(contents, ".app/Contents") {
			p := paths{
				lib:  filepath.Join(contents, "Frameworks", "librime.1.dylib"),
				data: filepath.Join(contents, "Resources", "rime-data"),
			}
			if _, err := os.Stat(p.lib); err != nil {
				p.lib = homebrewLibrime // until bundles ship our own librime build
			}
			if _, err := os.Stat(filepath.Join(contents, "Resources", "rime-prebuilt")); err == nil {
				p.prebuilt = filepath.Join(contents, "Resources", "rime-prebuilt")
			}
			return p
		}
	}
	return paths{lib: homebrewLibrime, data: "build/rime-data"}
}

const homebrewLibrime = "/opt/homebrew/opt/librime/lib/librime.1.dylib"

// keepWarm re-reads the compiled dictionaries periodically, so the OS keeps
// them in the page cache through long idle periods and the first key after
// a break doesn't fault them in from disk (DESIGN §3.4). It runs at low
// priority on its own goroutine, never on the engine thread.
func keepWarm(logf func(string, ...any), dirs ...string) {
	buf := make([]byte, 1<<20)
	for {
		touchData(logf, buf, dirs...)
		time.Sleep(10 * time.Minute)
	}
}

// touchData runs before startup and periodically off the engine thread.
func touchData(logf func(string, ...any), buf []byte, dirs ...string) {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(dir, "*.bin"))
		for _, path := range files {
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			if _, err := io.CopyBuffer(io.Discard, onlyReader{f}, buf); err != nil && !errors.Is(err, io.EOF) {
				logf("prefault %s: %v", filepath.Base(path), err)
			}
			f.Close()
		}
	}
}

// onlyReader hides WriterTo/ReaderFrom so CopyBuffer really reads the file
// through buf instead of using sendfile-like shortcuts.
type onlyReader struct{ r io.Reader }

func (o onlyReader) Read(p []byte) (int, error) { return o.r.Read(p) }

func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
