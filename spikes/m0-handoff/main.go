// Command m0-handoff is M0 spike 4 (DESIGN §3.5): can a standby engine
// process take over the user dictionary from a running one without losing
// learned words, and how long is the engine unavailable?
//
// librime is process-global, so each engine is a child process driven over
// stdin/stdout. Scenarios run on scratch copies of an already deployed user
// dir (build/spike/user-nolua from spike 1).
//
//	go run ./spikes/m0-handoff            # both scenarios
//
// Spike code: throwaway, not part of the product.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"shixin/internal/rime"
)

const librimePath = "/opt/homebrew/opt/librime/lib/librime.1.dylib"

func main() {
	child := flag.Bool("child", false, "run as an engine child (internal)")
	user := flag.String("user", "", "Rime user data dir (child)")
	shared := flag.String("shared", "", "Rime shared data dir (child)")
	staging := flag.String("staging", "", "Rime staging (build) dir; default <user>/build (child)")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if *child {
		runChild(*user, *shared, *staging)
		return
	}
	shared0 := filepath.Join(*root, "build/rime-data")
	fixture := filepath.Join(*root, "build/spike/user-nolua")
	danger(scratch(*root, fixture, "danger"), shared0)
	handoff(scratch(*root, fixture, "handoff"), shared0, false)
	handoff(scratch(*root, fixture, "handoff-warm"), shared0, true)
}

// ---------------------------------------------------------------- child

func runChild(user, shared, staging string) {
	lib, err := rime.Load(librimePath)
	must(err)
	lib.Setup(rime.Traits{
		SharedDataDir: shared, UserDataDir: user, StagingDir: staging, LogDir: "",
		MinLogLevel: 0, DistributionName: "Shixin", DistributionCodeName: "shixin",
		DistributionVersion: "0.0.0-spike", AppName: "rime.shixin", Modules: []string{"default"},
	})
	lib.Initialize()
	var s, warm *rime.Session
	var ctx rime.Context
	out := bufio.NewWriter(os.Stdout)
	reply := func(format string, a ...any) {
		fmt.Fprintf(out, format+"\n", a...)
		out.Flush()
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		f := strings.Fields(in.Text())
		if len(f) == 0 {
			continue
		}
		t := time.Now()
		switch f[0] {
		case "session": // session [SCHEMA]: create, optionally select SCHEMA
			s, err = lib.CreateSession()
			if err != nil {
				reply("err %v", err)
				continue
			}
			if len(f) > 1 {
				s.SelectSchema(f[1])
			}
			var st rime.Status
			s.ReadStatus(&st)
			reply("ok %v schema=%s", time.Since(t), st.SchemaID)
		case "cands": // cands KEYS: type, list the page, clear
			s.ClearComposition()
			s.SimulateKeySequence(f[1])
			s.ReadContext(&ctx)
			var b []string
			for _, c := range ctx.Candidates {
				b = append(b, string(c.Text))
			}
			s.ClearComposition()
			reply("ok %v %s", time.Since(t), strings.Join(b, ","))
		case "learn": // learn KEYS INDEX N: N times type KEYS and pick INDEX
			idx, _ := strconv.Atoi(f[2])
			n, _ := strconv.Atoi(f[3])
			for i := 0; i < n; i++ {
				s.ClearComposition()
				s.SimulateKeySequence(f[1])
				s.SelectCandidateOnCurrentPage(idx)
			}
			reply("ok %v", time.Since(t))
		case "warm": // warm KEYS: a session on rime_ice_warm (no user db), kept open
			warm, err = lib.CreateSession()
			if err != nil {
				reply("err %v", err)
				continue
			}
			var st rime.Status
			warm.ReadStatus(&st)
			warm.SimulateKeySequence(f[1])
			warm.ReadContext(&ctx)
			warm.ClearComposition()
			reply("ok %v schema=%s cands=%d", time.Since(t), st.SchemaID, len(ctx.Candidates))
		case "promote": // promote SCHEMA: switch the warm session to SCHEMA and use it
			s, warm = warm, nil
			s.SelectSchema(f[1])
			var st rime.Status
			s.ReadStatus(&st)
			reply("ok %v schema=%s", time.Since(t), st.SchemaID)
		case "release": // end every session, which closes the user db
			s.Destroy()
			s = nil
			reply("ok %v", time.Since(t))
		case "prefault": // read compiled data so it's in the page cache
			var n int64
			files, _ := filepath.Glob(filepath.Join(user, "build", "*.bin"))
			for _, p := range files {
				if fh, err := os.Open(p); err == nil {
					m, _ := io.Copy(io.Discard, fh)
					n += m
					fh.Close()
				}
			}
			reply("ok %v %dMB", time.Since(t), n>>20)
		case "quit":
			lib.Finalize()
			reply("ok")
			return
		default:
			reply("err unknown %q", f[0])
		}
	}
}

