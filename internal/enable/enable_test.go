package enable

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// writeStore builds a plugin-shaped store version root with skills,
// nested agents, an executable dispatcher for the current platform,
// and the platform hook template (10 upstream events, once on the
// Session pair).
func writeStore(t *testing.T) string {
	t.Helper()
	store := filepath.Join(t.TempDir(), "1.0.0-rc.23")
	platform, err := detectPlatform()
	if err != nil {
		t.Skipf("no canonical platform tuple here: %v", err)
	}

	mustWrite := func(rel, content string, mode os.FileMode) {
		t.Helper()
		p := filepath.Join(store, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite(".claude-plugin/plugin.json", `{"name": "vsdd-factory", "version": "1.0.0-rc.23"}`, 0o644)
	mustWrite("skills/jira/SKILL.md", "---\nname: jira\n---\n\nRun /vsdd-factory:github-ops.\n", 0o644)
	mustWrite("agents/github-ops.md", "---\nname: github-ops\n---\n\nbody\n", 0o644)
	mustWrite("hooks/dispatcher/bin/"+platform+"/factory-dispatcher", "#!/bin/sh\nexit 0\n", 0o755)

	tmpl := map[string]any{"hooks": map[string]any{}}
	hooks := tmpl["hooks"].(map[string]any)
	for _, ev := range []string{"PreToolUse", "PostToolUse", "SessionStart", "SessionEnd", "Stop"} {
		h := map[string]any{
			"type":    "command",
			"command": "${CLAUDE_PLUGIN_ROOT}/hooks/dispatcher/bin/" + platform + "/factory-dispatcher",
			"timeout": 10000,
		}
		if ev == "SessionStart" || ev == "SessionEnd" {
			h["once"] = true
		}
		hooks[ev] = []any{map[string]any{"hooks": []any{h}}}
	}
	data, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite("hooks/hooks.json."+platform, string(data), 0o644)
	return store
}

// writeRepo builds a consumer repo with pre-existing user content
// including a second pack's managed hook groups.
func writeRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	settings := `{
  "env": {"USER_KEY": "kept"},
  "hooks": {
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "bmad-session"}], "_managed_by": "sideshow:bmad"},
      {"hooks": [{"type": "command", "command": "user-own-hook"}]}
    ]
  }
}`
	// Normalize the settings fixture through the same rendering the
	// merge/unmerge writer uses, so the byte diff tests semantic
	// restoration rather than JSON formatting.
	var parsed map[string]any
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		t.Fatal(err)
	}
	normalized, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	settings = string(normalized) + "\n"

	for rel, content := range map[string]string{
		".claude/settings.local.json":  settings,
		".claude/skills/mine/SKILL.md": "# mine\n",
		"README.md":                    "user repo\n",
	} {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			snap[rel] = "link -> " + target
		case info.IsDir():
			snap[rel] = "dir"
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snap[rel] = fmt.Sprintf("file %x", sha256.Sum256(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func baseOpts(t *testing.T, repo, store string) Options {
	t.Helper()
	return Options{
		RepoDir:    repo,
		Pack:       "vsdd-factory",
		Version:    "1.0.0-rc.23",
		StoreRoot:  store,
		Prefix:     "vsdd",
		BoundDir:   filepath.Join(t.TempDir(), "bound"),
		LedgerPath: filepath.Join(t.TempDir(), "repo-bindings.yaml"),
		ConfigDir:  t.TempDir(),
		Now:        time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
}

// The .7 done criterion: enable and disable are exact inverses,
// settings byte diff included, with a second pack present throughout.
func TestEnableDisable_ExactInverse(t *testing.T) {
	t.Parallel()
	store := writeStore(t)
	repo := writeRepo(t)
	opts := baseOpts(t, repo, store)

	before := snapshot(t, repo)
	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// Enabled state sanity: prefixed skill bound, shim + hooks merged,
	// ledger row present.
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "skills", "vsdd-jira")); err != nil {
		t.Errorf("bound skill missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "plugins", "vsdd-factory")); err != nil {
		t.Errorf("compat symlink missing: %v", err)
	}
	settings, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(settings)
	for _, want := range []string{"CLAUDE_PLUGIN_ROOT", "PreCompact", "PostCompact", `"once": true`, "sideshow:vsdd-factory", "sideshow:bmad", "user-own-hook"} {
		if !strings.Contains(s, want) {
			t.Errorf("enabled settings missing %q", want)
		}
	}
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	row := led.RepoRow(repo, "vsdd-factory")
	if row == nil || row.Version != "1.0.0-rc.23" || row.Channel != "sideshow-native" {
		t.Fatalf("ledger row = %+v", row)
	}
	if row.StorePath != store {
		t.Errorf("ledger store_path = %s, want the pinned version dir", row.StorePath)
	}

	if err := Disable(opts); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	after := snapshot(t, repo)
	var diffs []string
	for k, v := range after {
		if bv, ok := before[k]; !ok || bv != v {
			diffs = append(diffs, "added/changed "+k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			diffs = append(diffs, "removed "+k)
		}
	}
	if len(diffs) > 0 {
		t.Errorf("repo not restored exactly:\n  %s", strings.Join(diffs, "\n  "))
	}
	led, err = ledger.Load(opts.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	if led.RepoRow(repo, "vsdd-factory") != nil {
		t.Error("ledger row survived disable")
	}
}

func TestEnable_RefusesForeignDualEnable(t *testing.T) {
	t.Parallel()
	store := writeStore(t)
	repo := writeRepo(t)
	opts := baseOpts(t, repo, store)
	for rel, content := range map[string]string{
		"plugins/config.json": `{"version": 2, "repositories": {"claude-mp": {"plugins": {"vsdd-factory": [{"scope": "user", "installPath": "/nonexistent", "version": "1.0.0-rc.23"}]}}}}`,
		"settings.json":       `{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`,
	} {
		p := filepath.Join(opts.ConfigDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	before := snapshot(t, repo)
	err := Enable(opts)
	if err == nil || !strings.Contains(err.Error(), "coexist-check") {
		t.Fatalf("Enable = %v, want coexist-check refusal", err)
	}
	after := snapshot(t, repo)
	if len(before) != len(after) {
		t.Error("refused enable still wrote into the repo")
	}
}

func TestEnable_RefusesUnexpiredFactoryLock(t *testing.T) {
	t.Parallel()
	store := writeStore(t)
	repo := writeRepo(t)
	opts := baseOpts(t, repo, store)
	lock := "---\nfactory_lock:\n  holder: dev@example.com\n  locked_at: " +
		opts.Now.Add(-time.Minute).Format(time.RFC3339) + "\n  expires_at: " +
		opts.Now.Add(40*time.Minute).Format(time.RFC3339) + "\n---\n"
	p := filepath.Join(repo, ".factory", "STATE.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Enable(opts)
	if err == nil || !strings.Contains(err.Error(), "dev@example.com") {
		t.Fatalf("Enable = %v, want hard lock refusal naming the holder", err)
	}
	// The override must NOT pass a hard signal.
	opts.OverrideStaleLock = true
	if err := Enable(opts); err == nil {
		t.Fatal("override passed an unexpired lock; hard signals never pass")
	}
}

func TestEnable_MissingDispatcherRefuses(t *testing.T) {
	t.Parallel()
	store := writeStore(t)
	platform, _ := detectPlatform()
	if err := os.RemoveAll(filepath.Join(store, "hooks", "dispatcher")); err != nil {
		t.Fatal(err)
	}
	_ = platform
	opts := baseOpts(t, t.TempDir(), store)

	err := Enable(opts)
	if err == nil || !strings.Contains(err.Error(), "zero-hooks trap") {
		t.Fatalf("Enable = %v, want dispatcher refusal", err)
	}
}

func TestHookEntries_WiresAllTwelveEvents(t *testing.T) {
	t.Parallel()
	store := writeStore(t)
	platform, err := detectPlatform()
	if err != nil {
		t.Skip(err)
	}
	entries, err := hookEntries(store, platform)
	if err != nil {
		t.Fatal(err)
	}
	events := map[string]bool{}
	for _, e := range entries {
		events[e.Event] = true
		if e.Timeout != 10000 {
			t.Errorf("%s timeout = %d, want 10000", e.Event, e.Timeout)
		}
		if !strings.HasPrefix(e.Command, "CLAUDE_PLUGIN_ROOT='") {
			t.Errorf("%s command missing inline env belt: %s", e.Event, e.Command)
		}
		if strings.Contains(e.Command, "${CLAUDE_PLUGIN_ROOT}") {
			t.Errorf("%s command not resolved: %s", e.Event, e.Command)
		}
	}
	// D5: the template's events plus the registered-but-dead pair.
	for _, ev := range []string{"PreToolUse", "SessionStart", "SessionEnd", "PreCompact", "PostCompact"} {
		if !events[ev] {
			t.Errorf("event %s not wired", ev)
		}
	}
}

// writeMidWaveFactory plants the class-1 durable state a repo running a
// factory carries mid-wave: the worktree and its run state, the pack's
// customization and output trees, and a STORY wave checkout. Returns a
// snapshot of exactly that state.
func writeMidWaveFactory(t *testing.T, repo string) map[string]string {
	t.Helper()
	for rel, content := range map[string]string{
		".factory/STATE.md":                   "# wave 3 in flight\n",
		".factory/wave-state.yaml":            "wave: 3\nstatus: running\n",
		".factory/logs/burst-014.log":         "burst 14 output\n",
		".factory-project/config.toml":        "[project]\nname = \"x\"\n",
		".worktrees/STORY-14/src/main.go":     "package main\n",
		"_vsdd-factory-custom/weave.yaml":     "schema_version: 0.1.0\n",
		"_vsdd-factory-custom/skills/mine.md": "# my skill\n",
		"_vsdd-factory-output/report-014.md":  "# results\n",
		"HANDOFF.md":                          "# handoff\n",
	} {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	snap := map[string]string{}
	for _, dir := range []string{".factory", ".factory-project", ".worktrees", "_vsdd-factory-custom", "_vsdd-factory-output"} {
		for rel, hash := range snapshot(t, filepath.Join(repo, dir)) {
			snap[dir+"/"+rel] = hash
		}
	}
	return snap
}

// TestEnableDisable_MidWaveFactorySurvives is the aae-orc-d3nq.42
// acceptance criterion: class 1 durable user state is bit-identical
// across a full enable/disable cycle. Sideshow has no uninstall verb,
// so disable's ledger replay is the removal path that runs inside a
// user's repo, and it is the one that has to be safe.
func TestEnableDisable_MidWaveFactorySurvives(t *testing.T) {
	t.Parallel()
	store := writeStore(t)
	repo := writeRepo(t)
	opts := baseOpts(t, repo, store)

	classOne := writeMidWaveFactory(t, repo)
	if len(classOne) == 0 {
		t.Fatal("fixture planted no class 1 state")
	}

	// A mid-wave factory makes the running-factory guard refuse, which
	// is correct and is tested elsewhere. The case worth pinning here is
	// the one past it: an operator who knowingly overrides still must not
	// lose class 1 state. The guard is policy; the floor is the property
	// that holds when policy has been overridden.
	opts.OverrideStaleLock = true

	if err := Enable(opts); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if diff := diffSnapshots(classOne, snapshotClassOne(t, repo)); diff != "" {
		t.Fatalf("enable disturbed class 1 state:\n%s", diff)
	}

	if err := Disable(opts); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if diff := diffSnapshots(classOne, snapshotClassOne(t, repo)); diff != "" {
		t.Fatalf("disable disturbed class 1 state:\n%s", diff)
	}
}

func snapshotClassOne(t *testing.T, repo string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	for _, dir := range []string{".factory", ".factory-project", ".worktrees", "_vsdd-factory-custom", "_vsdd-factory-output"} {
		for rel, hash := range snapshot(t, filepath.Join(repo, dir)) {
			snap[dir+"/"+rel] = hash
		}
	}
	return snap
}

func diffSnapshots(want, got map[string]string) string {
	var lines []string
	for rel, w := range want {
		g, ok := got[rel]
		switch {
		case !ok:
			lines = append(lines, "REMOVED: "+rel)
		case g != w:
			lines = append(lines, "CHANGED: "+rel)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			lines = append(lines, "ADDED: "+rel)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
