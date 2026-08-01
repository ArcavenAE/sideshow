package adopt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ArcavenAE/sideshow/internal/ledger"
)

func platform(t *testing.T) string {
	t.Helper()
	m := map[string]string{
		"darwin/arm64": "darwin-arm64", "darwin/amd64": "darwin-x64",
		"linux/arm64": "linux-arm64", "linux/amd64": "linux-x64",
		"windows/amd64": "windows-x64",
	}
	p, ok := m[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		t.Skipf("no canonical platform tuple for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return p
}

func mustWrite(t *testing.T, root, rel, content string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// writeTree lays down the plugin-shaped content used for BOTH the
// sideshow store and the foreign cache tree, so E1/E4 compare
// byte-identical trees.
func writeTree(t *testing.T, root, plat string) {
	t.Helper()
	mustWrite(t, root, ".claude-plugin/plugin.json", `{"name": "vsdd-factory", "version": "1.0.0-rc.23"}`, 0o644)
	mustWrite(t, root, "skills/jira/SKILL.md", "---\nname: jira\n---\n\nRun /vsdd-factory:github-ops.\n", 0o644)
	mustWrite(t, root, "agents/github-ops.md", "---\nname: github-ops\n---\n\nbody\n", 0o644)
	mustWrite(t, root, "hooks/dispatcher/bin/"+plat+"/factory-dispatcher", "#!/bin/sh\nexit 0\n", 0o755)

	tmpl := map[string]any{"hooks": map[string]any{}}
	hooks := tmpl["hooks"].(map[string]any)
	for _, ev := range []string{"PreToolUse", "SessionStart", "SessionEnd", "Stop"} {
		h := map[string]any{
			"type":    "command",
			"command": "${CLAUDE_PLUGIN_ROOT}/hooks/dispatcher/bin/" + plat + "/factory-dispatcher",
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
	mustWrite(t, root, "hooks/hooks.json."+plat, string(data), 0o644)
}

// fixture stands up the full adoption scene: a store, a foreign cache
// tree with an install record, and a repo where the foreign identity
// is enabled at the given scope ("project" writes the repo's
// settings.json; "user" writes the config dir's settings.json).
func fixture(t *testing.T, enableScope string) (opts Options, repo, cache string) {
	t.Helper()
	plat := platform(t)

	store := filepath.Join(t.TempDir(), "1.0.0-rc.23")
	writeTree(t, store, plat)
	cache = filepath.Join(t.TempDir(), "cache", "vsdd-factory")
	writeTree(t, cache, plat)

	configDir := t.TempDir()
	reg := fmt.Sprintf(`{"version": 2, "plugins": {"vsdd-factory@claude-mp": [
	  {"scope": %q, "installPath": %q, "version": "1.0.0-rc.23", "gitCommitSha": "abc123"}
	]}}`, enableScope, cache)
	mustWrite(t, configDir, "plugins/installed_plugins.json", reg, 0o644)

	repo = t.TempDir()
	enableJSON := `{"enabledPlugins": {"vsdd-factory@claude-mp": true}}`
	switch enableScope {
	case "user":
		mustWrite(t, configDir, "settings.json", enableJSON, 0o644)
	default:
		mustWrite(t, repo, ".claude/settings.json", enableJSON, 0o644)
	}

	opts = Options{
		RepoDir: repo, Pack: "vsdd-factory",
		LedgerPath: filepath.Join(t.TempDir(), "repo-bindings.yaml"),
		ConfigDir:  configDir,
		StoreRoot:  store, Prefix: "vsdd",
		BoundDir: filepath.Join(t.TempDir(), "bound"),
		Now:      time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}
	return opts, repo, cache
}

func readLocalSettings(t *testing.T, repo string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAdopt_EndToEnd(t *testing.T) {
	opts, repo, _ := fixture(t, "project")
	// A foreign-form default agent, to exercise the consented flip.
	mustWrite(t, repo, ".claude/settings.local.json", `{"agent": "vsdd-factory:orchestrator:orchestrator"}`, 0o644)
	opts.RewriteAgent = true

	out, err := Adopt(opts)
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if !out.Suppressed || out.Identity != "vsdd-factory@claude-mp" {
		t.Errorf("outcome = %+v, want suppression of vsdd-factory@claude-mp", out)
	}
	if !out.AgentRewritten {
		t.Error("agent not rewritten despite consent flag")
	}
	if len(out.FootprintDrift) != 0 {
		t.Errorf("machine-level harness state changed: %v", out.FootprintDrift)
	}

	joined := strings.Join(out.Equivalence, "\n")
	for _, want := range []string{"E1 content parity: PASS", "E4 dispatcher identity: PASS"} {
		if !strings.Contains(joined, want) {
			t.Errorf("equivalence report missing %q:\n%s", want, joined)
		}
	}

	local := readLocalSettings(t, repo)
	enables, _ := local["enabledPlugins"].(map[string]any)
	if v, ok := enables["vsdd-factory@claude-mp"].(bool); !ok || v {
		t.Errorf("suppression override missing or true: %v", local)
	}
	if agent, _ := local["agent"].(string); agent != "vsdd-orchestrator" {
		t.Errorf("agent = %q, want vsdd-orchestrator", agent)
	}

	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	row := led.RepoRow(repo, "vsdd-factory")
	if row == nil || row.Version != "1.0.0-rc.23" {
		t.Fatalf("ledger row = %+v, want version 1.0.0-rc.23", row)
	}

	// Finish is print-only; it must not error against real residue.
	if err := Finish(opts); err != nil {
		t.Errorf("Finish: %v", err)
	}
}

func TestAdopt_NothingToAdopt(t *testing.T) {
	opts, _, _ := fixture(t, "project")
	// Remove the enable: install record exists, but nothing dispatches.
	if err := os.Remove(filepath.Join(opts.RepoDir, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	_, err := Adopt(opts)
	if err == nil || !strings.Contains(err.Error(), "nothing to adopt") {
		t.Fatalf("Adopt = %v, want nothing-to-adopt refusal", err)
	}
}

func TestAdopt_UserScopeEnableRefuses(t *testing.T) {
	opts, repo, _ := fixture(t, "user")
	before := snapshotDir(t, repo)
	_, err := Adopt(opts)
	if err == nil || !strings.Contains(err.Error(), "USER scope") {
		t.Fatalf("Adopt = %v, want user-scope refusal", err)
	}
	if diff := diffSnapshots(before, snapshotDir(t, repo)); len(diff) != 0 {
		t.Errorf("refusal wrote into the repo: %v", diff)
	}
}

func TestAdopt_VersionDriftRefuses(t *testing.T) {
	opts, _, _ := fixture(t, "project")
	opts.Version = "2.0.0"
	_, err := Adopt(opts)
	if err == nil || !strings.Contains(err.Error(), "--allow-version-change") {
		t.Fatalf("Adopt = %v, want version-drift refusal", err)
	}
}

func TestAdopt_OrphanedEnableRefuses(t *testing.T) {
	opts, _, _ := fixture(t, "project")
	// Drop the install registry: the enable survives with no record.
	if err := os.Remove(filepath.Join(opts.ConfigDir, "plugins", "installed_plugins.json")); err != nil {
		t.Fatal(err)
	}
	_, err := Adopt(opts)
	if err == nil || !strings.Contains(err.Error(), "orphaned") {
		t.Fatalf("Adopt = %v, want orphaned-enable refusal", err)
	}
}

func TestAdopt_DryRunWritesNothing(t *testing.T) {
	opts, repo, _ := fixture(t, "project")
	opts.DryRun = true
	before := snapshotDir(t, repo)

	out, err := Adopt(opts)
	if err != nil {
		t.Fatalf("Adopt dry run: %v", err)
	}
	if out.Suppressed {
		t.Error("dry run reported suppression")
	}
	if diff := diffSnapshots(before, snapshotDir(t, repo)); len(diff) != 0 {
		t.Errorf("dry run wrote into the repo: %v", diff)
	}
	if _, err := os.Stat(opts.LedgerPath); !os.IsNotExist(err) {
		t.Errorf("dry run created a ledger: %v", err)
	}
}

func TestAdopt_DryRunPredictsPreflightRefusal(t *testing.T) {
	opts, repo, _ := fixture(t, "project")
	opts.DryRun = true
	// An unexpired factory lock is a hazard the real run refuses on;
	// the dry run must predict it (D1 from the .23 pilot report).
	lock := "---\nfactory_lock:\n  holder: dev@example.com\n  locked_at: " +
		opts.Now.Add(-10*time.Minute).Format(time.RFC3339) +
		"\n  expires_at: " + opts.Now.Add(30*time.Minute).Format(time.RFC3339) +
		"\n---\n\n# Factory State\n"
	mustWrite(t, repo, ".factory/STATE.md", lock, 0o644)

	_, err := Adopt(opts)
	if err == nil || !strings.Contains(err.Error(), "the real run would REFUSE") {
		t.Fatalf("Adopt dry run = %v, want predicted refusal", err)
	}
}

func TestAdopt_RollsBackSuppressionOnEnableFailure(t *testing.T) {
	opts, repo, _ := fixture(t, "project")
	// Break the store so enable fails after suppression is written.
	if err := os.RemoveAll(filepath.Join(opts.StoreRoot, "hooks")); err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, repo)

	_, err := Adopt(opts)
	if err == nil || !strings.Contains(err.Error(), "suppression rolled back") {
		t.Fatalf("Adopt = %v, want enable failure with rollback", err)
	}
	if diff := diffSnapshots(before, snapshotDir(t, repo)); len(diff) != 0 {
		t.Errorf("repo not restored after rollback: %v", diff)
	}
}

// TestAdopt_RollbackRestoresAPriorLocalEnable covers the repo shape a
// per-repo enable leaves behind: the identity is enabled TRUE in the
// same file and key adopt's suppression writes. Rollback must restore
// that true, not delete the key. Deleting it turns the foreign channel
// off in a repo the operator never converted.
//
// This is the shape the user-scope migration produces, and the shape
// any hand-written local enable already had.
func TestAdopt_RollbackRestoresAPriorLocalEnable(t *testing.T) {
	opts, repo, _ := fixture(t, "project")
	// The only enable is a local-scope true, as after a migration.
	if err := os.Remove(filepath.Join(repo, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, repo, ".claude/settings.local.json",
		"{\n  \"enabledPlugins\": {\n    \"vsdd-factory@claude-mp\": true\n  }\n}\n", 0o644)

	// Break the store so enable fails after suppression is written.
	if err := os.RemoveAll(filepath.Join(opts.StoreRoot, "hooks")); err != nil {
		t.Fatal(err)
	}
	before := snapshotDir(t, repo)

	if _, err := Adopt(opts); err == nil {
		t.Fatal("Adopt succeeded against a broken store")
	}
	if diff := diffSnapshots(before, snapshotDir(t, repo)); len(diff) != 0 {
		t.Errorf("repo not restored after rollback: %v", diff)
	}
	enables, _ := readLocalSettings(t, repo)["enabledPlugins"].(map[string]any)
	if v, ok := enables["vsdd-factory@claude-mp"].(bool); !ok || !v {
		t.Errorf("prior local enable not restored: %v", enables)
	}
}

func snapshotDir(t *testing.T, dir string) map[string]string {
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
		if d.IsDir() {
			snap[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snap[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func diffSnapshots(a, b map[string]string) []string {
	var out []string
	for k, v := range a {
		if b[k] != v {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// storeWith points SIDESHOW_HOME at a registry holding exactly the
// given pack versions, so the store gate resolves against a real
// registry instead of the injected StoreRoot seam.
func storeWith(t *testing.T, versions ...string) {
	t.Helper()
	home := t.TempDir()
	var b strings.Builder
	b.WriteString("packs:\n")
	for _, v := range versions {
		fmt.Fprintf(&b, "  - name: vsdd-factory\n    version: %q\n    path: %q\n",
			v, filepath.Join(home, "packs", "vsdd-factory", v))
	}
	mustWrite(t, home, "registry.yaml", b.String(), 0o644)
	t.Setenv("SIDESHOW_HOME", home)
}

// TestAdopt_DefaultVersionAbsentFromStoreRefuses is the regression
// guard for sideshow#96. The default path sets adoptVersion to the
// foreign tree's running version, so the equality gate compares that
// value against itself and never fires. Nothing then confirmed the
// store actually held it, and enable failed at step 2 after step 1 had
// written.
func TestAdopt_DefaultVersionAbsentFromStoreRefuses(t *testing.T) {
	opts, repo, _ := fixture(t, "project")
	// The real seam: no injected StoreRoot, and a store that holds a
	// different version than the foreign tree runs (rc.23).
	opts.StoreRoot = ""
	storeWith(t, "1.0.0-rc.99")

	_, err := Adopt(opts)
	if err == nil {
		t.Fatal("adopt proceeded with a version the store does not hold")
	}
	for _, want := range []string{"1.0.0-rc.23", "1.0.0-rc.99", "--allow-version-change"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not name %q: %v", want, err)
		}
	}

	// Refused BEFORE step 1: the repo is exactly as found.
	if _, statErr := os.Stat(filepath.Join(repo, ".claude", "settings.local.json")); !os.IsNotExist(statErr) {
		t.Error("step 1 wrote a suppression override before the refusal")
	}
}

// TestAdopt_DryRunPredictsStoreRefusal is the other half: a dry run
// must validate every step of the plan it prints, not just the
// preconditions of step one. Before the fix it printed step 2 and
// then reported "the real run would proceed".
func TestAdopt_DryRunPredictsStoreRefusal(t *testing.T) {
	opts, _, _ := fixture(t, "project")
	opts.StoreRoot = ""
	opts.DryRun = true
	storeWith(t, "1.0.0-rc.99")

	_, err := Adopt(opts)
	if err == nil {
		t.Fatal("dry run predicted success for a plan whose step 2 cannot resolve")
	}
	if !strings.Contains(err.Error(), "would REFUSE") {
		t.Errorf("dry run did not report a refusal: %v", err)
	}
}

// TestAdopt_DefaultVersionPresentInStoreProceeds pins the other side,
// so the gate cannot be satisfied by refusing everything.
func TestAdopt_DefaultVersionPresentInStoreProceeds(t *testing.T) {
	opts, _, _ := fixture(t, "project")
	opts.StoreRoot = ""
	opts.DryRun = true
	storeWith(t, "1.0.0-rc.23")

	if _, err := Adopt(opts); err != nil {
		t.Fatalf("dry run refused a version the store holds: %v", err)
	}
}
