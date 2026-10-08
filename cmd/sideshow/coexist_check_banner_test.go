package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A preflight that raised a WARN must not end on "preflight clean"
// (sideshow#95 item 2): the banner counts the warnings it passed with.
func TestRunCoexistCheck_BannerCountsWarningsItPassedWith(t *testing.T) {
	unwiredFixture(t, "alpha", "beta")
	if err := runCommandsSync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	repo := repoWithSkill(t, "alpha")

	out, err := captureStdout(t, func() error { return runCoexistCheck([]string{"demo", "--repo", repo}) })
	if err != nil {
		t.Fatalf("a shadow is advisory, got error: %v\n%s", err, out)
	}
	if strings.Contains(out, "preflight clean") {
		t.Errorf("a run with a WARN ended on the clean banner:\n%s", out)
	}
	if !strings.Contains(out, "preflight passed with 1 warning(s): enable/adopt may proceed") {
		t.Errorf("banner does not count the warning:\n%s", out)
	}
}

// Control: a run with no WARN and no ERROR still ends on the clean banner.
func TestRunCoexistCheck_NoWarningKeepsTheCleanBanner(t *testing.T) {
	unwiredFixture(t, "alpha")
	repo := repoWithSkill(t, "alpha")

	out, err := captureStdout(t, func() error { return runCoexistCheck([]string{"demo", "--repo", repo}) })
	if err != nil {
		t.Fatalf("runCoexistCheck: %v\n%s", err, out)
	}
	if !strings.Contains(out, "preflight clean: enable/adopt may proceed") {
		t.Errorf("a clean run lost the clean banner:\n%s", out)
	}
	if strings.Contains(out, "preflight passed with") {
		t.Errorf("a clean run printed the warning banner:\n%s", out)
	}
}

// The case from the issue, end to end: a foreign default agent beside a
// suppressed identity gives the WARN and the counted banner.
func TestRunCoexistCheck_DanglingDefaultAgentIsAWarning(t *testing.T) {
	unwiredFixture(t, "alpha")
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	for rel, body := range map[string]string{
		"plugins/installed_plugins.json": `{"version": 2, "plugins": {"demo@claude-mp": [{"scope": "project", "installPath": "/nonexistent", "version": "1.0.0", "gitCommitSha": "abc123"}]}}`,
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(cfg, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfg, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".claude", "settings.json"), []byte(`{"enabledPlugins": {"demo@claude-mp": true}, "agent": "demo:orchestrator"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".claude", "settings.local.json"), []byte(`{"enabledPlugins": {"demo@claude-mp": false}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error { return runCoexistCheck([]string{"demo", "--repo", repo}) })
	if err != nil {
		t.Fatalf("a dangling agent is advisory, got error: %v\n%s", err, out)
	}
	if !strings.Contains(out, `WARN [5 agent-key-audit]: default agent "demo:orchestrator"`) {
		t.Errorf("no default-agent WARN:\n%s", out)
	}
	if !strings.Contains(out, "preflight passed with 1 warning(s): enable/adopt may proceed") {
		t.Errorf("banner does not count the finding:\n%s", out)
	}
}
