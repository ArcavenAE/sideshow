package weave

import (
	"fmt"
	"os"
	"strings"
)

// CSVInjection appends rows to a pack-owned CSV the installer regenerates.
type CSVInjection struct {
	Name   string   `yaml:"name"`
	Target string   `yaml:"target"`
	Guard  string   `yaml:"guard"`
	Rows   []string `yaml:"rows"`
}

// applyCSVInjections appends declared rows to each target.
//
// Rows are written verbatim with no CSV parsing. The upstream installer's own
// parser accumulates fields without trimming, so a row a well-behaved CSV
// writer would normalize is a row that behaves differently at runtime. See
// docs/rule-inventory.md, the no-trim contract.
func applyCSVInjections(d *Declaration, repoRoot string, opts Options) []Action {
	var actions []Action

	for _, inj := range d.CSVInjections {
		a := Action{Type: "csv", Name: inj.Name, Path: inj.Target}
		abs, err := resolveWriteTarget(repoRoot, inj.Target)
		if err != nil {
			a.Outcome = Failed
			a.Detail = err.Error()
			actions = append(actions, a)
			continue
		}

		content, err := os.ReadFile(abs)
		if err != nil {
			// The shell scripts printed [ERROR] and carried on, so a missing
			// manifest silently produced a partial weave.
			a.Outcome = Failed
			if os.IsNotExist(err) {
				a.Detail = "target does not exist"
			} else {
				a.Detail = err.Error()
			}
			actions = append(actions, a)
			continue
		}

		if inj.Guard != "" && strings.Contains(string(content), inj.Guard) {
			a.Outcome = Skipped
			a.Detail = fmt.Sprintf("guard %q already present", inj.Guard)
			actions = append(actions, a)
			continue
		}

		if len(inj.Rows) == 0 {
			a.Outcome = Skipped
			a.Detail = "no rows declared"
			actions = append(actions, a)
			continue
		}

		var b strings.Builder
		b.Write(content)
		// Trailing-newline normalization. Without it the first appended row
		// fuses onto the last existing row. None of the five scripts did this.
		if len(content) > 0 && content[len(content)-1] != '\n' {
			b.WriteByte('\n')
		}
		for _, row := range inj.Rows {
			b.WriteString(row)
			b.WriteByte('\n')
		}

		a.Outcome = Applied
		a.Detail = fmt.Sprintf("appended %d row(s)", len(inj.Rows))
		if !opts.DryRun {
			if err := writeFilePreservingMode(abs, []byte(b.String())); err != nil {
				a.Outcome = Failed
				a.Detail = err.Error()
			}
		}
		actions = append(actions, a)
	}

	return actions
}

// writeFilePreservingMode writes content, keeping the existing file mode when
// there is one.
func writeFilePreservingMode(path string, content []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return os.WriteFile(path, content, mode)
}
