package bindings

import (
	"path/filepath"
	"testing"
)

// shippedPack lays out a pack that ships one skill and one command.
func shippedPack(t *testing.T, skill, command string) string {
	t.Helper()
	p := t.TempDir()
	writeFile(t, filepath.Join(p, ".claude", "skills", skill, "SKILL.md"), "x")
	writeFile(t, filepath.Join(p, "commands", command), "x")
	return p
}

// Two packs that ship the same canonical ids must not cross-count: status
// reads "available == synced" as fully wired, so a pack that never synced
// has to read zero even when another pack's copies sit at the targets
// (aae-orc-zwx4b).
func TestSyncedCount_CountsOnlyBindingsThisPackWrote(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	a := shippedPack(t, "shared-skill", "shared.md")
	b := shippedPack(t, "shared-skill", "shared.md")

	skill := filepath.Join(home, ".claude", "skills", "shared-skill")
	cmd := filepath.Join(home, ".claude", "commands", "shared.md")
	writeFile(t, filepath.Join(skill, "SKILL.md"), "synced")
	writeFile(t, cmd, "synced")
	if err := saveManifest([]ManifestEntry{
		{Pack: "alpha", Version: "1", Kind: "skill-dir", Path: skill},
		{Pack: "alpha", Version: "1", Kind: "markdown-command", Path: cmd},
	}); err != nil {
		t.Fatal(err)
	}

	gotA, err := SyncedCount("alpha", a)
	if err != nil {
		t.Fatal(err)
	}
	if gotA != 2 {
		t.Errorf("SyncedCount(alpha) = %d, want 2 (it wrote both)", gotA)
	}
	gotB, err := SyncedCount("beta", b)
	if err != nil {
		t.Fatal(err)
	}
	if gotB != 0 {
		t.Errorf("SyncedCount(beta) = %d, want 0 (alpha wrote those copies)", gotB)
	}
}

// A recorded binding whose file has since been deleted is not synced.
func TestSyncedCount_RecordedButMissingIsNotSynced(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	a := shippedPack(t, "only-skill", "only.md")
	if err := saveManifest([]ManifestEntry{
		{Pack: "alpha", Version: "1", Kind: "skill-dir", Path: filepath.Join(home, ".claude", "skills", "only-skill")},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := SyncedCount("alpha", a)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("SyncedCount = %d, want 0 (nothing on disk)", got)
	}
}
