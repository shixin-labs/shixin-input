package ipc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// Run the real pipe and discovery checks inside a new, capability-free
// AppContainer, without registering an IME or touching a live dictionary.
func TestInputPipeAppContainer(t *testing.T) {
	dir := t.TempDir()
	sid, _, err := identity()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString(runtimeSecurity(sid))
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(dir, "sandbox.test.exe")
	if err := os.WriteFile(child, b, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "g-sandbox")
	public := EnginePath(dir)
	if err := os.WriteFile(public, []byte(path), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := ListenInput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	privatePath := filepath.Join(dir, "control")
	private, err := Listen(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer private.Close()
	name, err := pipeName(path)
	if err != nil {
		t.Fatal(err)
	}
	privateName, err := pipeName(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIXIN_TEST_SANDBOX_INPUT", name)
	t.Setenv("SHIXIN_TEST_SANDBOX_PRIVATE", privateName)
	t.Setenv("SHIXIN_TEST_SANDBOX_DISCOVERY", public)
	t.Setenv("SHIXIN_TEST_SANDBOX_TARGET", path)
	accepted := make(chan error, 1)
	go func() {
		c, err := Accept(input)
		if err != nil {
			accepted <- err
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(10 * time.Second))
		var ping [512]byte
		n, err := c.Read(ping[:])
		if err == nil && string(ping[:n]) != "ping" {
			err = fmt.Errorf("sandbox request: %s", ping[:n])
		}
		if err == nil {
			_, err = c.Write([]byte("pong"))
		}
		accepted <- err
	}()
	profile, _ := windows.UTF16PtrFromString(fmt.Sprintf("Shixin.IPCTest.%d.%d", os.Getpid(), time.Now().UnixNano()))
	userenv := windows.NewLazySystemDLL("userenv.dll")
	var appSID *windows.SID
	hr, _, _ := userenv.NewProc("CreateAppContainerProfile").Call(uintptr(unsafe.Pointer(profile)), uintptr(unsafe.Pointer(profile)), uintptr(unsafe.Pointer(profile)), 0, 0, uintptr(unsafe.Pointer(&appSID)))
	if int32(hr) < 0 {
		t.Fatalf("CreateAppContainerProfile: 0x%x", hr)
	}
	defer windows.FreeSid(appSID)
	defer userenv.NewProc("DeleteAppContainerProfile").Call(uintptr(unsafe.Pointer(profile)))
	caps := struct {
		sid             *windows.SID
		capabilities    uintptr
		count, reserved uint32
	}{sid: appSID}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		t.Fatal(err)
	}
	defer attrs.Delete()
	if err := attrs.Update(0x20009, unsafe.Pointer(&caps), unsafe.Sizeof(caps)); err != nil {
		t.Fatal(err)
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	cmd, _ := windows.UTF16PtrFromString(windows.EscapeArg(child) + " -test.run=^TestInputPipeSandboxChild$")
	var process windows.ProcessInformation
	if err := windows.CreateProcess(nil, cmd, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT, nil, nil, &si.StartupInfo, &process); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(caps)
	defer windows.CloseHandle(process.Process)
	windows.CloseHandle(process.Thread)
	result, err := windows.WaitForSingleObject(process.Process, 15000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		windows.TerminateProcess(process.Process, 99)
		t.Fatalf("sandbox wait: %d %v", result, err)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(process.Process, &code); err != nil || code != 0 {
		if code == 13 {
			t.Log(<-accepted)
		}
		t.Fatalf("sandbox exit: %d %v (10 discovery, 11 private pipe, 12 connect, 13 peer, 14 write, 15 read)", code, err)
	}
	select {
	case err := <-accepted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sandbox accept timeout")
	}
}

func TestInputPipeSandboxChild(t *testing.T) {
	name := os.Getenv("SHIXIN_TEST_SANDBOX_INPUT")
	if name == "" {
		t.Skip("AppContainer child only")
	}
	b, err := ReadEndpoint(os.Getenv("SHIXIN_TEST_SANDBOX_DISCOVERY"))
	if err != nil || string(b) != os.Getenv("SHIXIN_TEST_SANDBOX_TARGET") {
		os.Exit(10)
	}
	if err := os.WriteFile(os.Getenv("SHIXIN_TEST_SANDBOX_DISCOVERY"), []byte("tampered"), 0o600); err == nil {
		os.Exit(16)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const access = windows.FILE_GENERIC_READ | windows.FILE_WRITE_DATA
	if c, err := winio.DialPipeAccess(ctx, os.Getenv("SHIXIN_TEST_SANDBOX_PRIVATE"), access); err == nil {
		c.Close()
		os.Exit(11)
	}
	c, err := winio.DialPipeAccess(ctx, name, access)
	if err != nil {
		os.Exit(12)
	}
	defer c.Close()
	// Identity checks must succeed, then the image check must reject the
	// parent's intentionally different path (the child is a scratch copy).
	if err := checkPeer(c, true); err == nil || err.Error() != "ipc: unexpected server executable" {
		fmt.Fprintf(c, "peer check: %v", err)
		os.Exit(13)
	}
	fd := windows.Handle(c.(interface{ Fd() uintptr }).Fd())
	var pid uint32
	if windows.GetNamedPipeServerProcessId(fd, &pid) != nil {
		os.Exit(17)
	}
	for _, rights := range []uint32{windows.PROCESS_TERMINATE, windows.PROCESS_VM_READ, windows.PROCESS_DUP_HANDLE, windows.WRITE_DAC} {
		if p, err := windows.OpenProcess(rights, false, pid); err == nil {
			windows.CloseHandle(p)
			os.Exit(18)
		}
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		os.Exit(19)
	}
	defer windows.CloseHandle(p)
	var token windows.Token
	if err := windows.OpenProcessToken(p, windows.TOKEN_DUPLICATE, &token); err == nil {
		token.Close()
		os.Exit(20)
	}
	if _, err := c.Write([]byte("ping")); err != nil {
		os.Exit(14)
	}
	var pong [4]byte
	if _, err := io.ReadFull(c, pong[:]); err != nil || string(pong[:]) != "pong" {
		os.Exit(15)
	}
}
