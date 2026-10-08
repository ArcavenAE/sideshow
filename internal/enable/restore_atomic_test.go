//go:build unix

package enable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/bindings"
)

// restoreScene plants a file that is exactly sideshow's rendering of
// nonCanonicalSettings, as a killed disable pass leaves it, with the
// original bytes on record. restoreOriginalSettings then restores.
func restoreScene(t *testing.T, dir string) (ledgerPath, settings string) {
	t.Helper()
	ledgerPath = filepath.Join(t.TempDir(), "repo-bindings.yaml")
	settings = filepath.Join(dir, "settings.local.json")
	var m map[string]any
	if err := json.Unmarshal([]byte(nonCanonicalSettings), &m); err != nil {
		t.Fatal(err)
	}
	rendered, err := bindings.RenderSettings(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, rendered, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecar(ledgerPath, "ref.orig", sha256Hex([]byte("enabled")), []byte(nonCanonicalSettings)); err != nil {
		t.Fatal(err)
	}
	return ledgerPath, settings
}

// A restore that cannot complete leaves the file as it was, not a torn
// one (sideshow#183). A directory that cannot take a new file makes the
// difference observable: an in-place write still succeeds into the file.
func TestRestoreOriginalSettings_AFailedRestoreLeavesThePreviousBytes(t *testing.T) {
	dir := t.TempDir()
	ledgerPath, settings := restoreScene(t, dir)
	before, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	out := captureOut(t, func() {
		restoreOriginalSettings(ledgerPath, settings, "ref.orig", sha256Hex(before), false)
	})
	after, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the file changed although the restore could not complete:\n%q", after)
	}
	if !strings.Contains(out, "could not be restored") {
		t.Errorf("missing the could-not-be-restored note:\n%s", out)
	}
}

// A restore through a symlink leaves the link and updates its target.
func TestRestoreOriginalSettings_RestoreThroughASymlinkUpdatesTheTarget(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ledgerPath, target := restoreScene(t, realDir)
	link := filepath.Join(dir, "settings.local.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(target)
	restoreOriginalSettings(ledgerPath, link, "ref.orig", sha256Hex(before), false)
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v, %v", info, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != nonCanonicalSettings {
		t.Errorf("target not restored: %q, %v", got, err)
	}
}

// A restore keeps the mode the file had.
func TestRestoreOriginalSettings_RestoreKeepsTheExistingMode(t *testing.T) {
	dir := t.TempDir()
	ledgerPath, settings := restoreScene(t, dir)
	if err := os.Chmod(settings, 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(settings)
	restoreOriginalSettings(ledgerPath, settings, "ref.orig", sha256Hex(before), false)
	if info, err := os.Stat(settings); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode after restore: %v, %v", info, err)
	}
	if got, _ := os.ReadFile(settings); string(got) != nonCanonicalSettings {
		t.Errorf("not restored: %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("stray files after the restore: %v", entries)
	}
}
