package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// ClaudeCommandsDir returns the global Claude Code commands directory.
func ClaudeCommandsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "commands")
}

// Sync generates command files in ~/.claude/commands/ for all installed packs.
// Command files are rewritten so agent/workflow references point to the
// global sideshow installation rather than {project-root}/_bmad/.
func Sync() error {
	packs, err := pack.List()
	if err != nil {
		return err
	}

	if len(packs) == 0 {
		fmt.Println("No packs installed. Run 'sideshow install <pack> --from <path>' first.")
		return nil
	}

	claudeDir := ClaudeCommandsDir()
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create commands dir: %w", err)
	}

	totalSynced := 0

	for _, p := range packs {
		synced, err := syncPack(p, claudeDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: sync %s: %v\n", p.Name, err)
			continue
		}
		totalSynced += synced
	}

	fmt.Printf("Synced %d commands to %s\n", totalSynced, claudeDir)
	return nil
}

// syncPack finds command files in the pack and installs them with rewritten paths.
func syncPack(p pack.InstalledPack, claudeDir string) (int, error) {
	// Resolve the pack path (follow current symlink)
	packPath, err := filepath.EvalSymlinks(p.Path)
	if err != nil {
		return 0, fmt.Errorf("resolve pack path: %w", err)
	}

	// Find command files — look in commands/ subdirectory or .claude/commands/
	commandDirs := []string{
		filepath.Join(packPath, "commands"),
	}

	// Also check if there's a sibling .claude/commands/ from the source repo
	// The pack might have been installed from a repo that had commands in .claude/
	parentDir := filepath.Dir(filepath.Dir(packPath)) // go up from packs/{name}/{version}
	_ = parentDir                                      // not used for now

	synced := 0
	for _, cmdDir := range commandDirs {
		info, err := os.Stat(cmdDir)
		if err != nil || !info.IsDir() {
			continue
		}

		err = filepath.WalkDir(cmdDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}

			if !strings.HasSuffix(path, ".md") {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read command %s: %w", path, err)
			}

			// Rewrite paths: {project-root}/_bmad/ → pack installation path
			content := string(data)
			content = rewritePaths(content, packPath)

			destName := filepath.Base(path)
			destPath := filepath.Join(claudeDir, destName)

			if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write command %s: %w", destPath, err)
			}

			synced++
			return nil
		})
		if err != nil {
			return synced, err
		}
	}

	// Also look for command files that were in the source repo's .claude/commands/
	// These would have been installed as part of the pack if the source included them
	// For bmad: the commands are typically in the repo's .claude/commands/, not in _bmad/commands/
	// We need to handle this case — search for bmad-*.md files anywhere in the pack
	err = filepath.WalkDir(packPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		name := filepath.Base(path)
		if !strings.HasPrefix(name, "bmad-") || !strings.HasSuffix(name, ".md") {
			return nil
		}

		// Skip if we already processed this from a commands/ directory
		rel, _ := filepath.Rel(packPath, path)
		if strings.HasPrefix(rel, "commands/") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip on read error
		}

		content := string(data)
		content = rewritePaths(content, packPath)

		destPath := filepath.Join(claudeDir, name)
		if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
			return nil // skip on write error
		}

		synced++
		return nil
	})

	return synced, err
}

// rewritePaths replaces {project-root}/_bmad/ references with the global pack path.
// Preserves {project-root}/_bmad-custom/ and {project-root}/_bmad-output/ references
// since those are per-repo.
func rewritePaths(content, packPath string) string {
	// Replace _bmad/ references with global path, but NOT _bmad-custom/ or _bmad-output/
	// The pattern in commands is: {project-root}/_bmad/module/path
	// We want: ~/.local/share/sideshow/packs/bmad/current/module/path

	// Use the packPath which resolves the symlink
	globalPath := packPath

	// Replace patterns like:
	//   {project-root}/_bmad/tea/agents/tea.md
	// With:
	//   /absolute/path/to/sideshow/packs/bmad/6.2.2/tea/agents/tea.md
	content = strings.ReplaceAll(content, "{project-root}/_bmad/", globalPath+"/")

	return content
}

// CountForPack counts command files available in a pack.
func CountForPack(name, packPath string) (int, error) {
	resolved, err := filepath.EvalSymlinks(packPath)
	if err != nil {
		return 0, err
	}

	count := 0
	_ = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "bmad-") && strings.HasSuffix(base, ".md") {
			count++
		}
		return nil
	})
	return count, nil
}

// SyncedCount counts how many commands are synced for a pack.
func SyncedCount(name string) (int, error) {
	claudeDir := ClaudeCommandsDir()
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	prefix := name + "-"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".md") {
			count++
		}
	}
	return count, nil
}
