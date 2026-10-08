package adopt

import (
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// e1Line returns the E1 line of an equivalence report built over a store
// and a foreign tree that differ in one skill file.
func e1Line(t *testing.T, storeVersion, foreignVersion string) string {
	t.Helper()
	opts, _, cache := fixture(t, "project")
	mustWrite(t, cache, "skills/differs/SKILL.md", "foreign copy\n", 0o644)
	mustWrite(t, opts.StoreRoot, "skills/differs/SKILL.md", "store copy\n", 0o644)
	row := &ledger.Row{StorePath: opts.StoreRoot, Platform: platform(t), Version: storeVersion}
	install := &foreign.Install{InstallPath: cache, TreeVersion: foreignVersion}
	for _, l := range equivalenceReport(row, install) {
		if strings.HasPrefix(l, "E1 ") {
			return l
		}
	}
	t.Fatal("no E1 line in the report")
	return ""
}

// Same versions: differing trees are suspicious, and the report says to
// verify the store artifact (sideshow#97).
func TestEquivalenceReport_E1SameVersionDifferenceIsSuspicious(t *testing.T) {
	line := e1Line(t, "1.0.0-rc.23", "1.0.0-rc.23")
	for _, want := range []string{"E1 content parity: FAIL", "same-version trees should be identical", "verify the store artifact"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q:\n%s", want, line)
		}
	}
}

// Version drift the operator consented to makes differences the expected
// result, so the line says so and does not tell them to distrust the
// adoption (sideshow#97).
func TestEquivalenceReport_E1VersionDriftIsExpected(t *testing.T) {
	line := e1Line(t, "1.0.0-rc.23", "1.0.0-rc.22")
	for _, want := range []string{"E1 content parity: EXPECTED (version drift)", "1 differing paths", "1.0.0-rc.22", "1.0.0-rc.23"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q:\n%s", want, line)
		}
	}
	for _, bad := range []string{"same-version trees", "verify the store artifact", "FAIL"} {
		if strings.Contains(line, bad) {
			t.Errorf("drift line contains %q:\n%s", bad, line)
		}
	}
}
