package ipc

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// maxEndpointRecord bounds a discovery record; real ones hold one path.
const maxEndpointRecord = 32 << 10

// ReadEndpoint reads a discovery record. It shares the file for delete so
// the coordinator can replace the record (MoveFileEx) while it is being
// read; a reader without FILE_SHARE_DELETE makes the replacement fail.
func ReadEndpoint(path string) ([]byte, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxEndpointRecord+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxEndpointRecord {
		return nil, errors.New("ipc: discovery record too large")
	}
	return b, nil
}
