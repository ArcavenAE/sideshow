package distribute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
	"github.com/ArcavenAE/sideshow/internal/project"
)

// filesRig runs distribute for two files: artifacts across several runs,
// recording receipts in an in-memory registry as `init --scope project` does.
type filesRig struct {
	t      *testing.T
	repo   project.Subrepo
	root   string
	reg    *pack.Registry
	source map[string]string // target -> pack content
}

func newFilesRig(t *testing.T) *filesRig {
	t.Helper()
	return &filesRig{
		t:      t,
		repo:   project.Subrepo{Name: "r", AbsPath: t.TempDir(), Present: true},
		root:   t.TempDir(),
		reg:    &pack.Registry{},
		source: map[string]string{"conf/a.cfg": "a from pack\n", "conf/b.cfg": "b from pack\n"},
	}
}

func (g *filesRig) path(target string) string { return filepath.Join(g.repo.AbsPath, target) }

func (g *filesRig) read(target string) string {
	g.t.Helper()
	b, err := os.ReadFile(g.path(target))
	if err != nil {
		g.t.Fatal(err)
	}
	return string(b)
}

func (g *filesRig) run(dry bool) map[string]Action {
	g.t.Helper()
	m := &Manifest{}
	for _, target := range []string{"conf/a.cfg", "conf/b.cfg"} {
		src := "src-" + filepath.Base(target)
		if err := os.WriteFile(filepath.Join(g.root, src), []byte(g.source[target]), 0o644); err != nil {
			g.t.Fatal(err)
		}
		m.Files = append(m.Files, FileArtifact{Source: src, Target: target})
	}
	opts := Options{
		DryRun: dry, PackName: "testpack", PackVersion: "1.0.0", PackRoot: g.root,
		PriorChecksums: PriorChecksums(g.reg, "p", "/root", "repos.yaml", g.repo.Name, "testpack"),
	}
	res := ToRepo(g.repo, m, opts)
	if !dry {
		RecordResults(g.reg, "p", "/root", "repos.yaml", []Result{res}, opts)
	}
	out := map[string]Action{}
	for _, a := range res.Actions {
		out[a.Path] = a
	}
	return out
}

// An unedited file keeps its receipt when a sibling is written in the same
// run, so a later pack update still reaches it (sideshow#190).
func TestFiles_AnUneditedFileStillUpdatesAfterASiblingIsWritten(t *testing.T) {
	t.Parallel()
	g := newFilesRig(t)
	g.run(false)
	if err := os.Remove(g.path("conf/b.cfg")); err != nil {
		t.Fatal(err)
	}
	acts := g.run(false)
	wantAction(t, acts["conf/a.cfg"], "skipped", "already current")
	wantAction(t, acts["conf/b.cfg"], "wrote", "created")
	g.source["conf/a.cfg"] = "a from pack v2\n"
	acts = g.run(false)
	wantAction(t, acts["conf/a.cfg"], "wrote", "")
	if g.read("conf/a.cfg") != "a from pack v2\n" {
		t.Errorf("the pack update did not reach an unedited file:\n%s", g.read("conf/a.cfg"))
	}
}

// An edited file keeps its receipt too, so the reason stays "modified" and
// not "no record" after a sibling is written.
func TestFiles_AnEditedFileStaysModifiedAfterASiblingIsWritten(t *testing.T) {
	t.Parallel()
	g := newFilesRig(t)
	g.run(false)
	edited := g.read("conf/a.cfg") + "my edit\n"
	if err := os.WriteFile(g.path("conf/a.cfg"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(g.path("conf/b.cfg")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		acts := g.run(false)
		wantAction(t, acts["conf/a.cfg"], "skipped", "modified since sideshow wrote it")
		if i == 0 {
			wantAction(t, acts["conf/b.cfg"], "wrote", "created")
		}
		if g.read("conf/a.cfg") != edited {
			t.Fatalf("run %d changed the edit:\n%s", i+1, g.read("conf/a.cfg"))
		}
	}
}

// A user-authored file with no record is not given a receipt by a skip.
func TestFiles_AUserAuthoredFileIsNotGivenAReceipt(t *testing.T) {
	t.Parallel()
	g := newFilesRig(t)
	if err := os.MkdirAll(filepath.Dir(g.path("conf/a.cfg")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(g.path("conf/a.cfg"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		acts := g.run(false)
		wantAction(t, acts["conf/a.cfg"], "skipped", "no record of writing it")
		if _, ok := PriorChecksums(g.reg, "p", "/root", "repos.yaml", "r", "testpack")["conf/a.cfg"]; ok {
			t.Fatalf("run %d recorded a receipt for a user-authored file", i+1)
		}
	}
}

// A dry run asks for no receipt to be recorded, for files and for rules
// alike: the caller's own dry-run guard is not the only thing stopping it.
func TestDryRunNeverAsksToRecordAReceipt(t *testing.T) {
	t.Parallel()
	g := newFilesRig(t)
	g.run(false)
	edited := g.read("conf/a.cfg") + "my edit\n"
	if err := os.WriteFile(g.path("conf/a.cfg"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	for target, a := range g.run(true) {
		if a.RecordReceipt {
			t.Errorf("files dry-run %s asks to record a receipt: %+v", target, a)
		}
	}
	r := newRulesRig(t)
	r.run(false)
	old := r.receipt()
	r.version, r.source = "1.1.0", "# Rule\n\nNew pack content.\n"
	r.write(r.expected()) // a lagging receipt with current bytes
	if a := r.run(true); a.RecordReceipt || !strings.Contains(a.Detail, "already current") {
		t.Errorf("rules dry-run, lagging receipt: %+v", a)
	}
	r.write(r.read() + "\nEDIT\n")
	if a := r.run(true); a.RecordReceipt {
		t.Errorf("rules dry-run, edited: %+v", a)
	}
	if r.receipt() != old {
		t.Errorf("a dry run moved the receipt")
	}
}
