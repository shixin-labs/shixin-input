package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"shixin/internal/winsetup"
)

// Windows file versions use 100.major.minor.patch, independently of upstream
// DLL versions. Every owned binary advances with a Shixin release, including
// rebuilt librime-shixin code with an unchanged upstream Rime version.
func versionResource(version, name string) ([]byte, error) {
	if !winsetup.ValidVersion(version) {
		return nil, fmt.Errorf("invalid release version %q", version)
	}
	var major, minor, patch uint32
	fmt.Sscanf(version, "%d.%d.%d", &major, &minor, &patch)
	fixed := make([]byte, 52)
	kind := uint32(1)
	if strings.EqualFold(filepath.Ext(name), ".dll") {
		kind = 2
	}
	fields := []uint32{0xfeef04bd, 0x10000, 100<<16 | major, minor<<16 | patch, major<<16 | minor, patch << 16, 0x3f, 0, 0x40004, kind, 0, 0, 0}
	for i, v := range fields {
		binary.LittleEndian.PutUint32(fixed[i*4:], v)
	}
	stringNode := func(key, value string) []byte {
		v := wide(value)
		return resourceNode(key, v, uint16(len(v)/2), 1, nil)
	}
	table := resourceNode("040904b0", nil, 0, 1, [][]byte{
		stringNode("CompanyName", "Shixin Labs"), stringNode("ProductName", "拾新输入法 Shixin Input"),
		stringNode("FileDescription", "拾新输入法 Shixin Input ("+name+")"), stringNode("OriginalFilename", name),
		stringNode("FileVersion", fmt.Sprintf("100.%d.%d.%d", major, minor, patch)), stringNode("ProductVersion", version),
	})
	stringsNode := resourceNode("StringFileInfo", nil, 0, 1, [][]byte{table})
	translation := []byte{0x09, 0x04, 0xb0, 0x04}
	vars := resourceNode("VarFileInfo", nil, 0, 1, [][]byte{resourceNode("Translation", translation, 4, 0, nil)})
	return resourceNode("VS_VERSION_INFO", fixed, 52, 0, [][]byte{stringsNode, vars}), nil
}

func wide(s string) []byte {
	chars := utf16.Encode([]rune(s))
	chars = append(chars, 0)
	b := make([]byte, len(chars)*2)
	for i, v := range chars {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	return b
}
func resourceNode(key string, value []byte, valueLength, kind uint16, children [][]byte) []byte {
	b := make([]byte, 6)
	b = append(b, wide(key)...)
	align := func() {
		for len(b)%4 != 0 {
			b = append(b, 0)
		}
	}
	align()
	b = append(b, value...)
	for _, child := range children {
		align()
		b = append(b, child...)
	}
	binary.LittleEndian.PutUint16(b, uint16(len(b)))
	binary.LittleEndian.PutUint16(b[2:], valueLength)
	binary.LittleEndian.PutUint16(b[4:], kind)
	return b
}

// writeVersionRES supplies an architecture-neutral VERSIONINFO resource to
// the ARM64X linker. Updating an already-linked hybrid PE can damage its x64
// view, so its version must be present when both views are linked.
func writeVersionRES(path, version string) error {
	data, err := versionResource(version, "shixin_tsf.dll")
	if err != nil {
		return err
	}
	header := make([]byte, 32)
	binary.LittleEndian.PutUint32(header[4:], 32)
	binary.LittleEndian.PutUint16(header[8:], 0xffff)
	binary.LittleEndian.PutUint16(header[12:], 0xffff)
	out := append([]byte(nil), header...) // mandatory null resource
	binary.LittleEndian.PutUint32(header, uint32(len(data)))
	binary.LittleEndian.PutUint16(header[10:], 16)
	binary.LittleEndian.PutUint16(header[14:], 1)
	binary.LittleEndian.PutUint16(header[20:], 0x30)
	binary.LittleEndian.PutUint16(header[22:], 0x0409)
	out = append(out, header...)
	out = append(out, data...)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	return os.WriteFile(path, out, 0600)
}
