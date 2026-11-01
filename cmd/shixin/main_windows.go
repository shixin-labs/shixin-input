// Command shixin hosts Windows candidates and the engine supervisor. The
// native TSF DLL owns no UI and contains no Go runtime or librime.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/sys/windows"

	"shixin/internal/i18n"
	"shixin/internal/ipc"
	"shixin/internal/logx"
	"shixin/internal/settings"
	"shixin/internal/ui/brand"
	"shixin/internal/ui/candview"
	"shixin/internal/ui/fonts"
	"shixin/internal/ui/skin"
)

var version = "dev"
var user32 = windows.NewLazySystemDLL("user32.dll")
var foregroundWindow = user32.NewProc("GetForegroundWindow")
var windowPID = user32.NewProc("GetWindowThreadProcessId")
var moveWindow = user32.NewProc("SetWindowPos")
var dpiForWindow = user32.NewProc("GetDpiForWindow")
var monitorFromRect = user32.NewProc("MonitorFromRect")
var monitorInfo = user32.NewProc("GetMonitorInfoW")
var setWindowRegion = user32.NewProc("SetWindowRgn")
var gdi32 = windows.NewLazySystemDLL("gdi32.dll")
var roundRectRegion = gdi32.NewProc("CreateRoundRectRgn")
var deleteGDIObject = gdi32.NewProc("DeleteObject")

type windowsSession struct {
	id   uint64
	pid  uint32
	out  chan []byte
	done chan struct{}
}
type windowsHost struct {
	window   *mygo.Window
	mu       sync.Mutex
	display  presentation
	sessions map[uint64]*windowsSession
	queued   atomic.Bool
	shown    bool
	region   candidateRegion
	next     atomic.Uint64
	state    candview.State
	prefs    settings.Settings
	listener net.Listener
}

func foregroundPID() uint32 {
	hwnd, _, _ := foregroundWindow.Call()
	var pid uint32
	windowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}
func main() {
	var logger *logx.Logger
	if path := logx.DefaultPath("shixin.log"); path != "-" {
		logger, _ = logx.Open(path)
	}
	if logger == nil {
		logger = logx.New(os.Stderr)
	}
	err := runWindows(logger.Printf)
	if err != nil {
		logger.Printf("host: %v", err)
		fmt.Fprintln(os.Stderr, err)
	}
	logger.Close()
	if err != nil {
		os.Exit(1)
	}
}

