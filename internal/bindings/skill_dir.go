package bindings

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SkillDirBinding handles the bmad 6.3.0-era distribution shape: per-skill
// directories under .claude/skills/<skill-name>/ with a SKILL.md entry
// point plus per-skill assets (bmad-skill-manifest.yaml, methods.csv,
// template files, etc.). Targets are written to ~/.claude/skills/<name>/.
//
// Unlike MarkdownCommandBinding, SkillDirBinding preserves directory
// structure and copies the full skill tree rather than flattening.
type SkillDirBinding struct {
	name     string
	version  string
	packPath string
}

// NewSkillDirBinding constructs a binding for a pack that ships per-skill
// directories under .claude/skills/.
func NewSkillDirBinding(name, version, packPath string) *SkillDirBinding {
	return &SkillDirBinding{
		name:     name,
		version:  version,
		packPath: packPath,
	}
}

// Kind returns the binding type identifier.
func (b *SkillDirBinding) Kind() string { return "skill-dir" }

// PackName returns the owning pack's name.
func (b *SkillDirBinding) PackName() string { return b.name }

// PackVersion returns the owning pack's version.
func (b *SkillDirBinding) PackVersion() string { return b.version }

// Sync materializes skill directories into ~/.claude/skills/<name>/.
// Each skill directory under the pack's .claude/skills/ is copied intact.
// Text files (.md, .yaml, .yml, .csv) are passed through rewritePaths.
// SKILL.md receives the fallback-resolution footer (it is the LLM entry
// point). Returns the number of skills synced.
func (b *SkillDirBinding) Sync() (int, error) {
	skillsSrc := filepath.Join(b.packPath, ".claude", "skills")
	skillsDst := claudeSkillsDir()

	if err := os.MkdirAll(skillsDst, 0o755); err != nil {
		return 0, fmt.Errorf("create skills dir: %w", err)
	}

	entries, err := os.ReadDir(skillsSrc)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read skills source: %w", err)
	}

	synced := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillName := e.Name()
		src := filepath.Join(skillsSrc, skillName)
		dst := filepath.Join(skillsDst, skillName)

		if err := b.syncSkillTree(src, dst); err != nil {
			return synced, fmt.Errorf("sync skill %s: %w", skillName, err)
		}
		synced++
	}

	return synced, nil
}

// syncSkillTree recursively copies a single skill directory from src into
// dst, applying path rewrites to text files and appending the
// fallback-resolution footer to SKILL.md.
func (b *SkillDirBinding) syncSkillTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("compute rel path: %w", err)
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		if shouldRewrite(path) {
			content := string(data)
			content = rewritePaths(content, b.packPath)
			if filepath.Base(path) == "SKILL.md" {
				content = appendFallbackFooter(content, b.packPath)
			}
			data = []byte(content)
		}

		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		return nil
	})
}

// shouldRewrite reports whether a file's content should pass through
// rewritePaths before being written to the target. Binary-like files are
// passed through unchanged; text formats get the rewrite.
func shouldRewrite(path string) bool {
	switch filepath.Ext(path) {
	case ".md", ".yaml", ".yml", ".csv", ".txt", ".json":
		return true
	}
	return false
}

// Validate checks that the pack's .claude/skills/ directory contains at
// least one skill directory with a SKILL.md entry point. Empty skills
// directories or skills lacking SKILL.md are reported.
func (b *SkillDirBinding) Validate() error {
	skillsSrc := filepath.Join(b.packPath, ".claude", "skills")
	entries, err := os.ReadDir(skillsSrc)
	if err != nil {
		return fmt.Errorf("read skills: %w", err)
	}

	anySkill := false
	var missing []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		anySkill = true
		skillPath := filepath.Join(skillsSrc, e.Name())
		entryPoint := filepath.Join(skillPath, "SKILL.md")
		if _, err := os.Stat(entryPoint); err != nil {
			missing = append(missing, e.Name())
		}
	}
	if !anySkill {
		return fmt.Errorf("no skill directories found under %s", skillsSrc)
	}
	if len(missing) > 0 {
		return fmt.Errorf("skills missing SKILL.md entry point: %s", strings.Join(missing, ", "))
	}
	return nil
}

// hasSkillDirContent reports whether a pack path has a .claude/skills/
// directory with at least one skill subdir.
func hasSkillDirContent(packPath string) bool {
	skillsSrc := filepath.Join(packPath, ".claude", "skills")
	entries, err := os.ReadDir(skillsSrc)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
	}
	return false
}

// countSkillDirs counts the skill directories a pack ships under
// .claude/skills/.
func countSkillDirs(packPath string) int {
	skillsSrc := filepath.Join(packPath, ".claude", "skills")
	entries, err := os.ReadDir(skillsSrc)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			count++
		}
	}
	return count
}

// countSyncedSkills counts skill directories currently present in
// ~/.claude/skills/ that the given pack ships. Ownership is determined
// by canonical id — the directory name under the pack's .claude/skills/.
// This matches upstream bmad's `installed-skills.js` approach (added in
// bmad 6.5) which reads the canonicalId set rather than relying on a
// name prefix. Packs may ship skills with multiple prefixes (bmad ships
// bmad-* and gds-* both); a prefix heuristic undercounts those packs.
func countSyncedSkills(packPath string) (int, error) {
	owned := skillCanonicalIds(packPath)
	if len(owned) == 0 {
		return 0, nil
	}

	dir := claudeSkillsDir()
	count := 0
	for id := range owned {
		info, err := os.Stat(filepath.Join(dir, id))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		if info.IsDir() {
			count++
		}
	}
	return count, nil
}

// skillCanonicalIds returns the set of skill directory names (canonical
// ids) the pack ships under .claude/skills/. Returns an empty set if the
// pack has no skills binding.
func skillCanonicalIds(packPath string) map[string]struct{} {
	skillsSrc := filepath.Join(packPath, ".claude", "skills")
	entries, err := os.ReadDir(skillsSrc)
	if err != nil {
		return map[string]struct{}{}
	}
	ids := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids[e.Name()] = struct{}{}
		}
	}
	return ids
}
