package winsetup

import (
	"crypto/rand"
	"errors"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type approval struct {
	goEvent, ready    windows.Handle
	goName, readyName string
}

func (a *approval) close() {
	if a.goEvent != 0 {
		windows.CloseHandle(a.goEvent)
	}
	if a.ready != 0 {
		windows.CloseHandle(a.ready)
	}
}

// Global events cross the interactive user's and MSI service's sessions.
// Only that user, administrators and SYSTEM can signal or wait on them.
func newApproval() (*approval, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;BA)(A;;GA;;;SY)")
	if err != nil {
		return nil, err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	base := `Global\Shixin.Setup.` + rand.Text()
	a := &approval{goName: base + ".go", readyName: base + ".ready"}
	goName, _ := windows.UTF16PtrFromString(a.goName)
	readyName, _ := windows.UTF16PtrFromString(a.readyName)
	a.goEvent, err = windows.CreateEvent(&sa, 1, 0, goName)
	if err != nil {
		a.close()
		return nil, err
	}
	a.ready, err = windows.CreateEvent(&sa, 1, 0, readyName)
	if err != nil {
		a.close()
		return nil, err
	}
	return a, nil
}

// WaitApproval is the deferred MSI unregistration action's user gate. UAC
// has already succeeded before it signals ready. The original caller disables
// its keyboard while the machine TIP exists, then releases this action.
func WaitApproval(goName, readyName string) error {
	base, ok := strings.CutSuffix(goName, ".go")
	if !ok || !strings.HasPrefix(base, `Global\Shixin.Setup.`) || readyName != base+".ready" || len(base) > 128 {
		return errors.New("setup: invalid approval event")
	}
	suffix := strings.TrimPrefix(base, `Global\Shixin.Setup.`)
	if len(suffix) < 20 || strings.IndexFunc(suffix, func(r rune) bool { return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }) >= 0 {
		return errors.New("setup: invalid approval event")
	}
	p, _ := windows.UTF16PtrFromString(goName)
	goEvent, err := windows.OpenEvent(windows.SYNCHRONIZE, false, p)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(goEvent)
	p, _ = windows.UTF16PtrFromString(readyName)
	ready, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, p)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ready)
	if err = windows.SetEvent(ready); err != nil {
		return err
	}
	result, err := windows.WaitForSingleObject(goEvent, uint32((30*time.Second)/time.Millisecond))
	if err != nil {
		return err
	}
	if result != windows.WAIT_OBJECT_0 {
		return errors.New("setup: original-user profile removal was not approved")
	}
	return nil
}
