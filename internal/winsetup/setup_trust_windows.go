package winsetup

import (
	"os"
	"path/filepath"
)

// SetupPublisher keeps the signed build's trust checks mandatory. Only a
// separately compiled preview accepts unsigned files; it supplies no identity.
func SetupPublisher(f *os.File) ([32]byte, error) {
	if UnsignedPreview {
		return [32]byte{}, RequireUnsigned(f)
	}
	return Publisher(f)
}

// CheckPreviewTarget prevents the preview installer and its MSI drain guard
// from replacing a signed installation, even when its certificate is untrusted.
func CheckPreviewTarget(dir string) error {
	if !UnsignedPreview {
		return nil
	}
	code, err := ProductCode()
	if err != nil || code == "" {
		return err
	}
	f, err := LockedFile(filepath.Join(dir, "Shixin.exe"))
	if err != nil {
		return err
	}
	defer f.Close()
	return RequireUnsigned(f)
}
