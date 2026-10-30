// Command menu-capture records public-code menus from an isolated shixin-engine.
// It uses the normal IPC protocol; librime stays inside shixin-engine.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"shixin/internal/ipc"
)

type options struct {
	engine, generation, data, lib, prebuilt, input, output, profile string
	limit                                                           int
	timeout                                                         time.Duration
}

type ready struct {
	Version int      `json:"version"`
	Socket  string   `json:"socket"`
	Schemas []string `json:"schemas"`
}

type entry struct {
	Text         string `json:"text"`
	Comment      string `json:"comment"`
	Page         int    `json:"page"`
	Index        int    `json:"index"`
	Rank         int    `json:"rank"`
	FullSpan     bool   `json:"full_span"`
	SpanStart    *int   `json:"span_start,omitempty"`
	SpanEnd      *int   `json:"span_end,omitempty"`
	Verification string `json:"span_verification"`
}

type record struct {
	Version        int     `json:"schema_version"`
	Profile        string  `json:"profile_id"`
	Schema         string  `json:"schema_id"`
	Code           string  `json:"code"`
	Preedit        string  `json:"preedit"`
	Candidates     []entry `json:"candidates"`
	Complete       bool    `json:"complete"`
	LastPage       bool    `json:"last_page"`
	CapturedAt     string  `json:"captured_at"`
	Timezone       string  `json:"timezone"`
	TimezoneOffset int     `json:"timezone_offset_seconds"`
	Truncated      bool    `json:"truncated"`
}

var codePattern = regexp.MustCompile(`^[a-z]{1,32}$`)
var errCandidateChanged = errors.New("candidate changed during fresh-session probe")

