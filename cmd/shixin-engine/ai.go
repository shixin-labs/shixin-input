package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"shixin/internal/models"
	"shixin/internal/rerank"
	"shixin/internal/settings"
)

// strength is the protocol code of a 智能排序 setting; 0 is off.
func strength(setting string) uint8 {
	switch setting {
	case settings.RerankCautious:
		return rerank.StrengthCautious
	case settings.RerankStandard:
		return rerank.StrengthStandard
	}
	return 0
}

// aiControl runs shixin-ai while 智能排序 is on.
type aiControl struct {
	socket, log string
	logf        func(string, ...any)
	stop        func()
}

// set starts or stops shixin-ai for the setting.
func (a *aiControl) set(setting string) {
	switch on := strength(setting) != 0; {
	case on && a.stop == nil:
		a.stop = superviseAI(a.socket, a.log, a.logf)
	case !on && a.stop != nil:
		a.stop()
		a.stop = nil
	}
}

// superviseAI runs shixin-ai, the executable next to this one, on socket,
// logging to log (ai.log next to the engine's), and restarts it with
// backoff after it exits. It waits for the model to be downloaded first,
// and gives up without an accelerator. Its state is recorded next to the
// socket for the settings app (rerank.WriteState). shixin-ai exits when
// its stdin closes, so it never outlives the coordinator. The returned
// function stops it.
func superviseAI(socket, log string, logf func(string, ...any)) func() {
	exe, err := os.Executable()
	if err != nil {
		return func() {}
	}
	path := filepath.Join(filepath.Dir(exe), "shixin-ai")
	run := filepath.Dir(socket)
	modelDir, _ := models.Dir()
	// Developers point shixin-ai at another model (cmd/shixin-ai).
	custom := os.Getenv("SHIXIN_AI_MODEL") != ""
	stop := make(chan struct{})
	wait := func(d time.Duration) bool {
		select {
		case <-stop:
			return false
		case <-time.After(d):
			return true
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer rerank.WriteState(run, "")
		backoff := time.Second
		for {
			if !custom && !models.ERNIE.Present(modelDir) {
				rerank.WriteState(run, rerank.StateNoModel)
				if !wait(2 * time.Second) {
					return
				}
				continue
			}
			cmd := exec.Command(path, "-lifeline", "-socket", socket, "-log", log)
			in, err := cmd.StdinPipe()
			if err == nil {
				err = cmd.Start()
			}
			if err != nil {
				logf("start shixin-ai: %v", err)
				if errors.Is(err, os.ErrNotExist) {
					return
				}
			} else {
				rerank.WriteState(run, rerank.StateRunning)
				started := time.Now()
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				select {
				case <-stop:
					in.Close()
					select {
					case <-done:
					case <-time.After(2 * time.Second):
						cmd.Process.Kill()
						<-done
					}
					return
				case err = <-done:
				}
				var ee *exec.ExitError
				if errors.As(err, &ee) && ee.ExitCode() == rerank.ExitNoAccelerator {
					logf("shixin-ai: no supported accelerator; 智能排序 is unavailable")
					rerank.WriteState(run, rerank.StateNoAccelerator)
					<-stop
					return
				}
				if errors.As(err, &ee) && ee.ExitCode() == rerank.ExitNoModel {
					continue // removed meanwhile: wait for it again
				}
				logf("shixin-ai exited: %v", err)
				if time.Since(started) > time.Minute {
					backoff = time.Second
				}
			}
			if !wait(backoff) {
				return
			}
			backoff = min(2*backoff, time.Minute)
		}
	}()
	return func() {
		close(stop)
		wg.Wait()
	}
}
