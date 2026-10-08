package project

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
)

// SingleRepoManifest is the fixed manifest name under which
// `sideshow project init <pack>` records its receipts. The orchestrator path
// keys its receipts by the repos.yaml it read; the single-repo path has no
// such file, so it uses this constant instead (sideshow#91).
const SingleRepoManifest = "project-init"

// SingleRepoKey returns the registry key for a repo reached by cwd alone:
// the project id and the root. The root is the symlink-resolved absolute
// path. The id is the repo's own identity (.sideshow/project.yaml) when it
// has one, else "path:" plus the sha256 of the root, which is stable across
// runs and across a symlinked cwd. It never writes an identity file into the
// user's repo.
func SingleRepoKey(dir string) (id, root string, err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	root, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", fmt.Errorf("resolve %s: %w", abs, err)
	}
	ident, err := LoadIdentity(root)
	if err != nil {
		return "", "", err
	}
	if ident != nil && ident.ID != "" {
		return ident.ID, root, nil
	}
	sum := sha256.Sum256([]byte(root))
	return "path:" + hex.EncodeToString(sum[:]), root, nil
}
