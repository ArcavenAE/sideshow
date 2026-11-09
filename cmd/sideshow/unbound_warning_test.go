package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const unboundMarker = "_bmad/config.toml does not resolve"

// mkdangling plants a config.toml link whose target does not exist.
func mkdangling(t *testing.T, repo string) {
	t.Helper()
	dir := filepath.Join(repo, "_bmad")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), filepath.Join(dir, "config.toml")); err != nil {
		t.Fatal(err)
	}
}

func assertOneWarning(t *testing.T, out string) {
	t.Helper()
	if n := strings.Count(out, unboundMarker); n != 1 {
		t.Fatalf("want exactly one %q warning, got %d:\n%s", unboundMarker, n, out)
	}
	if !strings.Contains(out, "sideshow project init bmad") {
		t.Errorf("warning does not name the fix:\n%s", out)
	}
}

// Init that binds the repo fixes the state, so it must not warn about it.
func TestProjectInit_BindingTheRepoStaysQuiet(t *testing.T) {
	repo := linksFixture(t) // unbound: no _bmad/ at all
	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	})
	if err != nil {
		t.Fatalf("project init: %v", err)
	}
	if strings.Contains(out, "does not resolve") {
		t.Errorf("init warned about a state it just fixed:\n%s", out)
	}
	if _, serr := os.Stat(filepath.Join(repo, "_bmad", "config.toml")); serr != nil {
		t.Fatalf("fixture did not bind config.toml: %v", serr)
	}
}

// A dangling link that init leaves alone still does not resolve afterwards.
func TestProjectInit_DanglingLinkLeftAloneWarnsOnce(t *testing.T) {
	repo := linksFixture(t)
	mkdangling(t, repo)
	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	})
	if err != nil {
		t.Fatalf("a warning must not change the exit status: %v", err)
	}
	assertOneWarning(t, out)
}

// A dry run changes nothing, so it makes no claim about the final state.
func TestProjectInit_DryRunDoesNotWarn(t *testing.T) {
	linksFixture(t)
	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--dry-run"})
	})
	if err != nil {
		t.Fatalf("project init: %v", err)
	}
	if strings.Contains(out, "does not resolve") {
		t.Errorf("dry run warned:\n%s", out)
	}
}

// Sync in a repo that has bmad directories but no _bmad/ warns.
func TestSync_UnboundRepoWarns(t *testing.T) {
	repo := linksFixture(t)
	if err := os.MkdirAll(filepath.Join(repo, "_bmad-custom"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, runCommandsSync)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	assertOneWarning(t, out)
}

func TestSync_DanglingLinkWarns(t *testing.T) {
	repo := linksFixture(t)
	mkdangling(t, repo)
	out, err := captureStdout(t, runCommandsSync)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	assertOneWarning(t, out)
}

// A bound repo, and a directory that is not a bmad consumer at all, stay quiet.
func TestSync_BoundRepoStaysQuiet(t *testing.T) {
	linksFixture(t)
	if _, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	out, err := captureStdout(t, runCommandsSync)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if strings.Contains(out, "does not resolve") {
		t.Errorf("bound repo warned:\n%s", out)
	}
}

func TestSync_NonConsumerDirStaysQuiet(t *testing.T) {
	linksFixture(t)
	out, err := captureStdout(t, runCommandsSync)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if strings.Contains(out, "does not resolve") {
		t.Errorf("plain repo warned:\n%s", out)
	}
}
