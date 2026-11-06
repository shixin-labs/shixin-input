package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"shixin/internal/rime"
	"strings"
)

// Offline native replay; private user directory, warm-only schema, no AI.
func captureNative(root string) error {
	base, err := filepath.Abs("build/typo-recovery")
	if err != nil {
		return err
	}
	schema, err := os.ReadFile(filepath.Join(base, "generation/build/rime_ice_warm.schema.yaml"))
	if err != nil {
		return err
	}
	if !strings.Contains(string(schema), "enable_user_dict: false") || strings.Contains(string(schema), "enable_user_dict: true") {
		return fmt.Errorf("expected learning-disabled schema")
	}
	rows, err := read[struct{ Code string }](filepath.Join(root, "codes.jsonl"))
	if err != nil {
		return err
	}
	out, err := os.OpenFile(filepath.Join(root, "menus.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	user, err := os.MkdirTemp("", "shixin-hypothesis-capture-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(user)
	if err := os.CopyFS(filepath.Join(user, "opencc"), os.DirFS(filepath.Join(base, "share/opencc"))); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lib, err := rime.Load(filepath.Join(base, "lib/librime.1.dylib"))
	if err != nil {
		return err
	}
	lib.Setup(rime.Traits{SharedDataDir: filepath.Join(base, "rime-data"), UserDataDir: user, PrebuiltDataDir: filepath.Join(base, "prebuilt"), StagingDir: filepath.Join(base, "generation/build"), LogDir: user, MinLogLevel: 2, Modules: []string{"default", "shixin"}, AppName: "rime.shixin-hypotheses"})
	lib.Initialize()
	defer lib.Finalize()
	session, err := lib.CreateSession()
	if err != nil {
		return err
	}
	defer session.Destroy()
	if !session.SelectSchema("rime_ice_warm") {
		return fmt.Errorf("warm schema unavailable")
	}
	fresh := func(code string) (*rime.Session, error) {
		session.ClearComposition()
		session.ReadCommit(nil)
		for _, key := range code {
			session.ProcessKey(int(key), 0)
		}
		return session, nil
	}
	encoder := json.NewEncoder(out)
	for n, row := range rows {
		s, e := fresh(row.Code)
		if e != nil {
			return e
		}
		m := menu{Code: row.Code, Complete: true}
		type position struct{ page, index int }
		var positions []position
		var ctx rime.Context
		for {
			if !s.ReadContext(&ctx) {
				return fmt.Errorf("context unavailable")
			}
			for i, c := range ctx.Candidates {
				if len(m.Candidates) == 20 {
					break
				}
				m.Candidates = append(m.Candidates, candidate{Text: string(c.Text)})
				positions = append(positions, position{ctx.PageNo, i})
			}
			if len(m.Candidates) == 20 || ctx.IsLastPage || len(ctx.Candidates) == 0 {
				break
			}
			if !s.ChangePage(false) {
				return fmt.Errorf("paging failed")
			}
		}
		for i, pos := range positions {
			probe, e := fresh(row.Code)
			if e != nil {
				return e
			}
			for page := 0; page < pos.page; page++ {
				if !probe.ChangePage(false) {
					return fmt.Errorf("probe paging failed")
				}
			}
			if !probe.ReadContext(&ctx) || pos.index >= len(ctx.Candidates) || string(ctx.Candidates[pos.index].Text) != m.Candidates[i].Text {
				return fmt.Errorf("candidate changed: %s", row.Code)
			}
			selected := probe.SelectCandidateOnCurrentPage(pos.index)
			commit, ok := probe.ReadCommit(nil)
			probe.ReadContext(&ctx)
			m.Candidates[i].Full = selected && ok && string(commit) == m.Candidates[i].Text && len(ctx.Preedit) == 0
		}
		if err := encoder.Encode(m); err != nil {
			return err
		}
		if (n+1)%1000 == 0 {
			fmt.Printf("captured %d/%d\n", n+1, len(rows))
		}
	}
	return nil
}
