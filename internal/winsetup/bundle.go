// Package winsetup describes the signed Windows bootstrapper's MSI payload.
// It never changes settings or opens a user dictionary.
package winsetup

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
)

const footerSize = 112

var magic = [16]byte{'S', 'H', 'I', 'X', 'I', 'N', '-', 'M', 'S', 'I', '-', 'v', '1', 0, 0, 0}
var releaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]{0,4})$`)

// Bundle locates the MSI inside the executable's authenticated overlay.
type Bundle struct {
	Version      string
	Arch         string
	Offset, Size int64
	SHA256       [32]byte
}

// ValidVersion accepts stable MSI versions (major/minor <=255, build <=65535).
func ValidVersion(v string) bool {
	if !releaseVersion.MatchString(v) {
		return false
	}
	var a, b, c int
	_, err := fmt.Sscanf(v, "%d.%d.%d", &a, &b, &c)
	return err == nil && a <= 255 && b <= 255 && c <= 65535
}

// Read validates the payload boundaries and digest. The caller must also
// verify Authenticode; a digest alone cannot identify a publisher.
func Read(r io.ReaderAt, size int64) (Bundle, error) {
	var b Bundle
	p, err := pe.NewFile(r)
	if err != nil {
		return b, err
	}
	defer p.Close()
	var cert pe.DataDirectory
	switch h := p.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		cert = h.DataDirectory[4]
	default:
		return b, errors.New("setup: expected a 64-bit Windows executable")
	}
	end := size
	if cert.VirtualAddress != 0 || cert.Size != 0 {
		if cert.VirtualAddress == 0 || cert.Size < 8 || int64(cert.VirtualAddress)+int64(cert.Size) != size {
			return b, errors.New("setup: invalid Authenticode boundary")
		}
		end = int64(cert.VirtualAddress)
	}
	var f [footerSize + 7]byte
	n := int64(len(f))
	if end < n {
		return b, errors.New("setup: no MSI payload")
	}
	if _, err = r.ReadAt(f[:], end-n); err != nil {
		return b, err
	}
	start := -1
	for pad := 0; pad <= 7; pad++ {
		i := len(f) - pad - footerSize
		if bytes.Equal(f[i:i+16], magic[:]) && bytes.Equal(f[len(f)-pad:], make([]byte, pad)) {
			start = i
			end -= int64(pad)
			break
		}
	}
	if start < 0 {
		return b, errors.New("setup: no MSI payload")
	}
	v := f[start : start+footerSize]
	length := binary.LittleEndian.Uint64(v[16:24])
	if length < 512 || length > 2<<30 || length > uint64(end-footerSize) {
		return b, errors.New("setup: invalid MSI length")
	}
	b.Version = string(bytes.TrimRight(v[24:56], "\x00"))
	b.Arch = string(bytes.TrimRight(v[56:64], "\x00"))
	if !ValidVersion(b.Version) || (b.Arch != "amd64" && b.Arch != "arm64") {
		return b, errors.New("setup: invalid release metadata")
	}
	b.Size = int64(length)
	b.Offset = end - footerSize - b.Size
	copy(b.SHA256[:], v[64:96])
	if b.Offset < int64(p.OptionalHeader.(*pe.OptionalHeader64).SizeOfHeaders) {
		return b, errors.New("setup: overlapping MSI payload")
	}
	for _, s := range p.Sections {
		if b.Offset < int64(s.Offset)+int64(s.Size) {
			return b, errors.New("setup: payload overlaps executable sections")
		}
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.NewSectionReader(r, b.Offset, b.Size)); err != nil {
		return b, err
	}
	if !bytes.Equal(h.Sum(nil), b.SHA256[:]) {
		return b, errors.New("setup: MSI digest mismatch")
	}
	return b, nil
}

// Append attaches a payload before signing the final bootstrapper. Both
// input files must be unsigned/independently signed respectively.
func Append(exe, msi, version, arch string) error {
	if !ValidVersion(version) || (arch != "amd64" && arch != "arm64") {
		return errors.New("setup: invalid version or architecture")
	}
	p, err := pe.Open(exe)
	if err != nil {
		return err
	}
	h, ok := p.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || h.DataDirectory[4].Size != 0 {
		p.Close()
		return errors.New("setup: append the MSI before signing the executable")
	}
	p.Close()
	in, err := os.Open(msi)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if st.Size() < 512 || st.Size() > 2<<30 {
		return errors.New("setup: invalid MSI size")
	}
	out, err := os.OpenFile(exe, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer out.Close()
	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(out, hash), in); err != nil {
		return err
	}
	var footer [footerSize]byte
	copy(footer[:], magic[:])
	binary.LittleEndian.PutUint64(footer[16:24], uint64(st.Size()))
	copy(footer[24:56], version)
	copy(footer[56:64], arch)
	copy(footer[64:96], hash.Sum(nil))
	_, err = out.Write(footer[:])
	return err
}
