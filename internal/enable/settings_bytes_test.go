package enable

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/bindings"
)

// nonCanonicalSettings is valid JSON that the settings writer would
// reformat: unsorted keys, inline objects, no indentation.
const nonCanonicalSettings = "{\n\"permissions\": { \"allow\": [\"Bash(ls:*)\"] },\n" +
	"\"env\": { \"TEAM_VAR\": \"shared\" },\n" +
	"\"hooks\": { \"PreToolUse\": [ { \"matcher\": \"Bash\", \"hooks\": [{ \"type\": \"command\", \"command\": \"echo team-guard\" }] } ] }\n}\n"

// captureOut returns what fn wrote to stdout.
func captureOut(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	os.Stdout = old
	return <-done
}

func settingsPathFor(repo string, scope bindings.RepoScope) string {
	return settingsFile(repo, scope)
}

func plantSettings(t *testing.T, repo string, scope bindings.RepoScope, content string) string {
	t.Helper()
	p := settingsPathFor(repo, scope)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// sidecarFiles lists what is left in the original-bytes sidecar dir.
func sidecarFiles(t *testing.T, opts Options) []string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(opts.LedgerPath), "settings-originals")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// Disable returns an unchanged settings file to its original bytes at
// both scopes, and removes the sidecar it restored from (aae-orc-gf80m).
func TestDisable_RestoresSettingsBytesWhenUnchangedSinceEnable(t *testing.T) {
	for _, scope := range []bindings.RepoScope{bindings.ScopeProject, bindings.ScopeLocal} {
		t.Run(string(scope), func(t *testing.T) {
			store := writeStore(t)
			repo := t.TempDir()
			p := plantSettings(t, repo, scope, nonCanonicalSettings)
			opts := baseOpts(t, repo, store)
			opts.Scope = scope

			if err := Enable(opts); err != nil {
				t.Fatalf("Enable: %v", err)
			}
			if got := sidecarFiles(t, opts); len(got) != 1 {
				t.Fatalf("sidecar after enable = %v, want one file", got)
			}
			out := captureOut(t, func() {
				if err := Disable(opts); err != nil {
					t.Errorf("Disable: %v", err)
				}
			})
			got, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != nonCanonicalSettings {
				t.Errorf("settings not byte-exact after round trip:\nwant %q\n got %q", nonCanonicalSettings, got)
			}
			if strings.Contains(out, "canonical form") {
				t.Errorf("restore path printed the fallback line:\n%s", out)
			}
			if left := sidecarFiles(t, opts); len(left) != 0 {
				t.Errorf("sidecar left after disable: %v", left)
			}
		})
	}
}

// A file edited after enable cannot be restored from the record: disable
// removes exactly what enable added, keeps the edit, says so, and still
// deletes the sidecar.
func TestDisable_FallsBackAndSaysSoWhenSettingsEditedAfterEnable(t *testing.T) {
	store := writeStore(t)
	repo := t.TempDir()
	p := plantSettings(t, repo, bindings.ScopeLocal, nonCanonicalSettings)
	opts := baseOpts(t, repo, store)

	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "\"TEAM_VAR\": \"shared\"", "\"TEAM_VAR\": \"shared\",\n    \"ADDED_LATER\": \"1\"", 1)
	if edited == string(data) {
		t.Fatal("fixture edit did not change the file")
	}
	if err := os.WriteFile(p, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureOut(t, func() {
		if err := Disable(opts); err != nil {
			t.Errorf("Disable: %v", err)
		}
	})
	if !strings.Contains(out, "changed since enable") || !strings.Contains(out, "canonical form") {
		t.Errorf("fallback line missing from output:\n%s", out)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "ADDED_LATER") || !strings.Contains(string(got), "team-guard") {
		t.Errorf("fallback lost user content:\n%s", got)
	}
	if strings.Contains(string(got), "CLAUDE_PLUGIN_ROOT") || strings.Contains(string(got), "sideshow:") {
		t.Errorf("fallback left enable's entries behind:\n%s", got)
	}
	if left := sidecarFiles(t, opts); len(left) != 0 {
		t.Errorf("sidecar left after fallback: %v", left)
	}
}

// The sidecar can hold env values; it is private to the user and sits
// beside the ledger in the sideshow store, never in the repo.
func TestEnable_SidecarIsPrivateAndOutsideTheRepo(t *testing.T) {
	store := writeStore(t)
	repo := t.TempDir()
	plantSettings(t, repo, bindings.ScopeLocal, nonCanonicalSettings)
	opts := baseOpts(t, repo, store)

	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	dir := filepath.Join(filepath.Dir(opts.LedgerPath), "settings-originals")
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("sidecar dir %s = %v, %v", dir, entries, err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("sidecar mode = %o, want 0600", perm)
	}
	if strings.HasPrefix(dir, repo) {
		t.Errorf("sidecar dir %s is inside the repo %s", dir, repo)
	}
}

// An enable that created the settings file has no original bytes to
// keep, so it writes no sidecar and disable prints no fallback line.
func TestEnable_CreatedSettingsFileWritesNoSidecar(t *testing.T) {
	store := writeStore(t)
	repo := t.TempDir()
	opts := baseOpts(t, repo, store)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if got := sidecarFiles(t, opts); len(got) != 0 {
		t.Errorf("sidecar for a created file: %v", got)
	}
	out := captureOut(t, func() {
		if err := Disable(opts); err != nil {
			t.Errorf("Disable: %v", err)
		}
	})
	if strings.Contains(out, "canonical form") {
		t.Errorf("fallback line for a file enable created:\n%s", out)
	}
}

// A row enabled before sidecars existed has no record of the original
// bytes: disable still removes exactly what enable added and says why the
// file is in canonical form.
func TestDisable_NoSidecarSaysTheOriginalBytesWereNotKept(t *testing.T) {
	store := writeStore(t)
	repo := t.TempDir()
	p := plantSettings(t, repo, bindings.ScopeLocal, nonCanonicalSettings)
	opts := baseOpts(t, repo, store)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	for _, f := range sidecarFiles(t, opts) {
		if err := os.Remove(filepath.Join(filepath.Dir(opts.LedgerPath), "settings-originals", f)); err != nil {
			t.Fatal(err)
		}
	}
	out := captureOut(t, func() {
		if err := Disable(opts); err != nil {
			t.Errorf("Disable: %v", err)
		}
	})
	if !strings.Contains(out, "no record of its original bytes") || !strings.Contains(out, "canonical form") {
		t.Errorf("older-row line missing:\n%s", out)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "CLAUDE_PLUGIN_ROOT") || !strings.Contains(string(got), "team-guard") {
		t.Errorf("fallback removal wrong:\n%s", got)
	}
}
