package coexistcheck

import (
	"reflect"
	"testing"
)

func TestRepoSkillNames_ListsDirsCarryingASkillFile(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	write(t, repo, ".claude/skills/bmad-help/SKILL.md", "---\nname: bmad-help\n---\n")
	write(t, repo, ".claude/skills/bmad-party/SKILL.md", "---\nname: bmad-party\n---\n")
	// A directory with no SKILL.md is not a skill the harness loads.
	write(t, repo, ".claude/skills/notes/README.md", "x")
	// A loose file is not a skill either.
	write(t, repo, ".claude/skills/stray.md", "x")

	got := repoSkillNames(repo)
	want := []string{"bmad-help", "bmad-party"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("repoSkillNames = %v, want %v", got, want)
	}
}

func TestRepoSkillNames_NoSkillsDirIsEmpty(t *testing.T) {
	t.Parallel()
	if got := repoSkillNames(t.TempDir()); len(got) != 0 {
		t.Errorf("repoSkillNames = %v, want none", got)
	}
}
