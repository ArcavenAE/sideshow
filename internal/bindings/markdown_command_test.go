package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownCommandBinding_Kind(t *testing.T) {
	b := NewMarkdownCommandBinding("bmad", "6.2.2", "/tmp/whatever")
	if b.Kind() != "markdown-command" {
		t.Errorf("Kind() = %q, want %q", b.Kind(), "markdown-command")
	}
}

func TestMarkdownCommandBinding_Sync_RewritesAndFooter(t *testing.T) {
	packPath := t.TempDir()
	destRoot := t.TempDir()
	t.Setenv("HOME", destRoot)

	// commands/ subdir layout.
	writeFile(t, filepath.Join(packPath, "commands", "bmad-party-mode.md"),
		"LOAD {project-root}/_bmad/core/workflow.md\n")
	// Root-level bmad-*.md layout (second-pass discovery).
	writeFile(t, filepath.Join(packPath, "bmad-help.md"),
		"See {project-root}/_bmad/help/index.md\n")
	// A non-bmad file at root — should NOT be picked up.
	writeFile(t, filepath.Join(packPath, "README.md"), "hello")

	b := NewMarkdownCommandBinding("bmad", "6.2.2", packPath)
	n, err := b.Sync()
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 2 {
		t.Errorf("Sync synced %d commands, want 2", n)
	}

	party, err := os.ReadFile(filepath.Join(destRoot, ".claude", "commands", "bmad-party-mode.md"))
	if err != nil {
		t.Fatalf("read party: %v", err)
	}
	if !strings.Contains(string(party), packPath+"/core/workflow.md") {
		t.Errorf("party command not rewritten:\n%s", party)
	}
	if !strings.Contains(string(party), "<!-- sideshow:fallback-resolution:begin -->") {
		t.Errorf("party command missing fallback footer:\n%s", party)
	}

	help, err := os.ReadFile(filepath.Join(destRoot, ".claude", "commands", "bmad-help.md"))
	if err != nil {
		t.Fatalf("read help: %v", err)
	}
	if !strings.Contains(string(help), packPath+"/help/index.md") {
		t.Errorf("help command not rewritten:\n%s", help)
	}

	// README.md must not be synced.
	if _, err := os.Stat(filepath.Join(destRoot, ".claude", "commands", "README.md")); !os.IsNotExist(err) {
		t.Errorf("README.md should not have been synced")
	}
}

func TestHasMarkdownCommandContent(t *testing.T) {
	// Empty pack → false.
	packPath := t.TempDir()
	if hasMarkdownCommandContent(packPath) {
		t.Error("hasMarkdownCommandContent = true for empty pack")
	}

	// commands/ with one .md → true.
	p2 := t.TempDir()
	writeFile(t, filepath.Join(p2, "commands", "bmad-test.md"), "x")
	if !hasMarkdownCommandContent(p2) {
		t.Error("hasMarkdownCommandContent = false for commands/ layout")
	}

	// Root bmad-*.md → true.
	p3 := t.TempDir()
	writeFile(t, filepath.Join(p3, "bmad-test.md"), "x")
	if !hasMarkdownCommandContent(p3) {
		t.Error("hasMarkdownCommandContent = false for root layout")
	}
}

func TestCountMarkdownCommands_Deduplicates(t *testing.T) {
	packPath := t.TempDir()
	writeFile(t, filepath.Join(packPath, "commands", "bmad-a.md"), "a")
	writeFile(t, filepath.Join(packPath, "commands", "bmad-b.md"), "b")
	// Same basename in a different location — the second-pass walk should
	// skip it because the commands/ scan already saw it via the first pass.
	writeFile(t, filepath.Join(packPath, "bmad-a.md"), "a-root")

	count := countMarkdownCommands(packPath)
	if count != 2 {
		t.Errorf("countMarkdownCommands = %d, want 2 (a and b; the root-level bmad-a.md is a duplicate basename)", count)
	}
}
