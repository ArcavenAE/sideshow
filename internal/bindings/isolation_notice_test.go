package bindings

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// captureStdout returns what fn wrote to stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

// installOnePack puts one syncable pack into the store at SIDESHOW_HOME.
func installOnePack(t *testing.T) {
	t.Helper()
	store := pack.SideshowDir()
	root := filepath.Join(store, "packs", "demo", "1.0.0")
	writeFile(t, filepath.Join(root, "pack.yaml"), "name: demo\nversion: 1.0.0\n")
	writeFile(t, filepath.Join(root, ".claude", "skills", "demo-skill", "SKILL.md"), "x")
	if err := os.Symlink("1.0.0", filepath.Join(store, "packs", "demo", "current")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(store, "registry.yaml"), "packs:\n  - name: demo\n    version: 1.0.0\n    path: "+filepath.Join(store, "packs", "demo", "current")+"\n")
}

// A scratch SIDESHOW_HOME without CLAUDE_CONFIG_DIR still writes skills and
// commands under the default config dir. Sync says where they go
// (aae-orc-c07dl).
func TestSync_NotesWhereBindingsGoWhenOnlyTheStoreIsIsolated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	installOnePack(t)

	out := captureStdout(t, func() {
		if err := Sync(); err != nil {
			t.Errorf("Sync: %v", err)
		}
	})
	want := "SIDESHOW_HOME is set but CLAUDE_CONFIG_DIR is not; skills and commands are written under " + filepath.Join(home, ".claude")
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

func TestSync_NoIsolationNoteWhenBothAreSet(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	installOnePack(t)

	out := captureStdout(t, func() {
		if err := Sync(); err != nil {
			t.Errorf("Sync: %v", err)
		}
	})
	if strings.Contains(out, "SIDESHOW_HOME is set but") {
		t.Errorf("note printed with both variables set:\n%s", out)
	}
}

func TestSync_NoIsolationNoteWhenTheStoreIsTheDefault(t *testing.T) {
	// No SIDESHOW_HOME: the store is the real one, so there is no scratch
	// setup to warn about. Point HOME at a temp dir so nothing real is read.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("SIDESHOW_HOME", "")
	installOnePack(t)

	out := captureStdout(t, func() {
		if err := Sync(); err != nil {
			t.Errorf("Sync: %v", err)
		}
	})
	if strings.Contains(out, "SIDESHOW_HOME is set but") {
		t.Errorf("note printed with SIDESHOW_HOME unset:\n%s", out)
	}
}
