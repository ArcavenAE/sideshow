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
	return identityFixtureWith(t, packName, repoName, true)
}

// identityFixtureWith lets a test leave the pack's gitignore entry for
// the personal layer out.
func identityFixtureWith(t *testing.T, packName, repoName string, ignoreLayer bool) string {
	t.Helper()
	store := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	// Isolate git's own config: a global or system user.name on the host
	// would otherwise answer the "no name" cases.
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
	yml := "name: " + packName + "\nversion: 1.0.0\ndistribute:\n  custom_bridge:\n    upstream_path: _" + packName + "/custom\n    per_repo_dir: _" + packName + "-custom\n"
	if ignoreLayer {
		yml += "  gitignore:\n    - /_" + packName + "-custom/config.user.toml\n"
	}
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

const notSetLine = "user_name not set: pass --user-name or set git user.name\n"

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

func TestProjectInit_FlagBeatsGitName(t *testing.T) {
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
		t.Errorf("git name was not used:\n%s", got)
	}

	// Never invent a name: with neither source, user_name stays absent
	// and project_name is still seeded.
	repo3 := identityFixture(t, "bmad", "third")
	out3, err3 := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad"}) })
	if err3 != nil {
		t.Fatalf("project init: %v", err3)
	}
	if n := strings.Count(out3, notSetLine); n != 1 {
		t.Errorf("want the not-set line exactly once, got %d:\n%s", n, out3)
	}
	got := userLayer(repo3, "bmad")
	if strings.Contains(got, "user_name") {
		t.Errorf("a name was invented:\n%s", got)
	}
	if !strings.Contains(got, `project_name = "third"`) {
		t.Errorf("project_name was not seeded:\n%s", got)
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
	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada", "--dry-run"})
	})
	if err != nil {
		t.Fatalf("project init: %v", err)
	}
	if !strings.Contains(out, "would seed user_name and project_name") {
		t.Errorf("dry run did not say what it would seed:\n%s", out)
	}
	if got := userLayer(repo, "bmad"); got != "" {
		t.Errorf("dry run wrote the identity layer:\n%s", got)
	}
}

// The layer holds a person's name. If the repo would commit it, init
// must not write it, and must say why.
func TestProjectInit_RefusesToWriteANameTheRepoWouldCommit(t *testing.T) {
	repo := identityFixtureWith(t, "bmad", "widget", false)
	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	})
	if err != nil {
		t.Fatalf("project init: %v", err)
	}
	if got := userLayer(repo, "bmad"); got != "" {
		t.Errorf("identity written to a layer that is not gitignored:\n%s", got)
	}
	if !strings.Contains(out, "not gitignored") {
		t.Errorf("init did not say why it skipped the identity:\n%s", out)
	}
}

// A core table written inline cannot take a second [core] header, so
// init leaves the file alone and says so.
func TestProjectInit_InlineCoreTableIsLeftAlone(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	dir := filepath.Join(repo, "_bmad-custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "core = { language = \"en\" }\n"
	if err := os.WriteFile(filepath.Join(dir, "config.user.toml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	})
	if err != nil {
		t.Fatalf("project init: %v", err)
	}
	if got := userLayer(repo, "bmad"); got != existing {
		t.Errorf("the file was edited:\n%s", got)
	}
	if !strings.Contains(out, "inline") {
		t.Errorf("init did not say why it skipped:\n%s", out)
	}
}

// The not-set line is printed only when it applies.
func TestProjectInit_NotSetLineOnlyWhenNoNameResolves(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	out, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "user_name not set") {
		t.Errorf("a name was given, yet init said none was set:\n%s", out)
	}
	_ = repo

	identityFixture(t, "bmad", "other")
	out, err = captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, notSetLine) != 1 {
		t.Errorf("dry run must print the not-set line once:\n%s", out)
	}
}

// The name is the one git resolves inside the repo: local over global.
func TestProjectInit_LocalGitNameBeatsGlobal(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	global := filepath.Join(os.Getenv("HOME"), ".gitconfig")
	if err := os.WriteFile(global, []byte("[user]\n\tname = Global Name\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	if _, err := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad"}) }); err != nil {
		t.Fatal(err)
	}
	if got := userLayer(repo, "bmad"); !strings.Contains(got, `user_name = "Global Name"`) {
		t.Fatalf("global name not used when no local one is set:\n%s", got)
	}

	repo2 := identityFixture(t, "bmad", "second")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	gitName(t, repo2, "Local Name")
	if _, err := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad"}) }); err != nil {
		t.Fatal(err)
	}
	if got := userLayer(repo2, "bmad"); !strings.Contains(got, `user_name = "Local Name"`) {
		t.Errorf("local name did not beat global:\n%s", got)
	}
}

