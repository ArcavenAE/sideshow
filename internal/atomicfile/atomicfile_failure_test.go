package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

// When the rename fails, the temp file does not stay behind.
func TestWriteFile_FailureRemovesTheTempFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "m.yaml")
	if err := os.Mkdir(target, 0o755); err != nil { // a directory cannot be replaced by a file
		t.Fatal(err)
	}
	if err := WriteFile(target, []byte("x"), 0o644); err == nil {
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
