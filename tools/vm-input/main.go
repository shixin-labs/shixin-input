// Command shixin-vm-input sends native keyboard events inside the macOS test
// VM. scripts/macos-vm.sh builds and runs it in the guest, never on the host.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "shixin-vm-input: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	f := flag.NewFlagSet("shixin-vm-input", flag.ContinueOnError)
	delay := f.Duration("delay", 10*time.Millisecond, "delay after each key stroke")
	timeout := f.Duration("timeout", 2*time.Second, "clipboard assertion timeout")
	f.Usage = func() {
		fmt.Fprintln(f.Output(), "usage: shixin-vm-input [flags] (key CHORD | type ASCII | wait DURATION | expect TEXT)...")
		fmt.Fprintln(f.Output(), "example: key cmd+n type 'Hello >:' key cmd+a key cmd+c expect 'Hello >:'")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *delay < 0 || *timeout < 0 {
		return errors.New("delay and timeout must not be negative")
	}
	// Validate the entire sequence before requesting permission or posting
	// anything, so an invalid trailing command cannot leave partial input.
	actions, err := parse(f.Args())
	if err != nil {
		return err
	}
	press, close, err := openKeyboard()
	if err != nil {
		return err
	}
	defer close()
	return execute(actions, press, readClipboard, *delay, *timeout)
}

type action struct {
	op   string
	key  stroke
	wait time.Duration
	text string
}

func parse(args []string) ([]action, error) {
	if len(args) == 0 || len(args)%2 != 0 {
		return nil, errors.New("commands require a verb and an argument")
	}
	var out []action
	for i := 0; i < len(args); i += 2 {
		value := args[i+1]
		switch args[i] {
		case "key":
			k, err := chord(value)
			if err != nil {
				return nil, err
			}
			out = append(out, action{op: "key", key: k})
		case "type":
			for _, ch := range value {
				k, ok := asciiKey(ch)
				if !ok {
					return nil, fmt.Errorf("type supports printable US ASCII, tab and newline; unsupported U+%04X", ch)
				}
				out = append(out, action{op: "key", key: k})
			}
		case "wait":
			d, err := time.ParseDuration(value)
			if err != nil || d < 0 {
				return nil, errors.New("wait requires a non-negative duration, such as 100ms")
			}
			out = append(out, action{op: "wait", wait: d})
		case "expect":
			out = append(out, action{op: "expect", text: value})
		default:
			return nil, fmt.Errorf("unknown command %q", args[i])
		}
	}
	return out, nil
}

func execute(actions []action, press func(stroke) error, clipboard func() (string, error), delay, timeout time.Duration) error {
	for _, a := range actions {
		switch a.op {
		case "key":
			if err := press(a.key); err != nil {
				return err
			}
			time.Sleep(delay)
		case "wait":
			time.Sleep(a.wait)
		case "expect":
			deadline := time.Now().Add(timeout)
			for {
				text, err := clipboard()
				if err != nil {
					return fmt.Errorf("read clipboard: %w", err)
				}
				if text == a.text {
					break
				}
				if !time.Now().Before(deadline) {
					// Typed text stays out of logs, including on failure.
					return errors.New("clipboard does not exactly match expected text")
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	}
	return nil
}
