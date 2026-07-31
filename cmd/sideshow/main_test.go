package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected and returns what was
// written.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stdout pipe: %v", pipeErr)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	err := fn()
	_ = w.Close()
	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		t.Fatalf("read stdout pipe: %v", copyErr)
	}
	return buf.String(), err
}

func TestRunStatus_FailsClosedOnUnreadableActivation(t *testing.T) {
	home := t.TempDir()
	store := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", store)

	packPath := filepath.Join(store, "packs", "vsdd-factory", "1.0.0-rc.23")
	if err := os.MkdirAll(packPath, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packPath, "pack.yaml"), []byte("activation: [unclosed\n"), 0o644); err != nil {
		t.Fatalf("write pack.yaml: %v", err)
	}
	reg := fmt.Sprintf("packs:\n  - name: vsdd-factory\n    version: 1.0.0-rc.23\n    path: %s\n", packPath)
	if err := os.WriteFile(filepath.Join(store, "registry.yaml"), []byte(reg), 0o644); err != nil {
		t.Fatalf("write registry.yaml: %v", err)
	}

	out, err := captureStdout(t, runStatus)
	if err != nil {
		t.Fatalf("runStatus() error: %v", err)
	}
	if !strings.Contains(out, "activation: ERROR:") {
		t.Errorf("status output missing activation ERROR line, got: %q", out)
	}
	if strings.Contains(out, "available:") {
		t.Errorf("status evaluated bindings for a pack with unreadable activation (fail open), got: %q", out)
	}
}
