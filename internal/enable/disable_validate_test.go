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

// A ledger row disable would refuse must be refused before disable touches
// anything: the settings file keeps its enable-written bytes, the bound
// artifacts stay, the sidecar stays, and the row stays, so a retry after
// the row is fixed starts from a whole state (sideshow#160).
func TestDisable_RefusedRowChangesNothing(t *testing.T) {
	for name, bad := range map[string]string{
		"unknown kind":     "future-kind:abc.orig",
		"outside the repo": "agent-file:../outside.md",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := writeStore(t)
			repo := writeRepo(t)
			opts := baseOpts(t, repo, store)
			if err := Enable(opts); err != nil {
				t.Fatalf("Enable: %v", err)
			}
			plantRow(t, opts, repo, bad)

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
