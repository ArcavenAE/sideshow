// Package bindings manages tool-config integrations that a content pack
// ships — flat .claude/commands/*.md files (bmad 6.2.2 era), .claude/skills/
// <name>/ directories (bmad 6.3.0 era), and future shapes like Cursor rules
// or Windsurf skills. One pack may carry several bindings.
package bindings

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// Binding is the integration surface between a pack and a tool-config
// target directory. Implementations discover their source content from a
// pack path at construction time; Sync writes the resolved content into
// the user's tool-config directory.
type Binding interface {
	// Kind returns a stable identifier like "markdown-command" or
	// "skill-dir" — used for diagnostics and for selecting sync targets.
	Kind() string

	// PackName returns the owning pack's name.
	PackName() string

	// PackVersion returns the owning pack's version.
	PackVersion() string

	// Sync installs the binding's artifacts into its tool-config target.
	// Returns the number of artifacts written.
	Sync() (int, error)

	// Artifacts returns the destination paths this binding owns — the
	// exact set Sync writes. Used for the sync-manifest ownership ledger
	// so stale artifacts from a previously active version are removed.
	Artifacts() ([]string, error)

	// Validate checks the binding is internally consistent.
	Validate() error
}

// DiscoverBindings inspects an installed pack and returns every binding it
// ships. A pack with both commands and skills content returns two
// bindings; a pack with neither returns zero.
func DiscoverBindings(p pack.InstalledPack) ([]Binding, error) {
	packPath, err := filepath.EvalSymlinks(p.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve pack path %s: %w", p.Path, err)
	}

	var result []Binding

	if hasMarkdownCommandContent(packPath) {
		result = append(result, NewMarkdownCommandBinding(p.Name, p.Version, packPath))
	}

	if hasSkillDirContent(packPath) {
		result = append(result, NewSkillDirBinding(p.Name, p.Version, packPath))
	}

	return result, nil
}

// Sync discovers and syncs every binding for every installed pack plus
// every registered custom source, printing a human-readable summary to
// stdout for CLI consumption. When run inside a consumer repo that has
// bindable custom skills for an installed pack, the repo is
// auto-registered as a custom source so later syncs from elsewhere
// keep its skills alive.
func Sync() error {
	packs, err := pack.List()
	if err != nil {
		return err
	}

	if len(packs) == 0 {
		fmt.Println("No packs installed. Run 'sideshow install <pack> --from <path>' first.")
		return nil
	}

	var all []Binding
	packSkillOwners := make(map[string]string)
	for _, p := range packs {
		// Plugin-class packs activate outside the binding sync
		// (per-repo, via their declared mechanism). Announce them
		// instead of silently counting zero bindings, and never let
		// their content leak into user-scope bindings.
		act, actErr := pack.LoadActivation(p.Path)
		if actErr != nil {
			// Fail closed: an unreadable activation block could be
			// hiding a per-repo-only declaration, so the pack is
			// excluded from user-scope sync entirely.
			fmt.Fprintf(os.Stderr, "ERROR: %s %s: activation unreadable (%v); excluded from user-scope sync\n",
				p.Name, p.Version, actErr)
			continue
		}
		if act.PluginClass() {
			fmt.Printf("%s %s: plugin-class pack (%s); bindings do not apply, see the pack's enablement runbook\n",
				p.Name, p.Version, act.Mechanism)
			continue
		}
		if act != nil && act.PerRepoRequired {
			fmt.Printf("%s %s: per-repo-required pack; user-scope bindings do not apply, see the pack's enablement runbook\n",
				p.Name, p.Version)
			continue
		}
		discovered, err := DiscoverBindings(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: discover %s: %v\n", p.Name, err)
			continue
		}
		all = append(all, discovered...)

		if resolved, evalErr := filepath.EvalSymlinks(p.Path); evalErr == nil {
			for id := range skillCanonicalIds(resolved) {
				packSkillOwners[id] = p.Name
			}
		}
	}

	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		autoRegisterCustomSources(cwd, packs)
	}

	custom, err := discoverCustomBindings(packs, packSkillOwners)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: discover custom sources: %v\n", err)
	}
	all = append(all, custom...)

	totalSynced, removed, err := runSync(all)
	if err != nil {
		return err
	}

	fmt.Printf("Synced %d artifacts across all bindings\n", totalSynced)
	if removed > 0 {
		fmt.Printf("Removed %d stale artifact(s) from a previously active version\n", removed)
	}
	return nil
}

// autoRegisterCustomSources registers the working directory as a custom
// source for every installed pack it carries bindable custom skills
// for. Best-effort: registration failures warn, never fail the sync.
func autoRegisterCustomSources(cwd string, packs []pack.InstalledPack) {
	for _, p := range packs {
		if !hasCustomSkillContent(cwd, p.Name) {
			continue
		}
		added, err := RegisterCustomSource(cwd, p.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: register custom source %s: %v\n", cwd, err)
			continue
		}
		if added {
			fmt.Printf("Registered custom source %s (pack %s)\n", cwd, p.Name)
		}
	}
}

