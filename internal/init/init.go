package init

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// Run creates a minimal _bmad/ config shim in the target project directory.
// This satisfies BMAD's init gate (bmad_init.py check) so agents can
// activate without per-repo BMAD installation.
//
// The shim contains only config.yaml files — no skills, workflows, or
// agent definitions. Those are loaded from the sideshow installation
// by Claude Code's skill discovery.
func Run(projectRoot string, userName string) error {
	if projectRoot == "" {
		var err error
		projectRoot, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	// Expand ~ in project root
	if strings.HasPrefix(projectRoot, "~/") {
		home, _ := os.UserHomeDir()
		projectRoot = filepath.Join(home, projectRoot[2:])
	}

	// Find the installed bmad pack
	packPath, err := findBmadPack()
	if err != nil {
		return err
	}

	// Discover modules by finding config.yaml files in the pack
	modules, err := discoverModules(packPath)
	if err != nil {
		return fmt.Errorf("discover modules: %w", err)
	}

	if len(modules) == 0 {
		return fmt.Errorf("no modules found in bmad pack at %s", packPath)
	}

	bmadDir := filepath.Join(projectRoot, "_bmad")
	created := 0

	for _, mod := range modules {
		destDir := filepath.Join(bmadDir, mod.code)
		destFile := filepath.Join(destDir, "config.yaml")

		// Don't overwrite existing config
		if _, err := os.Stat(destFile); err == nil {
			fmt.Printf("  skip %s/config.yaml (exists)\n", mod.code)
			continue
		}

		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", destDir, err)
		}

		config := mod.config

		// Override user_name if provided
		if userName != "" {
			config = replaceConfigValue(config, "user_name", userName)
		}

		if err := os.WriteFile(destFile, []byte(config), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", destFile, err)
		}

		fmt.Printf("  wrote %s/config.yaml\n", mod.code)
		created++
	}

	// Create _bmad-output directory if output_folder references it
	outputDir := filepath.Join(projectRoot, "_bmad-output")
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
		fmt.Printf("  created _bmad-output/\n")
	}

	if created == 0 {
		fmt.Println("All configs already exist. Nothing to do.")
	} else {
		fmt.Printf("Initialized %d module configs in %s\n", created, bmadDir)
	}

	return nil
}

type moduleConfig struct {
	code   string
	config string // raw YAML content
}

// findBmadPack locates the installed bmad pack via the registry.
func findBmadPack() (string, error) {
	packs, err := pack.List()
	if err != nil {
		return "", fmt.Errorf("list packs: %w", err)
	}

	for _, p := range packs {
		if p.Name == "bmad" {
			resolved, err := filepath.EvalSymlinks(p.Path)
			if err != nil {
				return "", fmt.Errorf("resolve pack path: %w", err)
			}
			return resolved, nil
		}
	}

	return "", fmt.Errorf("bmad pack not installed — run 'sideshow install bmad --from <path>' first")
}

// discoverModules finds all modules in the pack by looking for
// top-level directories that contain a config.yaml file.
func discoverModules(packPath string) ([]moduleConfig, error) {
	var modules []moduleConfig

	entries, err := os.ReadDir(packPath)
	if err != nil {
		return nil, fmt.Errorf("read pack dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		// Skip non-module directories
		name := e.Name()
		if name == "commands" || name == "_config" || name == "assets" || strings.HasPrefix(name, ".") {
			continue
		}

		configPath := filepath.Join(packPath, name, "config.yaml")
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue // no config.yaml — not a configured module
		}

		modules = append(modules, moduleConfig{
			code:   name,
			config: string(data),
		})
	}

	return modules, nil
}

// replaceConfigValue replaces a YAML value in raw config content.
// Works on simple key: value lines without parsing/re-serializing
// to preserve comments and formatting.
func replaceConfigValue(content, key, newValue string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+":") {
			lines[i] = key + ": " + newValue
		}
	}
	return strings.Join(lines, "\n")
}

// Status reports the init state for the current project.
func Status(projectRoot string) error {
	if projectRoot == "" {
		var err error
		projectRoot, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	bmadDir := filepath.Join(projectRoot, "_bmad")
	info, err := os.Stat(bmadDir)
	if err != nil || !info.IsDir() {
		fmt.Println("Not initialized. Run 'sideshow init' to create config shim.")
		return nil
	}

	// Count module configs
	var modules []string
	_ = filepath.WalkDir(bmadDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if d.Name() == "config.yaml" {
			rel, _ := filepath.Rel(bmadDir, filepath.Dir(path))
			modules = append(modules, rel)
		}
		return nil
	})

	if len(modules) == 0 {
		fmt.Printf("_bmad/ exists but no module configs found.\n")
		return nil
	}

	fmt.Printf("Initialized with %d modules:\n", len(modules))
	for _, m := range modules {
		fmt.Printf("  %s\n", m)
	}

	// Check user_name from core config
	coreConfig := filepath.Join(bmadDir, "core", "config.yaml")
	data, err := os.ReadFile(coreConfig)
	if err == nil {
		var cfg map[string]any
		if yaml.Unmarshal(data, &cfg) == nil {
			if name, ok := cfg["user_name"].(string); ok {
				fmt.Printf("User: %s\n", name)
			}
		}
	}

	return nil
}
