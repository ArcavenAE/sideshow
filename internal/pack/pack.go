package pack

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// InstalledPack represents a pack in the registry.
type InstalledPack struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Path        string `yaml:"path"`
	InstalledAt string `yaml:"installed_at,omitempty"`
}

// Registry tracks installed packs and project distributions.
type Registry struct {
	Packs    []InstalledPack `yaml:"packs"`
	Projects []Project       `yaml:"projects,omitempty"`
}

// Project tracks a project identified by UUID with one or more installations.
type Project struct {
	ID            string         `yaml:"id"`
	Installations []Installation `yaml:"installations"`
}

// Installation is one checkout of a project at a specific path.
type Installation struct {
	Root     string             `yaml:"root"`
	LastSeen string             `yaml:"last_seen"`
	Manifest string             `yaml:"manifest"`
	Repos    []RepoDistribution `yaml:"repos,omitempty"`
}

// RepoDistribution tracks what was distributed to a single subrepo.
type RepoDistribution struct {
	Name  string             `yaml:"name"`
	Path  string             `yaml:"path"`
	Packs []PackDistribution `yaml:"packs,omitempty"`
}

// PackDistribution tracks one pack's distribution to a repo.
type PackDistribution struct {
	Pack          string                `yaml:"pack"`
	Version       string                `yaml:"version"`
	Scope         string                `yaml:"scope"`
	DistributedAt string                `yaml:"distributed_at"`
	Artifacts     []DistributedArtifact `yaml:"artifacts,omitempty"`
}

// DistributedArtifact records a single artifact placed in a repo.
type DistributedArtifact struct {
	Type      string `yaml:"type"`                 // rules, hook, claude_md, symlink, gitignore
	Path      string `yaml:"path,omitempty"`       // file path relative to repo root
	Checksum  string `yaml:"checksum,omitempty"`   // sha256:hex for file artifacts
	Event     string `yaml:"event,omitempty"`      // for hook type: SessionStart, PreCompact, etc.
	Command   string `yaml:"command,omitempty"`    // for hook type: the command string
	SectionID string `yaml:"section_id,omitempty"` // for claude_md type
	Target    string `yaml:"target,omitempty"`     // for symlink type: what it points to
	Line      string `yaml:"line,omitempty"`       // for gitignore type
}

// FindProject returns the project with the given UUID, or nil.
func (r *Registry) FindProject(id string) *Project {
	for i := range r.Projects {
		if r.Projects[i].ID == id {
			return &r.Projects[i]
		}
	}
	return nil
}

// FindOrCreateProject returns the project with the given UUID,
// creating it if it doesn't exist.
func (r *Registry) FindOrCreateProject(id string) *Project {
	if p := r.FindProject(id); p != nil {
		return p
	}
	r.Projects = append(r.Projects, Project{ID: id})
	return &r.Projects[len(r.Projects)-1]
}

// FindInstallation returns the installation at the given root path, or nil.
func (p *Project) FindInstallation(root string) *Installation {
	for i := range p.Installations {
		if p.Installations[i].Root == root {
			return &p.Installations[i]
		}
	}
	return nil
}

