package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"shixin/internal/ipc"
)

// ServeQuit serves a cold, private shutdown endpoint. prepare flushes pending
// work and returns an action dispatched only after the caller acknowledges
// the reply. This keeps a fast GUI exit from destroying its own reply pipe.
func ServeQuit(path string, prepare func() (func(), error)) (net.Listener, error) {
	l, err := ipc.Listen(path)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			c, err := ipc.Accept(l)
			if err != nil {
				return
			}
			c.SetDeadline(time.Now().Add(5 * time.Second))
			var req Request
			var quit func()
			err = json.NewDecoder(io.LimitReader(c, 4096)).Decode(&req)
			if err == nil {
				if req.Version != Version || req.Op != OpStop {
					err = errors.New("unsupported lifecycle request")
				} else {
					quit, err = prepare()
				}
			}
			reply := Reply{Version: Version}
			if err != nil {
				reply.Error = err.Error()
			}
			if json.NewEncoder(c).Encode(reply) == nil && err == nil && quit != nil {
				var ack [1]byte
				if _, err := io.ReadFull(c, ack[:]); err == nil && ack[0] == 1 {
					quit()
				}
			}
			c.Close()
		}
	}()
	return l, nil
}

// Stop asks an authenticated sibling endpoint to stop. The reply acknowledges
// the request; callers must still wait for dictionary locks/process exit.
func Stop(path string) error {
	c, err := ipc.Dial(path, time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(40 * time.Second))
	if err = json.NewEncoder(c).Encode(Request{Version: Version, Op: OpStop}); err != nil {
		return err
	}
	var reply Reply
	if err = json.NewDecoder(io.LimitReader(c, 4096)).Decode(&reply); err != nil {
		return err
	}
	if reply.Version != Version {
		return errors.New("unsupported lifecycle reply")
	}
	if reply.Error != "" {
		return fmt.Errorf("lifecycle: %s", reply.Error)
	}
	_, err = c.Write([]byte{1})
	return err
}
