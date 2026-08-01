package weave

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveWriteTarget joins rel onto repoRoot and resolves symlinks before
// returning the absolute path a weave operation may write to. A target whose
// resolved location falls outside the resolved repo root is refused:
// pack.yaml runtime_links symlink store content into the repo (for example
// _bmad/_config pointing at the frozen store version), so a prefix check on
// the unresolved path would let a declaration write through the link and
// corrupt producer-validated store content (aae-orc-a3v6). The check is by
// resolution, never by path spelling: legacy repos carrying a real _bmad/
// tree resolve inside the root and pass unchanged.
func resolveWriteTarget(repoRoot, rel string) (string, error) {
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}
	root, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}

	resolved, err := resolveExisting(filepath.Join(absRoot, rel))
	if err != nil {
		return "", err
	}

	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"target %s resolves to %s, outside the repo; refusing to write through a runtime link",
			rel, resolved)
	}
	return resolved, nil
}

// resolveExisting resolves symlinks in abs. The final path elements may not
// exist yet (shim files and their directories are created on apply), so the
// deepest existing ancestor is resolved and the missing remainder re-joined
// onto the resolution.
func resolveExisting(abs string) (string, error) {
	var tail []string
	p := abs
	for {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			return filepath.Join(append([]string{resolved}, tail...)...), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve %s: %w", abs, err)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", fmt.Errorf("resolve %s: no existing ancestor", abs)
		}
		tail = append([]string{filepath.Base(p)}, tail...)
		p = parent
	}
}
