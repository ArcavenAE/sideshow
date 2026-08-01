package coexistcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/foreign"
)

var testNow = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

func write(t *testing.T, base, rel, content string) {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// baseOptions builds a run against empty foreign config, empty
// ledger, and a minimal inventory.
func baseOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		RepoDir:         t.TempDir(),
		Pack:            "vsdd-factory",
		Prefix:          "vsdd",
		ConfigDir:       t.TempDir(),
		LedgerPath:      filepath.Join(t.TempDir(), "repo-bindings.yaml"),
		PerRepoRequired: true,
		Now:             testNow,
		Inventory: &bindings.PluginInventory{
			SkillDirs:  []string{"skills/pr-manager"},
			AgentFiles: []string{"agents/github-ops.md"},
		},
	}
}

func severities(rep *Report, check int) []foreign.Severity {
	var out []foreign.Severity
	for _, r := range rep.Results {
		if r.Check == check {
			out = append(out, r.Severity)
		}
	}
	return out
}

func TestRun_CleanRepoPasses(t *testing.T) {
	t.Parallel()
	rep, err := Run(baseOptions(t))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Refuse() {
		t.Errorf("clean repo refused: %+v", rep.Results)
	}
	if rep.RetreatAnchor == nil {
		t.Error("retreat anchor not captured")
	}
}

func TestRun_ForeignEnableIsHardRefusal(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	write(t, opts.ConfigDir, "plugins/config.json", `{
	  "version": 2,
	  "repositories": {
	    "claude-mp": {"plugins": {"vsdd-factory": [{"scope": "user", "installPath": "/nonexistent", "version": "1.0.0-rc.23"}]}}
	  }
	}`)
	write(t, opts.ConfigDir, "settings.json", `{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`)

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.Refuse() {
		t.Fatalf("foreign enable did not refuse: %+v", rep.Results)
	}
	found := false
	for _, r := range rep.Results {
		if r.Check == 10 && r.Severity == foreign.Error && strings.Contains(r.Detail, "never auto-") {
			found = true
		}
	}
	if !found {
		t.Errorf("check 10 hard refusal with consent menu missing: %+v", rep.Results)
	}
}

func TestRun_SuppressedForeignEnablePasses(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	write(t, opts.ConfigDir, "plugins/config.json", `{
	  "version": 2,
	  "repositories": {
	    "claude-mp": {"plugins": {"vsdd-factory": [{"scope": "user", "installPath": "/nonexistent", "version": "1.0.0-rc.23"}]}}
	  }
	}`)
	write(t, opts.ConfigDir, "settings.json", `{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`)
	// T10: repo-side suppression beats the user-scope enable, but the
	// per-repo-required containment error on the user-scope enable
	// itself still fires.
	write(t, opts.RepoDir, ".claude/settings.local.json", `{"enabledPlugins": {"vsdd-factory@claude-mp": false}}`)

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, r := range rep.Results {
		if r.Check == 10 {
			t.Errorf("suppressed identity still graded dual-enable: %+v", r)
		}
	}
}

func TestRun_ContentCollisionRefuses(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	write(t, opts.RepoDir, ".claude/skills/vsdd-pr-manager/SKILL.md", "# theirs\n")

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sevs := severities(rep, 9)
	if len(sevs) != 1 || sevs[0] != foreign.Error {
		t.Errorf("check 9 = %v, want one ERROR", sevs)
	}
}

func TestRun_RunningFactorySignals(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	write(t, opts.RepoDir, ".factory/STATE.md",
		"---\nfactory_lock:\n  holder: dev@example.com\n  locked_at: 2026-07-31T11:50:00Z\n  expires_at: 2026-07-31T12:30:00Z\n---\n")
	if err := os.MkdirAll(filepath.Join(opts.RepoDir, ".worktrees", "STORY-042"), 0o755); err != nil {
		t.Fatal(err)
	}

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.Refuse() {
		t.Fatalf("unexpired lock did not refuse: %+v", rep.Results)
	}
	var sawLock, sawWorktrees bool
	for _, r := range rep.Results {
		if r.Check != 2 {
			continue
		}
		if r.Severity == foreign.Error && strings.Contains(r.Detail, "dev@example.com") {
			sawLock = true
		}
		if strings.Contains(r.Detail, "STORY-042") {
			sawWorktrees = true
		}
	}
	if !sawLock || !sawWorktrees {
		t.Errorf("lock=%v worktrees=%v: %+v", sawLock, sawWorktrees, rep.Results)
	}
}

func TestRun_VersionSkewAndPlatformBind(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	store := t.TempDir()
	opts.StoreRoot = filepath.Join(store, "1.0.0-rc.24")
	write(t, opts.StoreRoot, "hooks/dispatcher/placeholder", "not executable\n")
	write(t, "", opts.LedgerPath, "schema_version: \"0.1.0\"\nrepos:\n  "+opts.RepoDir+":\n    vsdd-factory:\n      version: 1.0.0-rc.23\n      store_path: /store/1.0.0-rc.23\n      settings_scope: local\n")

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sevs := severities(rep, 6); len(sevs) != 1 || sevs[0] != foreign.Warn {
		t.Errorf("check 6 = %v, want one WARN", sevs)
	}
	// Bound repo with no env shim written: check 4 flags it.
	var envShimError bool
	for _, r := range rep.Results {
		if r.Check == 4 && r.Severity == foreign.Error && strings.Contains(r.Detail, "env shim") {
			envShimError = true
		}
	}
	if !envShimError {
		t.Errorf("check 4 missing env-shim error: %+v", rep.Results)
	}
}

func TestSnapshotAndDiffFootprints(t *testing.T) {
	t.Parallel()
	cfg := t.TempDir()
	write(t, cfg, "plugins/config.json", `{"version": 2}`)
	write(t, cfg, "settings.json", `{"enabledPlugins": {}}`)

	before, err := SnapshotForeignFootprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("snapshot = %v, want 2 entries", before)
	}
	after, err := SnapshotForeignFootprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if diff := DiffFootprints(before, after); len(diff) > 0 {
		t.Errorf("identical footprints diffed: %v", diff)
	}

	write(t, cfg, "plugins/config.json", `{"version": 2, "changed": true}`)
	after, err = SnapshotForeignFootprint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	diff := DiffFootprints(before, after)
	if len(diff) != 1 || !strings.Contains(diff[0], "changed plugins/config.json") {
		t.Errorf("diff = %v, want the changed registry", diff)
	}
}
