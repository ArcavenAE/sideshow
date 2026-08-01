package bindings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// execManifestName is the pack-root executable census emitted by the
// sideshow-packs build pipeline (same contract as the install-time
// check in internal/pack).
const execManifestName = "exec-manifest.txt"

// RepoScope selects the D1 materialization strategy (finding-094
// ratification addendum, hybrid by scope).
type RepoScope string

const (
	// ScopeLocal symlinks bindings into the repo. The store copy is the
	// only copy: exec bits live there, drift is structurally impossible,
	// and nothing binding-sized enters the repo's history. Trial-backed
	// on macOS (finding-091 addendum 2, T17: the harness follows
	// symlinked skill dirs AND symlinked agent files); the Linux half is
	// untested, so no cross-platform claim yet.
	ScopeLocal RepoScope = "local"

	// ScopeProject materializes full copies, because absolute symlinks
	// do not cross machines and committed bindings must be
	// self-contained bytes. Copies carry the source's permission bits
	// and are verified against the pack's exec-manifest.txt census.
	ScopeProject RepoScope = "project"
)

// RepoTarget names where bindings materialize inside one repo.
//
// HarnessDir is a parameter rather than a constant as the recorded
// cross-harness seam (aae-orc-d3nq.57): the same bindings later project
// into .agents/skills/, .opencode/, .crush/ by supplying a different
// value. Nothing beyond the parameter is built today.
type RepoTarget struct {
	RepoDir    string
	HarnessDir string // "" means ".claude"
	Scope      RepoScope
}

func (t RepoTarget) harness() string {
	if t.HarnessDir == "" {
		return ".claude"
	}
	return t.HarnessDir
}

// harnessRoot returns the absolute directory all materialization is
// contained to.
func (t RepoTarget) harnessRoot() string {
	return filepath.Join(t.RepoDir, t.harness())
}

// ErrWouldClobber marks a refused materialization: a destination path
// already exists and is not sideshow's to overwrite. Enable never
// clobbers pre-existing repo content; the operator resolves the
// collision first (coexist-check reads this signal).
var ErrWouldClobber = errors.New("destination already exists; sideshow never overwrites pre-existing repo content")

// Artifact kinds recorded per materialized path. The ledger row
// (docs/repo-bindings-ledger.md) carries these so disable replays the
// exact set in reverse.
const (
	// ArtifactSkillDir is a materialized skill directory unit — a
	// symlink at local scope, a copied tree at project scope. Removed
	// whole on disable.
	ArtifactSkillDir = "skill-dir"
	// ArtifactAgentFile is a materialized agent file — a symlink at
	// local scope, a mode-preserving copy at project scope.
	ArtifactAgentFile = "agent-file"
	// ArtifactParentDir is a directory created on the way to a unit
	// (including the harness dir itself when absent). Removed on
	// disable only if empty; user content that arrived later survives.
	ArtifactParentDir = "parent-dir"
)

// RepoArtifact is one materialized path: repo-relative, slash-separated,
// with the kind that decides its removal semantics.
type RepoArtifact struct {
	Kind string `yaml:"kind"`
	Path string `yaml:"path"`
}

// unit is one (source, destination) materialization pair.
type unit struct {
	srcRel string // pack-root-relative, e.g. skills/pr-manager
	dstRel string // repo-relative, e.g. .claude/skills/pr-manager
	kind   string
}

