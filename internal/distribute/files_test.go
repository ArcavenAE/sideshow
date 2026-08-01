package distribute

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/pack"
	"github.com/ArcavenAE/sideshow/internal/project"
)

// editorconfig is deliberately a non-markdown format. An HTML comment marker
// would corrupt it, which is why file artifacts are written verbatim.
const editorconfig = "root = true\n\n[*]\nindent_style = tab\n"

func filesPackRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dist := filepath.Join(root, "distribute")
	if err := os.MkdirAll(filepath.Join(dist, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "editorconfig"), []byte(editorconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "workflows", "codeql.yml"),
		[]byte("name: codeql\non: push\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func filesRepo(t *testing.T) project.Subrepo {
	t.Helper()
	return project.Subrepo{Name: "testrepo", AbsPath: t.TempDir(), Present: true}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func onlyAction(t *testing.T, result Result) Action {
	t.Helper()
	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %+v", len(result.Actions), result.Actions)
	}
	return result.Actions[0]
}

func editorconfigManifest() *Manifest {
	return &Manifest{Files: []FileArtifact{{Source: "distribute/editorconfig", Target: ".editorconfig"}}}
}

// --- creation ---

// The headline requirement: the bytes on disk are the bytes the pack shipped.
// No ownership marker, because `#` is the comment character in this format and
// `<!-- -->` would break it.
func TestDistributeFile_WritesVerbatimWithNoMarker(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)

	action := onlyAction(t, ToRepo(repo, editorconfigManifest(), defaultOpts(packRoot)))
	if action.Status != "wrote" {
		t.Fatalf("status = %q (%s)", action.Status, action.Detail)
	}
	if action.Type != "files" {
		t.Errorf("type = %q, want files", action.Type)
	}

	got := readFile(t, filepath.Join(repo.AbsPath, ".editorconfig"))
	if got != editorconfig {
		t.Errorf("content is not verbatim:\n got %q\nwant %q", got, editorconfig)
	}
	if strings.Contains(got, markerPrefix) || strings.Contains(got, "<!--") {
		t.Error("an ownership marker was injected into a non-markdown file")
	}
	if want := sum([]byte(editorconfig)); action.Artifact.Checksum != want {
		t.Errorf("receipt checksum = %q, want %q", action.Artifact.Checksum, want)
	}
}

func TestDistributeFile_CreatesNestedDirectories(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)
	m := &Manifest{Files: []FileArtifact{
		{Source: "distribute/workflows/codeql.yml", Target: ".github/workflows/codeql.yml"},
	}}

	if action := onlyAction(t, ToRepo(repo, m, defaultOpts(packRoot))); action.Status != "wrote" {
		t.Fatalf("status = %q (%s)", action.Status, action.Detail)
	}
	if got := readFile(t, filepath.Join(repo.AbsPath, ".github/workflows/codeql.yml")); got == "" {
		t.Error("nested target was not written")
	}
}

// --- ownership: the four cases ---

func TestDistributeFile_SkipsPreexistingWithNoReceipt(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)
	target := filepath.Join(repo.AbsPath, ".editorconfig")
	if err := os.WriteFile(target, []byte("root = true\n# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	action := onlyAction(t, ToRepo(repo, editorconfigManifest(), defaultOpts(packRoot)))
	if action.Status != "skipped" {
		t.Fatalf("status = %q, want skipped (%s)", action.Status, action.Detail)
	}
	if got := readFile(t, target); got != "root = true\n# mine\n" {
		t.Errorf("a user-authored file was overwritten: %q", got)
	}
}

// The case the marker model gets wrong. distributeRule overwrites whenever it
// sees its marker, so an edit to a managed rule file is lost. Here it survives.
func TestDistributeFile_PreservesUserEditAfterSideshowWroteIt(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)
	target := filepath.Join(repo.AbsPath, ".editorconfig")

	edited := editorconfig + "indent_size = 4\n"
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := defaultOpts(packRoot)
	// Receipt says we wrote the pristine version; disk says otherwise.
	opts.PriorChecksums = map[string]string{".editorconfig": sum([]byte(editorconfig))}

	action := onlyAction(t, ToRepo(repo, editorconfigManifest(), opts))
	if action.Status != "skipped" {
		t.Fatalf("status = %q, want skipped (%s)", action.Status, action.Detail)
	}
	if !strings.Contains(action.Detail, "modified") {
		t.Errorf("detail should report drift, got %q", action.Detail)
	}
	if got := readFile(t, target); got != edited {
		t.Errorf("user edit was clobbered:\n got %q\nwant %q", got, edited)
	}
}

func TestDistributeFile_UpdatesWhenUnmodifiedAndSourceChanged(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)
	target := filepath.Join(repo.AbsPath, ".editorconfig")

	stale := "root = true\n"
	if err := os.WriteFile(target, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := defaultOpts(packRoot)
	opts.PriorChecksums = map[string]string{".editorconfig": sum([]byte(stale))}

	action := onlyAction(t, ToRepo(repo, editorconfigManifest(), opts))
	if action.Status != "wrote" {
		t.Fatalf("status = %q, want wrote (%s)", action.Status, action.Detail)
	}
	if got := readFile(t, target); got != editorconfig {
		t.Errorf("target was not refreshed: %q", got)
	}
}

func TestDistributeFile_SkipsWhenAlreadyCurrent(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)
	if err := os.WriteFile(filepath.Join(repo.AbsPath, ".editorconfig"), []byte(editorconfig), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := defaultOpts(packRoot)
	opts.PriorChecksums = map[string]string{".editorconfig": sum([]byte(editorconfig))}

	action := onlyAction(t, ToRepo(repo, editorconfigManifest(), opts))
	if action.Status != "skipped" || !strings.Contains(action.Detail, "current") {
		t.Fatalf("status = %q, detail = %q; want skipped/already current", action.Status, action.Detail)
	}
}