// ---------------------------------------------------------------- orchestrator

type engine struct {
	name string
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Scanner
}

func start(name, user, shared string, staging ...string) *engine {
	self, err := os.Executable()
	must(err)
	args := []string{"-child", "-user", user, "-shared", shared}
	if len(staging) > 0 {
		args = append(args, "-staging", staging[0])
	}
	cmd := exec.Command(self, args...)
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	must(err)
	outp, err := cmd.StdoutPipe()
	must(err)
	must(cmd.Start())
	return &engine{name: name, cmd: cmd, in: in, out: bufio.NewScanner(outp)}
}

// do sends a command and returns the reply.
func (e *engine) do(cmd string) string {
	fmt.Fprintln(e.in, cmd)
	if !e.out.Scan() {
		log.Fatalf("%s: no reply to %q", e.name, cmd)
	}
	r := e.out.Text()
	fmt.Printf("  %s> %-28s %s\n", e.name, cmd, r)
	return r
}

func (e *engine) quit() {
	e.do("quit")
	e.cmd.Wait()
}

// lockHolder reports the pid holding LevelDB's LOCK on the user db, or 0.
func lockHolder(user string) int {
	f, err := os.OpenFile(filepath.Join(user, "rime_ice.userdb", "LOCK"), os.O_RDWR, 0)
	if err != nil {
		return 0
	}
	defer f.Close()
	lk := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(f.Fd(), unix.F_GETLK, &lk); err != nil || lk.Type == unix.F_UNLCK {
		return 0
	}
	return int(lk.Pid)
}

func listDir(user string) string {
	ents, _ := os.ReadDir(user)
	var names []string
	for _, e := range ents {
		if strings.Contains(e.Name(), "userdb") {
			names = append(names, e.Name())
		}
	}
	return strings.Join(names, " ")
}

// danger: B creates a session while A holds the user db.
func danger(user, shared string) {
	fmt.Println("\n== danger: standby opens the user db while the old engine holds it")
	a := start("A", user, shared)
	a.do("session")
	a.do("learn shixin 3 5") // pick 狮心-ish candidate 4 five times
	fmt.Printf("  lock holder: %d (A is %d)\n", lockHolder(user), a.cmd.Process.Pid)
	b := start("B", user, shared)
	b.do("session")
	time.Sleep(2 * time.Second) // let B's deployer thread run any recovery
	b.do("cands shixin")
	fmt.Printf("  user dir after B: %s\n", listDir(user))
	a.do("cands shixin")
	a.do("learn shiyan 1 5") // A keeps learning after B's failed open (试验)
	a.do("cands shiyan")
	b.quit()
	a.quit()
	c := start("C", user, shared)
	c.do("session")
	c.do("cands shixin")
	c.do("cands shiyan")
	c.quit()
	fmt.Printf("  user dir at end: %s; leveldb repaired: %v\n", listDir(user), repaired(user))
}

