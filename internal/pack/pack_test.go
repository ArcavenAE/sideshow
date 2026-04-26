package pack

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDetectVersion_BmadManifest_DirectChild(t *testing.T) {
	t.Parallel()
	// --from points at the _bmad/ dir itself: _config/manifest.yaml is a direct child
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

func TestDetectVersion_BmadManifest_ProjectRoot(t *testing.T) {
	t.Parallel()
	// --from points at the project root: _bmad/_config/manifest.yaml
	dir := t.TempDir()
	configDir := filepath.Join(dir, "_bmad", "_config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `installation:
  version: "6.3.0"
`
	if err := os.WriteFile(filepath.Join(configDir, "manifest.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	version := DetectVersion(dir)
	if version != "6.3.0" {
		t.Errorf("DetectVersion() = %q, want 6.3.0", version)
	}
}

func TestDetectVersion_BmadManifest_DirectChildWins(t *testing.T) {
	t.Parallel()
	// If both paths exist, direct child (_config/) wins over nested (_bmad/_config/)
	dir := t.TempDir()

	directConfig := filepath.Join(dir, "_config")
	if err := os.MkdirAll(directConfig, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directConfig, "manifest.yaml"),
		[]byte("installation:\n  version: \"6.2.2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	nestedConfig := filepath.Join(dir, "_bmad", "_config")
	if err := os.MkdirAll(nestedConfig, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedConfig, "manifest.yaml"),
		[]byte("installation:\n  version: \"6.3.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	version := DetectVersion(dir)
	if version != "6.2.2" {
		t.Errorf("DetectVersion() = %q, want 6.2.2 (direct child should win)", version)
	}
}

func TestDetectVersion_PackageJSON(t *testing.T) {
	t.Parallel()
	// --from points at the BMAD source repo: package.json at root
	dir := t.TempDir()
	pkg := `{"name": "bmad-method", "version": "6.0.1"}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}

	version := DetectVersion(dir)
	if version != "6.0.1" {
		t.Errorf("DetectVersion() = %q, want 6.0.1", version)
	}
}

func TestDetectVersion_BmadManifest_BeatsPackageJSON(t *testing.T) {
	t.Parallel()
	// If both manifest.yaml and package.json exist, manifest wins
	dir := t.TempDir()

	configDir := filepath.Join(dir, "_config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "manifest.yaml"),
		[]byte("installation:\n  version: \"6.2.2\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"version": "6.0.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	version := DetectVersion(dir)
	if version != "6.2.2" {
		t.Errorf("DetectVersion() = %q, want 6.2.2 (manifest should beat package.json)", version)
	}
}

func TestDetectVersion_PackYaml(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	dir := t.TempDir()
	version := DetectVersion(dir)
	if version != "unknown" {
		t.Errorf("DetectVersion() = %q, want unknown", version)
	}
}

func TestDetectVersion_MalformedYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configDir := filepath.Join(dir, "_config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "manifest.yaml"),
		[]byte("not: [valid: yaml: {"), 0o644); err != nil {
		t.Fatal(err)
	}

	version := DetectVersion(dir)
	if version != "unknown" {
		t.Errorf("DetectVersion() = %q, want unknown (malformed YAML)", version)
	}
}

func TestDetectVersion_MalformedJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	version := DetectVersion(dir)
	if version != "unknown" {
		t.Errorf("DetectVersion() = %q, want unknown (malformed JSON)", version)
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

func TestValidateShape_AcceptsBmadInstallerOutputDirectChild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "_config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "manifest.yaml"), []byte("version: 6.3.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateShape(dir); err != nil {
		t.Fatalf("ValidateShape rejected installer-output layout: %v", err)
	}
}

func TestValidateShape_AcceptsBmadInstallerOutputNestedPrefix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "_bmad", "_config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "manifest.yaml"), []byte("version: 6.3.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateShape(dir); err != nil {
		t.Fatalf("ValidateShape rejected nested _bmad/ layout: %v", err)
	}
}

func TestValidateShape_AcceptsSideshowNativePack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte("name: foo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateShape(dir); err != nil {
		t.Fatalf("ValidateShape rejected sideshow-native pack: %v", err)
	}
}

func TestValidateShape_RejectsUpstreamSourceTarball(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Mimic bmad-method-6.5.0.tgz extracted: package.json + src/ + tools/
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"version":"6.5.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := ValidateShape(dir)
	if err == nil {
		t.Fatal("ValidateShape accepted upstream source tarball; expected rejection")
	}
	if !strings.Contains(err.Error(), "upstream npm source tarball") {
		t.Fatalf("expected 'upstream npm source tarball' in error, got: %v", err)
	}
}

