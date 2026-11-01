package ipc

import (
	"os"
	"regexp"
	"testing"
)

// winioLayoutVersion is the go-winio release whose private ioOperation
// layout pipeconn_windows.go mirrors (an OVERLAPPED followed by the result
// channel). go-winio writes completions through that layout, so a newer
// release must be checked against its file.go before this is bumped.
const winioLayoutVersion = "v0.6.4"

func TestGoWinioLayoutPinned(t *testing.T) {
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`github\.com/Microsoft/go-winio (v\S+)`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go-winio missing from go.mod")
	}
	if got := string(m[1]); got != winioLayoutVersion {
		t.Fatalf("go-winio is %s; pipeconn_windows.go mirrors %s's ioOperation layout: check go-winio's file.go, then update winioLayoutVersion", got, winioLayoutVersion)
	}
}