// handoff: B waits for A to release before touching the user db. With warm,
// B first keeps a session on a schema variant without a user db, so the
// dictionaries are already loaded when the real session is created.
func handoff(user, shared string, warm bool) {
	fmt.Printf("\n== handoff (warm standby: %v): standby touches the user db only after release\n", warm)
	stagingB := filepath.Join(user, "build")
	if warm {
		stagingB = writeWarmGeneration(user)
	}
	a := start("A", user, shared)
	a.do("session")
	before := cands(a.do("cands shixin"))
	a.do("learn shixin 3 5")
	afterA := cands(a.do("cands shixin"))

	b := start("B", user, shared, stagingB) // standby: librime initialized, no user-db session
	b.do("prefault")
	if warm {
		b.do("warm jintiantianqizhenhao")
		fmt.Printf("  lock holder after warm-up: %d (A is %d)\n", lockHolder(user), a.cmd.Process.Pid)
	}
	// A keeps learning after B's warm-up: pick a two-character word below
	// the top so the whole input is committed.
	later := cands(a.do("cands shiyan"))
	li := 1
	for i := 1; i < len(later); i++ {
		if len([]rune(later[i])) == 2 {
			li = i
			break
		}
	}
	a.do(fmt.Sprintf("learn shiyan %d 5", li))
	fmt.Printf("  lock holder before release: %d (A is %d)\n", lockHolder(user), a.cmd.Process.Pid)

	t0 := time.Now()
	a.do("release")
	t1 := time.Now()
	holder := lockHolder(user)
	t2 := time.Now()
	if warm {
		b.do("promote rime_ice")
	} else {
		b.do("session rime_ice")
	}
	t3 := time.Now()
	r := b.do("cands shixin")
	t4 := time.Now()
	rw := b.do("cands shiyan")
	fmt.Printf("  lock holder after release: %d\n", holder)
	fmt.Printf("  unavailable: release %v + lock probe %v + session/promote %v + first query %v = %v (round trips included)\n",
		t1.Sub(t0).Round(time.Microsecond), t2.Sub(t1).Round(time.Microsecond),
		t3.Sub(t2).Round(time.Microsecond), t4.Sub(t3).Round(time.Microsecond), t4.Sub(t0).Round(time.Microsecond))
	afterB := cands(r)
	fmt.Printf("  learned %q: rank before %d, in A after %d, in B after handoff %d\n",
		before[3], 3, index(afterA, before[3]), index(afterB, before[3]))
	fmt.Printf("  learned after B's warm-up %q: rank before %d, in B after handoff %d\n", later[li], li, index(cands(rw), later[li]))
	fmt.Printf("  leveldb repaired: %v\n", repaired(user))
	a.quit()
	b.quit()
	fmt.Printf("  user dir at end: %s\n", listDir(user))
}

// writeWarmGeneration makes the standby's own staging dir, as the product's
// builder would for a new generation: hard links to the compiled data, a
// rime_ice_warm schema (same dictionaries, no user db), and a default.yaml
// whose schema_list holds only rime_ice_warm, so create_session never opens
// the user db. The engine selects rime_ice explicitly after the handoff.
func writeWarmGeneration(user string) string {
	src := filepath.Join(user, "build")
	dst := filepath.Join(user, "build-standby")
	must(os.MkdirAll(dst, 0o755))
	ents, err := os.ReadDir(src)
	must(err)
	for _, e := range ents {
		must(os.Link(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())))
	}
	rewrite := func(name, out string, repl [][2]string) {
		b, err := os.ReadFile(filepath.Join(src, name))
		must(err)
		s := string(b)
		for _, r := range repl {
			if strings.Count(s, r[0]) != 1 {
				log.Fatalf("%s: %q not found once", name, r[0])
			}
			s = strings.Replace(s, r[0], r[1], 1)
		}
		p := filepath.Join(dst, out)
		os.Remove(p) // drop the hard link before writing a different file
		must(os.WriteFile(p, []byte(s), 0o644))
	}
	rewrite("rime_ice.schema.yaml", "rime_ice_warm.schema.yaml", [][2]string{
		{"  schema_id: rime_ice\n", "  schema_id: rime_ice_warm\n"},
		{"\ntranslator:\n", "\ntranslator:\n  enable_user_dict: false\n"},
	})
	rewrite("default.yaml", "default.yaml", [][2]string{
		{"schema_list:\n  - schema: rime_ice\n", "schema_list:\n  - schema: rime_ice_warm\n"},
	})
	return dst
}

func repaired(user string) bool {
	_, err := os.Stat(filepath.Join(user, "rime_ice.userdb", "lost"))
	return err == nil
}

// cands parses "ok <dur> a,b,c" into its candidates.
func cands(reply string) []string {
	f := strings.Fields(reply)
	if len(f) < 3 {
		return nil
	}
	return strings.Split(f[2], ",")
}

func index(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

func scratch(root, fixture, name string) string {
	dir := filepath.Join(root, "build/spike/m0-handoff", name)
	must(os.RemoveAll(dir))
	must(os.MkdirAll(filepath.Dir(dir), 0o755))
	out, err := exec.Command("cp", "-R", fixture, dir).CombinedOutput()
	if err != nil {
		log.Fatalf("cp: %v %s", err, out)
	}
	return dir
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
