package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles the CLI once per test so the help and version
// routing in main is tested as a user runs it.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "sideshow")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func runBinary(t *testing.T, bin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	cmd.Env = append(cmd.Environ(), "SIDESHOW_HOME="+t.TempDir(), "HOME="+t.TempDir())
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

// `-v` is an alias of `--version` and `-V` (sideshow#100).
func TestCLI_LowercaseVersionAlias(t *testing.T) {
	bin := buildBinary(t)
	want, _, _ := runBinary(t, bin, "--version")
	got, errOut, code := runBinary(t, bin, "-v")
	if code != 0 || got != want || !strings.HasPrefix(got, "sideshow ") {
		t.Errorf("-v: code=%d stdout=%q stderr=%q, want %q", code, got, errOut, want)
	}
}

// `<verb> --help` and `help <verb>` print the verb's usage line and its
// flags, not the whole top-level usage (sideshow#100).
func TestCLI_VerbHelpIsPerSubcommand(t *testing.T) {
	bin := buildBinary(t)
	for verb, wants := range map[string][]string{
		"enable":  {"sideshow enable <pack>", "--repo", "--scope", "--override-stale-lock"},
		"disable": {"sideshow disable <pack>", "--repo", "--override-stale-lock"},
	} {
		for _, args := range [][]string{{verb, "--help"}, {"help", verb}, {verb, "-h"}} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				so, se, code := runBinary(t, bin, args...)
				out := so + se
				if code != 0 {
					t.Errorf("exit %d, want 0", code)
				}
				for _, want := range wants {
					if !strings.Contains(out, want) {
						t.Errorf("help lacks %q:\n%s", want, out)
					}
				}
				for _, bad := range []string{"sideshow install", "Install options", "sideshow doctor"} {
					if strings.Contains(out, bad) {
						t.Errorf("help carries the top-level usage (%q):\n%s", bad, out)
					}
				}
				if verb == "disable" && strings.Contains(out, "--scope") {
					t.Errorf("disable help lists --scope, which disable does not take:\n%s", out)
				}
			})
		}
	}
}

// Controls: the top-level help is unchanged, and a verb with no help of
// its own still falls back to it.
func TestCLI_TopLevelHelpStillPrintsFullUsage(t *testing.T) {
	bin := buildBinary(t)
	for _, args := range [][]string{{"--help"}, {"help"}, {"list", "--help"}, {"help", "list"}} {
		_, se, code := runBinary(t, bin, args...)
		if code != 0 || !strings.Contains(se, "sideshow install <pack>") || !strings.Contains(se, "Install options") {
			t.Errorf("%v: code=%d, want the full usage:\n%s", args, code, se)
		}
	}
}
