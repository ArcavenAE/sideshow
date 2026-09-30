package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pipelineManifest renders file-manifest.csv the way the sideshow-packs
// builders write it: sha256,size,relpath, no header, sorted, never
// listing itself.
func pipelineManifest(t *testing.T, root string, rels ...string) string {
	t.Helper()
	var b strings.Builder
	for _, rel := range rels {
		p := filepath.Join(root, rel)
		sum, err := sha256File(p)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "%s,%d,%s\n", sum, info.Size(), rel)
	}
	return b.String()
}

func storeFileManifestFinding(t *testing.T) Finding {
	t.Helper()
	report, _, err := Run(Options{Layers: []int{1}, Pack: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	fs := findBy(report.Findings, "store-file-manifest")
	if len(fs) != 1 {
		t.Fatalf("want one store-file-manifest finding, got %+v", fs)
	}
	return fs[0]
}

func TestStoreFileManifest(t *testing.T) {
	home := fakeHome(t)
	dir := installVersion(t, home, "alpha", "1.0.0", true)
	registryYAML(t, home, `packs:
  - name: alpha
    version: 1.0.0
    path: `+filepath.Join(home, "packs", "alpha", "current")+`
`)

	// No manifest: the check has no input, and says so rather than ok.
	f := storeFileManifestFinding(t)
	if f.Status != Unavailable || f.Next == "" {
		t.Fatalf("no manifest must be unavailable with a next step: %+v", f)
	}

	write(t, filepath.Join(dir, "agents", "a.md"), "x\n")
	write(t, filepath.Join(dir, "file-manifest.csv"),
		pipelineManifest(t, dir, "agents/a.md", "pack.yaml"))

	f = storeFileManifestFinding(t)
	if f.Status != OK || !strings.Contains(f.Detail, "2 files") {
		t.Fatalf("matching store must be ok over 2 files: %+v", f)
	}

	cases := []struct {
		name   string
		mutate func()
		undo   func()
		want   string
	}{
		{
			"content differs",
			func() { write(t, filepath.Join(dir, "agents", "a.md"), "tampered\n") },
			func() { write(t, filepath.Join(dir, "agents", "a.md"), "x\n") },
			"agents/a.md",
		},
		{
			"listed file missing",
			func() { _ = os.Remove(filepath.Join(dir, "agents", "a.md")) },
			func() { write(t, filepath.Join(dir, "agents", "a.md"), "x\n") },
			"missing",
		},
		{
			"unlisted file present",
			func() { write(t, filepath.Join(dir, "agents", "extra.md"), "y\n") },
			func() { _ = os.Remove(filepath.Join(dir, "agents", "extra.md")) },
			"agents/extra.md",
		},
	}
	for _, tc := range cases {
		tc.mutate()
		f = storeFileManifestFinding(t)
		if f.Status != Fail || f.Class != Structural || !strings.Contains(f.Detail, tc.want) || f.Next == "" {
			t.Errorf("%s: want structural fail naming %q, got %+v", tc.name, tc.want, f)
		}
		tc.undo()
		if g := storeFileManifestFinding(t); g.Status != OK {
			t.Fatalf("%s: undo did not restore ok: %+v", tc.name, g)
		}
	}
}
