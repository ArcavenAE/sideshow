// Package foreign reads and classifies FOREIGN harness plugin state:
// claude-mp (or any marketplace) installs of packs that sideshow
// delivers by repo bindings. It is the conversion-side surface behind
// the coexistence guard (aae-orc-paqn), coexist-check, and adopt.
//
// The census, resolution, and diagnosis surfaces are READ-ONLY.
// Sideshow never auto-retires or auto-uninstalls a foreign install:
// removing a plugin, purging its cache, and retiring its marketplace
// happen only through documented claude verbs the operator runs.
//
// Two consented WRITE surfaces exist, both narrow and both reachable
// only from a verb that asked first: the repo-side suppression override
// (suppress.go, adopt step 1) and the scope-general enabledPlugins
// writers (enables.go) the user-scope migration uses to move a
// machine-wide enable per repo. Both touch enabledPlugins entries in
// settings files and nothing else, which is the class-3 allowlist from
// the preserve taxonomy. Neither touches an install tree.
//
// Ground truth for every parsed shape: aae-orc finding-091 addendum 3
// (executed trials, Claude Code 2.1.220) and
// docs/claude-plugin-conversion-reference.md.
package foreign

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConfigDir returns the harness config directory: CLAUDE_CONFIG_DIR
// when set, else ~/.claude.
func ConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// Install is one entry of a foreign identity's install array in
// installed_plugins.json (format version 2).
type Install struct {
	Identity     string // vsdd-factory@claude-mp
	PluginName   string // vsdd-factory
	Marketplace  string // claude-mp
	Scope        string // user | project | local
	InstallPath  string
	Version      string // registry-recorded; NOT authoritative for git-subdir sources
	GitCommitSha string
	ProjectPath  string // present on project-scope installs
	// TreeVersion is the version read from the installed tree's
	// .claude-plugin/plugin.json — the version authority (the
	// marketplace serves ref-pinned content under a static label).
	// Empty when the installPath is missing or unreadable.
	TreeVersion string
	// Legacy marks the pre-rc.7 identity registered through the
	// retired drbothen/vsdd-factory marketplace.
	Legacy bool
}

// OrphanedEnable is an enabledPlugins entry naming an identity with
// no install record behind it. The harness is SILENT about these at
// both the CLI and session layers (trial T14); only a census finds
// them.
type OrphanedEnable struct {
	Identity string
	Scope    string // user | project | local
	Enabled  bool
	Path     string // settings file carrying the entry
}

// enableEntry is one scope's enabledPlugins verdict for an identity.
type enableEntry struct {
	value bool
	path  string
}

// Census is the machine-level view of foreign installs for one pack.
type Census struct {
	Pack        string
	ConfigDir   string
	Installs    []Install
	userEnables map[string]enableEntry // identity -> user-scope entry
	installed   map[string]bool        // identity -> has install record
	// marketplaces maps marketplace name -> every plugin name it serves
	// on this machine, the census pack included. Retiring a marketplace
	// turns on what ELSE it serves, so this one field looks past the
	// pack filter the rest of the census applies.
	marketplaces map[string][]string
}

// legacyMarketplaces are marketplace names whose identities predate
// the current upstream channel (README migration note; register
// wrinkle plugin-identity-registration).
var legacyMarketplaces = map[string]bool{
	"vsdd-factory": true,
}

// installedPluginsFile mirrors the v2 format observed in trials.
type installedPluginsFile struct {
	Version int                       `json:"version"`
	Plugins map[string][]installEntry `json:"plugins"`
}

type installEntry struct {
	Scope        string `json:"scope"`
	InstallPath  string `json:"installPath"`
	Version      string `json:"version"`
	GitCommitSha string `json:"gitCommitSha"`
	ProjectPath  string `json:"projectPath"`
}

type settingsFile struct {
	EnabledPlugins map[string]bool `json:"enabledPlugins"`
}

// splitIdentity separates <plugin>@<marketplace>. The marketplace
// name is everything after the FIRST @ (plugin names carry no @).
func splitIdentity(identity string) (plugin, marketplace string) {
	if i := strings.Index(identity, "@"); i >= 0 {
		return identity[:i], identity[i+1:]
	}
	return identity, ""
}

// readEnables loads the enabledPlugins map from one settings file.
// A missing file is an empty map; an unreadable or malformed file is
// an error (the guard must not classify on partial state).
func readEnables(path string) (map[string]enableEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]enableEntry{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s settingsFile
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make(map[string]enableEntry, len(s.EnabledPlugins))
	for id, v := range s.EnabledPlugins {
		out[id] = enableEntry{value: v, path: path}
	}
	return out, nil
}

