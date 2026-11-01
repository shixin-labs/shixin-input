package ipc

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func identity() (string, uint32, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", 0, err
	}
	var session uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &session); err != nil {
		return "", 0, err
	}
	return u.User.Sid.String(), session, nil
}

// RuntimeDir returns the per-user, per-logon-session discovery directory.
// Named pipes carry the traffic; files here only publish worker endpoints.
func RuntimeDir() (string, error) {
	sid, session, err := identity()
	if err != nil {
		return "", err
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Shixin", fmt.Sprintf("run-%d", session))
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", err
	}
	// Elevated tokens may otherwise create directories owned by the local
	// Administrators group. Assign this user's SID at creation as well.
	private, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)")
	if err != nil {
		return "", err
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: private}
	if err := windows.CreateDirectory(name, sa); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("ipc: runtime directory is not a directory")
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner.String() != sid {
		return "", errors.New("ipc: runtime directory belongs to another user")
	}
	// Unix permissions have no effect on Windows. Protect discovery records
	// with an explicit inheritable DACL, including on an existing directory.
	acl, _, err := private.DACL()
	if err != nil {
		return "", err
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return "", err
	}
	return dir, nil
}

// EnginePath is the stable discovery record, not a pipe name.
func EnginePath(dir string) string { return filepath.Join(dir, "engine.sock") }

func pipeName(path string) (string, error) {
	sid, session, err := identity()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(abs))))
	return fmt.Sprintf(`\\.\pipe\shixin-%d-%s-%x`, session, sid, digest[:16]), nil
}

// Listen creates a byte-stream pipe using overlapped I/O. Another listener
// cannot steal an active name (go-winio uses FILE_FLAG_FIRST_PIPE_INSTANCE).
func Listen(path string) (net.Listener, error) {
	name, err := pipeName(path)
	if err != nil {
		return nil, err
	}
	sid, _, err := identity()
	if err != nil {
		return nil, err
	}
	return winio.ListenPipe(name, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")",
		// go-winio still exposes a byte stream, and uses a zero-length
		// message for CloseWrite. Migration needs that half-close so final
		// accepted commits drain before the old worker disconnects.
		MessageMode:     true,
		InputBufferSize: 65536, OutputBufferSize: 65536,
	})
}

// Accept rejects connections outside the current user and logon session.
func Accept(l net.Listener) (net.Conn, error) {
	for {
		c, err := l.Accept()
		if err != nil {
			return nil, err
		}
		if err := checkPeer(c, false); err == nil {
			return c, nil
		}
		c.Close()
	}
}

// Dial resolves the current generation off the key path, then connects.
func Dial(path string, timeout time.Duration) (net.Conn, error) {
	if b, err := ReadEndpoint(path); err == nil {
		target := string(b)
		// Only a sibling private endpoint may be published. Never follow a
		// chain, a remote pipe, or a record in another user's directory.
		if !filepath.IsAbs(target) || filepath.Dir(target) != filepath.Dir(path) || !strings.HasPrefix(filepath.Base(target), "g-") {
			return nil, errors.New("ipc: invalid discovery record")
		}
		path = target
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	name, err := pipeName(path)
	if err != nil {
		return nil, err
	}
	c, err := winio.DialPipe(name, &timeout)
	if err != nil {
		return nil, err
	}
	if err := checkPeer(c, true); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func checkPeer(c net.Conn, server bool) error {
	f, ok := c.(interface{ Fd() uintptr })
	if !ok {
		return errors.New("ipc: pipe handle unavailable")
	}
	var pid uint32
	var err error
	if server {
		err = windows.GetNamedPipeServerProcessId(windows.Handle(f.Fd()), &pid)
	} else {
		err = windows.GetNamedPipeClientProcessId(windows.Handle(f.Fd()), &pid)
	}
	if err != nil {
		return err
	}
	sid, session, err := identity()
	if err != nil {
		return err
	}
	var peerSession uint32
	if err := windows.ProcessIdToSessionId(pid, &peerSession); err != nil {
		return err
	}
	if session != peerSession {
		return errors.New("ipc: peer is in another session")
	}
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(p)
	var token windows.Token
	if err := windows.OpenProcessToken(p, windows.TOKEN_QUERY, &token); err != nil {
		return err
	}
	defer token.Close()
	u, err := token.GetTokenUser()
	if err != nil {
		return err
	}
	if u.User.Sid.String() != sid {
		return errors.New("ipc: peer belongs to another user")
	}
	if server {
		var image [32768]uint16
		n := uint32(len(image))
		if err := windows.QueryFullProcessImageName(p, 0, &image[0], &n); err != nil {
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		peer := windows.UTF16ToString(image[:n])
		trusted := false
		for _, expected := range []string{exe, filepath.Join(filepath.Dir(exe), "shixin-engine.exe"), filepath.Join(filepath.Dir(exe), "shixin-ai.exe")} {
			if strings.EqualFold(peer, expected) {
				trusted = true
			}
		}
		if !trusted {
			return errors.New("ipc: unexpected server executable")
		}
	}
	return nil
}