func TestValidateShape_RejectsUnknownLayout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateShape(dir)
	if err == nil {
		t.Fatal("ValidateShape accepted unrecognized layout; expected rejection")
	}
	if !strings.Contains(err.Error(), "does not look like a sideshow-installable pack") {
		t.Fatalf("expected generic 'does not look like' error, got: %v", err)
	}
}

func TestInstallFromLocal_RejectsSourceTarballShape(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "package.json"), []byte(`{"version":"6.5.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := InstallFromLocal("bmad", source)
	if err == nil {
		t.Fatal("InstallFromLocal accepted upstream source tarball; expected rejection")
	}
	if !strings.Contains(err.Error(), "upstream npm source tarball") {
		t.Fatalf("expected upstream-source-rejection in error, got: %v", err)
	}
}

func TestHasInstallerSiblingLayout_Detects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// _bmad/_config/manifest.yaml + .claude/skills sibling
	cfg := filepath.Join(dir, "_bmad", "_config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "manifest.yaml"), []byte("version: 6.5.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasInstallerSiblingLayout(dir) {
		t.Errorf("hasInstallerSiblingLayout = false for canonical sibling layout")
	}
}

func TestHasInstallerSiblingLayout_RequiresClaudeSibling(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "_bmad", "_config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "manifest.yaml"), []byte("version: 6.5.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No .claude sibling — the user pointed at a project root that doesn't have IDE bindings.
	if hasInstallerSiblingLayout(dir) {
		t.Errorf("hasInstallerSiblingLayout = true without .claude/ sibling")
	}
}

func TestHasInstallerSiblingLayout_AlreadyUnified(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pack has both _bmad/_config AND _config — already unified, don't strip.
	if err := os.MkdirAll(filepath.Join(dir, "_bmad", "_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_bmad", "_config", "manifest.yaml"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_config", "manifest.yaml"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if hasInstallerSiblingLayout(dir) {
		t.Errorf("hasInstallerSiblingLayout = true for already-unified pack (would re-strip)")
	}
}

func TestInstallFromLocal_UnifiesInstallerSiblingLayout(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	source := t.TempDir()
	// Build a realistic upstream installer output: _bmad/_config/manifest.yaml
	// + _bmad/core/something + _bmad/bmm/dir + .claude/skills/bmad-help/SKILL.md
	if err := os.MkdirAll(filepath.Join(source, "_bmad", "_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "_bmad", "_config", "manifest.yaml"),
		[]byte("installation:\n  version: 6.5.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "_bmad", "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "_bmad", "core", "config.yaml"),
		[]byte("module: core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, ".claude", "skills", "bmad-help"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".claude", "skills", "bmad-help", "SKILL.md"),
		[]byte("# bmad-help\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallFromLocal("bmad", source); err != nil {
		t.Fatalf("InstallFromLocal: %v", err)
	}

	// Verify the installed pack has _bmad/ stripped: _config/manifest.yaml at root,
	// core/config.yaml at root, .claude/skills/bmad-help/SKILL.md at root, and
	// NO _bmad/ directory in the destination.
	packRoot := filepath.Join(PacksDir(), "bmad", "6.5.0")

	tests := []struct {
		path  string
		isDir bool
		want  string
	}{
		{filepath.Join(packRoot, "_config", "manifest.yaml"), false, "installation:\n  version: 6.5.0\n"},
		{filepath.Join(packRoot, "core", "config.yaml"), false, "module: core\n"},
		{filepath.Join(packRoot, ".claude", "skills", "bmad-help", "SKILL.md"), false, "# bmad-help\n"},
	}
	for _, tc := range tests {
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Errorf("expected %s after unification: %v", tc.path, err)
			continue
		}
		if string(data) != tc.want {
			t.Errorf("%s content = %q, want %q", tc.path, data, tc.want)
		}
	}

	// _bmad/ must not exist at the destination — the prefix was stripped.
	if _, err := os.Stat(filepath.Join(packRoot, "_bmad")); !os.IsNotExist(err) {
		t.Errorf("_bmad/ leaked into destination; stat err = %v", err)
	}
}

func TestInstallFromLocal_AlreadyUnifiedPassesThrough(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	source := t.TempDir()
	// Pack root with _config/ at top level (no _bmad/ prefix at all).
	if err := os.MkdirAll(filepath.Join(source, "_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "_config", "manifest.yaml"),
		[]byte("installation:\n  version: 6.3.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "core", "config.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallFromLocal("bmad", source); err != nil {
		t.Fatalf("InstallFromLocal: %v", err)
	}

	packRoot := filepath.Join(PacksDir(), "bmad", "6.3.0")
	if _, err := os.Stat(filepath.Join(packRoot, "_config", "manifest.yaml")); err != nil {
		t.Errorf("expected _config/manifest.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(packRoot, "core", "config.yaml")); err != nil {
		t.Errorf("expected core/config.yaml: %v", err)
	}
}
