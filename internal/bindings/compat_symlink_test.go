package bindings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeCompatSymlink_ExactInverse(t *testing.T) {
	t.Parallel()
	store := t.TempDir()
	repo := writeRepoWithUserContent(t)
	target := RepoTarget{RepoDir: repo, Scope: ScopeLocal}

	before := snapshotTree(t, repo)
	artifacts, err := MaterializeCompatSymlink(store, target, "vsdd-factory")
	if err != nil {
		t.Fatalf("MaterializeCompatSymlink: %v", err)
	}

	link := filepath.Join(repo, "plugins", "vsdd-factory")
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("compat symlink missing: %v", err)
	}
	if got != store {
		t.Errorf("symlink target = %s, want the pinned store root %s", got, store)
	}

	if _, err := RemoveRepoArtifacts(target, artifacts); err != nil {
		t.Fatalf("RemoveRepoArtifacts: %v", err)
	}
	if diff := diffSnapshots(before, snapshotTree(t, repo)); len(diff) > 0 {
		t.Errorf("compat symlink not exactly inverted:\n  %s", strings.Join(diff, "\n  "))
	}
}

// The pack's own development repo vendors the real engine at
// plugins/<pack>; enable there must refuse, not clobber.
func TestMaterializeCompatSymlink_RefusesVendoredEngine(t *testing.T) {
	t.Parallel()
	store := t.TempDir()
	repo := t.TempDir()
	engine := filepath.Join(repo, "plugins", "vsdd-factory")
	if err := os.MkdirAll(engine, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(engine, "plugin.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := snapshotTree(t, repo)
	_, err := MaterializeCompatSymlink(store, RepoTarget{RepoDir: repo, Scope: ScopeLocal}, "vsdd-factory")
	if !errors.Is(err, ErrWouldClobber) {
		t.Fatalf("err = %v, want ErrWouldClobber", err)
	}
	if diff := diffSnapshots(before, snapshotTree(t, repo)); len(diff) > 0 {
		t.Errorf("refused symlink still wrote:\n  %s", strings.Join(diff, "\n  "))
	}
}

// A pre-existing plugins/ dir with other content: the symlink goes in
// beside it, and disable removes only the symlink.
func TestMaterializeCompatSymlink_SharesExistingPluginsDir(t *testing.T) {
	t.Parallel()
	store := t.TempDir()
	repo := t.TempDir()
	other := filepath.Join(repo, "plugins", "other-tool")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}

	artifacts, err := MaterializeCompatSymlink(store, RepoTarget{RepoDir: repo, Scope: ScopeLocal}, "vsdd-factory")
	if err != nil {
		t.Fatalf("MaterializeCompatSymlink: %v", err)
	}
	for _, a := range artifacts {
		if a.Kind == ArtifactParentDir {
			t.Errorf("pre-existing plugins dir recorded as created: %+v", a)
		}
	}

	if _, err := RemoveRepoArtifacts(RepoTarget{RepoDir: repo, Scope: ScopeLocal}, artifacts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("sibling plugins content disturbed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "plugins", "vsdd-factory")); !os.IsNotExist(err) {
		t.Errorf("compat symlink survived disable (err=%v)", err)
	}
}

// Fail-closed removal: a ledger row claiming compat-symlink at a path
// holding a real directory refuses rather than deleting the engine.
func TestRemoveRepoArtifacts_CompatSymlinkNeverRemovesRealDir(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	engine := filepath.Join(repo, "plugins", "vsdd-factory")
	if err := os.MkdirAll(engine, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := RemoveRepoArtifacts(RepoTarget{RepoDir: repo, Scope: ScopeLocal}, []RepoArtifact{
		{Kind: ArtifactCompatSymlink, Path: "plugins/vsdd-factory"},
	})
	if err == nil {
		t.Fatal("removal accepted a real directory recorded as a compat symlink")
	}
	if _, statErr := os.Stat(engine); statErr != nil {
		t.Fatalf("real directory removed: %v", statErr)
	}
}

// Containment: compat kinds only allow the exact shapes this package
// writes; anything else outside the harness root still fails closed.
func TestRemoveRepoArtifacts_CompatContainmentShapes(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := RepoTarget{RepoDir: repo, Scope: ScopeLocal}

	for _, bad := range []RepoArtifact{
		{Kind: ArtifactCompatSymlink, Path: "plugins/a/b"},
		{Kind: ArtifactCompatSymlink, Path: "src/thing"},
		{Kind: ArtifactCompatSymlink, Path: "plugins/.."},
		{Kind: ArtifactParentDir, Path: "src"},
		{Kind: ArtifactSkillDir, Path: "plugins/vsdd-factory"},
	} {
		if _, err := RemoveRepoArtifacts(target, []RepoArtifact{bad}); err == nil {
			t.Errorf("artifact %+v accepted, want containment refusal", bad)
		}
	}

	// The legal compat shapes pass containment (nothing on disk, so
	// removal is a no-op rather than an error).
	for _, ok := range []RepoArtifact{
		{Kind: ArtifactCompatSymlink, Path: "plugins/vsdd-factory"},
		{Kind: ArtifactParentDir, Path: "plugins"},
	} {
		if _, err := RemoveRepoArtifacts(target, []RepoArtifact{ok}); err != nil {
			t.Errorf("artifact %+v refused: %v", ok, err)
		}
	}
}
