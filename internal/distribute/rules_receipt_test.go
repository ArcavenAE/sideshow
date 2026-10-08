package distribute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
	"github.com/ArcavenAE/sideshow/internal/project"
)

const ruleTarget = ".claude/rules/r.md"

// rulesRig runs distribute for one rule across several runs, recording
// receipts in an in-memory registry as `init --scope project` does.
type rulesRig struct {
	t       *testing.T
	repo    project.Subrepo
	root    string
	reg     *pack.Registry
	source  string
	version string
}

func newRulesRig(t *testing.T) *rulesRig {
	t.Helper()
	dir := t.TempDir()
	return &rulesRig{
		t:       t,
		repo:    project.Subrepo{Name: "r", AbsPath: dir, Present: true},
		root:    t.TempDir(),
		reg:     &pack.Registry{},
		source:  "# Rule\n\nPack content.\n",
		version: "1.0.0",
	}
}

func (g *rulesRig) path() string { return filepath.Join(g.repo.AbsPath, ruleTarget) }

func (g *rulesRig) read() string {
	g.t.Helper()
	b, err := os.ReadFile(g.path())
	if err != nil {
		g.t.Fatal(err)
	}
	return string(b)
}

func (g *rulesRig) write(content string) {
	g.t.Helper()
	if err := os.MkdirAll(filepath.Dir(g.path()), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(g.path(), []byte(content), 0o644); err != nil {
		g.t.Fatal(err)
	}
}

func (g *rulesRig) expected() string {
	return fileMarker("testpack", g.version) + "\n" + g.source
}

func (g *rulesRig) receipt() string {
	return PriorRuleChecksums(g.reg, "p", "/root", "repos.yaml", g.repo.Name, "testpack")[ruleTarget]
}

func (g *rulesRig) run(dry bool) Action {
	g.t.Helper()
	if err := os.WriteFile(filepath.Join(g.root, "rule.md"), []byte(g.source), 0o644); err != nil {
		g.t.Fatal(err)
	}
	opts := Options{
		DryRun: dry, PackName: "testpack", PackVersion: g.version, PackRoot: g.root,
		PriorChecksums:     PriorChecksums(g.reg, "p", "/root", "repos.yaml", g.repo.Name, "testpack"),
		PriorRuleChecksums: PriorRuleChecksums(g.reg, "p", "/root", "repos.yaml", g.repo.Name, "testpack"),
	}
	m := &Manifest{Rules: []RuleArtifact{{Source: "rule.md", Target: ruleTarget}}}
	result := ToRepo(g.repo, m, opts)
	if !dry {
		RecordResults(g.reg, "p", "/root", "repos.yaml", []Result{result}, opts)
	}
	if len(result.Actions) != 1 {
		g.t.Fatalf("actions: %+v", result.Actions)
	}
	return result.Actions[0]
}

func wantAction(t *testing.T, a Action, status, detail string) {
	t.Helper()
	if a.Status != status || !strings.Contains(a.Detail, detail) {
		t.Errorf("action = %q %q, want %q containing %q", a.Status, a.Detail, status, detail)
	}
}

// An edit after the first run is preserved: the second run skips, and the
// receipt survives the skip so the third run says the same (sideshow#90).
func TestRules_EditAfterFirstRunIsPreserved(t *testing.T) {
	t.Parallel()
	g := newRulesRig(t)
	wantAction(t, g.run(false), "wrote", "created")
	edited := g.read() + "\nMY LOCAL EDIT\n"
	g.write(edited)
	wantAction(t, g.run(false), "skipped", "modified since sideshow wrote it")
	if g.read() != edited {
		t.Errorf("the edit was overwritten:\n%s", g.read())
	}
	wantAction(t, g.run(false), "skipped", "modified since sideshow wrote it")
	if g.read() != edited {
		t.Errorf("the edit was overwritten on the third run:\n%s", g.read())
	}
}

// An unedited file plus a pack version bump updates, and the receipt moves
// to the new bytes.
func TestRules_UneditedFileUpdatesOnAVersionBump(t *testing.T) {
	t.Parallel()
	g := newRulesRig(t)
	g.run(false)
	old := g.receipt()
	if old == "" {
		t.Fatal("no receipt after the first run")
	}
	g.version = "1.1.0"
	g.source = "# Rule\n\nNew pack content.\n"
	wantAction(t, g.run(false), "wrote", "updated")
	if g.read() != g.expected() {
		t.Errorf("not updated:\n%s", g.read())
	}
	if got, want := g.receipt(), sum([]byte(g.expected())); got != want || got == old {
		t.Errorf("receipt = %q, want the new sha %q (old %q)", got, want, old)
	}
}

// An unedited, current file is left alone and keeps its receipt.
func TestRules_CurrentFileIsLeftAloneAndKeepsItsReceipt(t *testing.T) {
	t.Parallel()
	g := newRulesRig(t)
	g.run(false)
	want := g.receipt()
	wantAction(t, g.run(false), "skipped", "already current")
	if g.receipt() != want {
		t.Errorf("receipt lost on an unchanged run: %q", g.receipt())
	}
}

// No receipt and the bytes equal what this run would write: record the
// receipt, and the next run after an edit skips.
func TestRules_NoReceiptBytesEqualRecordsTheReceipt(t *testing.T) {
	t.Parallel()
	g := newRulesRig(t)
	g.write(g.expected())
	wantAction(t, g.run(false), "skipped", "unchanged (receipt recorded)")
	if got, want := g.receipt(), sum([]byte(g.expected())); got != want {
		t.Fatalf("receipt = %q, want %q", got, want)
	}
	g.write(g.read() + "\nEDIT\n")
	wantAction(t, g.run(false), "skipped", "modified since sideshow wrote it")
}

// No receipt and the bytes differ: left as is, nothing recorded, the same
// on a rerun.
func TestRules_NoReceiptBytesDifferIsLeftAsIs(t *testing.T) {
	t.Parallel()
	g := newRulesRig(t)
	stale := fileMarker("testpack", "0.9.0") + "\n# Old content\n"
	g.write(stale)
	const msg = "has a sideshow marker but no receipt; left as is; delete it to take the pack's version"
	for i := 0; i < 2; i++ {
		wantAction(t, g.run(false), "skipped", msg)
		if g.read() != stale {
			t.Fatalf("run %d changed the file:\n%s", i+1, g.read())
		}
		if g.receipt() != "" {
			t.Fatalf("run %d recorded a receipt: %q", i+1, g.receipt())
		}
	}
}

// A rule and a files: entry at the same path keep separate receipts.
func TestRules_ReceiptsAreKeyedByType(t *testing.T) {
	t.Parallel()
	reg := &pack.Registry{}
	res := Result{RepoName: "r", Actions: []Action{
		{Status: "wrote", Artifact: pack.DistributedArtifact{Type: "rules", Path: "x", Checksum: "sha256:aaa"}},
		{Status: "wrote", Artifact: pack.DistributedArtifact{Type: "files", Path: "x", Checksum: "sha256:bbb"}},
	}}
	RecordResults(reg, "p", "/root", "repos.yaml", []Result{res}, Options{PackName: "testpack"})
	if got := PriorRuleChecksums(reg, "p", "/root", "repos.yaml", "r", "testpack")["x"]; got != "sha256:aaa" {
		t.Errorf("rule receipt = %q", got)
	}
	if got := PriorChecksums(reg, "p", "/root", "repos.yaml", "r", "testpack")["x"]; got != "sha256:bbb" {
		t.Errorf("file receipt = %q", got)
	}
}

// A files: receipt at the rule's path is not a rule receipt.
func TestRules_AFilesReceiptAtTheSamePathIsNotARuleReceipt(t *testing.T) {
	t.Parallel()
	g := newRulesRig(t)
	g.write(g.expected())
	res := Result{RepoName: "r", Actions: []Action{
		{Status: "wrote", Artifact: pack.DistributedArtifact{Type: "files", Path: ruleTarget, Checksum: "sha256:bbb"}},
	}}
	RecordResults(g.reg, "p", "/root", "repos.yaml", []Result{res}, Options{PackName: "testpack"})
	wantAction(t, g.run(false), "skipped", "unchanged (receipt recorded)")
}

// Dry-run reports each outcome as "would ..." and changes nothing.
func TestRules_DryRunMirrorsEachCase(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		setup  func(g *rulesRig)
		status string
		detail string
	}{
		{"create", func(g *rulesRig) {}, "wrote", "would create"},
		{"update", func(g *rulesRig) {
			g.run(false)
			g.version, g.source = "1.1.0", "# Rule\n\nNew.\n"
		}, "wrote", "would update"},
		{"edit", func(g *rulesRig) { g.run(false); g.write(g.read() + "\nEDIT\n") }, "skipped", "would skip: modified since sideshow wrote it"},
		{"current", func(g *rulesRig) { g.run(false) }, "skipped", "would skip: already current"},
		{"no receipt equal", func(g *rulesRig) { g.write(g.expected()) }, "skipped", "would record receipt (unchanged)"},
		{"no receipt differs", func(g *rulesRig) { g.write(fileMarker("testpack", "0.9.0") + "\nold\n") }, "skipped", "would skip: has a sideshow marker but no receipt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRulesRig(t)
			c.setup(g)
			var before string
			if _, err := os.Stat(g.path()); err == nil {
				before = g.read()
			}
			receiptBefore := g.receipt()
			wantAction(t, g.run(true), c.status, c.detail)
			if _, err := os.Stat(g.path()); err == nil {
				if g.read() != before {
					t.Errorf("dry-run changed the file")
				}
			} else if before != "" {
				t.Errorf("dry-run removed the file")
			}
			if g.receipt() != receiptBefore {
				t.Errorf("dry-run changed the receipt")
			}
		})
	}
}
