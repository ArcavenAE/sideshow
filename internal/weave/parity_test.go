package weave

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// copyTree copies src into dst, creating dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if de.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// snapshot returns a path-to-content map of every file under root.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diffSnapshots(t *testing.T, want, got map[string]string, ignore func(string) bool) {
	t.Helper()

	var keys []string
	seen := map[string]bool{}
	for k := range want {
		if !ignore(k) && !seen[k] {
			keys, seen[k] = append(keys, k), true
		}
	}
	for k := range got {
		if !ignore(k) && !seen[k] {
			keys, seen[k] = append(keys, k), true
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		w, wok := want[k]
		g, gok := got[k]
		switch {
		case !wok:
			t.Errorf("engine produced an unexpected file %s:\n%s", k, g)
		case !gok:
			t.Errorf("engine did not produce %s; shell wrote:\n%s", k, w)
		case w != g:
			t.Errorf("%s differs from the shell output:\n--- shell ---\n%s\n--- engine ---\n%s", k, w, g)
		}
	}
}

// TestMidwayParity asserts the engine reproduces a real bmad-post-update.sh
// byte for byte. testdata/midway/after was captured by executing a
// synthetic-data variant of that script against a byte-identical copy of
// testdata/midway/before. See testdata/midway/PROVENANCE.md for what the
// substitution changed and what it does not establish.
func TestMidwayParity(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("testdata", "midway", "before"), root)

	decl, err := Load(filepath.Join("testdata", "midway", "weave.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	res, err := Apply(decl, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if failures := res.Failures(); len(failures) > 0 {
		t.Fatalf("unexpected failures: %+v", failures)
	}

	want := snapshot(t, filepath.Join("testdata", "midway", "after"))
	got := snapshot(t, root)
	// The declaration itself is not part of the shell script's output.
	diffSnapshots(t, want, got, func(p string) bool {
		return p == "_bmad-custom/weave.yaml"
	})
}

// TestMidwayParityIsIdempotent asserts a second application changes nothing, and
// that every operation reports skipped rather than silently reapplying.
func TestMidwayParityIsIdempotent(t *testing.T) {
	root := t.TempDir()
	copyTree(t, filepath.Join("testdata", "midway", "before"), root)

	decl, err := Load(filepath.Join("testdata", "midway", "weave.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(decl, root, Options{}); err != nil {
		t.Fatal(err)
	}
	first := snapshot(t, root)

	res, err := Apply(decl, root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	diffSnapshots(t, first, snapshot(t, root), func(string) bool { return false })

	for _, a := range res.Actions {
		if a.Type == "verify" {
			continue
		}
		if a.Outcome != Skipped {
			t.Errorf("second run: %s %s reported %s (%s), want skipped",
				a.Type, a.Name, a.Outcome, a.Detail)
		}
	}
}

// TestMidwayRowsMatchTheShellSource guards the byte-exactness of the ported rows
// independently of the tree diff, so a fixture regeneration cannot quietly
// launder a transcription error.
func TestMidwayRowsMatchTheShellSource(t *testing.T) {
	decl, err := Load(filepath.Join("testdata", "midway", "weave.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	after := snapshot(t, filepath.Join("testdata", "midway", "after"))
	targets := map[string]string{
		"agent-manifest": "_bmad/_config/agent-manifest.csv",
		"bmad-help":      "_bmad/_config/bmad-help.csv",
		"default-party":  "_bmad/bmm/teams/default-party.csv",
	}

	for _, inj := range decl.CSVInjections {
		golden, ok := after[targets[inj.Name]]
		if !ok {
			t.Fatalf("no golden for %s", inj.Name)
		}
		for i, row := range inj.Rows {
			if !strings.Contains(golden, row+"\n") {
				t.Errorf("%s row %d is not present verbatim in the shell output:\n%s",
					inj.Name, i, row)
			}
		}
	}

	// The script hardcodes 5 memories in its PLATFORM_MEMORIES array while the
	// <project>-agent-memories.yaml it calls its single source of truth carries
	// 6. The array is what runs, so the array is the parity target. See the
	// divergence note in examples/midway/weave.yaml.
	mems := decl.MemoryInjections[0].Memories
	if len(mems) != 5 {
		t.Fatalf("want 5 memories to match the shell array, got %d", len(mems))
	}
	arch := after["_bmad/_config/agents/bmm-architect.customize.yaml"]
	for i, m := range mems {
		if !strings.Contains(arch, "  - "+quoteYAML(m)+"\n") {
			t.Errorf("memory %d is not present verbatim in the shell output:\n%s", i, m)
		}
	}
}
