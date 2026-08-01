package preserve

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsProtected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		// Class 1: durable user state, refused wherever it appears.
		{"factory worktree", "/repo/.factory", true},
		{"inside the factory worktree", "/repo/.factory/state/wave.yaml", true},
		{"factory project state", "/repo/.factory-project", true},
		{"linked worktrees dir", "/repo/.worktrees", true},
		{"a STORY wave checkout", "/repo/.worktrees/STORY-14/src", true},
		{"git metadata carries every ref", "/repo/.git", true},
		{"a ref file under git", "/repo/.git/refs/heads/factory-artifacts", true},
		{"pack customization tree", "/repo/_vsdd-factory-custom", true},
		{"inside a customization tree", "/repo/_bmad-custom/skills/x", true},
		{"pack output tree", "/repo/_vsdd-factory-output", true},
		{"protected state at depth", "/a/b/c/.factory/d", true},
		{"a pack sideshow has never seen still gets the floor", "/repo/_totally-new-pack-custom", true},

		// Class 2 and 3: removal is expected.
		{"bound variant cache", "/data/bound/vsdd-factory/1.0.0", false},
		{"harness dir", "/repo/.claude", false},
		{"a bound skill dir", "/repo/.claude/skills/vsdd-orchestrator", false},
		{"the store", "/data/packs/vsdd-factory/1.0.0", false},
		{"a settings file", "/repo/.claude/settings.local.json", false},

		// Near misses: the shape has to actually match.
		{"custom without the underscore", "/repo/vsdd-factory-custom", false},
		{"underscore without the suffix", "/repo/_vsdd-factory", false},
		{"a bare underscore", "/repo/_", false},
		{"factory as a substring, not a component", "/repo/my.factory.notes", false},
		{"output as a substring", "/repo/_outputs", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, _ := IsProtected(tc.path)
			if got != tc.want {
				t.Fatalf("IsProtected(%q) = %v, want %v", tc.path, got, tc.want)
			}
			err := Check(tc.path)
			if (err != nil) != tc.want {
				t.Fatalf("Check(%q) error = %v, want protected=%v", tc.path, err, tc.want)
			}
		})
	}
}

func TestCheck_ErrorNamesTheComponentAndIsTyped(t *testing.T) {
	t.Parallel()
	err := Check("/repo/.factory/wave-state.yaml")
	if err == nil {
		t.Fatal("protected path was not refused")
	}
	var protected *ErrProtected
	if !errors.As(err, &protected) {
		t.Fatalf("error is not *ErrProtected: %T", err)
	}
	if protected.Component != ".factory" {
		t.Errorf("component = %q, want .factory", protected.Component)
	}
	// The operator has to be able to tell WHAT was refused and why from
	// the message alone.
	for _, want := range []string{".factory", "protected user state"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message does not contain %q: %v", want, err)
		}
	}
}

func TestCheckAll_RefusesTheWholeSetOnOneOffender(t *testing.T) {
	t.Parallel()
	set := []string{
		"/repo/.claude/skills/a",
		"/repo/.claude/skills/b",
		"/repo/.factory",
		"/repo/.claude/skills/c",
	}
	err := CheckAll(set)
	if err == nil {
		t.Fatal("set containing protected state was accepted")
	}
	if !strings.Contains(err.Error(), ".factory") {
		t.Errorf("refusal did not name the offender: %v", err)
	}
	if err := CheckAll(set[:2]); err != nil {
		t.Errorf("clean set refused: %v", err)
	}
}

// TestNoDestructiveGitInvocations keeps ticket item 3 true rather than
// assuming it stays true. Git-side protection is verb behavior, not a
// path list: a branch or a ref is not something a containment predicate
// can see, so the guard is that sideshow never invokes the subcommands
// at all. Measured 2026-08-01: git usage is rev-parse and status only.
func TestNoDestructiveGitInvocations(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	var offenders []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		body := string(data)
		for _, sub := range DestructiveGitSubcommands {
			// Match the argv shape sideshow would use: the subcommand
			// and its flag as adjacent quoted strings.
			parts := strings.Fields(sub)
			needle := `"` + parts[0] + `", "` + parts[1] + `"`
			if strings.Contains(body, needle) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+": "+sub)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if len(offenders) > 0 {
		t.Fatalf("destructive git invocations found; sideshow's git usage must stay read-only:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
