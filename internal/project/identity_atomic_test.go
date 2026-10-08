//go:build unix

package project

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// scene returns the identity path and a create that writes it.
func scene(t *testing.T) (string, func() error) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sideshow"), 0o755); err != nil {
		t.Fatal(err)
	}
	return IdentityPath(root), func() error {
		_, err := InitIdentity(root, "demo", "")
		return err
	}
}

// The write creates a file that nothing rewrites later, so a torn one
// would stay torn (sideshow#185 tier 2). It never overwrites, so there is
// no previous-bytes pin; the guard test is the red evidence for this site.
// A new file is 0644 under umask 022 and leaves no temp file beside it.
func TestInitIdentity_NewFileIs0644UnderUmask022AndLeavesNoTempFile(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	path, create := scene(t)
	if err := create(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("mode of a new file: %v, %v", info, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("stray files beside the new file: %v", entries)
	}
}

// A dangling symlink at the path keeps its link and gets its target
// created, as an in-place write did.
func TestInitIdentity_DanglingSymlinkCreatesItsTarget(t *testing.T) {
	path, create := scene(t)
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := create(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v, %v", info, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("the target was not created: %v", err)
	}
}
