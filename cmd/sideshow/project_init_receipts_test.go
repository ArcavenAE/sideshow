package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/distribute"
	"github.com/ArcavenAE/sideshow/internal/pack"
	"github.com/ArcavenAE/sideshow/internal/project"
)

const (
	rcptPack = "fleetp"
	rcptRule = ".claude/rules/r.md"
	rcptFile = "conf/a.cfg"
)

// rcptRig is an isolated store, one pack with a rule and a file, and a git
// repo to run `project init` in (sideshow#91).
type rcptRig struct {
	t    *testing.T
	repo string
}

func newRcptRig(t *testing.T) *rcptRig {
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
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "work")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(repo)
	return &rcptRig{t: t, repo: repo}
}

// install makes version the active one: the rule says "rule <v>", the
// file says "a <v>".
func (r *rcptRig) install(version string) {
	r.t.Helper()
	src := r.t.TempDir()
	yml := "name: " + rcptPack + "\nversion: " + version + "\ndistribute:\n  rules:\n    - source: rules/r.md\n      target: " + rcptRule + "\n  files:\n    - source: f/a.cfg\n      target: " + rcptFile + "\n"
	for p, c := range map[string]string{
		"pack.yaml":  yml,
		"rules/r.md": "rule " + version + "\n",
		"f/a.cfg":    "a " + version + "\n",
	} {
		full := filepath.Join(src, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	if err := pack.InstallFromLocal(rcptPack, src, true); err != nil {
		r.t.Fatalf("install %s: %v", version, err)
	}
}

func (r *rcptRig) init(args ...string) string {
	r.t.Helper()
	out, err := captureStdout(r.t, func() error {
		return runProjectInitForPack(append([]string{rcptPack}, args...))
	})
	if err != nil {
		r.t.Fatalf("project init: %v\n%s", err, out)
	}
	return out
}

func (r *rcptRig) read(rel string) string {
	b, _ := os.ReadFile(filepath.Join(r.repo, rel))
	return string(b)
}

func (r *rcptRig) write(rel, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.repo, rel), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rcptRig) receipts() (files, rules map[string]string) {
	r.t.Helper()
	reg, err := pack.LoadRegistry()
	if err != nil {
		r.t.Fatal(err)
	}
	id, root, err := project.SingleRepoKey(r.repo)
	if err != nil {
		r.t.Fatal(err)
	}
	name := filepath.Base(r.repo)
	return distribute.PriorChecksums(reg, id, root, project.SingleRepoManifest, name, rcptPack),
		distribute.PriorRuleChecksums(reg, id, root, project.SingleRepoManifest, name, rcptPack)
}

func marked(version string) string {
	return "<!-- managed by sideshow:" + rcptPack + ":" + version + " -->\nrule " + version + "\n"
}

// The issue's repro, flipped: 0.1.0 then 0.2.0 updates the file and the
// rule, and records receipts for both.
func TestProjectInit_AnUneditedFileAndRuleUpdateOnAPackBump(t *testing.T) {
	r := newRcptRig(t)
	r.install("0.1.0")
	r.init()
	if r.read(rcptFile) != "a 0.1.0\n" || r.read(rcptRule) != marked("0.1.0") {
		t.Fatalf("first run wrote:\n%s\n%s", r.read(rcptFile), r.read(rcptRule))
	}
	r.install("0.2.0")
	r.init()
	if got := r.read(rcptFile); got != "a 0.2.0\n" {
		t.Errorf("file not updated: %q", got)
	}
	if got := r.read(rcptRule); got != marked("0.2.0") {
		t.Errorf("rule not updated: %q", got)
	}
	files, rules := r.receipts()
	if want := "sha256:" + sha256Hex([]byte("a 0.2.0\n")); files[rcptFile] != want {
		t.Errorf("file receipt = %q, want %q", files[rcptFile], want)
	}
	if want := "sha256:" + sha256Hex([]byte(marked("0.2.0"))); rules[rcptRule] != want {
		t.Errorf("rule receipt = %q, want %q", rules[rcptRule], want)
	}
}

// A user edit, then 0.3.0: the edit is kept and the reason is printed.
func TestProjectInit_AnEditIsKeptAndTheReasonIsPrinted(t *testing.T) {
	r := newRcptRig(t)
	r.install("0.1.0")
	r.init()
	r.install("0.2.0")
	r.init()
	editedFile := r.read(rcptFile) + "my edit\n"
	editedRule := r.read(rcptRule) + "my rule edit\n"
	r.write(rcptFile, editedFile)
	r.write(rcptRule, editedRule)
	r.install("0.3.0")
	out := r.init()
	if r.read(rcptFile) != editedFile || r.read(rcptRule) != editedRule {
		t.Errorf("an edit was overwritten:\n%s\n%s", r.read(rcptFile), r.read(rcptRule))
	}
	for _, want := range []string{
		"files: skipped " + rcptFile + ": modified since sideshow wrote it (user edit preserved)",
		"rules: skipped " + rcptRule + ": modified since sideshow wrote it (user edit preserved)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// With no identity file nothing is written under .sideshow, and the derived
// key is the same from the repo path and from a symlink to it.
func TestProjectInit_WritesNoIdentityFileAndKeysByTheResolvedPath(t *testing.T) {
	r := newRcptRig(t)
	r.install("0.1.0")
	r.init()
	if _, err := os.Stat(filepath.Join(r.repo, ".sideshow")); !os.IsNotExist(err) {
		t.Errorf("project init wrote under .sideshow: %v", err)
	}
	link := filepath.Join(filepath.Dir(r.repo), "link")
	if err := os.Symlink(r.repo, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)
	r.install("0.2.0")
	r.init()
	if got := r.read(rcptFile); got != "a 0.2.0\n" {
		t.Errorf("a run through a symlinked cwd did not find the receipt: %q", got)
	}
}

// A dry run records nothing and prints the same reasons.
func TestProjectInit_DryRunRecordsNothingAndPrintsTheSameReasons(t *testing.T) {
	r := newRcptRig(t)
	r.install("0.1.0")
	r.init()
	edited := r.read(rcptFile) + "my edit\n"
	r.write(rcptFile, edited)
	r.install("0.2.0")
	filesBefore, rulesBefore := r.receipts()
	dry := r.init("--dry-run")
	filesAfter, rulesAfter := r.receipts()
	if filesAfter[rcptFile] != filesBefore[rcptFile] || rulesAfter[rcptRule] != rulesBefore[rcptRule] {
		t.Errorf("a dry run changed the receipts")
	}
	if r.read(rcptFile) != edited || r.read(rcptRule) != marked("0.1.0") {
		t.Errorf("a dry run changed a file")
	}
	real := r.init()
	reason := "files: skipped " + rcptFile + ": modified since sideshow wrote it (user edit preserved)"
	if !strings.Contains(dry, reason) || !strings.Contains(real, reason) {
		t.Errorf("reason missing\ndry:\n%s\nreal:\n%s", dry, real)
	}
}

// A moved repo has no receipt: a file is user-authored, and a rule is left
// as is unless its bytes equal the current content.
func TestProjectInit_AMovedRepoHasNoReceipt(t *testing.T) {
	r := newRcptRig(t)
	r.install("0.1.0")
	r.init()
	moved := filepath.Join(filepath.Dir(r.repo), "moved")
	if err := os.Rename(r.repo, moved); err != nil {
		t.Fatal(err)
	}
	t.Chdir(moved)
	r.repo = moved
	r.install("0.2.0")
	out := r.init()
	if got := r.read(rcptFile); got != "a 0.1.0\n" {
		t.Errorf("file changed in a moved repo: %q", got)
	}
	for _, want := range []string{
		"files: skipped " + rcptFile + ": exists and sideshow has no record of writing it (user-authored)",
		"rules: skipped " + rcptRule + ": has a sideshow marker but no receipt",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Two keys for one repo are fine. A repo reached by both paths: the cwd path
// records a rule receipt at v1, the orchestrator path moves the rule to v2,
// and the cwd path then reads the v2 bytes as already current and moves its
// receipt.
func TestProjectInit_TwoKeysForOneRepoAgree(t *testing.T) {
	r := newRcptRig(t)
	orch := filepath.Join(filepath.Dir(r.repo), "orch")
	sub := filepath.Join(orch, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orch, "repos.yaml"), []byte("repos:\n  sub:\n    path: sub\n    type: service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.repo = sub
	t.Chdir(sub)
	orchInit := func() {
		t.Helper()
		if _, err := captureStdout(t, func() error { return runInitProject(orch, "", rcptPack, false) }); err != nil {
			t.Fatalf("init --scope project: %v", err)
		}
	}
	r.install("0.1.0")
	orchInit()
	if out := r.init(); !strings.Contains(out, "rules: skipped "+rcptRule+": unchanged (receipt recorded)") {
		t.Errorf("cwd run at v1:\n%s", out)
	}
	r.install("0.2.0")
	orchInit()
	if r.read(rcptRule) != marked("0.2.0") {
		t.Fatalf("the orchestrator run did not move the rule: %q", r.read(rcptRule))
	}
	out := r.init()
	if !strings.Contains(out, "rules: skipped "+rcptRule+": already current") {
		t.Errorf("cwd run at v2:\n%s", out)
	}
	_, rules := r.receipts()
	if want := "sha256:" + sha256Hex([]byte(marked("0.2.0"))); rules[rcptRule] != want {
		t.Errorf("rule receipt = %q, want the v2 sha %q", rules[rcptRule], want)
	}
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// A files: artifact has no marker, so equal bytes with no receipt prove
// equality and not authorship, and are never adopted. A file created through
// one path (subrepo or cwd) is user-authored to the other; only the creating
// path refreshes it.
func TestProjectInit_AFilesArtifactCreatedByOnePathIsUserAuthoredToTheOther(t *testing.T) {
	r := newRcptRig(t)
	orch := filepath.Join(filepath.Dir(r.repo), "orch")
	sub := filepath.Join(orch, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orch, "repos.yaml"), []byte("repos:\n  sub:\n    path: sub\n    type: service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.repo = sub
	t.Chdir(sub)
	orchInit := func() {
		t.Helper()
		if _, err := captureStdout(t, func() error { return runInitProject(orch, "", rcptPack, false) }); err != nil {
			t.Fatalf("init --scope project: %v", err)
		}
	}

	r.install("0.2.0")
	orchInit()
	if got := r.read(rcptFile); got != "a 0.2.0\n" {
		t.Fatalf("the subrepo path did not create the file: %q", got)
	}
	out := r.init()
	if want := "files: skipped " + rcptFile + ": exists and sideshow has no record of writing it (user-authored)"; !strings.Contains(out, want) {
		t.Errorf("the cwd run did not print the reason %q:\n%s", want, out)
	}
	if files, _ := r.receipts(); files[rcptFile] != "" {
		t.Errorf("the cwd run recorded a receipt for a file it did not create: %q", files[rcptFile])
	}

	r.install("0.3.0")
	orchInit()
	if got := r.read(rcptFile); got != "a 0.3.0\n" {
		t.Errorf("the creating path did not refresh the file: %q", got)
	}
}
