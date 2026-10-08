package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// installTwoVersions installs versions 1.0.0 (active) and 2.0.0 of one
// pack into an isolated store; extra is added to each pack.yaml.
func installTwoVersions(t *testing.T, name, extra string) {
	t.Helper()
	store := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SIDESHOW_HOME", store)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Cleanup(func() { _ = pack.UnfreezeTree(store) })
	for i, v := range []string{"1.0.0", "2.0.0"} {
		src := t.TempDir()
		yml := "name: " + name + "\nversion: " + v + "\n" + extra
		if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte(yml), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "x.md"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := pack.InstallFromLocal(name, src, i == 0); err != nil {
			t.Fatalf("install %s: %v", v, err)
		}
	}
}

const pluginClassYAML = "activation:\n  default_scope: per-repo\n  mechanism: claude-plugin\n  runbook: https://example.com/runbook\n"

// `use` on a plugin-class pack creates no user-scope bindings, but it does
// change which version a later `sideshow enable <pack>` binds when no
// @version is given. Its output has to say so rather than read as a no-op
// (aae-orc-y75sj).
func TestUse_PluginClassSaysWhatItChanged(t *testing.T) {
	installTwoVersions(t, "vsdd", pluginClassYAML)
	out, err := captureStdout(t, func() error { return runUse([]string{"vsdd", "2.0.0"}) })
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	for _, want := range []string{
		"Activated vsdd 2.0.0",
		"'sideshow enable vsdd' with no @version now binds 2.0.0",
		"repos already enabled keep the version they were enabled with",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// The statement comes before the global sync summary it explains.
	if i, j := strings.Index(out, "now binds 2.0.0"), strings.Index(out, "Synced "); i < 0 || j < 0 || i > j {
		t.Errorf("statement should precede the sync summary:\n%s", out)
	}
}

// A pack that syncs user-scope bindings gets no such statement: for it,
// the sync summary is the truthful account.
func TestUse_OrdinaryPackPrintsNoEnableStatement(t *testing.T) {
	installTwoVersions(t, "plain", "")
	out, err := captureStdout(t, func() error { return runUse([]string{"plain", "2.0.0"}) })
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if strings.Contains(out, "sideshow enable") {
		t.Errorf("ordinary pack output mentions enable:\n%s", out)
	}
}

// A per-repo-required pack without a plugin mechanism is skipped by the
// user-scope sync the same way, so `use` says the same about it.
func TestUse_PerRepoRequiredPackSaysWhatItChanged(t *testing.T) {
	installTwoVersions(t, "perrepo", "activation:\n  per_repo_required: true\n")
	out, err := captureStdout(t, func() error { return runUse([]string{"perrepo", "2.0.0"}) })
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if !strings.Contains(out, "'sideshow enable perrepo' with no @version now binds 2.0.0") {
		t.Errorf("output lacks the enable statement:\n%s", out)
	}
}
