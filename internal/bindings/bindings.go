// Package bindings manages tool-config integrations that a content pack
// ships — flat .claude/commands/*.md files (bmad 6.2.2 era), .claude/skills/
// <name>/ directories (bmad 6.3.0 era), and future shapes like Cursor rules
// or Windsurf skills. One pack may carry several bindings.
package bindings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// Binding is the integration surface between a pack and a tool-config
// target directory. Implementations discover their source content from a
// pack path at construction time; Sync writes the resolved content into
// the user's tool-config directory.
type Binding interface {
	// Kind returns a stable identifier like "markdown-command" or
	// "skill-dir" — used for diagnostics and for selecting sync targets.
	Kind() string

	// PackName returns the owning pack's name.
	PackName() string

	// PackVersion returns the owning pack's version.
	PackVersion() string

	// Sync installs the binding's artifacts into its tool-config target.
	// Returns the number of artifacts written and the destination paths
	// actually written, in write order. A path is listed only after its
	// write succeeded, so on error the list is what landed before it; the
	// sync manifest records writers from it (sideshow#161).
	Sync() (int, []string, error)

	// Artifacts returns the destination paths this binding owns — the
	// exact set Sync writes. Used for the sync-manifest ownership ledger
	// so stale artifacts from a previously active version are removed.
	Artifacts() ([]string, error)

	// Validate checks the binding is internally consistent.
	Validate() error
}

// DiscoverBindings inspects an installed pack and returns every binding it
// ships. A pack with both commands and skills content returns two
// bindings; a pack with neither returns zero.
func DiscoverBindings(p pack.InstalledPack) ([]Binding, error) {
	packPath, err := filepath.EvalSymlinks(p.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve pack path %s: %w", p.Path, err)
	}

	// Plugin-layout trees are refused here structurally, independent of
	// the activation-block guard in Sync: even a plugin pack with no
	// pack.yaml at all (so the activation guard cannot fire) never has
	// its content discovered for user scope. The per-repo path uses
	// DiscoverPluginLayout instead.
	if IsPluginLayout(packPath) {
		return nil, fmt.Errorf("%s %s: %w", p.Name, p.Version, ErrPluginLayout)
	}

	var result []Binding

	if hasMarkdownCommandContent(packPath) {
		result = append(result, NewMarkdownCommandBinding(p.Name, p.Version, packPath))
	}

	if hasSkillDirContent(packPath) {
		result = append(result, NewSkillDirBinding(p.Name, p.Version, packPath))
	}

	return result, nil
}

// Sync discovers and syncs every binding for every installed pack plus
// every registered custom source, printing a human-readable summary to
// stdout for CLI consumption. When run inside a consumer repo that has
// bindable custom skills for an installed pack, the repo is
// auto-registered as a custom source so later syncs from elsewhere
// keep its skills alive.
func Sync() error {
	packs, err := pack.List()
	if err != nil {
		return err
	}

	if len(packs) == 0 {
		fmt.Println("No packs installed. Run 'sideshow install <pack> --from <path>' first.")
		return nil
	}

	// A scratch store with the default config dir still writes skills and
	// commands where a real setup keeps them; say where (aae-orc-c07dl).
	if os.Getenv("SIDESHOW_HOME") != "" && os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		fmt.Printf("SIDESHOW_HOME is set but CLAUDE_CONFIG_DIR is not; skills and commands are written under %s\n", foreign.ConfigDir())
	}

	var all []Binding
	packSkillOwners := make(map[string]string)
	for _, p := range packs {
		// Plugin-class packs activate outside the binding sync
		// (per-repo, via their declared mechanism). Announce them
		// instead of silently counting zero bindings, and never let
		// their content leak into user-scope bindings.
		act, actErr := pack.LoadActivation(p.Path)
		if actErr != nil {
			// Fail closed: an unreadable activation block could be
			// hiding a per-repo-only declaration, so the pack is
			// excluded from user-scope sync entirely.
			fmt.Fprintf(os.Stderr, "ERROR: %s %s: activation unreadable (%v); excluded from user-scope sync\n",
				p.Name, p.Version, actErr)
			continue
		}
		if act.PluginClass() {
			fmt.Printf("%s %s: plugin-class pack (%s); bindings do not apply, see the pack's enablement runbook\n",
				p.Name, p.Version, act.Mechanism)
			continue
		}
		if act != nil && act.PerRepoRequired {
			fmt.Printf("%s %s: per-repo-required pack; user-scope bindings do not apply, see the pack's enablement runbook\n",
				p.Name, p.Version)
			continue
		}
		discovered, err := DiscoverBindings(p)
		if errors.Is(err, ErrPluginLayout) {
			fmt.Fprintf(os.Stderr, "ERROR: %s %s: plugin-layout tree with no readable activation contract; excluded from user-scope sync\n",
				p.Name, p.Version)
			continue
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: discover %s: %v\n", p.Name, err)
			continue
		}
		all = append(all, discovered...)

		if resolved, evalErr := filepath.EvalSymlinks(p.Path); evalErr == nil {
			for id := range skillCanonicalIds(resolved) {
				packSkillOwners[id] = p.Name
			}
		}
	}

	if cwd, cwdErr := os.Getwd(); cwdErr == nil {
		autoRegisterCustomSources(cwd, packs)
	}

	custom, err := discoverCustomBindings(packs, packSkillOwners)
	if err != nil {
		// Fail closed before anything is written: a collision (or an
		// unreadable source registry) must block the sync loudly, not
		// downgrade to a warning while the sync serves a silently
		// chosen winner (sideshow#110 ruling).
		return fmt.Errorf("custom sources: %w", err)
	}
	all = append(all, custom...)

	totalSynced, removed, err := runSync(all)
	if err != nil {
		return err
	}

	fmt.Printf("Synced %d artifacts across all bindings\n", totalSynced)
	if len(removed) > 0 {
		fmt.Printf("Removed %d stale artifact(s) from a previously active version; these no longer resolve:\n", len(removed))
		for _, line := range FormatRemoved(removed) {
			fmt.Println(line)
		}
	}
	return nil
}

