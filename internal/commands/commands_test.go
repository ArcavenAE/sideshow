package commands

import (
	"strings"
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

func TestAppendFallbackFooter_AddsFooter(t *testing.T) {
	input := "Some command content.\n"
	got := appendFallbackFooter(input, "/global/packs/bmad/6.2.2")

	if !strings.Contains(got, "<!-- sideshow:fallback-resolution:begin -->") {
		t.Errorf("appendFallbackFooter did not add begin marker: %q", got)
	}
	if !strings.Contains(got, "<!-- sideshow:fallback-resolution:end -->") {
		t.Errorf("appendFallbackFooter did not add end marker: %q", got)
	}
	if !strings.Contains(got, "/global/packs/bmad/6.2.2") {
		t.Errorf("appendFallbackFooter did not interpolate pack path: %q", got)
	}
	if !strings.HasPrefix(got, input) {
		t.Errorf("appendFallbackFooter did not preserve original content prefix")
	}
}

func TestAppendFallbackFooter_Idempotent(t *testing.T) {
	input := "Some command content.\n"
	once := appendFallbackFooter(input, "/global/packs/bmad/6.2.2")
	twice := appendFallbackFooter(once, "/global/packs/bmad/6.2.2")

	if once != twice {
		t.Errorf("appendFallbackFooter is not idempotent:\n  once: %q\n  twice: %q", once, twice)
	}

	beginCount := strings.Count(twice, "<!-- sideshow:fallback-resolution:begin -->")
	if beginCount != 1 {
		t.Errorf("second call added another begin marker; count=%d", beginCount)
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
