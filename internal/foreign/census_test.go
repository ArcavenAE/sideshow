package foreign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fixtureConfig builds a harness config dir in the v2 shape observed
// in the finding-091 addendum-3 trials.
func fixtureConfig(t *testing.T, installedJSON, userSettings string) string {
	t.Helper()
	dir := t.TempDir()
	if installedJSON != "" {
		writeFile(t, filepath.Join(dir, "plugins", "installed_plugins.json"), installedJSON)
	}
	if userSettings != "" {
		writeFile(t, filepath.Join(dir, "settings.json"), userSettings)
	}
	return dir
}

func TestTakeCensus_MatchesByNameAcrossMarketplaces(t *testing.T) {
	dir := t.TempDir()
	installPath := filepath.Join(dir, "cache", "claude-mp", "vsdd-factory", "1.0.0-rc.23")
	writeFile(t, filepath.Join(installPath, ".claude-plugin", "plugin.json"),
		`{"name":"vsdd-factory","version":"1.0.0-rc.24"}`)

	cfg := fixtureConfig(t, `{
	  "version": 2,
	  "plugins": {
	    "vsdd-factory@claude-mp": [
	      {"scope":"user","installPath":"`+installPath+`","version":"1.0.0-rc.23","gitCommitSha":"80e5cd7b"}
	    ],
	    "vsdd-factory@vsdd-factory": [
	      {"scope":"user","installPath":"/nonexistent","version":"1.0.0-rc.6","gitCommitSha":"deadbeef"}
	    ],
	    "other-pack@claude-mp": [
	      {"scope":"user","installPath":"/x","version":"1.0.0"}
	    ]
	  }
	}`, "")

	c, err := TakeCensus(cfg, "vsdd-factory")
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	if len(c.Installs) != 2 {
		t.Fatalf("Installs = %d, want 2 (other-pack must be excluded): %+v", len(c.Installs), c.Installs)
	}
	current, legacy := c.Installs[0], c.Installs[1]
	if current.Marketplace != "claude-mp" || current.Legacy {
		t.Errorf("current install misclassified: %+v", current)
	}
	// The registry says rc.23 but the tree says rc.24: the tree is
	// the authority (git-subdir sources drift under a static label).
	if current.TreeVersion != "1.0.0-rc.24" {
		t.Errorf("TreeVersion = %q, want 1.0.0-rc.24 (tree is the version authority)", current.TreeVersion)
	}
	if !legacy.Legacy || legacy.Marketplace != "vsdd-factory" {
		t.Errorf("legacy identity not flagged: %+v", legacy)
	}
	if legacy.TreeVersion != "" {
		t.Errorf("missing tree must yield empty TreeVersion, got %q", legacy.TreeVersion)
	}
}

func TestResolveRepo_SuppressionPrecedence(t *testing.T) {
	cfg := fixtureConfig(t, `{
	  "version": 2,
	  "plugins": {"vsdd-factory@claude-mp": [{"scope":"user","installPath":"/x","version":"1"}]}
	}`, `{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`)

	c, err := TakeCensus(cfg, "vsdd-factory")
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}

	// T10: project-scope false suppresses a user-scope enable.
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".claude", "settings.json"),
		`{"enabledPlugins": {"vsdd-factory@claude-mp": false}}`)
	view, err := c.ResolveRepo(repo)
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	if len(view.EffectivelyEnabled) != 0 {
		t.Errorf("suppressed identity reported effective: %v", view.EffectivelyEnabled)
	}
	if len(view.Suppressed) != 1 || view.Suppressed[0] != "vsdd-factory@claude-mp" {
		t.Errorf("Suppressed = %v, want the claude-mp identity", view.Suppressed)
	}

	// No repo settings at all: the user-scope enable is effective.
	bare := t.TempDir()
	view, err = c.ResolveRepo(bare)
	if err != nil {
		t.Fatalf("ResolveRepo(bare): %v", err)
	}
	if len(view.EffectivelyEnabled) != 1 {
		t.Errorf("user-scope enable not effective in a bare repo: %+v", view)
	}

	// Local false over project true (personal-over-shared).
	repo2 := t.TempDir()
	writeFile(t, filepath.Join(repo2, ".claude", "settings.json"),
		`{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`)
	writeFile(t, filepath.Join(repo2, ".claude", "settings.local.json"),
		`{"enabledPlugins": {"vsdd-factory@claude-mp": false}}`)
	view, err = c.ResolveRepo(repo2)
	if err != nil {
		t.Fatalf("ResolveRepo(repo2): %v", err)
	}
	if len(view.EffectivelyEnabled) != 0 {
		t.Errorf("local false did not suppress project true: %+v", view)
	}
}