// MaterializeRepo writes a plugin-shaped pack's discovery surface
// (skills, agents — the materialize dispositions of the unshaping
// spec) into one repo. storeRoot must be the resolved absolute
// packs/<pack>/<version>/ path, never the `current` symlink, so a
// store-level flip can never retarget a bound repo.
//
// The write is all-or-nothing: a clobber preflight refuses before any
// write if any destination exists, and a mid-write failure rolls back
// everything already created. The returned artifact list is the exact
// removal set for RemoveRepoArtifacts (ledger `artifacts:` rows).
//
// Content transforms (binding-prefix, namespace-rewrite) layer on in
// aae-orc-d3nq.51; the env shim, settings hooks, and compat symlink are
// .50/.52/.58. This function materializes source bytes as-is.
func MaterializeRepo(storeRoot string, inv *PluginInventory, t RepoTarget) ([]RepoArtifact, error) {
	if t.Scope != ScopeLocal && t.Scope != ScopeProject {
		return nil, fmt.Errorf("unknown repo scope %q (want %q or %q)", t.Scope, ScopeLocal, ScopeProject)
	}
	repoDir, err := filepath.Abs(t.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repo dir: %w", err)
	}
	t.RepoDir = repoDir
	if _, err := os.Stat(repoDir); err != nil {
		return nil, fmt.Errorf("repo dir %s: %w", repoDir, err)
	}

	units := planUnits(inv, t)

	// Clobber preflight: check every destination before writing any.
	var clobbers []string
	for _, u := range units {
		if _, err := os.Lstat(filepath.Join(repoDir, filepath.FromSlash(u.dstRel))); err == nil {
			clobbers = append(clobbers, u.dstRel)
		}
	}
	if len(clobbers) > 0 {
		return nil, fmt.Errorf("%w:\n  %s", ErrWouldClobber, strings.Join(clobbers, "\n  "))
	}

	var created []RepoArtifact
	rollback := func() {
		if _, rmErr := RemoveRepoArtifacts(t, created); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: rollback after failed materialization: %v\n", rmErr)
		}
	}

	for _, u := range units {
		dst := filepath.Join(repoDir, filepath.FromSlash(u.dstRel))
		parents, mkErr := mkdirsRecording(t, filepath.Dir(dst))
		created = append(created, parents...)
		if mkErr != nil {
			rollback()
			return nil, mkErr
		}

		src := filepath.Join(storeRoot, filepath.FromSlash(u.srcRel))
		var writeErr error
		if t.Scope == ScopeLocal {
			writeErr = os.Symlink(src, dst)
		} else {
			writeErr = copyPreservingMode(src, dst)
		}
		if writeErr != nil {
			rollback()
			return nil, fmt.Errorf("materialize %s: %w", u.dstRel, writeErr)
		}
		created = append(created, RepoArtifact{Kind: u.kind, Path: u.dstRel})
	}

	if t.Scope == ScopeProject {
		if err := verifyRepoExec(storeRoot, t, units); err != nil {
			rollback()
			return nil, err
		}
	}

	return created, nil
}

// planUnits maps the plugin inventory onto repo destinations. Skills
// keep their unit-directory shape; agents keep their nested layout
// (bare-name addressing per trial T20).
func planUnits(inv *PluginInventory, t RepoTarget) []unit {
	harness := t.harness()
	units := make([]unit, 0, len(inv.SkillDirs)+len(inv.AgentFiles))
	for _, s := range inv.SkillDirs {
		units = append(units, unit{
			srcRel: filepath.ToSlash(s),
			dstRel: filepath.ToSlash(filepath.Join(harness, s)),
			kind:   ArtifactSkillDir,
		})
	}
	for _, a := range inv.AgentFiles {
		units = append(units, unit{
			srcRel: filepath.ToSlash(a),
			dstRel: filepath.ToSlash(filepath.Join(harness, a)),
			kind:   ArtifactAgentFile,
		})
	}
	return units
}

// mkdirsRecording creates dir (and missing parents) inside the repo,
// recording every directory it actually creates so disable can prune
// them. Creation outside the harness root is refused.
func mkdirsRecording(t RepoTarget, dir string) ([]RepoArtifact, error) {
	root := t.harnessRoot()
	relFromRepo, err := filepath.Rel(t.RepoDir, dir)
	if err != nil || strings.HasPrefix(relFromRepo, "..") {
		return nil, fmt.Errorf("refusing to create %s: outside repo %s", dir, t.RepoDir)
	}
	relFromRoot, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(relFromRoot, "..") {
		return nil, fmt.Errorf("refusing to create %s: outside binding root %s", dir, root)
	}

	// Walk from the harness root down, creating what's missing.
	var toCreate []string
	cur := dir
	for {
		if _, statErr := os.Stat(cur); statErr == nil {
			break
		}
		toCreate = append([]string{cur}, toCreate...)
		if cur == root {
			break
		}
		cur = filepath.Dir(cur)
	}

	var created []RepoArtifact
	for _, d := range toCreate {
		if err := os.Mkdir(d, 0o755); err != nil {
			return created, fmt.Errorf("create %s: %w", d, err)
		}
		rel, relErr := filepath.Rel(t.RepoDir, d)
		if relErr != nil {
			return created, relErr
		}
		created = append(created, RepoArtifact{Kind: ArtifactParentDir, Path: filepath.ToSlash(rel)})
	}
	return created, nil
}

