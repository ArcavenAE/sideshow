package enable

import (
	"os"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/bindings"
)

// killedPassScene enables over a non-canonical settings file and returns
// what a disable pass leaves when it is killed after rewriting the file
// and before the restore: sideshow's entries are gone and the file is in
// canonical form, with the original-bytes record still in place.
func killedPassScene(t *testing.T, removeShim bool) (opts Options, settings string) {
	t.Helper()
	store := writeStore(t)
	repo := t.TempDir()
	settings = plantSettings(t, repo, bindings.ScopeLocal, nonCanonicalSettings)
	opts = baseOpts(t, repo, store)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if _, err := bindings.RemoveHookChain(settings, opts.Pack); err != nil {
		t.Fatal(err)
	}
	if removeShim {
		if _, err := bindings.RemoveEnvShim(settings, "CLAUDE_PLUGIN_ROOT", opts.StoreRoot); err != nil {
			t.Fatal(err)
		}
	}
	if len(sidecarFiles(t, opts)) != 1 {
		t.Fatal("the scene has no original-bytes record to survive the kill")
	}
	return opts, settings
}

// A rerun after a pass killed after the settings rewrite restores the
// original bytes: the file is exactly what sideshow's own writer renders
// from the recorded original (sideshow#178).
func TestDisable_RerunAfterAKilledPassRestoresTheOriginalBytes(t *testing.T) {
	for name, removeShim := range map[string]bool{
		"killed after the hook chain and the shim were removed": true,
		"killed between the hook chain and the shim removal":    false,
	} {
		t.Run(name, func(t *testing.T) {
			opts, settings := killedPassScene(t, removeShim)
			out := captureOut(t, func() {
				if err := Disable(opts); err != nil {
					t.Errorf("Disable: %v", err)
				}
			})
			got, err := os.ReadFile(settings)
			if err != nil || string(got) != nonCanonicalSettings {
				t.Errorf("original bytes not restored:\nwant %q\n got %q (%v)", nonCanonicalSettings, got, err)
			}
			if strings.Contains(out, "canonical form") {
				t.Errorf("the rerun printed the fallback note:\n%s", out)
			}
			if left := sidecarFiles(t, opts); len(left) != 0 {
				t.Errorf("record left after the rerun: %v", left)
			}
		})
	}
}

// A file whose content matches the original but whose bytes are not what
// sideshow's writer renders (a hand edit that only reformats) is not
// overwritten. This is the case that reverted the first attempt in #175.
func TestDisable_RerunDoesNotOverwriteAHandReformattedFile(t *testing.T) {
	for name, bytesNow := range map[string]string{
		"four-space indent": "{\n    \"permissions\": {\n        \"allow\": [\"Bash(ls:*)\"]\n    },\n    \"env\": {\n        \"TEAM_VAR\": \"shared\"\n    },\n" +
			"    \"hooks\": {\n        \"PreToolUse\": [{\"matcher\": \"Bash\", \"hooks\": [{\"type\": \"command\", \"command\": \"echo team-guard\"}]}]\n    }\n}\n",
		"tab indent":          "{\n\t\"env\": {\"TEAM_VAR\": \"shared\"},\n\t\"permissions\": {\"allow\": [\"Bash(ls:*)\"]},\n\t\"hooks\": {\"PreToolUse\": [{\"matcher\": \"Bash\", \"hooks\": [{\"type\": \"command\", \"command\": \"echo team-guard\"}]}]}\n}\n",
		"no trailing newline": "",
	} {
		t.Run(name, func(t *testing.T) {
			opts, settings := killedPassScene(t, true)
			if bytesNow == "" {
				data, err := os.ReadFile(settings)
				if err != nil {
					t.Fatal(err)
				}
				bytesNow = strings.TrimSuffix(string(data), "\n")
			}
			if err := os.WriteFile(settings, []byte(bytesNow), 0o644); err != nil {
				t.Fatal(err)
			}
			out := captureOut(t, func() { _ = Disable(opts) })
			got, _ := os.ReadFile(settings)
			if string(got) != bytesNow {
				t.Errorf("a hand edit was overwritten:\n%s", got)
			}
			if !strings.Contains(out, "changed since enable") {
				t.Errorf("missing the note:\n%s", out)
			}
		})
	}
}
