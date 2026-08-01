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

// TestConsentToPermissions is the regression guard for sideshow#94:
// the consent read treated anything that was not "n"/"no" as yes,
// including the empty string an EOF returns, so a piped or redirected
// install wrote Read permissions with nobody answering.
func TestConsentToPermissions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		isTerminal bool
		input      string
		want       bool
		// unanswered marks the refusals where nobody declined: the
		// message has to name --yes, or an unattended caller has no way
		// forward. An operator who typed "n" chose, and is not nudged.
		unanswered bool
	}{
		{"non-interactive stdin never consents", false, "", false, true},
		{"non-interactive stdin ignores even a typed yes", false, "y\n", false, true},
		{"eof with nothing typed is not consent", true, "", false, true},
		{"explicit n declines", true, "n\n", false, false},
		{"explicit no declines", true, "no\n", false, false},
		{"uppercase N declines", true, "N\n", false, false},
		{"bare enter keeps the documented [Y/n] default", true, "\n", true, false},
		{"explicit y accepts", true, "y\n", true, false},
		{"a terse answer without a newline still counts", true, "yes", true, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			got := consentToPermissions(
				strings.NewReader(tc.input), &out, tc.isTerminal,
				"/packs", "/settings.json",
			)
			if got != tc.want {
				t.Fatalf("consent = %v, want %v (output: %q)", got, tc.want, out.String())
			}
			if tc.unanswered && !strings.Contains(out.String(), "--yes") {
				t.Errorf("unanswered refusal did not name --yes: %q", out.String())
			}
		})
	}
}

// TestConsentToPermissions_NonInteractiveDoesNotPrompt guards the
// half that matters for unattended installs: a non-terminal stdin is
// not asked a question it cannot answer.
func TestConsentToPermissions_NonInteractiveDoesNotPrompt(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if consentToPermissions(strings.NewReader(""), &out, false, "/packs", "/settings.json") {
		t.Fatal("non-interactive stdin consented")
	}
	if strings.Contains(out.String(), "[Y/n]") {
		t.Errorf("prompted a non-interactive stdin: %q", out.String())
	}
}
