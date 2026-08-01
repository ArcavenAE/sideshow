// Package preserve is the compiled-in conservative floor under every
// sideshow removal path (aae-orc-d3nq.42).
//
// The register (sideshow-packs registry/<pack>-pack-support.yaml) is the
// authoritative never-remove declaration: it names, per pack, the durable
// user state sideshow must never write, delete, or clean. This package is
// the floor beneath that declaration — the subset that holds when no
// register is on hand, which is every removal running against a machine
// that only has the binary and a ledger.
//
// Three classes, per the taxonomy the register encodes:
//
//   - Class 1, durable user state. A running factory's worktree and
//     branches, the pack's customization and output trees, in-repo run
//     state. Never touched by any verb, through install, version flips,
//     and conversion in either direction. This package enforces class 1.
//   - Class 2, regenerable machine state. Rendered hooks, bound-variant
//     caches. Droppable and re-derivable; removal is expected.
//   - Class 3, harness config. Per-repo enable entries, the default
//     agent. Touched only per an explicit allowlist, with consent.
//
// The floor is a denylist by path shape, deliberately independent of the
// containment allowlists elsewhere (RemoveRepoArtifacts confines removal
// to the harness root; this refuses protected paths wherever they turn
// up). Two independent checks that must both pass is the point: a
// containment predicate answers "is this inside the area I manage", and
// this answers "is this something nobody may delete". A bug in the first
// is survivable while the second holds.
package preserve

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ErrProtected marks a refused removal. Callers wrap it; tests match it
// with errors.Is.
type ErrProtected struct {
	Path      string
	Component string
	Reason    string
}

func (e *ErrProtected) Error() string {
	return fmt.Sprintf("refusing removal: %s is protected user state (%s: %s); sideshow never removes it, through install, uninstall, version flips, or conversion in either direction",
		e.Path, e.Component, e.Reason)
}

// protectedComponents are exact path components that are class 1
// wherever they appear. Matching on the component rather than a prefix
// means a nested checkout or a worktree at any depth is covered.
var protectedComponents = map[string]string{
	".git":             "git metadata; every ref, including the factory-artifacts branch, lives here",
	".factory":         "the running factory's worktree and its state",
	".factory-project": "factory project state",
	".worktrees":       "linked worktrees, including STORY-* wave checkouts",
}

// IsProtected reports whether path is class 1 durable user state, and
// why. Paths are matched by component, so both a path TO protected
// state and a path INSIDE it refuse.
//
// The underscore conventions (_<pack>-custom, _<pack>-output) are
// matched by shape rather than by pack name, so a pack whose register
// sideshow has never seen still gets the floor.
func IsProtected(path string) (bool, string, string) {
	cleaned := filepath.ToSlash(filepath.Clean(path))
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			continue
		}
		if reason, ok := protectedComponents[part]; ok {
			return true, part, reason
		}
		if isCustomOrOutput(part) {
			return true, part, "pack customization or output tree; checked-in territory that survives version switches"
		}
	}
	return false, "", ""
}

// isCustomOrOutput matches the _<pack>-custom / _<pack>-output
// convention. The pack name is not checked: the shape is the contract,
// and a floor that only protected packs it recognized would fail
// exactly where it matters most.
func isCustomOrOutput(part string) bool {
	if !strings.HasPrefix(part, "_") || len(part) < 2 {
		return false
	}
	return strings.HasSuffix(part, "-custom") || strings.HasSuffix(part, "-output")
}

// Check refuses removal of protected state. Every removal path calls it
// immediately before the syscall, not at plan time: a plan can be stale,
// and the floor's whole value is that it holds when everything upstream
// of it was wrong.
func Check(path string) error {
	if protected, component, reason := IsProtected(path); protected {
		return &ErrProtected{Path: path, Component: component, Reason: reason}
	}
	return nil
}

// CheckAll refuses if any path in the set is protected, naming the first
// offender. Use it as a preflight over a whole removal set so a partial
// removal cannot begin.
func CheckAll(paths []string) error {
	for _, p := range paths {
		if err := Check(p); err != nil {
			return err
		}
	}
	return nil
}

// DestructiveGitSubcommands is the set sideshow must never invoke.
// Sideshow's git usage is read-only by construction (rev-parse, status);
// this list exists so the guard test in this package keeps it that way
// rather than relying on nobody adding one. Git-side protection is verb
// behavior, not a path list: a branch or a ref is not something a
// containment predicate can see.
var DestructiveGitSubcommands = []string{
	"branch -D",
	"branch -d",
	"push --delete",
	"worktree remove",
	"worktree prune",
	"reset --hard",
	"clean -fd",
	"checkout --",
	"update-ref -d",
}
