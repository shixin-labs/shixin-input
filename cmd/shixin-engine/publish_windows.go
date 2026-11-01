package main

import (
	"encoding/binary"
	"errors"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Replacing the record can briefly collide with a reader that holds it open
// without FILE_SHARE_DELETE (an older client, antivirus, an indexer).
// Retry for a short, bounded time; publish runs on the coordinator, never on
// a key path.
const (
	publishRetryFor   = time.Second
	publishRetryEvery = 10 * time.Millisecond
)

func retryableRename(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

// Windows symlinks require Developer Mode or elevation. Publish an atomic
// discovery record instead; ipc.Dial maps the target to a named pipe.
func publish(path, target string) error {
	tmp := path + ".next"
	if err := os.WriteFile(tmp, []byte(target), 0o600); err != nil {
		return err
	}
	deadline := time.Now().Add(publishRetryFor)
	for {
		err := replaceFile(tmp, path)
		if err == nil {
			return nil
		}
		if !retryableRename(err) || !time.Now().Before(deadline) {
			os.Remove(tmp)
			return err
		}
		time.Sleep(publishRetryEvery)
	}
}

// replaceFile renames from over to with POSIX semantics (Windows 10 1709+):
// the record is replaced even while readers hold it open with
// FILE_SHARE_DELETE, and they keep reading the old one. MoveFileEx, which
// os.Rename uses, refuses that with ERROR_ACCESS_DENIED. On a file system
// without POSIX renames it falls back to os.Rename.
func replaceFile(from, to string) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	name, err := windows.UTF16FromString(to)
	if err != nil {
		return err
	}
	name = name[:len(name)-1] // FileNameLength excludes the terminator
	h, err := windows.CreateFile(src, windows.DELETE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	// FILE_RENAME_INFO: Flags (DWORD, padded to the handle), RootDirectory
	// (HANDLE, nil), FileNameLength (DWORD, bytes), FileName.
	const (
		replaceIfExists = 0x1 // FILE_RENAME_FLAG_REPLACE_IF_EXISTS
		posixSemantics  = 0x2 // FILE_RENAME_FLAG_POSIX_SEMANTICS
		nameOffset      = 8 + unsafe.Sizeof(uintptr(0)) + 4
	)
	buf := make([]byte, nameOffset+uintptr(len(name)+1)*2)
	binary.LittleEndian.PutUint32(buf[0:], replaceIfExists|posixSemantics)
	binary.LittleEndian.PutUint32(buf[8+unsafe.Sizeof(uintptr(0)):], uint32(len(name)*2))
	for i, c := range name {
		binary.LittleEndian.PutUint16(buf[nameOffset+uintptr(i)*2:], c)
	}
	err = windows.SetFileInformationByHandle(h, windows.FileRenameInfoEx, &buf[0], uint32(len(buf)))
	windows.CloseHandle(h) // before any fallback rename of the same file
	if err == nil {
		return nil
	}
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) {
		return os.Rename(from, to)
	}
	return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
}