func main() {
	var o options
	flag.StringVar(&o.engine, "engine", "", "shixin-engine executable")
	flag.StringVar(&o.generation, "generation", "", "validated builder generation")
	flag.StringVar(&o.data, "data", "", "read-only public dictionary snapshot")
	flag.StringVar(&o.lib, "lib", "", "librime library snapshot")
	flag.StringVar(&o.prebuilt, "prebuilt", "", "optional compiled dictionaries")
	flag.StringVar(&o.input, "input", "", "JSONL public codes, one {code} per row")
	flag.StringVar(&o.output, "output", "", "new JSONL output (never overwritten)")
	flag.StringVar(&o.profile, "profile", "", "profile fingerprint recorded with each row")
	flag.IntVar(&o.limit, "limit", 50, "raw menu prefix length, 1-50")
	flag.DurationVar(&o.timeout, "timeout", 30*time.Minute, "whole-job deadline")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	if err := run(ctx, o); err != nil {
		fmt.Fprintln(os.Stderr, "menu-capture:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options) error {
	if o.engine == "" || o.generation == "" || o.data == "" || o.lib == "" || o.profile == "" || o.input == "" || o.output == "" {
		return errors.New("-engine, -generation, -data, -lib, -profile, -input and -output are required")
	}
	if o.limit < 1 || o.limit > 50 || o.timeout <= 0 {
		return errors.New("limit must be 1-50 and timeout positive")
	}
	codes, err := readCodes(o.input)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(o.output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer output.Close()
	w, err := startWorker(ctx, o)
	if err != nil {
		return err
	}
	defer w.close()
	enc := json.NewEncoder(output)
	for _, code := range codes {
		if err := ctx.Err(); err != nil {
			return err
		}
		r, err := capture(ctx, w.ready.Socket, code, o.limit)
		if err != nil {
			return fmt.Errorf("capture failed: %w", err)
		}
		r.Profile, r.Schema = o.profile, w.ready.Schemas[0]
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

func readCodes(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var codes []string
	seen := map[string]bool{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		var row struct {
			Code string `json:"code"`
		}
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&row); err != nil {
			return nil, fmt.Errorf("invalid input row: %w", err)
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return nil, errors.New("input row has trailing JSON")
		}
		if !codePattern.MatchString(row.Code) {
			return nil, errors.New("codes must be 1-32 lowercase ASCII letters")
		}
		if seen[row.Code] {
			return nil, errors.New("duplicate input code")
		}
		seen[row.Code] = true
		codes = append(codes, row.Code)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, errors.New("input is empty")
	}
	return codes, nil
}

type worker struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan error
	ready ready
}

func startWorker(ctx context.Context, o options) (*worker, error) {
	args := []string{"-capture", "-generation", o.generation, "-data", o.data, "-lib", o.lib, "-log", "-"}
	if o.prebuilt != "" {
		args = append(args, "-prebuilt", o.prebuilt)
	}
	w := &worker{cmd: exec.CommandContext(ctx, o.engine, args...), done: make(chan error, 1)}
	var err error
	w.stdin, err = w.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	w.cmd.Cancel = func() error { return w.stdin.Close() }
	w.cmd.WaitDelay = 5 * time.Second
	stdout, err := w.cmd.StdoutPipe()
	if err != nil {
		w.stdin.Close()
		return nil, err
	}
	w.cmd.Stderr = os.Stderr
	if err := w.cmd.Start(); err != nil {
		w.stdin.Close()
		return nil, err
	}
	go func() { w.done <- w.cmd.Wait() }()
	type readiness struct {
		reply ready
		err   error
	}
	read := make(chan readiness, 1)
	go func() {
		var reply ready
		err := json.NewDecoder(io.LimitReader(stdout, 64<<10)).Decode(&reply)
		read <- readiness{reply, err}
	}()
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case result := <-read:
		w.ready, err = result.reply, result.err
		if err == nil && (w.ready.Version != 1 || w.ready.Socket == "" || len(w.ready.Schemas) == 0) {
			err = errors.New("invalid capture worker readiness")
		}
		if err != nil {
			w.close()
			return nil, fmt.Errorf("worker readiness: %w", err)
		}
		return w, nil
	case <-ctx.Done():
		w.close()
		return nil, ctx.Err()
	case <-timer.C:
		w.close()
		return nil, errors.New("capture worker startup timed out")
	}
}

func (w *worker) close() {
	w.stdin.Close()
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		w.cmd.Process.Kill()
		<-w.done
	}
}

type client struct {
	c   net.Conn
	r   *ipc.Reader
	seq uint32
	ctx context.Context
}

func connect(ctx context.Context, socket string) (*client, error) {
	c, err := ipc.Dial(socket, 5*time.Second)
	if err != nil {
		return nil, err
	}
	cl := &client{c: c, r: ipc.NewReader(c, 4096), ctx: ctx}
	if err = cl.write((&ipc.Hello{Version: ipc.Version, Kind: ipc.KindTest, PID: uint32(os.Getpid())}).Append(nil)); err != nil {
		c.Close()
		return nil, err
	}
	typ, payload, err := cl.r.Next()
	var welcome ipc.Welcome
	if err != nil || typ != ipc.TypeWelcome || welcome.Decode(payload) != nil || welcome.Version != ipc.Version {
		c.Close()
		return nil, errors.New("invalid engine welcome")
	}
	if err = cl.write((&ipc.FocusIn{Ctx: 1, App: []byte("offline-menu-capture")}).Append(nil)); err != nil {
		c.Close()
		return nil, err
	}
	if _, err = cl.result(0); err != nil {
		c.Close()
		return nil, err
	}
	return cl, nil
}

func (cl *client) deadline() time.Time {
	deadline := time.Now().Add(5 * time.Second)
	if limit, ok := cl.ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	return deadline
}

func (cl *client) write(frame []byte) error {
	if err := cl.ctx.Err(); err != nil {
		return err
	}
	if err := cl.c.SetDeadline(cl.deadline()); err != nil {
		return err
	}
	_, err := io.Copy(cl.c, strings.NewReader(string(frame)))
	return err
}

func (cl *client) result(sequence uint32) (*ipc.Result, error) {
	for {
		typ, payload, err := cl.r.Next()
		if err != nil {
			return nil, err
		}
		if typ == ipc.TypePolicy {
			continue
		}
		if typ != ipc.TypeResult {
			return nil, fmt.Errorf("unexpected IPC type %d", typ)
		}
		var result ipc.Result
		if err := result.Decode(payload); err != nil {
			return nil, err
		}
		if result.Seq != sequence || result.Ctx != 1 {
			return nil, errors.New("unexpected result sequence/context")
		}
		return &result, nil
	}
}

func (cl *client) typeCode(code string) (*ipc.Result, error) {
	var result *ipc.Result
	for _, key := range []byte(code) {
		cl.seq++
		if err := cl.write((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: uint32(key)}).Append(nil)); err != nil {
			return nil, err
		}
		var err error
		result, err = cl.result(cl.seq)
		if err != nil {
			return nil, err
		}
		if len(result.Commit) != 0 {
			return nil, errors.New("code committed text before selection")
		}
	}
	if result.Flags&ipc.ResultComposing == 0 {
		return nil, errors.New("code did not leave a composition")
	}
	return result, nil
}

