package bindings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a test helper that writes content to a path, creating
// parents as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSkillDirBinding_Kind(t *testing.T) {
	b := NewSkillDirBinding("bmad", "6.3.0", "/tmp/whatever")
	if b.Kind() != "skill-dir" {
		t.Errorf("Kind() = %q, want %q", b.Kind(), "skill-dir")
	}
}

func TestSkillDirBinding_Sync_CopiesTreeAndRewrites(t *testing.T) {
	packPath := t.TempDir()
	destRoot := t.TempDir()

	// Force HOME so claudeSkillsDir() lands under destRoot.
	t.Setenv("HOME", destRoot)

	// Build a fake pack with a skill dir containing a SKILL.md + a manifest + a nested file.
	skillBase := filepath.Join(packPath, ".claude", "skills", "bmad-agent-test")
	writeFile(t, filepath.Join(skillBase, "SKILL.md"),
		"# Test skill\nLoad {project-root}/_bmad/config.yaml\n")
	writeFile(t, filepath.Join(skillBase, "bmad-skill-manifest.yaml"),
		"schema_version: 1.0\nagent: test\n")
	writeFile(t, filepath.Join(skillBase, "nested", "helper.md"),
		"Refers to {project-root}/_bmad/helpers/util.md\n")
	writeFile(t, filepath.Join(skillBase, "data.bin"),
		"\x00\x01\x02binarycontent\x00") // should NOT be rewritten

	b := NewSkillDirBinding("bmad", "6.3.0", packPath)
	n, err := b.Sync()
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 1 {
		t.Errorf("Sync synced %d skills, want 1", n)
	}

	// SKILL.md: rewrite + footer.
	skillMD, err := os.ReadFile(filepath.Join(destRoot, ".claude", "skills", "bmad-agent-test", "SKILL.md"))
	if err != nil {
		t.Fatalf("read synced SKILL.md: %v", err)
	}
	skillStr := string(skillMD)
	if !strings.Contains(skillStr, packPath+"/config.yaml") {
		t.Errorf("SKILL.md rewrite did not substitute pack path:\n%s", skillStr)
	}
	if !strings.Contains(skillStr, "<!-- sideshow:fallback-resolution:begin -->") {
		t.Errorf("SKILL.md missing fallback footer:\n%s", skillStr)
	}

	// Manifest: rewritten (it's yaml) but NOT footer-amended (not SKILL.md).
	manifest, err := os.ReadFile(filepath.Join(destRoot, ".claude", "skills", "bmad-agent-test", "bmad-skill-manifest.yaml"))
	if err != nil {
		t.Fatalf("read synced manifest: %v", err)
	}
	if strings.Contains(string(manifest), "sideshow:fallback-resolution") {
		t.Errorf("manifest should not receive fallback footer:\n%s", manifest)
	}

	// Nested file: rewritten.
	nested, err := os.ReadFile(filepath.Join(destRoot, ".claude", "skills", "bmad-agent-test", "nested", "helper.md"))
	if err != nil {
		t.Fatalf("read synced nested: %v", err)
	}
	if !strings.Contains(string(nested), packPath+"/helpers/util.md") {
		t.Errorf("nested file rewrite did not substitute pack path:\n%s", nested)
	}

	// Binary file: passed through unchanged.
	bin, err := os.ReadFile(filepath.Join(destRoot, ".claude", "skills", "bmad-agent-test", "data.bin"))
	if err != nil {
		t.Fatalf("read synced binary: %v", err)
	}
	if string(bin) != "\x00\x01\x02binarycontent\x00" {
		t.Errorf("binary file content was altered: %q", bin)
	}
}

func TestHasSkillDirContent(t *testing.T) {
	packPath := t.TempDir()
	if hasSkillDirContent(packPath) {
		t.Errorf("hasSkillDirContent = true for empty pack")
	}

	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-agent-test", "SKILL.md"),
		"content")
	if !hasSkillDirContent(packPath) {
		t.Errorf("hasSkillDirContent = false for pack with skill dir")
	}
}

