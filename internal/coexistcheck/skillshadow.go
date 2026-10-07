package coexistcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ArcavenAE/sideshow/internal/foreign"
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

// checkSkillShadow reports each skill present both in the repo's
// .claude/skills and in the pack's user-scope bound set. Advisory:
// a shadow does not make enable unsafe, so it is WARN, never a
// refusal. The loaded copy is the user-scope one on the Claude Code
// version where this was observed (2.1.x, native 6.2.2 repo loading
// the user-scope 6.10.0 bmad-help); it is an observation, not a
// documented contract.
func checkSkillShadow(rep *Report, opts Options) {
	if len(opts.BoundSkills) == 0 {
		return
	}
	bound := make(map[string]bool, len(opts.BoundSkills))
	for _, n := range opts.BoundSkills {
		bound[n] = true
	}
	for _, name := range repoSkillNames(opts.RepoDir) {
		if !bound[name] {
			continue
		}
		rep.add(11, "skill-shadow", foreign.Warn,
			fmt.Sprintf("%s exists in this repo's .claude/skills and in the user-scope %s binding; the user-scope copy loads, so the repo's copy is shadowed", name, opts.Pack))
	}
}
