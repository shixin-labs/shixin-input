package dynlib

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Open loads a DLL and its dependencies from its own directory and System32.
// Never search the working directory or PATH for downloaded or bundled DLLs.
func Open(path string, _ bool) (uintptr, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, err
	}
	h, err := windows.LoadLibraryEx(abs, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	return uintptr(h), err
}

// Symbol resolves an exported function.
func Symbol(h uintptr, name string) (uintptr, error) {
	return windows.GetProcAddress(windows.Handle(h), name)
}
