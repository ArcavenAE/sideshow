package pack

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSideshowDir_Default(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", "")
	dir := SideshowDir()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "sideshow")
	if dir != want {
		t.Errorf("SideshowDir() = %q, want %q", dir, want)
	}
}

func TestSideshowDir_Override(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", "/tmp/test-sideshow")
	dir := SideshowDir()
	if dir != "/tmp/test-sideshow" {
		t.Errorf("SideshowDir() = %q, want /tmp/test-sideshow", dir)
	}
}

func TestPacksDir(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", "/tmp/test-sideshow")
	dir := PacksDir()
	if dir != "/tmp/test-sideshow/packs" {
		t.Errorf("PacksDir() = %q, want /tmp/test-sideshow/packs", dir)
	}
}

func TestLoadRegistry_Empty(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	if len(reg.Packs) != 0 {
		t.Errorf("expected empty registry, got %d packs", len(reg.Packs))
	}
}

func TestRegistrySaveAndLoad(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	reg := &Registry{
		Packs: []InstalledPack{
			{Name: "bmad", Version: "6.2.2", Path: "/tmp/packs/bmad/current"},
		},
	}

	if err := reg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}

	if len(loaded.Packs) != 1 {
		t.Fatalf("expected 1 pack, got %d", len(loaded.Packs))
	}
	if loaded.Packs[0].Name != "bmad" {
		t.Errorf("pack name = %q, want bmad", loaded.Packs[0].Name)
	}
	if loaded.Packs[0].Version != "6.2.2" {
		t.Errorf("pack version = %q, want 6.2.2", loaded.Packs[0].Version)
	}
}

func TestDetectVersion_BmadManifest(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "_config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `installation:
  version: "6.2.2"
`
	if err := os.WriteFile(filepath.Join(configDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	version := DetectVersion(dir)
	if version != "6.2.2" {
		t.Errorf("DetectVersion() = %q, want 6.2.2", version)
	}
}

func TestDetectVersion_PackYaml(t *testing.T) {
	dir := t.TempDir()
	packYaml := `version: "1.0.0"
`
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(packYaml), 0o644); err != nil {
		t.Fatal(err)
	}

	version := DetectVersion(dir)
	if version != "1.0.0" {
		t.Errorf("DetectVersion() = %q, want 1.0.0", version)
	}
}

func TestDetectVersion_Unknown(t *testing.T) {
	dir := t.TempDir()
	version := DetectVersion(dir)
	if version != "unknown" {
		t.Errorf("DetectVersion() = %q, want unknown", version)
	}
}

func TestInstallFromLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)

	// Create a source pack with a manifest
	src := t.TempDir()
	configDir := filepath.Join(src, "_config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "manifest.yaml"), []byte("installation:\n  version: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallFromLocal("testpack", src); err != nil {
		t.Fatalf("InstallFromLocal() error: %v", err)
	}

	// Verify registry
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	if len(reg.Packs) != 1 {
		t.Fatalf("expected 1 pack, got %d", len(reg.Packs))
	}
	if reg.Packs[0].Name != "testpack" {
		t.Errorf("pack name = %q, want testpack", reg.Packs[0].Name)
	}

	// Verify files were copied
	readmePath := filepath.Join(home, "packs", "testpack", "1.0.0", "readme.txt")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error: %v", readmePath, err)
	}
	if string(data) != "hello" {
		t.Errorf("readme content = %q, want hello", string(data))
	}

	// Verify current symlink
	currentLink := filepath.Join(home, "packs", "testpack", "current")
	target, err := os.Readlink(currentLink)
	if err != nil {
		t.Fatalf("Readlink() error: %v", err)
	}
	if target != "1.0.0" {
		t.Errorf("current symlink = %q, want 1.0.0", target)
	}
}
