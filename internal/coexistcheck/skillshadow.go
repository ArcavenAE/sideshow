package coexistcheck

import (
	"os"
	"path/filepath"
	"sort"
)

// repoSkillNames lists the skills the repo carries natively: each
// directory under .claude/skills that holds a SKILL.md, sorted.
func repoSkillNames(repoDir string) []string {
	dir := filepath.Join(repoDir, ".claude", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
