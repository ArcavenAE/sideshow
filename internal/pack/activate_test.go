package pack

import (
	"os"
	"path/filepath"
	"testing"
)

func writePackYAML(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "name: testpack\nversion: " + version + "\nschema_version: 0.1.0\n"
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestActivateAndInstalledVersions(t *testing.T) {
	freezeSafeHome(t)

	// Two installed versions, current pointing at 2.0.0.
	for _, v := range []string{"1.0.0", "2.0.0"} {
		writePackYAML(t, filepath.Join(PacksDir(), "testpack", v), v)
	}
	if err := os.Symlink("2.0.0", filepath.Join(PacksDir(), "testpack", "current")); err != nil {
		t.Fatal(err)
	}

	versions, active, err := InstalledVersions("testpack")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0] != "1.0.0" || versions[1] != "2.0.0" {
		t.Errorf("versions = %v, want [1.0.0 2.0.0]", versions)
	}
	if active != "2.0.0" {
		t.Errorf("active = %q, want 2.0.0", active)
	}

	// Activate the other version.
	if err := Activate("testpack", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	_, active, err = InstalledVersions("testpack")
	if err != nil {
		t.Fatal(err)
	}
	if active != "1.0.0" {
		t.Errorf("active after Activate = %q, want 1.0.0", active)
	}

	// Registry follows the flip.
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range reg.Packs {
		if p.Name == "testpack" {
			found = true
			if p.Version != "1.0.0" {
				t.Errorf("registry version = %q, want 1.0.0", p.Version)
			}
		}
	}
	if !found {
		t.Error("registry has no testpack entry after Activate")
	}

	// Activating a version that isn't installed fails.
	if err := Activate("testpack", "9.9.9"); err == nil {
		t.Error("Activate of uninstalled version should error")
	}
}

func TestInstallFromLocal_NoActivateKeepsCurrent(t *testing.T) {
	freezeSafeHome(t)

	src1 := t.TempDir()
	writePackYAML(t, src1, "1.0.0")
	// First install always activates, even with activate=false.
	if err := InstallFromLocal("testpack", src1, false); err != nil {
		t.Fatal(err)
	}
	_, active, err := InstalledVersions("testpack")
	if err != nil {
		t.Fatal(err)
	}
	if active != "1.0.0" {
		t.Fatalf("first install must activate; active = %q", active)
	}

	// Second install with activate=false: version lands, current stays.
	src2 := t.TempDir()
	writePackYAML(t, src2, "2.0.0")
	if err := InstallFromLocal("testpack", src2, false); err != nil {
		t.Fatal(err)
	}
	versions, active, err := InstalledVersions("testpack")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Errorf("versions = %v, want both installed", versions)
	}
	if active != "1.0.0" {
		t.Errorf("active = %q after --no-activate install, want 1.0.0", active)
	}

	// Third install WITH activate: current flips.
	src3 := t.TempDir()
	writePackYAML(t, src3, "3.0.0")
	if err := InstallFromLocal("testpack", src3, true); err != nil {
		t.Fatal(err)
	}
	_, active, err = InstalledVersions("testpack")
	if err != nil {
		t.Fatal(err)
	}
	if active != "3.0.0" {
		t.Errorf("active = %q after activating install, want 3.0.0", active)
	}
}
