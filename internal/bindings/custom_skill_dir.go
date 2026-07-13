package bindings

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// CustomSkillDirBinding binds custom-built skills — bmb-authored agents
// and other project-local skills — from a consumer repo's
// _<pack>-custom/skills/<name>/ directories into ~/.claude/skills/.
//
// Unlike SkillDirBinding, content is copied verbatim: no path rewrites
// and no fallback-resolution footer. Custom skills live in checked-in
// project territory, not the user-scope pack store, so store-relative
// rewrites do not apply. The skill set is fixed at construction time
// (collision filtering against pack-owned canonical ids happens during
// discovery), so Sync writes exactly the listed skills.
type CustomSkillDirBinding struct {
	pack       string
	projectDir string
	skills     []string
}

// NewCustomSkillDirBinding constructs a binding for the given consumer
// repo, pack, and pre-filtered skill list.
func NewCustomSkillDirBinding(packName, projectDir string, skills []string) *CustomSkillDirBinding {
	return &CustomSkillDirBinding{
		pack:       packName,
		projectDir: projectDir,
		skills:     skills,
	}
}

// Kind returns the binding type identifier.
func (b *CustomSkillDirBinding) Kind() string { return "custom-skill-dir" }

// PackName returns the pack this customization layer belongs to.
func (b *CustomSkillDirBinding) PackName() string { return b.pack }

// PackVersion returns "custom" — custom skills are project content and
// do not track the active pack version.
func (b *CustomSkillDirBinding) PackVersion() string { return "custom" }

// skillsSrcDir returns the consumer repo's custom skills directory.
func (b *CustomSkillDirBinding) skillsSrcDir() string {
	return customSkillsDir(b.projectDir, b.pack)
}

// Sync copies each listed skill directory verbatim into
// ~/.claude/skills/<name>/. Returns the number of skills synced.
func (b *CustomSkillDirBinding) Sync() (int, error) {
	dst := claudeSkillsDir()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, fmt.Errorf("create skills dir: %w", err)
	}

	synced := 0
	for _, name := range b.skills {
		src := filepath.Join(b.skillsSrcDir(), name)
		if err := copyTree(src, filepath.Join(dst, name)); err != nil {
			return synced, fmt.Errorf("sync custom skill %s: %w", name, err)
		}
		synced++
	}
	return synced, nil
}

// Artifacts returns the destination skill directories this binding owns.
func (b *CustomSkillDirBinding) Artifacts() ([]string, error) {
	dst := claudeSkillsDir()
	out := make([]string, 0, len(b.skills))
	for _, name := range b.skills {
		out = append(out, filepath.Join(dst, name))
	}
	sort.Strings(out)
	return out, nil
}

// Validate checks that every listed skill directory still has its
// SKILL.md entry point.
func (b *CustomSkillDirBinding) Validate() error {
	var missing []string
	for _, name := range b.skills {
		entry := filepath.Join(b.skillsSrcDir(), name, "SKILL.md")
		if _, err := os.Stat(entry); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("custom skills missing SKILL.md entry point: %v", missing)
	}
	return nil
}

// copyTree recursively copies src into dst with no content transforms.
func copyTree(src, dst string) error {
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
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		return nil
	})
}

// customSkillsDir returns the custom skills directory for a consumer
// repo + pack pair: <project>/_<pack>-custom/skills.
func customSkillsDir(projectDir, packName string) string {
	return filepath.Join(projectDir, fmt.Sprintf("_%s-custom", packName), "skills")
}

// customSkillIds returns the names of skill directories (containing a
// SKILL.md entry point) under a consumer repo's custom skills dir.
// Directories without SKILL.md are ignored — half-authored skills must
// not break sync.
func customSkillIds(projectDir, packName string) []string {
	entries, err := os.ReadDir(customSkillsDir(projectDir, packName))
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		entry := filepath.Join(customSkillsDir(projectDir, packName), e.Name(), "SKILL.md")
		if _, err := os.Stat(entry); err == nil {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

// hasCustomSkillContent reports whether a consumer repo has at least
// one bindable custom skill for the pack.
func hasCustomSkillContent(projectDir, packName string) bool {
	return len(customSkillIds(projectDir, packName)) > 0
}
