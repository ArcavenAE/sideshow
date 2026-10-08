package bindings

import (
	"os"
	"path/filepath"
	"testing"
)

// Each known kind removes as before, and a row of a kind this build does
// not know is skipped and left on disk, not counted as removed
// (sideshow#171).
func TestRemoveRepoArtifacts_KnownKindsRemoveUnknownIsKept(t *testing.T) {
	repo := t.TempDir()
	harness := filepath.Join(repo, ".claude")
	writeFile(t, filepath.Join(harness, "agents", "a.md"), "agent")
	writeFile(t, filepath.Join(harness, "skills", "s", "SKILL.md"), "skill")
	writeFile(t, filepath.Join(harness, "notes.txt"), "mine")
	if err := os.MkdirAll(filepath.Join(harness, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "engine")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(repo, "plugins", "p")); err != nil {
		t.Fatal(err)
	}

	arts := []RepoArtifact{
		{Kind: ArtifactAgentFile, Path: ".claude/agents/a.md"},
		{Kind: ArtifactSkillDir, Path: ".claude/skills/s"},
		{Kind: ArtifactParentDir, Path: ".claude/empty"},
		{Kind: ArtifactCompatSymlink, Path: "plugins/p"},
		{Kind: "future-kind", Path: ".claude/notes.txt"},
	}
	n, err := RemoveRepoArtifacts(RepoTarget{RepoDir: repo, Scope: ScopeProject}, arts)
	if err != nil {
		t.Fatalf("RemoveRepoArtifacts: %v", err)
	}
	if n != 4 {
		t.Errorf("removed %d, want 4 (the unknown kind is not counted)", n)
	}
	for _, gone := range []string{".claude/agents/a.md", ".claude/skills/s", ".claude/empty", "plugins/p"} {
		if _, err := os.Lstat(filepath.Join(repo, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still exists (%v)", gone, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(harness, "notes.txt")); err != nil || string(got) != "mine" {
		t.Errorf("unknown-kind file changed: %q, %v", got, err)
	}
	if !KnownArtifactKind(ArtifactAgentFile) || KnownArtifactKind("future-kind") {
		t.Error("KnownArtifactKind misclassifies")
	}
}
