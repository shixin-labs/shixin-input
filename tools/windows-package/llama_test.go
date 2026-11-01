package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func importPE(t *testing.T, name string) []byte {
	t.Helper()
	b := make([]byte, 0x400)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[0x3c:], 0x80)
	w := bytes.NewBuffer(b[:0x80:0x80])
	w.WriteString("PE\x00\x00")
	h := pe.OptionalHeader64{Magic: 0x20b, ImageBase: 0x180000000, NumberOfRvaAndSizes: 16}
	h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_IMPORT] = pe.DataDirectory{VirtualAddress: 0x1000, Size: 40}
	for _, v := range []any{
		pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_ARM64, NumberOfSections: 1, SizeOfOptionalHeader: uint16(binary.Size(h)), Characteristics: pe.IMAGE_FILE_DLL},
		h,
		pe.SectionHeader32{Name: [8]byte{'.', 'i', 'd', 'a', 't', 'a'}, VirtualAddress: 0x1000, VirtualSize: 0x200, SizeOfRawData: 0x200, PointerToRawData: 0x200},
	} {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	copy(b, w.Bytes())
	// Valid import with OriginalFirstThunk=0 and an ordinal-only IAT.
	binary.LittleEndian.PutUint32(b[0x20c:], 0x1080)
	binary.LittleEndian.PutUint32(b[0x210:], 0x10b0)
	copy(b[0x280:], name)
	binary.LittleEndian.PutUint64(b[0x2b0:], 1<<63|1)
	return b
}

func TestPEImports(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func([]byte)
		bad    bool
	}{
		{"ordinal-only", func([]byte) {}, false},
		{"invalid-name-rva", func(b []byte) { binary.LittleEndian.PutUint32(b[0x20c:], 0xfffffff0) }, true},
		{"missing-terminator", func(b []byte) { binary.LittleEndian.PutUint32(b[0x220:], 1) }, true},
		{"empty-name", func(b []byte) { b[0x280] = 0 }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := importPE(t, "VCRUNTIME140.dll")
			c.mutate(b)
			f, err := pe.NewFile(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got, err := peImports(f)
			if c.bad {
				if err == nil {
					t.Fatal("accepted malformed imports")
				}
				return
			}
			if err != nil || len(got) != 1 || got[0] != "VCRUNTIME140.dll" {
				t.Fatalf("imports %v: %v", got, err)
			}
		})
	}
}

func TestCheckLlamaRuntimeClosure(t *testing.T) {
	dir := t.TempDir()
	for _, name := range llamaDLLs {
		if err := os.WriteFile(filepath.Join(dir, name), importPE(t, "KERNEL32.dll"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkLlama(dir, "arm64"); err != nil {
		t.Fatal(err)
	}
	if err := checkLlama(dir, "amd64"); err == nil {
		t.Fatal("accepted wrong architecture")
	}
	if err := os.WriteFile(filepath.Join(dir, "ggml-cpu.dll"), importPE(t, "VCRUNTIME140.dll"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkLlama(dir, "arm64"); err == nil || !strings.Contains(err.Error(), "VCRUNTIME140.dll") {
		t.Fatalf("missing runtime accepted: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vcruntime140.dll"), importPE(t, "KERNEL32.dll"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkLlama(dir, "arm64"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vcruntime140.dll"), importPE(t, "vcruntime140_threads.dll"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := checkLlama(dir, "arm64"); err == nil || !strings.Contains(err.Error(), "vcruntime140_threads.dll") {
		t.Fatalf("transitive dependency accepted: %v", err)
	}
}

func TestLlamaImports(t *testing.T) {
	for _, c := range []struct {
		binary, dependency string
		want               bool
	}{
		{"llama.dll", "ggml-base.dll", true},
		{"ggml-vulkan.dll", "VULKAN-1.dll", true},
		{"llama.dll", "vulkan-1.dll", false},
		{"ggml-cpu.dll", "KERNEL32.dll", true},
		{"ggml-cpu.dll", "api-ms-win-crt-runtime-l1-1-0.dll", true},
		{"llama.dll", "VCRUNTIME140.dll", true},
		{"llama.dll", "MSVCP140.dll", true},
		{"llama.dll", "libomp.dll", false},
		{"llama.dll", "ggml-cuda.dll", false},
		{"llama.dll", "../ggml-base.dll", false},
		{"llama.dll", `api-ms-win-..\evil.dll`, false},
	} {
		if got := llamaImport(c.binary, c.dependency); got != c.want {
			t.Errorf("%s imports %s: %v, want %v", c.binary, c.dependency, got, c.want)
		}
	}
}
