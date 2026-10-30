package engine

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"shixin/internal/ipc"
	"shixin/internal/policy"
)

// raceEnabled is set by race_test.go in -race builds.
var raceEnabled bool

// librimePath finds librime for tests: $SHIXIN_LIBRIME, else Homebrew's.
func librimePath(t testing.TB) string {
	if p := os.Getenv("SHIXIN_LIBRIME"); p != "" {
		return p
	}
	for _, p := range []string{"/opt/homebrew/opt/librime/lib/librime.1.dylib", "/usr/lib/x86_64-linux-gnu/librime.so.1", "/usr/lib/librime.so.1"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("librime not found; set SHIXIN_LIBRIME")
	return ""
}

var (
	shared     *Engine
	sharedErr  error
	sharedOnce sync.Once
	sharedDir  string
)

// testEngine starts one engine for the package on the test schema, with a
// scratch user dir (never a real one, AGENTS.md).
func testEngine(t testing.TB) *Engine {
	lib := librimePath(t)
	sharedOnce.Do(func() {
		sharedDir, sharedErr = os.MkdirTemp("/tmp", "shixin-engine-test-")
		if sharedErr != nil {
			return
		}
		data, _ := filepath.Abs("testdata/rime")
		user := filepath.Join(sharedDir, "user")
		release, err := Lock(user)
		if err != nil {
			sharedErr = err
			return
		}
		_ = release // held for the life of the test binary
		opt := Options{
			LibPath:       lib,
			SharedDataDir: data,
			UserDataDir:   user,
			LogDir:        sharedDir,
			MinLogLevel:   2,
			Version:       "test",
			Deploy:        true,
			Warmup:        []string{"nihao"},
			RerankTimeout: testRerankTimeout,
		}
		// SHIXIN_TEST_RERANK=off compares the key path without 智能排序.
		if os.Getenv("SHIXIN_TEST_RERANK") != "off" {
			opt.Scorer, opt.Rerank = testScorer, true
		}
		shared, sharedErr = Start(opt)
		testScorer.e = shared
	})
	if sharedErr != nil {
		t.Fatal(sharedErr)
	}
	return shared
}

func TestMain(m *testing.M) {
	code := m.Run()
	if shared != nil {
		shared.Stop()
	}
	if sharedDir != "" {
		os.RemoveAll(sharedDir)
	}
	os.Exit(code)
}

// client is a minimal synchronous test client.
type client struct {
	t    testing.TB
	c    net.Conn
	r    *ipc.Reader
	buf  []byte
	seq  uint32
	pols map[uint16]*policy.Policy
	res  ipc.Result
	cur  uint16 // policy for the next key
}

func dial(t testing.TB, e *Engine) *client {
	a, b := socketPair(t)
	e.ServeConn(b)
	cl := &client{t: t, c: a, r: ipc.NewReader(a, 4096), buf: make([]byte, 0, 256), pols: map[uint16]*policy.Policy{}}
	cl.send((&ipc.Hello{Version: ipc.Version, Kind: ipc.KindTest, PID: uint32(os.Getpid())}).Append(cl.buf[:0]))
	typ, payload, err := cl.r.Next()
	var w ipc.Welcome
	if err != nil || typ != ipc.TypeWelcome || w.Decode(payload) != nil || w.Version != ipc.Version {
		t.Fatalf("welcome: %v %v %+v", typ, err, w)
	}
	cl.send((&ipc.FocusIn{Ctx: 1, App: []byte("test")}).Append(cl.buf[:0]))
	cl.result()
	t.Cleanup(func() { a.Close() })
	return cl
}

// socketPair returns two connected Unix sockets.
func socketPair(t testing.TB) (net.Conn, net.Conn) {
	dir, err := os.MkdirTemp("/tmp", "sx-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	l, err := ipc.Listen(filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ch := make(chan net.Conn, 1)
	go func() {
		c, _ := ipc.Accept(l)
		ch <- c
	}()
	a, err := ipc.Dial(filepath.Join(dir, "s"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return a, <-ch
}

func (cl *client) send(frame []byte) {
	if _, err := cl.c.Write(frame); err != nil {
		cl.t.Fatal(err)
	}
}

// result reads frames up to the next Result, storing policies on the way.
func (cl *client) result() *ipc.Result {
	cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		typ, payload, err := cl.r.Next()
		if err != nil {
			cl.t.Fatal(err)
		}
		switch typ {
		case ipc.TypePolicy:
			p := &policy.Policy{}
			if err := ipc.DecodePolicy(payload, p); err != nil {
				cl.t.Fatal(err)
			}
			cl.pols[p.ID] = p
		case ipc.TypeResult:
			if err := cl.res.Decode(payload); err != nil {
				cl.t.Fatal(err)
			}
			if cl.pols[cl.res.Policy] == nil {
				cl.t.Fatalf("result refers to policy %d before sending it", cl.res.Policy)
			}
			cl.cur = cl.res.Policy
			return &cl.res
		default:
			cl.t.Fatalf("unexpected frame type %d", typ)
		}
	}
}

func (cl *client) key(keysym, mods uint32) *ipc.Result {
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: keysym, Mods: mods}).Append(cl.buf[:0]))
	r := cl.result()
	if r.Seq != cl.seq {
		cl.t.Fatalf("result seq %d, want %d", r.Seq, cl.seq)
	}
	return r
}

