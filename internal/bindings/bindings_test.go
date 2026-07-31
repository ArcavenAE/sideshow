package bindings

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureOutput runs fn with os.Stdout and os.Stderr redirected and
// returns what was written to each.
func captureOutput(t *testing.T, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stdout pipe: %v", pipeErr)
	}
	rErr, wErr, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("stderr pipe: %v", pipeErr)
	}
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()

	err = fn()
	_ = wOut.Close()
	_ = wErr.Close()
	var bufOut, bufErr bytes.Buffer
	if _, copyErr := io.Copy(&bufOut, rOut); copyErr != nil {
		t.Fatalf("read stdout pipe: %v", copyErr)
	}
	if _, copyErr := io.Copy(&bufErr, rErr); copyErr != nil {
		t.Fatalf("read stderr pipe: %v", copyErr)
	}
	return bufOut.String(), bufErr.String(), err
}

// registerTestPack writes a registry.yaml under sideshowHome listing a
// single installed pack at packPath.
func registerTestPack(t *testing.T, sideshowHome, name, version, packPath string) {
	t.Helper()
	reg := fmt.Sprintf("packs:\n  - name: %s\n    version: %s\n    path: %s\n", name, version, packPath)
	writeFile(t, filepath.Join(sideshowHome, "registry.yaml"), reg)
}

// syncTestPack lays down a pack with bindable skill-dir content (which
// WOULD be written to user scope if discovery ran) plus the given
// pack.yaml content ("" means no pack.yaml at all), registers it, and
// runs Sync.
func syncTestPack(t *testing.T, packYAML string) (home, stdout, stderr string) {
	t.Helper()
	home = t.TempDir()
	store := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", store)

	packPath := filepath.Join(store, "packs", "vsdd-factory", "1.0.0-rc.23")
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "vsdd-probe", "SKILL.md"), "# probe\n")
	if packYAML != "" {
		writeFile(t, filepath.Join(packPath, "pack.yaml"), packYAML)
	}
	registerTestPack(t, store, "vsdd-factory", "1.0.0-rc.23", packPath)

	stdout, stderr, err := captureOutput(t, Sync)
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
	return home, stdout, stderr
}

func TestSync_FailsClosedOnUnreadableActivation(t *testing.T) {
	home, _, stderr := syncTestPack(t, "activation: [unclosed\n")

	if !strings.Contains(stderr, "activation unreadable") {
		t.Errorf("stderr missing fail-closed ERROR, got: %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
		t.Errorf("user-scope skills dir exists: pack with unreadable activation was synced (fail open)")
	}
}

func TestSync_SkipsPerRepoRequiredPack(t *testing.T) {
	home, stdout, _ := syncTestPack(t, "activation:\n  per_repo_required: true\n")

	if !strings.Contains(stdout, "per-repo-required pack") {
		t.Errorf("stdout missing per-repo-required announcement, got: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
		t.Errorf("user-scope skills dir exists: per-repo-required pack was synced at user scope")
	}
}

func TestSync_PluginClassPackStillSkipped(t *testing.T) {
	home, stdout, _ := syncTestPack(t, "activation:\n  mechanism: claude-plugin\n  per_repo_required: true\n")

	if !strings.Contains(stdout, "plugin-class pack") {
		t.Errorf("stdout missing plugin-class announcement, got: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(err) {
		t.Errorf("user-scope skills dir exists: plugin-class pack was synced at user scope")
	}
}

// The positive control: a pack with no pack.yaml syncs normally, which
// proves the fixture WOULD write to user scope if the guards above
// failed open.
func TestSync_PositiveControl_PlainPackSyncs(t *testing.T) {
	home, stdout, _ := syncTestPack(t, "")

	if !strings.Contains(stdout, "Synced") {
		t.Errorf("stdout missing sync summary, got: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "vsdd-probe", "SKILL.md")); err != nil {
		t.Errorf("expected synced skill at user scope, stat: %v", err)
	}
}
