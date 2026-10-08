//go:build unix

package init

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// scene installs a one-module bmad pack and returns the module's config
// path and a Run that writes it.
func scene(t *testing.T) (string, func() error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	packDir := filepath.Join(home, "packs", "bmad", "1.0.0", "core")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "config.yaml"), []byte("user_name: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "packs", "bmad", "current")
	if err := os.Symlink("1.0.0", link); err != nil {
		t.Fatal(err)
	}
	registry := "packs:\n  - name: bmad\n    version: \"1.0.0\"\n    path: " + link + "\n"
	if err := os.WriteFile(filepath.Join(home, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "_bmad", "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(project, "_bmad", "core", "config.yaml"), func() error {
		return Run(project, "Michael")
	}
}

// The write creates a file that nothing rewrites later, so a torn one
// would stay torn (sideshow#185 tier 2). It never overwrites, so there is
// no previous-bytes pin; the guard test is the red evidence for this site.
// A new file is 0644 under umask 022 and leaves no temp file beside it.
func TestRun_NewFileIs0644UnderUmask022AndLeavesNoTempFile(t *testing.T) {
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
func TestRun_DanglingSymlinkCreatesItsTarget(t *testing.T) {
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
