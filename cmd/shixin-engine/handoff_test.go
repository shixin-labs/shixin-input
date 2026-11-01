package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
	"shixin/internal/engine"
	"shixin/internal/generation"
	"shixin/internal/ipc"
	"shixin/internal/lifecycle"
	"shixin/internal/policy"
	"shixin/internal/settings"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "-worker" || os.Args[1] == "-user") {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fixture(t *testing.T) (*coordinator, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("slow: builds real rime-ice generations")
	}
	lib := os.Getenv("SHIXIN_LIBRIME")
	if lib == "" {
		lib = homebrewLibrime
	}
	if _, err := os.Stat(lib); err != nil {
		t.Skip("librime not available")
	}
	data, _ := filepath.Abs("../../build/rime-data")
	if _, err := os.Stat(filepath.Join(data, "rime_ice.schema.yaml")); err != nil {
		t.Skip("rime-ice not available")
	}
	root, err := ipc.TempDir("sx-swap-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	builder := filepath.Join(root, "builder")
	cmd := exec.Command("go", "build", "-o", builder, "../shixin-builder")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build builder: %v: %s", err, b)
	}
	c := config{lib: lib, data: data, user: filepath.Join(root, "user"), settings: filepath.Join(root, "settings.json"), socket: filepath.Join(root, "engine.sock"), builder: builder, log: "-"}
	os.MkdirAll(c.user, 0o700)
	opencc := filepath.Join(filepath.Dir(lib), "..", "share", "opencc")
	if _, err := os.Stat(filepath.Join(opencc, "s2t.json")); err == nil {
		if err := os.CopyFS(filepath.Join(c.user, "opencc"), os.DirFS(opencc)); err != nil {
			t.Fatal(err)
		}
	}
	s := settings.Default()
	if err := settings.Save(c.settings, s); err != nil {
		t.Fatal(err)
	}
	return &coordinator{c: c, applied: s, logf: t.Logf}, root
}

func mustBuild(t *testing.T, s *coordinator, next settings.Settings) string {
	t.Helper()
	dir, err := s.build(next)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustStart(t *testing.T, s *coordinator, dir string) *child {
	t.Helper()
	p, err := startChild(s.c, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.kill() })
	return p
}

