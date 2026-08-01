package weave

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Verification asserts the woven state is present.
type Verification struct {
	// OnFailure is "warn" (default) or "error".
	//
	// Warn is the default, and it is the most consequential default in the
	// spec. The first ported script computes a VERIFY_OK flag, prints [OK] or [WARN], and
	// contains no exit statement in 240 lines: it always exits 0. Upstream's own
	// file-reference validator shipped blocking in BMAD-METHOD PR #1490 and was
	// reverted to warning before merge in #1494, because it turned CI red
	// against an existing corpus. Two months on, strict promotion is still an
	// open step in both that repo and bmad-builder.
	OnFailure string `yaml:"on_failure"`

	RequiredFiles []string      `yaml:"required_files"`
	CSVContains   []CSVContains `yaml:"csv_contains"`
}

// CSVContains asserts a target holds a needle.
type CSVContains struct {
	Target string `yaml:"target"`
	Needle string `yaml:"needle"`
}

// runVerification checks the declared assertions.
func runVerification(d *Declaration, repoRoot string, opts Options) []Action {
	if d.Verification == nil {
		return nil
	}
	v := d.Verification

	// A verification failure reports as Failed only when the declaration or the
	// run asks for it. Otherwise it reports as Skipped with the failure in the
	// detail, so the run stays advisory while the finding stays visible.
	outcomeFor := func() Outcome {
		if opts.Strict || v.OnFailure == "error" {
			return Failed
		}
		return Skipped
	}

	var actions []Action

	for _, rel := range v.RequiredFiles {
		a := Action{Type: "verify", Name: "required_file", Path: rel}
		if _, err := os.Stat(filepath.Join(repoRoot, rel)); err != nil {
			a.Outcome = outcomeFor()
			a.Detail = "missing"
		} else {
			a.Outcome = Applied
			a.Detail = "present"
		}
		actions = append(actions, a)
	}

	for _, c := range v.CSVContains {
		a := Action{Type: "verify", Name: "csv_contains", Path: c.Target}
		content, err := os.ReadFile(filepath.Join(repoRoot, c.Target))
		switch {
		case err != nil:
			a.Outcome = outcomeFor()
			a.Detail = "target unreadable"
		case !strings.Contains(string(content), c.Needle):
			a.Outcome = outcomeFor()
			a.Detail = fmt.Sprintf("needle %q not found", c.Needle)
		default:
			a.Outcome = Applied
			a.Detail = fmt.Sprintf("needle %q found", c.Needle)
		}
		actions = append(actions, a)
	}

	return actions
}
