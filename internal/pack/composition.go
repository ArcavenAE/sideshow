package pack

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Composition is the pin disclosure a frozen-composition pack carries in
// pack.yaml from schema 0.2.0 (sideshow-packs build-bmad.sh step 3c,
// aae-orc-soh8q). External modules pinned as of the upstream release date
// never pick up later module releases, fixes included; this block is how
// an installed pack says so.
type Composition struct {
	PinPolicy       string           `yaml:"pin_policy"`
	AsOfDate        string           `yaml:"as_of_date"`
	ExternalModules []ExternalModule `yaml:"external_modules"`
}

// ExternalModule is one module pulled from outside the pack's own
// upstream, at the version the build resolved.
type ExternalModule struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// LoadComposition reads the composition block from root/pack.yaml.
// Returns nil with no error when pack.yaml or the block is absent, which
// is every pack built before schema 0.2.0.
func LoadComposition(root string) (*Composition, error) {
	data, err := os.ReadFile(filepath.Join(root, "pack.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pack.yaml: %w", err)
	}
	var doc struct {
		Composition *Composition `yaml:"composition"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse pack.yaml: %w", err)
	}
	return doc.Composition, nil
}

// Summary is the one-line disclosure list prints. A pack without the
// block says so rather than implying it is current.
func (c *Composition) Summary() string {
	if c == nil || c.PinPolicy == "" {
		return "pin data not recorded"
	}
	if len(c.ExternalModules) == 0 {
		return "no external modules"
	}
	if c.AsOfDate == "" {
		return fmt.Sprintf("external modules not pinned (%s)", c.PinPolicy)
	}
	mods := make([]string, 0, len(c.ExternalModules))
	for _, m := range c.ExternalModules {
		mods = append(mods, m.Name+" "+m.Version)
	}
	date := c.AsOfDate
	if len(date) >= 10 {
		date = date[:10]
	}
	return fmt.Sprintf("external modules pinned as of %s (%s)", date, strings.Join(mods, ", "))
}
