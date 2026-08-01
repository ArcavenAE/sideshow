// Package enable implements the per-repo activation verbs
// (aae-orc-d3nq.7): Enable materializes a plugin-shaped pack's
// binding set into one repo per the unshaping spec and records the
// exact removal set in the repo-bindings ledger; Disable replays that
// record in reverse. No harness plugin state of any kind is written
// on this channel (finding-094): registration is the repo settings
// chain, the identity lives only in sideshow's own ledger.
package enable

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/coexistcheck"
	"github.com/ArcavenAE/sideshow/internal/factoryguard"
	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/ledger"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// Options configures Enable and Disable.
type Options struct {
	RepoDir    string
	Pack       string
	Version    string // "" resolves the registered version
	Scope      bindings.RepoScope
	LedgerPath string // "" means ledger.Path()
	ConfigDir  string // "" means foreign.ConfigDir()
	// OverrideStaleLock permits proceeding past an EXPIRED factory
	// lock and the soft in-flight signals. Hard signals never pass.
	OverrideStaleLock bool
	// StoreRoot, Prefix, and BoundDir are resolved from the
	// registry/activation and the sideshow data dir when empty; tests
	// inject them.
	StoreRoot string
	Prefix    string
	BoundDir  string
	Now       time.Time
}

func (o *Options) normalize() error {
	if o.Scope == "" {
		o.Scope = bindings.ScopeLocal
	}
	if o.Scope != bindings.ScopeLocal && o.Scope != bindings.ScopeProject {
		return fmt.Errorf("unknown scope %q (want local or project); user scope is not a thing on this channel — a per-repo-required pack never activates machine-wide", o.Scope)
	}
	if o.LedgerPath == "" {
		o.LedgerPath = ledger.Path()
	}
	if o.ConfigDir == "" {
		o.ConfigDir = foreign.ConfigDir()
	}
	if o.Now.IsZero() {
		o.Now = time.Now().UTC()
	}
	abs, err := filepath.Abs(o.RepoDir)
	if err != nil {
		return fmt.Errorf("resolve repo dir: %w", err)
	}
	o.RepoDir = abs
	return nil
}

// settingsFile returns the repo settings path for the scope: local
// stays out of history (the ratified default), project is the
// committed file.
func settingsFile(repoDir string, scope bindings.RepoScope) string {
	name := "settings.local.json"
	if scope == bindings.ScopeProject {
		name = "settings.json"
	}
	return filepath.Join(repoDir, ".claude", name)
}

