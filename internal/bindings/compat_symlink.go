package bindings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The compat symlink is decision D2 (finding-094, aae-orc-d3nq.58):
// <repo>/plugins/<pack> points at the pinned store version root, so
// the 451 repo-relative plugins/<pack>/... references in the pack —
// including three live dispatcher capability grants that only ever
// resolved when the engine was vendored — resolve with zero content
// rewriting and zero store mutation.
//
// Ratification riders: the symlink is recorded in the per-repo
// ownership records (removal is exact) and belongs in the repo
// gitignore (.53) so it never enters history. Grant-resolution is a
// behavior change: those grants have never executed in any consumer
// repo, so their first observed outcome is a trial before it is a
// claim (recorded in the divergence register, .55).

// ArtifactCompatSymlink is the compat symlink's artifact kind.
// Removal only ever removes a SYMLINK at the recorded path — a real
// directory there (the pack developing itself vendors the engine at
// the same path) fails closed.
const ArtifactCompatSymlink = "compat-symlink"

// compatDir is the repo-relative directory compat symlinks live in.
const compatDir = "plugins"

// MaterializeCompatSymlink writes <repo>/plugins/<pack> ->
// storeRoot. A pre-existing entry at the destination refuses with
// ErrWouldClobber before anything is written — in particular the
// pack's own development repo, where plugins/<pack> is the real
// vendored engine. Returned artifacts follow the MaterializeRepo
// contract (creation order; parent dir recorded only if created).
func MaterializeCompatSymlink(storeRoot string, t RepoTarget, packName string) ([]RepoArtifact, error) {
	repoDir, err := filepath.Abs(t.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repo dir: %w", err)
	}
	if _, err := os.Stat(repoDir); err != nil {
		return nil, fmt.Errorf("repo dir %s: %w", repoDir, err)
	}
	if packName == "" || strings.ContainsAny(packName, "/\\") {
		return nil, fmt.Errorf("invalid pack name %q for compat symlink", packName)
	}

	dst := filepath.Join(repoDir, compatDir, packName)
	if _, err := os.Lstat(dst); err == nil {
		return nil, fmt.Errorf("%w:\n  %s", ErrWouldClobber, filepath.ToSlash(filepath.Join(compatDir, packName)))
	}

	var created []RepoArtifact
	parent := filepath.Join(repoDir, compatDir)
	if _, err := os.Stat(parent); err != nil {
		if err := os.Mkdir(parent, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", compatDir, err)
		}
		created = append(created, RepoArtifact{Kind: ArtifactParentDir, Path: compatDir})
	}
	if err := os.Symlink(storeRoot, dst); err != nil {
		if _, rmErr := RemoveRepoArtifacts(t, created); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: rollback after failed compat symlink: %v\n", rmErr)
		}
		return nil, fmt.Errorf("create compat symlink: %w", err)
	}
	created = append(created, RepoArtifact{
		Kind: ArtifactCompatSymlink,
		Path: filepath.ToSlash(filepath.Join(compatDir, packName)),
	})
	return created, nil
}

// compatRemovalAllowed reports whether a recorded compat artifact
// path is one this package could have written: the symlink is exactly
// plugins/<name>, and the only parent dir is plugins itself.
func compatRemovalAllowed(a RepoArtifact) bool {
	parts := strings.Split(filepath.ToSlash(a.Path), "/")
	switch a.Kind {
	case ArtifactCompatSymlink:
		return len(parts) == 2 && parts[0] == compatDir && parts[1] != "" && parts[1] != ".." && parts[1] != "."
	case ArtifactParentDir:
		return a.Path == compatDir
	}
	return false
}
