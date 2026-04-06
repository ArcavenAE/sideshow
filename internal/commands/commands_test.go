package commands

import (
	"testing"
)

func TestRewritePaths_GlobalRewrite(t *testing.T) {
	input := "Load from {project-root}/_bmad/bmm/agents/pm.md"
	want := "Load from /global/packs/bmad/6.2.2/bmm/agents/pm.md"

	got := rewritePaths(input, "/global/packs/bmad/6.2.2")
	if got != want {
		t.Errorf("rewritePaths() =\n  %q\nwant\n  %q", got, want)
	}
}

func TestRewritePaths_PreservesCustom(t *testing.T) {
	input := "Read {project-root}/_bmad-custom/overrides.yaml"
	got := rewritePaths(input, "/global/packs/bmad/6.2.2")
	if got != input {
		t.Errorf("rewritePaths() modified _bmad-custom path:\n  got:  %q\n  want: %q", got, input)
	}
}

func TestRewritePaths_PreservesOutput(t *testing.T) {
	input := "Write to {project-root}/_bmad-output/results.md"
	got := rewritePaths(input, "/global/packs/bmad/6.2.2")
	if got != input {
		t.Errorf("rewritePaths() modified _bmad-output path:\n  got:  %q\n  want: %q", got, input)
	}
}

func TestRewritePaths_PreservesProjectRoot(t *testing.T) {
	input := "Read {project-root}/docs/readme.md"
	got := rewritePaths(input, "/global/packs/bmad/6.2.2")
	if got != input {
		t.Errorf("rewritePaths() modified project-root path:\n  got:  %q\n  want: %q", got, input)
	}
}

func TestRewritePaths_MixedContent(t *testing.T) {
	input := `Load {project-root}/_bmad/core/workflow.md
Read {project-root}/_bmad-custom/config.yaml
Write {project-root}/_bmad-output/report.md
Check {project-root}/docs/spec.md`

	want := `Load /g/core/workflow.md
Read {project-root}/_bmad-custom/config.yaml
Write {project-root}/_bmad-output/report.md
Check {project-root}/docs/spec.md`

	got := rewritePaths(input, "/g")
	if got != want {
		t.Errorf("rewritePaths() =\n  %q\nwant\n  %q", got, want)
	}
}
