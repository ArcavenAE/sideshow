package enable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// enabledFixture stands up an enabled repo: ledger row + settings
// file, without running the full Enable pipeline.
func enabledFixture(t *testing.T, settingsContent string) (Options, string) {
	t.Helper()
	repo := t.TempDir()
	settings := filepath.Join(repo, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if settingsContent != "" {
		if err := os.WriteFile(settings, []byte(settingsContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ledgerPath := filepath.Join(t.TempDir(), "repo-bindings.yaml")
	led := &ledger.Ledger{}
	platform, _ := detectPlatform()
	if err := led.SetRow(repo, "vsdd-factory", ledger.Row{
		Version: "1.0.0-rc.23", StorePath: "/store/1.0.0-rc.23",
		Channel: "sideshow-native", Platform: platform, SettingsScope: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if err := led.Save(ledgerPath); err != nil {
		t.Fatal(err)
	}

	return Options{
		RepoDir: repo, Pack: "vsdd-factory", Prefix: "vsdd",
		LedgerPath: ledgerPath, ConfigDir: t.TempDir(),
	}, settings
}

func readAgent(t *testing.T, settings string) string {
	t.Helper()
	m, _, err := readSettingsJSON(settings)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m["agent"].(string)
	return s
}

func TestActivateDeactivate_RoundTrip(t *testing.T) {
	t.Parallel()
	opts, settings := enabledFixture(t, `{"env": {"OTEL_SERVICE_NAME": "kept"}}`)

	if err := Activate(opts, ""); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if got := readAgent(t, settings); got != "vsdd-orchestrator" {
		t.Errorf("agent = %q, want vsdd-orchestrator", got)
	}
	// The .50 env write and unrelated env keys survive.
	m, _, err := readSettingsJSON(settings)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := m["env"].(map[string]any)
	if env["OTEL_SERVICE_NAME"] != "kept" {
		t.Errorf("sibling env key disturbed: %v", m)
	}
	// No upstream state blocks are written; the ledger is the record.
	if _, ok := m["vsdd-factory"]; ok {
		t.Error("activated_platform block written into settings; the ledger is the single record")
	}

	if err := Deactivate(opts); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if got := readAgent(t, settings); got != "" {
		t.Errorf("agent survived deactivate: %q", got)
	}
}

func TestActivate_RefusesForeignAgentKey(t *testing.T) {
	t.Parallel()
	opts, settings := enabledFixture(t, `{"agent": "my-own-agent"}`)

	err := Activate(opts, "")
	if err == nil || !strings.Contains(err.Error(), "my-own-agent") {
		t.Fatalf("Activate = %v, want refusal naming the foreign agent", err)
	}
	if got := readAgent(t, settings); got != "my-own-agent" {
		t.Errorf("foreign agent key disturbed: %q", got)
	}
}

func TestActivate_RequiresEnabledRow(t *testing.T) {
	t.Parallel()
	opts := Options{
		RepoDir: t.TempDir(), Pack: "vsdd-factory", Prefix: "vsdd",
		LedgerPath: filepath.Join(t.TempDir(), "repo-bindings.yaml"),
		ConfigDir:  t.TempDir(),
	}
	err := Activate(opts, "")
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("Activate = %v, want not-enabled refusal", err)
	}
}

func TestActivate_RefusesUnprefixedAgentOverride(t *testing.T) {
	t.Parallel()
	opts, _ := enabledFixture(t, "")
	err := Activate(opts, "orchestrator")
	if err == nil || !strings.Contains(err.Error(), "binding prefix") {
		t.Fatalf("Activate = %v, want prefix refusal", err)
	}
}

func TestDeactivate_GuardRefusesForeignPersona(t *testing.T) {
	t.Parallel()
	opts, settings := enabledFixture(t, `{"agent": "someone-elses-agent"}`)

	err := Deactivate(opts)
	if err == nil || !strings.Contains(err.Error(), "refusing to remove") {
		t.Fatalf("Deactivate = %v, want guard refusal", err)
	}
	if got := readAgent(t, settings); got != "someone-elses-agent" {
		t.Errorf("foreign persona disturbed: %q", got)
	}

	// Absent key is a clean no-op, not an error.
	opts2, _ := enabledFixture(t, "")
	if err := Deactivate(opts2); err != nil {
		t.Errorf("Deactivate on empty settings: %v", err)
	}
}
