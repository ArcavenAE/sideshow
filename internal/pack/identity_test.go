package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeNativePack creates a sideshow-native pack source whose pack.yaml
// carries the given body, plus one content file.
func writeNativePack(t *testing.T, packYaml string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte(packYaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

// aae-orc-oihza / aae-orc-cv1b6: bmad content installed under the name
// spectacle must be refused, and nothing may land in the store.
func TestInstallFromLocal_RefusesNameMismatch(t *testing.T) {
	home := freezeSafeHome(t)
	src := writeNativePack(t, "name: bmad\nversion: 6.12.0\nschema_version: 0.1.0\n")

	err := InstallFromLocal("spectacle", src, true)
	if err == nil {
		t.Fatal("InstallFromLocal(spectacle, <bmad pack>) succeeded; want a name-mismatch error")
	}
	for _, want := range []string{`"spectacle"`, `"bmad"`, "pack.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
	if _, statErr := os.Stat(filepath.Join(home, "packs", "spectacle")); !os.IsNotExist(statErr) {
		t.Errorf("store has packs/spectacle after a refused install (stat err: %v)", statErr)
	}
	reg, rerr := LoadRegistry()
	if rerr != nil {
		t.Fatal(rerr)
	}
	if len(reg.Packs) != 0 {
		t.Errorf("registry has %d packs after a refused install, want 0", len(reg.Packs))
	}
}

func TestInstallFromLocal_AcceptsMatchingName(t *testing.T) {
	home := freezeSafeHome(t)
	src := writeNativePack(t, "name: bmad\nversion: 6.12.0\n")

	if err := InstallFromLocal("bmad", src, true); err != nil {
		t.Fatalf("InstallFromLocal(bmad, <bmad pack>) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "packs", "bmad", "6.12.0", "readme.txt")); err != nil {
		t.Errorf("installed content missing: %v", err)
	}
}

// A pack.yaml without a name cannot establish identity; today's
// behaviour (caller-supplied name) stands so the guard is not an
// accidental gate on older packs.
func TestInstallFromLocal_PackYamlWithoutNameKeepsCallerName(t *testing.T) {
	home := freezeSafeHome(t)
	src := writeNativePack(t, "version: 1.0.0\n")

	if err := InstallFromLocal("custom", src, true); err != nil {
		t.Fatalf("InstallFromLocal(custom, <nameless pack.yaml>) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "packs", "custom", "1.0.0")); err != nil {
		t.Errorf("install missing: %v", err)
	}
}

// Installer output under _bmad/ with its pack.yaml alongside the
// manifest: the name is still checked.
func TestInstallFromLocal_RefusesNameMismatchInSiblingLayout(t *testing.T) {
	home := freezeSafeHome(t)
	src := t.TempDir()
	cfg := filepath.Join(src, "_bmad", "_config")
	if err := os.MkdirAll(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "manifest.yaml"), []byte("installation:\n  version: \"6.12.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "_bmad", "pack.yaml"), []byte("name: bmad\nversion: 6.12.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallFromLocal("spectacle", src, true); err == nil {
		t.Fatal("sibling-layout install under the wrong name succeeded; want a name-mismatch error")
	}
	if _, statErr := os.Stat(filepath.Join(home, "packs", "spectacle")); !os.IsNotExist(statErr) {
		t.Errorf("store has packs/spectacle after a refused install (stat err: %v)", statErr)
	}
}

// A pack.yaml that exists but will not parse cannot vouch for any name.
func TestInstallFromLocal_RefusesUnparseablePackYaml(t *testing.T) {
	freezeSafeHome(t)
	src := writeNativePack(t, "name: [unclosed\n")

	err := InstallFromLocal("bmad", src, true)
	if err == nil {
		t.Fatal("install with an unparseable pack.yaml succeeded; want an error")
	}
	if !strings.Contains(err.Error(), "pack.yaml") {
		t.Errorf("error %q does not name pack.yaml", err)
	}
}
