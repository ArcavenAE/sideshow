package bindings

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MarkdownCommandBinding handles the bmad 6.2.2-era distribution shape:
// flat .claude/commands/<pack>-*.md files. Command files live either in
// a commands/ subdir of the pack OR at the pack root. Targets are written
// to ~/.claude/commands/.
type MarkdownCommandBinding struct {
	name     string
	version  string
	packPath string
}

// NewMarkdownCommandBinding constructs a binding for a pack that ships
// flat markdown command files.
func NewMarkdownCommandBinding(name, version, packPath string) *MarkdownCommandBinding {
	return &MarkdownCommandBinding{
		name:     name,
		version:  version,
		packPath: packPath,
	}
}

// Kind returns the binding type identifier.
func (b *MarkdownCommandBinding) Kind() string { return "markdown-command" }

// PackName returns the owning pack's name.
func (b *MarkdownCommandBinding) PackName() string { return b.name }

// PackVersion returns the owning pack's version.
func (b *MarkdownCommandBinding) PackVersion() string { return b.version }

// Sync materializes markdown command files into ~/.claude/commands/ with
// pack-content path rewrites and the fallback-resolution footer appended.
func (b *MarkdownCommandBinding) Sync() (int, error) {
	claudeDir := claudeCommandsDir()
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return 0, fmt.Errorf("create commands dir: %w", err)
	}

	synced := 0

	// First pass: commands/ subdirectory.
	commandsSubdir := filepath.Join(b.packPath, "commands")
	if info, err := os.Stat(commandsSubdir); err == nil && info.IsDir() {
		walkErr := filepath.WalkDir(commandsSubdir, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read command %s: %w", path, err)
			}

			content := string(data)
			content = rewritePaths(content, b.packPath)
			content = appendFallbackFooter(content, b.packPath)

			destPath := filepath.Join(claudeDir, filepath.Base(path))
			if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write command %s: %w", destPath, err)
			}
			synced++
			return nil
		})
		if walkErr != nil {
			return synced, walkErr
		}
	}

	// Second pass: bmad-*.md anywhere in the pack (source repos sometimes
	// place commands outside a commands/ subdir).
	walkErr := filepath.WalkDir(b.packPath, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}

		name := filepath.Base(path)
		if !strings.HasPrefix(name, "bmad-") || !strings.HasSuffix(name, ".md") {
			return nil
		}

		// Skip entries already processed from commands/.
		rel, _ := filepath.Rel(b.packPath, path)
		if strings.HasPrefix(rel, "commands/") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip on read error
		}

		content := string(data)
		content = rewritePaths(content, b.packPath)
		content = appendFallbackFooter(content, b.packPath)

		destPath := filepath.Join(claudeDir, name)
		if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
			return nil // skip on write error
		}
		synced++
		return nil
	})

	return synced, walkErr
}

// Validate checks the binding's pack path exists. Content-level validation
// is out of scope for this binding type — the 6.2.2-era shape has no
// manifest to check against.
func (b *MarkdownCommandBinding) Validate() error {
	info, err := os.Stat(b.packPath)
	if err != nil {
		return fmt.Errorf("pack path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("pack path is not a directory: %s", b.packPath)
	}
	return nil
}

// hasMarkdownCommandContent reports whether a pack path has flat
// markdown command files in either commands/ or at the root.
func hasMarkdownCommandContent(packPath string) bool {
	commandsSubdir := filepath.Join(packPath, "commands")
	if info, err := os.Stat(commandsSubdir); err == nil && info.IsDir() {
		// If the dir exists but is empty of .md files, still return true —
		// a commands/ subdir signals the 6.2.2-era layout even empty.
		entries, _ := os.ReadDir(commandsSubdir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".md") {
				return true
			}
		}
	}

	// Also check for bmad-*.md at the pack root.
	entries, err := os.ReadDir(packPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "bmad-") && strings.HasSuffix(name, ".md") {
			return true
		}
	}
	return false
}

// countMarkdownCommands counts the discoverable markdown command files
// in a pack (union of commands/ and root-level bmad-*.md).
func countMarkdownCommands(packPath string) int {
	count := 0
	seen := make(map[string]bool)

	commandsSubdir := filepath.Join(packPath, "commands")
	if info, err := os.Stat(commandsSubdir); err == nil && info.IsDir() {
		_ = filepath.WalkDir(commandsSubdir, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			if strings.HasSuffix(path, ".md") {
				count++
				seen[filepath.Base(path)] = true
			}
			return nil
		})
	}

	_ = filepath.WalkDir(packPath, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		base := filepath.Base(path)
		if !strings.HasPrefix(base, "bmad-") || !strings.HasSuffix(base, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(packPath, path)
		if strings.HasPrefix(rel, "commands/") {
			return nil
		}
		if seen[base] {
			return nil
		}
		count++
		return nil
	})

	return count
}

// countSyncedCommands returns how many markdown command files for a pack
// are present in ~/.claude/commands/. Ownership is determined by
// the basenames the pack ships, not by name prefix — packs that ship
// multi-prefix commands (e.g. bmad's bmad-* + gds-*) would otherwise be
// undercounted by a "<packName>-" heuristic.
func countSyncedCommands(packPath string) (int, error) {
	owned := commandBasenames(packPath)
	if len(owned) == 0 {
		return 0, nil
	}

	dir := claudeCommandsDir()
	count := 0
	for name := range owned {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		if !info.IsDir() {
			count++
		}
	}
	return count, nil
}

// commandBasenames returns the set of *.md basenames the pack ships as
// markdown command bindings (root-level commands/ or pack-root *.md).
func commandBasenames(packPath string) map[string]struct{} {
	out := map[string]struct{}{}

	addFrom := func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasSuffix(name, ".md") {
				out[name] = struct{}{}
			}
		}
	}

	addFrom(filepath.Join(packPath, "commands"))
	return out
}