// The privacy guarantee rests on this: after init in a freshly bound
// repo, git ignores the file that now holds a name.
func TestProjectInit_WrittenLayerIsGitIgnored(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	if _, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	}); err != nil {
		t.Fatal(err)
	}
	if userLayer(repo, "bmad") == "" {
		t.Fatal("nothing was written, so the ignore check proves nothing")
	}
	if out, err := exec.Command("git", "-C", repo, "check-ignore", "-q", "_bmad-custom/config.user.toml").CombinedOutput(); err != nil {
		t.Errorf("the identity layer is not gitignored: %v\n%s", err, out)
	}
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "config.user.toml") {
		t.Errorf("git would commit the identity layer:\n%s", out)
	}
}

// Control: a user_name the file already defines needs no hint.
func TestProjectInit_NoNotSetLineWhenTheFileHasAName(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	dir := filepath.Join(repo, "_bmad-custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.user.toml"), []byte("[core]\nuser_name = \"Mine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad"}) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "user_name not set") {
		t.Errorf("the file has a user_name, yet init said none was set:\n%s", out)
	}
}

// A second definition of core after dotted keys or a quoted header makes
// the layer invalid TOML for a strict parser (tomllib), so init leaves
// such a file alone and says why. Nothing is added to it.
func TestProjectInit_DottedOrQuotedCoreIsLeftAlone(t *testing.T) {
	for name, existing := range map[string]string{
		"dotted user_name":     "core.user_name = \"Mine\"\n",
		"dotted other key":     "core.communication_language = \"English\"\n",
		"quoted header":        "[\"core\"]\nlanguage = \"en\"\n",
		"single-quoted header": "['core']\nlanguage = 'en'\n",
		"spaced dotted key":    "core . user_name = \"Mine\"\n",
		"quoted dotted key":    "\"core\".user_name = \"Mine\"\n",
		"single-quoted dotted": "'core'.user_name = \"Mine\"\n",
		"quoted inline table":  "\"core\" = { language = \"en\" }\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo := identityFixture(t, "bmad", "widget")
			dir := filepath.Join(repo, "_bmad-custom")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.user.toml"), []byte(existing), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := captureStdout(t, func() error {
				return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
			})
			if err != nil {
				t.Fatalf("project init: %v", err)
			}
			if got := userLayer(repo, "bmad"); got != existing {
				t.Errorf("the file was edited:\n%s", got)
			}
			if !strings.Contains(out, "by hand") {
				t.Errorf("init did not say what to do:\n%s", out)
			}
		})
	}
}

// Control: dotted core keys that already define everything wanted need
// no edit and no complaint.
func TestProjectInit_DottedCoreThatAlreadyHasBothKeysIsQuiet(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	dir := filepath.Join(repo, "_bmad-custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "core.user_name = \"A\"\ncore.project_name = \"B\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.user.toml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad"}) })
	if err != nil {
		t.Fatal(err)
	}
	if got := userLayer(repo, "bmad"); got != existing {
		t.Errorf("the file was edited:\n%s", got)
	}
	if strings.Contains(out, "by hand") {
		t.Errorf("a complete file was reported as needing a hand edit:\n%s", out)
	}
}

// Control: a dotted key under another table is that table's own key
// (other.core.y), so it does not define core and the file is edited.
func TestProjectInit_DottedKeyUnderAnotherTableDoesNotBlock(t *testing.T) {
	repo := identityFixture(t, "bmad", "widget")
	dir := filepath.Join(repo, "_bmad-custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "[other]\nx = 1\ncore.y = 2\n"
	if err := os.WriteFile(filepath.Join(dir, "config.user.toml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return runProjectInitForPack([]string{"bmad", "--user-name", "Ada"})
	}); err != nil {
		t.Fatal(err)
	}
	got := userLayer(repo, "bmad")
	if !strings.Contains(got, "core.y = 2") || !strings.Contains(got, "[core]") || !strings.Contains(got, `user_name = "Ada"`) {
		t.Errorf("the file was not edited as expected:\n%s", got)
	}
}

// Control: the same spellings that already define both keys need no
// edit, and are not reported as needing a hand edit.
func TestProjectInit_SpelledDottedCoreThatHasBothKeysIsQuiet(t *testing.T) {
	for name, existing := range map[string]string{
		"spaced":        "core . user_name = \"A\"\ncore . project_name = \"B\"\n",
		"quoted":        "\"core\".user_name = \"A\"\n\"core\".project_name = \"B\"\n",
		"single-quoted": "'core'.user_name = \"A\"\n'core'.project_name = \"B\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			repo := identityFixture(t, "bmad", "widget")
			dir := filepath.Join(repo, "_bmad-custom")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.user.toml"), []byte(existing), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := captureStdout(t, func() error { return runProjectInitForPack([]string{"bmad"}) })
			if err != nil {
				t.Fatal(err)
			}
			if got := userLayer(repo, "bmad"); got != existing {
				t.Errorf("the file was edited:\n%s", got)
			}
			if strings.Contains(out, "by hand") {
				t.Errorf("a complete file was reported as needing a hand edit:\n%s", out)
			}
		})
	}
}
