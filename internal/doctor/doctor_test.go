package doctor

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHome points SIDESHOW_HOME at a fresh temp dir and returns it.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	return home
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// installVersion lays down one store version dir with optional
// current symlink, without going through InstallFromLocal (tests
// exercise doctor's reading, not install's writing).
func installVersion(t *testing.T, home, name, version string, current bool) string {
	t.Helper()
	dir := filepath.Join(home, "packs", name, version)
	write(t, filepath.Join(dir, "pack.yaml"), "name: "+name+"\nversion: \""+version+"\"\n")
	if current {
		link := filepath.Join(home, "packs", name, "current")
		_ = os.Remove(link)
		if err := os.Symlink(version, link); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func registryYAML(t *testing.T, home, content string) {
	t.Helper()
	write(t, filepath.Join(home, "registry.yaml"), content)
}

func findBy(fs []Finding, id string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out
}

// --- exit policy: the diagnostic-not-gate rule as code ---

func TestExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		findings []Finding
		strict   bool
		want     int
	}{
		{"clean", []Finding{{Status: OK, Class: Structural}}, false, 0},
		{"structural fail", []Finding{{Status: Fail, Class: Structural}}, false, 2},
		{"advisory fail never gates", []Finding{{Status: Fail, Class: Advisory}}, false, 0},
		{"advisory fail never gates even strict", []Finding{{Status: Fail, Class: Advisory}}, true, 0},
		{"structural warn lenient", []Finding{{Status: Warn, Class: Structural}}, false, 0},
		{"structural warn strict", []Finding{{Status: Warn, Class: Structural}}, true, 2},
		{"advisory warn strict never gates", []Finding{{Status: Warn, Class: Advisory}}, true, 0},
		{"unavailable never gates", []Finding{{Status: Unavailable, Class: Structural}}, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ExitCode(tt.findings, tt.strict); got != tt.want {
				t.Errorf("ExitCode = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestClampAdvisoryForcesWarnOnly(t *testing.T) {
	t.Parallel()
	fs := clampAdvisory([]Finding{
		{Status: Fail, Class: Structural, Detail: "x"},
		{Status: OK, Class: Structural},
	})
	if fs[0].Status != Warn || fs[0].Class != Advisory {
		t.Errorf("fail not clamped: %+v", fs[0])
	}
	if !strings.Contains(fs[0].Detail, "clamped") {
		t.Errorf("clamp must name itself: %q", fs[0].Detail)
	}
	if fs[1].Class != Advisory {
		t.Errorf("layer-3 ok finding must still be advisory: %+v", fs[1])
	}
}

func TestSweepInvariantsNamesMissingNext(t *testing.T) {
	t.Parallel()
	fs := sweepInvariants([]Finding{{Status: Warn, Class: Advisory}})
	if fs[0].Next == "" || !strings.Contains(fs[0].Next, "doctor bug") {
		t.Errorf("missing next must be visible, got %q", fs[0].Next)
	}
}

// --- layer 1: store ---

func TestStoreCoherence(t *testing.T) {
	home := fakeHome(t)
	installVersion(t, home, "alpha", "1.0.0", true)
	installVersion(t, home, "skewed", "1.0.0", true)
	dangDir := installVersion(t, home, "dangling", "1.0.0", true)
	if err := os.RemoveAll(dangDir); err != nil {
		t.Fatal(err)
	}
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
  - name: skewed
    version: 2.0.0
    path: `+filepath.Join(home, "packs", "skewed", "current")+`
  - name: dangling
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "dangling", "current")+`
`)
	report, _, err := Run(Options{Layers: []int{1}, Now: time.Unix(0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Status{}
	for _, f := range findBy(report.Findings, "store-coherence") {
		got[f.Pack] = f.Status
	}
	if got["alpha"] != OK {
		t.Errorf("alpha = %v, want ok", got["alpha"])
	}
	if got["skewed"] != Fail {
		t.Errorf("skewed = %v, want fail (registry 2.0.0 vs symlink 1.0.0)", got["skewed"])
	}
	if got["dangling"] != Fail {
		t.Errorf("dangling = %v, want fail", got["dangling"])
	}
}

func TestStoreFreeze(t *testing.T) {
	home := fakeHome(t)
	dir := installVersion(t, home, "alpha", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	// Writable tree: expect the advisory warn naming benign causes.
	report, _, err := Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "store-freeze")
	if len(fs) != 1 || fs[0].Status != Warn || fs[0].Class != Advisory {
		t.Fatalf("writable store: %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "cause not recorded") {
		t.Errorf("must name benign causes: %q", fs[0].Detail)
	}

	// Freeze it: expect ok.
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode().Perm()&^0o222)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			return os.Chmod(path, info.Mode().Perm()|0o200)
		})
	})
	report, _, err = Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	fs = findBy(report.Findings, "store-freeze")
	if len(fs) != 1 || fs[0].Status != OK {
		t.Fatalf("frozen store: %+v", fs)
	}
}

func TestContentCensus(t *testing.T) {
	home := fakeHome(t)
	dir := installVersion(t, home, "alpha", "1.0.0", true)
	installVersion(t, home, "nocensus", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
  - name: nocensus
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "nocensus", "current")+`
`)
	// content "x\n" -> sha256
	write(t, filepath.Join(dir, "agents", "a.md"), "x\n")
	sum, err := sha256File(filepath.Join(dir, "agents", "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "_config", "files-manifest.csv"),
		"type,name,module,path,hash\n"+
			`"md","a","agents","agents/a.md","`+sum+`"`+"\n"+
			`"md","gone","agents","agents/gone.md","deadbeef"`+"\n")

	report, _, err := Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	byPack := map[string]Finding{}
	for _, f := range findBy(report.Findings, "store-content-census") {
		byPack[f.Pack] = f
	}
	if byPack["alpha"].Status != OK {
		t.Errorf("alpha census = %+v, want ok (unresolved paths are counted, not failed)", byPack["alpha"])
	}
	if byPack["nocensus"].Status != Unavailable || !strings.Contains(byPack["nocensus"].Next, "wk92") {
		t.Errorf("no census must be unavailable naming wk92: %+v", byPack["nocensus"])
	}

	// Now corrupt the file: structural fail.
	write(t, filepath.Join(dir, "agents", "a.md"), "tampered\n")
	report, _, err = Run(Options{Layers: []int{1}, Pack: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "store-content-census")
	if len(fs) != 1 || fs[0].Status != Fail || fs[0].Class != Structural {
		t.Fatalf("mismatch must fail structurally: %+v", fs)
	}
}

func TestSyncManifest(t *testing.T) {
	home := fakeHome(t)
	installVersion(t, home, "alpha", "2.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 2.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	present := filepath.Join(home, "bind-present.md")
	write(t, present, "x")
	write(t, filepath.Join(home, "sync-manifest.yaml"), `schema_version: 0.1.0
entries:
  - pack: alpha
    version: 2.0.0
    kind: command
    path: `+present+`
  - pack: alpha
    version: 1.0.0
    kind: command
    path: `+present+`
  - pack: alpha
    version: 2.0.0
    kind: command
    path: `+filepath.Join(home, "gone.md")+`
  - pack: alpha
    version: custom
    kind: skill-dir
    path: `+present+`
`)
	report, _, err := Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "sync-manifest")
	var stale, missing int
	for _, f := range fs {
		if f.Status != Warn {
			continue
		}
		if strings.Contains(f.Detail, "synced from 1.0.0") {
			stale++
		}
		if strings.Contains(f.Detail, "file is gone") {
			missing++
		}
		if f.Next != "sideshow commands sync" {
			t.Errorf("next must name the command: %+v", f)
		}
	}
	if stale != 1 || missing != 1 {
		t.Errorf("stale=%d missing=%d, want 1 and 1: %+v", stale, missing, fs)
	}
	// The custom-source entry (version "custom") is project content;
	// currency does not apply and it must not have warned.
	for _, f := range fs {
		if strings.Contains(f.Detail, "synced from custom") {
			t.Errorf("custom-source entry warned on currency: %+v", f)
		}
	}
}

// --- layer 1: receipts ---

// receiptFixture builds a project root with a repos.yaml, one
// consumer repo, and a registry receipt naming the given artifacts.
func receiptFixture(t *testing.T, home string, artifactsYAML string) (root, repo string) {
	t.Helper()
	root = t.TempDir()
	repo = filepath.Join(root, "consumer")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "repos.yaml"), `repos:
  consumer:
    path: consumer
`)
	installVersion(t, home, "alpha", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
projects:
  - id: proj-1
    installations:
      - root: `+root+`
        manifest: `+filepath.Join(root, "repos.yaml")+`
        repos:
          - name: consumer
            packs:
              - pack: alpha
                version: 1.0.0
                scope: project
                artifacts:
`+artifactsYAML)
	return root, repo
}

func TestReceiptMarkers(t *testing.T) {
	home := fakeHome(t)
	_, repo := receiptFixture(t, home, `                  - type: claude_md
                    section_id: alpha-rules
                  - type: rules
                    path: .claude/rules/alpha.md
                    checksum: sha256:ignored
`)
	// Lone begin marker: the silent-duplication hazard.
	write(t, filepath.Join(repo, "CLAUDE.md"), "# repo\n<!-- sideshow:alpha-rules:begin -->\ncontent\n")
	// Rules file that lost its managed-by first line.
	write(t, filepath.Join(repo, ".claude", "rules", "alpha.md"), "no marker here\n")

	report, _, err := Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "receipt-markers")
	var unpaired, unmarked bool
	for _, f := range fs {
		if f.Status == Fail && strings.Contains(f.Detail, "unpaired") {
			unpaired = true
		}
		if f.Status == Warn && strings.Contains(f.Detail, "managed-by marker") {
			unmarked = true
		}
	}
	if !unpaired || !unmarked {
		t.Errorf("unpaired=%v unmarked=%v: %+v", unpaired, unmarked, fs)
	}
}

func TestReceiptSymlinks(t *testing.T) {
	home := fakeHome(t)
	_, repo := receiptFixture(t, home, `                  - type: symlink
                    path: _alpha/custom
                    target: ../_alpha-custom
`)
	// Points at the wrong target.
	if err := os.MkdirAll(filepath.Join(repo, "_alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../elsewhere", filepath.Join(repo, "_alpha", "custom")); err != nil {
		t.Fatal(err)
	}
	report, _, err := Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "receipt-symlinks")
	if len(fs) != 1 || fs[0].Status != Fail || !strings.Contains(fs[0].Detail, "receipt records") {
		t.Fatalf("wrong target must fail naming both paths: %+v", fs)
	}
}

// --- layer 1: plugin-class seam ---

func TestLedgerRowCoherence(t *testing.T) {
	home := fakeHome(t)
	good := installVersion(t, home, "plug", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: plug
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "plug", "current")+`
`)
	repoGood := t.TempDir()
	repoCurrent := t.TempDir()
	write(t, filepath.Join(home, "repo-bindings.yaml"), `schema_version: 0.1.0
repos:
  `+repoGood+`:
    plug:
      version: 1.0.0
      store_path: `+good+`
      channel: sideshow-native
  `+repoCurrent+`:
    plug:
      version: 1.0.0
      store_path: `+filepath.Join(home, "packs", "plug", "current")+`
      channel: sideshow-native
  /nonexistent-repo-dir:
    plug:
      version: 1.0.0
      store_path: `+good+`
      channel: sideshow-native
`)
	report, _, err := Run(Options{Layers: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "ledger-row-coherence")
	got := map[string]Finding{}
	for _, f := range fs {
		got[f.Subject] = f
	}
	if got[repoGood].Status != OK {
		t.Errorf("good row: %+v", got[repoGood])
	}
	if f := got[repoCurrent]; f.Status != Fail || !strings.Contains(f.Detail, "current") {
		t.Errorf("current pin must fail: %+v", f)
	}
	if f := got["/nonexistent-repo-dir"]; f.Status != Warn || f.Class != Advisory {
		t.Errorf("dead repo dir is advisory, not failure: %+v", f)
	}
}

// --- layer 4 ---

func TestReceiptDrift(t *testing.T) {
	home := fakeHome(t)
	_, repo := receiptFixture(t, home, "") // artifacts appended below
	// Rebuild registry with checksummed artifacts.
	current := filepath.Join(repo, "current.md")
	write(t, current, "kept\n")
	sum, err := sha256File(current)
	if err != nil {
		t.Fatal(err)
	}
	drifting := filepath.Join(repo, "drifting.md")
	write(t, drifting, "edited by hand\n")
	root := filepath.Dir(repo)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
projects:
  - id: proj-1
    installations:
      - root: `+root+`
        manifest: `+filepath.Join(root, "repos.yaml")+`
        repos:
          - name: consumer
            packs:
              - pack: alpha
                version: 1.0.0
                artifacts:
                  - type: files
                    path: current.md
                    checksum: sha256:`+sum+`
                  - type: files
                    path: drifting.md
                    checksum: sha256:0000000000000000000000000000000000000000000000000000000000000000
                  - type: files
                    path: missing.md
                    checksum: sha256:1111111111111111111111111111111111111111111111111111111111111111
`)
	report, _, err := Run(Options{Layers: []int{4}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "receipt-drift")
	var driftedAdvisory, missingStructural, summary bool
	for _, f := range fs {
		if f.Status == Warn && f.Class == Advisory && strings.Contains(f.Subject, "drifting.md") {
			driftedAdvisory = true
		}
		if f.Status == Warn && f.Class == Structural && strings.Contains(f.Subject, "missing.md") {
			missingStructural = true
		}
		if f.Status == OK && strings.Contains(f.Detail, "1 current, 1 drifted") {
			summary = true
		}
	}
	if !driftedAdvisory || !missingStructural || !summary {
		t.Errorf("drifted=%v missing=%v summary=%v: %+v", driftedAdvisory, missingStructural, summary, fs)
	}
}

func TestReceiptDriftNoReceiptsIsUnavailable(t *testing.T) {
	home := fakeHome(t)
	installVersion(t, home, "alpha", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	report, _, err := Run(Options{Layers: []int{4}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "receipt-drift")
	if len(fs) != 1 || fs[0].Status != Unavailable {
		t.Fatalf("no receipts must be unavailable, not clean: %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "not the same as no drift") {
		t.Errorf("must say silence is not cleanliness: %q", fs[0].Detail)
	}
}

func TestVersionSkew(t *testing.T) {
	home := fakeHome(t)
	old := installVersion(t, home, "alpha", "1.0.0", false)
	installVersion(t, home, "alpha", "2.0.0", true)
	repo := t.TempDir()
	registryYAML(t, home, `packs:
  - name: alpha
    version: 2.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	write(t, filepath.Join(home, "repo-bindings.yaml"), `schema_version: 0.1.0
repos:
  `+repo+`:
    alpha:
      version: 1.0.0
      store_path: `+old+`
      channel: sideshow-native
`)
	report, _, err := Run(Options{Layers: []int{4}})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "version-skew")
	if len(fs) != 1 || fs[0].Status != Warn || fs[0].Class != Advisory {
		t.Fatalf("skewed enable must warn advisory: %+v", fs)
	}
}

// --- absent inputs and layer plumbing ---

func TestAbsentInputsReportUnavailable(t *testing.T) {
	home := fakeHome(t)
	installVersion(t, home, "alpha", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	report, _, err := Run(Options{Layers: []int{4, 5}})
	if err != nil {
		t.Fatal(err)
	}
	wantTickets := map[string]string{
		"lock-pins":          "aae-orc-333y",
		"known-defects":      "aae-orc-ztg5",
		"unapplied-overlays": "aae-orc-10vq",
	}
	for id, ticket := range wantTickets {
		fs := findBy(report.Findings, id)
		if len(fs) != 1 || fs[0].Status != Unavailable {
			t.Errorf("%s: %+v, want unavailable", id, fs)
			continue
		}
		if !strings.Contains(fs[0].Next, ticket) {
			t.Errorf("%s next must name %s: %q", id, ticket, fs[0].Next)
		}
	}
}

func TestLayerTwoIsRejectedWithPointer(t *testing.T) {
	fakeHome(t)
	_, _, err := Run(Options{Layers: []int{2}})
	if err == nil || !strings.Contains(err.Error(), "aae-orc-a44c") {
		t.Fatalf("layer 2 must be rejected naming the deferral: %v", err)
	}
}

func TestLayer3IsAdvisoryByConstruction(t *testing.T) {
	home := fakeHome(t)
	installVersion(t, home, "alpha", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	report, _, err := Run(Options{Layers: []int{3}, RepoDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("layer 3 must produce findings")
	}
	for _, f := range report.Findings {
		if f.Class != Advisory {
			t.Errorf("layer-3 finding not advisory: %+v", f)
		}
		if f.Status == Fail {
			t.Errorf("layer-3 finding graded fail: %+v", f)
		}
	}
	if report.Summary.ExitCode != 0 {
		t.Errorf("layer 3 alone must never move the exit code, got %d", report.Summary.ExitCode)
	}
}

func TestEveryNonOKFindingCarriesNext(t *testing.T) {
	home := fakeHome(t)
	installVersion(t, home, "alpha", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 2.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	report, _, err := Run(Options{RepoDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Status != OK && f.Next == "" {
			t.Errorf("non-ok finding without next: %+v", f)
		}
	}
}

func TestReportJSONAndText(t *testing.T) {
	home := fakeHome(t)
	installVersion(t, home, "alpha", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)
	report, layers, err := Run(Options{Now: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := report.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("json round-trip: %v", err)
	}
	if decoded.SchemaVersion != "0.1.0" {
		t.Errorf("schema_version = %q", decoded.SchemaVersion)
	}

	buf.Reset()
	report.WriteText(&buf, layers)
	text := buf.String()
	if !strings.Contains(text, "layer 2, pack-declared validation, is deferred") {
		t.Errorf("text must explain the layer-2 gap:\n%s", text)
	}
	for _, l := range []string{"layer 1:", "layer 3:", "layer 4:", "layer 5:"} {
		if !strings.Contains(text, l) {
			t.Errorf("text missing %q:\n%s", l, text)
		}
	}
}

func TestPackFilterNarrowsEveryLayer(t *testing.T) {
	home := fakeHome(t)
	installVersion(t, home, "alpha", "1.0.0", true)
	installVersion(t, home, "beta", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
  - name: beta
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "beta", "current")+`
`)
	report, _, err := Run(Options{Layers: []int{1}, Pack: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Pack == "beta" {
			t.Errorf("beta finding leaked through the filter: %+v", f)
		}
	}
}
