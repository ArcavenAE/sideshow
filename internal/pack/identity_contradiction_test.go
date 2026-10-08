package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBmadPackYaml adds _bmad/pack.yaml with the given body to a source.
func writeBmadPackYaml(t *testing.T, src, body string) {
	t.Helper()
	dir := filepath.Join(src, "_bmad")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A source whose pack.yaml and _bmad/pack.yaml declare different names is
// malformed, so install refuses under either name, names both files and
// both names, and stores nothing (sideshow#132).
func TestInstallFromLocal_RefusesContradictoryDeclarations(t *testing.T) {
	for _, asName := range []string{"bmad", "spectacle", "other"} {
		t.Run(asName, func(t *testing.T) {
			home := freezeSafeHome(t)
			src := writeNativePack(t, "name: bmad\nversion: 1.0.0\n")
			writeBmadPackYaml(t, src, "name: spectacle\nversion: 1.0.0\n")

			err := InstallFromLocal(asName, src, true)
			if err == nil {
				t.Fatalf("install as %q accepted a source declaring two names", asName)
			}
			for _, want := range []string{
				filepath.Join(src, "pack.yaml"), filepath.Join(src, "_bmad", "pack.yaml"),
				`"bmad"`, `"spectacle"`,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %s", err, want)
				}
			}
			if _, statErr := os.Stat(filepath.Join(home, "packs", asName)); !os.IsNotExist(statErr) {
				t.Errorf("store has packs/%s after a refused install (stat err: %v)", asName, statErr)
			}
		})
	}
}

// Controls: the same name in both files, or a name in only one, still
// installs; a second file with no name is not a contradiction.
func TestInstallFromLocal_AgreeingOrPartialDeclarationsStillInstall(t *testing.T) {
	for name, second := range map[string]string{
		"same name":       "name: bmad\nversion: 1.0.0\n",
		"second no name":  "version: 1.0.0\n",
		"second is empty": "",
	} {
		t.Run(name, func(t *testing.T) {
			freezeSafeHome(t)
			src := writeNativePack(t, "name: bmad\nversion: 1.0.0\n")
			writeBmadPackYaml(t, src, second)
			if err := InstallFromLocal("bmad", src, true); err != nil {
				t.Fatalf("install: %v", err)
			}
		})
	}
}
