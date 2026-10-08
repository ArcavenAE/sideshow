//go:build unix

package bindings

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func withUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

// A replaced file keeps the mode it had, as an in-place write would
// (sideshow#173, review of #179).
func TestWriteFileAtomic_KeepsTheExistingFilesMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640, 0o644} {
		path := filepath.Join(t.TempDir(), "m.yaml")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := writeFileAtomic(path, []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("mode %v became %v", mode, info.Mode().Perm())
		}
	}
}

// A new file gets what os.WriteFile(path, b, 0644) would give: 0644
// minus the umask.
func TestWriteFileAtomic_NewFileHonoursTheUmask(t *testing.T) {
	for _, mask := range []int{0o022, 0o077, 0o002} {
		withUmask(t, mask)
		path := filepath.Join(t.TempDir(), "m.yaml")
		if err := writeFileAtomic(path, []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		ref := filepath.Join(t.TempDir(), "ref")
		if err := os.WriteFile(ref, []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		refInfo, err := os.Stat(ref)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != refInfo.Mode().Perm() {
			t.Errorf("umask %03o: mode %v, os.WriteFile gives %v", mask, info.Mode().Perm(), refInfo.Mode().Perm())
		}
	}
}

// A symlinked manifest stays a symlink: the target is replaced, the link
// is not.
func TestWriteFileAtomic_ReplacesTheTargetOfASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "m.yaml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "m.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(link, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink was replaced by a regular file: %v, %v", info, err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Errorf("target = %q, want new", got)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o600 {
		t.Errorf("target mode %v, want 0600", info.Mode().Perm())
	}
}

// The temp file sits in the manifest's own directory, not in TMPDIR and
// not in its parent: a save must succeed with TMPDIR unusable and the
// parent read-only.
func TestSaveManifest_TempFileIsCreatedBesideTheManifest(t *testing.T) {
	collisionEnv(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does", "not", "exist"))
	dir := filepath.Dir(manifestPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(dir)
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	if err := saveManifest([]ManifestEntry{{Pack: "alpha", Version: "1", Kind: "skill-dir", Path: "/x/y"}}); err != nil {
		t.Fatalf("save failed: %v", err)
	}
}
