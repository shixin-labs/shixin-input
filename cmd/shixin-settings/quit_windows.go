package main

import (
	"errors"
	"path/filepath"
	"reflect"
	"time"

	"github.com/egoist/mygo"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
)

func (a *app) setupQuit() {
	dir, err := ipc.RuntimeDir()
	if err != nil {
		return
	}
	l, err := lifecycle.ServeQuit(filepath.Join(dir, "settings.stop"), func() (func(), error) {
		done := make(chan error, 1)
		a.win.Update(func() {
			if !reflect.DeepEqual(a.s, a.saved) {
				a.apply()
			}
			if !reflect.DeepEqual(a.s, a.saved) {
				done <- errors.New("save settings before installing")
				return
			}
			done <- nil
		})
		select {
		case err := <-done:
			if err != nil {
				return nil, err
			}
			return func() { a.win.Update(func() { mygo.App.Quit() }) }, nil
		case <-time.After(4 * time.Second):
			return nil, errors.New("settings did not respond to shutdown")
		}
	})
	if err == nil {
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { l.Close() })
	}
}
