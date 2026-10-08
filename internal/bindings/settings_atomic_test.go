package bindings

import (
	"os"
	"path/filepath"
	"testing"
)

// The settings file is replaced, not rewritten in place, so a write that
// cannot complete leaves the user's previous settings intact instead of a
// torn file that stops enable and disable (sideshow#180).
func TestWriteSettings_AFailedWriteLeavesThePreviousFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := writeSettings(path, map[string]any{"env": map[string]any{"A": "1"}}); err != nil {
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

	if err := writeSettings(path, map[string]any{"env": map[string]any{"B": "2"}}); err == nil {
		t.Fatal("a write into a directory that cannot take a new file succeeded; the settings were written in place")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the previous settings changed:\nbefore %q\nafter  %q", before, after)
	}
}

// A successful write leaves only the settings file, with the mode it had.
func TestWriteSettings_LeavesNoTempFileAndKeepsTheMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeSettings(path, map[string]any{"env": map[string]any{"A": "1"}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory after write: %v, %v", entries, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode after write: %v, %v", info, err)
	}
}
