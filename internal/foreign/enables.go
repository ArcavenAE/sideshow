package foreign

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Scope names a settings file in the harness precedence chain.
const (
	ScopeLocal   = "local"
	ScopeProject = "project"
	ScopeUser    = "user"
)

// EnableKey is the settings key carrying per-identity plugin
// enablement. Exported so operator-facing text elsewhere can name the
// key it is about to change without spelling the literal: the policy
// guard in cmd/sideshow confines plugin-delivery strings to this
// package, and a message that says "some entries" is worse than no
// message.
const EnableKey = "enabledPlugins"

// SettingsPath returns the settings file for a scope. repoDir is
// ignored for user scope, configDir for the two repo scopes.
func SettingsPath(repoDir, configDir, scope string) (string, error) {
	switch scope {
	case ScopeLocal:
		return filepath.Join(repoDir, ".claude", "settings.local.json"), nil
	case ScopeProject:
		return filepath.Join(repoDir, ".claude", "settings.json"), nil
	case ScopeUser:
		return filepath.Join(configDir, "settings.json"), nil
	default:
		return "", fmt.Errorf("unknown settings scope %q (want local, project, or user)", scope)
	}
}

// EnableEntry is one settings file's verdict for an identity, with the
// file that carries it. A caller restoring a prior posture needs both.
type EnableEntry struct {
	Present bool
	Value   bool
	Path    string
}

// ReadEnable reports the identity's explicit entry in one settings
// file. A missing file or a missing key is Present=false.
func ReadEnable(path, identity string) (EnableEntry, error) {
	entries, err := readEnables(path)
	if err != nil {
		return EnableEntry{}, err
	}
	e, ok := entries[identity]
	if !ok {
		return EnableEntry{Path: path}, nil
	}
	return EnableEntry{Present: true, Value: e.value, Path: path}, nil
}

// SetEnable writes enabledPlugins[identity] = value into one settings
// file, preserving every other key, and reports whether the file had
// to be created.
//
// This is a consented write: callers reach it only from a verb that
// asked (adopt's suppression, the user-scope migration). Nothing here
// touches a foreign install, its cache, or its marketplace.
func SetEnable(path, identity string, value bool) (created bool, err error) {
	settings, existed, err := readSettingsObject(path)
	if err != nil {
		return false, err
	}
	enables, err := enabledPluginsObject(settings, path)
	if err != nil {
		return false, err
	}
	enables[identity] = value
	settings["enabledPlugins"] = enables
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create settings dir for %s: %w", path, err)
	}
	return !existed, writeSettingsObject(path, settings)
}

// DeleteEnable removes the identity's entry from one settings file
// whatever its value, pruning an emptied enabledPlugins object. Use it
// to reverse SetEnable; UnsuppressInRepo is the narrower form that
// refuses to touch a true the user chose.
//
// Removing the last key never removes the FILE: deciding a settings
// file has no reason to exist belongs to the caller that created it,
// which is the only party that knows.
func DeleteEnable(path, identity string) (removed bool, err error) {
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
	if _, ok := enables[identity]; !ok {
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

// CanWriteSettings reports why SetEnable would fail against path,
// without writing anything at all: an unparseable file, an
// enabledPlugins key of the wrong shape, a file that cannot be opened
// for writing, or a directory the write would have to be created under
// that refuses new entries. A dry run calls it so it validates the step
// it prints rather than only the step before it.
//
// It creates nothing, not even the parent directory SetEnable would
// make, because a dry run that leaves a new .claude/ behind has already
// written. For a path whose parents do not exist yet, that costs
// exactness: the nearest existing ancestor is checked by mode bit
// rather than by attempting a write, so an ACL or an ownership quirk
// that a real write would trip can still get past here.
func CanWriteSettings(path string) error {
	settings, existed, err := readSettingsObject(path)
	if err != nil {
		return err
	}
	if _, err := enabledPluginsObject(settings, path); err != nil {
		return err
	}
	if existed {
		f, err := os.OpenFile(path, os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("settings %s is not writable: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close %s after the write check: %w", path, err)
		}
		return nil
	}

	dir := filepath.Dir(path)
	for {
		info, statErr := os.Stat(dir)
		switch {
		case statErr == nil && !info.IsDir():
			return fmt.Errorf("settings %s cannot be written: %s is not a directory", path, dir)
		case statErr == nil && info.Mode().Perm()&0o200 == 0:
			return fmt.Errorf("settings %s cannot be written: %s is not writable (mode %s)", path, dir, info.Mode().Perm())
		case statErr == nil:
			return nil
		case !os.IsNotExist(statErr):
			return fmt.Errorf("stat %s on the way to %s: %w", dir, path, statErr)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("settings %s cannot be written: no existing directory above it", path)
		}
		dir = parent
	}
}

// UserEnabledIdentities returns the census pack's identities carrying a
// user-scope enable set to true, sorted. This is the machine-wide
// posture a per-repo-required pack must not be left in.
func (c *Census) UserEnabledIdentities() []string {
	var out []string
	for id, e := range c.userEnables {
		if plugin, _ := splitIdentity(id); plugin == c.Pack && e.value {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// UserEnablePath returns the settings file carrying the identity's
// user-scope entry, empty when there is none.
func (c *Census) UserEnablePath(identity string) string {
	return c.userEnables[identity].path
}

// WithoutUserEnables returns a census identical to c but with the named
// identities' user-scope entries dropped. Resolving a repo against it
// answers the question the migration turns on: would this repo still
// load the foreign channel if the machine-wide enable were gone?
//
// Install records are shared, not copied. Nothing mutates them.
func (c *Census) WithoutUserEnables(identities []string) *Census {
	drop := make(map[string]bool, len(identities))
	for _, id := range identities {
		drop[id] = true
	}
	clone := &Census{
		Pack:        c.Pack,
		ConfigDir:   c.ConfigDir,
		Installs:    c.Installs,
		userEnables: make(map[string]enableEntry, len(c.userEnables)),
		installed:   c.installed,
	}
	for id, e := range c.userEnables {
		if !drop[id] {
			clone.userEnables[id] = e
		}
	}
	return clone
}

// MarketplaceSiblings returns the OTHER plugin names the marketplace
// serves on this machine, sorted. Empty means the marketplace serves
// only the census pack, which is the condition under which removing it
// is safe (aae-orc-d3nq.22 clause e).
//
// Read from the install registry rather than the marketplace metadata:
// what matters is what would break, and that is what is installed.
func (c *Census) MarketplaceSiblings(marketplace string) []string {
	seen := map[string]bool{}
	for _, plugin := range c.marketplaces[marketplace] {
		if plugin == c.Pack || seen[plugin] {
			continue
		}
		seen[plugin] = true
	}
	out := make([]string, 0, len(seen))
	for plugin := range seen {
		out = append(out, plugin)
	}
	sort.Strings(out)
	return out
}
