package bindings

import (
	"os"
	"path/filepath"
	"testing"
)

// makeSkillPack creates a pack dir shipping the named skills.
func makeSkillPack(t *testing.T, skills ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, s := range skills {
		dir := filepath.Join(root, ".claude", "skills", s)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
			[]byte("---\nname: "+s+"\n---\ncontent\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func skillExists(t *testing.T, home, name string) bool {
	t.Helper()
	info, err := os.Stat(filepath.Join(home, ".claude", "skills", name))
	return err == nil && info.IsDir()
}

func TestRunSync_ReconcilesStaleSkillsOnVersionFlip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", "")

	// Version 1 ships skills a + b.
	v1 := makeSkillPack(t, "bmad-alpha", "bmad-beta")
	synced, removed, err := runSync([]Binding{NewSkillDirBinding("bmad", "1.0.0", v1)})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if synced != 2 || removed != 0 {
		t.Fatalf("first sync = %d synced, %d removed; want 2, 0", synced, removed)
	}
	if !skillExists(t, home, "bmad-alpha") || !skillExists(t, home, "bmad-beta") {
		t.Fatal("v1 skills not synced")
	}

	// Version 2 drops bmad-alpha, adds bmad-gamma — the activation flip.
	v2 := makeSkillPack(t, "bmad-beta", "bmad-gamma")
	synced, removed, err = runSync([]Binding{NewSkillDirBinding("bmad", "2.0.0", v2)})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if synced != 2 {
		t.Errorf("second sync synced = %d, want 2", synced)
	}
	if removed != 1 {
		t.Errorf("second sync removed = %d, want 1 (the stale bmad-alpha)", removed)
	}
	if skillExists(t, home, "bmad-alpha") {
		t.Error("stale bmad-alpha survived the flip — chimera not reconciled")
	}
	if !skillExists(t, home, "bmad-beta") || !skillExists(t, home, "bmad-gamma") {
		t.Error("v2 skills missing after flip")
	}
}

func TestRunSync_DoesNotRemoveUnrecordedArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", "")

	// A user-authored skill sideshow never wrote.
	userSkill := filepath.Join(home, ".claude", "skills", "my-own-skill")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	v1 := makeSkillPack(t, "bmad-alpha")
	if _, _, err := runSync([]Binding{NewSkillDirBinding("bmad", "1.0.0", v1)}); err != nil {
		t.Fatal(err)
	}
	v2 := makeSkillPack(t, "bmad-beta")
	if _, _, err := runSync([]Binding{NewSkillDirBinding("bmad", "2.0.0", v2)}); err != nil {
		t.Fatal(err)
	}

	if !skillExists(t, home, "my-own-skill") {
		t.Error("user-authored skill was removed — reconciliation must only touch manifest-recorded artifacts")
	}
}

func TestArtifacts_MatchSyncDestinations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", "")

	pack := makeSkillPack(t, "bmad-one", "gds-two")
	b := NewSkillDirBinding("bmad", "1.0.0", pack)

	arts, err := b.Artifacts()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(home, ".claude", "skills", "bmad-one"),
		filepath.Join(home, ".claude", "skills", "gds-two"),
	}
	if len(arts) != 2 || arts[0] != want[0] || arts[1] != want[1] {
		t.Errorf("Artifacts() = %v, want %v", arts, want)
	}
}
