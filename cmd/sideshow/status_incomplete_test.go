package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// While the sync manifest is incomplete, status prints one line saying
// when it was written and which bindings failed; a completed manifest
// prints none (sideshow#161).
func TestStatus_IncompleteManifestLine(t *testing.T) {
	installTwoVersions(t, "alpha", "")
	mf := filepath.Join(os.Getenv("SIDESHOW_HOME"), "sync-manifest.yaml")
	if err := os.WriteFile(mf, []byte("schema_version: 0.2.0\nsynced_at: \"2026-10-08T00:00:00Z\"\ncomplete: false\nfailed:\n  - pack: beta\n    version: 1.0.0\n    kind: markdown-command\n    error: boom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, runStatus)
	if err != nil {
		t.Fatal(err)
	}
	const want = "as of 2026-10-08T00:00:00Z; incomplete: beta/markdown-command"
	if strings.Count(out, want) != 1 {
		t.Errorf("want %q once:\n%s", want, out)
	}

	if err := os.WriteFile(mf, []byte("schema_version: 0.2.0\ncomplete: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = captureStdout(t, runStatus)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "incomplete") {
		t.Errorf("incomplete line after a completed sync:\n%s", out)
	}
}
