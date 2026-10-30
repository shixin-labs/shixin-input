package engine

import (
	"errors"
	"fmt"
	"strings"

	"shixin/internal/ipc"
)

// ErrComposing postpones migration until an idle boundary.
var ErrComposing = errors.New("engine: a session is composing")

// ErrClients means clients have not finished draining their writes.
var ErrClients = errors.New("engine: clients are draining")

// Migrate asks clients to finish writes and reconnect to endpoint. It
// changes nothing when any session is composing. Cold lifecycle path.
func (e *Engine) Migrate(endpoint string) error { return e.control(reqMigrate, endpoint) }

// Drained reports whether every client has left the old generation.
func (e *Engine) Drained() error { return e.control(reqDrained, "") }

// Promote attaches the real schema after the caller acquires the engine
// lock and verifies the old worker released the user dictionary.
func (e *Engine) Promote(schema string) error { return e.control(reqPromote, schema) }

func (e *Engine) control(kind reqKind, name string) error {
	ch := make(chan error, 1)
	select {
	case e.reqs <- request{kind: kind, name: name, answer: ch}:
	case <-e.stopped:
		return errors.New("engine: stopped")
	}
	select {
	case err := <-ch:
		return err
	case <-e.stopped:
		return errors.New("engine: stopped")
	}
}

// migrate asks every client to reconnect to endpoint, unless a session is
// composing. Clients that can't be told are disconnected.
func (e *Engine) migrate(endpoint string) error {
	for c := range e.conns {
		for _, s := range c.sessions {
			s.ReadStatus(&e.st)
			if e.st.IsComposing {
				return ErrComposing
			}
		}
	}
	e.migrating = true
	for c := range e.conns {
		b := c.buffer()
		if b == nil || !c.enqueue((&ipc.Migrate{Endpoint: []byte(endpoint)}).Append(b)) {
			c.kill()
		}
	}
	return nil
}

func (e *Engine) promote(schema string) error {
	if !e.opt.Standby || e.realSchema != "" {
		return errors.New("engine: not a standby")
	}
	sp := e.schemas[schema+"_warm"]
	if sp == nil {
		return fmt.Errorf("engine: schema %s was not warmed", schema)
	}
	// Only now, under the engine lock, expose real schemas to the
	// switcher and create_session. The immutable file stays warm-only, so
	// a future process cannot accidentally start on a remembered real id.
	cfg, err := e.lib.OpenConfig("default")
	if err != nil {
		return err
	}
	defer cfg.Close()
	for i, n := 0, cfg.ListSize("schema_list"); i < n; i++ {
		key := fmt.Sprintf("schema_list/@%d/schema", i)
		warm, _ := cfg.String(key)
		real := strings.TrimSuffix(warm, "_warm")
		if e.schemas[warm] == nil || !cfg.SetString(key, real) {
			return errors.New("engine: activate schema list failed")
		}
		e.schemas[real] = e.schemas[warm]
	}
	// Keep one warmed session attached at activation. New contexts use the
	// pool fallback, selecting the real schema explicitly if needed.
	s := e.pool[0]
	if !s.SelectSchema(schema) {
		return errors.New("engine: promotion failed")
	}
	e.warmSession(s)
	for _, spare := range e.pool[1:] {
		e.destroySession(spare)
	}
	e.pool = e.pool[:1]
	// The macOS front uses one context. Do not build an unused replacement
	// session immediately after FocusIn, delaying the first queued key.
	e.opt.PoolSize = 0
	e.realSchema = schema
	e.schemas[schema] = sp
	e.stats.Schemas = []string{schema}
	return nil
}
