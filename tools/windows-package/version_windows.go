package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func stamp(dir, version, skip string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.IsDir() || strings.EqualFold(file.Name(), skip) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(file.Name()))
		if ext != ".exe" && ext != ".dll" {
			continue
		}
		if err := stampFile(filepath.Join(dir, file.Name()), version); err != nil {
			return err
		}
	}
	return nil
}
func stampFile(path, version string) error {
	data, err := versionResource(version, filepath.Base(path))
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// Data-file mapping never executes DLL initialization. Remove any old
	// language variants so version queries cannot select stale upstream data.
	module, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_AS_DATAFILE|windows.LOAD_LIBRARY_AS_IMAGE_RESOURCE)
	if err != nil && !errors.Is(err, windows.ERROR_RESOURCE_DATA_NOT_FOUND) && !errors.Is(err, windows.ERROR_RESOURCE_TYPE_NOT_FOUND) {
		return err
	}
	var languages []uint16
	callback := syscall.NewCallback(func(_, _, _, language, _ uintptr) uintptr { languages = append(languages, uint16(language)); return 1 })
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	if module != 0 {
		enumerated, _, enumErr := kernel.NewProc("EnumResourceLanguagesW").Call(uintptr(module), 16, 1, callback, 0)
		windows.FreeLibrary(module)
		if enumerated == 0 && !errors.Is(enumErr, windows.ERROR_RESOURCE_DATA_NOT_FOUND) && !errors.Is(enumErr, windows.ERROR_RESOURCE_TYPE_NOT_FOUND) && !errors.Is(enumErr, windows.ERROR_RESOURCE_NAME_NOT_FOUND) && !errors.Is(enumErr, windows.ERROR_RESOURCE_LANG_NOT_FOUND) {
			return enumErr
		}
	}
	handle, _, callErr := kernel.NewProc("BeginUpdateResourceW").Call(uintptr(unsafe.Pointer(p)), 0)
	runtime.KeepAlive(p)
	if handle == 0 {
		return fmt.Errorf("version resource: %s: %w", path, callErr)
	}
	committed := false
	defer func() {
		if !committed {
			kernel.NewProc("EndUpdateResourceW").Call(handle, 1)
		}
	}()
	update := kernel.NewProc("UpdateResourceW")
	for _, language := range languages {
		ok, _, err := update.Call(handle, 16, 1, uintptr(language), 0, 0)
		if ok == 0 {
			return err
		}
	}
	ok, _, callErr := update.Call(handle, 16, 1, 0x0409, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	runtime.KeepAlive(data)
	if ok == 0 {
		return callErr
	}
	ok, _, callErr = kernel.NewProc("EndUpdateResourceW").Call(handle, 0)
	committed = true
	if ok == 0 {
		return callErr
	}
	// Resource updates may truncate Go's trailing COFF string table. Release
	// builds are stripped and have no long section names requiring that table.
	image, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(image) < 64 {
		return fmt.Errorf("invalid PE file %s", path)
	}
	peOffset := int(binary.LittleEndian.Uint32(image[0x3c:]))
	if peOffset < 0 || peOffset+20 > len(image) {
		return fmt.Errorf("invalid PE header %s", path)
	}
	clear(image[peOffset+12 : peOffset+20])
	return os.WriteFile(path, image, 0600)
}
