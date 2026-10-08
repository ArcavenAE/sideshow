package enable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/ledger"
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
	opts, _, settings := unknownRowScene(t, ".claude/notes.txt")
	var first error
	captureOut(t, func() { first = Disable(opts) })
	before, _ := os.ReadFile(settings)
	var second error
	out := captureOut(t, func() { second = Disable(opts) })
	if second == nil || first == nil || first.Error() != second.Error() {
		t.Errorf("rerun error = %v, want the same as %v", second, first)
	}
	if strings.Contains(out, "no record of its original bytes") || strings.Contains(out, "canonical form") || strings.Contains(out, "changed since") {
		t.Errorf("rerun printed a note:\n%s", out)
	}
	after, _ := os.ReadFile(settings)
	if string(before) != string(after) {
		t.Error("rerun changed the settings bytes")
	}
}

// A rerun by a build that knows the kind (the planted row is renamed to a
// known kind, as an upgrade would) prints no note, deletes the row and
// exits zero.
func TestDisable_RerunByABuildThatKnowsTheKindIsQuietAndComplete(t *testing.T) {
	opts, repo, _ := unknownRowScene(t, ".claude/notes.txt")
	captureOut(t, func() { _ = Disable(opts) })

	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	row := led.RepoRow(repo, "vsdd-factory")
	if row == nil || row.SettingsRestoredSHA == "" {
		t.Fatalf("the kept row carries no settings_restored_sha: %+v", row)
	}
	upgraded := *row
	upgraded.Artifacts = []string{"agent-file:.claude/notes.txt"}
	if err := led.SetRow(repo, "vsdd-factory", upgraded); err != nil {
		t.Fatal(err)
	}
	if err := led.Save(opts.LedgerPath); err != nil {
		t.Fatal(err)
	}

	var derr error
	out := captureOut(t, func() { derr = Disable(opts) })
	if derr != nil {
		t.Fatalf("Disable: %v", derr)
	}
	if strings.Contains(out, "no record") || strings.Contains(out, "canonical form") || strings.Contains(out, "changed since") {
		t.Errorf("a clean upgrade rerun printed a note:\n%s", out)
	}
	if rowArtifacts(t, opts, repo) != nil {
		t.Error("the row survived a complete rerun")
	}
}

// A user edit between the two passes is kept, and the rerun says so.
func TestDisable_RerunSaysWhenTheFileChangedSinceTheEarlierPass(t *testing.T) {
	opts, _, settings := unknownRowScene(t, ".claude/notes.txt")
	captureOut(t, func() { _ = Disable(opts) })
	edited := `{"env": {"MINE": "1"}}` + "\n"
	if err := os.WriteFile(settings, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureOut(t, func() { _ = Disable(opts) })
	if !strings.Contains(out, settings+" changed since the earlier disable pass; left as it is") {
		t.Errorf("missing the changed-since line:\n%s", out)
	}
	got, _ := os.ReadFile(settings)
	if string(got) != edited {
		t.Errorf("the user's bytes were changed: %q", got)
	}
}

// A settings file whose sideshow entries were removed by hand and that
// was reformatted keeps its bytes, with the note, as before: the restore
// never overwrites a hand edit just because its content matches.
func TestDisable_HandReformattedFileKeepsItsBytesWithTheNote(t *testing.T) {
	store := writeStore(t)
	repo := t.TempDir()
	settings := plantSettings(t, repo, bindings.ScopeLocal, nonCanonicalSettings)
	opts := baseOpts(t, repo, store)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	handEdited := "{\n    \"permissions\": {\n        \"allow\": [\"Bash(ls:*)\"]\n    },\n    \"env\": {\n        \"TEAM_VAR\": \"shared\"\n    },\n" +
		"    \"hooks\": {\n        \"PreToolUse\": [{\"matcher\": \"Bash\", \"hooks\": [{\"type\": \"command\", \"command\": \"echo team-guard\"}]}]\n    }\n}\n"
	if err := os.WriteFile(settings, []byte(handEdited), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureOut(t, func() {
		if err := Disable(opts); err != nil {
			t.Errorf("Disable: %v", err)
		}
	})
	got, _ := os.ReadFile(settings)
	if string(got) != handEdited {
		t.Errorf("a hand edit was overwritten:\n%s", got)
	}
	if !strings.Contains(out, "changed since enable") {
		t.Errorf("missing the note:\n%s", out)
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
