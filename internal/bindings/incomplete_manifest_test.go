package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingPack is a pack whose commands/aaa.md cannot be read, so its
// markdown-command binding fails after the pack's skill binding has
// written. It returns the pack path and the unreadable file.
func failingPack(t *testing.T, skillText string) (string, string) {
	t.Helper()
	p := skillPack(t, "shared-skill", skillText)
	bad := filepath.Join(p, "commands", "aaa.md")
	writeFile(t, bad, "x")
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o644) })
	return p, bad
}

func manifestByPath(t *testing.T) map[string]ManifestEntry {
	t.Helper()
	m, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ManifestEntry{}
	for _, e := range m.Entries {
		out[filepath.Base(e.Path)] = e
	}
	return out
}

// T1: a sync with a failing binding still records what its other
// bindings wrote (sideshow#161). The shared path names the pack whose
// bytes are on disk, the previous record stays for paths nothing
// claimed, the manifest says it is incomplete and which binding failed,
// nothing is removed, and the exit is nonzero.
func TestRunSync_FailedBindingSavesAnIncompleteMergedManifest(t *testing.T) {
	home := collisionEnv(t)
	a := skillPack(t, "shared-skill", "alpha text")
	writeFile(t, filepath.Join(a, ".claude", "skills", "alpha-only", "SKILL.md"), "only alpha")
	b, _ := failingPack(t, "beta text")

	if _, _, err := runSync([]Binding{NewSkillDirBinding("alpha", "1", a)}); err != nil {
		t.Fatalf("alpha sync: %v", err)
	}
	var err error
	captureStderr(t, func() {
		_, _, err = runSync([]Binding{NewSkillDirBinding("beta", "1", b), NewMarkdownCommandBinding("beta", "1", b)})
	})
	if err == nil {
		t.Fatal("a sync with a failing binding exited zero")
	}

	m, lerr := LoadManifest()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if m.IsComplete() {
		t.Error("manifest reads complete after a failed sync")
	}
	if len(m.Failed) != 1 || m.Failed[0].Pack != "beta" || m.Failed[0].Kind != "markdown-command" || m.Failed[0].Error == "" {
		t.Errorf("failed = %+v, want beta's markdown-command with an error", m.Failed)
	}
	by := manifestByPath(t)
	if by["shared-skill"].Pack != "beta" {
		t.Errorf("shared-skill is recorded for %q, want beta (its bytes are on disk)", by["shared-skill"].Pack)
	}
	if by["alpha-only"].Pack != "alpha" {
		t.Errorf("alpha-only is recorded for %q, want alpha (no succeeding binding claimed it)", by["alpha-only"].Pack)
	}
	if _, serr := os.Stat(filepath.Join(home, ".claude", "skills", "alpha-only")); serr != nil {
		t.Errorf("a failed sync removed alpha-only: %v", serr)
	}
	if note := m.IncompleteNote(); !strings.Contains(note, "beta/markdown-command") {
		t.Errorf("note = %q", note)
	}
}

// T2: a markdown command whose write fails in the second pass (bmad-*.md
// outside commands/) returns its error like the first pass, and the file
// that was never written has no manifest entry.
func TestMarkdownCommand_SecondPassWriteErrorIsReturned(t *testing.T) {
	home := collisionEnv(t)
	cmds := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(cmds, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cmds, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cmds, 0o755) })
	p := t.TempDir()
	writeFile(t, filepath.Join(p, "bmad-help.md"), "x")

	var n int
	var err error
	captureStderr(t, func() {
		n, _, err = runSync([]Binding{NewMarkdownCommandBinding("alpha", "1", p)})
	})
	if err == nil {
		t.Fatal("a failed write exited zero")
	}
	if n != 0 {
		t.Errorf("synced = %d, want 0", n)
	}
	if _, ok := manifestByPath(t)["bmad-help.md"]; ok {
		t.Error("an unwritten file has a manifest entry")
	}
}

// T3: the next completed sync clears the incompleteness, removes the
// path nothing lists any more, and keeps a still-listed path.
func TestRunSync_CompletedSyncAfterAFailureClearsItAndReconciles(t *testing.T) {
	home := collisionEnv(t)
	a := skillPack(t, "shared-skill", "alpha text")
	writeFile(t, filepath.Join(a, ".claude", "skills", "alpha-only", "SKILL.md"), "only alpha")
	b, bad := failingPack(t, "beta text")

	if _, _, err := runSync([]Binding{NewSkillDirBinding("alpha", "1", a)}); err != nil {
		t.Fatal(err)
	}
	captureStderr(t, func() {
		_, _, _ = runSync([]Binding{NewSkillDirBinding("beta", "1", b), NewMarkdownCommandBinding("beta", "1", b)})
	})
	if err := os.Chmod(bad, 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	captureStderr(t, func() {
		_, _, err = runSync([]Binding{NewSkillDirBinding("beta", "1", b), NewMarkdownCommandBinding("beta", "1", b)})
	})
	if err != nil {
		t.Fatalf("completed sync: %v", err)
	}

	m, lerr := LoadManifest()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if !m.IsComplete() || len(m.Failed) != 0 {
		t.Errorf("manifest still incomplete: complete=%v failed=%+v", m.IsComplete(), m.Failed)
	}
	if m.Complete == nil || !*m.Complete {
		t.Error("a completed sync should write complete: true")
	}
	if _, serr := os.Stat(filepath.Join(home, ".claude", "skills", "alpha-only")); !os.IsNotExist(serr) {
		t.Errorf("stale alpha-only was not removed: %v", serr)
	}
	if _, serr := os.Stat(filepath.Join(home, ".claude", "skills", "shared-skill")); serr != nil {
		t.Errorf("a still-listed path was removed: %v", serr)
	}
}

// T4: a manifest written before the field existed reads as complete, and
// a saved one carries the bumped schema version.
func TestManifest_OldSchemaReadsAsComplete(t *testing.T) {
	collisionEnv(t)
	old := "schema_version: 0.1.0\nsynced_at: \"2026-10-01T00:00:00Z\"\nentries:\n  - pack: alpha\n    version: 1\n    kind: skill-dir\n    path: /x/y\n"
	writeFile(t, manifestPath(), old)
	m, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsComplete() || m.IncompleteNote() != "" {
		t.Errorf("0.1.0 manifest reads incomplete: %q", m.IncompleteNote())
	}

	if err := saveManifest(nil); err != nil {
		t.Fatal(err)
	}
	m, err = LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != "0.2.0" {
		t.Errorf("schema_version = %q, want 0.2.0", m.SchemaVersion)
	}
}
