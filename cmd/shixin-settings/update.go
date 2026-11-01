package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"shixin/internal/process"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	shixupdate "shixin/internal/update"
)

// The in-app update (DESIGN §5.5, "In-app update"), on the 关于 page. It
// only ever runs when the user clicks: nothing checks by itself. Network
// work happens on goroutines; the installed front verifies and applies the
// download (`Shixin -apply-update`), because the upgrade stops this app.

type updatePhase int

const (
	updIdle updatePhase = iota
	updChecking
	updUpToDate
	updAvailable
	updDownloading
	updInstalling
	updFailed
)

// updateState is the update's progress, main thread only.
type updateState struct {
	phase       updatePhase
	rel         *shixupdate.Release // the latest release, once checked
	done, total int64
	err         string
	cancel      context.CancelFunc
}

// canInstall reports whether this build may install the release found.
func (a *app) canInstall() bool {
	return plat.selfUpdate && shixupdate.CanInstall(a.upd.rel, version) && a.front != ""
}

// checkUpdate asks GitHub for the latest release.
func (a *app) checkUpdate() {
	u := &a.upd
	if u.phase == updChecking || u.phase == updDownloading || u.phase == updInstalling {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.phase, u.rel, u.err, u.cancel = updChecking, nil, "", cancel
	go func() {
		rel, err := a.checkRelease(ctx, version)
		update(a, func() {
			cancel()
			a.finishCheck(rel, err)
		})
	}()
}

// finishCheck shows a check's result.
func (a *app) finishCheck(rel *shixupdate.Release, err error) {
	u := &a.upd
	u.cancel = nil
	switch {
	case err != nil:
		u.phase, u.err = updFailed, err.Error()
	case rel.Newer:
		u.phase, u.rel = updAvailable, rel
	default:
		u.phase, u.rel = updUpToDate, rel
	}
}

// installUpdate downloads the release, then starts the front to verify and
// apply it.
func (a *app) installUpdate() {
	u := &a.upd
	front := a.front
	rel := u.rel
	if !a.canInstall() || u.phase == updDownloading || u.phase == updInstalling {
		return
	}
	root, err := shixupdate.Dir()
	if err != nil {
		u.phase, u.err = updFailed, err.Error()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.phase, u.done, u.total, u.err, u.cancel = updDownloading, 0, rel.Size, "", cancel
	go func() {
		var last time.Time
		path, err := shixupdate.Download(ctx, rel, root, version, func(done, total int64) {
			if now := time.Now(); now.Sub(last) > 100*time.Millisecond {
				last = now
				update(a, func() { u.done = done })
			}
		})
		if err == nil {
			update(a, func() { u.phase, u.cancel = updInstalling, nil })
			err = a.applyUpdate(front, path, rel.Version)
		}
		update(a, func() {
			cancel()
			u.cancel = nil
			switch {
			case ctx.Err() != nil:
				u.phase = updAvailable
			case err != nil:
				u.phase, u.err = updFailed, err.Error()
			default:
				u.phase = updAvailable // the front exited without replacing us
			}
		})
	}()
}

// applyUpdate runs `Shixin -apply-update` and waits for it. It is its own
// session, so it outlives this app, which the upgrade stops once the
// download has been verified: the wait only ends here if it failed.
func (a *app) applyUpdate(front, dmg, ver string) error {
	cmd := exec.Command(front, "-apply-update", dmg, "-update-version", ver)
	process.Detach(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

// updateGroup is 更新 on the 关于 page.
func (a *app) updateGroup(c *ui.Context) {
	u := &a.upd
	p := colors(c)
	t := c.Theme()
	canInstall := a.canInstall()
	group(c, T("s.u.title"), T("s.u.note"), func() {
		desc := T("s.u.idle")
		switch u.phase {
		case updChecking:
			desc = T("s.u.checking")
		case updUpToDate:
			desc = T("s.u.uptodate")
		case updAvailable, updDownloading, updInstalling:
			desc = fmt.Sprintf(T("s.u.available"), u.rel.Version)
		case updFailed:
			desc = shorten(u.err, 240)
		}
		if u.phase == updAvailable || u.phase == updUpToDate {
			switch {
			case shixupdate.IsDevelopment(version):
				desc += " · " + T("s.u.dev")
			case u.phase == updAvailable && a.front == "" && plat.selfUpdate:
				desc += " · " + T("s.u.nofront")
			}
		}
		row(c, fmt.Sprintf(T("s.u.current"), version), desc, func() {
			switch u.phase {
			case updChecking:
				ui.Button(c, T("s.u.checking")).Disabled(true)
			case updAvailable:
				switch {
				case canInstall:
					ui.PrimaryButton(c, T("s.u.install")).OnClick(a.installUpdate)
				case !plat.selfUpdate:
					// No in-place update here yet: the release page has it.
					ui.Button(c, T("s.u.download")).OnClick(func() { go mygo.Shell.OpenExternal(releasesPage) })
				}
			case updDownloading:
				pct := 0
				if u.total > 0 {
					pct = int(u.done * 100 / u.total)
				}
				ui.Text(c, fmt.Sprintf(T("s.u.downloading"), pct)).FontSize(12.5).TextColor(p.muted).FontFeatures("tnum")
				ui.Button(c, T("s.cancel")).OnClick(func() { u.cancel() })
			case updInstalling:
				ui.Spinner(c)
			case updFailed:
				pill(c, T("s.u.failed"), t.Danger, iconAlert).Tooltip(u.err)
				if u.rel != nil && canInstall {
					ui.Button(c, T("s.u.retry")).OnClick(a.installUpdate)
				} else {
					ui.Button(c, T("s.u.retry")).OnClick(a.checkUpdate)
				}
			default:
				ui.Button(c, T("s.u.check")).OnClick(a.checkUpdate)
			}
		})
		switch u.phase {
		case updDownloading:
			block(c, func() {
				frac := 0.0
				if u.total > 0 {
					frac = float64(u.done) / float64(u.total)
				}
				ui.Progress(c, frac)
			})
		case updInstalling:
			block(c, func() {
				key := "s.u.installing"
				if plat.installingText != "" {
					key = plat.installingText
				}
				ui.Text(c, T(key)).FontSize(12.5).TextColor(p.muted).LineHeight(1.4)
			})
		}
		if u.rel != nil && u.rel.Newer && u.rel.Notes != "" {
			block(c, func() {
				ui.Text(c, T("s.u.notes")).FontSize(12).FontWeight(600).TextColor(p.muted)
				ui.Text(c, u.rel.Notes).FontSize(12.5).LineHeight(1.45)
			})
		}
	})
}

// shorten cuts s to n characters.
func shorten(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
