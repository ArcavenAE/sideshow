package enable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// enabledWithUnknown enables the pack, then lands extra user content and
// plants one ledger row of a kind this build does not know. It returns the
// repo snapshot from before enable, so a test can assert that everything
// enable made is gone and only the named extras remain.
func enabledWithUnknown(t *testing.T, planted string, userFiles map[string]string) (opts Options, repo string, before map[string]string) {
	t.Helper()
	store := writeStore(t)
	repo = writeRepo(t)
	opts = baseOpts(t, repo, store)
	before = snapshot(t, repo)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	for rel, content := range userFiles {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plantRow(t, opts, repo, planted)
	return opts, repo, before
}

func rowArtifacts(t *testing.T, opts Options, repo string) []string {
	t.Helper()
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	row := led.RepoRow(repo, "vsdd-factory")
	if row == nil {
		return nil
	}
	return row.Artifacts
}

const incompleteWant = "disable incomplete: 1 artifacts of kinds this build does not know (future-kind .claude/notes.txt); upgrade sideshow and run disable again"

// A row of a kind this build does not know is skipped and kept, not
// deleted as a file: known rows still go, the user's bytes stay, the
// ledger keeps only the unknown row, and the exit is an error naming it
// (sideshow#171).
func TestDisable_UnknownKindIsSkippedAndKept(t *testing.T) {
	opts, repo, before := enabledWithUnknown(t, "future-kind:.claude/notes.txt", map[string]string{".claude/notes.txt": "my notes\n"})

	err := Disable(opts)
	if err == nil || !strings.Contains(err.Error(), incompleteWant) {
		t.Fatalf("Disable error = %v, want it to contain %q", err, incompleteWant)
	}

	got, rerr := os.ReadFile(filepath.Join(repo, ".claude", "notes.txt"))
	if rerr != nil || string(got) != "my notes\n" {
		t.Errorf("the unknown-kind path was touched: %q, %v", got, rerr)
	}
	after := snapshot(t, repo)
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s = %q, want the pre-enable %q (known rows are removed)", k, after[k], v)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok && k != filepath.Join(".claude", "notes.txt") {
			t.Errorf("enable left %s behind", k)
		}
	}
	rows := rowArtifacts(t, opts, repo)
	if len(rows) != 1 || rows[0] != "future-kind:.claude/notes.txt" {
		t.Errorf("ledger row = %v, want only the future-kind row", rows)
	}
}

// An unknown kind at a non-empty directory: nothing under it is touched
// and disable does not fail partway through a removal.
func TestDisable_UnknownKindAtNonEmptyDirTouchesNothing(t *testing.T) {
	opts, repo, _ := enabledWithUnknown(t, "future-kind:.claude/notes.txt", map[string]string{".claude/notes.txt/inner.txt": "inner\n"})

	err := Disable(opts)
	if err == nil || !strings.Contains(err.Error(), incompleteWant) {
		t.Fatalf("Disable error = %v, want the incomplete error and no other", err)
	}
	got, rerr := os.ReadFile(filepath.Join(repo, ".claude", "notes.txt", "inner.txt"))
	if rerr != nil || string(got) != "inner\n" {
		t.Errorf("content under the unknown-kind dir changed: %q, %v", got, rerr)
	}
}

// A second disable changes nothing further and reports the same thing.
func TestDisable_UnknownKindRerunIsIdempotent(t *testing.T) {
	opts, repo, _ := enabledWithUnknown(t, "future-kind:.claude/notes.txt", map[string]string{".claude/notes.txt": "my notes\n"})

	first := Disable(opts)
	snapAfterFirst := snapshot(t, repo)
	rowsAfterFirst := rowArtifacts(t, opts, repo)
	second := Disable(opts)

	if first == nil || second == nil || first.Error() != second.Error() {
		t.Fatalf("errors differ across runs: %v / %v", first, second)
	}
	snapAfterSecond := snapshot(t, repo)
	if len(snapAfterFirst) != len(snapAfterSecond) {
		t.Errorf("second run changed the tree: %v -> %v", snapAfterFirst, snapAfterSecond)
	}
	for k, v := range snapAfterFirst {
		if snapAfterSecond[k] != v {
			t.Errorf("second run changed %s", k)
		}
	}
	rows := rowArtifacts(t, opts, repo)
	if strings.Join(rows, ",") != strings.Join(rowsAfterFirst, ",") || len(rows) != 1 {
		t.Errorf("ledger rows %v -> %v", rowsAfterFirst, rows)
	}
}

// An unknown kind outside the binding roots still fails the whole set
// closed before any removal.
func TestDisable_UnknownKindOutsideRootsFailsTheSetClosed(t *testing.T) {
	opts, repo, _ := enabledWithUnknown(t, "future-kind:../outside.txt", nil)
	before := snapshot(t, repo)

	if err := Disable(opts); err == nil || strings.Contains(err.Error(), "disable incomplete") {
		t.Fatalf("Disable error = %v, want a containment refusal", err)
	}
	after := snapshot(t, repo)
	if len(after) != len(before) {
		t.Errorf("a refused set removed something: %d -> %d entries", len(before), len(after))
	}
	if rows := rowArtifacts(t, opts, repo); len(rows) == 0 {
		t.Error("a refused set dropped the ledger row")
	}
}
