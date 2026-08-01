package weave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// storeLinkedRepo builds the aae-orc-a3v6 repro shape: a repo whose
// _bmad/_config is a runtime_links symlink into a store directory outside
// the repo, carrying a manifest the weave must never write through to.
func storeLinkedRepo(t *testing.T) (repoRoot, storeManifest string) {
	t.Helper()
	base := t.TempDir()
	repoRoot = filepath.Join(base, "repo")
	storeConfig := filepath.Join(base, "store", "packs", "bmad", "6.10.0", "_config")
	storeManifest = filepath.Join(storeConfig, "manifest.csv")
	write(t, storeManifest, "header\n")
	if err := os.MkdirAll(filepath.Join(repoRoot, "_bmad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(storeConfig, filepath.Join(repoRoot, "_bmad", "_config")); err != nil {
		t.Fatal(err)
	}
	return repoRoot, storeManifest
}

func TestCSVRefusesWriteThroughRuntimeLink(t *testing.T) {
	root, storeManifest := storeLinkedRepo(t)

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
csv_injections:
  - name: m
    target: _bmad/_config/manifest.csv
    rows:
      - '"pe","Sam"'
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Failed {
		t.Fatalf("outcome = %v (%s), want Failed", res.Actions[0].Outcome, res.Actions[0].Detail)
	}
	if !strings.Contains(res.Actions[0].Detail, "outside the repo") {
		t.Fatalf("detail = %q, want refusal naming the escape", res.Actions[0].Detail)
	}
	if got := read(t, storeManifest); got != "header\n" {
		t.Fatalf("store manifest mutated: %q", got)
	}
}

func TestMemoryRefusesWriteThroughRuntimeLink(t *testing.T) {
	root, storeManifest := storeLinkedRepo(t)

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
memory_injections:
  - targets: [_bmad/_config/manifest.csv]
    anchor: "memories:"
    memories: [remember this]
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Failed {
		t.Fatalf("outcome = %v (%s), want Failed", res.Actions[0].Outcome, res.Actions[0].Detail)
	}
	if !strings.Contains(res.Actions[0].Detail, "outside the repo") {
		t.Fatalf("detail = %q, want refusal naming the escape", res.Actions[0].Detail)
	}
	if got := read(t, storeManifest); got != "header\n" {
		t.Fatalf("store manifest mutated: %q", got)
	}
}

func TestShimRefusesWriteThroughRuntimeLink(t *testing.T) {
	root, _ := storeLinkedRepo(t)

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
slash_commands:
  - id: rogue
    target: _bmad/_config/commands/rogue.md
    body: "hi"
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Failed {
		t.Fatalf("outcome = %v (%s), want Failed", res.Actions[0].Outcome, res.Actions[0].Detail)
	}
	if !strings.Contains(res.Actions[0].Detail, "outside the repo") {
		t.Fatalf("detail = %q, want refusal naming the escape", res.Actions[0].Detail)
	}
	// Nothing may be created on the store side of the link.
	if _, err := os.Stat(filepath.Join(root, "_bmad", "_config", "commands")); !os.IsNotExist(err) {
		t.Fatalf("commands dir created through the link (stat err = %v)", err)
	}
}

func TestLegacyRealTreeStillWeaves(t *testing.T) {
	// Repos carrying a real per-project _bmad/ tree (aae-orc-vaqh) are where
	// weave works as designed; resolution keeps them inside the root.
	root := t.TempDir()
	target := filepath.Join(root, "_bmad", "_config", "manifest.csv")
	write(t, target, "header\n")

	d := loadFrom(t, `schema_version: 0.1.0
pack: bmad
csv_injections:
  - name: m
    target: _bmad/_config/manifest.csv
    rows:
      - '"pe","Sam"'
`)
	res, err := Apply(d, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Outcome != Applied {
		t.Fatalf("outcome = %v (%s), want Applied", res.Actions[0].Outcome, res.Actions[0].Detail)
	}
	if got := read(t, target); got != "header\n\"pe\",\"Sam\"\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestResolveWriteTarget(t *testing.T) {
	t.Parallel()

	t.Run("symlink inside repo is allowed", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		write(t, filepath.Join(root, "real", "f.csv"), "x\n")
		if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")); err != nil {
			t.Fatal(err)
		}
		got, err := resolveWriteTarget(root, "alias/f.csv")
		if err != nil {
			t.Fatalf("resolveWriteTarget: %v", err)
		}
		want, err := filepath.EvalSymlinks(filepath.Join(root, "real", "f.csv"))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("resolved = %q, want %q", got, want)
		}
	})

	t.Run("missing tail resolves through existing ancestors", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		got, err := resolveWriteTarget(root, "new/dir/file.md")
		if err != nil {
			t.Fatalf("resolveWriteTarget: %v", err)
		}
		if !strings.HasSuffix(got, filepath.Join("new", "dir", "file.md")) {
			t.Errorf("resolved = %q, want suffix new/dir/file.md", got)
		}
	})

	t.Run("dot-dot traversal is refused", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if _, err := resolveWriteTarget(root, "../escape.csv"); err == nil {
			t.Error("expected refusal for ../ traversal")
		}
	})

	t.Run("symlinked repo root compares like for like", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		real := filepath.Join(base, "real-repo")
		write(t, filepath.Join(real, "f.csv"), "x\n")
		linkRoot := filepath.Join(base, "link-repo")
		if err := os.Symlink(real, linkRoot); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveWriteTarget(linkRoot, "f.csv"); err != nil {
			t.Errorf("repo root behind a symlink must still weave: %v", err)
		}
	})
}
