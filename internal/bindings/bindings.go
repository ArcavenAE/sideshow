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

// Sync discovers and syncs every binding for every installed pack, printing
// a human-readable summary to stdout for CLI consumption.
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
	for _, p := range packs {
		discovered, err := DiscoverBindings(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: discover %s: %v\n", p.Name, err)
			continue
		}
		all = append(all, discovered...)
	}

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
