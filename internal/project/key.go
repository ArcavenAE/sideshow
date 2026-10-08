package project

// SingleRepoManifest is the fixed manifest name under which
// `sideshow project init <pack>` records its receipts. The orchestrator path
// keys its receipts by the repos.yaml it read; the single-repo path has no
// such file, so it uses this constant instead (sideshow#91).
const SingleRepoManifest = "project-init"

// SingleRepoKey returns the registry key for a repo reached by cwd alone:
// the project id and the root.
func SingleRepoKey(dir string) (id, root string, err error) {
	return "", "", nil
}
