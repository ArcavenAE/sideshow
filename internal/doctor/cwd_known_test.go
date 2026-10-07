package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArcavenAE/sideshow/internal/bindings"
)

// aae-orc-edg8t (fault F-i): `sideshow project init` registers the repo
// as a custom source and `status` lists it, but cwd-known read only the
// ledger and the project installations, so doctor said sideshow had no
// record of a directory it had just initialized, and recommended
// `enable`, which refuses a pack that is not plugin-class.

func cwdFixture(t *testing.T, packs map[string]string) {
	t.Helper()
	home := fakeHome(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	var reg strings.Builder
	reg.WriteString("packs:\n")
	for name, packYAML := range packs {
		dir := filepath.Join(home, "packs", name, "1.0.0")
		write(t, filepath.Join(dir, "pack.yaml"), packYAML)
		if err := os.Symlink("1.0.0", filepath.Join(home, "packs", name, "current")); err != nil {
			t.Fatal(err)
		}
		reg.WriteString("  - name: " + name + "\n    version: 1.0.0\n    path: " + filepath.Join(home, "packs", name, "current") + "\n")
	}
	registryYAML(t, home, reg.String())
}

const (
	plainPack  = "name: demo\nversion: \"1.0.0\"\n"
	pluginPack = "name: plug\nversion: \"1.0.0\"\nactivation:\n  default_scope: per-repo\n  per_repo_required: true\n  mechanism: claude-plugin\n"
)

func cwdKnown(t *testing.T, repo string) Finding {
	t.Helper()
	report, _, err := Run(Options{Layers: []int{3}, RepoDir: repo, Now: time.Unix(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	got := findBy(report.Findings, "cwd-known")
	if len(got) != 1 {
		t.Fatalf("cwd-known findings = %d, want 1: %+v", len(got), report.Findings)
	}
	return got[0]
}

func TestCwdKnown_CountsACustomSourceRegistration(t *testing.T) {
	cwdFixture(t, map[string]string{"demo": plainPack})
	repo := t.TempDir()
	if _, err := bindings.RegisterCustomSource(repo, "demo"); err != nil {
		t.Fatal(err)
	}

	f := cwdKnown(t, repo)
	if f.Status != OK {
		t.Fatalf("cwd-known = %v (%s), want ok for a registered custom source", f.Status, f.Detail)
	}
	if !strings.Contains(f.Detail, "custom-source registration") {
		t.Errorf("detail does not name how the directory is known: %q", f.Detail)
	}
}

func TestCwdKnown_CustomSourceCoversADirectoryBelowItsRoot(t *testing.T) {
	cwdFixture(t, map[string]string{"demo": plainPack})
	repo := t.TempDir()
	sub := filepath.Join(repo, "pkg", "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.RegisterCustomSource(repo, "demo"); err != nil {
		t.Fatal(err)
	}

	if f := cwdKnown(t, sub); f.Status != OK {
		t.Errorf("cwd-known below the registered root = %v (%s), want ok", f.Status, f.Detail)
	}
}

func TestCwdKnown_UnrelatedDirectoryStillWarns(t *testing.T) {
	cwdFixture(t, map[string]string{"demo": plainPack})
	registered := t.TempDir()
	if _, err := bindings.RegisterCustomSource(registered, "demo"); err != nil {
		t.Fatal(err)
	}

	if f := cwdKnown(t, t.TempDir()); f.Status != Warn {
		t.Errorf("cwd-known for an unrelated directory = %v, want warn", f.Status)
	}
}

// A path that merely starts with the registered root's characters is a
// different directory.
func TestCwdKnown_SiblingWithSharedPrefixDoesNotMatch(t *testing.T) {
	cwdFixture(t, map[string]string{"demo": plainPack})
	parent := t.TempDir()
	registered := filepath.Join(parent, "repo")
	sibling := filepath.Join(parent, "repo-other")
	for _, d := range []string{registered, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := bindings.RegisterCustomSource(registered, "demo"); err != nil {
		t.Fatal(err)
	}

	if f := cwdKnown(t, sibling); f.Status != Warn {
		t.Errorf("cwd-known for %q = %v, want warn", sibling, f.Status)
	}
}

func TestCwdKnown_HintFollowsThePackClass(t *testing.T) {
	cwdFixture(t, map[string]string{"demo": plainPack, "plug": pluginPack})

	f := cwdKnown(t, t.TempDir())
	if f.Status != Warn {
		t.Fatalf("cwd-known = %v, want warn", f.Status)
	}
	if !strings.Contains(f.Next, "sideshow project init demo") {
		t.Errorf("no project init hint for the plain pack: %q", f.Next)
	}
	if !strings.Contains(f.Next, "sideshow enable plug --repo .") {
		t.Errorf("no enable hint for the plugin-class pack: %q", f.Next)
	}
	if strings.Contains(f.Next, "enable demo") {
		t.Errorf("enable recommended for a pack that refuses it: %q", f.Next)
	}
}

func TestCwdKnown_PlainPackOnlyNeverRecommendsEnable(t *testing.T) {
	cwdFixture(t, map[string]string{"demo": plainPack})

	f := cwdKnown(t, t.TempDir())
	if strings.Contains(f.Next, "enable") {
		t.Errorf("enable recommended when only a non-plugin pack is installed: %q", f.Next)
	}
}

// A registry that cannot be read is said so; it is not read as "no
// record" without comment.
func TestCwdKnown_NamesAnUnreadableCustomSourceRegistry(t *testing.T) {
	cwdFixture(t, map[string]string{"demo": plainPack})
	write(t, filepath.Join(os.Getenv("SIDESHOW_HOME"), "custom-sources.yaml"), "sources: [unclosed\n")

	f := cwdKnown(t, t.TempDir())
	if f.Status != Warn {
		t.Fatalf("cwd-known = %v, want warn", f.Status)
	}
	if !strings.Contains(f.Detail, "custom-source registry could not be read") {
		t.Errorf("detail hides the unreadable registry: %q", f.Detail)
	}
}
