package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// identityFixture installs a minimal pack with the given name into an
// isolated store, creates a git repo named repoName, and makes it the
// working directory. It returns the repo path.
func identityFixture(t *testing.T, packName, repoName string) string {
	t.Helper()
	store := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SIDESHOW_HOME", store)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("USER", "osuser")
	t.Cleanup(func() { _ = pack.UnfreezeTree(store) })

	src := t.TempDir()
	yml := "name: " + packName + "\nversion: 1.0.0\ndistribute:\n  custom_bridge:\n    upstream_path: _" + packName + "/custom\n    per_repo_dir: _" + packName + "-custom\n"
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pack.InstallFromLocal(packName, src, true); err != nil {
		t.Fatalf("install fixture: %v", err)
	}

	repo := filepath.Join(t.TempDir(), repoName)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(repo)
	return repo
}

func gitName(t *testing.T, repo, name string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", repo, "config", "user.name", name).CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}
}

func userLayer(repo, packName string) string {
	b, _ := os.ReadFile(filepath.Join(repo, "_"+packName+"-custom", "config.user.toml"))
	return string(b)
}

// aae-orc-jy1j: the pack ships no identity, so a bound repo resolved
// core.user_name and core.project_name as absent. project init seeds
// both into the human-authored user layer.
func TestProjectInit_SeedsIdentityFromTheFlag(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	if _, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	got := userLayer(repo, "bmad")
	for _, want := range []string{"[core]", `user_name = "Ada"`, `project_name = "widget"`} {
		if !strings.Contains(got, want) {
			t.Errorf("user layer missing %q:\n%s", want, got)
		}
	}
}

func TestProjectInit_FlagBeatsGitNameBeatsOSUser(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	gitName(t, repo, "Git Name")
	if _, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Flag Name"})
	}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	if got := userLayer(repo, "bmad"); !strings.Contains(got, `user_name = "Flag Name"`) {
		t.Errorf("flag did not win:\n%s", got)
	}

	repo2 := identityFixture(t, "bmad", "other")
	gitName(t, repo2, "Git Name")
	if _, err := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad"}) }); err != nil {
		t.Fatalf("project init: %v", err)
	}
	if got := userLayer(repo2, "bmad"); !strings.Contains(got, `user_name = "Git Name"`) {
		t.Errorf("git name did not beat the OS user:\n%s", got)
	}

	repo3 := identityFixture(t, "bmad", "third")
	if _, err := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad"}) }); err != nil {
		t.Fatalf("project init: %v", err)
	}
	if got := userLayer(repo3, "bmad"); !strings.Contains(got, `user_name = "osuser"`) {
		t.Errorf("OS user fallback missing:\n%s", got)
	}
}

// Control: a key the user already set is theirs; init adds only what
// is absent and never rewrites the file.
func TestProjectInit_KeepsExistingKeysAndOtherContent(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	dir := filepath.Join(repo, "_bmad-custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# mine\n[core]\nuser_name = \"Mine\"\n\n[agents.x]\nk = 1\n"
	if err := os.WriteFile(filepath.Join(dir, "config.user.toml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ignored"})
	}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	got := userLayer(repo, "bmad")
	if !strings.Contains(got, `user_name = "Mine"`) || strings.Contains(got, "Ignored") {
		t.Errorf("existing user_name was not kept:\n%s", got)
	}
	if !strings.Contains(got, "# mine") || !strings.Contains(got, "[agents.x]") {
		t.Errorf("existing content was lost:\n%s", got)
	}
	if !strings.Contains(got, `project_name = "widget"`) {
		t.Errorf("the absent project_name was not added:\n%s", got)
	}
}

func TestProjectInit_RerunChangesNothing(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	args := []string{"bmad", "--user-name", "Ada"}
	if _, err := captureStdout(t, func() error { return runProjectInitForPack(args) }); err != nil {
		t.Fatal(err)
	}
	first := userLayer(repo, "bmad")
	if _, err := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad", "--user-name", "Someone Else"}) }); err != nil {
		t.Fatal(err)
	}
	if second := userLayer(repo, "bmad"); second != first {
		t.Errorf("a re-run rewrote the layer:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// Control: only bmad has this layer convention here.
func TestProjectInit_OtherPackGetsNoIdentityFile(t *testing.T) {
	repo := identityFixture(t, "demo", "widget")
	if _, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"demo", "--user-name", "Ada"})
	}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "_demo-custom", "config.user.toml")); err == nil {
		t.Error("a non-bmad pack got an identity file")
	}
}

func TestProjectInit_DryRunWritesNoIdentity(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	if _, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada", "--dry-run"})
	}); err != nil {
		t.Fatalf("project init: %v", err)
	}
	if got := userLayer(repo, "bmad"); got != "" {
		t.Errorf("dry run wrote the identity layer:\n%s", got)
	}
}
