package bindings

import (
	"os"
	"path/filepath"
	"testing"
)

// The manifest is replaced, not rewritten in place, so a save that cannot
// complete leaves the previous manifest intact instead of a torn file that
// stops every later sync (sideshow#173). A directory that cannot take a new
// file makes the difference observable: an in-place write still succeeds
// into the existing file; a write-then-rename cannot create its temp file
// and must leave the old manifest alone.
func TestSaveManifest_AFailedSaveLeavesThePreviousManifest(t *testing.T) {
	collisionEnv(t)
	if err := saveManifest([]ManifestEntry{{Pack: "alpha", Version: "1", Kind: "skill-dir", Path: "/x/old"}}); err != nil {
		t.Fatal(err)
	}
	path := manifestPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := saveManifest([]ManifestEntry{{Pack: "beta", Version: "2", Kind: "skill-dir", Path: "/x/new"}}); err == nil {
		t.Fatal("a save into a directory that cannot take a new file succeeded; the manifest was written in place")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the previous manifest changed:\nbefore %q\nafter  %q", before, after)
	}
	if m, lerr := LoadManifest(); lerr != nil || len(m.Entries) != 1 || m.Entries[0].Pack != "alpha" {
		t.Errorf("manifest no longer loads as the old one: %+v, %v", m, lerr)
	}
}

// A successful save leaves only the manifest, with the mode it had before.
func TestSaveManifest_LeavesNoTempFileAndKeepsTheMode(t *testing.T) {
	collisionEnv(t)
	for i := 0; i < 2; i++ {
		if err := saveManifest([]ManifestEntry{{Pack: "alpha", Version: "1", Kind: "skill-dir", Path: "/x/y"}}); err != nil {
			t.Fatal(err)
		}
	}
	path := manifestPath()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("stray file left beside the manifest: %s", e.Name())
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("manifest mode = %v, want 0644", info.Mode().Perm())
	}
	if m, lerr := LoadManifest(); lerr != nil || len(m.Entries) != 1 {
		t.Errorf("manifest does not load: %+v, %v", m, lerr)
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
