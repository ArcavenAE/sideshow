// Package distribute implements project-scope artifact distribution.
//
// Packs declare distributable artifacts in their pack.yaml under a
// `distribute` key. This package reads those declarations and applies
// them to subrepo directories using a safety model that respects
// file ownership (marker-based for sideshow-managed content, merge
// for shared files like settings.json).
package distribute

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Manifest is the distribute section of a pack.yaml.
type Manifest struct {
	Rules     []RuleArtifact     `yaml:"rules,omitempty"`
	Hooks     []HookArtifact     `yaml:"hooks,omitempty"`
	ClaudeMD  []ClaudeMDArtifact `yaml:"claude_md,omitempty"`
	Symlinks  []SymlinkArtifact  `yaml:"symlinks,omitempty"`
	Gitignore []string           `yaml:"gitignore,omitempty"`
}

// RuleArtifact is a file to copy into .claude/rules/.
type RuleArtifact struct {
	Source string `yaml:"source"` // relative to pack root
	Target string `yaml:"target"` // relative to repo root (e.g. .claude/rules/task-workflow.md)
}

// HookArtifact is a hook to merge into .claude/settings.json.
type HookArtifact struct {
	Event   string `yaml:"event"`   // SessionStart, PreCompact, PreToolUse, etc.
	Command string `yaml:"command"` // the command to run
}

// ClaudeMDArtifact is a section to inject into CLAUDE.md.
type ClaudeMDArtifact struct {
	ID     string `yaml:"id"`     // unique section identifier for markers
	Source string `yaml:"source"` // relative to pack root
}

// SymlinkArtifact is a symlink to create in the repo.
type SymlinkArtifact struct {
	Path   string `yaml:"path"`   // where to create the symlink (relative to repo root)
	Target string `yaml:"target"` // what the symlink points to (relative)
}

// PackYAML is the top-level pack.yaml with the distribute section.
type PackYAML struct {
	Name       string   `yaml:"name"`
	Version    string   `yaml:"version"`
	Distribute Manifest `yaml:"distribute"`
}

// LoadPackYAML reads a pack.yaml file and returns its distribute manifest.
func LoadPackYAML(packRoot string) (*PackYAML, error) {
	path := filepath.Join(packRoot, "pack.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pack.yaml: %w", err)
	}

	var p PackYAML
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse pack.yaml: %w", err)
	}
	return &p, nil
}

// IsEmpty reports whether the manifest has no artifacts to distribute.
func (m *Manifest) IsEmpty() bool {
	return len(m.Rules) == 0 &&
		len(m.Hooks) == 0 &&
		len(m.ClaudeMD) == 0 &&
		len(m.Symlinks) == 0 &&
		len(m.Gitignore) == 0
}
