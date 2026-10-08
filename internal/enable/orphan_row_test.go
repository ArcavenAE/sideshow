package enable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// wipeRepo removes everything in the repo directory, as deleting and
// re-creating the repo at the same path would.
func wipeRepo(t *testing.T, repo string) {
	t.Helper()
	entries, err := os.ReadDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(repo, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

// A repo deleted without disable leaves its ledger row behind. Enable in
// a re-created repo at that path must name the row as the cause and the
// command that clears it, not only report the missing env shim the row
// implies (aae-orc-v0i1i).
func TestEnable_OrphanedRowNamesTheRowAndTheFix(t *testing.T) {
	t.Parallel()
	store := writeStore(t)
	repo := t.TempDir()
	opts := baseOpts(t, repo, store)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	wipeRepo(t, repo)

	err := Enable(opts)
	if err == nil {
		t.Fatal("re-enable over an orphaned row succeeded; the row should still refuse it")
	}
	msg := err.Error()
	for _, want := range []string{"ledger row", "sideshow disable vsdd-factory", "env shim missing"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal lacks %q:\n%s", want, msg)
		}
	}
}

// The recovery the refusal names has to work: disable in the re-created
// repo clears the row and the sidecar quietly (the settings file is gone,
// so there is nothing to restore or rewrite), and enable then succeeds.
func TestDisable_OrphanedRowClearsQuietlyThenEnableSucceeds(t *testing.T) {
	store := writeStore(t)
	repo := t.TempDir()
	plantSettings(t, repo, "local", nonCanonicalSettings)
	opts := baseOpts(t, repo, store)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	wipeRepo(t, repo)

	out := captureOut(t, func() {
		if err := Disable(opts); err != nil {
			t.Errorf("Disable: %v", err)
		}
	})
	if strings.Contains(out, "canonical form") {
		t.Errorf("disable of an orphaned row claims it rewrote a file:\n%s", out)
	}
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if led.RepoRow(repo, "vsdd-factory") != nil {
		t.Error("orphaned row survived disable")
	}
	if left := sidecarFiles(t, opts); len(left) != 0 {
		t.Errorf("sidecar left behind: %v", left)
	}
	if err := Enable(opts); err != nil {
		t.Errorf("re-enable after clearing the row: %v", err)
	}
}
