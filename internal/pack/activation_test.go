package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadActivation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string // empty means no pack.yaml at all
		want    *Activation
		wantErr bool
		wantNil bool
		hasYAML bool
	}{
		{
			name:    "full block",
			hasYAML: true,
			yaml: "name: vsdd-factory\nversion: \"1.0.0-rc.23\"\n" +
				"activation:\n  default_scope: per-repo\n  per_repo_required: true\n" +
				"  mechanism: claude-plugin\n  runbook: https://example.com/runbook\n" +
				"  validated_harness_floor: \"claude-code 2.1.220\"\n",
			want: &Activation{
				DefaultScope:          "per-repo",
				PerRepoRequired:       true,
				Mechanism:             "claude-plugin",
				Runbook:               "https://example.com/runbook",
				ValidatedHarnessFloor: "claude-code 2.1.220",
			},
		},
		{
			name:    "no activation block",
			hasYAML: true,
			yaml:    "name: plain\nversion: \"1.0.0\"\n",
			wantNil: true,
		},
		{
			name:    "no pack.yaml",
			wantNil: true,
		},
		{
			name:    "malformed yaml",
			hasYAML: true,
			yaml:    "activation: [not: a: map\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if tt.hasYAML {
				if err := os.WriteFile(filepath.Join(root, "pack.yaml"), []byte(tt.yaml), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := LoadActivation(root)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadActivation: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil activation, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected activation, got nil")
			}
			if *got != *tt.want {
				t.Errorf("activation = %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

func TestActivationPluginClass(t *testing.T) {
	t.Parallel()
	var nilAct *Activation
	if nilAct.PluginClass() {
		t.Error("nil activation must not be plugin-class")
	}
	if (&Activation{}).PluginClass() {
		t.Error("empty mechanism must not be plugin-class")
	}
	if !(&Activation{Mechanism: "claude-plugin"}).PluginClass() {
		t.Error("claude-plugin mechanism must be plugin-class")
	}
}

// captureStdout runs fn with os.Stdout redirected and returns what it
// printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

func TestInstallFromLocal_PluginClassNoticeReplacesSyncHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	src := t.TempDir()
	packYAML := "name: vsdd-factory\nversion: \"1.0.0-rc.23\"\n" +
		"activation:\n  default_scope: per-repo\n  per_repo_required: true\n" +
		"  mechanism: claude-plugin\n  runbook: https://example.com/runbook\n"
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte(packYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := InstallFromLocal("vsdd-factory", src, true); err != nil {
			t.Errorf("InstallFromLocal: %v", err)
		}
	})

	for _, want := range []string{
		"activates via \"claude-plugin\"",
		"nothing is enabled",
		"per-repo only",
		"https://example.com/runbook",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("install output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Run 'sideshow commands sync'") {
		t.Errorf("sync hint must be suppressed for plugin-class packs:\n%s", out)
	}
	if strings.Contains(out, "WARNING") {
		t.Errorf("recognized mechanism must not warn:\n%s", out)
	}
}

func TestInstallFromLocal_UnknownMechanismWarns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	src := t.TempDir()
	packYAML := "name: mystery\nversion: \"1.0.0\"\n" +
		"activation:\n  mechanism: quantum-entangle\n"
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte(packYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := InstallFromLocal("mystery", src, true); err != nil {
			t.Errorf("InstallFromLocal: %v", err)
		}
	})
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "quantum-entangle") {
		t.Errorf("unknown mechanism must warn by name:\n%s", out)
	}
}
