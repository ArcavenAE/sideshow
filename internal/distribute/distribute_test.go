package distribute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArcavenAE/sideshow/internal/project"
)

func setupPackRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create a rules source file
	rulesDir := filepath.Join(root, "distribute", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "task-workflow.md"),
		[]byte("# Task Workflow\n\nUse bd for tracking.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a claude_md source file
	claudeDir := filepath.Join(root, "distribute", "claude-md")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "beads-section.md"),
		[]byte("## Beads Issue Tracker\n\nUse `bd ready` to find work.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return root
}

func defaultOpts(packRoot string) Options {
	return Options{
		PackName:    "testpack",
		PackVersion: "1.0.0",
		PackRoot:    packRoot,
	}
}

// --- Rules tests ---

func TestDistributeRule_CreatesNew(t *testing.T) {
	t.Parallel()
	packRoot := setupPackRoot(t)
	repoRoot := t.TempDir()

	repo := project.Subrepo{Name: "testrepo", AbsPath: repoRoot, Present: true}
	rule := RuleArtifact{
		Source: "distribute/rules/task-workflow.md",
		Target: ".claude/rules/task-workflow.md",
	}

	action := distributeRule(repoRoot, rule, defaultOpts(packRoot))
	if action.Status != "wrote" {
		t.Errorf("status = %q, want wrote", action.Status)
	}
	_ = repo // used for context

	// Verify file exists with marker
	data, err := os.ReadFile(filepath.Join(repoRoot, ".claude", "rules", "task-workflow.md"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if !strings.HasPrefix(string(data), markerPrefix) {
		t.Error("file missing sideshow marker")
	}
	if !strings.Contains(string(data), "Task Workflow") {
		t.Error("file missing source content")
	}
}

func TestDistributeRule_SkipsUserAuthored(t *testing.T) {
	t.Parallel()
	packRoot := setupPackRoot(t)
	repoRoot := t.TempDir()

	// Create a user-authored file (no marker)
	rulesDir := filepath.Join(repoRoot, ".claude", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "task-workflow.md"),
		[]byte("# My custom workflow\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rule := RuleArtifact{
		Source: "distribute/rules/task-workflow.md",
		Target: ".claude/rules/task-workflow.md",
	}

	action := distributeRule(repoRoot, rule, defaultOpts(packRoot))
	if action.Status != "skipped" {
		t.Errorf("status = %q, want skipped", action.Status)
	}

	// Original content should be unchanged
	data, _ := os.ReadFile(filepath.Join(rulesDir, "task-workflow.md"))
	if string(data) != "# My custom workflow\n" {
		t.Error("user-authored file was modified")
	}
}

func TestDistributeRule_UpdatesMarkedFile(t *testing.T) {
	t.Parallel()
	packRoot := setupPackRoot(t)
	repoRoot := t.TempDir()

	// Create an existing sideshow-managed file
	rulesDir := filepath.Join(repoRoot, ".claude", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "task-workflow.md"),
		[]byte("<!-- managed by sideshow:testpack:0.9.0 -->\n# Old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rule := RuleArtifact{
		Source: "distribute/rules/task-workflow.md",
		Target: ".claude/rules/task-workflow.md",
	}

	action := distributeRule(repoRoot, rule, defaultOpts(packRoot))
	if action.Status != "wrote" {
		t.Errorf("status = %q, want wrote", action.Status)
	}

	// Should have new content
	data, _ := os.ReadFile(filepath.Join(rulesDir, "task-workflow.md"))
	if strings.Contains(string(data), "Old content") {
		t.Error("old content still present after update")
	}
	if !strings.Contains(string(data), "Task Workflow") {
		t.Error("new content not present")
	}
}

// --- Hook tests ---

func TestDistributeHook_CreatesNew(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	hook := HookArtifact{Event: "SessionStart", Command: "bd prime"}
	action := distributeHook(repoRoot, hook, defaultOpts(t.TempDir()))
	if action.Status != "merged" {
		t.Errorf("status = %q, want merged", action.Status)
	}

	// Verify settings.json uses Claude Code's nested format
	data, err := os.ReadFile(filepath.Join(repoRoot, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	hooks := settings["hooks"].(map[string]any)
	ruleGroups := hooks["SessionStart"].([]any)
	if len(ruleGroups) != 1 {
		t.Fatalf("expected 1 rule group, got %d", len(ruleGroups))
	}

	group := ruleGroups[0].(map[string]any)
	if group["_managed_by"] != "sideshow:testpack" {
		t.Errorf("_managed_by = %q, want sideshow:testpack", group["_managed_by"])
	}
	if group["matcher"] != "" {
		t.Errorf("matcher = %q, want empty", group["matcher"])
	}

	innerHooks := group["hooks"].([]any)
	if len(innerHooks) != 1 {
		t.Fatalf("expected 1 inner hook, got %d", len(innerHooks))
	}

	entry := innerHooks[0].(map[string]any)
	if entry["command"] != "bd prime" {
		t.Errorf("command = %q, want bd prime", entry["command"])
	}
	if entry["type"] != "command" {
		t.Errorf("type = %q, want command", entry["type"])
	}
}

func TestDistributeHook_MergesExisting(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	// Create existing settings with a different hook (Claude Code nested format)
	claudeDir := filepath.Join(repoRoot, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "git-safety-check",
						},
					},
				},
			},
		},
		"permissions": map[string]any{
			"allow": []any{"Read(~/work/)"},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	hook := HookArtifact{Event: "SessionStart", Command: "bd prime"}
	action := distributeHook(repoRoot, hook, defaultOpts(t.TempDir()))
	if action.Status != "merged" {
		t.Errorf("status = %q, want merged", action.Status)
	}

	// Read back and verify both events exist
	data, _ = os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	hooks := settings["hooks"].(map[string]any)

	// Original PreToolUse hook preserved
	preToolUse := hooks["PreToolUse"].([]any)
	if len(preToolUse) != 1 {
		t.Errorf("PreToolUse rule groups = %d, want 1", len(preToolUse))
	}

	// New SessionStart hook added
	sessionStart := hooks["SessionStart"].([]any)
	if len(sessionStart) != 1 {
		t.Errorf("SessionStart rule groups = %d, want 1", len(sessionStart))
	}

	// Permissions preserved
	perms := settings["permissions"].(map[string]any)
	allow := perms["allow"].([]any)
	if len(allow) != 1 {
		t.Errorf("permissions.allow = %d, want 1", len(allow))
	}
}

func TestDistributeHook_SkipsDuplicate_NestedFormat(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	// Existing settings in Claude Code's nested format
	claudeDir := filepath.Join(repoRoot, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "bd prime",
						},
					},
					"_managed_by": "sideshow:testpack",
				},
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	hook := HookArtifact{Event: "SessionStart", Command: "bd prime"}
	action := distributeHook(repoRoot, hook, defaultOpts(t.TempDir()))
	if action.Status != "skipped" {
		t.Errorf("status = %q, want skipped", action.Status)
	}
}

