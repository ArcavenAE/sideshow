package pack

import (
	"os"
	"path/filepath"
	"testing"
)

// A dot-prefixed directory is a staging leftover, not an installed
// version, so InstalledVersions never lists it.
func TestInstalledVersions_SkipsDotEntries(t *testing.T) {
	freezeSafeHome(t)
	writePackYAML(t, filepath.Join(PacksDir(), "testpack", "1.0.0"), "1.0.0")
	if err := os.MkdirAll(filepath.Join(PacksDir(), "testpack", ".2.0.0.partial"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("1.0.0", filepath.Join(PacksDir(), "testpack", "current")); err != nil {
		t.Fatal(err)
	}

	versions, active, err := InstalledVersions("testpack")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0] != "1.0.0" {
		t.Errorf("versions = %v, want [1.0.0] (the .2.0.0.partial dir is not a version)", versions)
	}
	if active != "1.0.0" {
		t.Errorf("active = %q, want 1.0.0", active)
	}
}

// Activate refuses a version name that starts with a dot, since such a
// name is a staging entry that the version listings skip.
func TestActivate_RefusesADotVersion(t *testing.T) {
	freezeSafeHome(t)
	writePackYAML(t, filepath.Join(PacksDir(), "testpack", "1.0.0"), "1.0.0")
	writePackYAML(t, filepath.Join(PacksDir(), "testpack", ".2.0.0.partial"), "2.0.0")
	if err := Activate("testpack", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	if err := Activate("testpack", ".2.0.0.partial"); err == nil {
		t.Fatal("Activate accepted a dot-prefixed version, want an error")
	}
	if got, err := os.Readlink(filepath.Join(PacksDir(), "testpack", "current")); err != nil || got != "1.0.0" {
		t.Errorf("current = %q (err %v), want it left at 1.0.0", got, err)
	}
}
