package adopt

import (
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// e1Line returns the E1 line of an equivalence report built over a store
// and a foreign tree that differ in one skill file. rowVersion is the
// adopted (store) version; treeVersion and registryVersion are the foreign
// install's tree and registry-recorded versions.
func e1Line(t *testing.T, rowVersion, treeVersion, registryVersion string) string {
	t.Helper()
	opts, _, cache := fixture(t, "project")
	mustWrite(t, cache, "skills/differs/SKILL.md", "foreign copy\n", 0o644)
	mustWrite(t, opts.StoreRoot, "skills/differs/SKILL.md", "store copy\n", 0o644)
	row := &ledger.Row{StorePath: opts.StoreRoot, Platform: platform(t), Version: rowVersion}
	install := &foreign.Install{InstallPath: cache, TreeVersion: treeVersion, Version: registryVersion}
	for _, l := range equivalenceReport(row, install) {
		if strings.HasPrefix(l, "E1 ") {
			return l
		}
	}
	t.Fatal("no E1 line in the report")
	return ""
}

const (
	e1Fail = "E1 content parity: FAIL \u2014 1 differing paths (first 1: skills/differs/SKILL.md); " +
		"same-version trees should be identical \u2014 verify the store artifact before trusting the adoption"
	e1Drift = "E1 content parity: EXPECTED (version drift): 1 differing paths (first 1: skills/differs/SKILL.md) " +
		"between the running 1.0.0-rc.22 and the adopted 1.0.0-rc.23"
)

// The whole E1 line is pinned in each case, so the running and adopted
// versions appear in order and the original wording is untouched where the
// drift is not provable (sideshow#97).
func TestEquivalenceReport_E1Line(t *testing.T) {
	for name, tc := range map[string]struct {
		row, tree, registry string
		want                string
	}{
		"same version is suspicious":           {"1.0.0-rc.23", "1.0.0-rc.23", "", e1Fail},
		"tree version differs is drift":        {"1.0.0-rc.23", "1.0.0-rc.22", "", e1Drift},
		"registry version used when no tree":   {"1.0.0-rc.23", "", "1.0.0-rc.22", e1Drift},
		"tree version wins over the registry":  {"1.0.0-rc.23", "1.0.0-rc.23", "1.0.0-rc.22", e1Fail},
		"empty adopted version keeps the FAIL": {"", "1.0.0-rc.22", "", e1Fail},
		"empty running version keeps the FAIL": {"1.0.0-rc.23", "", "", e1Fail},
	} {
		t.Run(name, func(t *testing.T) {
			if got := e1Line(t, tc.row, tc.tree, tc.registry); got != tc.want {
				t.Errorf("E1 line:\n got  %s\n want %s", got, tc.want)
			}
		})
	}
}
