package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"shixin/internal/engine"
	"shixin/internal/generation"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/rerank"
	"shixin/internal/settings"
)

type child struct {
	cmd         *exec.Cmd
	in          io.WriteCloser
	out         *json.Decoder
	stdout      io.ReadCloser
	done        chan struct{}
	err         error // published by closing done
	dir, socket string
}

func startChild(c config, dir string) (*child, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Short socket names also work under macOS's 104-byte sun_path limit.
	f, err := os.CreateTemp(filepath.Dir(c.socket), "g-")
	if err != nil {
		return nil, err
	}
	socket := f.Name()
	f.Close()
	os.Remove(socket)
	cmd := exec.Command(exe, "-worker", "-generation", dir, "-lib", c.lib, "-data", c.data, "-prebuilt", c.prebuilt,
		"-user", c.user, "-settings", c.settings, "-socket", socket, "-public", c.socket, "-log", c.log)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, writer, err := os.Pipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stdout = writer
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		writer.Close()
		return nil, err
	}
	// Wait must not close stdout before the stop acknowledgement is read.
	writer.Close()
	p := &child{cmd: cmd, in: in, out: json.NewDecoder(out), stdout: out, done: make(chan struct{}), dir: dir, socket: socket}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	if _, err := p.receive(30 * time.Second); err != nil {
		p.kill()
		return nil, fmt.Errorf("standby warm-up: %w", err)
	}
	return p, nil
}

func (p *child) receive(timeout time.Duration) (lifecycle.Reply, error) {
	type result struct {
		r   lifecycle.Reply
		err error
	}
	ch := make(chan result, 1)
	go func() { var r lifecycle.Reply; err := p.out.Decode(&r); ch <- result{r, err} }()
	select {
	case v := <-ch:
		if v.err != nil {
			return v.r, v.err
		}
		if v.r.Version != lifecycle.Version {
			return v.r, errors.New("worker: unsupported version")
		}
		if v.r.Error != "" {
			return v.r, workerError(v.r.Error)
		}
		return v.r, nil
	case <-time.After(timeout):
		// A timed-out decoder must never race another call on the same pipe.
		p.kill()
		return lifecycle.Reply{}, errors.New("worker: lifecycle timeout")
	}
}

// workerError turns a worker's reply error back into the engine's error
// of that text, so callers can test it with errors.Is.
func workerError(text string) error {
	for _, err := range []error{engine.ErrComposing, engine.ErrClients} {
		if text == err.Error() {
			return err
		}
	}
	return errors.New(text)
}

func (p *child) call(op string) (lifecycle.Reply, error) {
	if err := json.NewEncoder(p.in).Encode(lifecycle.Request{Version: lifecycle.Version, Op: op}); err != nil {
		return lifecycle.Reply{}, err
	}
	return p.receive(5 * time.Second)
}

func (p *child) kill() {
	p.in.Close()
	p.cmd.Process.Kill()
	<-p.done // locks must be released before another process can attach
	p.stdout.Close()
	os.Remove(p.socket)
}

func (p *child) stop() (time.Duration, error) {
	t := time.Now()
	_, err := p.call(lifecycle.OpStop)
	if err != nil {
		p.kill()
		return time.Since(t), err
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.kill()
	}
	os.Remove(p.socket)
	p.stdout.Close()
	return time.Since(t), err
}

type coordinator struct {
	c        config
	logf     func(string, ...any)
	active   *child
	applied  settings.Settings
	previous string
}

func (s *coordinator) build(snapshot settings.Settings) (string, error) {
	root := filepath.Join(s.c.user, "generations")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	temp, err := os.MkdirTemp(root, "gen-")
	if err != nil {
		return "", err
	}
	// The builder requires a new directory; the snapshot is a sibling so it
	// cannot accidentally read a later settings edit halfway through deploy.
	path := temp + ".json"
	defer os.Remove(path)
	os.Remove(temp)
	if err := settings.Save(path, snapshot); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.c.builder, "-lib", s.c.lib, "-data", s.c.data, "-prebuilt", s.c.prebuilt, "-settings", path, "-output", temp)
	// librime diagnostics may quote source YAML. Keep them off the product
	// log; a failed step surfaces as a bounded lifecycle error in Settings.
	if err := cmd.Run(); err != nil {
		os.RemoveAll(temp)
		return "", fmt.Errorf("builder failed: %w", err)
	}
	return temp, nil
}

