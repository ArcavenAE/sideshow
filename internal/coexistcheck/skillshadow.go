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
// .claude/skills and in the pack's user-scope bound set, naming both
// paths. Advisory: a shadow does not make enable unsafe, so it is
// WARN, never a refusal.
//
// Which copy loads is an observation, not a documented contract: a
// repo with a native 6.2.2 bmad install loaded the user-scope 6.10.0
// bmad-help (Claude Code 2.1.x, aae-orc-phytt), which matches the
// personal-over-project order. The finding says "loads" for that
// observed copy and nothing wider.
func checkSkillShadow(rep *Report, opts Options) {
	repoSkills := repoSkillNames(opts.RepoDir)
	if len(opts.BoundSkills) == 0 {
		// Nothing was compared. Say so when the repo has skills that
		// could have been, so a quiet report does not read as clean.
		if len(repoSkills) > 0 {
			rep.add(11, "skill-shadow", foreign.Info,
				"skill-shadow not checked: no sync manifest for "+opts.Pack)
		}
		return
	}
	for _, name := range repoSkills {
		userPath, ok := opts.BoundSkills[name]
		if !ok {
			continue
		}
		repoPath := filepath.Join(opts.RepoDir, ".claude", "skills", name)
		rep.add(11, "skill-shadow", foreign.Warn,
			fmt.Sprintf("%s: the user-scope copy at %s loads and shadows this repo's copy at %s (observed on Claude Code 2.1.x)", name, userPath, repoPath))
	}
}
