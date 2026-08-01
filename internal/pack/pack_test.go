package pack

import (
	"io/fs"
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
	freezeSafeHome(t)
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry() error: %v", err)
	}
	if len(reg.Packs) != 0 {
		t.Errorf("expected empty registry, got %d packs", len(reg.Packs))
	}
}

func TestRegistrySaveAndLoad(t *testing.T) {
	freezeSafeHome(t)

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

// freezeSafeHome sets SIDESHOW_HOME to a fresh temp dir and registers
// a cleanup that unfreezes any frozen store trees first, so TempDir's
// RemoveAll can delete them. Cleanups run LIFO: this one is registered
// after TempDir's, so it runs before it.
func freezeSafeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	t.Cleanup(func() { _ = UnfreezeTree(home) })
	return home
}

func TestInstallFromLocal(t *testing.T) {
	home := freezeSafeHome(t)

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

	if err := InstallFromLocal("testpack", src, true); err != nil {
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
	freezeSafeHome(t)

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

	err := InstallFromLocal("bmad", source, true)
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
	freezeSafeHome(t)

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

	if err := InstallFromLocal("bmad", source, true); err != nil {
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
	freezeSafeHome(t)

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

	if err := InstallFromLocal("bmad", source, true); err != nil {
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

// makeModeFixture creates a sideshow-native source pack containing one
// executable and one plain file, returning its path.
func makeModeFixture(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte("name: modes\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "tool.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "doc.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

// The mode contract after the store freeze (aae-orc-dihj): read and
// exec bits survive the copy, write bits are stripped. 0755 becomes
// 0555 and never 0444; 0644 becomes 0444.
func TestInstallFromLocal_PreservesFileModes(t *testing.T) {
	home := freezeSafeHome(t)
	src := makeModeFixture(t)

	if err := InstallFromLocal("modes", src, true); err != nil {
		t.Fatalf("InstallFromLocal: %v", err)
	}

	packRoot := filepath.Join(home, "packs", "modes", "1.0.0")
	tests := []struct {
		rel  string
		want os.FileMode
	}{
		{filepath.Join("bin", "tool.sh"), 0o555},
		{"doc.md", 0o444},
	}
	for _, tt := range tests {
		info, err := os.Stat(filepath.Join(packRoot, tt.rel))
		if err != nil {
			t.Fatalf("stat %s: %v", tt.rel, err)
		}
		if got := info.Mode().Perm(); got != tt.want {
			t.Errorf("%s mode = %o, want %o", tt.rel, got, tt.want)
		}
	}
}

func TestInstallFromLocal_ExecManifestVerified(t *testing.T) {
	freezeSafeHome(t)
	src := makeModeFixture(t)
	if err := os.WriteFile(filepath.Join(src, "exec-manifest.txt"), []byte("bin/tool.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallFromLocal("modes", src, true); err != nil {
		t.Fatalf("InstallFromLocal with satisfied exec-manifest: %v", err)
	}
}

func TestInstallFromLocal_ExecManifestDriftFailsLoudly(t *testing.T) {
	freezeSafeHome(t)
	src := makeModeFixture(t)
	// Census references an entry the source does not satisfy: the listed
	// path exists but is not executable, plus one missing path.
	if err := os.Chmod(filepath.Join(src, "bin", "tool.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "exec-manifest.txt"), []byte("bin/tool.sh\nbin/absent.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := InstallFromLocal("modes", src, true)
	if err == nil {
		t.Fatal("InstallFromLocal succeeded despite exec-manifest drift")
	}
	for _, want := range []string{"exec-manifest.txt", "bin/tool.sh (not executable)", "bin/absent.sh (missing)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

// walkPerms collects perm bits for every non-symlink entry under root.
func walkPerms(t *testing.T, root string) map[string]os.FileMode {
	t.Helper()
	perms := map[string]os.FileMode{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		perms[rel] = info.Mode().Perm()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return perms
}

// The store freezes WHOLE (finding-094 round 3): every file and every
// directory in the installed tree, no exemptions.
func TestInstallFromLocal_FreezesStoreWhole(t *testing.T) {
	home := freezeSafeHome(t)
	src := makeModeFixture(t)

	if err := InstallFromLocal("modes", src, true); err != nil {
		t.Fatalf("InstallFromLocal: %v", err)
	}

	for rel, perm := range walkPerms(t, filepath.Join(home, "packs", "modes", "1.0.0")) {
		if perm&0o222 != 0 {
			t.Errorf("%s carries write bits: %o", rel, perm)
		}
	}
}

// A write into the frozen store fails loudly instead of corrupting the
// active version silently (the finding-074 class).
func TestInstallFromLocal_StoreWriteFailsLoudly(t *testing.T) {
	home := freezeSafeHome(t)
	src := makeModeFixture(t)

	if err := InstallFromLocal("modes", src, true); err != nil {
		t.Fatalf("InstallFromLocal: %v", err)
	}

	packRoot := filepath.Join(home, "packs", "modes", "1.0.0")
	if err := os.WriteFile(filepath.Join(packRoot, "doc.md"), []byte("mutated"), 0o644); err == nil {
		t.Error("overwrite of a frozen store file succeeded")
	}
	if err := os.WriteFile(filepath.Join(packRoot, "bin", "new.sh"), []byte("#!/bin/sh\n"), 0o755); err == nil {
		t.Error("creating a file inside a frozen store dir succeeded")
	}
	if err := os.Remove(filepath.Join(packRoot, "doc.md")); err == nil {
		t.Error("deleting a file from a frozen store dir succeeded")
	}
}

// Reinstalling over a frozen version is the unlock half of the
// envelope: before the fix this EACCESed on the first overwrite, which
// is the state every manually frozen store has been in since the
// 2026-07-12 chmod (finding-074 interim).
func TestInstallFromLocal_ReinstallOverFrozenTree(t *testing.T) {
	home := freezeSafeHome(t)
	src := makeModeFixture(t)

	if err := InstallFromLocal("modes", src, true); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "doc.md"), []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InstallFromLocal("modes", src, true); err != nil {
		t.Fatalf("reinstall over frozen tree: %v", err)
	}

	packRoot := filepath.Join(home, "packs", "modes", "1.0.0")
	data, err := os.ReadFile(filepath.Join(packRoot, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "updated" {
		t.Errorf("reinstall did not replace content: %q", data)
	}
	for rel, perm := range walkPerms(t, packRoot) {
		if perm&0o222 != 0 {
			t.Errorf("%s writable after reinstall: %o", rel, perm)
		}
	}
}

// A failed install leaves the tree frozen, not writable: the deferred
// freeze runs on every exit (fail frozen).
func TestInstallFromLocal_FreezesEvenOnFailure(t *testing.T) {
	home := freezeSafeHome(t)
	src := makeModeFixture(t)
	// Exec-manifest drift makes the install fail after the copy.
	if err := os.Chmod(filepath.Join(src, "bin", "tool.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "exec-manifest.txt"), []byte("bin/tool.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallFromLocal("modes", src, true); err == nil {
		t.Fatal("install succeeded despite exec-manifest drift")
	}
	for rel, perm := range walkPerms(t, filepath.Join(home, "packs", "modes", "1.0.0")) {
		if perm&0o222 != 0 {
			t.Errorf("failed install left %s writable: %o", rel, perm)
		}
	}
}

// Chmod follows symlinks; the freeze must not. A bridge link inside
// the tree may point at user-owned content outside the store.
func TestFreezeTree_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	tree := t.TempDir()
	t.Cleanup(func() { _ = UnfreezeTree(tree) })
	outside := filepath.Join(t.TempDir(), "user-owned.md")
	if err := os.WriteFile(outside, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tree, "bridge")); err != nil {
		t.Fatal(err)
	}

	if err := FreezeTree(tree); err != nil {
		t.Fatalf("FreezeTree: %v", err)
	}
	info, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("freeze reached through the symlink: outside target mode = %o, want 644", info.Mode().Perm())
	}
}

func TestUnfreezeTree_RestoresOwnerWrite(t *testing.T) {
	t.Parallel()
	tree := t.TempDir()
	sub := filepath.Join(tree, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := FreezeTree(tree); err != nil {
		t.Fatal(err)
	}
	if err := UnfreezeTree(tree); err != nil {
		t.Fatalf("UnfreezeTree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "f.txt"), []byte("y"), 0o644); err != nil {
		t.Errorf("write after unfreeze failed: %v", err)
	}
}
