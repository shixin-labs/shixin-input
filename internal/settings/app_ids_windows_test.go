package settings

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsAppIdentities(t *testing.T) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_System, 0x4000)
	if err != nil {
		t.Fatal(err)
	}
	id := windows.GUID(*windows.FOLDERID_System).String() + `\cmd.exe`
	want := strings.ToLower(filepath.Join(base, "cmd.exe"))
	aliases := AppAliases(id)
	if len(aliases) != 2 || aliases[0] != id || aliases[1] != want {
		t.Fatalf("aliases %q, want %q and %q", aliases, id, want)
	}
	if AppKey(strings.ToUpper(want)) != want || AppKey(strings.ReplaceAll(want, `\`, "/")) != want {
		t.Fatal("path case/separators changed identity")
	}
	for _, id := range []string{"MSEdge", "Microsoft.WindowsTerminal_8wekyb3d8bbwe!App", "No.Such.Shixin.Test.App"} {
		if AppKey(id) != id || AppAliases(id)[0] != id {
			t.Fatalf("explicit identity changed: %q", id)
		}
	}
	if knownFolderApp(windows.GUID(*windows.FOLDERID_System).String()+`\..\bad.exe`) != "" {
		t.Fatal("known-folder escape accepted")
	}
}

func TestWindowsShellAppAlias(t *testing.T) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFilesX86, 0x4000)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe")
	if _, err := os.Stat(target); err != nil {
		t.Skip("Edge is not installed")
	}
	aliases := AppAliases("MSEdge")
	if len(aliases) != 2 || aliases[1] != AppKey(target) {
		t.Fatalf("shell app target %q, want %q", aliases, target)
	}
}
