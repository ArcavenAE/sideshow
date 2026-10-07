package bindings

import (
	"path/filepath"
	"testing"
)

func TestBoundSkillPaths_ReadsOnlyThisPacksSkillDirs(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	skills := filepath.Join(t.TempDir(), "skills")
	err := saveManifest([]ManifestEntry{
		{Pack: "demo", Version: "1", Kind: "skill-dir", Path: filepath.Join(skills, "alpha")},
		{Pack: "demo", Version: "1", Kind: "markdown-command", Path: filepath.Join(skills, "beta")},
		{Pack: "other", Version: "1", Kind: "skill-dir", Path: filepath.Join(skills, "gamma")},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := BoundSkillPaths("demo")
	if err != nil {
		t.Fatalf("BoundSkillPaths: %v", err)
	}
	if len(got) != 1 || got["alpha"] != filepath.Join(skills, "alpha") {
		t.Errorf("BoundSkillPaths(demo) = %v, want only alpha", got)
	}
}

func TestBoundSkillPaths_MissingManifestIsEmpty(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	got, err := BoundSkillPaths("demo")
	if err != nil || len(got) != 0 {
		t.Errorf("BoundSkillPaths = %v, %v; want empty and no error", got, err)
	}
}
