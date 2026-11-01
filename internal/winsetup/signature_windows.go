package winsetup

import (
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

// LockedFile prevents replacement, deletion and writes until Close. Keep it
// open across signature verification and launching the same path.
func LockedFile(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

// Publisher verifies Windows' Authenticode trust policy and returns the
// verified leaf's public-key identity. No network request is made. Timestamp
// validation and cached revocation state remain Windows' responsibility.
func Publisher(f *os.File) ([32]byte, error) {
	var key [32]byte
	path, err := windows.UTF16PtrFromString(f.Name())
	if err != nil {
		return key, err
	}
	info := windows.WinTrustFileInfo{FilePath: path, File: windows.Handle(f.Fd())}
	info.Size = uint32(unsafe.Sizeof(info))
	data := windows.WinTrustData{UIChoice: windows.WTD_UI_NONE, UnionChoice: windows.WTD_CHOICE_FILE,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&info), StateAction: windows.WTD_STATEACTION_VERIFY,
		ProvFlags: windows.WTD_CACHE_ONLY_URL_RETRIEVAL | windows.WTD_REVOCATION_CHECK_CHAIN_EXCLUDE_ROOT | windows.WTD_DISABLE_MD2_MD4}
	data.Size = uint32(unsafe.Sizeof(data))
	err = windows.WinVerifyTrustEx(0, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	defer func() {
		data.StateAction = windows.WTD_STATEACTION_CLOSE
		windows.WinVerifyTrustEx(0, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
		runtime.KeepAlive(f)
	}()
	if err != nil {
		return key, fmt.Errorf("setup: Authenticode verification failed: %w", err)
	}
	trust := windows.NewLazySystemDLL("wintrust.dll")
	provider, _, _ := trust.NewProc("WTHelperProvDataFromStateData").Call(uintptr(data.StateData))
	if provider == 0 {
		return key, errors.New("setup: missing signature provider")
	}
	type providerCert struct {
		Size uint32
		Cert *windows.CertContext
	}
	type providerSigner struct {
		Size  uint32
		Time  windows.Filetime
		Count uint32
		Chain *providerCert
	}
	// A typed foreign pointer avoids retaining an unmanaged C address in
	// uintptr across Go calls, and keeps vet/checkptr valid on both ABIs.
	var signer func(uintptr, uint32, uint32, uint32) *providerSigner
	purego.RegisterFunc(&signer, trust.NewProc("WTHelperGetProvSignerFromChain").Addr())
	chain := signer(provider, 0, 0, 0)
	if chain == nil {
		return key, errors.New("setup: missing verified signer")
	}
	if chain.Count == 0 || chain.Chain == nil || chain.Chain.Cert == nil {
		return key, errors.New("setup: missing verified certificate")
	}
	cert := chain.Chain.Cert
	if cert.Length == 0 || cert.Length > 1<<20 {
		return key, errors.New("setup: invalid signing certificate")
	}
	parsed, err := x509.ParseCertificate(unsafe.Slice(cert.EncodedCert, cert.Length))
	if err != nil {
		return key, err
	}
	return sha256.Sum256(parsed.RawSubjectPublicKeyInfo), nil
}
