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

// writeInstallerOutput creates an installer-layout source: the unified
// _config/manifest.yaml, no pack.yaml, so nothing in it names the pack.
func writeInstallerOutput(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "_config", "manifest.yaml"), []byte("installation:\n  version: \"6.12.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

// An installer-layout source declares no pack name, so the caller's name
// stands. That is by design, but the install says so rather than staying
// silent, so a mislabelled install is visible (aae-orc-cv1b6).
func TestInstallFromLocal_InstallerLayoutSaysNoNameWasDeclared(t *testing.T) {
	freezeSafeHome(t)
	src := writeInstallerOutput(t)

	out := captureStdout(t, func() {
		if err := InstallFromLocal("spectacle", src, true); err != nil {
			t.Errorf("InstallFromLocal: %v", err)
		}
	})
	want := "the source declares no pack name (installer layout); installed as spectacle as given"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

// A pack.yaml with no name field is the same case with its own reason.
func TestInstallFromLocal_NamelessPackYamlSaysNoNameWasDeclared(t *testing.T) {
	freezeSafeHome(t)
	src := writeNativePack(t, "version: 1.0.0\n")

	out := captureStdout(t, func() {
		if err := InstallFromLocal("custom", src, true); err != nil {
			t.Errorf("InstallFromLocal: %v", err)
		}
	})
	want := "the source declares no pack name (pack.yaml has no name); installed as custom as given"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

// A source that names itself, and the name matches, prints no notice.
func TestInstallFromLocal_DeclaredNamePrintsNoNotice(t *testing.T) {
	freezeSafeHome(t)
	src := writeNativePack(t, "name: demo\nversion: 1.0.0\n")

	out := captureStdout(t, func() {
		if err := InstallFromLocal("demo", src, true); err != nil {
			t.Errorf("InstallFromLocal: %v", err)
		}
	})
	if strings.Contains(out, "declares no pack name") {
		t.Errorf("a declared name printed the notice:\n%s", out)
	}
}

// A later install with --no-activate returns before the activation tail, so
// the note has to be printed before that return too: the doc comment on
// ValidateName promises the install is never silent about an unnamed source.
func TestInstallFromLocal_NoActivateLaterInstallStillSaysNoNameWasDeclared(t *testing.T) {
	freezeSafeHome(t)
	first := writeInstallerOutput(t)
	if err := InstallFromLocal("spectacle", first, true); err != nil {
		t.Fatalf("first install: %v", err)
	}
	second := writeInstallerOutput(t)
	if err := os.WriteFile(filepath.Join(second, "_config", "manifest.yaml"), []byte("installation:\n  version: \"6.13.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if err := InstallFromLocal("spectacle", second, false); err != nil {
			t.Errorf("second install: %v", err)
		}
	})
	want := "the source declares no pack name (installer layout); installed as spectacle as given"
	if !strings.Contains(out, want) {
		t.Errorf("later --no-activate install lacks %q:\n%s", want, out)
	}
	if !strings.Contains(out, "Not activated") {
		t.Errorf("later --no-activate install lacks the Not activated line:\n%s", out)
	}
}
