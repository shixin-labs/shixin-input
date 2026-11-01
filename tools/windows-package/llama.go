package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var llamaDLLs = []string{"llama.dll", "ggml.dll", "ggml-base.dll", "ggml-cpu.dll", "ggml-vulkan.dll"}

// App-local, vendor-signed MSVC release redistributables. Preserve their
// signatures and original versions; only Shixin-built binaries are stamped.
var microsoftCRTDLLs = []string{
	"concrt140.dll", "msvcp140.dll", "msvcp140_1.dll", "msvcp140_2.dll",
	"msvcp140_atomic_wait.dll", "msvcp140_codecvt_ids.dll", "vccorlib140.dll",
	"vcruntime140.dll", "vcruntime140_1.dll", "vcruntime140_threads.dll",
}

func microsoftCRT(name string) bool {
	for _, dll := range microsoftCRTDLLs {
		if strings.EqualFold(name, dll) {
			return true
		}
	}
	return false
}

func checkLlama(dir, arch string) error {
	var machine uint16
	switch arch {
	case "amd64":
		machine = pe.IMAGE_FILE_MACHINE_AMD64
	case "arm64":
		machine = pe.IMAGE_FILE_MACHINE_ARM64
	default:
		return fmt.Errorf("llama: unsupported architecture %q", arch)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		name := strings.ToLower(f.Name())
		for _, prefix := range []string{"vcruntime", "msvcp", "concrt", "vccorlib"} {
			if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".dll") && !microsoftCRT(name) {
				return fmt.Errorf("llama: unsupported Microsoft runtime %s", f.Name())
			}
		}
	}
	names := append([]string(nil), llamaDLLs...)
	for _, name := range microsoftCRTDLLs {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			names = append(names, name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, name := range names {
		f, err := pe.Open(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("llama: %s: %w", name, err)
		}
		imports, err := peImports(f)
		got := f.Machine
		f.Close()
		if err != nil {
			return fmt.Errorf("llama: %s imports: %w", name, err)
		}
		if got != machine {
			return fmt.Errorf("llama: %s machine %#x, expected %s", name, got, arch)
		}
		for _, dependency := range imports {
			if !llamaImport(name, dependency) {
				return fmt.Errorf("llama: %s depends on unbundled/non-system %s", name, dependency)
			}
			if microsoftCRT(dependency) {
				if _, err := os.Stat(filepath.Join(dir, strings.ToLower(dependency))); err != nil {
					return fmt.Errorf("llama: %s needs bundled %s: %w", name, dependency, err)
				}
			}
		}
	}
	return nil
}

// debug/pe.ImportedLibraries is a stub in our pinned Go version. Read the
// descriptor names ourselves, including ordinal-only imports and descriptors
// without OriginalFirstThunk. Never infer dependencies from symbol names.
func peImports(f *pe.File) ([]string, error) {
	h, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		return nil, fmt.Errorf("expected a PE32+ optional header")
	}
	if h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_DELAY_IMPORT].VirtualAddress != 0 {
		return nil, fmt.Errorf("delay imports are not supported in the runtime build")
	}
	d := h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT]
	if d.VirtualAddress == 0 && d.Size == 0 {
		return nil, nil
	}
	if d.Size < 20 || d.Size > 1<<20 {
		return nil, fmt.Errorf("invalid import directory size")
	}
	rvaData := func(rva uint32) ([]byte, error) {
		for _, s := range f.Sections {
			if rva < s.VirtualAddress {
				continue
			}
			off := uint64(rva) - uint64(s.VirtualAddress)
			if off >= uint64(s.Size) {
				continue
			}
			data, err := s.Data()
			if err != nil {
				return nil, err
			}
			if off >= uint64(len(data)) {
				break
			}
			return data[off:], nil
		}
		return nil, fmt.Errorf("import RVA %#x outside file data", rva)
	}
	table, err := rvaData(d.VirtualAddress)
	if err != nil {
		return nil, err
	}
	if uint64(len(table)) < uint64(d.Size) {
		return nil, fmt.Errorf("truncated import directory")
	}
	table = table[:d.Size]
	var imports []string
	for len(table) >= 20 {
		entry := table[:20]
		table = table[20:]
		if bytes.Equal(entry, make([]byte, 20)) {
			return imports, nil
		}
		rva := binary.LittleEndian.Uint32(entry[12:16])
		if rva == 0 {
			return nil, fmt.Errorf("import descriptor has no name")
		}
		name, err := rvaData(rva)
		if err != nil {
			return nil, err
		}
		n := bytes.IndexByte(name[:min(len(name), 4096)], 0)
		if n <= 0 {
			return nil, fmt.Errorf("unterminated or empty import name")
		}
		imports = append(imports, string(name[:n]))
	}
	return nil, fmt.Errorf("unterminated import directory")
}

func llamaImport(binary, dependency string) bool {
	name := strings.ToLower(dependency)
	if strings.ContainsAny(name, `/\:`) {
		return false
	}
	if microsoftCRT(name) {
		return true
	}
	for _, bundled := range llamaDLLs {
		if name == bundled {
			if name == "ggml-vulkan.dll" || name == "ggml-cpu.dll" {
				return false
			}
			return true
		}
	}
	if name == "vulkan-1.dll" {
		return binary == "ggml-vulkan.dll"
	}
	if strings.HasPrefix(name, "api-ms-win-") || strings.HasPrefix(name, "ext-ms-win-") {
		return true
	}
	switch name {
	case "kernel32.dll", "ntdll.dll", "advapi32.dll", "user32.dll", "ole32.dll", "oleaut32.dll", "combase.dll", "rpcrt4.dll", "shell32.dll", "shlwapi.dll", "bcrypt.dll", "ucrtbase.dll", "msvcrt.dll":
		return true
	}
	return false
}
