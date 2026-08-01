package foreign

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		scope   string
		want    string
		wantErr bool
	}{
		{ScopeLocal, filepath.Join("repo", ".claude", "settings.local.json"), false},
		{ScopeProject, filepath.Join("repo", ".claude", "settings.json"), false},
		{ScopeUser, filepath.Join("config", "settings.json"), false},
		{"machine", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.scope, func(t *testing.T) {
			t.Parallel()
			got, err := SettingsPath("repo", "config", tc.scope)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SettingsPath(%q) error = %v, wantErr %v", tc.scope, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("SettingsPath(%q) = %q, want %q", tc.scope, got, tc.want)
			}
		})
	}
}

func TestSetEnable_RoundTripPreservesSiblings(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".claude", "settings.local.json")

	created, err := SetEnable(path, "vsdd-factory@claude-mp", true)
	if err != nil {
		t.Fatalf("SetEnable: %v", err)
	}
	if !created {
		t.Error("created=false for a file that did not exist")
	}
	entry, err := ReadEnable(path, "vsdd-factory@claude-mp")
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Present || !entry.Value || entry.Path != path {
		t.Errorf("ReadEnable = %+v, want present true at %s", entry, path)
	}

	// A second identity and an unrelated key must both survive a delete.
	if _, err := SetEnable(path, "other@mp", false); err != nil {
		t.Fatal(err)
	}
	settings, _, err := readSettingsObject(path)
	if err != nil {
		t.Fatal(err)
	}
	settings["env"] = map[string]any{"KEPT": "yes"}
	if err := writeSettingsObject(path, settings); err != nil {
		t.Fatal(err)
	}

	removed, err := DeleteEnable(path, "vsdd-factory@claude-mp")
	if err != nil || !removed {
		t.Fatalf("DeleteEnable = %v, %v", removed, err)
	}
	after, _, err := readSettingsObject(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after["env"]; !ok {
		t.Errorf("unrelated key dropped: %v", after)
	}
	enables, _ := after["enabledPlugins"].(map[string]any)
	if _, still := enables["vsdd-factory@claude-mp"]; still {
		t.Errorf("entry survived deletion: %v", enables)
	}
	if _, sibling := enables["other@mp"]; !sibling {
		t.Errorf("sibling identity dropped: %v", enables)
	}
}

func TestDeleteEnable_PrunesEmptiedObjectButKeepsTheFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".claude", "settings.local.json")
	if _, err := SetEnable(path, "vsdd-factory@claude-mp", true); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteEnable(path, "vsdd-factory@claude-mp"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file removed by DeleteEnable: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings["enabledPlugins"]; ok {
		t.Errorf("emptied object not pruned: %s", data)
	}
}

func TestDeleteEnable_MissingFileAndMissingKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	removed, err := DeleteEnable(filepath.Join(dir, "absent.json"), "vsdd-factory@claude-mp")
	if err != nil || removed {
		t.Errorf("DeleteEnable on a missing file = %v, %v; want false, nil", removed, err)
	}

	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"enabledPlugins": {"other@mp": true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err = DeleteEnable(path, "vsdd-factory@claude-mp")
	if err != nil || removed {
		t.Errorf("DeleteEnable on a missing key = %v, %v; want false, nil", removed, err)
	}
}

func TestCanWriteSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string) string
		wantErr string
	}{
		{
			name: "absent file in a writable dir",
			setup: func(t *testing.T, dir string) string {
				return filepath.Join(dir, ".claude", "settings.local.json")
			},
		},
		{
			name: "existing writable file",
			setup: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "settings.json")
				if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
					t.Fatal(err)
				}
				return path
			},
		},
		{
			name: "unparseable file",
			setup: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "settings.json")
				if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr: "refusing to merge",
		},
		{
			name: "enabledPlugins of the wrong shape",
			setup: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "settings.json")
				if err := os.WriteFile(path, []byte(`{"enabledPlugins": ["a"]}`), 0o644); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr: "not an object",
		},
		{
			name: "read-only file",
			setup: func(t *testing.T, dir string) string {
				if os.Geteuid() == 0 {
					t.Skip("root ignores the write bit")
				}
				path := filepath.Join(dir, "settings.json")
				if err := os.WriteFile(path, []byte(`{}`), 0o400); err != nil {
					t.Fatal(err)
				}
				return path
			},
			wantErr: "not writable",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := tc.setup(t, t.TempDir())
			err := CanWriteSettings(path)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("CanWriteSettings = %v, want nil", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("CanWriteSettings = nil, want %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("CanWriteSettings = %v, want it to name %q", err, tc.wantErr)
			}
			// Validation must not write, and that includes the parent
			// directory SetEnable would create: a dry run that leaves a
			// new .claude/ behind has already written.
			if tc.name == "absent file in a writable dir" {
				for _, absent := range []string{path, filepath.Dir(path)} {
					if _, statErr := os.Stat(absent); !os.IsNotExist(statErr) {
						t.Errorf("validation created %s", absent)
					}
				}
			}
		})
	}
}

