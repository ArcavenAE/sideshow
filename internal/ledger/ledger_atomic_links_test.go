//go:build unix

package ledger

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A save through a symlink leaves the link in place and updates its
// target, as an in-place write did (sideshow#180, ruled pin).
func TestSave_SaveThroughASymlinkUpdatesTheTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "repo-bindings.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("schema_version: 0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "repo-bindings.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	l := &Ledger{Repos: map[string]map[string]Row{"/r/x": {"alpha": {}}}}
	if err := l.Save(link); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced by a regular file: %v, %v", info, err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) == "schema_version: 0.0.1\n" {
		t.Errorf("the target was not updated: %q, %v", got, err)
	}
}

// A new file is created at 0644 under umask 022 (sideshow#180, ruled pin).
func TestSave_NewFileIs0644UnderUmask022(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	dir := t.TempDir()
	l := &Ledger{Repos: map[string]map[string]Row{"/r/x": {"alpha": {}}}}
	if err := l.Save(filepath.Join(dir, "repo-bindings.yaml")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "repo-bindings.yaml")); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode of a new file: %v, %v", info, err)
	}
}
