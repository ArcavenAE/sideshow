package bindings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

func TestCustomSkillDirBinding_Kind(t *testing.T) {
	t.Parallel()
	b := NewCustomSkillDirBinding("bmad", "/tmp/whatever", []string{"eos-coach"})
	if b.Kind() != "custom-skill-dir" {
		t.Errorf("Kind() = %q, want %q", b.Kind(), "custom-skill-dir")
	}
	if b.PackVersion() != "custom" {
		t.Errorf("PackVersion() = %q, want %q", b.PackVersion(), "custom")
	}
}

func TestCustomSkillIds(t *testing.T) {
	t.Parallel()
	project := t.TempDir()
	base := filepath.Join(project, "_bmad-custom", "skills")

	writeFile(t, filepath.Join(base, "eos-coach", "SKILL.md"), "# coach\n")
	writeFile(t, filepath.Join(base, "zeta", "SKILL.md"), "# zeta\n")
	writeFile(t, filepath.Join(base, "half-authored", "notes.md"), "no entry point\n")
	writeFile(t, filepath.Join(base, "stray-file.md"), "not a dir\n")

	got := customSkillIds(project, "bmad")
	want := []string{"eos-coach", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("customSkillIds = %v, want %v", got, want)
	}

	if customSkillIds(t.TempDir(), "bmad") != nil {
		t.Error("customSkillIds on empty project should be nil")
	}
	if !hasCustomSkillContent(project, "bmad") {
		t.Error("hasCustomSkillContent = false, want true")
	}
}

func TestCustomSkillDirBinding_Sync_CopiesVerbatim(t *testing.T) {
	project := t.TempDir()
	destRoot := t.TempDir()
	t.Setenv("HOME", destRoot)

	base := filepath.Join(project, "_bmad-custom", "skills", "eos-coach")
	content := "# Coach\nReads {project-root}/_bmad/config.yaml verbatim\n"
	writeFile(t, filepath.Join(base, "SKILL.md"), content)
	writeFile(t, filepath.Join(base, "nested", "helper.md"), "helper\n")

	b := NewCustomSkillDirBinding("bmad", project, []string{"eos-coach"})
	n, err := b.Sync()
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 1 {
		t.Errorf("Sync synced %d, want 1", n)
	}

	got, err := os.ReadFile(filepath.Join(destRoot, ".claude", "skills", "eos-coach", "SKILL.md"))
	if err != nil {
		t.Fatalf("read synced SKILL.md: %v", err)
	}
	if string(got) != content {
		t.Errorf("SKILL.md not copied verbatim:\ngot:  %q\nwant: %q", got, content)
	}
	if strings.Contains(string(got), "fallback") {
		t.Error("custom skill must not receive the pack fallback footer")
	}
	if _, err := os.Stat(filepath.Join(destRoot, ".claude", "skills", "eos-coach", "nested", "helper.md")); err != nil {
		t.Errorf("nested file not copied: %v", err)
	}
}

func TestCustomSkillDirBinding_ArtifactsAndValidate(t *testing.T) {
	project := t.TempDir()
	destRoot := t.TempDir()
	t.Setenv("HOME", destRoot)

	base := filepath.Join(project, "_bmad-custom", "skills")
	writeFile(t, filepath.Join(base, "eos-coach", "SKILL.md"), "# coach\n")

	b := NewCustomSkillDirBinding("bmad", project, []string{"eos-coach"})
	arts, err := b.Artifacts()
	if err != nil {
		t.Fatalf("Artifacts: %v", err)
	}
	want := []string{filepath.Join(destRoot, ".claude", "skills", "eos-coach")}
	if !reflect.DeepEqual(arts, want) {
		t.Errorf("Artifacts = %v, want %v", arts, want)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}

	missing := NewCustomSkillDirBinding("bmad", project, []string{"ghost"})
	if err := missing.Validate(); err == nil {
		t.Error("Validate should fail for a skill without SKILL.md")
	}
}

