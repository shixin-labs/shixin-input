package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"shixin/internal/engine"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/rerank"
	"shixin/internal/settings"
)

var errLocked = errors.New("another coordinator owns the user directory")

// worker is one generation's engine under a coordinator (-worker). It
// warms up as a standby, then serves lifecycle requests from the
// coordinator on stdin until told to stop or stdin closes.
type worker struct {
	c        config
	logf     func(string, ...any)
	s        settings.Settings // the generation's snapshot
	e        *engine.Engine
	remote   *rerank.Remote
	stop     chan struct{} // stops remote
	release  func()        // the engine lock, once promoted
	listener net.Listener  // the private socket, once promoted
}

func runWorker(c config, logf func(string, ...any), sig <-chan os.Signal) error {
	out := json.NewEncoder(os.Stdout)
	reply := func(err error, d time.Duration) error {
		r := lifecycle.Reply{Version: lifecycle.Version, NS: int64(d)}
		if err != nil {
			r.Error = err.Error()
		}
		return out.Encode(r)
	}
	w := &worker{c: c, logf: logf, stop: make(chan struct{})}
	defer close(w.stop)
	if err := w.start(); err != nil {
		reply(err, 0)
		return err
	}
	defer w.close()
	if err := reply(nil, w.e.Stats().StartupTime); err != nil {
		return err
	}
	requests := make(chan lifecycle.Request)
	go func() {
		defer close(requests)
		in := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
		for {
			var r lifecycle.Request
			if in.Decode(&r) != nil {
				return
			}
			select {
			case requests <- r:
			case <-sig:
				return
			}
		}
	}()
	for {
		select {
		case <-sig:
			return nil
		case r, ok := <-requests:
			if !ok {
				return nil // the coordinator is gone: close sessions and release the lock
			}
			t := time.Now()
			var err error
			switch {
			case r.Version != lifecycle.Version:
				err = errors.New("worker: unsupported lifecycle version")
			case r.Op == lifecycle.OpPromote:
				err = w.promote()
			case r.Op == lifecycle.OpMigrate:
				err = w.e.Migrate(c.public)
			case r.Op == lifecycle.OpDrained:
				err = w.e.Drained()
			case r.Op == lifecycle.OpStop:
				// Stop before acknowledging: the coordinator may then
				// promote a standby.
				w.close()
				reply(nil, time.Since(t))
				return nil
			default:
				err = errors.New("worker: unknown lifecycle operation")
			}
			if err := reply(err, time.Since(t)); err != nil {
				return err
			}
		}
	}
}

// start warms up the generation as a standby: validated warm schemas only,
// no engine lock, no clients.
func (w *worker) start() error {
	c := &w.c
	var err error
	w.s, err = settings.Load(filepath.Join(c.generation, "settings.json"))
	if err != nil {
		return err
	}
	touchData(w.logf, make([]byte, 1<<20), filepath.Join(c.generation, "build"), c.prebuilt)
	opt := engine.Options{LibPath: c.lib, SharedDataDir: c.data, UserDataDir: c.user, PrebuiltDataDir: c.prebuilt,
		StagingDir: filepath.Join(c.generation, "build"), Standby: true, LogDir: "", MinLogLevel: 2, Version: version,
		Warmup: engine.DefaultWarmup, Apps: appModes(&w.s), Logf: w.logf}
	// 智能排序 is applied live from the settings file, not the
	// generation's snapshot. shixin-ai is reached only once this generation
	// is active, so a standby never takes it over.
	live, _ := settings.Load(c.settings)
	w.remote = rerank.NewRemote(rerank.SocketPath(filepath.Dir(c.public)), strength(live.Rerank), w.logf)
	opt.Scorer, opt.Rerank = w.remote, strength(live.Rerank) != 0
	w.e, err = engine.Start(opt)
	return err
}

// promote takes the engine lock, attaches the real schema and starts
// serving clients, the settings watcher, keep-warm and shixin-ai.
func (w *worker) promote() error {
	if w.release != nil {
		return errors.New("worker: already active")
	}
	c := &w.c
	release, err := engine.Lock(c.user)
	if err != nil {
		return err
	}
	w.release = release
	if err := engine.UserLocksFree(c.user); err != nil {
		return err
	}
	if err := w.e.Promote(w.s.Scheme); err != nil {
		return err
	}
	l, err := ipc.Listen(c.socket)
	if err != nil {
		return err
	}
	w.listener = l
	e, remote := w.e, w.remote
	go e.Serve(l)
	go settings.Watch(c.settings, time.Second, nil, func(s settings.Settings) {
		e.SetApps(appModes(&s))
		if n := strength(s.Rerank); n != 0 {
			remote.SetStrength(n)
		}
		e.SetRerank(strength(s.Rerank) != 0)
	})
	go keepWarm(w.logf, filepath.Join(c.generation, "build"), c.prebuilt)
	go remote.Run(w.stop, e.Rerank)
	return nil
}

// close stops serving, stops the engine, which releases the user
// dictionary, and then releases the engine lock. It may be called twice.
func (w *worker) close() {
	if w.listener != nil {
		w.listener.Close()
		w.listener = nil
	}
	if w.e != nil {
		w.e.Stop()
	}
	if w.release != nil {
		w.release()
		w.release = nil
	}
}