// treeVersion reads the version authority from an installed tree.
func treeVersion(installPath string) string {
	data, err := os.ReadFile(filepath.Join(installPath, ".claude-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.Version
}

// TakeCensus reads the machine-level foreign state for one pack from
// a harness config dir: every install record whose plugin name
// matches the pack (any marketplace, per the name-field-only
// detection rule), plus the user-scope enable entries.
func TakeCensus(configDir, pack string) (*Census, error) {
	c := &Census{
		Pack:         pack,
		ConfigDir:    configDir,
		userEnables:  map[string]enableEntry{},
		installed:    map[string]bool{},
		marketplaces: map[string][]string{},
	}

	regPath := filepath.Join(configDir, "plugins", "installed_plugins.json")
	data, err := os.ReadFile(regPath)
	switch {
	case os.IsNotExist(err):
		// No plugin registry at all: nothing installed.
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", regPath, err)
	default:
		var reg installedPluginsFile
		if err := json.Unmarshal(data, &reg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", regPath, err)
		}
		for identity, entries := range reg.Plugins {
			plugin, mp := splitIdentity(identity)
			if mp != "" {
				c.marketplaces[mp] = append(c.marketplaces[mp], plugin)
			}
			if plugin != pack {
				continue
			}
			c.installed[identity] = true
			for _, e := range entries {
				c.Installs = append(c.Installs, Install{
					Identity:     identity,
					PluginName:   plugin,
					Marketplace:  mp,
					Scope:        e.Scope,
					InstallPath:  e.InstallPath,
					Version:      e.Version,
					GitCommitSha: e.GitCommitSha,
					ProjectPath:  e.ProjectPath,
					TreeVersion:  treeVersion(e.InstallPath),
					Legacy:       legacyMarketplaces[mp],
				})
			}
		}
		sort.Slice(c.Installs, func(i, j int) bool {
			if c.Installs[i].Identity != c.Installs[j].Identity {
				return c.Installs[i].Identity < c.Installs[j].Identity
			}
			return c.Installs[i].Scope < c.Installs[j].Scope
		})
	}

	c.userEnables, err = readEnables(filepath.Join(configDir, "settings.json"))
	if err != nil {
		return nil, err
	}
	return c, nil
}

// UserEnabled reports whether the identity carries a user-scope
// (machine-wide) enable set to true. Callers use it to refuse flows
// that would leave a per-repo-required pack activating everywhere
// (the containment mandate; Diagnose grades the same state ERROR).
func (c *Census) UserEnabled(identity string) bool {
	e, ok := c.userEnables[identity]
	return ok && e.value
}

// RepoView resolves the census against one repo's settings scopes.
type RepoView struct {
	RepoDir string
	// EffectivelyEnabled are foreign identities of the pack that a
	// session started in this repo would load.
	EffectivelyEnabled []string
	// Suppressed are identities enabled at a wider scope but disabled
	// by a narrower one in this repo (the native suppression lever,
	// trial T10).
	Suppressed []string
	// Orphans are enable entries for the pack, at any scope visible
	// from this repo, with no install record behind them.
	Orphans []OrphanedEnable
}

// ResolveRepo computes effective enablement for identities of the
// census pack in one repo. Precedence: local over project over user.
// The project-vs-user edge is trial-verified in both directions
// (T3, T10); local-over-project follows the harness's
// personal-over-shared model and is asserted, not yet trialed.
func (c *Census) ResolveRepo(repoDir string) (*RepoView, error) {
	projEnables, err := readEnables(filepath.Join(repoDir, ".claude", "settings.json"))
	if err != nil {
		return nil, err
	}
	localEnables, err := readEnables(filepath.Join(repoDir, ".claude", "settings.local.json"))
	if err != nil {
		return nil, err
	}

	// Collect every identity of this pack mentioned anywhere.
	identities := map[string]bool{}
	for id := range c.installed {
		identities[id] = true
	}
	for _, scope := range []map[string]enableEntry{c.userEnables, projEnables, localEnables} {
		for id := range scope {
			if plugin, _ := splitIdentity(id); plugin == c.Pack {
				identities[id] = true
			}
		}
	}

	view := &RepoView{RepoDir: repoDir}
	for id := range identities {
		var effective, found, widerEnabled bool
		if e, ok := c.userEnables[id]; ok {
			effective, found = e.value, true
			widerEnabled = e.value
		}
		if e, ok := projEnables[id]; ok {
			if found && widerEnabled && !e.value {
				view.Suppressed = append(view.Suppressed, id)
			}
			effective, found = e.value, true
			widerEnabled = widerEnabled || e.value
		}
		if e, ok := localEnables[id]; ok {
			if found && widerEnabled && !e.value {
				view.Suppressed = append(view.Suppressed, id)
			}
			effective, found = e.value, true
		}
		if found && effective {
			view.EffectivelyEnabled = append(view.EffectivelyEnabled, id)
		}

		if !c.installed[id] {
			for scope, m := range map[string]map[string]enableEntry{
				"user": c.userEnables, "project": projEnables, "local": localEnables,
			} {
				if e, ok := m[id]; ok {
					view.Orphans = append(view.Orphans, OrphanedEnable{
						Identity: id, Scope: scope, Enabled: e.value, Path: e.path,
					})
				}
			}
		}
	}
	sort.Strings(view.EffectivelyEnabled)
	sort.Strings(view.Suppressed)
	sort.Slice(view.Orphans, func(i, j int) bool {
		if view.Orphans[i].Identity != view.Orphans[j].Identity {
			return view.Orphans[i].Identity < view.Orphans[j].Identity
		}
		return view.Orphans[i].Scope < view.Orphans[j].Scope
	})
	return view, nil
}