// Enable activates a pack in one repo. The pipeline: activation gate,
// coexist-check preflight, bound-variant render, materialization,
// compat symlink, env shim, hook-chain merge, verification, ledger
// row. Any failure rolls back everything already written.
func Enable(opts Options) error {
	if err := opts.normalize(); err != nil {
		return err
	}

	storeRoot, prefix, perRepoRequired, err := resolveStore(&opts)
	if err != nil {
		return err
	}
	if !perRepoRequired {
		fmt.Printf("note: %s does not declare per_repo_required; per-repo enable proceeds anyway\n", opts.Pack)
	}

	inv, err := bindings.DiscoverPluginLayout(pack.InstalledPack{Name: opts.Pack, Version: opts.Version, Path: storeRoot})
	if err != nil {
		return err
	}

	// Preflight: refuse on any ERROR; soft factory signals pass only
	// under the explicit override.
	report, err := coexistcheck.Run(coexistcheck.Options{
		RepoDir:         opts.RepoDir,
		Pack:            opts.Pack,
		Prefix:          prefix,
		StoreRoot:       storeRoot,
		Inventory:       inv,
		ConfigDir:       opts.ConfigDir,
		LedgerPath:      opts.LedgerPath,
		PerRepoRequired: perRepoRequired,
		Now:             opts.Now,
	})
	if err != nil {
		return err
	}
	guard := factoryguard.CheckRepo(opts.RepoDir, opts.Now)
	if guard.HardRefusal() {
		return fmt.Errorf("enable refused:\n%s", guard.Refusal())
	}
	if guard.InFlight() && !opts.OverrideStaleLock {
		return fmt.Errorf("enable refused (pass --override-stale-lock to proceed past these):\n%s", guard.Refusal())
	}
	if report.Refuse() {
		var lines []string
		for _, r := range report.Results {
			if r.Severity == foreign.Error && r.Check != 2 {
				lines = append(lines, fmt.Sprintf("[%d %s] %s", r.Check, r.Name, r.Detail))
			}
		}
		if len(lines) > 0 {
			return fmt.Errorf("enable refused by coexist-check:\n%s", strings.Join(lines, "\n"))
		}
	}

	platform, err := detectPlatform()
	if err != nil {
		return err
	}
	entries, err := hookEntries(storeRoot, platform)
	if err != nil {
		return err
	}
	if err := verifyDispatcher(storeRoot, platform); err != nil {
		return err
	}

	// Render the bound variant (transforms, .51) and materialize from
	// it (.48), then the compat symlink (.58).
	boundDir := opts.BoundDir
	if boundDir == "" {
		boundDir = bindings.BoundVariantDir(opts.Pack, filepath.Base(storeRoot))
	}
	boundInv, err := bindings.RenderBoundVariant(storeRoot, inv, opts.Pack, prefix, boundDir)
	if err != nil {
		return err
	}
	materializeFrom := boundDir

	target := bindings.RepoTarget{RepoDir: opts.RepoDir, Scope: opts.Scope}
	var artifacts []bindings.RepoArtifact
	rollback := func() {
		if _, rmErr := bindings.RemoveRepoArtifacts(target, artifacts); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: rollback: %v\n", rmErr)
		}
	}

	arts, err := bindings.MaterializeRepo(materializeFrom, boundInv, target)
	artifacts = append(artifacts, arts...)
	if err != nil {
		rollback()
		return err
	}
	compat, err := bindings.MaterializeCompatSymlink(storeRoot, target, opts.Pack)
	artifacts = append(artifacts, compat...)
	if err != nil {
		rollback()
		return err
	}

	settings := settingsFile(opts.RepoDir, opts.Scope)
	created, err := bindings.MergeEnvShim(settings, "CLAUDE_PLUGIN_ROOT", storeRoot)
	if err != nil {
		rollback()
		return err
	}
	unwindShim := func() {
		if _, rmErr := bindings.RemoveEnvShim(settings, "CLAUDE_PLUGIN_ROOT", storeRoot); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: rollback env shim: %v\n", rmErr)
		}
		if created {
			_ = os.Remove(settings)
		}
	}

	if _, err := bindings.MergeHookChain(settings, opts.Pack, entries); err != nil {
		unwindShim()
		rollback()
		return err
	}
	// Rule 4: refuse success if the post-write chain is short of the
	// declared event set.
	if err := bindings.VerifyHookChain(settings, opts.Pack, entries); err != nil {
		if _, rmErr := bindings.RemoveHookChain(settings, opts.Pack); rmErr != nil {
			fmt.Fprintf(os.Stderr, "warning: rollback hook chain: %v\n", rmErr)
		}
		unwindShim()
		rollback()
		return err
	}

	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		return err
	}
	row := ledger.Row{
		Version:       filepath.Base(storeRoot),
		StorePath:     storeRoot,
		Channel:       "sideshow-native",
		Platform:      platform,
		SettingsScope: string(opts.Scope),
		EnabledAt:     opts.Now.Format(time.RFC3339),
		Artifacts:     artifactStrings(artifacts, created),
		Selection:     "full",
	}
	if err := led.SetRow(opts.RepoDir, opts.Pack, row); err != nil {
		return err
	}
	if err := led.Save(opts.LedgerPath); err != nil {
		return err
	}

	fmt.Printf("enabled %s %s in %s (scope %s, platform %s): %d artifacts, %d hook events\n",
		opts.Pack, row.Version, opts.RepoDir, opts.Scope, platform, len(artifacts), len(entries))
	return nil
}

