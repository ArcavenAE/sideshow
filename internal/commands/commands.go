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
	_ = parentDir                                     // not used for now

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
			content = appendFallbackFooter(content, packPath)

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
		content = appendFallbackFooter(content, packPath)

		destPath := filepath.Join(claudeDir, name)
		if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
			return nil // skip on write error
		}

		synced++
		return nil
	})

	return synced, err
}

// rewritePaths replaces pack content references with the global installation path.
//
// Rewrites (global — pack content, read-only):
//
//	{project-root}/_bmad/module/path → /absolute/sideshow/packs/bmad/6.2.2/module/path
//
// Preserves (per-repo — stays relative to the invoking project):
//
//	{project-root}/_bmad-custom/  → unchanged (per-repo customization)
//	{project-root}/_bmad-output/  → unchanged (per-repo output)
//	{project-root}/               → unchanged (any other project-relative path)
//
// This means agents and workflows load their definitions from the global
// installation but read custom content from and write output to the repo
// they're acting on.
func rewritePaths(content, packPath string) string {
	globalPath := packPath

	// Protect per-repo paths by temporarily replacing them
	content = strings.ReplaceAll(content, "{project-root}/_bmad-custom/", "\x00BMAD_CUSTOM\x00")
	content = strings.ReplaceAll(content, "{project-root}/_bmad-output/", "\x00BMAD_OUTPUT\x00")

	// Rewrite pack content paths to global
	content = strings.ReplaceAll(content, "{project-root}/_bmad/", globalPath+"/")

	// Restore per-repo paths
	content = strings.ReplaceAll(content, "\x00BMAD_CUSTOM\x00", "{project-root}/_bmad-custom/")
	content = strings.ReplaceAll(content, "\x00BMAD_OUTPUT\x00", "{project-root}/_bmad-output/")

	return content
}

// appendFallbackFooter adds LLM-executable guidance at the end of a synced
// command file so that pack-internal workflow/step files retaining literal
// `{project-root}/_bmad/...` references can still resolve at orchestrator
// roots that have no `_bmad/` directory.
//
// The top-level command file is rewritten by rewritePaths (cwd-relative
// `{project-root}/_bmad/` → absolute pack path). Pack-internal files
// (workflow.md, step-*.md) are not rewritten because they remain inside the
// installed pack. The footer tells the reading LLM to resolve such
// references via a fallback chain: cwd first, then the user-install pack
// path below.
//
// The footer is idempotent: sentinel markers let Sync be re-run without
// duplicating content.
func appendFallbackFooter(content, packPath string) string {
	const beginMarker = "<!-- sideshow:fallback-resolution:begin -->"

	if strings.Contains(content, beginMarker) {
		return content
	}

	const tmpl = `

---

<!-- sideshow:fallback-resolution:begin -->
## Sideshow Fallback Resolution for pack-internal references

Workflow files inside this pack may reference paths of the form X{project-root}/_bmad/...X. Those references are not rewritten at sync time. When you follow this command into such a file and encounter such a reference, resolve it as:

1. First try X{project-root}/_bmad/...X relative to the current working directory (per-project install, if any).
2. If that path does not exist, substitute X{project-root}/_bmad/X with the pack user-install location:

   X__PACK_PATH__/X

3. Per-repo paths (X{project-root}/_bmad-custom/X, X{project-root}/_bmad-output/X) stay relative to the invoking project — these are per-repo, not pack content.
<!-- sideshow:fallback-resolution:end -->
`
	footer := strings.ReplaceAll(tmpl, "X", "`")
	footer = strings.ReplaceAll(footer, "__PACK_PATH__", packPath)

	return content + footer
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
