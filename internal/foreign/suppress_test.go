package foreign

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSuppressUnsuppress_RoundTrip(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	path := filepath.Join(repo, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"env": {"KEPT": "yes"}, "enabledPlugins": {"other@mp": true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := SuppressInRepo(repo, "vsdd-factory@claude-mp")
	if err != nil {
		t.Fatalf("SuppressInRepo: %v", err)
	}
	if created {
		t.Error("created=true on a pre-existing file")
	}
	settings, _, err := readSettingsObject(path)
	if err != nil {
		t.Fatal(err)
	}
	enables := settings["enabledPlugins"].(map[string]any)
	if v, ok := enables["vsdd-factory@claude-mp"].(bool); !ok || v {
		t.Errorf("suppression missing or true: %v", enables)
	}
	if enables["other@mp"] != true {
		t.Errorf("sibling enable disturbed: %v", enables)
	}
	env := settings["env"].(map[string]any)
	if env["KEPT"] != "yes" {
		t.Errorf("sibling key disturbed: %v", settings)
	}

	removed, err := UnsuppressInRepo(repo, "vsdd-factory@claude-mp")
	if err != nil || !removed {
		t.Fatalf("UnsuppressInRepo = %v, %v", removed, err)
	}
	settings, _, err = readSettingsObject(path)
	if err != nil {
		t.Fatal(err)
	}
	enables = settings["enabledPlugins"].(map[string]any)
	if _, ok := enables["vsdd-factory@claude-mp"]; ok {
		t.Errorf("suppression survived unsuppress: %v", enables)
	}
	if enables["other@mp"] != true {
		t.Errorf("sibling enable lost: %v", enables)
	}
}

func TestSuppress_CreatesFileAndUnsuppressPrunes(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()

	created, err := SuppressInRepo(repo, "vsdd-factory@claude-mp")
	if err != nil || !created {
		t.Fatalf("SuppressInRepo = created %v, %v; want created on a fresh repo", created, err)
	}

	removed, err := UnsuppressInRepo(repo, "vsdd-factory@claude-mp")
	if err != nil || !removed {
		t.Fatalf("UnsuppressInRepo = %v, %v", removed, err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["enabledPlugins"]; ok {
		t.Errorf("emptied enabledPlugins object not pruned: %s", data)
	}
}

func TestUnsuppress_LeavesUserTrueAlone(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	path := filepath.Join(repo, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := UnsuppressInRepo(repo, "vsdd-factory@claude-mp")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("removed a true entry; that is the user's own choice")
	}
}

func TestSuppress_RefusesMalformedSettings(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	path := filepath.Join(repo, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{broken`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := SuppressInRepo(repo, "vsdd-factory@claude-mp")
	if err == nil || !strings.Contains(err.Error(), "round-trip") {
		t.Fatalf("SuppressInRepo = %v, want fail-closed parse refusal", err)
	}
}