func TestDiscoverCustomBindings_CollisionsAndPrune(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SIDESHOW_HOME", dataDir)

	projectA := t.TempDir()
	projectB := t.TempDir()
	writeFile(t, filepath.Join(projectA, "_bmad-custom", "skills", "eos-coach", "SKILL.md"), "# a\n")
	writeFile(t, filepath.Join(projectA, "_bmad-custom", "skills", "bmad-agent-dev", "SKILL.md"), "# collides with pack\n")
	writeFile(t, filepath.Join(projectB, "_bmad-custom", "skills", "eos-coach", "SKILL.md"), "# b duplicate\n")
	writeFile(t, filepath.Join(projectB, "_bmad-custom", "skills", "b-only", "SKILL.md"), "# b\n")

	for _, p := range []string{projectA, projectB, filepath.Join(dataDir, "gone-project")} {
		if _, err := RegisterCustomSource(p, "bmad"); err != nil {
			t.Fatalf("register %s: %v", p, err)
		}
	}
	// Re-register is a no-op.
	added, err := RegisterCustomSource(projectA, "bmad")
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if added {
		t.Error("re-register should not add a duplicate")
	}

	packs := []pack.InstalledPack{{Name: "bmad", Version: "6.3.0"}}
	owners := map[string]string{"bmad-agent-dev": "bmad"}

	got, err := discoverCustomBindings(packs, owners)
	if err != nil {
		t.Fatalf("discoverCustomBindings: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d bindings, want 2", len(got))
	}

	a := got[0].(*CustomSkillDirBinding)
	b := got[1].(*CustomSkillDirBinding)
	if !reflect.DeepEqual(a.skills, []string{"eos-coach"}) {
		t.Errorf("project A skills = %v, want [eos-coach] (pack collision must be skipped)", a.skills)
	}
	if !reflect.DeepEqual(b.skills, []string{"b-only"}) {
		t.Errorf("project B skills = %v, want [b-only] (cross-source duplicate must be skipped)", b.skills)
	}

	// The missing project must have been pruned from the registry.
	sources, err := ListCustomSources()
	if err != nil {
		t.Fatalf("ListCustomSources: %v", err)
	}
	if len(sources) != 2 {
		t.Errorf("registry has %d sources after prune, want 2: %v", len(sources), sources)
	}
	for _, s := range sources {
		if strings.Contains(s.Project, "gone-project") {
			t.Error("missing project not pruned from registry")
		}
	}
}

func TestDiscoverCustomBindings_PackNotInstalled(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	project := t.TempDir()
	writeFile(t, filepath.Join(project, "_spectacle-custom", "skills", "spec-thing", "SKILL.md"), "# s\n")
	if _, err := RegisterCustomSource(project, "spectacle"); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := discoverCustomBindings([]pack.InstalledPack{{Name: "bmad"}}, nil)
	if err != nil {
		t.Fatalf("discoverCustomBindings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d bindings for uninstalled pack, want 0", len(got))
	}
}

func TestRunSync_RemovesCustomSkillWhenSourceGone(t *testing.T) {
	destRoot := t.TempDir()
	t.Setenv("HOME", destRoot)
	t.Setenv("SIDESHOW_HOME", t.TempDir())

	project := t.TempDir()
	writeFile(t, filepath.Join(project, "_bmad-custom", "skills", "eos-coach", "SKILL.md"), "# coach\n")

	b := NewCustomSkillDirBinding("bmad", project, []string{"eos-coach"})
	if _, _, err := runSync([]Binding{b}); err != nil {
		t.Fatalf("first runSync: %v", err)
	}
	synced := filepath.Join(destRoot, ".claude", "skills", "eos-coach")
	if _, err := os.Stat(synced); err != nil {
		t.Fatalf("custom skill not synced: %v", err)
	}

	// Next sync discovers no custom binding (source dropped) —
	// reconcile must remove the stale skill.
	_, removed, err := runSync(nil)
	if err != nil {
		t.Fatalf("second runSync: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(synced); !os.IsNotExist(err) {
		t.Errorf("stale custom skill still present: %v", err)
	}
}
