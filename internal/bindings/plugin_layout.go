package bindings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// ErrPluginLayout marks a pack whose tree is plugin-shaped
// (.claude-plugin/plugin.json plus top-level content dirs). Such packs
// activate per repo through the unshaping path (docs/unshaping-spec.md)
// and must never be swept into user-scope binding sync: their skills
// alone would put a hundred-plus entries into $HOME/.claude with no
// per-repo consent.
var ErrPluginLayout = errors.New("plugin-layout pack: activates per repo, not via user-scope sync")

// pluginContentDirs are the top-level directories whose presence
// (beside .claude-plugin/plugin.json) identifies the plugin layout.
var pluginContentDirs = []string{"skills", "agents", "hooks", "commands"}

// pluginExcludedSkills are upstream skill dirs the unshaping spec
// replaces rather than materializes: both write into the frozen shared
// store (activate renders hooks.json into it, deactivate deletes it).
// Sideshow-authored replacements ship separately (aae-orc-d3nq.60).
var pluginExcludedSkills = map[string]bool{
	"activate":   true,
	"deactivate": true,
}

// IsPluginLayout reports whether packPath holds a plugin-shaped tree:
// .claude-plugin/plugin.json at the root plus at least one recognized
// top-level content directory.
func IsPluginLayout(packPath string) bool {
	manifest := filepath.Join(packPath, ".claude-plugin", "plugin.json")
	if info, err := os.Stat(manifest); err != nil || info.IsDir() {
		return false
	}
	for _, dir := range pluginContentDirs {
		if info, err := os.Stat(filepath.Join(packPath, dir)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// PluginInventory is the discovery surface of a plugin-shaped tree:
// the units the unshaping spec materializes into a repo (skills,
// agents) plus the hook platform templates the enable path reads to
// derive the settings event set. Store-referenced units (bin/,
// hook-plugins/, templates/, ...) are deliberately absent: they never
// enter a repo (docs/unshaping-spec.md).
type PluginInventory struct {
	// SkillDirs are top-level skills/<name>/ entries carrying a
	// SKILL.md, relative to the pack root, excluded pairs removed.
	SkillDirs []string
	// ExcludedSkills are the replace-disposition skill dirs found in
	// the tree (skills/activate, skills/deactivate when present).
	ExcludedSkills []string
	// AgentFiles are every file under agents/, relative to the pack
	// root. Nested layouts are preserved: the harness loads them and
	// addresses agents by bare frontmatter name (trial T20).
	AgentFiles []string
	// HookTemplates are hooks/hooks.json.<platform> template paths,
	// relative to the pack root.
	HookTemplates []string
}

// DiscoverPluginLayout inventories a plugin-shaped pack for the
// per-repo enable path.
//
// CONTAINMENT: this function must only be called from an explicit
// repo-scope enable request naming one repo. It is intentionally not
// called from DiscoverBindings or Sync; those refuse plugin-layout
// packs with ErrPluginLayout, and TestSync_RefusesPluginLayoutPack
// locks the refusal in.
func DiscoverPluginLayout(p pack.InstalledPack) (*PluginInventory, error) {
	packPath, err := filepath.EvalSymlinks(p.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve pack path %s: %w", p.Path, err)
	}
	if !IsPluginLayout(packPath) {
		return nil, fmt.Errorf("%s %s: not a plugin-layout tree", p.Name, p.Version)
	}

	inv := &PluginInventory{}

	skillEntries, err := os.ReadDir(filepath.Join(packPath, "skills"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	for _, e := range skillEntries {
		if !e.IsDir() {
			continue
		}
		rel := filepath.Join("skills", e.Name())
		if pluginExcludedSkills[e.Name()] {
			inv.ExcludedSkills = append(inv.ExcludedSkills, rel)
			continue
		}
		if _, err := os.Stat(filepath.Join(packPath, rel, "SKILL.md")); err == nil {
			inv.SkillDirs = append(inv.SkillDirs, rel)
		}
	}

	agentsRoot := filepath.Join(packPath, "agents")
	if _, err := os.Stat(agentsRoot); err == nil {
		walkErr := filepath.WalkDir(agentsRoot, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(packPath, path)
			if relErr != nil {
				return relErr
			}
			inv.AgentFiles = append(inv.AgentFiles, rel)
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("walk agents dir: %w", walkErr)
		}
	}

	hookEntries, err := os.ReadDir(filepath.Join(packPath, "hooks"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read hooks dir: %w", err)
	}
	for _, e := range hookEntries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "hooks.json.") {
			inv.HookTemplates = append(inv.HookTemplates, filepath.Join("hooks", e.Name()))
		}
	}

	sort.Strings(inv.SkillDirs)
	sort.Strings(inv.ExcludedSkills)
	sort.Strings(inv.AgentFiles)
	sort.Strings(inv.HookTemplates)
	return inv, nil
}
