// Package lifecycle implements the cold supervisor/Settings protocol.
// It is independent of the binary key stream and carries no input text.
package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"shixin/internal/ipc"
)

// Version versions the private lifecycle protocol.
const Version = 1

// Operations. Settings sends OpApply to the coordinator's control socket;
// the coordinator sends the others to a worker on its stdin (DESIGN §3.5).
const (
	OpApply      = "apply"      // build and activate a generation for settings.json
	OpPromote    = "promote"    // take the engine lock and serve the real schema
	OpMigrate    = "migrate"    // move clients to the public endpoint at an idle boundary
	OpDrained    = "drained"    // report whether every client has left
	OpStop       = "stop"       // release the user dictionary and exit
	OpDictionary = "dictionary" // exclusive, cold dictionary maintenance
)

// ExitLocked is shixin-engine's exit status when another coordinator owns
// the user directory: the front then connects to that one.
const ExitLocked = 3

// Request asks for one lifecycle operation.
type Request struct {
	Version int    `json:"version"`
	Op      string `json:"op"`
	Action  string `json:"action,omitempty"`
	Path    string `json:"path,omitempty"`
}

// Dictionary requests import, export or clear on a background Settings
// goroutine. Only this cold JSON protocol changes; the typing wire is unchanged.
func Dictionary(socket, action, path string) error {
	if action != "import" && action != "export" && action != "clear" {
		return errors.New("lifecycle: unsupported dictionary action")
	}
	c, err := ipc.Dial(socket+".control", time.Second)
	if err != nil {
		return errors.New("lifecycle: engine is unavailable")
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(180 * time.Second))
	if err := json.NewEncoder(c).Encode(Request{Version: Version, Op: OpDictionary, Action: action, Path: path}); err != nil {
		return errors.New("lifecycle: dictionary request failed")
	}
	var r Reply
	if err := json.NewDecoder(io.LimitReader(c, 64<<10)).Decode(&r); err != nil {
		return errors.New("lifecycle: dictionary reply failed; check the result before retrying")
	}
	if r.Version != Version {
		return errors.New("lifecycle: unsupported version")
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}

// Reply reports completion. Error contains only lifecycle diagnostics.
type Reply struct {
	Version int    `json:"version"`
	Error   string `json:"error,omitempty"`
	NS      int64  `json:"duration_ns,omitempty"`
}

// Apply requests a hot swap from the persistent coordinator. It waits on
// the Settings background goroutine, never on an input thread.
func Apply(socket string) error {
	c, err := ipc.Dial(socket+".control", time.Second)
	if err != nil {
		return fmt.Errorf("lifecycle: connect: %w", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(90 * time.Second))
	if err := json.NewEncoder(c).Encode(Request{Version: Version, Op: OpApply}); err != nil {
		return fmt.Errorf("lifecycle: send: %w", err)
	}
	var r Reply
	if err := json.NewDecoder(io.LimitReader(c, 64<<10)).Decode(&r); err != nil {
		return fmt.Errorf("lifecycle: reply: %w", err)
	}
	if r.Version != Version {
		return errors.New("lifecycle: unsupported version")
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	return nil
}
