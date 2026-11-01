// Command shixin-builder compiles a private Rime generation. It never
// receives the live user directory and never creates librime sessions.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"shixin/internal/generation"
	"shixin/internal/settings"
)

func main() {
	var o generation.Options
	flag.StringVar(&o.Lib, "lib", defaultLibrary(), "librime shared library")
	flag.StringVar(&o.Data, "data", "build/rime-data", "shared Rime data (read-only)")
	flag.StringVar(&o.Prebuilt, "prebuilt", "", "prebuilt dictionaries (optional)")
	flag.StringVar(&o.ReuseGeneration, "reuse-generation", "", "copy compiled resources from a validated warm-only generation")
	flag.StringVar(&o.Dir, "output", "", "new generation directory (must not exist)")
	path := flag.String("settings", "", "settings snapshot (empty: defaults)")
	status := flag.String("status-file", "", "private file for safe build-stage diagnostics")
	flag.BoolVar(&o.All, "all", false, "compile every supported schema for packaging")
	flag.Parse()
	if *status != "" {
		f, err := os.OpenFile(*status, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shixin-builder: cannot create status file")
			os.Exit(1)
		}
		defer f.Close()
		o.Progress = func(stage string) { f.Truncate(0); f.Seek(0, 0); f.WriteString(stage) }
		o.Progress("settings")
	}
	if o.Dir == "" {
		fmt.Fprintln(os.Stderr, "shixin-builder: -output is required")
		os.Exit(2)
	}
	s, err := settings.Load(*path)
	if err == nil {
		o.Settings = s
		t := time.Now()
		err = generation.Build(o)
		if err == nil {
			fmt.Fprintf(os.Stderr, "built generation in %v\n", time.Since(t).Round(time.Millisecond))
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "shixin-builder: %v\n", err)
		os.Exit(1)
	}
}

func defaultLibrary() string {
	if runtime.GOOS == "windows" {
		exe, err := os.Executable()
		if err == nil {
			return filepath.Join(filepath.Dir(exe), "rime.dll")
		}
		return "rime.dll"
	}
	return "/opt/homebrew/opt/librime/lib/librime.1.dylib"
}
