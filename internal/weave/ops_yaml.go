package weave

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MemoryInjection replaces a memories block in one or more customize files.
type MemoryInjection struct {
	Targets         []string `yaml:"targets"`
	Anchor          string   `yaml:"anchor"`
	OnMissingAnchor string   `yaml:"on_missing_anchor"` // "fail" (default) or "skip"
	Memories        []string `yaml:"memories"`
}

// applyMemoryInjections rewrites the memories block in each target.
func applyMemoryInjections(d *Declaration, repoRoot string, opts Options) []Action {
	var actions []Action

	for _, inj := range d.MemoryInjections {
		onMissing := inj.OnMissingAnchor
		if onMissing == "" {
			onMissing = "fail"
		}

		for _, target := range inj.Targets {
			a := Action{Type: "memory", Name: filepath.Base(target), Path: target}
			abs, err := resolveWriteTarget(repoRoot, target)
			if err != nil {
				a.Outcome = Failed
				a.Detail = err.Error()
				actions = append(actions, a)
				continue
			}

			content, err := os.ReadFile(abs)
			if err != nil {
				if os.IsNotExist(err) && onMissing == "skip" {
					a.Outcome = Skipped
					a.Detail = "target does not exist"
				} else {
					a.Outcome = Failed
					a.Detail = "target does not exist"
					if !os.IsNotExist(err) {
						a.Detail = err.Error()
					}
				}
				actions = append(actions, a)
				continue
			}

			out, found := rewriteMemories(string(content), inj.Anchor, inj.Memories)
			if !found {
				// The shell awk deletes the old block and then never reaches an
				// insertion point, so the memories are gone and the run still
				// reports success. Refusing to write is the whole point of this
				// branch: the file is left exactly as it was.
				if onMissing == "skip" {
					a.Outcome = Skipped
				} else {
					a.Outcome = Failed
				}
				a.Detail = fmt.Sprintf("anchor %q not found; file left unchanged", inj.Anchor)
				actions = append(actions, a)
				continue
			}

			if out == string(content) {
				a.Outcome = Skipped
				a.Detail = "already current"
				actions = append(actions, a)
				continue
			}

			a.Outcome = Applied
			a.Detail = fmt.Sprintf("wrote %d memory line(s)", len(inj.Memories))
			if !opts.DryRun {
				if err := writeFilePreservingMode(abs, []byte(out)); err != nil {
					a.Outcome = Failed
					a.Detail = err.Error()
				}
			}
			actions = append(actions, a)
		}
	}

	return actions
}

// rewriteMemories deletes any existing top-level memories block and inserts a
// fresh one immediately before the anchor line, followed by one blank line.
// Returns the new content and whether the anchor was found.
//
// The line rules reproduce the shell awk this replaces:
//
//	/^memories:/                { skip=1; next }
//	skip && /^[a-z_#]/          { skip=0 }        // falls through, line kept
//	skip                        { next }
//	/^<anchor>/                 { emit block; emit blank }
//	                            { print }
//
// The awk's second rule, /^memories: \[\]/, is unreachable: the first already
// matches `memories: []`. It is not carried forward.
//
// Idempotent by construction, since the delete precedes the insert.
func rewriteMemories(content, anchor string, memories []string) (string, bool) {
	// Preserve whether the file ended with a newline.
	trailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")

	var out []string
	skip := false
	found := false

	for _, line := range lines {
		if strings.HasPrefix(line, "memories:") {
			skip = true
			continue
		}
		if skip && endsMemoriesBlock(line) {
			skip = false
		}
		if skip {
			continue
		}
		if anchor != "" && strings.HasPrefix(line, anchor) {
			found = true
			out = append(out, renderMemories(memories)...)
			out = append(out, "")
		}
		out = append(out, line)
	}

	res := strings.Join(out, "\n")
	if trailingNewline {
		res += "\n"
	}
	return res, found
}

// endsMemoriesBlock reports whether a line terminates the skipped region. The
// awk predicate is /^[a-z_#]/: a lowercase letter, an underscore, or a comment
// marker in column one. An indented list item stays inside the block, which is
// what makes the deletion work.
func endsMemoriesBlock(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	return (c >= 'a' && c <= 'z') || c == '_' || c == '#'
}

// renderMemories emits the memories block.
//
// Values are double-quoted with proper escaping. No shell script escaped them,
// so a memory containing a quote or a backslash would have produced invalid
// YAML. None does today; the engine escapes rather than reproducing a latent
// break.
func renderMemories(memories []string) []string {
	out := []string{"memories:"}
	for _, m := range memories {
		out = append(out, "  - "+quoteYAML(m))
	}
	return out
}

// quoteYAML renders s as a YAML double-quoted scalar.
func quoteYAML(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