func runWindows(logf func(string, ...any)) error {
	register := flag.Bool("register", false, "register the adjacent TSF DLL (Windows test VM only)")
	unregister := flag.Bool("unregister", false, "unregister the adjacent TSF DLL (Windows test VM only)")
	flag.Parse()
	if *register || *unregister {
		if *register && *unregister {
			return fmt.Errorf("choose register or unregister")
		}
		if err := registerWindows(*unregister); err != nil {
			return err
		}
		return nil
	}

	logf("host starting (version %s)", version)
	dir, err := ipc.RuntimeDir()
	if err != nil {
		return err
	}
	listener, err := ipc.Listen(filepath.Join(dir, "ui.sock"))
	if err != nil {
		return err
	}
	defer listener.Close()
	path, err := settings.Path()
	if err != nil {
		return err
	}
	prefs, _ := settings.Load(path)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	supervisor := newSupervisor(filepath.Join(filepath.Dir(exe), "shixin-engine.exe"), logf)
	supervisor.ensure(nil)
	host := &windowsHost{prefs: prefs, listener: listener, sessions: make(map[uint64]*windowsSession)}
	host.display.init()
	mygo.App.SetName(i18n.T("app.name"))
	mygo.App.OnWindowAllClosed(func() {})
	var startupErr error
	mygo.App.WhenReady(func() {
		var icon bytes.Buffer
		if err := png.Encode(&icon, brand.Icon(32)); err != nil {
			startupErr = err
			mygo.App.Quit()
			return
		}
		tray, err := mygo.NewTray(mygo.TrayOptions{ID: "shixin", Icon: icon.Bytes(), ToolTip: i18n.T("app.name"), Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: i18n.T("menu.settings"), Click: func(*mygo.MenuItem, *mygo.Window) {
				cmd := exec.Command(filepath.Join(filepath.Dir(exe), "shixin-settings.exe"))
				if err := cmd.Start(); err == nil {
					go cmd.Wait()
				}
			}},
			{Role: mygo.RoleQuit},
		})})
		if err != nil {
			startupErr = err
			mygo.App.Quit()
			return
		}
		fonts.Warm(prefs.FontFamily, float32(prefs.FontSize))
		host.window = mygo.NewFlyout(mygo.FlyoutOptions{Tray: tray, Hidden: true, Width: 1400, Height: 560, Shadow: false, Content: ui.View(host.view)})
		go host.accept()
		go settings.Watch(path, time.Second, nil, func(s settings.Settings) {
			fonts.Warm(s.FontFamily, float32(s.FontSize))
			host.window.Update(func() { host.prefs = s; host.window.Invalidate() })
		})
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if c, err := ipc.Dial(ipc.EnginePath(dir), 100*time.Millisecond); err != nil {
					supervisor.ensure(err)
				} else {
					c.Close()
				}
				host.mu.Lock()
				changed := host.display.visibleIn(foregroundPID()) != host.shown
				host.mu.Unlock()
				if changed {
					host.refresh()
				}
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return startupErr
}
func (h *windowsHost) refresh() {
	if !h.queued.CompareAndSwap(false, true) {
		return
	}
	h.window.Update(func() {
		h.queued.Store(false)
		h.mu.Lock()
		visible := h.display.visibleIn(foregroundPID())
		h.shown = visible
		if visible {
			h.display.update.apply(&h.state)
		}
		h.mu.Unlock()
		if visible {
			h.window.Show()
			h.window.Invalidate()
		} else {
			h.state.Clear()
			h.window.Hide()
		}
	})
}
func (h *windowsHost) accept() {
	slots := make(chan struct{}, 64)
	for {
		c, err := ipc.Accept(h.listener)
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
			go func() { defer func() { <-slots }(); h.serve(c) }()
		default:
			c.Close()
		}
	}
}
func (h *windowsHost) serve(c net.Conn) {
	defer c.Close()
	fd, ok := c.(interface{ Fd() uintptr })
	if !ok {
		return
	}
	var pid uint32
	if windows.GetNamedPipeClientProcessId(windows.Handle(fd.Fd()), &pid) != nil {
		return
	}
	reader := ipc.NewReader(c, 65536)
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	typ, payload, err := reader.Next()
	var hello ipc.Hello
	if err != nil || typ != ipc.TypeHello || hello.Decode(payload) != nil || hello.Version != ipc.Version || hello.Kind != ipc.KindTSF || hello.PID != pid {
		return
	}
	if _, err := c.Write((&ipc.Welcome{Version: ipc.Version}).Append(nil)); err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	s := &windowsSession{id: h.next.Add(1), pid: pid, out: make(chan []byte, 8), done: make(chan struct{})}
	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()
	defer func() {
		close(s.done)
		h.mu.Lock()
		h.display.hide(s.id)
		delete(h.sessions, s.id)
		h.mu.Unlock()
		h.refresh()
	}()
	go func() {
		for {
			select {
			case b := <-s.out:
				c.SetWriteDeadline(time.Now().Add(time.Second))
				if _, err := c.Write(b); err != nil {
					c.Close()
					return
				}
			case <-s.done:
				return
			}
		}
	}()
	var result ipc.Result
	for {
		typ, payload, err = reader.Next()
		if err != nil || len(payload) > 65531 {
			return
		}
		// The foreground window can lag behind TSF's focus notification, so
		// frames are never filtered here: presentation keeps only the focused
		// session's menu and the foreground check happens when drawing.
		h.mu.Lock()
		switch typ {
		case ipc.TypeFocusIn:
			var f ipc.FocusIn
			if f.Decode(payload) != nil {
				h.mu.Unlock()
				return
			}
			h.display.focus(s.id, s.pid, f.Ctx, f.Flags&ipc.FocusSecure != 0)
		case ipc.TypeFocusOut:
			var f ipc.FocusOut
			if f.Decode(payload) != nil {
				h.mu.Unlock()
				return
			}
			h.display.unfocus(s.id, f.Ctx)
		case ipc.TypeResult:
			if result.Decode(payload) != nil {
				h.mu.Unlock()
				return
			}
			h.display.result(s.id, &result)
		case ipc.TypeCaret:
			var caret ipc.Caret
			if caret.Decode(payload) != nil {
				h.mu.Unlock()
				return
			}
			h.display.position(s.id, caret)
		default:
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
		h.refresh()
	}
}
func (h *windowsHost) choose(index int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.sessions[h.display.client]
	if s == nil || !h.display.visibleIn(foregroundPID()) {
		return
	}
	frame := (&ipc.Select{Ctx: h.display.ctx, Seq: h.display.seq, Index: uint32(index)}).Append(nil)
	select {
	case s.out <- frame:
	default:
	}
}
func (h *windowsHost) view(c *ui.Context) {
	c.Root().Background(ui.Transparent).Padding(12)
	if len(h.state.Items) == 0 {
		return
	}
	p := &h.prefs
	style := candview.Style{Skin: skin.Get(p.Skin), Dark: p.Theme == "dark" || p.Theme == "system" && c.Theme().Dark, FontFamily: p.FontFamily, FontSize: float32(p.FontSize), Vertical: p.Layout == "vertical"}
	width, height := candview.Measure(c, &h.state, &style)
	h.mu.Lock()
	caret, valid := h.display.caret, h.display.visibleIn(foregroundPID())
	h.mu.Unlock()
	if valid {
		h.position(caret, width, height)
	}
	candview.Draw(c, &h.state, &style, h.choose)
}
func (h *windowsHost) position(c ipc.Caret, width, height float32) {
	type rect struct{ left, top, right, bottom int32 }
	r := rect{c.Left, c.Top, c.Right, c.Bottom}
	monitor, _, _ := monitorFromRect.Call(uintptr(unsafe.Pointer(&r)), 2)
	info := struct {
		size          uint32
		monitor, work rect
		flags         uint32
	}{}
	info.size = uint32(unsafe.Sizeof(info))
	if ok, _, _ := monitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return
	}
	handle := h.window.NativeHandle()
	dpi, _, _ := dpiForWindow.Call(handle)
	if dpi == 0 {
		dpi = 96
	}
	region := candidatePanelRegion(width, height, float32(h.prefs.FontSize), uint32(dpi))
	if region != h.region {
		rgn, _, _ := roundRectRegion.Call(uintptr(region.left), uintptr(region.top), uintptr(region.right), uintptr(region.bottom), uintptr(region.diameter), uintptr(region.diameter))
		if rgn != 0 {
			if ok, _, _ := setWindowRegion.Call(handle, rgn, 1); ok != 0 {
				// Windows owns the region after SetWindowRgn succeeds.
				h.region = region
			} else {
				deleteGDIObject.Call(rgn)
			}
		}
	}
	scale := float64(dpi) / 96
	w, hgt := int32((float64(width)+24)*scale), int32((float64(height)+24)*scale)
	x, y := r.left-int32(12*scale), r.bottom
	if y+hgt > info.work.bottom {
		y = r.top - hgt
	}
	x = max(info.work.left, min(x, info.work.right-w))
	y = max(info.work.top, min(y, info.work.bottom-hgt))
	moveWindow.Call(handle, ^uintptr(0), uintptr(x), uintptr(y), 0, 0, 0x0001|0x0010) // TOPMOST, NOSIZE, NOACTIVATE
}

func registerWindows(remove bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	h, err := windows.LoadLibraryEx(filepath.Join(filepath.Dir(exe), "shixin_tsf.dll"), 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return err
	}
	defer windows.FreeLibrary(h)
	name := "DllRegisterServer"
	if remove {
		name = "DllUnregisterServer"
	}
	proc, err := windows.GetProcAddress(h, name)
	if err != nil {
		return err
	}
	result, _, _ := syscall.SyscallN(proc)
	if int32(result) < 0 {
		return fmt.Errorf("%s failed: HRESULT 0x%08x", name, uint32(result))
	}
	return nil
}