// autoRegisterCustomSources registers the working directory as a custom
// source for every installed pack it carries bindable custom skills
// for. Best-effort: registration failures warn, never fail the sync.
func autoRegisterCustomSources(cwd string, packs []pack.InstalledPack) {
	for _, p := range packs {
		if !hasCustomSkillContent(cwd, p.Name) {
			continue
		}
		added, err := RegisterCustomSource(cwd, p.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: register custom source %s: %v\n", cwd, err)
			continue
		}
		if added {
			fmt.Printf("Registered custom source %s (pack %s)\n", cwd, p.Name)
		}
	}
}

// discoverCustomBindings loads the custom-source registry and returns a
// binding per source that still exists, has its pack installed, and has
// bindable skills. Skill names already owned by a pack (canonical id)
// or claimed by an earlier source are skipped with a warning — pack
// content wins collisions. Sources whose project directory no longer
// exists are pruned from the registry; their previously synced skills
// fall out via manifest reconciliation.
func discoverCustomBindings(packs []pack.InstalledPack, packSkillOwners map[string]string) ([]Binding, error) {
	reg, err := loadCustomSources()
	if err != nil {
		return nil, err
	}
	if len(reg.Sources) == 0 {
		return nil, nil
	}

	installed := make(map[string]struct{}, len(packs))
	for _, p := range packs {
		installed[p.Name] = struct{}{}
	}

	var kept []CustomSource
	pruned := false
	claimed := make(map[string]CustomSource) // skill name -> claiming source
	var out []Binding

	for _, s := range reg.Sources {
		if _, statErr := os.Stat(s.Project); statErr != nil {
			fmt.Fprintf(os.Stderr, "warning: custom source %s missing on disk — pruning from registry\n", s.Project)
			pruned = true
			continue
		}
		kept = append(kept, s)

		if _, ok := installed[s.Pack]; !ok {
			continue
		}

		var skills []string
		for _, name := range customSkillIds(s.Project, s.Pack) {
			if owner, ok := packSkillOwners[name]; ok {
				fmt.Fprintf(os.Stderr, "warning: skipping custom skill %s from %s: name owned by pack %s\n", name, s.Project, owner)
				continue
			}
			if prev, ok := claimed[name]; ok {
				// Ruled 2026-08-01 (sideshow#110), content-aware
				// amendment: identical content is a benign duplicate
				// (worktrees and second checkouts of one project are
				// routine here) and one copy serves; differing
				// content refuses the sync instead of silently
				// serving the registry-order winner. A user editing
				// their own skill must never wonder why the edit
				// does not take effect.
				prevDir := filepath.Join(customSkillsDir(prev.Project, prev.Pack), name)
				curDir := filepath.Join(customSkillsDir(s.Project, s.Pack), name)
				same, cmpErr := sameDirContent(prevDir, curDir)
				if cmpErr != nil {
					return nil, fmt.Errorf("compare custom skill %q between %s and %s: %w", name, prev.Project, s.Project, cmpErr)
				}
				if same {
					fmt.Fprintf(os.Stderr, "note: custom skill %s in %s is byte-identical to the copy in %s; serving one copy\n", name, s.Project, prev.Project)
					continue
				}
				return nil, fmt.Errorf(
					"custom skill %q is declared with differing content by two registered repos: %s and %s; user-scope skills are shared across all registered repos, so rename the skill in one of them, or run 'sideshow project unregister %s' in the repo that should stop serving it, before sync can proceed",
					name, prev.Project, s.Project, s.Pack,
				)
			}
			claimed[name] = s
			skills = append(skills, name)
		}
		if len(skills) == 0 {
			continue
		}
		out = append(out, NewCustomSkillDirBinding(s.Pack, s.Project, skills))
	}

	if pruned {
		if saveErr := saveCustomSources(kept); saveErr != nil {
			fmt.Fprintf(os.Stderr, "warning: save custom sources after prune: %v\n", saveErr)
		}
	}
	return out, nil
}

