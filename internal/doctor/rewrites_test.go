package doctor

import (
	"path/filepath"
	"strings"
	"testing"
)

const rewritesTwo = `
rewrites:
  neutralizer_rule_version: 1
  census_refreshed:
    - path: bmm/config.yaml
      installer_sha256: aaaa
    - path: cis/config.yaml
      installer_sha256: bbbb
`

// rewritesFixture installs one pack whose pack.yaml carries tail after
// the name and version, and returns its version dir.
func rewritesFixture(t *testing.T, tail string) string {
	t.Helper()
	home := fakeHome(t)
	dir := installVersion(t, home, "alpha", "1.0.0", true)
	write(t, filepath.Join(dir, "pack.yaml"), "name: alpha\nversion: \"1.0.0\"\n"+tail)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	return dir
}

func rewriteFindings(t *testing.T) []Finding {
	t.Helper()
	report, _, err := Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	return findBy(report.Findings, "store-declared-rewrites")
}

// aae-orc-psih9: the pack records the files its build rewrote after the
// installer hashed them. doctor reports the count as information.
func TestDeclaredRewrites_ReportsTheCountAsInfo(t *testing.T) {
	rewritesFixture(t, rewritesTwo)
	fs := rewriteFindings(t)
	if len(fs) != 1 {
		t.Fatalf("want one finding, got %+v", fs)
	}
	f := fs[0]
	if f.Status != OK || f.Class != Advisory || f.Pack != "alpha" || f.Layer != 1 {
		t.Errorf("finding = %+v, want an ok advisory layer-1 finding for alpha", f)
	}
	for _, want := range []string{"2 declared rewrites", "rule version 1", "bmm/config.yaml", "cis/config.yaml"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("detail missing %q: %q", want, f.Detail)
		}
	}
}

func TestDeclaredRewrites_AnEmptyRecordSaysZero(t *testing.T) {
	rewritesFixture(t, "\nrewrites:\n  neutralizer_rule_version: 1\n  census_refreshed: []\n")
	fs := rewriteFindings(t)
	if len(fs) != 1 || !strings.Contains(fs[0].Detail, "0 declared rewrites") {
		t.Errorf("want one finding saying 0 declared rewrites, got %+v", fs)
	}
}

// Control: a pack with no record is not wrong, so nothing is said.
func TestDeclaredRewrites_NoRecordNoFinding(t *testing.T) {
	rewritesFixture(t, "")
	if fs := rewriteFindings(t); len(fs) != 0 {
		t.Errorf("a pack with no rewrites block got %+v", fs)
	}
}

// A record that cannot be read is a warning with a next step, not a
// silent skip.
func TestDeclaredRewrites_MalformedRecordWarns(t *testing.T) {
	rewritesFixture(t, "\nrewrites: not-a-mapping\n")
	fs := rewriteFindings(t)
	if len(fs) != 1 || fs[0].Status != Warn || fs[0].Class != Advisory || fs[0].Next == "" {
		t.Errorf("want one advisory warn with a next step, got %+v", fs)
	}
}

// The record never excuses a file: an edit to a rewritten file still
// fails the census, and the info line stays.
func TestDeclaredRewrites_NeverExemptsAFileFromTheCensus(t *testing.T) {
	dir := rewritesFixture(t, rewritesTwo)
	write(t, filepath.Join(dir, "bmm", "config.yaml"), "refreshed\n")
	sum, err := sha256File(filepath.Join(dir, "bmm", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "_config", "files-manifest.csv"),
		"type,name,module,path,hash\n"+`"yaml","config","bmm","bmm/config.yaml","`+sum+`"`+"\n")

	report, _, err := Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if c := findBy(report.Findings, "store-content-census"); len(c) != 1 || c[0].Status != OK {
		t.Fatalf("census before the edit = %+v, want ok", c)
	}

	write(t, filepath.Join(dir, "bmm", "config.yaml"), "tampered\n")
	report, _, err = Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if c := findBy(report.Findings, "store-content-census"); len(c) != 1 || c[0].Status != Fail {
		t.Errorf("an edit to a rewritten file must still fail the census, got %+v", c)
	}
	if fs := findBy(report.Findings, "store-declared-rewrites"); len(fs) != 1 || fs[0].Status != OK {
		t.Errorf("the info line must stay ok, got %+v", fs)
	}
}