func (cl *client) page() (*ipc.Result, error) {
	cl.seq++
	if err := cl.write((&ipc.ChangePage{Ctx: 1, Seq: cl.seq}).Append(nil)); err != nil {
		return nil, err
	}
	return cl.result(cl.seq)
}

func capture(ctx context.Context, socket, code string, limit int) (record, error) {
	now := time.Now()
	zone, offset := now.Zone()
	out := record{Version: 1, Code: code, Candidates: []entry{}, CapturedAt: now.Format(time.RFC3339Nano), Timezone: zone, TimezoneOffset: offset}
	cl, err := connect(ctx, socket)
	if err != nil {
		return out, err
	}
	r, err := cl.typeCode(code)
	if err != nil {
		cl.c.Close()
		return out, err
	}
	out.Preedit = string(r.Preedit)
	for {
		taken := 0
		for i, candidate := range r.Candidates {
			if len(out.Candidates) == limit {
				break
			}
			out.Candidates = append(out.Candidates, entry{Text: string(candidate.Text), Comment: string(candidate.Comment), Page: int(r.PageNo), Index: i, Rank: len(out.Candidates) + 1})
			taken++
		}
		out.LastPage = len(r.Candidates) == 0 || (r.Flags&ipc.ResultLastPage != 0 && taken == len(r.Candidates))
		if len(out.Candidates) >= limit || out.LastPage {
			break
		}
		previous := r.PageNo
		r, err = cl.page()
		if err != nil {
			cl.c.Close()
			return out, err
		}
		if r.PageNo <= previous {
			cl.c.Close()
			return out, errors.New("candidate paging did not advance")
		}
	}
	cl.c.Close()
	for i := range out.Candidates {
		full, err := verifyWholeInput(ctx, socket, code, out.Candidates[i])
		if errors.Is(err, errCandidateChanged) {
			out.Candidates[i].Verification = "candidate_changed"
			continue
		}
		if err != nil {
			return out, err
		}
		out.Candidates[i].FullSpan = full
		out.Candidates[i].Verification = "not_confirmed"
		if full {
			start, end := 0, len(code)
			out.Candidates[i].SpanStart, out.Candidates[i].SpanEnd = &start, &end
			out.Candidates[i].Verification = "whole_input_commit"
		}
	}
	out.Truncated = !out.LastPage && len(out.Candidates) == limit
	out.Complete = true
	return out, nil
}

func verifyWholeInput(ctx context.Context, socket, code string, candidate entry) (bool, error) {
	cl, err := connect(ctx, socket)
	if err != nil {
		return false, err
	}
	defer cl.c.Close()
	r, err := cl.typeCode(code)
	if err != nil {
		return false, err
	}
	for page := 0; page < candidate.Page; page++ {
		r, err = cl.page()
		if err != nil {
			return false, err
		}
	}
	if int(r.PageNo) != candidate.Page || candidate.Index >= len(r.Candidates) || string(r.Candidates[candidate.Index].Text) != candidate.Text {
		return false, errCandidateChanged
	}
	cl.seq++
	if err := cl.write((&ipc.Select{Ctx: 1, Seq: cl.seq, Index: uint32(candidate.Index)}).Append(nil)); err != nil {
		return false, err
	}
	r, err = cl.result(cl.seq)
	if err != nil {
		return false, err
	}
	return string(r.Commit) == candidate.Text && r.Flags&ipc.ResultComposing == 0, nil
}