// runSync syncs every binding, then reconciles the sync manifest:
// artifacts recorded by the previous sync but owned by no current
// binding are removed (the stale-binding chimera fix — an activation
// flip no longer leaves the old version's extra skills behind).
func runSync(all []Binding) (synced int, removed []ManifestEntry, err error) {
	var current []ManifestEntry
	var failures []FailedBinding
	fail := func(b Binding, e error) {
		failures = append(failures, FailedBinding{Pack: b.PackName(), Version: b.PackVersion(), Kind: b.Kind(), Error: e.Error()})
	}

	warnCollisions(all)

	for _, b := range all {
		n, written, syncErr := b.Sync()
		synced += n
		if syncErr == nil && len(written) == 0 { // red stub: old behavior
			written, _ = b.Artifacts()
		}
		// Record every path this binding wrote, a failed binding's
		// included, so the last entry for a path is the writer whose
		// bytes are on disk.
		seen := map[string]bool{}
		for _, a := range written {
			if seen[a] {
				continue
			}
			seen[a] = true
			current = append(current, ManifestEntry{
				Pack:    b.PackName(),
				Version: b.PackVersion(),
				Kind:    b.Kind(),
				Path:    a,
			})
		}
		if syncErr != nil {
			fail(b, syncErr)
			fmt.Fprintf(os.Stderr, "warning: sync %s/%s: %v\n", b.PackName(), b.Kind(), syncErr)
			continue
		}

		if _, artErr := b.Artifacts(); artErr != nil {
			fail(b, artErr)
			fmt.Fprintf(os.Stderr, "warning: enumerate %s/%s artifacts: %v\n", b.PackName(), b.Kind(), artErr)
		}
	}

	if len(failures) > 0 {
		// A failed binding's artifacts are missing from the current
		// set, so reconcile would remove content that binding still
		// owns. Save what the succeeding bindings wrote, plus the
		// previous record for every path none of them claimed, marked
		// incomplete; the next completed sync reconciles. A sync that
		// writes 0 of N must not exit 0 (sideshow#108).
		complete := false
		if serr := saveManifestWith(mergePrevious(current), &complete, failures); serr != nil {
			fmt.Fprintf(os.Stderr, "warning: save incomplete manifest: %v\n", serr)
		}
		return synced, nil, fmt.Errorf("%d binding(s) failed to sync; the manifest is saved as incomplete and stale reconcile is skipped so a failed binding's artifacts are not removed", len(failures))
	}

	removed, err = reconcile(current)
	return synced, removed, err
}

// mergePrevious puts the previous manifest's entries for paths that no
// current entry claims ahead of the current entries, which stay in sync
// order so the last entry for a path is still the writer on disk.
func mergePrevious(current []ManifestEntry) []ManifestEntry {
	prev, err := loadManifest()
	if err != nil || prev == nil {
		return current
	}
	claimed := map[string]bool{}
	for _, e := range current {
		claimed[e.Path] = true
	}
	var out []ManifestEntry
	for _, e := range prev.Entries {
		if !claimed[e.Path] {
			out = append(out, e)
		}
	}
	return append(out, current...)
}

