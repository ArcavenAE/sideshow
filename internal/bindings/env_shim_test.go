package bindings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func settingsFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.local.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("settings not valid JSON after write: %v\n%s", err, data)
	}
	return out
}

func TestMergeEnvShim_CreatesFile(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, "")

	created, err := MergeEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/packs/vsdd-factory/1.0.0-rc.23")
	if err != nil {
		t.Fatalf("MergeEnvShim: %v", err)
	}
	if !created {
		t.Error("created = false for a file that did not exist")
	}
	env := readJSON(t, path)["env"].(map[string]any)
	if got := env["CLAUDE_PLUGIN_ROOT"]; got != "/store/packs/vsdd-factory/1.0.0-rc.23" {
		t.Errorf("env.CLAUDE_PLUGIN_ROOT = %v", got)
	}
}

func TestMergeEnvShim_PreservesOtherKeys(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, `{"permissions": {"allow": ["Bash(ls:*)"]}, "env": {"OTHER": "kept"}}`)

	created, err := MergeEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/v1")
	if err != nil {
		t.Fatalf("MergeEnvShim: %v", err)
	}
	if created {
		t.Error("created = true for a pre-existing file")
	}
	got := readJSON(t, path)
	if _, ok := got["permissions"]; !ok {
		t.Error("permissions key lost in merge")
	}
	env := got["env"].(map[string]any)
	if env["OTHER"] != "kept" {
		t.Errorf("sibling env entry lost: %v", env)
	}
	if env["CLAUDE_PLUGIN_ROOT"] != "/store/v1" {
		t.Errorf("shim not merged: %v", env)
	}
}

func TestMergeEnvShim_IdempotentAndClobberRefusal(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, `{"env": {"CLAUDE_PLUGIN_ROOT": "/store/v1"}}`)

	if _, err := MergeEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/v1"); err != nil {
		t.Fatalf("same-value merge should be idempotent: %v", err)
	}
	_, err := MergeEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/v2")
	if !errors.Is(err, ErrWouldClobber) {
		t.Fatalf("err = %v, want ErrWouldClobber", err)
	}
	// The refused merge changed nothing.
	env := readJSON(t, path)["env"].(map[string]any)
	if env["CLAUDE_PLUGIN_ROOT"] != "/store/v1" {
		t.Errorf("refused merge mutated the file: %v", env)
	}
}

func TestMergeEnvShim_FailsClosedOnMalformedSettings(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"invalid json":   `{not json`,
		"env not object": `{"env": ["list"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := settingsFixture(t, content)
			if _, err := MergeEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/v1"); err == nil {
				t.Error("merge accepted a settings file it cannot round-trip")
			}
		})
	}
}

func TestRemoveEnvShim_RemovesOnlyOurValue(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, `{"env": {"CLAUDE_PLUGIN_ROOT": "/store/v1", "OTHER": "kept"}, "model": "opus"}`)

	removed, err := RemoveEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/v1")
	if err != nil || !removed {
		t.Fatalf("RemoveEnvShim = (%v, %v), want (true, nil)", removed, err)
	}
	got := readJSON(t, path)
	env := got["env"].(map[string]any)
	if _, ok := env["CLAUDE_PLUGIN_ROOT"]; ok {
		t.Error("shim entry survived removal")
	}
	if env["OTHER"] != "kept" || got["model"] != "opus" {
		t.Errorf("removal disturbed sibling keys: %v", got)
	}
}

func TestRemoveEnvShim_NeverGuesses(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, `{"env": {"CLAUDE_PLUGIN_ROOT": "/user/own/value"}}`)

	removed, err := RemoveEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/v1")
	if err != nil {
		t.Fatalf("RemoveEnvShim: %v", err)
	}
	if removed {
		t.Error("removed a value sideshow did not write")
	}
	env := readJSON(t, path)["env"].(map[string]any)
	if env["CLAUDE_PLUGIN_ROOT"] != "/user/own/value" {
		t.Errorf("foreign value disturbed: %v", env)
	}

	if removed, err := RemoveEnvShim(filepath.Join(t.TempDir(), "absent.json"), "X", "y"); err != nil || removed {
		t.Errorf("missing file: RemoveEnvShim = (%v, %v), want (false, nil)", removed, err)
	}
}

func TestRemoveEnvShim_PrunesEmptyEnv(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, `{"env": {"CLAUDE_PLUGIN_ROOT": "/store/v1"}, "model": "opus"}`)

	if _, err := RemoveEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/v1"); err != nil {
		t.Fatal(err)
	}
	got := readJSON(t, path)
	if _, ok := got["env"]; ok {
		t.Errorf("empty env object not pruned: %v", got)
	}
}

func TestVerifyEnvShim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"resolves", `{"env": {"CLAUDE_PLUGIN_ROOT": "/store/v1"}}`, ""},
		{"missing file", "", "does not exist"},
		{"missing entry", `{"env": {}}`, "no env.CLAUDE_PLUGIN_ROOT"},
		{"drifted", `{"env": {"CLAUDE_PLUGIN_ROOT": "/elsewhere"}}`, "drifted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := settingsFixture(t, tt.content)
			err := VerifyEnvShim(path, "CLAUDE_PLUGIN_ROOT", "/store/v1")
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("VerifyEnvShim = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("VerifyEnvShim = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestInlineEnvBelt_QuotesSafely(t *testing.T) {
	t.Parallel()
	got := InlineEnvBelt("CLAUDE_PLUGIN_ROOT", "/store/o'brien")
	want := `CLAUDE_PLUGIN_ROOT='/store/o'\''brien' `
	if got != want {
		t.Errorf("InlineEnvBelt = %q, want %q", got, want)
	}
}

// Regression coverage for the shim-prefix parameterization: a non-bmad
// prefix routes correctly, and the sibling per-repo dirs stay literal.
func TestRewriteForPrefix(t *testing.T) {
	t.Parallel()

	store := newFixtureStore(t, "", "templates/a.md")
	rules := newPackRefRules(store, "_vsdd")

	in := "read {project-root}/_vsdd/templates/a.md, write {project-root}/_vsdd-custom/x " +
		"and {project-root}/_vsdd-output/y, leave {project-root}/README.md"
	want := "read " + store + "/templates/a.md, write {project-root}/_vsdd-custom/x " +
		"and {project-root}/_vsdd-output/y, leave {project-root}/README.md"

	if got := rules.rewrite(in); got != want {
		t.Errorf("rewrite:\n got %q\nwant %q", got, want)
	}

	// A _bmad reference is not this pack's prefix and stays untouched.
	other := "{project-root}/_bmad/core/a.md"
	if got := rules.rewrite(other); got != other {
		t.Errorf("rewrite touched another pack's prefix:\n got %q\nwant %q", got, other)
	}
}
