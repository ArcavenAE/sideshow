package treehash

import (
	"os"
	"path/filepath"
	"testing"
)

// The worked vector of marvel's docs/design/companies.md, "The tree hash
// (v1)": a file, a directory, an empty file whose name holds a newline, a
// file, and a link. Its records are 264 bytes.
const workedVectorHash = "d44bd0c103e09a4e353971211d892ffcd9c5aad767eeff24a258211f7e0f5dcf"

func buildWorkedVector(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "company.toml"), []byte("name = \"x\"\n"), 0o444))
	must(os.Mkdir(filepath.Join(dir, "roles"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "roles", "a\nb.md"), nil, 0o444))
	must(os.WriteFile(filepath.Join(dir, "roles", "x.md"), []byte("hi\n"), 0o444))
	must(os.Symlink("roles/x.md", filepath.Join(dir, "top")))
	return dir
}

func TestHash_MatchesTheWorkedVector(t *testing.T) {
	dir := buildWorkedVector(t)
	got, err := Hash(dir)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if got != workedVectorHash {
		t.Fatalf("tree hash = %q, want %q", got, workedVectorHash)
	}
}

// A link is recorded as a link and never followed, so a dangling link
// hashes and retargeting it changes the hash.
func TestHash_RecordsALinkAndDoesNotFollowIt(t *testing.T) {
	dir := buildWorkedVector(t)
	if err := os.Remove(filepath.Join(dir, "top")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("roles/nowhere", filepath.Join(dir, "top")); err != nil {
		t.Fatal(err)
	}
	got, err := Hash(dir)
	if err != nil {
		t.Fatalf("a dangling link must hash, not fail: %v", err)
	}
	if got == workedVectorHash || got == "" {
		t.Fatalf("hash %q after retargeting the link, want a different non-empty hash", got)
	}
}

// Mode bits, owners and times are not hashed.
func TestHash_IgnoresModeBits(t *testing.T) {
	dir := buildWorkedVector(t)
	if err := os.Chmod(filepath.Join(dir, "roles", "x.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Hash(dir)
	if err != nil || got != workedVectorHash {
		t.Fatalf("hash %q err %v after a chmod, want the worked vector", got, err)
	}
}

func TestHash_MissingDirectoryIsAnError(t *testing.T) {
	if got, err := Hash(filepath.Join(t.TempDir(), "absent")); err == nil || got != "" {
		t.Fatalf("hash %q err %v, want an error and no hash", got, err)
	}
}

// A file that is neither a file, a directory nor a link has no record, so
// the hash refuses it.
func TestHash_RefusesAnEntryWithNoRecord(t *testing.T) {
	dir := buildWorkedVector(t)
	fifo := filepath.Join(dir, "pipe")
	if err := mkfifo(fifo); err != nil {
		t.Skipf("no fifo here: %v", err)
	}
	if got, err := Hash(dir); err == nil || got != "" {
		t.Fatalf("hash %q err %v, want an error and no hash for a fifo", got, err)
	}
}

// The root must be a real directory. A link to a directory, or a regular
// file, is refused instead of hashing as an empty tree.
func TestHash_RefusesARootThatIsNotARealDirectory(t *testing.T) {
	real := buildWorkedVector(t)
	link := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, root := range map[string]string{"symlink to a directory": link, "regular file": file} {
		if got, err := Hash(root); err == nil || got != "" {
			t.Errorf("%s: hash %q err %v, want an error and no hash", name, got, err)
		}
	}
}
