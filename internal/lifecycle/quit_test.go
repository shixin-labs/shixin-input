package lifecycle

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestQuitHandshake(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quit.sock")
	quit := make(chan struct{}, 1)
	l, err := ServeQuit(path, func() (func(), error) { return func() { quit <- struct{}{} }, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := Stop(path); err != nil {
		t.Fatal(err)
	}
	select {
	case <-quit:
	case <-time.After(time.Second):
		t.Fatal("quit not dispatched")
	}
}

func TestQuitPreparationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quit.sock")
	l, err := ServeQuit(path, func() (func(), error) { return func() { t.Error("quit after failed save") }, errors.New("save failed") })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := Stop(path); err == nil {
		t.Fatal("preparation failure hidden")
	}
}
