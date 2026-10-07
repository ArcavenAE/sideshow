package bindings

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArcavenAE/sideshow/internal/pack"
	"gopkg.in/yaml.v3"
)

// SyncManifest records what the last `commands sync` wrote, per artifact.
// It is the ownership ledger that makes activation flips honest: on the
// next sync, artifacts recorded here but no longer shipped by any active
// pack version are removed instead of lingering as stale mixed-version
// bindings.
type SyncManifest struct {
	SchemaVersion string          `yaml:"schema_version"`
	SyncedAt      string          `yaml:"synced_at"`
	Entries       []ManifestEntry `yaml:"entries"`
}

// ManifestEntry is one synced artifact: the destination path plus the
// pack@version and binding kind that produced it.
type ManifestEntry struct {
	Pack    string `yaml:"pack"`
	Version string `yaml:"version"`
	Kind    string `yaml:"kind"`
	Path    string `yaml:"path"`
}

// manifestPath returns the sync-manifest location inside the sideshow
// data dir (sibling of packs/).
func manifestPath() string {
	return filepath.Join(filepath.Dir(pack.PacksDir()), "sync-manifest.yaml")
}

// loadManifest reads the previous sync manifest. A missing file returns
// an empty manifest, not an error.
// LoadManifest exposes the sync-manifest receipt for read-only
// consumers (doctor). A missing file is an empty manifest, not an
// error; a malformed one is an error.
func LoadManifest() (*SyncManifest, error) {
	return loadManifest()
}

func loadManifest() (*SyncManifest, error) {
	data, err := os.ReadFile(manifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncManifest{}, nil
		}
		return nil, fmt.Errorf("read sync manifest: %w", err)
	}
	var m SyncManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse sync manifest: %w", err)
	}
	return &m, nil
}

// saveManifest writes the sync manifest for the just-completed sync.
func saveManifest(entries []ManifestEntry) error {
	m := SyncManifest{
		SchemaVersion: "0.1.0",
		SyncedAt:      time.Now().UTC().Format(time.RFC3339),
		Entries:       entries,
	}
	data, err := yaml.Marshal(&m)
	if err != nil {
		return fmt.Errorf("marshal sync manifest: %w", err)
	}
	path := manifestPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create sideshow dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write sync manifest: %w", err)
	}
	return nil
}

// reconcile removes artifacts recorded in the previous manifest that are
// absent from the current sync set, then persists the current set. Only
// paths inside the binding target dirs (~/.claude/commands, ~/.claude/
// skills) are ever removed — the manifest is the record that sideshow
// wrote them, and the containment check is the belt to that suspender.
// Returns the manifest entries it removed, so the caller can name what
// stopped resolving (aae-orc-86yp6).
func reconcile(current []ManifestEntry) ([]ManifestEntry, error) {
	prev, err := loadManifest()
	if err != nil {
		return nil, err
	}

	currentPaths := make(map[string]struct{}, len(current))
	for _, e := range current {
		currentPaths[e.Path] = struct{}{}
	}

	var removed []ManifestEntry
	for _, e := range prev.Entries {
		if _, ok := currentPaths[e.Path]; ok {
			continue
		}
		if !withinBindingTargets(e.Path) {
			continue
		}
		if _, statErr := os.Lstat(e.Path); statErr != nil {
			continue // already gone
		}
		if rmErr := os.RemoveAll(e.Path); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: remove stale %s: %v\n", e.Path, rmErr)
			continue
		}
		removed = append(removed, e)
	}

	if err := saveManifest(current); err != nil {
		return removed, err
	}
	return removed, nil
}

// withinBindingTargets reports whether a path is inside one of the
// binding target directories sync writes to.
func withinBindingTargets(path string) bool {
	for _, dir := range []string{claudeCommandsDir(), claudeSkillsDir()} {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			continue
		}
		if rel != "." && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}

// FormatRemoved renders removed manifest entries as one line per
// artifact, sorted by invocation name: the skill directory name, or the
// command file name without .md. Each line names the pack and version
// the artifact came from, so an operator learns at flip time which names
// stopped resolving (aae-orc-86yp6).
func FormatRemoved(entries []ManifestEntry) []string {
	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		name := strings.TrimSuffix(filepath.Base(e.Path), ".md")
		lines = append(lines, fmt.Sprintf("  %s (%s, from %s %s)", name, e.Kind, e.Pack, e.Version))
	}
	sort.Strings(lines)
	return lines
}

// BoundSkillPaths maps each skill-dir artifact the sync manifest
// records for pack to the user-scope path sideshow wrote it at, keyed
// by skill name. It is the receipt of what is bound, so a name appears
// only when a user-scope copy was actually written. A missing manifest
// is an empty map.
func BoundSkillPaths(packName string) (map[string]string, error) {
	m, err := loadManifest()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range m.Entries {
		if e.Pack == packName && e.Kind == "skill-dir" {
			out[filepath.Base(e.Path)] = e.Path
		}
	}
	return out, nil
}
