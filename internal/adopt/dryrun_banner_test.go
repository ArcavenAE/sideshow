package adopt

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	ferr := fn()
	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), ferr
}

// The dry run's last line sits beside the findings it just printed, so a
// WARN among them must not be followed by a bare "preflight clean"
// (sideshow#95 item 2).
func TestAdopt_DryRunCountsWarningsInItsVerdict(t *testing.T) {
	opts, repo, _ := fixture(t, "project")
	opts.DryRun = true
	// An unprefixed agent beside an enabled foreign identity is a WARN.
	mustWrite(t, repo, ".claude/agents/helper.md", "# helper\n", 0o644)

	out, err := captureStdout(t, func() error { _, err := Adopt(opts); return err })
	if err != nil {
		t.Fatalf("Adopt dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "WARN [5 agent-key-audit]") {
		t.Fatalf("fixture did not raise the WARN it relies on:\n%s", out)
	}
	if strings.Contains(out, "preflight clean") {
		t.Errorf("a dry run with a WARN printed the clean verdict:\n%s", out)
	}
	if !strings.Contains(out, "preflight passed with 1 warning(s) and every plan step resolves: the real run would proceed") {
		t.Errorf("verdict does not count the warning:\n%s", out)
	}
}

// Control: with no finding the dry run keeps its clean verdict.
func TestAdopt_DryRunWithoutWarningsKeepsTheCleanVerdict(t *testing.T) {
	opts, _, _ := fixture(t, "project")
	opts.DryRun = true

	out, err := captureStdout(t, func() error { _, err := Adopt(opts); return err })
	if err != nil {
		t.Fatalf("Adopt dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "preflight clean and every plan step resolves: the real run would proceed") {
		t.Errorf("a clean dry run lost its clean verdict:\n%s", out)
	}
}

// An INFO finding is not a warning: the dry run keeps its clean verdict
// while printing it. This pins the INFO/WARN split that the zero-finding
// control cannot (sideshow#95 follow-up).
func TestAdopt_DryRunInfoFindingKeepsTheCleanVerdict(t *testing.T) {
	opts, repo, _ := fixture(t, "project")
	opts.DryRun = true
	// A prefixed agent entry is the sideshow channel's own, reported as INFO.
	mustWrite(t, repo, ".claude/agents/"+opts.Prefix+"-helper.md", "# helper\n", 0o644)

	out, err := captureStdout(t, func() error { _, err := Adopt(opts); return err })
	if err != nil {
		t.Fatalf("Adopt dry run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "INFO [5 agent-key-audit]") {
		t.Fatalf("fixture did not raise the INFO it relies on:\n%s", out)
	}
	if strings.Contains(out, "WARN [") {
		t.Fatalf("fixture raised a WARN, so it cannot isolate INFO:\n%s", out)
	}
	if !strings.Contains(out, "preflight clean and every plan step resolves: the real run would proceed") {
		t.Errorf("an INFO finding was counted as a warning:\n%s", out)
	}
}
