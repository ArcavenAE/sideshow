package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// linksFixture installs a pack whose shim links three read surfaces
// into the store, and returns a git repo as the working directory.
func linksFixture(t *testing.T) string {
	t.Helper()
	store := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	emptyGitConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(emptyGitConfig, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", emptyGitConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("SIDESHOW_HOME", store)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Cleanup(func() { _ = pack.UnfreezeTree(store) })

	src := t.TempDir()
	yml := "name: bmad\nversion: 1.0.0\ndistribute:\n  custom_bridge:\n    upstream_path: _bmad/custom\n    per_repo_dir: _bmad-custom\n" +
		"  runtime_links:\n    - { link: scripts, target: scripts }\n    - { link: config.toml, target: config.toml }\n    - { link: _config, target: _config }\n"
	for rel, content := range map[string]string{
		"pack.yaml":         yml,
		"scripts/thing.sh":  "store\n",
		"config.toml":       "store\n",
		"_config/skill.csv": "store\n",
	} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := pack.InstallFromLocal("bmad", src, true); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	repo := filepath.Join(t.TempDir(), "widget")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(repo)
	return repo
}

// A repo that already carries a real _bmad/ must get a report naming each
// path project init left alone and why, and the summary must count them
// (aae-orc-ylmnc). Existing content is never replaced.
func TestProjectInit_RealShimContentIsReportedAsConflicts(t *testing.T) {
	repo := linksFixture(t)
	for rel, content := range map[string]string{
		"_bmad/config.toml":      "real\n",
		"_bmad/scripts/thing.sh": "real\n",
	} {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	})
	if err != nil {
		t.Fatalf("project init: %v", err)
	}
	for _, want := range []string{
		"conflict _bmad/config.toml",
		"conflict _bmad/scripts",
		"2 conflicts",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// Existing content is untouched, and the untouched paths stay real.
	for _, rel := range []string{"_bmad/config.toml", "_bmad/scripts"} {
		info, lerr := os.Lstat(filepath.Join(repo, filepath.FromSlash(rel)))
		if lerr != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s was replaced or removed: %v", rel, lerr)
		}
	}
	if info, lerr := os.Lstat(filepath.Join(repo, "_bmad", "_config")); lerr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("_bmad/_config should still be linked: %v", lerr)
	}
}

// A clean repo reports no conflicts and the summary does not mention them.
func TestProjectInit_NoConflictsWordWhenThereAreNone(t *testing.T) {
	linksFixture(t)
	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	})
	if err != nil {
		t.Fatalf("project init: %v", err)
	}
	if strings.Contains(out, "conflict") {
		t.Errorf("clean repo output mentions a conflict:\n%s", out)
	}
}

// One conflict reads "1 conflict", not "1 conflicts".
func TestProjectInit_OneConflictIsSingular(t *testing.T) {
	repo := linksFixture(t)
	p := filepath.Join(repo, "_bmad", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("real\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	})
	if err != nil {
		t.Fatalf("project init: %v", err)
	}
	if !strings.Contains(out, ", 1 conflict (") {
		t.Errorf("summary lacks \"1 conflict (\":\n%s", out)
	}
	if strings.Contains(out, "1 conflicts") {
		t.Errorf("summary says \"1 conflicts\":\n%s", out)
	}
}
