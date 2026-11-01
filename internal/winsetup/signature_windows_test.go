package winsetup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupPublisherBuildMode(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := LockedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = SetupPublisher(f)
	if (err == nil) != UnsignedPreview {
		t.Fatalf("preview=%v, unsigned setup verification: %v", UnsignedPreview, err)
	}
	// Publisher itself never trusts unsigned input, in either build mode.
	if _, err := Publisher(f); err == nil {
		t.Fatal("preview weakened Authenticode publisher verification")
	}
}

func TestLockedSignatureFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload.exe")
	if err := os.WriteFile(path, []byte("unsigned"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := LockedFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := Publisher(f); err == nil {
		t.Fatal("unsigned file trusted")
	}
	if write, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		write.Close()
		t.Fatal("verified file is writable")
	}
	if err := os.Rename(path, path+".old"); err == nil {
		t.Fatal("verified file is replaceable")
	}
	if err := os.Rename(filepath.Dir(path), filepath.Dir(path)+".old"); err == nil {
		t.Fatal("verified parent directory can be renamed")
	}
}