func (cl *client) typ(s string) *ipc.Result {
	var r *ipc.Result
	for i := 0; i < len(s); i++ {
		var mods uint32
		if shifted(s[i]) {
			mods = policy.ShiftMask
		}
		r = cl.key(uint32(s[i]), mods)
	}
	return r
}

func (cl *client) policy() *policy.Policy { return cl.pols[cl.cur] }

func TestTyping(t *testing.T) {
	cl := dial(t, testEngine(t))
	r := cl.typ("nihao")
	if r.Flags&ipc.ResultComposing == 0 || len(r.Candidates) == 0 || string(r.Candidates[0].Text) != "你好" {
		t.Fatalf("after nihao: flags %#x cands %d", r.Flags, len(r.Candidates))
	}
	if string(r.Preedit) != "ni hao" {
		t.Errorf("preedit %q", r.Preedit)
	}
	r = cl.key(' ', 0)
	if string(r.Commit) != "你好" || r.Flags&ipc.ResultComposing != 0 || r.Flags&ipc.ResultHandled == 0 {
		t.Fatalf("after space: commit %q flags %#x", r.Commit, r.Flags)
	}
	cl.typ("ni")
	r = cl.key('2', 0)
	if string(r.Commit) != "泥" { // 你 50, 泥 40
		t.Fatalf("select 2: commit %q", r.Commit)
	}
	// Mouse selection and paging go through the same path.
	cl.typ("shi")
	cl.seq++
	cl.send((&ipc.Select{Ctx: 1, Seq: cl.seq, Index: 0}).Append(cl.buf[:0]))
	if r := cl.result(); string(r.Commit) != "是" && string(r.Commit) != "拾新" {
		t.Fatalf("select: commit %q", r.Commit)
	}
}

func TestPolicies(t *testing.T) {
	cl := dial(t, testEngine(t))
	cl.seq++
	cl.send((&ipc.Clear{Ctx: 1, Seq: cl.seq}).Append(cl.buf[:0]))
	cl.result()
	idle := cl.policy()
	type c struct {
		keysym, mods uint32
		want         policy.Action
	}
	check := func(name string, p *policy.Policy, cases []c) {
		t.Helper()
		for _, k := range cases {
			if got := p.Lookup(k.keysym, k.mods); got != k.want {
				t.Errorf("%s: Lookup(%#x, %#x) = %v/%d, want %v/%d", name, k.keysym, k.mods,
					got.Disposition(), got.Next(), k.want.Disposition(), k.want.Next())
			}
		}
	}
	eatC := policy.Act(policy.Eat, policy.Composing)
	eatS := policy.Act(policy.Eat, policy.Same)
	eatU := policy.Act(policy.Eat, policy.Unknown)
	pass := policy.Act(policy.Pass, policy.Same)
	ask := policy.Act(policy.Ask, policy.Unknown)
	notify := policy.Act(policy.PassNotify, policy.Same)
	// With 智能排序, the engine hears about idle caret moves.
	caret := pass
	if testEngine(t).rr.attached {
		caret = notify
	}
	check("chinese", idle, []c{
		{'a', 0, eatC},
		{',', 0, eatS},   // commits ，
		{'/', 0, eatC},   // opens a menu of choices
		{'1', 0, notify}, // the engine tracks digits for 3.5
		{'.', 0, eatU},   // after a digit, a pending separator
		{' ', 0, pass},
		{policy.KeyReturn, 0, pass},
		{policy.KeyLeft, 0, caret},
		{policy.KeyShiftL, 0, notify},
		{policy.KeyControlL, 0, pass}, // switch_key Control_L: noop
		{'`', policy.ControlMask, ask},
		{'.', policy.ControlMask, ask}, // key_binder always binding
		{'c', policy.SuperMask, pass},
		{0xffc1, 0, ask}, // F4
	})
	r := cl.typ("ni")
	comp := cl.policy()
	if r.Flags&ipc.ResultComposing == 0 || comp.ID == idle.ID {
		t.Fatalf("composing policy not used: flags %#x id %d", r.Flags, comp.ID)
	}
	check("composing", comp, []c{
		{'b', 0, eatC},
		{'1', 0, eatU},
		{' ', 0, eatU},
		{',', 0, eatU},
		{policy.KeyReturn, 0, eatU},
		{policy.KeyBackSpace, 0, eatU},
		{policy.KeyEscape, 0, eatU},
		{policy.KeyLeft, 0, ask},
		{policy.KeyUp, 0, ask},
		{policy.KeyReturn, policy.ShiftMask, ask},
		{'c', policy.SuperMask, ask},
	})
	cl.key(policy.KeyEscape, 0)

	// Shift tap: switch to ASCII mode and its policy.
	cl.key(policy.KeyShiftL, 0)
	r = cl.key(policy.KeyShiftL, policy.ReleaseMask)
	if r.Flags&ipc.ResultASCII == 0 {
		t.Fatal("Shift tap didn't switch to ASCII mode")
	}
	check("ascii", cl.policy(), []c{
		{'a', 0, pass},
		{',', 0, pass},
		{policy.KeyShiftL, 0, notify},
	})
	cl.key(policy.KeyShiftL, 0)
	if r = cl.key(policy.KeyShiftL, policy.ReleaseMask); r.Flags&ipc.ResultASCII != 0 {
		t.Fatal("second Shift tap didn't switch back")
	}
}

