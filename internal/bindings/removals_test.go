package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSidesowOwned writes a SKILL.md (or generic md) at path with the
// fallback-resolution sentinel embedded — marking it as sideshow-managed.
func writeSideshowOwned(t *testing.T, path, body string) {
	t.Helper()
	content := body + "\n\n" + SideshowSentinel + "\nfooter\n<!-- sideshow:fallback-resolution:end -->\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeUserAuthored writes a file with no sentinel — simulating a user-
// authored skill or command sideshow must NOT touch.
func writeUserAuthored(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProcessRemovals_NoFile(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	got, err := ProcessRemovals(packPath)
	if err != nil {
		t.Fatalf("ProcessRemovals: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d removals, want 0 when removals.txt is absent", got)
	}
}

func TestProcessRemovals_RemovesManagedSkill(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(packPath, "removals.txt"),
		[]byte("# comments OK\n\nbmad-old-thing\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	writeSideshowOwned(t, filepath.Join(skillsDir, "bmad-old-thing", "SKILL.md"), "stale skill")

	got, err := ProcessRemovals(packPath)
	if err != nil {
		t.Fatalf("ProcessRemovals: %v", err)
	}
	if got != 1 {
		t.Errorf("got %d removals, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "bmad-old-thing")); !os.IsNotExist(err) {
		t.Errorf("expected bmad-old-thing/ to be deleted; stat err = %v", err)
	}
}

func TestProcessRemovals_LeavesUserAuthoredAlone(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(packPath, "removals.txt"),
		[]byte("bmad-handcrafted\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// User-authored skill at the same canonicalId — NO sentinel.
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	skillEntry := filepath.Join(skillsDir, "bmad-handcrafted", "SKILL.md")
	writeUserAuthored(t, skillEntry, "I wrote this myself")

	got, err := ProcessRemovals(packPath)
	if err != nil {
		t.Fatalf("ProcessRemovals: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d removals, want 0 (user-authored should be preserved)", got)
	}
	data, err := os.ReadFile(skillEntry)
	if err != nil {
		t.Fatalf("user-authored skill missing: %v", err)
	}
	if !strings.Contains(string(data), "I wrote this myself") {
		t.Errorf("user-authored content modified: %s", data)
	}
}

func TestProcessRemovals_DefersToCurrentlyShipped(t *testing.T) {
	// Defensive case: removals.txt names something the current pack
	// ALSO ships. Don't delete — pack is authority on what's current.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(packPath, "removals.txt"),
		[]byte("bmad-still-here\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	// Pack ships this canonicalId — it's NOT removed in this version.
	if err := os.MkdirAll(filepath.Join(packPath, ".claude", "skills", "bmad-still-here"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSideshowOwned(t, filepath.Join(packPath, ".claude", "skills", "bmad-still-here", "SKILL.md"), "x")

	// Target has it from a prior sync.
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	writeSideshowOwned(t, filepath.Join(skillsDir, "bmad-still-here", "SKILL.md"), "synced")

	got, err := ProcessRemovals(packPath)
	if err != nil {
		t.Fatalf("ProcessRemovals: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d removals, want 0 (still-shipped canonicalId must not be deleted)", got)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "bmad-still-here", "SKILL.md")); err != nil {
		t.Errorf("still-shipped skill incorrectly removed: %v", err)
	}
}

func TestProcessRemovals_RemovesManagedCommand(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(packPath, "removals.txt"),
		[]byte("bmad-old-command\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	cmdsDir := filepath.Join(homeDir, ".claude", "commands")
	writeSideshowOwned(t, filepath.Join(cmdsDir, "bmad-old-command.md"), "stale command")

	got, err := ProcessRemovals(packPath)
	if err != nil {
		t.Fatalf("ProcessRemovals: %v", err)
	}
	if got != 1 {
		t.Errorf("got %d removals, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(cmdsDir, "bmad-old-command.md")); !os.IsNotExist(err) {
		t.Errorf("expected old command to be deleted; stat err = %v", err)
	}
}

func TestProcessRemovals_SkipsBlanksAndComments(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(packPath, "removals.txt"),
		[]byte("# header\n\n   \n# another comment\nbmad-real-removal\n# trailing\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	writeSideshowOwned(t, filepath.Join(skillsDir, "bmad-real-removal", "SKILL.md"), "x")

	got, err := ProcessRemovals(packPath)
	if err != nil {
		t.Fatalf("ProcessRemovals: %v", err)
	}
	if got != 1 {
		t.Errorf("got %d removals, want 1 (only one real entry)", got)
	}
}

func TestProcessRemovals_EntryAbsentFromTarget(t *testing.T) {
	// removals.txt names something that isn't synced — silent no-op.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(packPath, "removals.txt"),
		[]byte("bmad-never-synced\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	got, err := ProcessRemovals(packPath)
	if err != nil {
		t.Fatalf("ProcessRemovals: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d removals, want 0", got)
	}
}
