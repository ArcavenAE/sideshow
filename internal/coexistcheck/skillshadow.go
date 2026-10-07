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
// Which copy loads comes from the Claude Code skills documentation,
// section "Resolve skills that share a name": "Enterprise over personal,
// and personal over project", so a personal (user-scope) copy runs in
// place of the repo's. It matches the observation in aae-orc-phytt (a
// native 6.2.2 repo loading the user-scope 6.10.0 bmad-help).
//
// Skills are matched by directory name, as the documented rule matches
// them. A skill whose SKILL.md frontmatter sets a different command name
// is not caught.
func checkSkillShadow(rep *Report, opts Options) {
	// nil means the caller never loaded the sync manifest (the enable
	// preflight, the adopt dry run and doctor layer 3 do not), so nothing
	// was compared and nothing is claimed.
	if opts.BoundSkills == nil {
		return
	}
	repoSkills := repoSkillNames(opts.RepoDir)
	if len(opts.BoundSkills) == 0 {
		// The manifest was read and holds no user-scope skills for this
		// pack. Say so when the repo has skills that could have been
		// compared, so a quiet report does not read as clean.
		if len(repoSkills) > 0 {
			rep.add(11, "skill-shadow", foreign.Info,
				"skill-shadow not checked: no skill-dir entries in the sync manifest for "+opts.Pack)
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
			fmt.Sprintf("%s: the user-scope copy at %s loads and shadows this repo's copy at %s (Claude Code: personal over project)", name, userPath, repoPath))
	}
}
