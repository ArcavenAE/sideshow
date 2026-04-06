package pack

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// InstalledPack represents a pack in the registry.
type InstalledPack struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Path    string `yaml:"path"`
}

// Registry tracks installed packs.
type Registry struct {
	Packs []InstalledPack `yaml:"packs"`
}

// SideshowDir returns the sideshow data directory.
func SideshowDir() string {
	if d := os.Getenv("SIDESHOW_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "sideshow")
}

// PacksDir returns the packs installation directory.
func PacksDir() string {
	return filepath.Join(SideshowDir(), "packs")
}

// RegistryPath returns the registry file path.
func RegistryPath() string {
	return filepath.Join(SideshowDir(), "registry.yaml")
}

// LoadRegistry loads the pack registry.
func LoadRegistry() (*Registry, error) {
	path := RegistryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{}, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var reg Registry
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return &reg, nil
}

// SaveRegistry saves the pack registry.
func (r *Registry) Save() error {
	if err := os.MkdirAll(SideshowDir(), 0o755); err != nil {
		return fmt.Errorf("create sideshow dir: %w", err)
	}
	data, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	return os.WriteFile(RegistryPath(), data, 0o644)
}

// DetectVersion tries to read the version from a pack's manifest.
func DetectVersion(packPath string) string {
	// Try _config/manifest.yaml (bmad format — version nested under installation:)
	manifest := filepath.Join(packPath, "_config", "manifest.yaml")
	data, err := os.ReadFile(manifest)
	if err == nil {
		var m struct {
			Version      string `yaml:"version"`
			Installation struct {
				Version string `yaml:"version"`
			} `yaml:"installation"`
		}
		if yaml.Unmarshal(data, &m) == nil {
			if m.Installation.Version != "" {
				return m.Installation.Version
			}
			if m.Version != "" {
				return m.Version
			}
		}
	}

	// Try pack.yaml
	packYaml := filepath.Join(packPath, "pack.yaml")
	data, err = os.ReadFile(packYaml)
	if err == nil {
		var m struct {
			Version string `yaml:"version"`
		}
		if yaml.Unmarshal(data, &m) == nil && m.Version != "" {
			return m.Version
		}
	}

	return "unknown"
}

// InstallFromLocal copies a pack from a local path to the sideshow directory.
func InstallFromLocal(name, sourcePath string) error {
	// Expand ~ in source path
	if strings.HasPrefix(sourcePath, "~/") {
		home, _ := os.UserHomeDir()
		sourcePath = filepath.Join(home, sourcePath[2:])
	}

	// Verify source exists
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path not found: %s", sourcePath)
	}
	if !info.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", sourcePath)
	}

	// Detect version
	version := DetectVersion(sourcePath)
	fmt.Printf("Installing %s %s from %s\n", name, version, sourcePath)

	// Create destination
	destDir := filepath.Join(PacksDir(), name, version)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create pack dir: %w", err)
	}

	// Copy content
	count := 0
	err = filepath.WalkDir(sourcePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
		count++
		return nil
	})
	if err != nil {
		return fmt.Errorf("copy pack: %w", err)
	}

	// Create current symlink
	currentLink := filepath.Join(PacksDir(), name, "current")
	_ = os.Remove(currentLink) // remove old symlink if exists
	if err := os.Symlink(version, currentLink); err != nil {
		return fmt.Errorf("create current symlink: %w", err)
	}

	// Update registry
	reg, err := LoadRegistry()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	// Remove existing entry for this pack
	var filtered []InstalledPack
	for _, p := range reg.Packs {
		if p.Name != name {
			filtered = append(filtered, p)
		}
	}
	filtered = append(filtered, InstalledPack{
		Name:    name,
		Version: version,
		Path:    filepath.Join(PacksDir(), name, "current"),
	})
	reg.Packs = filtered

	if err := reg.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}

	fmt.Printf("Installed %d files to %s\n", count, destDir)
	fmt.Println("Run 'sideshow commands sync' to update Claude Code commands.")
	return nil
}

// List returns all installed packs.
func List() ([]InstalledPack, error) {
	reg, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	return reg.Packs, nil
}
