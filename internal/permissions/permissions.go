package permissions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Scope determines where permissions are configured.
type Scope int

const (
	// ScopeUser configures ~/.claude/settings.json (global for the user).
	ScopeUser Scope = iota
	// ScopeProject configures {project-root}/.claude/settings.local.json.
	ScopeProject
)

// ClaudeSettings is a partial representation of Claude Code's settings.json.
// We only touch the permissions.allow array, preserving everything else.
type ClaudeSettings struct {
	raw map[string]any
}

// SettingsPath returns the settings file path for the given scope.
func SettingsPath(scope Scope, projectRoot string) string {
	switch scope {
	case ScopeProject:
		return filepath.Join(projectRoot, ".claude", "settings.local.json")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".claude", "settings.json")
	}
}

// LoadSettings reads a Claude Code settings file.
func LoadSettings(path string) (*ClaudeSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ClaudeSettings{raw: make(map[string]any)}, nil
		}
		return nil, fmt.Errorf("read settings: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse settings: %w", err)
	}

	return &ClaudeSettings{raw: raw}, nil
}

// Save writes the settings back to disk, preserving all existing content.
func (s *ClaudeSettings) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}

	data, err := json.MarshalIndent(s.raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// GetAllowList returns the current permissions.allow list.
func (s *ClaudeSettings) GetAllowList() []string {
	perms, ok := s.raw["permissions"].(map[string]any)
	if !ok {
		return nil
	}

	allow, ok := perms["allow"].([]any)
	if !ok {
		return nil
	}

	var result []string
	for _, v := range allow {
		if str, ok := v.(string); ok {
			result = append(result, str)
		}
	}
	return result
}

// AddReadPermission adds a Read() permission for the given path if not already present.
func (s *ClaudeSettings) AddReadPermission(readPath string) bool {
	perm := fmt.Sprintf("Read(%s)", readPath)

	// Check if already present
	for _, existing := range s.GetAllowList() {
		if existing == perm {
			return false // already exists
		}
		// Also check if a parent path is already allowed
		if strings.HasPrefix(perm, existing) {
			return false // parent already allowed
		}
	}

	// Ensure permissions.allow exists
	perms, ok := s.raw["permissions"].(map[string]any)
	if !ok {
		perms = make(map[string]any)
		s.raw["permissions"] = perms
	}

	allow, ok := perms["allow"].([]any)
	if !ok {
		allow = []any{}
	}

	perms["allow"] = append(allow, perm)
	return true
}

// RemoveReadPermission removes a Read() permission for the given path.
func (s *ClaudeSettings) RemoveReadPermission(readPath string) bool {
	perm := fmt.Sprintf("Read(%s)", readPath)

	perms, ok := s.raw["permissions"].(map[string]any)
	if !ok {
		return false
	}

	allow, ok := perms["allow"].([]any)
	if !ok {
		return false
	}

	var filtered []any
	removed := false
	for _, v := range allow {
		if str, ok := v.(string); ok && str == perm {
			removed = true
			continue
		}
		filtered = append(filtered, v)
	}

	if removed {
		perms["allow"] = filtered
	}
	return removed
}

// ConfigureForScope adds the read permission for a sideshow pack path
// to the appropriate Claude Code settings file.
func ConfigureForScope(scope Scope, packPath string, projectRoot string) error {
	settingsPath := SettingsPath(scope, projectRoot)

	settings, err := LoadSettings(settingsPath)
	if err != nil {
		return err
	}

	// Normalize the path — use the parent directory of the packs dir
	// so all packs under it are covered with one permission
	readPath := packPath
	if !strings.HasSuffix(readPath, "/") {
		readPath += "/"
	}

	if !settings.AddReadPermission(readPath) {
		fmt.Printf("  Permission already configured in %s\n", settingsPath)
		return nil
	}

	if err := settings.Save(settingsPath); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}

	fmt.Printf("  Added Read(%s) to %s\n", readPath, settingsPath)
	return nil
}
