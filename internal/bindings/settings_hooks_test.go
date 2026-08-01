package bindings

import (
	"os"
	"strings"
	"testing"
)

var vsddEntries = []HookEntry{
	{Event: "SessionStart", Command: "CLAUDE_PLUGIN_ROOT='/store/v1' /store/v1/hooks/dispatcher/d session-start", Timeout: 10000, Once: true},
	{Event: "PreToolUse", Command: "CLAUDE_PLUGIN_ROOT='/store/v1' /store/v1/hooks/dispatcher/d pre-tool-use", Timeout: 10000},
	{Event: "SessionEnd", Command: "CLAUDE_PLUGIN_ROOT='/store/v1' /store/v1/hooks/dispatcher/d session-end", Timeout: 10000, Once: true},
}

// The .52 done criterion: merge then unmerge is a settings-file byte
// diff of zero, with a second pack's hooks and the user's own hooks
// present and undisturbed throughout.
func TestHookChain_MergeUnmergeByteExact(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, `{
	  "env": {"USER_KEY": "kept"},
	  "hooks": {
	    "SessionStart": [
	      {"matcher": "", "hooks": [{"type": "command", "command": "bmad-session"}], "_managed_by": "sideshow:bmad"},
	      {"hooks": [{"type": "command", "command": "user-own-hook"}]}
	    ],
	    "PostToolUse": [
	      {"hooks": [{"type": "command", "command": "beads-export"}], "_managed_by": "sideshow:beads-config"}
	    ]
	  }
	}`)

	// Normalize once through the writer so the byte comparison is
	// against a stable rendering, then snapshot.
	if _, err := MergeHookChain(path, "vsdd-factory", nil); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	added, err := MergeHookChain(path, "vsdd-factory", vsddEntries)
	if err != nil || added != 3 {
		t.Fatalf("MergeHookChain = (%d, %v), want (3, nil)", added, err)
	}
	if err := VerifyHookChain(path, "vsdd-factory", vsddEntries); err != nil {
		t.Fatalf("VerifyHookChain after merge: %v", err)
	}

	removed, err := RemoveHookChain(path, "vsdd-factory")
	if err != nil || removed != 3 {
		t.Fatalf("RemoveHookChain = (%d, %v), want (3, nil)", removed, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("settings not byte-identical after unmerge:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(string(after), "sideshow:bmad") || !strings.Contains(string(after), "beads-config") || !strings.Contains(string(after), "user-own-hook") {
		t.Error("second pack or user hooks disturbed")
	}
}

func TestMergeHookChain_SchemaShape(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, "")
	if _, err := MergeHookChain(path, "vsdd-factory", vsddEntries); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"matcher"`) {
		t.Error("matcher key emitted; upstream groups carry only a hooks key (T19: omitted ≡ \"\")")
	}
	if !strings.Contains(s, `"timeout": 10000`) {
		t.Error("timeout not emitted")
	}
	if !strings.Contains(s, `"once": true`) {
		t.Error("once not emitted")
	}
	if !strings.Contains(s, `"_managed_by": "sideshow:vsdd-factory"`) {
		t.Error("ownership marker missing")
	}
}

// Re-merge converges: enabling twice (or after a version flip) does
// not duplicate groups.
func TestMergeHookChain_UpsertConverges(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, "")
	if _, err := MergeHookChain(path, "vsdd-factory", vsddEntries); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MergeHookChain(path, "vsdd-factory", vsddEntries); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("re-merge not convergent:\n%s\nvs\n%s", first, second)
	}
}

func TestVerifyHookChain_ReportsShortfall(t *testing.T) {
	t.Parallel()
	path := settingsFixture(t, "")
	if _, err := MergeHookChain(path, "vsdd-factory", vsddEntries[:1]); err != nil {
		t.Fatal(err)
	}
	err := VerifyHookChain(path, "vsdd-factory", vsddEntries)
	if err == nil || !strings.Contains(err.Error(), "PreToolUse") {
		t.Errorf("VerifyHookChain = %v, want shortfall naming PreToolUse", err)
	}
}

func TestRemoveHookChain_MissingFileAndForeignOnly(t *testing.T) {
	t.Parallel()
	if n, err := RemoveHookChain(settingsFixture(t, ""), "vsdd-factory"); err != nil || n != 0 {
		t.Errorf("missing file: RemoveHookChain = (%d, %v), want (0, nil)", n, err)
	}
	path := settingsFixture(t, `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "user"}]}]}}`)
	before, _ := os.ReadFile(path)
	if n, err := RemoveHookChain(path, "vsdd-factory"); err != nil || n != 0 {
		t.Errorf("foreign-only: RemoveHookChain = (%d, %v), want (0, nil)", n, err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Error("no-op removal rewrote the file")
	}
}