// TestPolicyMatchesLibrime is the policy-correctness gate in miniature
// (DESIGN §9): for every key, the disposition predicted by the policy in
// force must agree with what librime does. EAT on a key librime declines is
// only allowed for printable keys (the client repairs it), and PASS on a key
// librime handles is never allowed.
func TestPolicyMatchesLibrime(t *testing.T) {
	cl := dial(t, testEngine(t))
	keys := []struct {
		keysym, mods uint32
	}{}
	for _, s := range []string{"nihao ", "ni2", "shiyan,", "/", "zhongguo.", "a1b ", "x/y ", "3.5", "12:30 ", "1..", "7.a "} {
		for i := 0; i < len(s); i++ {
			var m uint32
			if shifted(s[i]) {
				m = policy.ShiftMask
			}
			keys = append(keys, struct{ keysym, mods uint32 }{uint32(s[i]), m})
		}
	}
	for _, k := range []uint32{policy.KeyReturn, policy.KeyBackSpace, policy.KeyEscape, policy.KeyLeft, policy.KeyRight, policy.KeyUp, policy.KeyDown, policy.KeyPrior, policy.KeyNext, policy.KeyHome, policy.KeyEnd, policy.KeyTab, policy.KeyDelete} {
		keys = append(keys, struct{ keysym, mods uint32 }{'n', 0}, struct{ keysym, mods uint32 }{'i', 0},
			struct{ keysym, mods uint32 }{k, 0}, struct{ keysym, mods uint32 }{k, 0}, struct{ keysym, mods uint32 }{k, 0})
		keys = append(keys, struct{ keysym, mods uint32 }{policy.KeyEscape, 0}, struct{ keysym, mods uint32 }{k, 0})
	}
	composing := false
	for i, k := range keys {
		a := cl.policy().Lookup(k.keysym, k.mods)
		d := a.Disposition()
		r := cl.key(k.keysym, k.mods)
		handled := r.Flags&ipc.ResultHandled != 0
		printable := k.keysym >= 0x20 && k.keysym < 0x7f
		switch {
		case d == policy.Eat && !handled && !printable:
			t.Errorf("key %d %#x: EAT but librime declined a non-printable key", i, k.keysym)
		case (d == policy.Pass || d == policy.PassNotify) && handled:
			t.Errorf("key %d %#x: PASS but librime handled it", i, k.keysym)
		}
		// The client classifies the next key with the predicted state.
		now := r.Flags&ipc.ResultComposing != 0
		if a.Next() == policy.Same && now != composing || a.Next() == policy.Composing && !now {
			t.Errorf("key %d %q: predicted next %d, composing %v → %v", i, rune(k.keysym), a.Next(), composing, now)
		}
		composing = now
	}
	cl.key(policy.KeyEscape, 0)
}

