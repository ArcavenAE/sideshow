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

// When the rename fails, the temp file does not stay behind.
func TestWriteFileAtomic_FailureRemovesTheTempFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "m.yaml")
	if err := os.Mkdir(target, 0o755); err != nil { // a directory cannot be replaced by a file
		t.Fatal(err)
	}
	if err := writeFileAtomic(target, []byte("x"), 0o644); err == nil {
		t.Fatal("renaming a file over a directory succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "m.yaml" {
		t.Errorf("stray files after a failed replace: %v", entries)
	}
}