// discoverCustomBindings loads the custom-source registry and returns a
// binding per source that still exists, has its pack installed, and has
// bindable skills. Skill names already owned by a pack (canonical id)
// or claimed by an earlier source are skipped with a warning — pack
// content wins collisions. Sources whose project directory no longer
// exists are pruned from the registry; their previously synced skills
// fall out via manifest reconciliation.
func discoverCustomBindings(packs []pack.InstalledPack, packSkillOwners map[string]string) ([]Binding, error) {
	reg, err := loadCustomSources()
	if err != nil {
		return nil, err
	}
	if len(reg.Sources) == 0 {
		return nil, nil
	}

	installed := make(map[string]struct{}, len(packs))
	for _, p := range packs {
		installed[p.Name] = struct{}{}
	}

	var kept []CustomSource
	pruned := false
	claimed := make(map[string]string) // skill name -> claiming project
	var out []Binding

	for _, s := range reg.Sources {
		if _, statErr := os.Stat(s.Project); statErr != nil {
			fmt.Fprintf(os.Stderr, "warning: custom source %s missing on disk — pruning from registry\n", s.Project)
			pruned = true
			continue
		}
		kept = append(kept, s)

		if _, ok := installed[s.Pack]; !ok {
			continue
		}

		var skills []string
		for _, name := range customSkillIds(s.Project, s.Pack) {
			if owner, ok := packSkillOwners[name]; ok {
				fmt.Fprintf(os.Stderr, "warning: skipping custom skill %s from %s: name owned by pack %s\n", name, s.Project, owner)
				continue
			}
			if prev, ok := claimed[name]; ok {
				fmt.Fprintf(os.Stderr, "warning: skipping custom skill %s from %s: already bound from %s\n", name, s.Project, prev)
				continue
			}
			claimed[name] = s.Project
			skills = append(skills, name)
		}
		if len(skills) == 0 {
			continue
		}
		out = append(out, NewCustomSkillDirBinding(s.Pack, s.Project, skills))
	}

	if pruned {
		if saveErr := saveCustomSources(kept); saveErr != nil {
			fmt.Fprintf(os.Stderr, "warning: save custom sources after prune: %v\n", saveErr)
		}
	}
	return out, nil
}

// runSync syncs every binding, then reconciles the sync manifest:
// artifacts recorded by the previous sync but owned by no current
// binding are removed (the stale-binding chimera fix — an activation
// flip no longer leaves the old version's extra skills behind).
func runSync(all []Binding) (synced, removed int, err error) {
	var current []ManifestEntry

	for _, b := range all {
		n, syncErr := b.Sync()
		if syncErr != nil {
			fmt.Fprintf(os.Stderr, "warning: sync %s/%s: %v\n", b.PackName(), b.Kind(), syncErr)
			continue
		}
		synced += n

		arts, artErr := b.Artifacts()
		if artErr != nil {
			fmt.Fprintf(os.Stderr, "warning: enumerate %s/%s artifacts: %v\n", b.PackName(), b.Kind(), artErr)
			continue
		}
		for _, a := range arts {
			current = append(current, ManifestEntry{
				Pack:    b.PackName(),
				Version: b.PackVersion(),
				Kind:    b.Kind(),
				Path:    a,
			})
		}
	}

	removed, err = reconcile(current)
	return synced, removed, err
}

// CountForPack returns the total discoverable artifacts across all bindings
// a pack ships (commands + skills + future types).
func CountForPack(_, packPath string) (int, error) {
	resolved, err := filepath.EvalSymlinks(packPath)
	if err != nil {
		return 0, err
	}

	total := 0
	if hasMarkdownCommandContent(resolved) {
		total += countMarkdownCommands(resolved)
	}
	if hasSkillDirContent(resolved) {
		total += countSkillDirs(resolved)
	}
	return total, nil
}

// SyncedCount returns the total number of artifacts currently synced to
// tool-config directories for this pack across all binding types.
// Ownership is determined by the canonical-id / basename set the pack
// ships at packPath — not by a name prefix — so packs that ship
// multi-prefix bindings (bmad ships bmad-* and gds-*) are accounted
// fully. The first arg is reserved for future per-pack lookup hints
// (e.g. a registry-stored manifest of what was actually written) and
// is unused today; pass the pack name for forward compatibility.
func SyncedCount(_, packPath string) (int, error) {
	resolved, err := filepath.EvalSymlinks(packPath)
	if err != nil {
		return 0, err
	}

	total := 0

	c, err := countSyncedCommands(resolved)
	if err != nil {
		return 0, err
	}
	total += c

	s, err := countSyncedSkills(resolved)
	if err != nil {
		return 0, err
	}
	total += s

	return total, nil
}

// claudeCommandsDir returns the Claude Code commands directory under $HOME.
func claudeCommandsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "commands")
}

// claudeSkillsDir returns the Claude Code skills directory under $HOME.
func claudeSkillsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "skills")
}
