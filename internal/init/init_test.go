package init

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceConfigValue(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		key      string
		newValue string
		want     string
	}{
		{
			name:     "simple replacement",
			content:  "user_name: avi\nlanguage: English\n",
			key:      "user_name",
			newValue: "Michael",
			want:     "user_name: Michael\nlanguage: English\n",
		},
		{
			name:     "preserves other lines",
			content:  "# comment\nuser_name: avi\noutput: dir\n",
			key:      "user_name",
			newValue: "Test",
			want:     "# comment\nuser_name: Test\noutput: dir\n",
		},
		{
			name:     "replaces all occurrences",
			content:  "user_name: avi\n# Core\nuser_name: avi\n",
			key:      "user_name",
			newValue: "New",
			want:     "user_name: New\n# Core\nuser_name: New\n",
		},
		{
			name:     "no match leaves content unchanged",
			content:  "language: English\n",
			key:      "user_name",
			newValue: "Test",
			want:     "language: English\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceConfigValue(tt.content, tt.key, tt.newValue)
			if got != tt.want {
				t.Errorf("replaceConfigValue() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestDiscoverModules(t *testing.T) {
	packDir := t.TempDir()

	// Create module directories with config.yaml
	for _, mod := range []string{"core", "bmm", "tea"} {
		dir := filepath.Join(packDir, mod)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("user_name: test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create directories that should be skipped
	for _, skip := range []string{"commands", "_config", ".git"} {
		if err := os.MkdirAll(filepath.Join(packDir, skip), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create a directory without config.yaml (should be skipped)
	if err := os.MkdirAll(filepath.Join(packDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}

	modules, err := discoverModules(packDir)
	if err != nil {
		t.Fatalf("discoverModules() error: %v", err)
	}

	if len(modules) != 3 {
		t.Fatalf("expected 3 modules, got %d", len(modules))
	}

	names := make(map[string]bool)
	for _, m := range modules {
		names[m.code] = true
	}
	for _, want := range []string{"core", "bmm", "tea"} {
		if !names[want] {
			t.Errorf("missing module %q", want)
		}
	}
}

func TestRun_CreatesConfigs(t *testing.T) {
	// Set up a fake sideshow home with a pack
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)

	// Create pack structure
	packDir := filepath.Join(home, "packs", "bmad", "1.0.0")
	for _, mod := range []string{"core", "bmm"} {
		dir := filepath.Join(packDir, mod)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		config := "user_name: default\ncommunication_language: English\n"
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create current symlink
	currentLink := filepath.Join(home, "packs", "bmad", "current")
	if err := os.Symlink("1.0.0", currentLink); err != nil {
		t.Fatal(err)
	}

	// Create registry
	registry := "packs:\n  - name: bmad\n    version: \"1.0.0\"\n    path: " + currentLink + "\n"
	if err := os.WriteFile(filepath.Join(home, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run init
	projectDir := t.TempDir()
	if err := Run(projectDir, "Michael"); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify core config
	coreConfig := filepath.Join(projectDir, "_bmad", "core", "config.yaml")
	data, err := os.ReadFile(coreConfig)
	if err != nil {
		t.Fatalf("ReadFile(core/config.yaml) error: %v", err)
	}
	if !strings.Contains(string(data), "user_name: Michael") {
		t.Errorf("core config doesn't contain user_name: Michael, got:\n%s", data)
	}

	// Verify bmm config
	bmmConfig := filepath.Join(projectDir, "_bmad", "bmm", "config.yaml")
	data, err = os.ReadFile(bmmConfig)
	if err != nil {
		t.Fatalf("ReadFile(bmm/config.yaml) error: %v", err)
	}
	if !strings.Contains(string(data), "user_name: Michael") {
		t.Errorf("bmm config doesn't contain user_name: Michael, got:\n%s", data)
	}

	// Verify _bmad-output created
	outputDir := filepath.Join(projectDir, "_bmad-output")
	info, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("_bmad-output not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("_bmad-output is not a directory")
	}
}

func TestRun_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)

	// Create pack
	packDir := filepath.Join(home, "packs", "bmad", "1.0.0", "core")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "config.yaml"), []byte("user_name: default\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentLink := filepath.Join(home, "packs", "bmad", "current")
	if err := os.Symlink("1.0.0", currentLink); err != nil {
		t.Fatal(err)
	}
	registry := "packs:\n  - name: bmad\n    version: \"1.0.0\"\n    path: " + currentLink + "\n"
	if err := os.WriteFile(filepath.Join(home, "registry.yaml"), []byte(registry), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := t.TempDir()

	// First run
	if err := Run(projectDir, "First"); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}

	// Second run — should not overwrite
	if err := Run(projectDir, "Second"); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	// Verify original value preserved
	data, err := os.ReadFile(filepath.Join(projectDir, "_bmad", "core", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "user_name: First") {
		t.Errorf("config was overwritten on second run, got:\n%s", data)
	}
}