func TestCountSkillDirs(t *testing.T) {
	packPath := t.TempDir()
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-agent-a", "SKILL.md"), "a")
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-agent-b", "SKILL.md"), "b")
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-agent-c", "SKILL.md"), "c")
	got := countSkillDirs(packPath)
	if got != 3 {
		t.Errorf("countSkillDirs = %d, want 3", got)
	}
}

func TestSkillDirBinding_Validate_RequiresSkillDotMD(t *testing.T) {
	packPath := t.TempDir()
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-agent-good", "SKILL.md"), "ok")
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-agent-bad", "methods.csv"), "oops")

	b := NewSkillDirBinding("bmad", "6.3.0", packPath)
	err := b.Validate()
	if err == nil {
		t.Fatalf("Validate passed; expected error for missing SKILL.md")
	}
	if !strings.Contains(err.Error(), "bmad-agent-bad") {
		t.Errorf("error should name the offending skill: %v", err)
	}
}

func TestSkillDirBinding_Validate_EmptySkillsDir(t *testing.T) {
	packPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(packPath, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	b := NewSkillDirBinding("bmad", "6.3.0", packPath)
	err := b.Validate()
	if err == nil {
		t.Fatalf("Validate passed on empty skills dir; expected error")
	}
}

func TestCountSyncedSkills_OwnershipByCanonicalId(t *testing.T) {
	// HOME override isolates the test from the user's real ~/.claude/.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	// Pack ships skills with TWO prefixes (mirrors bmad 6.3.0: bmad-* + gds-*).
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-help", "SKILL.md"), "x")
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-party-mode", "SKILL.md"), "x")
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "gds-create-prd", "SKILL.md"), "x")
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "gds-validate-gdd", "SKILL.md"), "x")

	// Simulate a sync: every skill the pack ships is present at the target.
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	for _, id := range []string{"bmad-help", "bmad-party-mode", "gds-create-prd", "gds-validate-gdd"} {
		writeFile(t, filepath.Join(skillsDir, id, "SKILL.md"), "synced")
	}
	// Foreign skill present at target, NOT owned by this pack.
	writeFile(t, filepath.Join(skillsDir, "user-authored-skill", "SKILL.md"), "foreign")

	got, err := countSyncedSkills(packPath)
	if err != nil {
		t.Fatalf("countSyncedSkills: %v", err)
	}
	if got != 4 {
		t.Errorf("countSyncedSkills = %d, want 4 (multi-prefix bmad-* + gds-*, ignoring foreign)", got)
	}
}

func TestCountSyncedSkills_PartiallySynced(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	for _, id := range []string{"bmad-help", "bmad-customize", "bmad-shard-doc"} {
		writeFile(t, filepath.Join(packPath, ".claude", "skills", id, "SKILL.md"), "x")
	}
	// Only one of the three is in the target.
	writeFile(t, filepath.Join(homeDir, ".claude", "skills", "bmad-help", "SKILL.md"), "ok")

	got, err := countSyncedSkills(packPath)
	if err != nil {
		t.Fatalf("countSyncedSkills: %v", err)
	}
	if got != 1 {
		t.Errorf("countSyncedSkills = %d, want 1 (only bmad-help present at target)", got)
	}
}

func TestCountSyncedSkills_PackHasNoSkills(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir() // no .claude/skills/

	got, err := countSyncedSkills(packPath)
	if err != nil {
		t.Fatalf("countSyncedSkills: %v", err)
	}
	if got != 0 {
		t.Errorf("countSyncedSkills = %d, want 0", got)
	}
}

func TestCountSyncedSkills_TargetMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	packPath := t.TempDir()
	writeFile(t, filepath.Join(packPath, ".claude", "skills", "bmad-help", "SKILL.md"), "x")
	// homeDir/.claude/skills doesn't exist — no sync has run.

	got, err := countSyncedSkills(packPath)
	if err != nil {
		t.Fatalf("countSyncedSkills: %v", err)
	}
	if got != 0 {
		t.Errorf("countSyncedSkills = %d, want 0 (target dir absent)", got)
	}
}
