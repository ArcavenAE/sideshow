package pack

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Activation is the pack's activation contract from pack.yaml. Packs
// with PerRepoRequired true are never active by default; Mechanism
// names the binding kind that activates them (e.g. claude-plugin).
type Activation struct {
	DefaultScope          string `yaml:"default_scope"`
	PerRepoRequired       bool   `yaml:"per_repo_required"`
	Mechanism             string `yaml:"mechanism"`
	Runbook               string `yaml:"runbook"`
	ValidatedHarnessFloor string `yaml:"validated_harness_floor"`
}

// knownMechanisms are the activation mechanisms this sideshow version
// recognizes. None are auto-activated yet; recognition only selects
// the calmer install notice over the unrecognized-mechanism warning.
var knownMechanisms = map[string]bool{
	"claude-plugin": true,
}

// PluginClass reports whether the pack activates through a mechanism
// outside sideshow's binding sync. Such packs must not be counted as
// zero-binding defects, and the commands-sync hint does not apply.
func (a *Activation) PluginClass() bool {
	return a != nil && a.Mechanism != ""
}

// LoadActivation reads the activation block from root/pack.yaml.
// Returns nil with no error when pack.yaml or the block is absent.
func LoadActivation(root string) (*Activation, error) {
	data, err := os.ReadFile(filepath.Join(root, "pack.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pack.yaml: %w", err)
	}
	var doc struct {
		Activation *Activation `yaml:"activation"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse pack.yaml: %w", err)
	}
	return doc.Activation, nil
}

// PrintInstallNotice announces the activation contract at install
// time for plugin-class packs: the store copy is producer-validated
// content only, nothing is enabled, and enablement is a documented
// manual step.
func (a *Activation) PrintInstallNotice() {
	if !a.PluginClass() {
		return
	}
	fmt.Printf("NOTE: this pack activates via %q, which sideshow does not activate yet.\n", a.Mechanism)
	fmt.Println("The store copy is producer-validated content; nothing is enabled and 'commands sync' does not apply.")
	if a.PerRepoRequired {
		fmt.Println("Activation is per-repo only; never enable this pack at user scope.")
	}
	if a.Runbook != "" {
		fmt.Printf("Enablement runbook: %s\n", a.Runbook)
	}
	if !knownMechanisms[a.Mechanism] {
		fmt.Printf("WARNING: mechanism %q is not recognized by this sideshow version (recognized: claude-plugin); the pack may need a newer sideshow.\n", a.Mechanism)
	}
}
