package winsetup

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func testBundle(t *testing.T) []byte {
	t.Helper()
	var p bytes.Buffer
	dos := make([]byte, 128)
	copy(dos, "MZ")
	binary.LittleEndian.PutUint32(dos[0x3c:], 128)
	p.Write(dos)
	p.WriteString("PE\x00\x00")
	binary.Write(&p, binary.LittleEndian, pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64, NumberOfSections: 1, SizeOfOptionalHeader: uint16(binary.Size(pe.OptionalHeader64{}))})
	binary.Write(&p, binary.LittleEndian, pe.OptionalHeader64{Magic: 0x20b, SizeOfHeaders: 512, NumberOfRvaAndSizes: 16})
	section := pe.SectionHeader32{PointerToRawData: 512, SizeOfRawData: 512}
	copy(section.Name[:], ".text")
	binary.Write(&p, binary.LittleEndian, section)
	p.Write(make([]byte, 1024-p.Len()))
	dir := t.TempDir()
	exe := filepath.Join(dir, "setup.exe")
	msi := filepath.Join(dir, "setup.msi")
	if err := os.WriteFile(exe, p.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(msi, bytes.Repeat([]byte{42}, 1024), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Append(exe, msi, "1.2.3", "arm64"); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBundle(t *testing.T) {
	data := testBundle(t)
	b, err := Read(bytes.NewReader(data), int64(len(data)))
	if err != nil || b.Version != "1.2.3" || b.Arch != "arm64" || b.Offset != 1024 || b.Size != 1024 {
		t.Fatalf("%+v: %v", b, err)
	}
	for _, pad := range []int{0, 1, 7} {
		signed := append(bytes.Clone(data), make([]byte, pad+128)...)
		// PE32+ security directory: optional header +112 +4*8.
		at := 128 + 4 + binary.Size(pe.FileHeader{}) + 112 + 4*8
		binary.LittleEndian.PutUint32(signed[at:], uint32(len(data)+pad))
		binary.LittleEndian.PutUint32(signed[at+4:], 128)
		if _, err := Read(bytes.NewReader(signed), int64(len(signed))); err != nil {
			t.Fatalf("padding %d: %v", pad, err)
		}
		signed[len(signed)-1] = 3 // bytes inside a certificate never become payload
		if _, err := Read(bytes.NewReader(signed), int64(len(signed))); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(bytes.NewReader(signed), int64(len(signed)-1)); err == nil {
			t.Fatal("accepted truncated signature")
		}
	}
	for name, mutate := range map[string]func([]byte){
		"payload": func(p []byte) { p[1050] ^= 1 },
		"version": func(p []byte) { p[len(p)-footerSize+24] = '/' },
		"arch":    func(p []byte) { p[len(p)-footerSize+56] = 'x' },
		"length":  func(p []byte) { binary.LittleEndian.PutUint64(p[len(p)-footerSize+16:], 1<<63) },
		"magic":   func(p []byte) { p[len(p)-footerSize] ^= 1 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Clone(data)
			mutate(bad)
			if _, err := Read(bytes.NewReader(bad), int64(len(bad))); err == nil {
				t.Fatal("accepted invalid payload")
			}
		})
	}
}

func TestMSIVersion(t *testing.T) {
	for _, v := range []string{"0.0.0", "1.2.3", "255.255.65535"} {
		if !ValidVersion(v) {
			t.Error(v)
		}
	}
	for _, v := range []string{"dev", "01.0.0", "1.2", "1.2.3-dev", "256.0.0", "0.256.0", "0.0.65536", "1.2.3/evil"} {
		if ValidVersion(v) {
			t.Error(v)
		}
	}
}
