package settings

import (
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// AppKey normalizes executable paths, leaving explicit/package IDs intact.
// It performs no I/O and is safe on a connection's focus reader goroutine.
func AppKey(id string) string {
	if filepath.IsAbs(id) {
		return strings.ToLower(filepath.Clean(id))
	}
	return id
}

// AppAliases keeps the picker ID and adds its executable identity, for TSF
// clients without an explicit AppUserModelID. Shell/known-folder resolution
// is cold settings work: never call it on the engine or application key path.
func AppAliases(id string) []string {
	key := AppKey(id)
	result := []string{key}
	if filepath.IsAbs(id) || id == "" {
		return result
	}
	target := knownFolderApp(id)
	if target == "" && !strings.ContainsAny(id, `\/:`) {
		target = shellAppTarget(id)
	}
	target = AppKey(target)
	if target != "" && target != key {
		result = append(result, target)
	}
	return result
}

func knownFolderApp(id string) string {
	if len(id) < 40 || id[0] != '{' || id[37] != '}' || id[38] != '\\' {
		return ""
	}
	guid, err := windows.GUIDFromString(id[:38])
	if err != nil {
		return ""
	}
	// KF_FLAG_DONT_VERIFY: expansion must not create or probe directories.
	base, err := windows.KnownFolderPath((*windows.KNOWNFOLDERID)(&guid), 0x4000)
	if err != nil {
		return ""
	}
	target := filepath.Clean(filepath.Join(base, id[39:]))
	if !strings.EqualFold(filepath.Ext(target), ".exe") || !strings.HasPrefix(strings.ToLower(target), strings.ToLower(filepath.Clean(base))+`\`) {
		return ""
	}
	return target
}

var shellAppItem = windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")
var shellItem2ID = windows.GUID{Data1: 0x7e9fb0d3, Data2: 0x919f, Data3: 0x4307, Data4: [8]byte{0xab, 0x2e, 0x9b, 0x18, 0x60, 0x31, 0x0c, 0x93}}

// PKEY_Link_TargetParsingPath from the Windows SDK's propkey.h.
var shellTargetKey = struct {
	id       windows.GUID
	property uint32
}{
	windows.GUID{Data1: 0xb9b4b3fc, Data2: 0x2b51, Data3: 0x4a42, Data4: [8]byte{0xb5, 0xd8, 0x32, 0x41, 0x46, 0xaf, 0xcf, 0x25}}, 2,
}

type shellItem2 struct{ table *[18]uintptr }

func shellAppTarget(id string) string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	if err == nil || err == syscall.Errno(1) {
		defer windows.CoUninitialize()
	} else if err != syscall.Errno(windows.RPC_E_CHANGED_MODE) {
		return ""
	}
	name, err := windows.UTF16PtrFromString(`shell:AppsFolder\` + id)
	if err != nil {
		return ""
	}
	var item *shellItem2
	hr, _, _ := shellAppItem.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&shellItem2ID)), uintptr(unsafe.Pointer(&item)))
	runtime.KeepAlive(name)
	if int32(hr) < 0 || item == nil {
		return ""
	}
	defer syscall.SyscallN(item.table[2], uintptr(unsafe.Pointer(item)))
	var target *uint16
	hr, _, _ = syscall.SyscallN(item.table[17], uintptr(unsafe.Pointer(item)), uintptr(unsafe.Pointer(&shellTargetKey)), uintptr(unsafe.Pointer(&target)))
	runtime.KeepAlive(item)
	if int32(hr) < 0 || target == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(target))
	path := windows.UTF16PtrToString(target)
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".exe") {
		return ""
	}
	// A shortcut into a generic launcher does not identify the application
	// it launches. Never apply a PowerShell/SDK rule to every cmd/Explorer app.
	switch strings.ToLower(filepath.Base(path)) {
	case "cmd.exe", "explorer.exe", "rundll32.exe", "mmc.exe", "conhost.exe":
		return ""
	}
	return path
}