// FindOrCreateInstallation returns the installation at the given root,
// creating it if it doesn't exist.
func (p *Project) FindOrCreateInstallation(root, manifest string) *Installation {
	if inst := p.FindInstallation(root); inst != nil {
		return inst
	}
	p.Installations = append(p.Installations, Installation{
		Root:     root,
		Manifest: manifest,
	})
	return &p.Installations[len(p.Installations)-1]
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

// ValidateShape returns nil if sourcePath looks like a recognized pack
// shape sideshow knows how to install. Two shapes are accepted today:
//
//  1. bmad installer output — marked by _config/manifest.yaml
//     (the layout npx bmad-method install produces, with or without
//     the _bmad/ prefix)
//  2. sideshow-native pack — marked by pack.yaml at the root
//
// Common rejection: an upstream npm source tarball (BMAD-METHOD/
// repo, with src/ + tools/ + package.json but no _config/manifest.yaml)
// is NOT installable as a pack — the upstream installer must run first
// to emit installer output. ValidateShape detects this case and returns
// a specific error pointing at the next step.
func ValidateShape(sourcePath string) error {
	if hasFile(sourcePath, "_config", "manifest.yaml") ||
		hasFile(sourcePath, "_bmad", "_config", "manifest.yaml") {
		return nil
	}
	if hasFile(sourcePath, "pack.yaml") {
		return nil
	}

	// Diagnose the most common rejection: looks like an upstream npm
	// source tarball (e.g. tar -xzf bmad-method-6.5.0.tgz).
	if hasFile(sourcePath, "package.json") &&
		(hasDir(sourcePath, "src") || hasDir(sourcePath, "tools")) {
		return fmt.Errorf(
			"source path %q looks like an upstream npm source tarball, "+
				"not an installable pack. Sideshow installs the OUTPUT of "+
				"the upstream installer (which contains _config/manifest.yaml), "+
				"not the source repo. Run the upstream installer first "+
				"(e.g. `npx bmad-method install --output-folder /tmp/build`) "+
				"and pass --from <build-dir>",
			sourcePath,
		)
	}

	return fmt.Errorf(
		"source path %q does not look like a sideshow-installable pack: "+
			"expected either an installer-output layout (_config/manifest.yaml) "+
			"or a sideshow-native pack (pack.yaml at the root)",
		sourcePath,
	)
}

func hasFile(root string, parts ...string) bool {
	info, err := os.Stat(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func hasDir(root string, name string) bool {
	info, err := os.Stat(filepath.Join(root, name))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// hasInstallerSiblingLayout reports whether sourcePath looks like the
// raw output of the upstream bmad installer: an _bmad/ directory
// containing _config/manifest.yaml plus a sibling .claude/skills/
// directory. This is the layout `npx bmad-method install
// --output-folder X` produces. Sideshow can ingest this directly by
// stripping the _bmad/ prefix; without the auto-detect, users have to
// manually restructure before running `sideshow install`.
func hasInstallerSiblingLayout(sourcePath string) bool {
	if !hasFile(sourcePath, "_bmad", "_config", "manifest.yaml") {
		return false
	}
	if !hasDir(sourcePath, ".claude") {
		return false
	}
	// Defensive: if _config/manifest.yaml ALSO exists at the root, the
	// pack has already been unified — don't strip a prefix that isn't
	// load-bearing.
	if hasFile(sourcePath, "_config", "manifest.yaml") {
		return false
	}
	return true
}

// DetectVersion tries to read the version from a pack's manifest.
//
// Supports three --from layouts:
//
//  1. --from path/to/_bmad (the _bmad/ dir itself)
//     manifest at: _config/manifest.yaml
//
//  2. --from path/to/project (project root containing _bmad/)
//     manifest at: _bmad/_config/manifest.yaml
//
//  3. --from path/to/source-repo (BMAD source repo, e.g. BMAD-METHOD)
//     version in: package.json "version" field
//
//  4. Non-BMAD packs: pack.yaml "version" field
func DetectVersion(packPath string) string {
	if v := detectBmadManifestVersion(packPath); v != "" {
		return v
	}
	if v := detectPackageJSONVersion(packPath); v != "" {
		return v
	}
	if v := detectPackYamlVersion(packPath); v != "" {
		return v
	}
	return "unknown"
}

// detectBmadManifestVersion checks for a BMAD _config/manifest.yaml
// at two relative locations: as a direct child (--from points at _bmad/)
// or nested under _bmad/ (--from points at the project root).
func detectBmadManifestVersion(packPath string) string {
	candidates := []string{
		filepath.Join(packPath, "_config", "manifest.yaml"),
		filepath.Join(packPath, "_bmad", "_config", "manifest.yaml"),
	}
	for _, manifest := range candidates {
		data, err := os.ReadFile(manifest)
		if err != nil {
			continue
		}
		var m struct {
			Version      string `yaml:"version"`
			Installation struct {
				Version string `yaml:"version"`
			} `yaml:"installation"`
		}
		if yaml.Unmarshal(data, &m) != nil {
			continue
		}
		if m.Installation.Version != "" {
			return m.Installation.Version
		}
		if m.Version != "" {
			return m.Version
		}
	}
	return ""
}

// detectPackageJSONVersion reads version from a package.json file.
// This handles the BMAD source repo case (e.g. BMAD-METHOD/) where
// no _config/manifest.yaml exists — the manifest is generated during
// installation, not present in the source.
func detectPackageJSONVersion(packPath string) string {
	data, err := os.ReadFile(filepath.Join(packPath, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return ""
	}
	return pkg.Version
}

// detectPackYamlVersion reads version from a pack.yaml file.
// This handles non-BMAD packs (spectacle, custom packs, etc.)
func detectPackYamlVersion(packPath string) string {
	data, err := os.ReadFile(filepath.Join(packPath, "pack.yaml"))
	if err != nil {
		return ""
	}
	var m struct {
		Version string `yaml:"version"`
	}
	if yaml.Unmarshal(data, &m) != nil {
		return ""
	}
	return m.Version
}

// Activate points a pack's current symlink at an installed version and
// updates the registry to match. The version must already be installed.
// Bindings are NOT synced here — callers run bindings.Sync after.
func Activate(name, version string) error {
	versionDir := filepath.Join(PacksDir(), name, version)
	info, err := os.Stat(versionDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("pack %s version %s is not installed at %s (run 'sideshow list' to see installed versions)", name, version, versionDir)
	}

	currentLink := filepath.Join(PacksDir(), name, "current")
	_ = os.Remove(currentLink)
	if err := os.Symlink(version, currentLink); err != nil {
		return fmt.Errorf("update current symlink: %w", err)
	}

	reg, err := LoadRegistry()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	found := false
	for i := range reg.Packs {
		if reg.Packs[i].Name == name {
			reg.Packs[i].Version = version
			found = true
		}
	}
	if !found {
		reg.Packs = append(reg.Packs, InstalledPack{
			Name:        name,
			Version:     version,
			Path:        currentLink,
			InstalledAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return reg.Save()
}

// InstalledVersions returns every version directory installed for a pack
// (sorted) plus the version the current symlink points at ("" if none).
func InstalledVersions(name string) (versions []string, active string, err error) {
	dir := filepath.Join(PacksDir(), name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", err
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != "current" {
			versions = append(versions, e.Name())
		}
	}
	sort.Strings(versions)
	active, _ = os.Readlink(filepath.Join(dir, "current"))
	return versions, active, nil
}

// InstallFromLocal copies a pack from a local path to the sideshow directory.
func InstallFromLocal(name, sourcePath string, activate bool) error {
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

	// Validate pack shape before copying. Sideshow accepts two shapes:
	//   - installer output (bmad-style, marked by _config/manifest.yaml)
	//   - sideshow-native pack (marked by pack.yaml)
	// An npm source tarball (package.json + src/ + tools/) is NOT a pack
	// — the upstream installer must run first to produce installable output.
	if err := ValidateShape(sourcePath); err != nil {
		return err
	}

	// Detect bmad installer-output sibling layout. The upstream
	// installer (npx bmad-method install --output-folder X) writes
	// X/_bmad/ (pack content) AND X/.claude/ (IDE bindings) as siblings.
	// Sideshow's on-disk pack format expects content at the pack root
	// (no _bmad/ prefix) — so when the sibling layout is detected, strip
	// the _bmad/ prefix during copy. .claude/ stays where it is.
	stripPrefix := ""
	if hasInstallerSiblingLayout(sourcePath) {
		stripPrefix = "_bmad"
		fmt.Println("Detected bmad installer-output layout; unifying _bmad/ + .claude/ into pack root.")
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

		// Apply installer-output unification: drop the bare _bmad
		// directory itself (we recurse into its contents) and strip
		// the _bmad/ prefix from anything inside it. Sibling .claude/
		// passes through unchanged.
		if stripPrefix != "" {
			if relPath == stripPrefix {
				return nil
			}
			sep := string(os.PathSeparator)
			if strings.HasPrefix(relPath, stripPrefix+sep) {
				relPath = strings.TrimPrefix(relPath, stripPrefix+sep)
			}
		}

		destPath := filepath.Join(destDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		perm := info.Mode().Perm()

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if err := os.WriteFile(destPath, data, perm); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
		// WriteFile applies perm only at create time (and subject to
		// umask); force the exact source mode so exec bits survive
		// reinstalls over an existing tree. Read-only store freeze
		// (aae-orc-dihj) must map 0755 -> 0555 here, never 0444.
		if err := os.Chmod(destPath, perm); err != nil {
			return fmt.Errorf("chmod %s: %w", destPath, err)
		}
		count++
		return nil
	})
	if err != nil {
		return fmt.Errorf("copy pack: %w", err)
	}

	// When the pack ships an executable census, the installed tree must
	// match it. The build pipeline treats exec bits as a fatal invariant;
	// the install path must not silently destroy the property at the
	// last hop.
	if err := verifyExecManifest(sourcePath, destDir); err != nil {
		return err
	}

	activation, err := LoadActivation(destDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: read activation contract: %v\n", err)
	}

	// Create/flip the current symlink. A first install always activates
	// (current + registry must exist); later installs honor the caller's
	// activate flag so installing is no longer silently activating.
	currentLink := filepath.Join(PacksDir(), name, "current")
	_, curErr := os.Lstat(currentLink)
	firstInstall := os.IsNotExist(curErr)
	if !activate && !firstInstall {
		fmt.Printf("Installed %d files to %s\n", count, destDir)
		fmt.Printf("Not activated: current stays as-is. Run 'sideshow use %s %s' to activate.\n", name, version)
		activation.PrintInstallNotice()
		return nil
	}
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
		Name:        name,
		Version:     version,
		Path:        filepath.Join(PacksDir(), name, "current"),
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	})
	reg.Packs = filtered

	if err := reg.Save(); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}

	fmt.Printf("Installed %d files to %s\n", count, destDir)
	if activation.PluginClass() {
		activation.PrintInstallNotice()
	} else {
		fmt.Println("Run 'sideshow commands sync' to update Claude Code commands.")
	}
	return nil
}

// execManifestName is the optional executable census emitted by the
// sideshow-packs build pipeline: one pack-root-relative path per line,
// each of which must carry an exec bit in the installed tree.
const execManifestName = "exec-manifest.txt"

// verifyExecManifest checks the installed tree at destDir against the
// pack's exec-manifest.txt when the source ships one. Absence of the
// manifest is not an error; any listed path that is missing or
// non-executable after install is.
func verifyExecManifest(sourcePath, destDir string) error {
	data, err := os.ReadFile(filepath.Join(sourcePath, execManifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", execManifestName, err)
	}

	var drift []string
	for _, line := range strings.Split(string(data), "\n") {
		rel := strings.TrimSpace(line)
		if rel == "" || strings.HasPrefix(rel, "#") {
			continue
		}
		info, err := os.Stat(filepath.Join(destDir, filepath.FromSlash(rel)))
		switch {
		case err != nil:
			drift = append(drift, rel+" (missing)")
		case info.Mode().Perm()&0o100 == 0:
			drift = append(drift, rel+" (not executable)")
		}
	}
	if len(drift) > 0 {
		return fmt.Errorf(
			"%s verification failed: installed tree at %s does not match "+
				"the pack's executable census (%d of its entries drifted); "+
				"the store copy should not be used:\n  %s",
			execManifestName, destDir, len(drift), strings.Join(drift, "\n  "),
		)
	}
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