// Disable reverses an enable exactly: hook chain unmerge, env shim
// removal, artifact replay in reverse, ledger row deletion. The
// running-factory guard applies the same way; class-1 state is never
// touched.
func Disable(opts Options) error {
	if err := opts.normalize(); err != nil {
		return err
	}
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		return err
	}
	row := led.RepoRow(opts.RepoDir, opts.Pack)
	if row == nil {
		return fmt.Errorf("%s is not enabled in %s (no ledger row)", opts.Pack, opts.RepoDir)
	}

	guard := factoryguard.CheckRepo(opts.RepoDir, opts.Now)
	if guard.HardRefusal() {
		return fmt.Errorf("disable refused:\n%s", guard.Refusal())
	}
	if guard.InFlight() && !opts.OverrideStaleLock {
		return fmt.Errorf("disable refused (pass --override-stale-lock to proceed past these):\n%s", guard.Refusal())
	}

	settings := settingsFile(opts.RepoDir, bindings.RepoScope(row.SettingsScope))
	if _, err := bindings.RemoveHookChain(settings, opts.Pack); err != nil {
		return err
	}
	if _, err := bindings.RemoveEnvShim(settings, "CLAUDE_PLUGIN_ROOT", row.StorePath); err != nil {
		return err
	}

	arts, removeSettingsFile := parseArtifactStrings(row.Artifacts)
	target := bindings.RepoTarget{RepoDir: opts.RepoDir, Scope: bindings.RepoScope(row.SettingsScope)}
	removed, err := bindings.RemoveRepoArtifacts(target, arts)
	if err != nil {
		return err
	}
	if removeSettingsFile {
		if data, readErr := os.ReadFile(settings); readErr == nil && strings.TrimSpace(string(data)) == "{}" {
			_ = os.Remove(settings)
		}
	}

	led.DeleteRow(opts.RepoDir, opts.Pack)
	if err := led.Save(opts.LedgerPath); err != nil {
		return err
	}
	fmt.Printf("disabled %s in %s: %d artifacts removed\n", opts.Pack, opts.RepoDir, removed)
	return nil
}

// resolveStore fills StoreRoot/Prefix from the pack registry and
// activation contract when not injected.
func resolveStore(opts *Options) (storeRoot, prefix string, perRepoRequired bool, err error) {
	if opts.StoreRoot != "" {
		prefix = opts.Prefix
		if prefix == "" {
			prefix = opts.Pack
		}
		return opts.StoreRoot, prefix, true, nil
	}
	packs, err := pack.List()
	if err != nil {
		return "", "", false, err
	}
	for _, p := range packs {
		if p.Name != opts.Pack {
			continue
		}
		if opts.Version != "" && p.Version != opts.Version {
			continue
		}
		resolved, err := filepath.EvalSymlinks(p.Path)
		if err != nil {
			return "", "", false, fmt.Errorf("resolve store path: %w", err)
		}
		act, actErr := pack.LoadActivation(resolved)
		if actErr != nil {
			return "", "", false, fmt.Errorf("activation unreadable for %s: %w", opts.Pack, actErr)
		}
		opts.Version = p.Version
		return resolved, act.Prefix(opts.Pack), act != nil && act.PerRepoRequired, nil
	}
	return "", "", false, fmt.Errorf("pack %s%s is not installed", opts.Pack, versionSuffix(opts.Version))
}

func versionSuffix(v string) string {
	if v == "" {
		return ""
	}
	return "@" + v
}