func TestDistributeHook_BadJSON(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	claudeDir := filepath.Join(repoRoot, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"),
		[]byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}

	hook := HookArtifact{Event: "SessionStart", Command: "bd prime"}
	action := distributeHook(repoRoot, hook, defaultOpts(t.TempDir()))
	if action.Status != "error" {
		t.Errorf("status = %q, want error", action.Status)
	}
}

// --- CLAUDE.md tests ---

func TestDistributeClaudeMD_AppendsNew(t *testing.T) {
	t.Parallel()
	packRoot := setupPackRoot(t)
	repoRoot := t.TempDir()

	// Create existing CLAUDE.md
	if err := os.WriteFile(filepath.Join(repoRoot, "CLAUDE.md"),
		[]byte("# My Project\n\nExisting content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	section := ClaudeMDArtifact{ID: "beads-tracker", Source: "distribute/claude-md/beads-section.md"}
	action := distributeClaudeMD(repoRoot, section, defaultOpts(packRoot))
	if action.Status != "wrote" {
		t.Errorf("status = %q, want wrote", action.Status)
	}

	data, _ := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md"))
	content := string(data)

	// Original content preserved
	if !strings.Contains(content, "Existing content.") {
		t.Error("original content lost")
	}
	// Markers present
	if !strings.Contains(content, sectionBegin("beads-tracker")) {
		t.Error("begin marker missing")
	}
	if !strings.Contains(content, sectionEnd("beads-tracker")) {
		t.Error("end marker missing")
	}
	// Injected content present
	if !strings.Contains(content, "Beads Issue Tracker") {
		t.Error("injected content missing")
	}
}

func TestDistributeClaudeMD_ReplacesExisting(t *testing.T) {
	t.Parallel()
	packRoot := setupPackRoot(t)
	repoRoot := t.TempDir()

	// Create CLAUDE.md with existing markers
	existing := "# Project\n\n" +
		sectionBegin("beads-tracker") + "\n" +
		"OLD CONTENT\n" +
		sectionEnd("beads-tracker") + "\n" +
		"\n## Other Stuff\n"

	if err := os.WriteFile(filepath.Join(repoRoot, "CLAUDE.md"),
		[]byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	section := ClaudeMDArtifact{ID: "beads-tracker", Source: "distribute/claude-md/beads-section.md"}
	action := distributeClaudeMD(repoRoot, section, defaultOpts(packRoot))
	if action.Status != "wrote" {
		t.Errorf("status = %q, want wrote", action.Status)
	}

	data, _ := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md"))
	content := string(data)

	// Old content replaced
	if strings.Contains(content, "OLD CONTENT") {
		t.Error("old content still present")
	}
	// New content present
	if !strings.Contains(content, "Beads Issue Tracker") {
		t.Error("new content missing")
	}
	// Content outside markers preserved
	if !strings.Contains(content, "Other Stuff") {
		t.Error("content outside markers was modified")
	}
}

func TestDistributeClaudeMD_CreatesFile(t *testing.T) {
	t.Parallel()
	packRoot := setupPackRoot(t)
	repoRoot := t.TempDir()

	section := ClaudeMDArtifact{ID: "beads-tracker", Source: "distribute/claude-md/beads-section.md"}
	action := distributeClaudeMD(repoRoot, section, defaultOpts(packRoot))
	if action.Status != "wrote" {
		t.Errorf("status = %q, want wrote", action.Status)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	if !strings.Contains(string(data), "Beads Issue Tracker") {
		t.Error("content missing from new CLAUDE.md")
	}
}

// --- Symlink tests ---

func TestDistributeSymlink_CreatesNew(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	link := SymlinkArtifact{Path: ".beads", Target: "../.beads"}
	action := distributeSymlink(repoRoot, link, defaultOpts(t.TempDir()))
	if action.Status != "wrote" {
		t.Errorf("status = %q, want wrote", action.Status)
	}

	target, err := os.Readlink(filepath.Join(repoRoot, ".beads"))
	if err != nil {
		t.Fatalf("Readlink error: %v", err)
	}
	if target != "../.beads" {
		t.Errorf("symlink target = %q, want ../.beads", target)
	}
}

func TestDistributeSymlink_SkipsCorrect(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	// Create existing correct symlink
	if err := os.Symlink("../.beads", filepath.Join(repoRoot, ".beads")); err != nil {
		t.Fatal(err)
	}

	link := SymlinkArtifact{Path: ".beads", Target: "../.beads"}
	action := distributeSymlink(repoRoot, link, defaultOpts(t.TempDir()))
	if action.Status != "skipped" {
		t.Errorf("status = %q, want skipped", action.Status)
	}
}

func TestDistributeSymlink_ConflictWrongTarget(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	// Create symlink pointing elsewhere
	if err := os.Symlink("/somewhere/else", filepath.Join(repoRoot, ".beads")); err != nil {
		t.Fatal(err)
	}

	link := SymlinkArtifact{Path: ".beads", Target: "../.beads"}
	action := distributeSymlink(repoRoot, link, defaultOpts(t.TempDir()))
	if action.Status != "conflict" {
		t.Errorf("status = %q, want conflict", action.Status)
	}
}

func TestDistributeSymlink_ConflictRegularFile(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	// Create a regular file at the symlink path
	if err := os.WriteFile(filepath.Join(repoRoot, ".beads"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := SymlinkArtifact{Path: ".beads", Target: "../.beads"}
	action := distributeSymlink(repoRoot, link, defaultOpts(t.TempDir()))
	if action.Status != "conflict" {
		t.Errorf("status = %q, want conflict", action.Status)
	}
}

// --- Gitignore tests ---

func TestDistributeGitignore_AppendsNew(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(repoRoot, ".gitignore"),
		[]byte("node_modules/\n.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	action := distributeGitignore(repoRoot, ".beads", defaultOpts(t.TempDir()))
	if action.Status != "wrote" {
		t.Errorf("status = %q, want wrote", action.Status)
	}

	data, _ := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	content := string(data)
	if !strings.Contains(content, "node_modules/") {
		t.Error("existing content lost")
	}
	if !strings.Contains(content, ".beads") {
		t.Error(".beads not appended")
	}
}

func TestDistributeGitignore_SkipsExisting(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	if err := os.WriteFile(filepath.Join(repoRoot, ".gitignore"),
		[]byte(".beads\nnode_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	action := distributeGitignore(repoRoot, ".beads", defaultOpts(t.TempDir()))
	if action.Status != "skipped" {
		t.Errorf("status = %q, want skipped", action.Status)
	}
}

func TestDistributeGitignore_CreatesFile(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	action := distributeGitignore(repoRoot, ".beads", defaultOpts(t.TempDir()))
	if action.Status != "wrote" {
		t.Errorf("status = %q, want wrote", action.Status)
	}

	data, err := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore not created: %v", err)
	}
	if !strings.Contains(string(data), ".beads") {
		t.Error(".beads missing from new .gitignore")
	}
}

// --- DryRun tests ---

func TestDryRun_NoWrites(t *testing.T) {
	t.Parallel()
	packRoot := setupPackRoot(t)
	repoRoot := t.TempDir()

	manifest := &Manifest{
		Rules: []RuleArtifact{
			{Source: "distribute/rules/task-workflow.md", Target: ".claude/rules/task-workflow.md"},
		},
		Hooks: []HookArtifact{
			{Event: "SessionStart", Command: "bd prime"},
		},
		ClaudeMD: []ClaudeMDArtifact{
			{ID: "beads-tracker", Source: "distribute/claude-md/beads-section.md"},
		},
		Symlinks: []SymlinkArtifact{
			{Path: ".beads", Target: "../.beads"},
		},
		Gitignore: []string{".beads"},
	}

	repo := project.Subrepo{Name: "testrepo", AbsPath: repoRoot, Present: true}
	opts := defaultOpts(packRoot)
	opts.DryRun = true

	result := ToRepo(repo, manifest, opts)

	// No files should have been created
	entries, _ := os.ReadDir(repoRoot)
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("dry run created files: %v", names)
	}

	// But all actions should report what would happen
	if len(result.Actions) != 5 {
		t.Errorf("expected 5 actions, got %d", len(result.Actions))
	}
}

// --- ToRepo integration test ---

func TestToRepo_SkipsAbsentRepo(t *testing.T) {
	t.Parallel()
	repo := project.Subrepo{Name: "missing", AbsPath: "/nonexistent", Present: false}
	manifest := &Manifest{
		Gitignore: []string{".beads"},
	}

	result := ToRepo(repo, manifest, defaultOpts(t.TempDir()))
	if !result.Skipped {
		t.Error("expected Skipped=true for absent repo")
	}
	if len(result.Actions) != 0 {
		t.Errorf("expected 0 actions for absent repo, got %d", len(result.Actions))
	}
}

// --- Manifest tests ---

func TestManifest_IsEmpty(t *testing.T) {
	t.Parallel()

	empty := Manifest{}
	if !empty.IsEmpty() {
		t.Error("empty manifest should report IsEmpty()")
	}

	withRules := Manifest{Rules: []RuleArtifact{{Source: "a", Target: "b"}}}
	if withRules.IsEmpty() {
		t.Error("manifest with rules should not be empty")
	}
}

// --- Custom bridge tests ---

func bmadBridge() CustomBridgeArtifact {
	return CustomBridgeArtifact{
		UpstreamPath: "_bmad/custom",
		PerRepoDir:   "_bmad-custom",
	}
}

func TestCustomBridge_FreshRepo(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	actions := distributeCustomBridge(repoRoot, bmadBridge(), defaultOpts(t.TempDir()))
	if len(actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(actions))
	}
	for _, a := range actions {
		if a.Status != "wrote" {
			t.Errorf("action %s/%s status = %q (%s), want wrote", a.Type, a.Path, a.Status, a.Detail)
		}
	}

	// Per-repo dir exists with .gitkeep
	if _, err := os.Stat(filepath.Join(repoRoot, "_bmad-custom", ".gitkeep")); err != nil {
		t.Errorf("_bmad-custom/.gitkeep missing: %v", err)
	}

	// Symlink resolves: a write through _bmad/custom/ lands in _bmad-custom/
	throughLink := filepath.Join(repoRoot, "_bmad", "custom", "agents.toml")
	if err := os.WriteFile(throughLink, []byte("x = 1\n"), 0o644); err != nil {
		t.Fatalf("write through bridge symlink: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "_bmad-custom", "agents.toml")); err != nil {
		t.Errorf("write through symlink did not land in _bmad-custom: %v", err)
	}

	// Gitignore carries the shim dir
	gi, err := os.ReadFile(filepath.Join(repoRoot, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gi), "/_bmad/") {
		t.Errorf(".gitignore missing /_bmad/ line, got %q", string(gi))
	}
}

func TestCustomBridge_Idempotent(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	opts := defaultOpts(t.TempDir())

	distributeCustomBridge(repoRoot, bmadBridge(), opts)
	second := distributeCustomBridge(repoRoot, bmadBridge(), opts)

	for _, a := range second {
		if a.Status != "skipped" {
			t.Errorf("second run action %s/%s status = %q (%s), want skipped", a.Type, a.Path, a.Status, a.Detail)
		}
	}
}

func TestCustomBridge_PreservesExistingCustomization(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	// Pre-existing customization content
	customDir := filepath.Join(repoRoot, "_bmad-custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "agents.toml"), []byte("keep = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	actions := distributeCustomBridge(repoRoot, bmadBridge(), defaultOpts(t.TempDir()))
	if actions[0].Status != "skipped" {
		t.Errorf("per-repo dir action status = %q, want skipped", actions[0].Status)
	}

	data, err := os.ReadFile(filepath.Join(customDir, "agents.toml"))
	if err != nil || string(data) != "keep = true\n" {
		t.Errorf("existing customization modified: %q, %v", string(data), err)
	}
	if _, err := os.Stat(filepath.Join(customDir, ".gitkeep")); !os.IsNotExist(err) {
		t.Error(".gitkeep should not be added to a pre-existing customization dir")
	}
}

func TestCustomBridge_ConflictAtSymlinkPath(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()

	// A real directory occupies the bridge path (e.g. upstream installer
	// materialized _bmad/custom/ before sideshow ran).
	if err := os.MkdirAll(filepath.Join(repoRoot, "_bmad", "custom"), 0o755); err != nil {
		t.Fatal(err)
	}

	actions := distributeCustomBridge(repoRoot, bmadBridge(), defaultOpts(t.TempDir()))
	var symlinkAction *Action
	for i := range actions {
		if actions[i].Type == "symlink" {
			symlinkAction = &actions[i]
		}
	}
	if symlinkAction == nil {
		t.Fatal("no symlink action emitted")
	}
	if symlinkAction.Status != "conflict" {
		t.Errorf("symlink action status = %q (%s), want conflict", symlinkAction.Status, symlinkAction.Detail)
	}
}

func TestCustomBridge_DryRun(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	opts := defaultOpts(t.TempDir())
	opts.DryRun = true

	actions := distributeCustomBridge(repoRoot, bmadBridge(), opts)
	for _, a := range actions {
		if a.Status != "wrote" {
			t.Errorf("dry-run action %s/%s status = %q, want wrote", a.Type, a.Path, a.Status)
		}
	}

	// Nothing on disk
	for _, p := range []string{"_bmad", "_bmad-custom", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(repoRoot, p)); !os.IsNotExist(err) {
			t.Errorf("dry-run created %s", p)
		}
	}
}

func TestCustomBridge_InvalidPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		bridge CustomBridgeArtifact
	}{
		{"absolute upstream", CustomBridgeArtifact{UpstreamPath: "/etc/bmad/custom", PerRepoDir: "_bmad-custom"}},
		{"absolute per-repo", CustomBridgeArtifact{UpstreamPath: "_bmad/custom", PerRepoDir: "/tmp/custom"}},
		{"escaping upstream", CustomBridgeArtifact{UpstreamPath: "../outside/custom", PerRepoDir: "_bmad-custom"}},
		{"escaping per-repo", CustomBridgeArtifact{UpstreamPath: "_bmad/custom", PerRepoDir: "../outside"}},
		{"no shim parent", CustomBridgeArtifact{UpstreamPath: "custom", PerRepoDir: "_bmad-custom"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			actions := distributeCustomBridge(repoRoot, tc.bridge, defaultOpts(t.TempDir()))
			if len(actions) != 1 || actions[0].Status != "error" {
				t.Errorf("actions = %+v, want single error action", actions)
			}
		})
	}
}

func TestLoadPackYAML_CustomBridge(t *testing.T) {
	t.Parallel()
	packRoot := t.TempDir()
	yaml := `name: bmad
version: 6.4.0
distribute:
  custom_bridge:
    upstream_path: _bmad/custom
    per_repo_dir: _bmad-custom
`
	if err := os.WriteFile(filepath.Join(packRoot, "pack.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPackYAML(packRoot)
	if err != nil {
		t.Fatalf("LoadPackYAML: %v", err)
	}
	if p.Distribute.CustomBridge == nil {
		t.Fatal("CustomBridge not parsed")
	}
	if p.Distribute.CustomBridge.UpstreamPath != "_bmad/custom" ||
		p.Distribute.CustomBridge.PerRepoDir != "_bmad-custom" {
		t.Errorf("CustomBridge = %+v", *p.Distribute.CustomBridge)
	}
	if p.Distribute.IsEmpty() {
		t.Error("manifest with custom_bridge should not be IsEmpty")
	}
}

func TestCustomBridge_SeedsPackCustomTemplate(t *testing.T) {
	t.Parallel()
	repoRoot := t.TempDir()
	packRoot := t.TempDir()

	// Pack ships a custom/ template (bmad 6.4+ four-file scaffold shape).
	tplDir := filepath.Join(packRoot, "custom")
	if err := os.MkdirAll(tplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"config.toml":      "[agents]\n",
		"config.user.toml": "# user overrides\n",
	} {
		if err := os.WriteFile(filepath.Join(tplDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	opts := defaultOpts(packRoot)
	actions := distributeCustomBridge(repoRoot, bmadBridge(), opts)
	if actions[0].Status != "wrote" {
		t.Fatalf("dir action = %q (%s), want wrote", actions[0].Status, actions[0].Detail)
	}
	if !strings.Contains(actions[0].Detail, "seeded") {
		t.Errorf("dir action detail = %q, want seeded mention", actions[0].Detail)
	}

	// Template files landed; no .gitkeep when seeded.
	if _, err := os.Stat(filepath.Join(repoRoot, "_bmad-custom", "config.toml")); err != nil {
		t.Errorf("seeded config.toml missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "_bmad-custom", ".gitkeep")); !os.IsNotExist(err) {
		t.Error(".gitkeep should not be written when template seeds the dir")
	}

	// Existing customization is still never touched on a second pass.
	second := distributeCustomBridge(repoRoot, bmadBridge(), opts)
	if second[0].Status != "skipped" {
		t.Errorf("second pass dir action = %q, want skipped", second[0].Status)
	}
}

// --- Runtime links tests (aae-orc-rpnd; sideshow#52) ---

func testRuntimeLinks() []RuntimeLink {
	return []RuntimeLink{
		{Link: "scripts", Target: "scripts"},
		{Link: "_config", Target: "_config"},
		{Link: "config.toml", Target: "config.toml"},
		{Link: "config.user.toml", Target: "config.user.toml"},
	}
}

// seedFakeStore creates a fake user store with a current symlink so link
// targets resolve, returning the store pack dir. SIDESHOW_HOME scoping
// keeps pack.PacksDir() inside the test sandbox.
func seedFakeStore(t *testing.T, packName string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SIDESHOW_HOME", home)
	versionDir := filepath.Join(home, "packs", packName, "9.9.9")
	for _, d := range []string{"scripts", "_config"} {
		if err := os.MkdirAll(filepath.Join(versionDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"config.toml", "config.user.toml"} {
		if err := os.WriteFile(filepath.Join(versionDir, f), []byte("# t\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("9.9.9", filepath.Join(home, "packs", packName, "current")); err != nil {
		t.Fatal(err)
	}
	return versionDir
}

func TestRuntimeLinks_FreshRepoCreatesAllThroughCurrent(t *testing.T) {
	seedFakeStore(t, "bmad")
	repoRoot := t.TempDir()
	opts := Options{PackName: "bmad", PackVersion: "9.9.9", PackRoot: t.TempDir()}

	actions := distributeRuntimeLinks(repoRoot, "_bmad", testRuntimeLinks(), opts)
	for _, a := range actions {
		if a.Status != "wrote" {
			t.Errorf("%s: status = %q (%s), want wrote", a.Path, a.Status, a.Detail)
		}
	}
	// Links exist, resolve, and point through current (not the version dir).
	target, err := os.Readlink(filepath.Join(repoRoot, "_bmad", "scripts"))
	if err != nil {
		t.Fatalf("scripts link missing: %v", err)
	}
	if !strings.Contains(target, "current") {
		t.Errorf("link target %q must route through current", target)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "_bmad", "config.toml")); err != nil {
		t.Errorf("config.toml link does not resolve: %v", err)
	}
}

func TestRuntimeLinks_Idempotent(t *testing.T) {
	seedFakeStore(t, "bmad")
	repoRoot := t.TempDir()
	opts := Options{PackName: "bmad", PackVersion: "9.9.9", PackRoot: t.TempDir()}

	distributeRuntimeLinks(repoRoot, "_bmad", testRuntimeLinks(), opts)
	second := distributeRuntimeLinks(repoRoot, "_bmad", testRuntimeLinks(), opts)
	for _, a := range second {
		if a.Status != "skipped" {
			t.Errorf("re-run %s: status = %q, want skipped", a.Path, a.Status)
		}
	}
}

func TestRuntimeLinks_RefusesConflict(t *testing.T) {
	seedFakeStore(t, "bmad")
	repoRoot := t.TempDir()
	// A real directory occupies _bmad/scripts (e.g. a genuine per-project install).
	if err := os.MkdirAll(filepath.Join(repoRoot, "_bmad", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := Options{PackName: "bmad", PackVersion: "9.9.9", PackRoot: t.TempDir()}

	actions := distributeRuntimeLinks(repoRoot, "_bmad", testRuntimeLinks()[:1], opts)
	if actions[0].Status != "conflict" {
		t.Errorf("status = %q (%s), want conflict", actions[0].Status, actions[0].Detail)
	}
	// The real dir is untouched.
	if fi, err := os.Lstat(filepath.Join(repoRoot, "_bmad", "scripts")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Error("pre-existing real directory was replaced")
	}
}

func TestRuntimeLinks_RefusesDanglingTarget(t *testing.T) {
	seedFakeStore(t, "bmad")
	repoRoot := t.TempDir()
	opts := Options{PackName: "bmad", PackVersion: "9.9.9", PackRoot: t.TempDir()}

	actions := distributeRuntimeLinks(repoRoot, "_bmad",
		[]RuntimeLink{{Link: "nope", Target: "does-not-exist"}}, opts)
	if actions[0].Status != "error" {
		t.Errorf("status = %q (%s), want error for dangling target", actions[0].Status, actions[0].Detail)
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, "_bmad", "nope")); !os.IsNotExist(err) {
		t.Error("dangling link was created — worse than none (Sam's clause)")
	}
}

func TestRuntimeLinks_NoDeclarationNoOp(t *testing.T) {
	t.Setenv("SIDESHOW_HOME", t.TempDir())
	repoRoot := t.TempDir()
	repo := project.Subrepo{Name: "r", AbsPath: repoRoot, Present: true}
	m := &Manifest{Gitignore: []string{"/x/"}}

	result := ToRepo(repo, m, Options{PackName: "bmad", PackRoot: t.TempDir()})
	for _, a := range result.Actions {
		if a.Type == "runtime_link" {
			t.Errorf("runtime_link action emitted without declaration: %+v", a)
		}
	}
	if _, err := os.Lstat(filepath.Join(repoRoot, "_bmad")); err == nil {
		if entries, _ := os.ReadDir(filepath.Join(repoRoot, "_bmad")); len(entries) > 0 {
			t.Error("_bmad populated without runtime_links declaration")
		}
	}
}
