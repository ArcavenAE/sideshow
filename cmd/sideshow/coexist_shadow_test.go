package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoWithSkill returns a git-less repo directory carrying one native
// skill, the shape of a committed native install.
func repoWithSkill(t *testing.T, name string) string {
	t.Helper()
	repo := t.TempDir()
	dir := filepath.Join(repo, ".claude", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\nrepo copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// aae-orc-phytt: a repo's native skill and a user-scope binding of the
// same name both exist, and coexist-check said "all checks clean".
func TestRunCoexistCheck_ReportsASkillShadowedByUserScope(t *testing.T) {
	unwiredFixture(t, "alpha", "beta")
	if err := runCommandsSync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	repo := repoWithSkill(t, "alpha")

	out, err := captureStdout(t, func() error { return runCoexistCheck([]string{"demo", "--repo", repo}) })
	if err != nil {
		t.Fatalf("a shadow is advisory, got error: %v\n%s", err, out)
	}
	if strings.Contains(out, "all checks clean") {
		t.Errorf("a shadowed skill printed as clean:\n%s", out)
	}
	for _, want := range []string{"skill-shadow", "alpha", filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "skills", "alpha"), filepath.Join(repo, ".claude", "skills", "alpha")} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "beta") {
		t.Errorf("beta is not in the repo and must not be reported:\n%s", out)
	}
	if !strings.Contains(out, "1 warn") {
		t.Errorf("summary must count the WARN so the output cannot read as zero findings:\n%s", out)
	}
}

// Control: nothing bound yet, so the repo's skill shadows nothing.
func TestRunCoexistCheck_NothingBoundIsNotAShadow(t *testing.T) {
	unwiredFixture(t, "alpha")
	repo := repoWithSkill(t, "alpha")

	out, err := captureStdout(t, func() error { return runCoexistCheck([]string{"demo", "--repo", repo}) })
	if err != nil {
		t.Fatalf("runCoexistCheck: %v\n%s", err, out)
	}
	if strings.Contains(out, "WARN [11") {
		t.Errorf("no user-scope copy exists, yet a shadow was reported:\n%s", out)
	}
	if !strings.Contains(out, "skill-shadow not checked: no skill-dir entries in the sync manifest for demo") {
		t.Errorf("an empty comparison must say it did not run:\n%s", out)
	}
}

// Control: a bound skill the repo does not carry is not a shadow.
func TestRunCoexistCheck_DifferentSkillIsQuiet(t *testing.T) {
	unwiredFixture(t, "alpha")
	if err := runCommandsSync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	repo := repoWithSkill(t, "own")

	out, err := captureStdout(t, func() error { return runCoexistCheck([]string{"demo", "--repo", repo}) })
	if err != nil {
		t.Fatalf("runCoexistCheck: %v\n%s", err, out)
	}
	if strings.Contains(out, "WARN [11") {
		t.Errorf("no overlap, yet a shadow was reported:\n%s", out)
	}
}

// Control: another pack's binding of the same name is not this pack's
// shadow; the manifest is read per pack.
func TestRunCoexistCheck_OtherPacksBindingIsQuiet(t *testing.T) {
	unwiredFixture(t, "alpha")
	if err := runCommandsSync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	repo := repoWithSkill(t, "alpha")

	out, err := captureStdout(t, func() error { return runCoexistCheck([]string{"other", "--repo", repo}) })
	if err != nil {
		t.Fatalf("runCoexistCheck: %v\n%s", err, out)
	}
	if strings.Contains(out, "WARN [11") {
		t.Errorf("pack other binds nothing, yet a shadow was reported:\n%s", out)
	}
}
