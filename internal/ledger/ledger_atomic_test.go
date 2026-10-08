package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

// The ledger is replaced, not rewritten in place, so a save that cannot
// complete leaves the previous ledger intact instead of a torn file that
// stops enable and disable (sideshow#180). A directory that cannot take a
// new file makes it observable: an in-place write still succeeds into the
// existing file; a write-then-rename cannot create its temp file.
func TestSave_AFailedSaveLeavesThePreviousLedger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repo-bindings.yaml")
	old := &Ledger{Repos: map[string]map[string]Row{"/r/old": {"alpha": {}}}}
	if err := old.Save(path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	next := &Ledger{Repos: map[string]map[string]Row{"/r/new": {"beta": {}}}}
	if err := next.Save(path); err == nil {
		t.Fatal("a save into a directory that cannot take a new file succeeded; the ledger was written in place")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the previous ledger changed:\nbefore %q\nafter  %q", before, after)
	}
	if l, lerr := Load(path); lerr != nil || len(l.Repos) != 1 || l.Repos["/r/old"] == nil {
		t.Errorf("ledger no longer loads as the old one: %+v, %v", l, lerr)
	}
}

// A successful save leaves only the ledger, with the mode it had.
func TestSave_LeavesNoTempFileAndKeepsTheMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repo-bindings.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 0.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := &Ledger{Repos: map[string]map[string]Row{"/r/x": {"alpha": {}}}}
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory after save: %v, %v", entries, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode after save: %v, %v", info, err)
	}
}
