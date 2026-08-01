package foreign

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SuppressInRepo and UnsuppressInRepo set (or remove) a repo-side
// enabledPlugins override in the CONSUMER repo's
// .claude/settings.local.json. This is exactly option 2 of the
// refusal menu (RefusalOptions) and the T10 trial mechanism: a
// project/local false beats a user-scope true, so the foreign
// identity stops dispatching in THIS repo only. Nothing here touches
// the foreign install, its cache, or any other repo. Paqn clause (d)
// holds: sideshow never auto-disables or auto-uninstalls a foreign
// install; these run only inside explicit, consented verbs (adopt).
//
// The scope-general writers live in enables.go; these two are the
// narrow repo-suppression pair, kept separate because Unsuppress
// deliberately refuses to touch a true value the user chose.

// SuppressInRepo writes enabledPlugins[identity] = false into the
// repo's settings.local.json, preserving every other key. Reports
// whether the file was created.
func SuppressInRepo(repoDir, identity string) (created bool, err error) {
	return SetEnable(localSettingsPath(repoDir), identity, false)
}

// UnsuppressInRepo removes the identity's override only when it is
// the suppression this package writes (false); a true value is the
// user's own choice and is left alone. Prunes an emptied
// enabledPlugins object.
func UnsuppressInRepo(repoDir, identity string) (removed bool, err error) {
	path := localSettingsPath(repoDir)
	settings, existed, err := readSettingsObject(path)
	if err != nil {
		return false, err
	}
	if !existed {
		return false, nil
	}
	enables, err := enabledPluginsObject(settings, path)
	if err != nil {
		return false, err
	}
	v, ok := enables[identity]
	b, isBool := v.(bool)
	if !ok || !isBool || b {
		return false, nil
	}
	delete(enables, identity)
	if len(enables) == 0 {
		delete(settings, "enabledPlugins")
	} else {
		settings["enabledPlugins"] = enables
	}
	return true, writeSettingsObject(path, settings)
}

func localSettingsPath(repoDir string) string {
	return filepath.Join(repoDir, ".claude", "settings.local.json")
}

func readSettingsObject(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, false, nil
		}
		return nil, false, fmt.Errorf("read settings %s: %w", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, false, fmt.Errorf("parse settings %s (refusing to merge into a file that cannot round-trip): %w", path, err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, true, nil
}

func enabledPluginsObject(settings map[string]any, path string) (map[string]any, error) {
	raw, ok := settings["enabledPlugins"]
	if !ok {
		return map[string]any{}, nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings %s: enabledPlugins is not an object; refusing to merge", path)
	}
	return obj, nil
}

func writeSettingsObject(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}
