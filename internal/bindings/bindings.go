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

	totalSynced := 0
	for _, p := range packs {
		bindings, err := DiscoverBindings(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: discover %s: %v\n", p.Name, err)
			continue
		}

		for _, b := range bindings {
			n, err := b.Sync()
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: sync %s/%s: %v\n", p.Name, b.Kind(), err)
				continue
			}
			totalSynced += n
		}
	}

	fmt.Printf("Synced %d artifacts across all bindings\n", totalSynced)
	return nil
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
func SyncedCount(packName string) (int, error) {
	total := 0

	c, err := countSyncedCommands(packName)
	if err != nil {
		return 0, err
	}
	total += c

	s, err := countSyncedSkills(packName)
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
