package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Verifying a downloaded update (DESIGN §5.5, "In-app update"). The disk
// image is untrusted data until every check here passes; any doubt fails
// closed. The decisions are plain Go over an injected command runner so
// they test headless; update_darwin.go runs the real tools.

// cmdRunner runs a command and returns its output. codesign prints its
// details on stderr, so both streams are returned.
type cmdRunner func(name string, args ...string) (stdout, stderr string, err error)

// errAdHoc means the installed bundle has no developer team, so a
// downloaded build can't be tied to the same publisher.
var errAdHoc = errors.New("this copy of Shixin is not signed with a developer team (an ad hoc or development build), so it can't update itself; install the new version from the disk image")

// teamID reads the TeamIdentifier line of `codesign -dv --verbose=2`
// output. "not set" (ad hoc signatures) and a missing line give "".
func teamID(codesignOutput string) string {
	for _, line := range strings.Split(codesignOutput, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "TeamIdentifier="); ok {
			v = strings.TrimSpace(v)
			if v == "not set" {
				return ""
			}
			return v
		}
	}
	return ""
}

// bundleTeam is the developer team app is signed by.
func bundleTeam(run cmdRunner, app string) (string, error) {
	_, stderr, err := run("/usr/bin/codesign", "-dv", "--verbose=2", app)
	if err != nil {
		return "", fmt.Errorf("codesign -dv %s: %w", filepath.Base(app), err)
	}
	return teamID(stderr), nil
}

// plistValue reads one key of app's Info.plist.
func plistValue(run cmdRunner, app, key string) (string, error) {
	out, _, err := run("/usr/bin/plutil", "-extract", key, "raw", "-o", "-", filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", key, err)
	}
	return strings.TrimSpace(out), nil
}

// verifyUpdate checks the disk image dmg and the Shixin.app on its mounted
// volume before anything is installed, and returns that app's path. The
// image must be notarized and validly signed; the app must be validly
// signed by the same developer team as the installed one, be Shixin, have
// the version the release announced, and be a newer build than installed.
func verifyUpdate(run cmdRunner, dmg, volume, installed, version string) (string, error) {
	installedTeam, err := bundleTeam(run, installed)
	if err != nil {
		return "", err
	}
	if installedTeam == "" {
		return "", errAdHoc
	}
	if _, stderr, err := run("/usr/sbin/spctl", "--assess", "--type", "open", "--context", "context:primary-signature", dmg); err != nil {
		return "", fmt.Errorf("the disk image is not notarized: %w: %s", err, strings.TrimSpace(stderr))
	}
	if _, stderr, err := run("/usr/bin/codesign", "--verify", "--strict", dmg); err != nil {
		return "", fmt.Errorf("the disk image's signature is invalid: %w: %s", err, strings.TrimSpace(stderr))
	}
	app := filepath.Join(volume, "Shixin.app")
	if fi, err := os.Stat(app); err != nil || !fi.IsDir() {
		return "", errors.New("the disk image has no Shixin.app")
	}
	if _, stderr, err := run("/usr/bin/codesign", "--verify", "--deep", "--strict", app); err != nil {
		return "", fmt.Errorf("the app's signature is invalid: %w: %s", err, strings.TrimSpace(stderr))
	}
	team, err := bundleTeam(run, app)
	if err != nil {
		return "", err
	}
	if team != installedTeam {
		return "", fmt.Errorf("the app is signed by team %q, not the installed %q", team, installedTeam)
	}
	id, err := plistValue(run, app, "CFBundleIdentifier")
	if err != nil {
		return "", err
	}
	if id != bundleID {
		return "", fmt.Errorf("the app is %q, not %s", id, bundleID)
	}
	got, err := plistValue(run, app, "CFBundleShortVersionString")
	if err != nil {
		return "", err
	}
	if version == "" || got != version {
		return "", fmt.Errorf("the app is version %q, not the announced %q", got, version)
	}
	newBuild, err := plistValue(run, app, "CFBundleVersion")
	if err != nil {
		return "", err
	}
	have, err := plistValue(run, installed, "CFBundleVersion")
	if err != nil {
		return "", err
	}
	if !newerBuild(newBuild, have) {
		return "", fmt.Errorf("build %s is not newer than the installed build %s", newBuild, have)
	}
	return app, nil
}
