package permissions

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// aae-orc-8qmpi: Claude Code reads a single leading slash in a Read rule
// as anchored at the settings source, not at the filesystem root, so
// Read(/abs/packs/) denies the store read like no rule at all. An
// absolute path is written with a second slash, Read(//abs/packs/).
// Observed on Claude Code 2.1.292 with `claude -p`: one slash denied the
// read with or without a glob; two slashes allowed it with /, /* and /**.

func projectAllow(t *testing.T, root string) []string {
	t.Helper()
	s, err := LoadSettings(SettingsPath(ScopeProject, root))
	if err != nil {
		t.Fatal(err)
	}
	return s.GetAllowList()
}

func seedAllow(t *testing.T, root string, allow ...string) {
	t.Helper()
	s := &ClaudeSettings{raw: map[string]any{}}
	for _, a := range allow {
		s.raw["permissions"] = map[string]any{"allow": append(allowAny(s), a)}
	}
	if err := s.Save(SettingsPath(ScopeProject, root)); err != nil {
		t.Fatal(err)
	}
}

func allowAny(s *ClaudeSettings) []any {
	perms, ok := s.raw["permissions"].(map[string]any)
	if !ok {
		return nil
	}
	a, _ := perms["allow"].([]any)
	return a
}

func TestConfigureForScope_WritesTheDoubleSlashAnchor(t *testing.T) {
	root := t.TempDir()
	if err := ConfigureForScope(ScopeProject, "/store/packs", root); err != nil {
		t.Fatal(err)
	}
	got := projectAllow(t, root)
	if !slices.Contains(got, "Read(//store/packs/)") {
		t.Errorf("allow = %v, want Read(//store/packs/)", got)
	}
	if slices.Contains(got, "Read(/store/packs/)") {
		t.Errorf("allow = %v still carries the single-slash form", got)
	}
}

func TestConfigureForScope_ReplacesTheOldSingleSlashRule(t *testing.T) {
	root := t.TempDir()
	seedAllow(t, root, "Bash(ls:*)", "Read(/store/packs/)", "Read(/other/packs/)")

	if err := ConfigureForScope(ScopeProject, "/store/packs", root); err != nil {
		t.Fatal(err)
	}
	got := projectAllow(t, root)
	want := []string{"Bash(ls:*)", "Read(/other/packs/)", "Read(//store/packs/)"}
	if !slices.Equal(got, want) {
		t.Errorf("allow = %v, want %v (old rule for this store replaced, others untouched)", got, want)
	}

	// A second run changes nothing.
	if err := ConfigureForScope(ScopeProject, "/store/packs", root); err != nil {
		t.Fatal(err)
	}
	if again := projectAllow(t, root); !slices.Equal(again, want) {
		t.Errorf("second run: allow = %v, want %v", again, want)
	}
}

func TestConfigureForScope_LeavesAnAlreadyAnchoredRuleAlone(t *testing.T) {
	root := t.TempDir()
	seedAllow(t, root, "Read(//store/packs/)")
	path := SettingsPath(ScopeProject, root)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := ConfigureForScope(ScopeProject, "/store/packs", root); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("settings file rewritten although the anchored rule was present:\n%s\n--\n%s", before, after)
	}
}

func TestConfigureForScope_UserScopeAnchorsToo(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)

	if err := ConfigureForScope(ScopeUser, "/store/packs", "."); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings(filepath.Join(cfg, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.GetAllowList(); !slices.Contains(got, "Read(//store/packs/)") {
		t.Errorf("allow = %v, want Read(//store/packs/)", got)
	}
}
