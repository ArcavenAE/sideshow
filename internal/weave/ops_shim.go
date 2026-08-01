package weave

import (
	"fmt"
	"os"
	"path/filepath"
)

// SlashCommand generates a harness command file pointing at a custom agent.
type SlashCommand struct {
	ID       string `yaml:"id"`
	Target   string `yaml:"target"`
	OnExists string `yaml:"on_exists"` // "skip" (default) or "overwrite"
	Body     string `yaml:"body"`

	// AgentSource is recorded for provenance. The body is carried verbatim
	// rather than rendered from it, because the five shell scripts' shims are
	// not identical and the differences are not systematic. A `template:`
	// alternative can render one once sideshow owns a canonical shim shape.
	AgentSource string `yaml:"agent_source"`

	// Frontmatter is recorded for provenance and future template rendering. It
	// is not used to generate Body.
	Frontmatter map[string]any `yaml:"frontmatter"`
}

// applySlashCommands writes each declared command file.
func applySlashCommands(d *Declaration, repoRoot string, opts Options) []Action {
	var actions []Action

	for _, cmd := range d.SlashCommands {
		a := Action{Type: "shim", Name: cmd.ID, Path: cmd.Target}
		abs := filepath.Join(repoRoot, cmd.Target)

		onExists := cmd.OnExists
		if onExists == "" {
			onExists = "skip"
		}

		if _, err := os.Stat(abs); err == nil {
			if onExists == "skip" {
				// A human who edited the shim should not lose the edit to a
				// reinstall. This matches every shell script's behavior.
				a.Outcome = Skipped
				a.Detail = "already exists"
				actions = append(actions, a)
				continue
			}
		} else if !os.IsNotExist(err) {
			a.Outcome = Failed
			a.Detail = err.Error()
			actions = append(actions, a)
			continue
		}

		if cmd.Body == "" {
			a.Outcome = Failed
			a.Detail = "no body declared"
			actions = append(actions, a)
			continue
		}

		a.Outcome = Applied
		a.Detail = fmt.Sprintf("wrote %d bytes", len(cmd.Body))
		if !opts.DryRun {
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				a.Outcome = Failed
				a.Detail = err.Error()
				actions = append(actions, a)
				continue
			}
			if err := writeFilePreservingMode(abs, []byte(cmd.Body)); err != nil {
				a.Outcome = Failed
				a.Detail = err.Error()
			}
		}
		actions = append(actions, a)
	}

	return actions
}
