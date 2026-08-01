package bindings

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ArcavenAE/sideshow/internal/pack"
	"gopkg.in/yaml.v3"
)

// CustomSources records consumer repos whose _<pack>-custom/skills/
// content is bound on every sync. Without this registry, a sync run
// from outside the consumer repo would not rediscover its custom
// skills and the manifest reconcile would remove them as stale.
type CustomSources struct {
	SchemaVersion string         `yaml:"schema_version"`
	UpdatedAt     string         `yaml:"updated_at"`
	Sources       []CustomSource `yaml:"sources"`
}

// CustomSource is one registered consumer repo + pack pair.
type CustomSource struct {
	Project string `yaml:"project"` // absolute path to the consumer repo root
	Pack    string `yaml:"pack"`
}

// customSourcesPath returns the registry location inside the sideshow
// data dir (sibling of packs/, like sync-manifest.yaml).
func customSourcesPath() string {
	return filepath.Join(filepath.Dir(pack.PacksDir()), "custom-sources.yaml")
}

// loadCustomSources reads the registry. A missing file returns an
// empty registry, not an error.
func loadCustomSources() (*CustomSources, error) {
	data, err := os.ReadFile(customSourcesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &CustomSources{}, nil
		}
		return nil, fmt.Errorf("read custom sources: %w", err)
	}
	var s CustomSources
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse custom sources: %w", err)
	}
	return &s, nil
}

// saveCustomSources persists the registry.
func saveCustomSources(sources []CustomSource) error {
	s := CustomSources{
		SchemaVersion: "0.1.0",
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Sources:       sources,
	}
	data, err := yaml.Marshal(&s)
	if err != nil {
		return fmt.Errorf("marshal custom sources: %w", err)
	}
	path := customSourcesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create sideshow dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write custom sources: %w", err)
	}
	return nil
}

// RegisterCustomSource adds a consumer repo + pack pair to the
// registry. The project path is normalized to absolute. Returns true
// if the pair was newly added, false if it was already registered.
func RegisterCustomSource(project, packName string) (bool, error) {
	abs, err := filepath.Abs(project)
	if err != nil {
		return false, fmt.Errorf("resolve project path: %w", err)
	}

	reg, err := loadCustomSources()
	if err != nil {
		return false, err
	}

	for _, s := range reg.Sources {
		if s.Project == abs && s.Pack == packName {
			return false, nil
		}
	}

	reg.Sources = append(reg.Sources, CustomSource{Project: abs, Pack: packName})
	if err := saveCustomSources(reg.Sources); err != nil {
		return false, err
	}
	return true, nil
}

// UnregisterCustomSource removes a consumer repo + pack pair from the
// registry (the escape hatch registration never had: project init and
// sync auto-register, and until this verb the only exits were deleting
// the directory or hand-editing the registry). Returns true if the
// pair was present and removed. The next 'commands sync' withdraws
// the source's served skills via the ownership reconcile.
func UnregisterCustomSource(project, packName string) (bool, error) {
	abs, err := filepath.Abs(project)
	if err != nil {
		return false, fmt.Errorf("resolve project path: %w", err)
	}

	reg, err := loadCustomSources()
	if err != nil {
		return false, err
	}

	kept := reg.Sources[:0]
	removed := false
	for _, s := range reg.Sources {
		if s.Project == abs && s.Pack == packName {
			removed = true
			continue
		}
		kept = append(kept, s)
	}
	if !removed {
		return false, nil
	}
	if err := saveCustomSources(kept); err != nil {
		return false, err
	}
	return true, nil
}

// ListCustomSources returns the registered sources for status display.
func ListCustomSources() ([]CustomSource, error) {
	reg, err := loadCustomSources()
	if err != nil {
		return nil, err
	}
	return reg.Sources, nil
}
