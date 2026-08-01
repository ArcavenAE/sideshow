// Package ledger reads the per-repo activation record at
// <sideshow-data>/repo-bindings.yaml (decision aae-orc-d3nq.5,
// docs/repo-bindings-ledger.md). The enable/disable verbs (.7) are
// the writers; everything else — coexist-check, doctor, version-skew
// reporting — reads through here.
package ledger

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ArcavenAE/sideshow/internal/pack"
	"gopkg.in/yaml.v3"
)

// Row is one (repo, pack) binding record.
type Row struct {
	Version       string   `yaml:"version"`
	StorePath     string   `yaml:"store_path"` // absolute version dir, never `current`
	Channel       string   `yaml:"channel"`    // sideshow-native | claude-mp
	Platform      string   `yaml:"platform"`
	SettingsScope string   `yaml:"settings_scope"` // local | project
	EnabledAt     string   `yaml:"enabled_at"`
	Artifacts     []string `yaml:"artifacts"`
	Selection     string   `yaml:"selection"`
}

// Ledger is the whole repo-bindings file.
type Ledger struct {
	SchemaVersion string                    `yaml:"schema_version"`
	Repos         map[string]map[string]Row `yaml:"repos"` // abs repo path -> pack -> row
}

// Path returns the ledger location inside the sideshow data dir.
func Path() string {
	return filepath.Join(filepath.Dir(pack.PacksDir()), "repo-bindings.yaml")
}

// Load reads the ledger. A missing file is an empty ledger, not an
// error; a malformed one is an error (readers fail closed rather
// than reporting a repo as unbound).
func Load(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Ledger{Repos: map[string]map[string]Row{}}, nil
		}
		return nil, fmt.Errorf("read repo-bindings ledger: %w", err)
	}
	var l Ledger
	if err := yaml.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse repo-bindings ledger %s: %w", path, err)
	}
	if l.Repos == nil {
		l.Repos = map[string]map[string]Row{}
	}
	return &l, nil
}

// RepoRow returns the row for (repoDir, packName), or nil.
func (l *Ledger) RepoRow(repoDir, packName string) *Row {
	abs, err := filepath.Abs(repoDir)
	if err != nil {
		return nil
	}
	packs, ok := l.Repos[abs]
	if !ok {
		return nil
	}
	row, ok := packs[packName]
	if !ok {
		return nil
	}
	return &row
}

// RepoDirs returns every repo path the ledger knows — the sweep list
// for machine-scoped operations.
func (l *Ledger) RepoDirs() []string {
	out := make([]string, 0, len(l.Repos))
	for dir := range l.Repos {
		out = append(out, dir)
	}
	return out
}