// copyPreservingMode copies a file or a directory tree, carrying each
// source file's permission bits (project-scope copies must keep the
// pack's executables executable; the exec-manifest census verifies).
func copyPreservingMode(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return writeWithSourceMode(dst, data, src)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeWithSourceMode(target, data, path)
	})
}

// verifyRepoExec checks project-scope copies against the pack's
// exec-manifest.txt census (validation rule 3 of the unshaping spec):
// every census entry falling under a materialized unit must carry an
// exec bit at its repo destination. A pack without the census file
// passes vacuously — mode-preserving copy is still in effect.
func verifyRepoExec(storeRoot string, t RepoTarget, units []unit) error {
	data, err := os.ReadFile(filepath.Join(storeRoot, execManifestName))
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
		for _, u := range units {
			if rel != u.srcRel && !strings.HasPrefix(rel, u.srcRel+"/") {
				continue
			}
			dst := filepath.Join(t.RepoDir, t.harness(), filepath.FromSlash(rel))
			info, statErr := os.Stat(dst)
			switch {
			case statErr != nil:
				drift = append(drift, rel+" (missing)")
			case info.Mode().Perm()&0o100 == 0:
				drift = append(drift, rel+" (not executable)")
			}
			break
		}
	}
	if len(drift) > 0 {
		return fmt.Errorf("%s verification failed for repo copies under %s (%d entries drifted):\n  %s",
			execManifestName, t.harnessRoot(), len(drift), strings.Join(drift, "\n  "))
	}
	return nil
}

// RemoveRepoArtifacts replays a recorded artifact list in reverse — the
// exact inverse of MaterializeRepo. Removal never guesses: only
// recorded paths go, every path must sit inside the repo's binding
// root (the repo-rooted containment predicate; a corrupt ledger row
// fails closed rather than deleting outside), and parent dirs are
// pruned only when empty so user content that arrived after enable
// survives. Returns the number of paths removed.
func RemoveRepoArtifacts(t RepoTarget, artifacts []RepoArtifact) (int, error) {
	repoDir, err := filepath.Abs(t.RepoDir)
	if err != nil {
		return 0, fmt.Errorf("resolve repo dir: %w", err)
	}
	t.RepoDir = repoDir
	root := t.harnessRoot()

	// Containment preflight over the whole set before touching anything.
	// Two allowed roots: the harness dir (the binding root itself is a
	// legal artifact only as a parent-dir, where Remove-if-empty is
	// safe) and the compat-symlink shapes under plugins/ (D2). Anything
	// else fails closed.
	abs := make([]string, len(artifacts))
	for i, a := range artifacts {
		p := filepath.Join(repoDir, filepath.FromSlash(a.Path))
		rel, relErr := filepath.Rel(root, p)
		rootAsParentDir := rel == "." && a.Kind == ArtifactParentDir
		underHarness := relErr == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(a.Path) &&
			(rel != "." || rootAsParentDir)
		if !underHarness && !compatRemovalAllowed(a) {
			return 0, fmt.Errorf("refusing removal: artifact %q (kind %s) resolves outside the allowed binding roots of %s", a.Path, a.Kind, repoDir)
		}
		abs[i] = p
	}

	removed := 0
	for i := len(artifacts) - 1; i >= 0; i-- {
		a := artifacts[i]
		p := abs[i]
		if _, statErr := os.Lstat(p); statErr != nil {
			continue // already gone
		}
		switch a.Kind {
		case ArtifactParentDir:
			if rmErr := os.Remove(p); rmErr != nil {
				// Non-empty means user content arrived; leave it.
				continue
			}
		case ArtifactSkillDir:
			if rmErr := os.RemoveAll(p); rmErr != nil {
				return removed, fmt.Errorf("remove %s: %w", a.Path, rmErr)
			}
		case ArtifactCompatSymlink:
			// Only ever remove a symlink here: a real directory at the
			// recorded path is the vendored engine (the pack developing
			// itself), and a corrupt ledger row must not delete it.
			info, statErr := os.Lstat(p)
			if statErr != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 {
				return removed, fmt.Errorf("refusing removal: %s is recorded as a compat symlink but is not a symlink on disk", a.Path)
			}
			if rmErr := os.Remove(p); rmErr != nil {
				return removed, fmt.Errorf("remove %s: %w", a.Path, rmErr)
			}
		default: // ArtifactAgentFile and future file-shaped kinds
			if rmErr := os.Remove(p); rmErr != nil {
				return removed, fmt.Errorf("remove %s: %w", a.Path, rmErr)
			}
		}
		removed++
	}
	return removed, nil
}
