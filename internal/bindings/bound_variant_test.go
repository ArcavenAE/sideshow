package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// writeBoundFixtureStore mirrors the upstream vsdd-factory shapes the
// transforms exist for: a generic-named skill (T16: user-scope
// shadowing makes prefixing correctness-critical), namespace-qualified
// slash and subagent_type references, prose with a bare "pack: " that
// must survive, an executable helper, and nested agents.
func writeBoundFixtureStore(t *testing.T) string {
	t.Helper()
	store := t.TempDir()
	mustWrite := func(rel, content string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(store, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite("skills/jira/SKILL.md",
		"---\nname: jira\ndescription: generic name upstream\n---\n\n"+
			"Run /vsdd-factory:github-ops first. Note that vsdd-factory: a plugin, ships this.\n"+
			"name: jira appears in the body too.\n", 0o644)
	mustWrite("skills/jira/bin/helper.sh", "#!/bin/sh\necho vsdd-factory:github-ops\n", 0o755)
	mustWrite("skills/jira/ref.csv", "a,b\n", 0o644)
	mustWrite("agents/github-ops.md",
		"---\nname: github-ops\n---\n\nI am spawned as subagent_type=\"vsdd-factory:github-ops\".\n", 0o644)
	mustWrite("agents/orchestrator/planner.md",
		"---\nname: planner\n---\n\nDelegate via Agent(subagent_type=\"vsdd-factory:pr-manager\").\n", 0o644)
	mustWrite("exec-manifest.txt", "skills/jira/bin/helper.sh\nbin/never-materialized.sh\n", 0o644)

	return store
}

func boundFixtureInventory() *PluginInventory {
	return &PluginInventory{
		SkillDirs:  []string{"skills/jira"},
		AgentFiles: []string{"agents/github-ops.md", "agents/orchestrator/planner.md"},
	}
}

func renderFixtureVariant(t *testing.T) (string, *PluginInventory) {
	t.Helper()
	store := writeBoundFixtureStore(t)
	dest := filepath.Join(t.TempDir(), "bound", "vsdd-factory", "1.0.0-rc.23")
	out, err := RenderBoundVariant(store, boundFixtureInventory(), "vsdd-factory", "vsdd", dest)
	if err != nil {
		t.Fatalf("RenderBoundVariant: %v", err)
	}
	return dest, out
}

func TestRenderBoundVariant_PrefixesNamesAndInventory(t *testing.T) {
	t.Parallel()
	dest, out := renderFixtureVariant(t)

	if got, want := out.SkillDirs, []string{"skills/vsdd-jira"}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("SkillDirs = %v, want %v", got, want)
	}
	wantAgents := map[string]bool{
		"agents/vsdd-github-ops.md":           true,
		"agents/vsdd-orchestrator/planner.md": true,
	}
	for _, a := range out.AgentFiles {
		if !wantAgents[a] {
			t.Errorf("unexpected agent path %s", a)
		}
	}
	if len(out.AgentFiles) != 2 {
		t.Errorf("AgentFiles = %v, want 2 entries", out.AgentFiles)
	}
	if _, err := os.Stat(filepath.Join(dest, "skills", "vsdd-jira", "SKILL.md")); err != nil {
		t.Errorf("prefixed skill dir missing: %v", err)
	}
}

func TestRenderBoundVariant_RewritesFrontmatterAndReferences(t *testing.T) {
	t.Parallel()
	dest, _ := renderFixtureVariant(t)

	skill, err := os.ReadFile(filepath.Join(dest, "skills", "vsdd-jira", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(skill)
	if !strings.Contains(s, "name: vsdd-jira\n") {
		t.Error("skill frontmatter name not prefixed")
	}
	if !strings.Contains(s, "/vsdd-github-ops first") {
		t.Error("slash-form namespace reference not rewritten")
	}
	if !strings.Contains(s, "vsdd-factory: a plugin") {
		t.Error("prose 'pack: ' false positive: bare mention was rewritten")
	}
	if !strings.Contains(s, "name: jira appears in the body") {
		t.Error("body name: line rewritten; only frontmatter should change")
	}

	agent, err := os.ReadFile(filepath.Join(dest, "agents", "vsdd-github-ops.md"))
	if err != nil {
		t.Fatal(err)
	}
	a := string(agent)
	if !strings.Contains(a, "name: vsdd-github-ops\n") {
		t.Error("agent frontmatter name not prefixed")
	}
	if !strings.Contains(a, `subagent_type="vsdd-github-ops"`) {
		t.Errorf("subagent_type reference not rewritten:\n%s", a)
	}

	nested, err := os.ReadFile(filepath.Join(dest, "agents", "vsdd-orchestrator", "planner.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nested), `subagent_type="vsdd-pr-manager"`) {
		t.Error("nested agent namespace reference not rewritten")
	}
	if !strings.Contains(string(nested), "name: vsdd-planner\n") {
		t.Error("nested agent frontmatter name not prefixed")
	}
}

func TestRenderBoundVariant_PreservesExecBitsAndTranslatesCensus(t *testing.T) {
	t.Parallel()
	dest, _ := renderFixtureVariant(t)

	helper := filepath.Join(dest, "skills", "vsdd-jira", "bin", "helper.sh")
	info, err := os.Stat(helper)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("variant helper lost exec bit: %o", info.Mode().Perm())
	}
	// Executable content still gets the namespace rewrite (.sh is not
	// a rewrite extension, so it stays byte-identical to the store).
	data, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "vsdd-factory:github-ops") {
		t.Error("non-rewrite-extension file was transformed; shell content must stay byte-identical")
	}

	census, err := os.ReadFile(filepath.Join(dest, execManifestName))
	if err != nil {
		t.Fatalf("translated census missing: %v", err)
	}
	got := strings.TrimSpace(string(census))
	if got != "skills/vsdd-jira/bin/helper.sh" {
		t.Errorf("translated census = %q, want the prefixed materialized entry only", got)
	}
}

func TestRenderBoundVariant_RebuildIsIdempotent(t *testing.T) {
	t.Parallel()
	store := writeBoundFixtureStore(t)
	dest := filepath.Join(t.TempDir(), "bound-variant")

	if _, err := RenderBoundVariant(store, boundFixtureInventory(), "vsdd-factory", "vsdd", dest); err != nil {
		t.Fatal(err)
	}
	first := snapshotTree(t, dest)
	if _, err := RenderBoundVariant(store, boundFixtureInventory(), "vsdd-factory", "vsdd", dest); err != nil {
		t.Fatal(err)
	}
	second := snapshotTree(t, dest)
	if diff := diffSnapshots(first, second); len(diff) > 0 {
		t.Errorf("re-render not deterministic:\n  %s", strings.Join(diff, "\n  "))
	}
}

// The .51 → .48 composition: the variant feeds MaterializeRepo
// unchanged, local scope symlinks resolve into the variant, and the
// exact-inverse property holds end to end.
func TestBoundVariant_ComposesWithMaterializeRepo(t *testing.T) {
	t.Parallel()
	dest, out := renderFixtureVariant(t)
	repo := writeRepoWithUserContent(t)
	target := RepoTarget{RepoDir: repo, Scope: ScopeLocal}

	before := snapshotTree(t, repo)
	artifacts, err := MaterializeRepo(dest, out, target)
	if err != nil {
		t.Fatalf("MaterializeRepo from variant: %v", err)
	}

	link := filepath.Join(repo, ".claude", "skills", "vsdd-jira")
	targetPath, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("prefixed skill not symlinked: %v", err)
	}
	if want := filepath.Join(dest, "skills", "vsdd-jira"); targetPath != want {
		t.Errorf("symlink target = %s, want the bound variant %s", targetPath, want)
	}

	if _, err := RemoveRepoArtifacts(target, artifacts); err != nil {
		t.Fatal(err)
	}
	if diff := diffSnapshots(before, snapshotTree(t, repo)); len(diff) > 0 {
		t.Errorf("variant materialization not exactly inverted:\n  %s", strings.Join(diff, "\n  "))
	}
}

// Real-tree check (env-gated like TestDiscoverPluginLayout_RealTree):
// after rendering the variant from an actual vsdd-factory tree, zero
// namespace-qualified references survive in rewrite-extension files —
// the full slash-form + subagent_type surface is covered.
func TestRenderBoundVariant_RealTreeLeavesNoQualifiedReferences(t *testing.T) {
	tree := os.Getenv("VSDD_TREE")
	if tree == "" {
		t.Skip("VSDD_TREE not set; real-tree rewrite check skipped")
	}
	inv, err := DiscoverPluginLayout(pack.InstalledPack{Name: "vsdd-factory", Version: "real-tree", Path: tree})
	if err != nil {
		t.Fatalf("DiscoverPluginLayout: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "bound")
	if _, err := RenderBoundVariant(tree, inv, "vsdd-factory", "vsdd", dest); err != nil {
		t.Fatalf("RenderBoundVariant: %v", err)
	}

	var leftovers []string
	err = filepath.WalkDir(dest, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || !shouldRewrite(path) {
			return werr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if idx := strings.Index(line, "vsdd-factory:"); idx >= 0 &&
				idx+len("vsdd-factory:") < len(line) && isNameChar(line[idx+len("vsdd-factory:")]) {
				rel, _ := filepath.Rel(dest, path)
				leftovers = append(leftovers, rel+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) > 0 {
		t.Errorf("%d namespace-qualified references survived the rewrite:\n  %s",
			len(leftovers), strings.Join(leftovers, "\n  "))
	}
}

func isNameChar(c byte) bool {
	return c == '-' || c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func TestRewriteFrontmatterName_Idempotent(t *testing.T) {
	t.Parallel()
	in := "---\nname: vsdd-jira\n---\nbody\n"
	if got := rewriteFrontmatterName(in, "vsdd"); got != in {
		t.Errorf("already-prefixed name double-prefixed: %q", got)
	}
	noFM := "no frontmatter here\nname: jira\n"
	if got := rewriteFrontmatterName(noFM, "vsdd"); got != noFM {
		t.Errorf("content without frontmatter modified: %q", got)
	}
}
