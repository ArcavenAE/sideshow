package coexistcheck

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/foreign"
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

func shadowFindings(rep *Report) []Result {
	var out []Result
	for _, r := range rep.Results {
		if r.Check == 11 {
			out = append(out, r)
		}
	}
	return out
}

func TestRun_ReportsASkillShadowedByUserScope(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	opts.BoundSkills = map[string]string{
		"bmad-help":  "/home/u/.claude/skills/bmad-help",
		"bmad-party": "/home/u/.claude/skills/bmad-party",
	}
	write(t, opts.RepoDir, ".claude/skills/bmad-help/SKILL.md", "x")
	write(t, opts.RepoDir, ".claude/skills/bmad-party/SKILL.md", "x")

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := shadowFindings(rep)
	if len(got) != 2 {
		t.Fatalf("want one finding per colliding skill, got %+v", got)
	}
	for i, name := range []string{"bmad-help", "bmad-party"} {
		if got[i].Name != "skill-shadow" || got[i].Severity != foreign.Warn {
			t.Errorf("finding %d = %+v, want skill-shadow at WARN", i, got[i])
		}
		if !strings.Contains(got[i].Detail, name) || !strings.Contains(got[i].Detail, "user-scope") {
			t.Errorf("finding %d must name the skill and the loaded copy: %q", i, got[i].Detail)
		}
		for _, path := range []string{"/home/u/.claude/skills/" + name, filepath.Join(opts.RepoDir, ".claude", "skills", name)} {
			if !strings.Contains(got[i].Detail, path) {
				t.Errorf("finding %d must name both copies, missing %s: %q", i, path, got[i].Detail)
			}
		}
	}
	if rep.Refuse() {
		t.Error("a shadow is advisory and must not refuse")
	}
}

func TestRun_NoOverlapIsQuiet(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	opts.BoundSkills = map[string]string{"bmad-help": "/home/u/.claude/skills/bmad-help"}
	write(t, opts.RepoDir, ".claude/skills/own-skill/SKILL.md", "x")

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := shadowFindings(rep); len(got) != 0 {
		t.Errorf("no overlap, got %+v", got)
	}
}

func TestRun_NoManifestEntriesSaysTheCheckDidNotRun(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	opts.BoundSkills = nil
	write(t, opts.RepoDir, ".claude/skills/bmad-help/SKILL.md", "x")

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := shadowFindings(rep)
	if len(got) != 1 || got[0].Severity != foreign.Info {
		t.Fatalf("want one INFO finding, got %+v", got)
	}
	want := "skill-shadow not checked: no sync manifest for " + opts.Pack
	if got[0].Detail != want {
		t.Errorf("detail = %q, want %q", got[0].Detail, want)
	}
}

// Control: with no native skills in the repo there is nothing to
// compare, so nothing needs saying.
func TestRun_NoManifestAndNoRepoSkillsIsQuiet(t *testing.T) {
	t.Parallel()
	opts := baseOptions(t)
	opts.BoundSkills = nil

	rep, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := shadowFindings(rep); len(got) != 0 {
		t.Errorf("nothing to compare, got %+v", got)
	}
}