// Applying twice must converge. Second run feeds back the first run's receipt.
func TestDistributeFile_IsIdempotent(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)

	first := onlyAction(t, ToRepo(repo, editorconfigManifest(), defaultOpts(packRoot)))
	opts := defaultOpts(packRoot)
	opts.PriorChecksums = map[string]string{".editorconfig": first.Artifact.Checksum}

	second := onlyAction(t, ToRepo(repo, editorconfigManifest(), opts))
	if second.Status != "skipped" {
		t.Errorf("second run status = %q, want skipped (%s)", second.Status, second.Detail)
	}
	if got := readFile(t, filepath.Join(repo.AbsPath, ".editorconfig")); got != editorconfig {
		t.Errorf("content drifted across runs: %q", got)
	}
}

// --- guards ---

// Target is arbitrary, so unlike the typed artifacts it has to be constrained.
func TestDistributeFile_RejectsEscapingTarget(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)

	for _, target := range []string{"../escaped", "../../etc/hosts", "a/../../escaped"} {
		m := &Manifest{Files: []FileArtifact{{Source: "distribute/editorconfig", Target: target}}}
		action := onlyAction(t, ToRepo(repo, m, defaultOpts(packRoot)))
		if action.Status != "error" {
			t.Errorf("target %q: status = %q, want error", target, action.Status)
		}
	}
}

func TestDistributeFile_RequiresSourceAndTarget(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)

	for name, f := range map[string]FileArtifact{
		"no target": {Source: "distribute/editorconfig"},
		"no source": {Target: ".editorconfig"},
	} {
		action := onlyAction(t, ToRepo(repo, &Manifest{Files: []FileArtifact{f}}, defaultOpts(packRoot)))
		if action.Status != "error" {
			t.Errorf("%s: status = %q, want error", name, action.Status)
		}
	}
}

func TestDistributeFile_ErrorsOnMissingSource(t *testing.T) {
	t.Parallel()
	repo := filesRepo(t)
	m := &Manifest{Files: []FileArtifact{{Source: "distribute/absent", Target: ".editorconfig"}}}

	action := onlyAction(t, ToRepo(repo, m, defaultOpts(filesPackRoot(t))))
	if action.Status != "error" || !strings.Contains(action.Detail, "read source") {
		t.Errorf("status = %q, detail = %q", action.Status, action.Detail)
	}
}

func TestDistributeFile_DryRunWritesNothing(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)
	opts := defaultOpts(packRoot)
	opts.DryRun = true

	action := onlyAction(t, ToRepo(repo, editorconfigManifest(), opts))
	if action.Status != "wrote" || !strings.Contains(action.Detail, "would") {
		t.Errorf("status = %q, detail = %q", action.Status, action.Detail)
	}
	if _, err := os.Stat(filepath.Join(repo.AbsPath, ".editorconfig")); !os.IsNotExist(err) {
		t.Error("dry run wrote to disk")
	}
}

// --- manifest plumbing ---

func TestManifest_FilesCountsTowardIsEmpty(t *testing.T) {
	t.Parallel()
	m := Manifest{Files: []FileArtifact{{Source: "a", Target: "b"}}}
	if m.IsEmpty() {
		t.Error("a manifest with only files should not be empty")
	}
}

func TestLoadPackYAML_ParsesFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	body := `name: fleet-bootstrap
version: 0.1.0
distribute:
  files:
    - source: distribute/editorconfig
      target: .editorconfig
    - source: distribute/workflows/codeql.yml
      target: .github/workflows/codeql.yml
`
	if err := os.WriteFile(filepath.Join(root, "pack.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPackYAML(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Distribute.Files) != 2 {
		t.Fatalf("parsed %d files, want 2", len(p.Distribute.Files))
	}
	if got := p.Distribute.Files[1].Target; got != ".github/workflows/codeql.yml" {
		t.Errorf("target = %q", got)
	}
}

// --- receipt round trip ---

// PriorChecksums is the read side of RecordResults. If they disagree on shape,
// every second run reports drift on files it wrote itself.
func TestPriorChecksums_RoundTripsWithRecordResults(t *testing.T) {
	t.Parallel()
	packRoot := filesPackRoot(t)
	repo := filesRepo(t)
	opts := defaultOpts(packRoot)

	result := ToRepo(repo, editorconfigManifest(), opts)
	reg := &pack.Registry{}
	RecordResults(reg, "proj-uuid", "/root", "repos.yaml", []Result{result}, opts)

	got := PriorChecksums(reg, "proj-uuid", "/root", "repos.yaml", repo.Name, opts.PackName)
	want := sum([]byte(editorconfig))
	if got[".editorconfig"] != want {
		t.Fatalf("round trip lost the checksum: got %q, want %q\nfull map: %+v",
			got[".editorconfig"], want, got)
	}

	// And feeding it back must produce a skip, not a rewrite.
	opts.PriorChecksums = got
	if action := onlyAction(t, ToRepo(repo, editorconfigManifest(), opts)); action.Status != "skipped" {
		t.Errorf("second run after a real receipt: status = %q (%s)", action.Status, action.Detail)
	}
}

func TestPriorChecksums_EmptyForUnknownProject(t *testing.T) {
	t.Parallel()
	if got := PriorChecksums(&pack.Registry{}, "nope", "/root", "repos.yaml", "repo", "pack"); len(got) != 0 {
		t.Errorf("expected empty map, got %+v", got)
	}
	if got := PriorChecksums(nil, "nope", "/root", "repos.yaml", "repo", "pack"); len(got) != 0 {
		t.Errorf("nil registry should yield empty map, got %+v", got)
	}
}
