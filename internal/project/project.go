// Package project manages project identity and subrepo discovery.
//
// A project is identified by a UUID stored in .sideshow/project.yaml
// at the project root. This file is checked into git so all checkouts
// share the same identity. The UUID is stable across renames, moves,
// and multiple checkouts (worktrees, multiclaude copies).
package project

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Identity is the content of .sideshow/project.yaml.
type Identity struct {
	ID        string `yaml:"id"`
	Name      string `yaml:"name"`
	CreatedAt string `yaml:"created_at"`
	Manifest  string `yaml:"manifest"`
}

// IdentityPath returns the path to the project identity file.
func IdentityPath(root string) string {
	return filepath.Join(root, ".sideshow", "project.yaml")
}

// LoadIdentity reads the project identity from .sideshow/project.yaml.
// Returns nil, nil if the file does not exist.
func LoadIdentity(root string) (*Identity, error) {
	data, err := os.ReadFile(IdentityPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read project identity: %w", err)
	}

	var id Identity
	if err := yaml.Unmarshal(data, &id); err != nil {
		return nil, fmt.Errorf("parse project identity: %w", err)
	}
	return &id, nil
}

// InitIdentity creates a new .sideshow/project.yaml with a fresh UUID.
// Returns the identity. If the file already exists, returns it unchanged.
func InitIdentity(root, name, manifest string) (*Identity, error) {
	existing, err := LoadIdentity(root)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	id := &Identity{
		ID:        newUUID(),
		Name:      name,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Manifest:  manifest,
	}

	dir := filepath.Join(root, ".sideshow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create .sideshow dir: %w", err)
	}

	data, err := yaml.Marshal(id)
	if err != nil {
		return nil, fmt.Errorf("marshal project identity: %w", err)
	}

	if err := os.WriteFile(IdentityPath(root), data, 0o644); err != nil {
		return nil, fmt.Errorf("write project identity: %w", err)
	}

	return id, nil
}

// newUUID generates a UUID v4 from crypto/rand. No external dependency.
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should never fail on supported platforms
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// RepoEntry is a single repository from repos.yaml.
type RepoEntry struct {
	Name        string
	Path        string `yaml:"path"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Remote      string `yaml:"remote"`
}

// ReposManifest is the parsed repos.yaml.
type ReposManifest struct {
	Repos map[string]RepoEntry `yaml:"repos"`
}

// LoadReposManifest reads and parses repos.yaml.
func LoadReposManifest(path string) (*ReposManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read repos manifest: %w", err)
	}

	var m ReposManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse repos manifest: %w", err)
	}

	// Backfill Name from map key
	for k, v := range m.Repos {
		v.Name = k
		m.Repos[k] = v
	}

	return &m, nil
}

// Subrepo is a resolved subrepo with its absolute path and presence status.
type Subrepo struct {
	Name    string
	RelPath string // relative to project root
	AbsPath string // resolved absolute path
	Present bool   // directory exists on disk
}

// ResolveSubrepos takes a repos manifest and project root, returning
// the list of subrepos with their presence status. Entries with
// type "orchestrator" are excluded (that's the parent, not a subrepo).
func ResolveSubrepos(root string, manifest *ReposManifest) []Subrepo {
	var repos []Subrepo

	for name, entry := range manifest.Repos {
		if strings.EqualFold(entry.Type, "orchestrator") {
			continue
		}

		absPath := filepath.Join(root, entry.Path)
		_, err := os.Stat(absPath)
		present := err == nil

		repos = append(repos, Subrepo{
			Name:    name,
			RelPath: entry.Path,
			AbsPath: absPath,
			Present: present,
		})
	}

	return repos
}

// FindReposManifest looks for repos.yaml in the given directory.
// Returns the path if found, empty string otherwise.
func FindReposManifest(root string) string {
	path := filepath.Join(root, "repos.yaml")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}
