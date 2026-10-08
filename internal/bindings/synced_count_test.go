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

// When two packs ship the same id, one sync writes the later pack's copy
// over the earlier one and the manifest records both entries in sync
// order. Only the last recorded writer of a path owns what is on disk, so
// only it counts that path as synced.
func TestSyncedCount_SharedPathCreditsOnlyTheLastRecordedWriter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	a := shippedPack(t, "shared-skill", "shared.md")
	b := shippedPack(t, "shared-skill", "shared.md")
	writeFile(t, filepath.Join(a, ".claude", "skills", "shared-skill", "SKILL.md"), "alpha text")
	writeFile(t, filepath.Join(b, ".claude", "skills", "shared-skill", "SKILL.md"), "beta text")

	skill := filepath.Join(home, ".claude", "skills", "shared-skill")
	cmd := filepath.Join(home, ".claude", "commands", "shared.md")
	// Disk holds beta's copies: beta synced after alpha.
	writeFile(t, filepath.Join(skill, "SKILL.md"), "beta text")
	writeFile(t, cmd, "beta text")

	record := func(order ...string) {
		t.Helper()
		var entries []ManifestEntry
		for _, p := range order {
			entries = append(entries,
				ManifestEntry{Pack: p, Version: "1", Kind: "skill-dir", Path: skill},
				ManifestEntry{Pack: p, Version: "1", Kind: "markdown-command", Path: cmd})
		}
		if err := saveManifest(entries); err != nil {
			t.Fatal(err)
		}
	}
	count := func(name, path string) int {
		t.Helper()
		n, err := SyncedCount(name, path)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	record("alpha", "beta")
	if got := count("alpha", a); got != 0 {
		t.Errorf("SyncedCount(alpha) = %d, want 0 (beta wrote last)", got)
	}
	if got := count("beta", b); got != 2 {
		t.Errorf("SyncedCount(beta) = %d, want 2", got)
	}

	record("beta", "alpha")
	if got := count("alpha", a); got != 2 {
		t.Errorf("SyncedCount(alpha) after reorder = %d, want 2 (alpha wrote last)", got)
	}
	if got := count("beta", b); got != 0 {
		t.Errorf("SyncedCount(beta) after reorder = %d, want 0", got)
	}
}

// A skill-dir record must not credit a command of the same name, nor the
// reverse: each kind feeds only its own set.
func TestSyncedCount_KindsDoNotCrossCredit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	// The pack ships a skill directory and a command with the same name.
	p := t.TempDir()
	writeFile(t, filepath.Join(p, ".claude", "skills", "dual.md", "SKILL.md"), "x")
	writeFile(t, filepath.Join(p, "commands", "dual.md"), "x")
	writeFile(t, filepath.Join(home, ".claude", "skills", "dual.md", "SKILL.md"), "synced")
	writeFile(t, filepath.Join(home, ".claude", "commands", "dual.md"), "synced")

	if err := saveManifest([]ManifestEntry{
		{Pack: "alpha", Version: "1", Kind: "skill-dir", Path: filepath.Join(home, ".claude", "skills", "dual.md")},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := SyncedCount("alpha", p)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("SyncedCount = %d, want 1 (only the recorded skill, not the unrecorded command)", got)
	}
}
