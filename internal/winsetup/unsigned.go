package winsetup

import (
	"debug/pe"
	"errors"
	"io"
)

// RequireUnsigned accepts a PE32+ file with no certificate table. A broken or
// untrusted signature is not an unsigned preview and must never bypass trust.
func RequireUnsigned(r io.ReaderAt) error {
	p, err := pe.NewFile(r)
	if err != nil {
		return err
	}
	defer p.Close()
	h, ok := p.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		return errors.New("setup: expected a 64-bit Windows executable")
	}
	if h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_SECURITY].VirtualAddress != 0 || h.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_SECURITY].Size != 0 {
		return errors.New("setup: unsigned previews cannot use or replace signed files")
	}
	return nil
}
