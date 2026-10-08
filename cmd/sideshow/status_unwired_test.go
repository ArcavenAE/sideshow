package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// unwiredFixture installs a native pack with the given skill names into
// an isolated store and config directory, and returns nothing: the
// caller reads status through runStatus.
func unwiredFixture(t *testing.T, skills ...string) {
	t.Helper()
	store := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SIDESHOW_HOME", store)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Cleanup(func() { _ = pack.UnfreezeTree(store) })

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte("name: demo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		dir := filepath.Join(src, ".claude", "skills", s)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + s + "\ndescription: " + s + "\n---\nx\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := pack.InstallFromLocal("demo", src, true); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
}

// aae-orc-zfkxy: after install and before sync, status printed
// available: N, synced: 0 and nothing else, so an installed pack that
// does nothing looked healthy. A line naming the sync command appears
// while synced is below available, and goes away once the sync runs.
func TestRunStatus_WarnsWhenPackIsAvailableButUnsynced(t *testing.T) {
	unwiredFixture(t, "alpha", "beta")

	out, err := captureStdout(t, runStatus)
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if !strings.Contains(out, "available: 2") || !strings.Contains(out, "synced:    0") {
		t.Fatalf("fixture did not reach the available 2, synced 0 state:\n%s", out)
	}
	if !strings.Contains(out, "UNWIRED") {
		t.Errorf("status has no UNWIRED line while synced < available:\n%s", out)
	}
	if !strings.Contains(out, "sideshow commands sync") {
		t.Errorf("UNWIRED line does not name the sync command:\n%s", out)
	}
}

// The control: a pack with nothing to bind has nothing to wire.
func TestRunStatus_NoWarningWhenNothingIsAvailable(t *testing.T) {
	unwiredFixture(t)

	out, err := captureStdout(t, runStatus)
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if strings.Contains(out, "UNWIRED") {
		t.Errorf("status warns for a pack with no bindable content:\n%s", out)
	}
}

func TestRunStatus_UnwiredLineGoesAwayAfterSync(t *testing.T) {
	unwiredFixture(t, "alpha", "beta")

	if err := runCommandsSync(); err != nil {
		t.Fatalf("runCommandsSync: %v", err)
	}
	out, err := captureStdout(t, runStatus)
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if !strings.Contains(out, "synced:    2") {
		t.Fatalf("sync did not reach synced 2:\n%s", out)
	}
	if strings.Contains(out, "UNWIRED") {
		t.Errorf("UNWIRED still printed after a full sync:\n%s", out)
	}
}

// Partly synced is still unwired, and the count says how many.
func TestRunStatus_UnwiredCountsTheUnsyncedRemainder(t *testing.T) {
	unwiredFixture(t, "alpha", "beta", "gamma")

	if err := runCommandsSync(); err != nil {
		t.Fatalf("runCommandsSync: %v", err)
	}
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if err := os.RemoveAll(filepath.Join(cfg, "skills", "gamma")); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, runStatus)
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if !strings.Contains(out, "UNWIRED:   1 of 3 artifacts are not synced") {
		t.Errorf("status does not report 1 of 3 unwired:\n%s", out)
	}
}

// A count that does not read cleanly is reported as an error, and the
// UNWIRED line must not appear beside it: synced would be a made-up zero.
func TestRunStatus_NoUnwiredLineWhenSyncedCountDoesNotRead(t *testing.T) {
	unwiredFixture(t, "alpha", "beta")

	// Sync first: the count reads only what the manifest says this pack
	// wrote, so the unreadable directory has to hold recorded skills for
	// the probe to reach it (aae-orc-zwx4b).
	if err := runCommandsSync(); err != nil {
		t.Fatalf("runCommandsSync: %v", err)
	}
	skills := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(skills, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skills, 0o755) })

	out, err := captureStdout(t, runStatus)
	if err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	if !strings.Contains(out, "synced:    error:") {
		t.Fatalf("probe did not make the synced count fail; output:\n%s", out)
	}
	if strings.Contains(out, "UNWIRED") {
		t.Errorf("UNWIRED printed beside an unreadable synced count:\n%s", out)
	}
}
