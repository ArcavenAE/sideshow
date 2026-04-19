package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewUUID_Format(t *testing.T) {
	t.Parallel()
	id := newUUID()
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Errorf("UUID has %d parts, want 5: %s", len(parts), id)
	}
	// Version nibble should be '4'
	if len(parts) >= 3 && parts[2][0] != '4' {
		t.Errorf("UUID version nibble = %c, want 4: %s", parts[2][0], id)
	}
}

func TestNewUUID_Unique(t *testing.T) {
	t.Parallel()
	a := newUUID()
	b := newUUID()
	if a == b {
		t.Errorf("two UUIDs are identical: %s", a)
	}
}

func TestInitIdentity_CreatesFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	id, err := InitIdentity(root, "test-project", "repos.yaml")
	if err != nil {
		t.Fatalf("InitIdentity() error: %v", err)
	}

	if id.Name != "test-project" {
		t.Errorf("Name = %q, want test-project", id.Name)
	}
	if id.Manifest != "repos.yaml" {
		t.Errorf("Manifest = %q, want repos.yaml", id.Manifest)
	}
	if id.ID == "" {
		t.Error("ID is empty")
	}
	if id.CreatedAt == "" {
		t.Error("CreatedAt is empty")
	}

	// File should exist
	path := IdentityPath(root)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("identity file not created: %v", err)
	}
}

func TestInitIdentity_Idempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	first, err := InitIdentity(root, "test-project", "repos.yaml")
	if err != nil {
		t.Fatalf("first InitIdentity() error: %v", err)
	}

	second, err := InitIdentity(root, "different-name", "other.yaml")
	if err != nil {
		t.Fatalf("second InitIdentity() error: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("second call changed ID: %s != %s", second.ID, first.ID)
	}
	if second.Name != first.Name {
		t.Errorf("second call changed Name: %s != %s", second.Name, first.Name)
	}
}

func TestLoadIdentity_NotExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	id, err := LoadIdentity(root)
	if err != nil {
		t.Fatalf("LoadIdentity() error: %v", err)
	}
	if id != nil {
		t.Errorf("expected nil for non-existent identity, got %+v", id)
	}
}

func TestLoadIdentity_Roundtrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	created, err := InitIdentity(root, "my-project", "repos.yaml")
	if err != nil {
		t.Fatalf("InitIdentity() error: %v", err)
	}

	loaded, err := LoadIdentity(root)
	if err != nil {
		t.Fatalf("LoadIdentity() error: %v", err)
	}

	if loaded.ID != created.ID {
		t.Errorf("ID mismatch: %s != %s", loaded.ID, created.ID)
	}
	if loaded.Name != created.Name {
		t.Errorf("Name mismatch: %s != %s", loaded.Name, created.Name)
	}
}

func TestLoadReposManifest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := `repos:
  orchestrator:
    path: .
    type: orchestrator
  forestage:
    path: forestage
    type: platform
    remote: git@github.com:ArcavenAE/forestage.git
  switchboard:
    path: switchboard
    type: platform
`
	path := filepath.Join(dir, "repos.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadReposManifest(path)
	if err != nil {
		t.Fatalf("LoadReposManifest() error: %v", err)
	}

	if len(m.Repos) != 3 {
		t.Fatalf("expected 3 repos, got %d", len(m.Repos))
	}

	fs := m.Repos["forestage"]
	if fs.Path != "forestage" {
		t.Errorf("forestage path = %q, want forestage", fs.Path)
	}
	if fs.Name != "forestage" {
		t.Errorf("forestage name not backfilled: %q", fs.Name)
	}
	if fs.Type != "platform" {
		t.Errorf("forestage type = %q, want platform", fs.Type)
	}
}

func TestResolveSubrepos_FiltersOrchestrator(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Create one subrepo dir
	if err := os.MkdirAll(filepath.Join(root, "forestage"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := &ReposManifest{
		Repos: map[string]RepoEntry{
			"orchestrator": {Name: "orchestrator", Path: ".", Type: "orchestrator"},
			"forestage":    {Name: "forestage", Path: "forestage", Type: "platform"},
			"director":     {Name: "director", Path: "director", Type: "platform"},
		},
	}

	repos := ResolveSubrepos(root, m)

	// Should exclude orchestrator
	for _, r := range repos {
		if r.Name == "orchestrator" {
			t.Error("orchestrator should be excluded from subrepos")
		}
	}

	// forestage should be present, director not present
	var forestage, director *Subrepo
	for i := range repos {
		switch repos[i].Name {
		case "forestage":
			forestage = &repos[i]
		case "director":
			director = &repos[i]
		}
	}

	if forestage == nil {
		t.Fatal("forestage not in results")
	}
	if !forestage.Present {
		t.Error("forestage should be present (dir exists)")
	}

	if director == nil {
		t.Fatal("director not in results")
	}
	if director.Present {
		t.Error("director should not be present (dir doesn't exist)")
	}
}

func TestFindReposManifest(t *testing.T) {
	t.Parallel()

	t.Run("exists", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "repos.yaml"), []byte("repos: {}"), 0o644); err != nil {
			t.Fatal(err)
		}
		path := FindReposManifest(dir)
		if path == "" {
			t.Error("FindReposManifest returned empty for existing file")
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := FindReposManifest(dir)
		if path != "" {
			t.Errorf("FindReposManifest returned %q for missing file", path)
		}
	})
}