func TestKeyPathDoesNotAllocate(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector allocates")
	}
	for _, englishPunct := range []bool{false, true} {
		name := "chinese-punctuation"
		if englishPunct {
			name = "english-punctuation"
		}
		t.Run(name, func(t *testing.T) {
			cl := dial(t, testEngine(t))
			cl.setPunctuation(englishPunct)
			cl.typ("ni") // warm buffers
			cl.key(policy.KeyEscape, 0)
			keys := []uint32{'n', 'i', 'h', 'a', 'o', policy.KeyEscape}
			runtime.GC()
			n := testing.AllocsPerRun(200, func() {
				for _, k := range keys {
					cl.seq++
					cl.buf = (&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: k}).Append(cl.buf[:0])
					cl.c.Write(cl.buf)
					for {
						typ, payload, err := cl.r.Next()
						if err != nil {
							t.Fatal(err)
						}
						if typ == ipc.TypeResult {
							cl.res.Decode(payload)
							break
						}
					}
				}
			})
			if n != 0 {
				t.Fatalf("%v allocations per %d keys (engine + client)", n, len(keys))
			}
		})
	}
}

func TestStuckClientIsDropped(t *testing.T) {
	e := testEngine(t)
	stuck, b := socketPair(t)
	defer stuck.Close()
	e.ServeConn(b)
	var buf []byte
	buf = (&ipc.Hello{Version: ipc.Version}).Append(buf)
	for i := 0; i < 5000; i++ {
		buf = (&ipc.Key{Ctx: 1, Seq: uint32(i), Keysym: 'a'}).Append(buf)
		buf = (&ipc.Key{Ctx: 1, Seq: uint32(i), Keysym: policy.KeyEscape}).Append(buf)
	}
	go stuck.Write(buf) // never reads results
	// Another client is still served promptly.
	cl := dial(t, e)
	t0 := time.Now()
	for time.Since(t0) < 2*time.Second && e.Stats().Dropped == 0 {
		cl.key('a', 0)
		cl.key(policy.KeyEscape, 0)
	}
	if e.Stats().Dropped == 0 {
		t.Fatal("stuck client was never dropped")
	}
}

func TestLock(t *testing.T) {
	dir := t.TempDir()
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); err == nil {
		t.Fatal("second lock succeeded")
	}
	release()
	release2, err := Lock(dir)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	release2()
	if err := StopRunning(dir, time.Second); err != nil {
		t.Fatalf("StopRunning with no engine: %v", err)
	}
}

func TestParseKey(t *testing.T) {
	for _, c := range []struct {
		in           string
		keysym, mods uint32
		ok           bool
	}{
		{"Control+grave", '`', policy.ControlMask, true},
		{"Control+Shift+grave", '`', policy.ControlMask | policy.ShiftMask, true},
		{"F4", 0xffc1, 0, true},
		{"a", 'a', 0, true},
		{"Shift+Tab", policy.KeyTab, policy.ShiftMask, true},
		{"Control++", '+', policy.ControlMask, true},
		{"0xff0d", policy.KeyReturn, 0, true},
		{"Hyper+a", 0, 0, false},
		{"NoSuchKey", 0, 0, false},
	} {
		k, m, ok := parseKey(c.in)
		if k != c.keysym || m != c.mods || ok != c.ok {
			t.Errorf("parseKey(%q) = %#x %#x %v", c.in, k, m, ok)
		}
	}
}

func BenchmarkKey(b *testing.B) {
	benchmarkKey(b, false)
}

func BenchmarkKeyEnglishPunctuation(b *testing.B) {
	benchmarkKey(b, true)
}

func benchmarkKey(b *testing.B, englishPunct bool) {
	cl := dial(b, testEngine(b))
	cl.setPunctuation(englishPunct)
	keys := []uint32{'n', 'i', 'h', 'a', 'o', policy.KeyEscape}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cl.key(keys[i%len(keys)], 0)
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "µs/key")
}

// TestSelectCharacter covers librime-shixin's 以词定字 through the engine.
// It needs our librime build (SHIXIN_LIBRIME=build/librime/out/lib/librime.1.dylib).
func TestSelectCharacter(t *testing.T) {
	e := testEngine(t)
	if !e.Native() {
		t.Skip("librime without librime-shixin; build it with scripts/build-librime.sh")
	}
	cl := dial(t, e)
	for _, c := range []struct {
		key  uint32
		want string
	}{{'[', "实"}, {']', "验"}} { // 实验 70 > 试验 60
		r := cl.typ("shiyan")
		if firstText(r) != "实验" {
			t.Fatalf("first candidate %q", firstText(r))
		}
		r = cl.key(c.key, 0)
		if string(r.Commit) != c.want || r.Flags&ipc.ResultComposing != 0 {
			t.Fatalf("%c: commit %q flags %#x", c.key, r.Commit, r.Flags)
		}
	}
	// A single character isn't split; '[' falls through to the punctuator.
	cl.typ("ni")
	cl.key(policy.KeyEscape, 0)
}

