//go:build unix

package bindings

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// scene returns the custom-sources path and a save that changes it.
func scene(t *testing.T) (string, func() error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	return customSourcesPath(), func() error {
		return saveCustomSources([]CustomSource{{Project: "/p", Pack: "alpha"}})
	}
}

// A write that cannot complete leaves the previous bytes, not a torn
// file (sideshow#185 tier 2). A directory that cannot take a new file
// makes it observable: an in-place write still succeeds into the file.
func TestSaveCustomSources_AFailedWriteLeavesThePreviousBytes(t *testing.T) {
	path, save := scene(t)
	if err := os.WriteFile(path, []byte("schema_version: 0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := save(); err == nil {
		t.Fatal("a write into a directory that cannot take a new file succeeded; the file was written in place")
	}
	if got, _ := os.ReadFile(path); string(got) != "schema_version: 0.0.1\n" {
		t.Errorf("the previous bytes changed: %q", got)
	}
}

// A write through a symlink keeps the link and updates its target.
func TestSaveCustomSources_WriteThroughASymlinkUpdatesTheTarget(t *testing.T) {
	path, save := scene(t)
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, []byte("schema_version: 0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := save(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v, %v", info, err)
	}
	if got, _ := os.ReadFile(target); string(got) == "schema_version: 0.0.1\n" {
		t.Errorf("the target was not updated")
	}
}

// A write keeps the mode the file had and leaves no temp file.
func TestSaveCustomSources_WriteKeepsTheExistingMode(t *testing.T) {
	path, save := scene(t)
	if err := os.WriteFile(path, []byte("schema_version: 0.0.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := save(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode after the write: %v, %v", info, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("stray files after the write: %v", entries)
	}
}

// A new file is 0644 under umask 022.
func TestSaveCustomSources_NewFileIs0644UnderUmask022(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	path, save := scene(t)
	if err := save(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode of a new file: %v, %v", info, err)
	}
}