func (s *coordinator) activate(p *child) error {
	if _, err := p.call(lifecycle.OpPromote); err != nil {
		return err
	}
	// Persist recovery before exposing the endpoint. On failure the caller
	// stops this worker and rolls back, still respecting the engine lock.
	state := filepath.Join(s.c.user, "active-generation")
	if err := os.WriteFile(state+".next", []byte(filepath.Base(p.dir)+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(state+".next", state); err != nil {
		return err
	}
	return publish(s.c.socket, p.socket)
}

func (s *coordinator) apply(next settings.Settings) error {
	if !settings.EngineChanged(s.applied, next) {
		return nil
	}
	t := time.Now()
	dir, err := s.build(next)
	if err != nil {
		return err
	}
	standby, err := startChild(s.c, dir)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	old := s.active
	// No lifecycle work runs on the front or its key thread. Wait for an
	// idle boundary without holding the user's app or the engine thread.
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err = old.call(lifecycle.OpMigrate)
		if err == nil {
			break
		}
		if !errors.Is(err, engine.ErrComposing) || time.Now().After(deadline) {
			standby.kill()
			os.RemoveAll(dir)
			return fmt.Errorf("handoff: %w", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		_, err = old.call(lifecycle.OpDrained)
		if err == nil {
			break
		}
		if !errors.Is(err, engine.ErrClients) || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	handoff := time.Now()
	release, _ := old.stop() // stop/kill always waits for process exit
	err = s.activate(standby)
	if err != nil {
		standby.kill()
		os.RemoveAll(dir)
		// The old executable is the same version here, but its immutable data
		// remains intact. Recreate it only after the failed standby has exited.
		rollback, e := startChild(s.c, old.dir)
		if e == nil {
			e = s.activate(rollback)
		}
		if e != nil {
			if rollback != nil {
				rollback.kill()
			}
			return fmt.Errorf("promotion failed; rollback failed: %w", e)
		}
		s.active = rollback
		return fmt.Errorf("promotion failed (previous generation restored): %w", err)
	}
	s.active = standby
	s.applied = next
	available := time.Since(handoff)
	if s.previous != "" && s.previous != old.dir {
		os.RemoveAll(s.previous)
	}
	s.previous = old.dir
	s.logf("generation active: release %v, release-to-ready %v, build+warm+swap %v", release, available, time.Since(t))
	return nil
}

type applyRequest struct{ done chan error }

func runCoordinator(c config, logf func(string, ...any), sig <-chan os.Signal) error {
	release, err := engine.Lock(filepath.Join(c.user, "supervisor"))
	if err != nil {
		return fmt.Errorf("%w: %v", errLocked, err)
	}
	defer release()
	if err := os.MkdirAll(filepath.Dir(c.socket), 0o700); err != nil {
		return err
	}
	s := &coordinator{c: c, logf: logf}
	next, err := settings.Load(c.settings)
	if err != nil {
		// Load repairs invalid fields while preserving valid choices. Settings
		// shows and backs up the malformed file; keep typing available.
		logf("settings repaired for startup: %v", err)
	}
	// Recovery uses only our own persisted generation basename.
	dir := ""
	if b, err := os.ReadFile(filepath.Join(c.user, "active-generation")); err == nil {
		name := strings.TrimSpace(string(b))
		if strings.HasPrefix(name, "gen-") && filepath.Base(name) == name {
			dir = filepath.Join(c.user, "generations", name)
		}
	}
	if dir != "" {
		_, err = os.Stat(filepath.Join(dir, "settings.json"))
		if err == nil {
			s.applied, err = settings.Load(filepath.Join(dir, "settings.json"))
		}
		if err == nil {
			var matches bool
			matches, err = generation.MatchesSettings(dir, s.applied)
			if err == nil && !matches {
				logf("saved generation predates the current settings compiler; rebuilding")
				dir = ""
			}
		}
		if err != nil {
			dir = ""
		}
	}
	if dir == "" {
		dir, err = s.build(next)
		if err != nil {
			return err
		}
		s.applied = next
	}
	s.active, err = startChild(c, dir)
	if err != nil {
		return err
	}
	defer func() {
		if s.active != nil {
			s.active.stop()
		}
		os.Remove(c.socket)
	}()
	if err := s.activate(s.active); err != nil {
		return err
	}
	ai := &aiControl{socket: rerank.SocketPath(filepath.Dir(c.socket)), log: "-", logf: logf}
	if c.log != "-" && c.log != "" {
		ai.log = filepath.Join(filepath.Dir(c.log), "ai.log")
	}
	ai.set(next.Rerank)
	defer ai.set("off")
	control, err := ipc.Listen(c.socket + ".control")
	if err != nil {
		return err
	}
	defer control.Close()
	stopCleanup := s.startGenerationCleanup()
	defer stopCleanup()
	requests := make(chan applyRequest, 8)
	done := make(chan struct{})
	defer close(done)
	go acceptControl(control, requests, done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	attempted := s.applied
	for {
		select {
		case <-sig:
			return nil
		case <-s.active.done:
			logf("worker exited: %v", s.active.err)
			s.active.kill()
			// A hung/crashed process is waited for before a replacement exists.
			replacement, e := startChild(c, s.active.dir)
			if e != nil {
				return e
			}
			s.active = replacement
			if err := s.activate(replacement); err != nil {
				return err
			}
		case r := <-requests:
			next, err := settings.Load(c.settings)
			if err == nil {
				ai.set(next.Rerank)
				attempted = next
				err = s.apply(next)
			}
			r.done <- err
		case <-tick.C:
			next, err := settings.Load(c.settings)
			if err != nil {
				continue
			}
			ai.set(next.Rerank) // live: no new generation
			if !settings.EngineChanged(attempted, next) {
				continue
			}
			attempted = next
			if err := s.apply(next); err != nil {
				logf("settings apply failed: %v", err)
			}
		}
	}
}

func acceptControl(l net.Listener, requests chan<- applyRequest, stop <-chan struct{}) {
	for {
		c, err := ipc.Accept(l)
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			c.SetDeadline(time.Now().Add(90 * time.Second))
			var req lifecycle.Request
			err := json.NewDecoder(io.LimitReader(c, 4096)).Decode(&req)
			if err == nil && (req.Version != lifecycle.Version || req.Op != lifecycle.OpApply) {
				err = errors.New("unsupported lifecycle request")
			}
			if err == nil {
				r := applyRequest{done: make(chan error, 1)}
				select {
				case requests <- r:
					select {
					case err = <-r.done:
					case <-stop:
						err = errors.New("coordinator stopped")
					}
				default:
					err = errors.New("settings apply queue full")
				}
			}
			reply := lifecycle.Reply{Version: lifecycle.Version}
			if err != nil {
				reply.Error = err.Error()
			}
			json.NewEncoder(c).Encode(reply)
		}()
	}
}
