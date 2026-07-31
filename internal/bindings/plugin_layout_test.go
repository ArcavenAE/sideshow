package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// writePluginTree lays down a minimal plugin-shaped tree: manifest,
// three skills (one excluded pair member), flat + nested agents, hook
// platform templates, and a decoy .claude/skills dir that user-scope
// discovery would bind if the layout refusal failed.
func writePluginTree(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"vsdd-factory","version":"1.0.0-rc.23"}`)
	writeFile(t, filepath.Join(root, "skills", "pr-manager", "SKILL.md"), "# pr-manager\n")
	writeFile(t, filepath.Join(root, "skills", "check-state-health", "SKILL.md"), "# check-state-health\n")
	writeFile(t, filepath.Join(root, "skills", "activate", "SKILL.md"), "# activate\n")
	writeFile(t, filepath.Join(root, "skills", "deactivate", "SKILL.md"), "# deactivate\n")
	writeFile(t, filepath.Join(root, "skills", "no-entrypoint", "notes.txt"), "not a skill\n")
	writeFile(t, filepath.Join(root, "agents", "adversary.md"), "---\nname: adversary\n---\n")
	writeFile(t, filepath.Join(root, "agents", "orchestrator", "orchestrator.md"), "---\nname: orchestrator\n---\n")
	writeFile(t, filepath.Join(root, "agents", "orchestrator", "sequence-01.md"), "# seq\n")
	writeFile(t, filepath.Join(root, "hooks", "hooks.json.darwin-arm64"), "{}\n")
	writeFile(t, filepath.Join(root, "hooks", "hooks.json.template"), "{}\n")
	writeFile(t, filepath.Join(root, "hooks", "guard.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(root, ".claude", "skills", "decoy", "SKILL.md"), "# decoy\n")
}

func TestIsPluginLayout(t *testing.T) {
	root := t.TempDir()
	if IsPluginLayout(root) {
		t.Error("empty dir reported as plugin layout")
	}
	writePluginTree(t, root)
	if !IsPluginLayout(root) {
		t.Error("plugin tree not recognized")
	}

	// Manifest alone, no content dirs: not the layout.
	bare := t.TempDir()
	writeFile(t, filepath.Join(bare, ".claude-plugin", "plugin.json"), "{}")
	if IsPluginLayout(bare) {
		t.Error("manifest without content dirs reported as plugin layout")
	}

	// Content dirs without the manifest: not the layout (bmad-shaped
	// packs have .claude/ content and must keep binding normally).
	noManifest := t.TempDir()
	writeFile(t, filepath.Join(noManifest, "skills", "x", "SKILL.md"), "# x\n")
	if IsPluginLayout(noManifest) {
		t.Error("content dirs without plugin.json reported as plugin layout")
	}
}

func TestDiscoverPluginLayout_Inventory(t *testing.T) {
	root := t.TempDir()
	writePluginTree(t, root)

	inv, err := DiscoverPluginLayout(pack.InstalledPack{Name: "vsdd-factory", Version: "1.0.0-rc.23", Path: root})
	if err != nil {
		t.Fatalf("DiscoverPluginLayout: %v", err)
	}

	wantSkills := []string{"skills/check-state-health", "skills/pr-manager"}
	if len(inv.SkillDirs) != len(wantSkills) {
		t.Fatalf("SkillDirs = %v, want %v", inv.SkillDirs, wantSkills)
	}
	for i, w := range wantSkills {
		if inv.SkillDirs[i] != w {
			t.Errorf("SkillDirs[%d] = %q, want %q", i, inv.SkillDirs[i], w)
		}
	}

	wantExcluded := []string{"skills/activate", "skills/deactivate"}
	if len(inv.ExcludedSkills) != 2 || inv.ExcludedSkills[0] != wantExcluded[0] || inv.ExcludedSkills[1] != wantExcluded[1] {
		t.Errorf("ExcludedSkills = %v, want %v", inv.ExcludedSkills, wantExcluded)
	}

	wantAgents := []string{"agents/adversary.md", "agents/orchestrator/orchestrator.md", "agents/orchestrator/sequence-01.md"}
	if len(inv.AgentFiles) != len(wantAgents) {
		t.Fatalf("AgentFiles = %v, want %v", inv.AgentFiles, wantAgents)
	}
	for i, w := range wantAgents {
		if inv.AgentFiles[i] != w {
			t.Errorf("AgentFiles[%d] = %q, want %q", i, inv.AgentFiles[i], w)
		}
	}

	wantHooks := []string{"hooks/hooks.json.darwin-arm64", "hooks/hooks.json.template"}
	if len(inv.HookTemplates) != 2 || inv.HookTemplates[0] != wantHooks[0] || inv.HookTemplates[1] != wantHooks[1] {
		t.Errorf("HookTemplates = %v, want %v", inv.HookTemplates, wantHooks)
	}
}

// The containment lock (aae-orc-d3nq.49): a plugin-layout pack with NO
// pack.yaml (so the activation-block guard cannot fire) and a decoy
// .claude/skills dir that would otherwise bind is still refused by
// user-scope sync with zero writes.
func TestSync_RefusesPluginLayoutPack(t *testing.T) {
	home := t.TempDir()
	store := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", store)

	packPath := filepath.Join(store, "packs", "vsdd-factory", "1.0.0-rc.23")
	writePluginTree(t, packPath)
	registerTestPack(t, store, "vsdd-factory", "1.0.0-rc.23", packPath)

	_, stderr, err := captureOutput(t, Sync)
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
	if !strings.Contains(stderr, "plugin-layout tree") {
		t.Errorf("stderr missing plugin-layout refusal, got: %q", stderr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "skills")); !os.IsNotExist(statErr) {
		t.Errorf("user-scope skills dir exists: plugin-layout pack content leaked into user scope")
	}
}

// Env-gated check against a real vsdd-factory tree. Run manually:
//
//	VSDD_TREE=/path/to/vsdd-factory/plugins/vsdd-factory go test ./internal/bindings/ -run RealTree
//
// At v1.0.0-rc.23 the expected counts are 124 skill dirs (126 minus
// the activate/deactivate pair), 44 agent files, 6 hook templates.
func TestDiscoverPluginLayout_RealTree(t *testing.T) {
	tree := os.Getenv("VSDD_TREE")
	if tree == "" {
		t.Skip("VSDD_TREE not set; real-tree inventory check skipped")
	}
	inv, err := DiscoverPluginLayout(pack.InstalledPack{Name: "vsdd-factory", Version: "real-tree", Path: tree})
	if err != nil {
		t.Fatalf("DiscoverPluginLayout: %v", err)
	}
	if got := len(inv.SkillDirs); got != 124 {
		t.Errorf("SkillDirs = %d, want 124", got)
	}
	if got := len(inv.ExcludedSkills); got != 2 {
		t.Errorf("ExcludedSkills = %d, want 2", got)
	}
	if got := len(inv.AgentFiles); got != 44 {
		t.Errorf("AgentFiles = %d, want 44", got)
	}
	if got := len(inv.HookTemplates); got != 6 {
		t.Errorf("HookTemplates = %d, want 6", got)
	}
}