func dbSnapshot(t *testing.T, user string) map[string][32]byte {
	t.Helper()
	out := map[string][32]byte{}
	err := filepath.WalkDir(filepath.Join(user, "rime_ice.userdb"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() == "LOCK" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[d.Name()] = sha256.Sum256(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStandbyPreservesLearning(t *testing.T) {
	s, _ := fixture(t)
	initial := mustBuild(t, s, s.applied)
	old := mustStart(t, s, initial)
	if err := s.activate(old); err != nil {
		t.Fatal(err)
	}
	cl := testDial(t, s.c.socket)
	// Pick a lower-ranked complete word and commit it repeatedly. It must
	// move to the top, rather than merely remain in the static dictionary.
	r := cl.typ("shiyan")
	chosen := ""
	index := -1
	for i, c := range r.Candidates {
		if i > 0 && len([]rune(string(c.Text))) == 2 {
			chosen = string(c.Text)
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("no alternate two-character word to learn")
	}
	cl.key(0xff1b)
	for i := 0; i < 6; i++ {
		r := cl.typ("shiyan")
		pick := -1
		for j, c := range r.Candidates {
			if string(c.Text) == chosen {
				pick = j
			}
		}
		if pick < 0 {
			t.Fatal("learning candidate disappeared")
		}
		cl.seq++
		cl.send((&ipc.Select{Ctx: 1, Seq: cl.seq, Index: uint32(pick)}).Append(nil))
		if r := cl.result(); string(r.Commit) != chosen {
			t.Fatal("phrase was not committed")
		}
	}
	if r := cl.typ("shiyan"); string(r.Candidates[0].Text) != chosen {
		t.Fatal("phrase was not learned")
	}
	cl.key(0xff1b)
	next := s.applied
	next.PageSize = 7
	staged := mustBuild(t, s, next)
	before := dbSnapshot(t, s.c.user)
	standby := mustStart(t, s, staged)
	if got := dbOwner(t, s.c.user); got != old.cmd.Process.Pid {
		t.Fatalf("standby touched live dictionary: owner %d", got)
	}
	if !maps.Equal(dbSnapshot(t, s.c.user), before) {
		t.Fatal("standby changed user dictionary files")
	}
	// It must also be impossible to promote while the old owner holds the
	// lock. No real schema selection may happen on this failure path.
	if _, err := standby.call(lifecycle.OpPromote); err == nil {
		t.Fatal("standby promoted without ownership")
	}
	if got := dbOwner(t, s.c.user); got != old.cmd.Process.Pid {
		t.Fatal("promotion attempt changed dictionary ownership")
	}
	// Continue learning after warm-up too.
	cl.typ("shiyan")
	cl.key(' ')
	// Start a composition: migration must defer without affecting typing.
	cl.typ("ni")
	if _, err := old.call(lifecycle.OpMigrate); !errors.Is(err, engine.ErrComposing) {
		t.Fatalf("composing migration: %v", err)
	}
	cl.key(0xff1b)
	if _, err := old.call(lifecycle.OpMigrate); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := cl.next()
	var m ipc.Migrate
	if err != nil || typ != ipc.TypeMigrate || m.Decode(payload) != nil || string(m.Endpoint) != s.c.socket {
		t.Fatalf("migrate frame: %v %v", typ, err)
	}
	// A final already-accepted key/commit behind Migrate must be answered
	// before EOF and release. This models the client's queued write drain.
	cl.typ("nihao")
	if r := cl.key(' '); !bytes.Equal(r.Commit, []byte("你好")) {
		t.Fatal("queued commit lost")
	}
	if half, ok := cl.c.(interface{ CloseWrite() error }); ok {
		half.CloseWrite()
	} else {
		cl.c.Close()
	}
	if _, _, err := cl.next(); err == nil {
		t.Fatal("old connection did not drain")
	}
	start := time.Now()
	release, err := old.stop()
	if err != nil {
		t.Fatal(err)
	}
	if owner := dbOwner(t, s.c.user); owner != 0 {
		t.Fatal("old worker retained LevelDB lock")
	}
	if err := s.activate(standby); err != nil {
		t.Fatal(err)
	}
	ready := time.Since(start)
	fresh := testDial(t, s.c.socket)
	r = fresh.typ("shiyan")
	if len(r.Candidates) == 0 || string(r.Candidates[0].Text) != chosen {
		t.Fatal("learned word lost during swap")
	}
	t.Logf("release %v; release-to-ready %v; through first query %v", release, ready, time.Since(start))
	if owner := dbOwner(t, s.c.user); owner != standby.cmd.Process.Pid {
		t.Fatal("new worker does not own user dictionary")
	}
	if _, err := os.Stat(filepath.Join(s.c.user, "rime_ice.userdb", "lost")); !os.IsNotExist(err) {
		t.Fatal("librime repaired the dictionary")
	}
	fresh.key(0xff1b)
	// The post-promotion switcher must expose real schemas, so selecting
	// the current schema cannot silently turn learning off.
	fresh.seq++
	fresh.send((&ipc.Key{Ctx: 1, Seq: fresh.seq, Keysym: '`', Mods: policy.ControlMask}).Append(nil))
	fresh.result()
	fresh.key('1')
	fresh.key(0xff1b)
	if r := fresh.typ("shiyan"); len(r.Candidates) == 0 || string(r.Candidates[0].Text) != chosen {
		t.Fatal("switcher selected a warm schema after takeover")
	}
	fresh.key(0xff1b)
	// The deployed file must remain safe for a future standby process.
	if err := generation.Validate(staged); err != nil {
		t.Fatal("promotion changed immutable warm config")
	}
	// Verify the immutable generation's phrase table is used on the real
	// schema. This also exercises a changed generated file and full apply.
	s.active = standby
	s.applied = next
	next.Phrases = []settings.Phrase{{Code: "yx", Text: "测试短语"}}
	fresh.c.Close()
	if err := s.apply(next); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.active.kill() })
	last := testDial(t, s.c.socket)
	if r := last.typ("yx"); len(r.Candidates) == 0 || string(r.Candidates[0].Text) != "测试短语" {
		t.Fatal("generation phrase table not active")
	}
	last.key(0xff1b)
	// An offline failure leaves the current worker and its dictionary open.
	live := s.active
	builder := s.c.builder
	s.c.builder = filepath.Join(filepath.Dir(builder), "missing-builder")
	next.PageSize = 9
	if err := s.apply(next); err == nil {
		t.Fatal("builder failure was hidden")
	}
	if s.active != live {
		t.Fatal("build failure replaced active engine")
	}
	if r := last.typ("nihao"); len(r.Candidates) == 0 {
		t.Fatal("old engine stopped after build failure")
	}
	last.key(0xff1b)
	// A schema that deploys but cannot produce a safe policy must fail in
	// standby warm-up, before the old generation receives Migrate.
	s.c.builder = builder
	source := filepath.Join(filepath.Dir(s.c.builder), "bad-warm-data")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.c.data)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "rime_ice.schema.yaml" {
			if err := os.Symlink(filepath.Join(s.c.data, entry.Name()), filepath.Join(source, entry.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(s.c.data, "rime_ice.schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := yaml.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	speller := schema["speller"].(map[string]any)
	speller["alphabet"] = "ABC"
	speller["initials"] = "ABC"
	b, err = yaml.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "rime_ice.schema.yaml"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	data := s.c.data
	s.c.data = source
	// Like the bundle's: compiled dictionaries only, no compiled YAML.
	bins := filepath.Join(filepath.Dir(s.c.builder), "bad-warm-prebuilt")
	if err := os.Mkdir(bins, 0o700); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(initial, "build", "*.bin"))
	for _, path := range files {
		if err := os.Link(path, filepath.Join(bins, filepath.Base(path))); err != nil {
			t.Fatal(err)
		}
	}
	s.c.prebuilt = bins
	if err := s.apply(next); err == nil || !strings.Contains(err.Error(), "standby warm-up") {
		t.Fatalf("warm-up failure: %v", err)
	}
	if s.active != live {
		t.Fatal("warm-up failure replaced active engine")
	}
	if r := last.typ("nihao"); len(r.Candidates) == 0 {
		t.Fatal("old engine stopped after warm-up failure")
	}

	last.key(0xff1b)
	last.c.Close()
	s.c.data = data
	next.Scheme = "double_pinyin_flypy"
	if err := s.apply(next); err != nil {
		t.Fatal(err)
	}
	double := testDial(t, s.c.socket)
	if r := double.typ("nihc"); len(r.Candidates) == 0 || string(r.Candidates[0].Text) != "你好" {
		t.Fatal("replacement scheme did not activate")
	}
	double.key(0xff1b)
	if owner := dbOwner(t, s.c.user); owner != s.active.cmd.Process.Pid {
		t.Fatal("replacement scheme lost dictionary ownership")
	}

}

func TestRejectUnsafeWarmGeneration(t *testing.T) {
	s, _ := fixture(t)
	dir := mustBuild(t, s, s.applied)
	path := filepath.Join(dir, "build", "rime_ice_warm.schema.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.ReplaceAll(b, []byte("enable_user_dict: false"), []byte("enable_user_dict: true"))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := generation.Validate(dir); err == nil {
		t.Fatal("unsafe warm generation validated")
	}
	p, err := startChild(s.c, dir)
	if err == nil {
		p.kill()
		t.Fatal("unsafe standby created sessions")
	}
	files, _ := filepath.Glob(filepath.Join(s.c.user, "*.userdb"))
	if len(files) != 0 {
		t.Fatal("rejected standby opened a user dictionary")
	}
}

type testClient struct {
	t      *testing.T
	c      net.Conn
	reader *ipc.Reader
	seq    uint32
}

func testDial(t *testing.T, socket string) *testClient {
	t.Helper()
	c, err := ipc.Dial(socket, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	cl := &testClient{t: t, c: c, reader: ipc.NewReader(c, 4096)}
	cl.send((&ipc.Hello{Version: ipc.Version, Kind: ipc.KindTest}).Append(nil))
	typ, _, err := cl.next()
	if err != nil || typ != ipc.TypeWelcome {
		t.Fatalf("hello: %v %v", typ, err)
	}
	cl.send((&ipc.FocusIn{Ctx: 1, App: []byte("test")}).Append(nil))
	cl.result()
	return cl
}

// ioTimeout bounds each exchange, not the connection: tests keep a client
// open while they build a generation, which takes longer on slow machines.
const ioTimeout = 15 * time.Second

func (c *testClient) send(b []byte) {
	c.t.Helper()
	c.c.SetWriteDeadline(time.Now().Add(ioTimeout))
	if _, err := c.c.Write(b); err != nil {
		c.t.Fatal(err)
	}
}

// next reads one frame.
func (c *testClient) next() (ipc.Type, []byte, error) {
	c.c.SetReadDeadline(time.Now().Add(ioTimeout))
	return c.reader.Next()
}

func (c *testClient) result() *ipc.Result {
	c.t.Helper()
	for {
		typ, b, err := c.next()
		if err != nil {
			c.t.Fatal(err)
		}
		if typ == ipc.TypePolicy {
			continue
		}
		if typ != ipc.TypeResult {
			c.t.Fatalf("unexpected frame %v", typ)
		}
		r := &ipc.Result{}
		if err := r.Decode(b); err != nil {
			c.t.Fatal(err)
		}
		return r
	}
}
func (c *testClient) key(k uint32) *ipc.Result {
	c.seq++
	c.send((&ipc.Key{Ctx: 1, Seq: c.seq, Keysym: k}).Append(nil))
	return c.result()
}
func (c *testClient) typ(s string) *ipc.Result {
	var r *ipc.Result
	for _, k := range s {
		r = c.key(uint32(k))
	}
	return r
}

// TestFirstRunTiming compares builder+warm+activation in independent empty
// user directories. Prebuilt contains only the .bin files shipped by the
// bundle, so this catches accidental reliance on compiled config there.
func TestFirstRunTiming(t *testing.T) {
	s, root := fixture(t)
	start := time.Now()
	cold := mustBuild(t, s, s.applied)
	coldBuild := time.Since(start)
	p := mustStart(t, s, cold)
	if err := s.activate(p); err != nil {
		t.Fatal(err)
	}
	coldReady := time.Since(start)
	p.stop()
	prebuilt := filepath.Join(root, "prebuilt")
	os.Mkdir(prebuilt, 0o700)
	files, err := filepath.Glob(filepath.Join(cold, "build", "*.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("builder produced no prebuilt dictionaries")
	}
	for _, path := range files {
		if err := os.Link(path, filepath.Join(prebuilt, filepath.Base(path))); err != nil {
			t.Fatal(err)
		}
	}
	s.c.prebuilt = prebuilt
	s.c.user = filepath.Join(root, "second-user")
	os.Mkdir(s.c.user, 0o700)
	opencc := filepath.Join(filepath.Dir(s.c.lib), "..", "share", "opencc")
	if _, err := os.Stat(filepath.Join(opencc, "s2t.json")); err == nil {
		if err := os.CopyFS(filepath.Join(s.c.user, "opencc"), os.DirFS(opencc)); err != nil {
			t.Fatal(err)
		}
	}
	start = time.Now()
	warm := mustBuild(t, s, s.applied)
	warmBuild := time.Since(start)
	q := mustStart(t, s, warm)
	if err := s.activate(q); err != nil {
		t.Fatal(err)
	}
	warmReady := time.Since(start)
	cl := testDial(t, s.c.socket)
	if r := cl.typ("nihao"); len(r.Candidates) == 0 {
		t.Fatal("prebuilt dictionary was not usable")
	}
	t.Logf("first run without prebuilt: build %v, ready %v; with prebuilt: build %v, ready %v", coldBuild, coldReady, warmBuild, warmReady)
}
