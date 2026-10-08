package enable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/bindings"
)

// unknownRowScene enables at local scope over a non-canonical settings
// file, lands a user file, and plants an unknown-kind row at it.
func unknownRowScene(t *testing.T, rel string) (opts Options, repo, settings string) {
	t.Helper()
	store := writeStore(t)
	repo = t.TempDir()
	settings = plantSettings(t, repo, bindings.ScopeLocal, nonCanonicalSettings)
	opts = baseOpts(t, repo, store)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plantRow(t, opts, repo, "future-kind:"+rel)
	return opts, repo, settings
}

// The first pass over a row with an unknown kind still restores the
// settings bytes and removes the sidecar (sideshow#175 review).
func TestDisable_UnknownKindFirstPassStillRestoresSettings(t *testing.T) {
	opts, _, settings := unknownRowScene(t, ".claude/notes.txt")
	out := captureOut(t, func() {
		if err := Disable(opts); err == nil {
			t.Error("Disable should report the unknown row")
		}
	})
	got, err := os.ReadFile(settings)
	if err != nil || string(got) != nonCanonicalSettings {
		t.Errorf("settings not restored: %q, %v", got, err)
	}
	if strings.Contains(out, "canonical form") {
		t.Errorf("first pass printed the fallback note:\n%s", out)
	}
	if left := sidecarFiles(t, opts); len(left) != 0 {
		t.Errorf("sidecar left after the first pass: %v", left)
	}
}

// The rerun has nothing to restore and must not claim the original
// bytes were never recorded.
func TestDisable_UnknownKindRerunPrintsNoFalseNote(t *testing.T) {
	opts, _, _ := unknownRowScene(t, ".claude/notes.txt")
	captureOut(t, func() { _ = Disable(opts) })
	out := captureOut(t, func() {
		if err := Disable(opts); err == nil {
			t.Error("rerun should still report the unknown row")
		}
	})
	if strings.Contains(out, "no record of its original bytes") || strings.Contains(out, "canonical form") {
		t.Errorf("rerun printed a false note:\n%s", out)
	}
}

// A pass killed after the settings rewrite and before the restore leaves
// the sidecar; the rerun restores the original bytes from it.
func TestDisable_UnknownKindRerunAfterKillRestoresFromSidecar(t *testing.T) {
	opts, _, settings := unknownRowScene(t, ".claude/notes.txt")
	if _, err := bindings.RemoveHookChain(settings, opts.Pack); err != nil {
		t.Fatal(err)
	}
	led := opts.LedgerPath
	_ = led
	if len(sidecarFiles(t, opts)) != 1 {
		t.Fatal("the scene has no sidecar to survive the kill")
	}
	captureOut(t, func() { _ = Disable(opts) })
	got, err := os.ReadFile(settings)
	if err != nil || string(got) != nonCanonicalSettings {
		t.Errorf("rerun did not restore from the sidecar: %q, %v", got, err)
	}
	if left := sidecarFiles(t, opts); len(left) != 0 {
		t.Errorf("sidecar left after the rerun: %v", left)
	}
}

// A known row that is still on disk after a pass (a parent dir holding
// the unknown path) stays in the ledger, so a later disable removes it.
func TestDisable_UnknownKindKeepsKnownRowsStillOnDisk(t *testing.T) {
	opts, repo, _ := unknownRowScene(t, ".claude/agents/future.md")
	if err := Disable(opts); err == nil {
		t.Fatal("Disable should report the unknown row")
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "agents")); err != nil {
		t.Fatalf("the parent dir should remain while the unknown path is inside it: %v", err)
	}
	var kept bool
	for _, a := range rowArtifacts(t, opts, repo) {
		if a == "parent-dir:.claude/agents" {
			kept = true
		}
	}
	if !kept {
		t.Errorf("ledger row dropped the parent dir that is still on disk: %v", rowArtifacts(t, opts, repo))
	}
}