// warnCollisions says, before any write, which destination paths this
// sync will write for a pack other than the one that last wrote them: two
// packs in this run shipping the same id, or one pack about to overwrite
// what the previous sync recorded for another (aae-orc-em8e). It only
// warns. What sync writes, the manifest it saves, and the exit status are
// unchanged, so the last writer still wins; a path is named once per
// sync. A binding whose artifacts cannot be listed is left to the sync
// itself to report.
func warnCollisions(all []Binding) {
	writers := map[string][]string{} // path -> distinct packs, in sync order
	var order []string
	for _, b := range all {
		arts, err := b.Artifacts()
		if err != nil {
			continue
		}
		for _, a := range arts {
			ps, seen := writers[a]
			if !seen {
				order = append(order, a)
			}
			if !slices.Contains(ps, b.PackName()) {
				writers[a] = append(ps, b.PackName())
			}
		}
	}

	// Manifest order is sync order, so the last entry for a path is the
	// pack whose bytes are on disk.
	lastWriter := map[string]string{}
	if m, err := loadManifest(); err == nil {
		for _, e := range m.Entries {
			lastWriter[e.Path] = e.Pack
		}
	}

	sort.Strings(order)
	for _, path := range order {
		ps := writers[path]
		switch {
		case len(ps) > 1:
			fmt.Fprintf(os.Stderr, "warning: %s is written by %s in this sync; the later copy (%s) wins\n",
				path, strings.Join(ps, " and "), ps[len(ps)-1])
		case lastWriter[path] != "" && lastWriter[path] != ps[0]:
			fmt.Fprintf(os.Stderr, "warning: %s was last written by %s; %s overwrites it\n",
				path, lastWriter[path], ps[0])
		}
	}
}

// CountForPack returns the total discoverable artifacts across all bindings
// a pack ships (commands + skills + future types).
func CountForPack(_, packPath string) (int, error) {
	resolved, err := filepath.EvalSymlinks(packPath)
	if err != nil {
		return 0, err
	}

	total := 0
	if hasMarkdownCommandContent(resolved) {
		total += countMarkdownCommands(resolved)
	}
	if hasSkillDirContent(resolved) {
		total += countSkillDirs(resolved)
	}
	return total, nil
}

// SyncedCount returns the total number of artifacts currently synced to
// tool-config directories for this pack across all binding types.
// An artifact counts only when the pack ships it at packPath, the sync
// manifest records packName as its writer, and the file is still at the
// target. A sync that fails part-way saves
// what its succeeding bindings wrote and marks the manifest incomplete, so
// the credit reflects those writes; status and doctor say so until a
// completed sync. Shipping alone is not enough: two packs that ship the same
// canonical id would otherwise each count the other's copy as their own
// (aae-orc-zwx4b). Ownership of what is shipped is by canonical id /
// basename, not a name prefix, so packs that ship multi-prefix bindings
// (bmad ships bmad-* and gds-*) are accounted fully. A pack synced
// before the manifest existed reads as not synced until the next sync
// records it.
func SyncedCount(packName, packPath string) (int, error) {
	resolved, err := filepath.EvalSymlinks(packPath)
	if err != nil {
		return 0, err
	}
	m, err := loadManifest()
	if err != nil {
		return 0, err
	}
	// Manifest order is sync order, so when two packs recorded the same
	// path the later entry wrote the bytes now on disk. Only that last
	// writer counts the path as synced.
	lastWriter := make(map[string]string, len(m.Entries))
	for _, e := range m.Entries {
		lastWriter[e.Path] = e.Pack
	}
	skills := map[string]struct{}{}
	commands := map[string]struct{}{}
	for _, e := range m.Entries {
		if e.Pack != packName || lastWriter[e.Path] != packName {
			continue
		}
		switch e.Kind {
		case "skill-dir":
			skills[filepath.Base(e.Path)] = struct{}{}
		case "markdown-command":
			commands[filepath.Base(e.Path)] = struct{}{}
		}
	}

	total := 0

	c, err := countSyncedCommandsRecorded(resolved, commands)
	if err != nil {
		return 0, err
	}
	total += c

	s, err := countSyncedSkillsRecorded(resolved, skills)
	if err != nil {
		return 0, err
	}
	total += s

	return total, nil
}

// intersectNames returns the names present in both sets.
func intersectNames(a, b map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(a))
	for n := range a {
		if _, ok := b[n]; ok {
			out[n] = struct{}{}
		}
	}
	return out
}

// claudeCommandsDir returns the Claude Code commands directory under
// the harness config dir (CLAUDE_CONFIG_DIR when set, else ~/.claude),
// the same dir the read path resolves through foreign.ConfigDir.
func claudeCommandsDir() string {
	return filepath.Join(foreign.ConfigDir(), "commands")
}

// claudeSkillsDir returns the Claude Code skills directory under the
// harness config dir, as claudeCommandsDir does.
func claudeSkillsDir() string {
	return filepath.Join(foreign.ConfigDir(), "skills")
}
