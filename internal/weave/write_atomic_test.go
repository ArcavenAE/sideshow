//go:build unix

package weave

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// writeFilePreservingMode is the one writer behind the CSV, shim and YAML
// injections (sideshow#185 tier 3).
func scene(t *testing.T) (string, func() error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rows.csv")
	if err := os.WriteFile(path, []byte("a,b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, func() error { return writeFilePreservingMode(path, []byte("a,b\nc,d\n")) }
}

func TestWriteFilePreservingMode_AFailedWriteLeavesThePreviousBytes(t *testing.T) {
	path, save := scene(t)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := save(); err == nil {
		t.Fatal("a write into a directory that cannot take a new file succeeded; the file was written in place")
	}
	if got, _ := os.ReadFile(path); string(got) != "a,b\n" {
		t.Errorf("the previous bytes changed: %q", got)
	}
}

func TestWriteFilePreservingMode_WriteThroughASymlinkUpdatesTheTarget(t *testing.T) {
	path, save := scene(t)
	target := filepath.Join(t.TempDir(), "elsewhere.csv")
	if err := os.Rename(path, target); err != nil {
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
	if got, _ := os.ReadFile(target); string(got) != "a,b\nc,d\n" {
		t.Errorf("the target was not updated: %q", got)
	}
}

func TestWriteFilePreservingMode_KeepsTheExistingModeAndLeavesNoTempFile(t *testing.T) {
	path, save := scene(t)
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

func TestWriteFilePreservingMode_NewFileIs0644UnderUmask022(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	path, save := scene(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := save(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode of a new file: %v, %v", info, err)
	}
}