// censusFixture writes a harness config dir with the given install
// registry and user settings, and returns a census for pack.
func censusFixture(t *testing.T, pack, registry, userSettings string) *Census {
	t.Helper()
	dir := t.TempDir()
	if registry != "" {
		if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugins", "installed_plugins.json"), []byte(registry), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if userSettings != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(userSettings), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := TakeCensus(dir, pack)
	if err != nil {
		t.Fatalf("TakeCensus: %v", err)
	}
	return c
}

func TestCensus_UserEnabledIdentities(t *testing.T) {
	t.Parallel()
	c := censusFixture(t, "vsdd-factory", "", `{"enabledPlugins": {
	  "vsdd-factory@claude-mp": true,
	  "vsdd-factory@vsdd-factory": false,
	  "other@claude-mp": true
	}}`)
	got := c.UserEnabledIdentities()
	if len(got) != 1 || got[0] != "vsdd-factory@claude-mp" {
		t.Errorf("UserEnabledIdentities = %v, want only the pack's true entry", got)
	}
	if path := c.UserEnablePath("vsdd-factory@claude-mp"); !strings.HasSuffix(path, "settings.json") {
		t.Errorf("UserEnablePath = %q, want the user settings file", path)
	}
}

// TestCensus_WithoutUserEnables covers the counterfactual the migration
// turns on: with the machine-wide entry gone, does this repo still load
// the foreign channel?
func TestCensus_WithoutUserEnables(t *testing.T) {
	t.Parallel()
	reg := `{"version": 2, "plugins": {"vsdd-factory@claude-mp": [{"scope": "user", "installPath": "/tmp/x", "version": "1.0.0"}]}}`
	c := censusFixture(t, "vsdd-factory", reg, `{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`)

	dependent := t.TempDir()
	independent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(independent, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(independent, ".claude", "settings.json"),
		[]byte(`{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	after := c.WithoutUserEnables([]string{"vsdd-factory@claude-mp"})
	if ids := after.UserEnabledIdentities(); len(ids) != 0 {
		t.Errorf("clone still carries user enables: %v", ids)
	}
	if ids := c.UserEnabledIdentities(); len(ids) != 1 {
		t.Errorf("original census mutated: %v", ids)
	}

	for _, tc := range []struct {
		name        string
		repo        string
		wantEnabled bool
	}{
		{"depends on the machine-wide enable", dependent, false},
		{"enabled independently at project scope", independent, true},
	} {
		view, err := after.ResolveRepo(tc.repo)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(view.EffectivelyEnabled) > 0; got != tc.wantEnabled {
			t.Errorf("%s: enabled without user scope = %v, want %v", tc.name, got, tc.wantEnabled)
		}
	}
}

func TestCensus_MarketplaceSiblings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		registry string
		want     []string
	}{
		{
			name:     "sole plugin",
			registry: `{"version": 2, "plugins": {"vsdd-factory@claude-mp": [{"scope": "user"}]}}`,
			want:     nil,
		},
		{
			name: "shares the marketplace",
			registry: `{"version": 2, "plugins": {
			  "vsdd-factory@claude-mp": [{"scope": "user"}],
			  "other-pack@claude-mp": [{"scope": "user"}],
			  "elsewhere@another-mp": [{"scope": "user"}]
			}}`,
			want: []string{"other-pack"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := censusFixture(t, "vsdd-factory", tc.registry, "")
			got := c.MarketplaceSiblings("claude-mp")
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("MarketplaceSiblings = %v, want %v", got, tc.want)
			}
		})
	}
}
