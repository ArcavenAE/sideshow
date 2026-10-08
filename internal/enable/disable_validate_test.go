package enable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// plantRow appends a recorded artifact string to the repo's ledger row.
func plantRow(t *testing.T, opts Options, repo, artifact string) {
	t.Helper()
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	row := led.RepoRow(repo, "vsdd-factory")
	if row == nil {
		t.Fatal("no ledger row to plant into")
	}
	row.Artifacts = append(row.Artifacts, artifact)
	if err := led.SetRow(repo, "vsdd-factory", *row); err != nil {
		t.Fatal(err)
	}
	if err := led.Save(opts.LedgerPath); err != nil {
		t.Fatal(err)
	}
}

// replaceCompatSymlink swaps the enable-created plugins/ symlink for a
// real directory holding a file, as when the pack develops itself.
func replaceCompatSymlink(t *testing.T, repo string) {
	t.Helper()
	link := filepath.Join(repo, "plugins", "vsdd-factory")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("enable left no compat symlink at %s: %v", link, err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(link, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A ledger row disable would refuse must be refused before disable touches
// anything: the settings file keeps its enable-written bytes, the bound
// artifacts stay, the sidecar stays, and the row stays, so a retry after
// the row is fixed starts from a whole state (sideshow#160).
func TestDisable_RefusedRowChangesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		bad   string
		setup func(t *testing.T, repo string)
	}{
		"unknown kind":     {bad: "future-kind:abc.orig"},
		"outside the repo": {bad: "agent-file:../outside.md"},
		// The removal loop's own refusal, which the plan must also make:
		// the recorded compat symlink was replaced by a real directory.
		"compat symlink replaced by a directory": {setup: replaceCompatSymlink},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := writeStore(t)
			repo := writeRepo(t)
			opts := baseOpts(t, repo, store)
			if err := Enable(opts); err != nil {
				t.Fatalf("Enable: %v", err)
			}
			if tc.bad != "" {
				plantRow(t, opts, repo, tc.bad)
			}
			if tc.setup != nil {
				tc.setup(t, repo)
			}

			settings := filepath.Join(repo, ".claude", "settings.local.json")
			beforeSettings, err := os.ReadFile(settings)
			if err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, repo)
			beforeSidecars := sidecarFiles(t, opts)

			if err := Disable(opts); err == nil {
				t.Fatal("Disable accepted a row it should refuse")
			}

			afterSettings, err := os.ReadFile(settings)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterSettings) != string(beforeSettings) {
				t.Errorf("refused disable rewrote the settings file:\nbefore %s\nafter  %s", beforeSettings, afterSettings)
			}
			after := snapshot(t, repo)
			for k, v := range before {
				if after[k] != v {
					t.Errorf("refused disable changed %s", k)
				}
			}
			for k := range after {
				if _, ok := before[k]; !ok {
					t.Errorf("refused disable added %s", k)
				}
			}
			led, err := ledger.Load(opts.LedgerPath)
			if err != nil {
				t.Fatal(err)
			}
			if led.RepoRow(repo, "vsdd-factory") == nil {
				t.Error("refused disable dropped the ledger row")
			}
			if got := sidecarFiles(t, opts); len(got) != len(beforeSidecars) {
				t.Errorf("sidecars %v -> %v", beforeSidecars, got)
			}
		})
	}
}

// Control: the same disable on an intact row still works.
func TestDisable_IntactRowStillDisables(t *testing.T) {
	t.Parallel()
	store := writeStore(t)
	repo := writeRepo(t)
	opts := baseOpts(t, repo, store)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := Disable(opts); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if led.RepoRow(repo, "vsdd-factory") != nil {
		t.Error("ledger row survived a good disable")
	}
}
