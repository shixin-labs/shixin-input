package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"shixin/internal/engine"
	"shixin/internal/generation"
	"shixin/internal/ipc"
)

// captureReady is a tool-only startup reply, separate from normal lifecycle IPC.
type captureReady struct {
	Version int      `json:"version"`
	Socket  string   `json:"socket"`
	Schemas []string `json:"schemas"`
}

func captureConfig(c *config, worker bool) error {
	if worker || c.user != "" || c.socket != "" || c.settings != "" || c.builder != "" || c.public != "" {
		return errors.New("capture: -worker, -user, -socket, -settings, -builder and -public are forbidden; capture owns its scratch paths")
	}
	if c.generation == "" || c.lib == "" || c.data == "" {
		return errors.New("capture: -generation, -lib and -data are required")
	}
	for _, p := range []*string{&c.generation, &c.lib, &c.data} {
		var err error
		*p, err = filepath.Abs(*p)
		if err != nil {
			return fmt.Errorf("capture: path: %w", err)
		}
	}
	if c.prebuilt != "" {
		var err error
		c.prebuilt, err = filepath.Abs(c.prebuilt)
		if err != nil {
			return err
		}
	}
	return nil
}

// runCapture serves only validated warm schemas. It never promotes or receives
// a live user directory, and never loads librime outside the engine thread.
func runCapture(c config, logf func(string, ...any), sig <-chan os.Signal, in io.Reader, out io.Writer) error {
	if err := generation.Validate(c.generation); err != nil {
		return fmt.Errorf("capture: validate generation: %w", err)
	}
	root, err := ipc.TempDir("sx-capture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	user := filepath.Join(root, "user")
	if err := os.Mkdir(user, 0o700); err != nil {
		return err
	}
	core := filepath.Join(filepath.Dir(c.lib), "..", "share", "opencc")
	if _, err := os.Stat(core); err == nil {
		if err := os.CopyFS(filepath.Join(user, "opencc"), os.DirFS(core)); err != nil {
			return fmt.Errorf("capture: copy OpenCC data: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	release, err := engine.Lock(user)
	if err != nil {
		return err
	}
	defer release()
	e, err := engine.Start(engine.Options{
		LibPath: c.lib, SharedDataDir: c.data, UserDataDir: user,
		PrebuiltDataDir: c.prebuilt, StagingDir: filepath.Join(c.generation, "build"),
		Standby: true, LogDir: root, MinLogLevel: 2, Version: version,
		Warmup: engine.DefaultWarmup, Logf: logf,
	})
	if err != nil {
		return fmt.Errorf("capture: start engine: %w", err)
	}
	defer e.Stop()
	socket := filepath.Join(root, "engine.sock")
	l, err := ipc.Listen(socket)
	if err != nil {
		return err
	}
	defer l.Close()
	served := make(chan error, 1)
	go func() { served <- e.Serve(l) }()
	if err := json.NewEncoder(out).Encode(captureReady{Version: 1, Socket: socket, Schemas: e.Stats().Schemas}); err != nil {
		return err
	}
	ended := make(chan struct{})
	go func() {
		io.Copy(io.Discard, in)
		close(ended)
	}()
	select {
	case <-sig:
		return nil
	case <-ended:
		return nil // parent exited or closed its lifetime pipe
	case err := <-served:
		return err
	}
}
