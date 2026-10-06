package pack

import (
	"os"
	"path/filepath"
	"testing"
)

// writeVersionedPack writes a native pack source at the given version
// with one content file, so two sources can claim the same version and
// differ only in content.
func writeVersionedPack(t *testing.T, version, content string) string {
	t.Helper()
	src := t.TempDir()
	writePackYAML(t, src, version)
	if err := os.WriteFile(filepath.Join(src, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

func readStoreSkill(t *testing.T, version string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(PacksDir(), "testpack", version, "skill.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// aae-orc-mobz8: a second install at an already-installed version, from
// a different tree, must be refused and must leave the store untouched.
// Before the fix it exited 0 and replaced the frozen content in place.
func TestInstallFromLocal_RefusesOverwriteOfInstalledVersion(t *testing.T) {
	freezeSafeHome(t)

	if err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "original"), true); err != nil {
		t.Fatal(err)
	}

	err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "TAMPERED"), true)
	if err == nil {
		t.Fatal("reinstall over an installed version succeeded; want a refusal")
	}
	if got := readStoreSkill(t, "1.0.0"); got != "original" {
		t.Errorf("store content = %q after a refused install, want original", got)
	}
}

// A different version of the same pack is not an overwrite.
func TestInstallFromLocal_NewVersionIsNotAnOverwrite(t *testing.T) {
	freezeSafeHome(t)

	if err := InstallFromLocal("testpack", writeVersionedPack(t, "1.0.0", "one"), true); err != nil {
		t.Fatal(err)
	}
	if err := InstallFromLocal("testpack", writeVersionedPack(t, "2.0.0", "two"), true); err != nil {
		t.Fatalf("install of a new version: %v", err)
	}
	if got := readStoreSkill(t, "1.0.0"); got != "one" {
		t.Errorf("1.0.0 content = %q, want one", got)
	}
}