func TestResolveRepo_Orphans(t *testing.T) {
	cfg := fixtureConfig(t, `{"version":2,"plugins":{}}`,
		`{"enabledPlugins": {"vsdd-factory@ghost-mp": true}}`)
	c, err := TakeCensus(cfg, "vsdd-factory")
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".claude", "settings.json"),
		`{"enabledPlugins": {"vsdd-factory@claude-mp": false}}`)
	view, err := c.ResolveRepo(repo)
	if err != nil {
		t.Fatalf("ResolveRepo: %v", err)
	}
	if len(view.Orphans) != 2 {
		t.Fatalf("Orphans = %+v, want ghost-mp (user) and claude-mp (project)", view.Orphans)
	}
	if view.Orphans[0].Identity != "vsdd-factory@claude-mp" || view.Orphans[0].Scope != "project" {
		t.Errorf("orphan[0] = %+v", view.Orphans[0])
	}
	if view.Orphans[1].Identity != "vsdd-factory@ghost-mp" || !view.Orphans[1].Enabled {
		t.Errorf("orphan[1] = %+v", view.Orphans[1])
	}
}

func TestCensus_FailsClosedOnMalformedState(t *testing.T) {
	cfg := fixtureConfig(t, `{"version": 2, "plugins": {`, "")
	if _, err := TakeCensus(cfg, "vsdd-factory"); err == nil {
		t.Error("malformed installed_plugins.json must error, not classify on partial state")
	}

	cfg2 := fixtureConfig(t, `{"version":2,"plugins":{}}`, `{"enabledPlugins": [`)
	if _, err := TakeCensus(cfg2, "vsdd-factory"); err == nil {
		t.Error("malformed settings.json must error")
	}

	c, err := TakeCensus(fixtureConfig(t, "", ""), "vsdd-factory")
	if err != nil {
		t.Fatalf("empty config dir must census cleanly: %v", err)
	}
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ".claude", "settings.json"), "{broken")
	if _, err := c.ResolveRepo(repo); err == nil {
		t.Error("malformed repo settings must error")
	}
}

func TestSplitIdentity(t *testing.T) {
	tests := []struct {
		in, plugin, mp string
	}{
		{"vsdd-factory@claude-mp", "vsdd-factory", "claude-mp"},
		{"vsdd-factory@vsdd-factory", "vsdd-factory", "vsdd-factory"},
		{"bare", "bare", ""},
	}
	for _, tt := range tests {
		p, m := splitIdentity(tt.in)
		if p != tt.plugin || m != tt.mp {
			t.Errorf("splitIdentity(%q) = %q,%q want %q,%q", tt.in, p, m, tt.plugin, tt.mp)
		}
	}
}

func TestConfigDir_EnvOverride(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/cfg")
	if got := ConfigDir(); got != "/custom/cfg" {
		t.Errorf("ConfigDir() = %q, want env override", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := ConfigDir(); !strings.HasSuffix(got, filepath.Join("", ".claude")) {
		t.Errorf("ConfigDir() = %q, want ~/.claude fallback", got)
	}
}