func (cl *client) focus(app string, flags uint8) bool {
	cl.send((&ipc.FocusIn{Ctx: 1, Flags: flags, App: []byte(app)}).Append(cl.buf[:0]))
	return cl.result().Flags&ipc.ResultASCII != 0
}

func (cl *client) setASCII(on bool) {
	cl.seq++
	cl.send((&ipc.SetOption{Ctx: 1, Seq: cl.seq, On: on, Name: []byte("ascii_mode")}).Append(cl.buf[:0]))
	cl.result()
}

func TestAppModes(t *testing.T) {
	e := testEngine(t)
	e.SetApps(AppModes{English: map[string]bool{"term": true}})
	t.Cleanup(func() { e.SetApps(AppModes{}) })
	cl := dial(t, e)
	steps := func(name string, want ...any) {
		t.Helper()
		for i := 0; i < len(want); i += 2 {
			if got := cl.focus(want[i].(string), 0); got != want[i+1].(bool) {
				t.Fatalf("%s: focus %s: English %v, want %v", name, want[i], got, want[i+1])
			}
		}
	}
	// A rule applies whenever its app gets focus; other apps share the mode.
	steps("rule", "term", true, "edit", true)
	cl.setASCII(false)
	steps("rule again", "term", true)

	// Remembered modes win over rules.
	e.SetApps(AppModes{English: map[string]bool{"term": true}, Remember: true})
	steps("remember", "edit", true)
	cl.setASCII(false)
	steps("remember", "term", true, "edit", false)
	steps("remember", "term", true)
	cl.setASCII(false)
	steps("remember", "edit", false, "term", false)

	// Secure fields are English only, and the mode comes back after.
	if !cl.focus("term", ipc.FocusSecure) {
		t.Fatal("secure field not English")
	}
	if cl.focus("term", 0) {
		t.Fatal("mode not restored after the secure field")
	}

	// Entering an app through a secure field still applies its rule, and
	// remembers the mode of the app left.
	e.SetApps(AppModes{English: map[string]bool{"term": true}})
	steps("secure entry", "edit", false)
	cl.focus("term", ipc.FocusSecure)
	steps("secure entry", "term", true)
	e.SetApps(AppModes{Remember: true})
	steps("secure remember", "a", true)
	cl.setASCII(true)
	cl.focus("b", ipc.FocusSecure)
	cl.focus("b", 0)
	cl.setASCII(false)
	steps("secure remember", "a", true)
}

func TestAutoSpaceFlag(t *testing.T) {
	e := testEngine(t)
	t.Cleanup(func() { e.SetApps(AppModes{}) })
	cl := dial(t, e)
	flag := func(app string, flags uint8) bool {
		cl.send((&ipc.FocusIn{Ctx: 1, Flags: flags, App: []byte(app)}).Append(cl.buf[:0]))
		return cl.result().Flags&ipc.ResultAutoSpace != 0
	}
	if flag("edit", 0) {
		t.Fatal("auto spacing on by default")
	}
	e.SetApps(AppModes{AutoSpace: true, NoAutoSpace: map[string]bool{"term": true}})
	if !flag("edit", 0) || flag("term", 0) || flag("edit", ipc.FocusSecure) || !flag("edit", 0) {
		t.Fatal("ResultAutoSpace ignores the app rules or the secure field")
	}
	// Every result carries it, not just FocusIn's.
	cl.seq++
	cl.send((&ipc.Key{Ctx: 1, Seq: cl.seq, Keysym: 'n'}).Append(cl.buf[:0]))
	if cl.result().Flags&ipc.ResultAutoSpace == 0 {
		t.Fatal("key result without ResultAutoSpace")
	}
}

func TestStatsRequest(t *testing.T) {
	e := testEngine(t)
	cl := dial(t, e)
	cl.typ("ni")
	cl.key(policy.KeyEscape, 0)
	cl.send((&ipc.StatsRequest{}).Append(cl.buf[:0]))
	cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	typ, payload, err := cl.r.Next()
	var st ipc.Stats
	if err != nil || typ != ipc.TypeStats || st.Decode(payload) != nil {
		t.Fatalf("stats: %v %v", typ, err)
	}
	if st.Keys < 3 || st.KeyTime.Count() < 3 || len(st.Librime) == 0 || string(st.Schemas) == "" {
		t.Errorf("stats %+v", st)
	}
	if e.rr.attached && (st.Rerank == nil || !st.Rerank.On) {
		t.Errorf("智能排序 stats %+v", st.Rerank)
	}
}
