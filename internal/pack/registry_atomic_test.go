package pack

import (
	"os"
	"testing"
)

// The registry is replaced, not rewritten in place, so a save that cannot
// complete leaves the previous registry intact instead of a torn file that
// stops list, status and install (sideshow#180).
func TestRegistrySave_AFailedSaveLeavesThePreviousRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	old := &Registry{Packs: []InstalledPack{{Name: "alpha"}}}
	if err := old.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })

	next := &Registry{Packs: []InstalledPack{{Name: "beta"}}}
	if err := next.Save(); err == nil {
		t.Fatal("a save into a directory that cannot take a new file succeeded; the registry was written in place")
	}
	after, err := os.ReadFile(RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the previous registry changed:\nbefore %q\nafter  %q", before, after)
	}
	if r, lerr := LoadRegistry(); lerr != nil || len(r.Packs) != 1 || r.Packs[0].Name != "alpha" {
		t.Errorf("registry no longer loads as the old one: %+v, %v", r, lerr)
	}
}

// A successful save leaves only the registry, with the mode it had.
func TestRegistrySave_LeavesNoTempFileAndKeepsTheMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	if err := os.WriteFile(RegistryPath(), []byte("packs: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &Registry{Packs: []InstalledPack{{Name: "alpha"}}}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory after save: %v, %v", entries, err)
	}
	if info, err := os.Stat(RegistryPath()); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode after save: %v, %v", info, err)
	}
}
