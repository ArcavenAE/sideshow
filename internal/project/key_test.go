package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With no identity file the id is derived from the resolved path, stable
// across runs and across a symlinked cwd, and nothing is written (#91).
func TestSingleRepoKey_DerivedFromTheResolvedPathAndWritesNothing(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}

	id1, root1, err := SingleRepoKey(repo)
	if err != nil {
		t.Fatal(err)
	}
	id2, root2, _ := SingleRepoKey(repo)
	idL, rootL, _ := SingleRepoKey(link)
	if !strings.HasPrefix(id1, "path:") || len(id1) <= len("path:") {
		t.Fatalf("id = %q, want path:<hash>", id1)
	}
	if id1 != id2 || root1 != root2 {
		t.Errorf("not stable across runs: %q %q vs %q %q", id1, root1, id2, root2)
	}
	if idL != id1 || rootL != root1 || root1 != repo {
		t.Errorf("symlinked cwd differs: %q %q vs %q %q (repo %q)", idL, rootL, id1, root1, repo)
	}
	if _, err := os.Stat(filepath.Join(repo, ".sideshow")); !os.IsNotExist(err) {
		t.Errorf("a .sideshow entry exists in the repo: %v", err)
	}
	other := filepath.Join(base, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if idO, _, _ := SingleRepoKey(other); idO == id1 {
		t.Errorf("two repos share the id %q", id1)
	}
}

// An identity file, when the repo has one, supplies the id.
func TestSingleRepoKey_UsesAnIdentityFileWhenThereIsOne(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want, err := InitIdentity(repo, "demo", "repos.yaml")
	if err != nil {
		t.Fatal(err)
	}
	id, root, err := SingleRepoKey(repo)
	if err != nil {
		t.Fatal(err)
	}
	if id != want.ID || root != repo {
		t.Errorf("key = %q %q, want %q %q", id, root, want.ID, repo)
	}
}
