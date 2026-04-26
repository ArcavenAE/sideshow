package permissions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsPath_ProjectScope(t *testing.T) {
	t.Parallel()
	got := SettingsPath(ScopeProject, "/tmp/example")
	want := "/tmp/example/.claude/settings.local.json"
	if got != want {
		t.Fatalf("SettingsPath(project)=%q want %q", got, want)
	}
}

func TestSettingsPath_UserScopeUsesHome(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	got := SettingsPath(ScopeUser, "/ignored")
	want := filepath.Join(home, ".claude", "settings.json")
	if got != want {
		t.Fatalf("SettingsPath(user)=%q want %q", got, want)
	}
}

func TestAddReadPermission_NewPath(t *testing.T) {
	t.Parallel()
	s := &ClaudeSettings{raw: map[string]any{}}
	if !s.AddReadPermission("/tmp/packs/") {
		t.Fatal("AddReadPermission returned false for new path")
	}
	allow := s.GetAllowList()
	if len(allow) != 1 || allow[0] != "Read(/tmp/packs/)" {
		t.Fatalf("allow list = %v, want [Read(/tmp/packs/)]", allow)
	}
}

func TestAddReadPermission_Idempotent(t *testing.T) {
	t.Parallel()
	s := &ClaudeSettings{raw: map[string]any{}}
	s.AddReadPermission("/tmp/packs/")
	if s.AddReadPermission("/tmp/packs/") {
		t.Fatal("AddReadPermission returned true for already-present path")
	}
	if got := len(s.GetAllowList()); got != 1 {
		t.Fatalf("allow list len = %d, want 1", got)
	}
}

func TestRemoveReadPermission_Removes(t *testing.T) {
	t.Parallel()
	s := &ClaudeSettings{raw: map[string]any{}}
	s.AddReadPermission("/tmp/packs/")
	s.AddReadPermission("/var/data/")
	if !s.RemoveReadPermission("/tmp/packs/") {
		t.Fatal("RemoveReadPermission returned false for present path")
	}
	allow := s.GetAllowList()
	if len(allow) != 1 || allow[0] != "Read(/var/data/)" {
		t.Fatalf("allow list = %v, want [Read(/var/data/)]", allow)
	}
}

func TestSaveAndLoadSettings_Roundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	s := &ClaudeSettings{raw: map[string]any{}}
	s.AddReadPermission("/tmp/packs/")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	allow := loaded.GetAllowList()
	if len(allow) != 1 || allow[0] != "Read(/tmp/packs/)" {
		t.Fatalf("loaded allow list = %v", allow)
	}
}

func TestSaveAndLoadSettings_PreservesUnrelatedKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Pre-populate a settings file with unrelated content.
	pre := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{"command": "bd prime"},
			},
		},
	}
	data, err := json.Marshal(pre)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Load, mutate via permissions, save.
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	s.AddReadPermission("/tmp/packs/")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Re-read raw bytes; verify both unrelated keys and new permissions.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var roundtrip map[string]any
	if err := json.Unmarshal(got, &roundtrip); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if roundtrip["theme"] != "dark" {
		t.Errorf("theme key lost: %v", roundtrip["theme"])
	}
	if _, ok := roundtrip["hooks"]; !ok {
		t.Errorf("hooks key lost")
	}
	if _, ok := roundtrip["permissions"]; !ok {
		t.Errorf("permissions key not added")
	}
}

func TestConfigureForScope_ProjectWritesProjectFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := ConfigureForScope(ScopeProject, "/tmp/packs", dir); err != nil {
		t.Fatalf("ConfigureForScope: %v", err)
	}
	expected := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected settings file at %s: %v", expected, err)
	}

	loaded, err := LoadSettings(expected)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	allow := loaded.GetAllowList()
	if len(allow) != 1 || allow[0] != "Read(/tmp/packs/)" {
		t.Fatalf("allow list = %v", allow)
	}
}