// detectPlatform mirrors upstream's detect-platform contract: five
// canonical tuples.
func detectPlatform() (string, error) {
	key := runtime.GOOS + "/" + runtime.GOARCH
	m := map[string]string{
		"darwin/arm64":  "darwin-arm64",
		"darwin/amd64":  "darwin-x64",
		"linux/arm64":   "linux-arm64",
		"linux/amd64":   "linux-x64",
		"windows/amd64": "windows-x64",
	}
	p, ok := m[key]
	if !ok {
		return "", fmt.Errorf("no canonical platform tuple for %s (upstream supports darwin-arm64, darwin-x64, linux-arm64, linux-x64, windows-x64)", key)
	}
	return p, nil
}

// verifyDispatcher mirrors apply-platform.sh's binary check (exit
// codes 2/3): present and executable at the platform path.
func verifyDispatcher(storeRoot, platform string) error {
	name := "factory-dispatcher"
	if strings.HasPrefix(platform, "windows") {
		name += ".exe"
	}
	path := filepath.Join(storeRoot, "hooks", "dispatcher", "bin", platform, name)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("dispatcher binary missing at %s (zero-hooks trap: enable refuses rather than registering hooks that silently do nothing)", path)
	}
	if info.Mode().Perm()&0o100 == 0 {
		return fmt.Errorf("dispatcher %s is present but not executable", path)
	}
	return nil
}

// hookEntries derives the settings hook chain from the pack's
// platform template. Ratified D5: our channel wires all 12 events the
// pack registers — the template's 10 plus PreCompact/PostCompact,
// which upstream registers but never wired (the dead-hooks defect);
// on this channel they point at the same dispatcher. Docs distinguish
// "registered" from "verified firing".
func hookEntries(storeRoot, platform string) ([]bindings.HookEntry, error) {
	tmplPath := filepath.Join(storeRoot, "hooks", "hooks.json."+platform)
	data, err := os.ReadFile(tmplPath)
	if err != nil {
		return nil, fmt.Errorf("platform hook template: %w", err)
	}
	var tmpl struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
				Once    bool   `json:"once"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("parse %s: %w", tmplPath, err)
	}

	belt := bindings.InlineEnvBelt("CLAUDE_PLUGIN_ROOT", storeRoot)
	var entries []bindings.HookEntry
	var dispatcherCmd bindings.HookEntry
	for event, groups := range tmpl.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				cmd := strings.ReplaceAll(h.Command, "${CLAUDE_PLUGIN_ROOT}", storeRoot)
				e := bindings.HookEntry{Event: event, Command: belt + cmd, Timeout: h.Timeout, Once: h.Once}
				entries = append(entries, e)
				dispatcherCmd = e
			}
		}
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("platform template %s declares no hooks", tmplPath)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Event] = true
	}
	for _, extra := range []string{"PreCompact", "PostCompact"} {
		if !seen[extra] {
			entries = append(entries, bindings.HookEntry{
				Event: extra, Command: dispatcherCmd.Command, Timeout: dispatcherCmd.Timeout,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Event < entries[j].Event })
	return entries, nil
}

// artifactStrings encodes the removal set for the ledger row:
// kind-tagged strings plus the env-shim record (and whether the
// settings file itself was created by enable).
func artifactStrings(arts []bindings.RepoArtifact, settingsCreated bool) []string {
	out := make([]string, 0, len(arts)+1)
	for _, a := range arts {
		out = append(out, a.Kind+":"+a.Path)
	}
	if settingsCreated {
		out = append(out, "settings-file-created:")
	}
	return out
}

func parseArtifactStrings(rows []string) ([]bindings.RepoArtifact, bool) {
	var arts []bindings.RepoArtifact
	settingsCreated := false
	for _, s := range rows {
		kind, path, ok := strings.Cut(s, ":")
		if !ok {
			continue
		}
		if kind == "settings-file-created" {
			settingsCreated = true
			continue
		}
		arts = append(arts, bindings.RepoArtifact{Kind: kind, Path: path})
	}
	return arts, settingsCreated
}
