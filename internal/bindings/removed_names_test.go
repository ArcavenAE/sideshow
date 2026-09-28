package bindings

import (
	"path/filepath"
	"reflect"
	"testing"
)

// aae-orc-86yp6: a version flip must name what stopped resolving, not
// only count it.
func TestRunSync_ReturnsRemovedEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SIDESHOW_HOME", "")

	v1 := makeSkillPack(t, "bmad-alpha", "bmad-beta")
	if _, _, err := runSync([]Binding{NewSkillDirBinding("bmad", "1.0.0", v1)}); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	v2 := makeSkillPack(t, "bmad-beta")
	_, removed, err := runSync([]Binding{NewSkillDirBinding("bmad", "2.0.0", v2)})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed %d entries, want 1: %+v", len(removed), removed)
	}
	got := removed[0]
	if filepath.Base(got.Path) != "bmad-alpha" || got.Pack != "bmad" || got.Version != "1.0.0" || got.Kind != "skill-dir" {
		t.Errorf("removed entry = %+v, want bmad-alpha from bmad 1.0.0 (skill-dir)", got)
	}
}

func TestFormatRemoved(t *testing.T) {
	entries := []ManifestEntry{
		{Pack: "bmad", Version: "6.11.0", Kind: "skill-dir", Path: "/h/.claude/skills/bmad-validate-prd"},
		{Pack: "bmad", Version: "6.11.0", Kind: "markdown-command", Path: "/h/.claude/commands/bmad-help.md"},
		{Pack: "bmad", Version: "6.11.0", Kind: "skill-dir", Path: "/h/.claude/skills/bmad-create-prd"},
	}
	want := []string{
		"  bmad-create-prd (skill-dir, from bmad 6.11.0)",
		"  bmad-help (markdown-command, from bmad 6.11.0)",
		"  bmad-validate-prd (skill-dir, from bmad 6.11.0)",
	}
	if got := FormatRemoved(entries); !reflect.DeepEqual(got, want) {
		t.Errorf("FormatRemoved =\n%q\nwant\n%q", got, want)
	}
	if got := FormatRemoved(nil); len(got) != 0 {
		t.Errorf("FormatRemoved(nil) = %q, want empty", got)
	}
}
