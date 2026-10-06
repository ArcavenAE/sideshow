package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// aae-orc-mobz8: the install command passes --force through to the
// store. Without the flag an install over an installed version fails;
// with it the install replaces the content.
func TestRunInstall_ForceFlagReachesTheStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	t.Cleanup(func() { _ = pack.UnfreezeTree(home) })

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "pack.yaml"), []byte("name: demo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(src, "doc.md")
	if err := os.WriteFile(doc, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"demo", "--from", src, "--no-activate", "--yes"}

	if err := runInstall(args); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := os.WriteFile(doc, []byte("replaced"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runInstall(args); !errors.Is(err, pack.ErrVersionInstalled) {
		t.Fatalf("install without --force: err = %v, want ErrVersionInstalled", err)
	}
	if err := runInstall(append(args, "--force")); err != nil {
		t.Fatalf("install with --force: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, "packs", "demo", "1.0.0", "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "replaced" {
		t.Errorf("store content = %q after --force, want replaced", got)
	}
}
