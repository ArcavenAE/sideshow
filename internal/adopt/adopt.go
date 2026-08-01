// Package adopt converts a repo running the foreign (claude-mp)
// channel of a plugin-shaped pack onto sideshow-native repo bindings
// (aae-orc-d3nq.22). The conversion is per-repo and reversible:
// suppress the foreign identity in THIS repo (a repo-side settings
// override; the foreign install itself is never touched), enable the
// sideshow channel at the version the foreign tree was actually
// running, then prove equivalence against the foreign install tree.
//
// Ordering is load-bearing: suppression precedes enable so the
// coexistence preflight's same-repo-double-dispatch check sees the
// foreign identity already silenced.
//
// Machine-level retirement (uninstalling the foreign channel, purging
// its cache, removing the marketplace) is deliberately NOT performed
// here. Finish reports the residue and prints the operator commands;
// executing them is the operator's act — the foreign census stays
// read-only.
package adopt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/coexistcheck"
	"github.com/ArcavenAE/sideshow/internal/enable"
	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/ledger"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// Options configures an adoption.
type Options struct {
	RepoDir    string
	Pack       string
	Version    string // "" adopts at the foreign tree's running version
	Scope      bindings.RepoScope
	LedgerPath string
	ConfigDir  string
	// OverrideStaleLock passes through to enable's factory guard.
	OverrideStaleLock bool
	// AllowVersionChange permits adopting at a version other than the
	// one the foreign tree is running. Without it, drift refuses:
	// equivalence is only provable at version equality.
	AllowVersionChange bool
	// RewriteAgent consents to flipping a foreign-form default agent
	// (<pack>:...) to the bound orchestrator. Off by default; adopt
	// never rewrites a persona choice without this flag.
	RewriteAgent bool
	// DryRun prints the plan and writes nothing.
	DryRun bool
	// StoreRoot, Prefix, BoundDir inject test fixtures (same seam as
	// enable.Options).
	StoreRoot string
	Prefix    string
	BoundDir  string
	Now       time.Time
}

// Outcome reports what an adoption did, for the caller and for tests.
type Outcome struct {
	Identity       string // foreign identity suppressed
	Version        string // version enabled on the sideshow channel
	Suppressed     bool
	AgentRewritten bool
	// Equivalence lines are the E1-E4 report (docs: honest
	// equivalence; E3 is never provable headless).
	Equivalence []string
	// FootprintDrift is non-empty if machine-level harness state
	// changed during adoption. It must be empty: adopt only writes
	// repo-side files.
	FootprintDrift []string
}

func (o *Options) normalize() error {
	if o.ConfigDir == "" {
		o.ConfigDir = foreign.ConfigDir()
	}
	if o.LedgerPath == "" {
		o.LedgerPath = ledger.Path()
	}
	if o.Scope == "" {
		o.Scope = bindings.ScopeLocal
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

// Adopt runs the conversion.
func Adopt(opts Options) (*Outcome, error) {
	if err := opts.normalize(); err != nil {
		return nil, err
	}

	census, err := foreign.TakeCensus(opts.ConfigDir, opts.Pack)
	if err != nil {
		return nil, err
	}
	view, err := census.ResolveRepo(opts.RepoDir)
	if err != nil {
		return nil, err
	}
	switch len(view.EffectivelyEnabled) {
	case 0:
		return nil, fmt.Errorf("no foreign %s channel dispatches in %s; nothing to adopt — use 'sideshow enable %s' for a fresh repo", opts.Pack, opts.RepoDir, opts.Pack)
	case 1:
		// the adoption target
	default:
		return nil, fmt.Errorf("multiple foreign identities dispatch in %s (%s); resolve with 'sideshow coexist' before adopting", opts.RepoDir, strings.Join(view.EffectivelyEnabled, ", "))
	}
	identity := view.EffectivelyEnabled[0]

	// User-scope enable is the containment-mandate defect (Diagnose
	// grades it ERROR): a machine-wide enable of a per-repo-required
	// pack. Adopt refuses the direct flip — migrating the machine
	// posture (removing the user-scope entry, adding per-repo enables
	// in every repo that still wants the foreign channel) is the
	// operator's act, because only the operator knows which repos
	// those are.
	if census.UserEnabled(identity) {
		return nil, fmt.Errorf("%s is enabled at USER scope (machine-wide); adopt refuses the direct flip — first move the enable per-repo: remove the entry from the user settings, add a project/local-scope enable in each repo that should keep the foreign channel, then re-run adopt here", identity)
	}

	install := findInstall(census, identity)
	if install == nil {
		return nil, fmt.Errorf("%s is enabled in %s but has no install record (orphaned enable, trial T14); there is no running tree to adopt from — clean the orphan per 'sideshow coexist', then use plain enable", identity, opts.RepoDir)
	}

	// Version gate: the foreign tree's plugin.json is the version
	// authority (trial T13; the marketplace label is ref-pinned).
	runningVersion := install.TreeVersion
	if runningVersion == "" {
		runningVersion = install.Version
	}
	adoptVersion := opts.Version
	if adoptVersion == "" {
		adoptVersion = runningVersion
	}
	if adoptVersion == "" {
		return nil, fmt.Errorf("cannot determine the running version of %s (tree at %s unreadable and no registry version); pass an explicit version with %s@<version> --allow-version-change", identity, install.InstallPath, opts.Pack)
	}
	if runningVersion != "" && adoptVersion != runningVersion && !opts.AllowVersionChange {
		return nil, fmt.Errorf("foreign tree runs %s but adoption targets %s; equivalence is only provable at version equality — re-run with --allow-version-change to accept the drift", runningVersion, adoptVersion)
	}

	// Pre-state snapshot.
	footBefore, err := coexistcheck.SnapshotForeignFootprint(opts.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("snapshot foreign footprint: %w", err)
	}
	agentBefore := readAgentKey(opts.RepoDir)

	if opts.DryRun {
		fmt.Printf("adopt plan for %s in %s (dry run, nothing written):\n", opts.Pack, opts.RepoDir)
		fmt.Printf("  1. suppress foreign identity %s in this repo only (settings.local.json override; the install is untouched)\n", identity)
		fmt.Printf("  2. sideshow enable %s@%s --scope %s\n", opts.Pack, adoptVersion, opts.Scope)
		if agentBefore != "" && strings.HasPrefix(agentBefore, opts.Pack+":") {
			if opts.RewriteAgent {
				fmt.Printf("  3. rewrite default agent %q to the bound orchestrator (consented via --rewrite-agent)\n", agentBefore)
			} else {
				fmt.Printf("  3. default agent %q is the foreign form; it will KEEP pointing at the suppressed channel — pass --rewrite-agent to flip it, or run 'sideshow activate' after\n", agentBefore)
			}
		}
		fmt.Printf("  4. prove equivalence against the foreign tree at %s\n", install.InstallPath)
		return &Outcome{Identity: identity, Version: adoptVersion}, nil
	}

	// Step 1: repo-side suppression, BEFORE enable (see package doc).
	settingsCreated, err := foreign.SuppressInRepo(opts.RepoDir, identity)
	if err != nil {
		return nil, err
	}

	// Step 2: enable the sideshow channel; roll the suppression back
	// on any failure so the repo is exactly as found.
	eOpts := enable.Options{
		RepoDir: opts.RepoDir, Pack: opts.Pack, Version: adoptVersion,
		Scope: opts.Scope, LedgerPath: opts.LedgerPath, ConfigDir: opts.ConfigDir,
		OverrideStaleLock: opts.OverrideStaleLock,
		StoreRoot:         opts.StoreRoot, Prefix: opts.Prefix, BoundDir: opts.BoundDir,
		Now: opts.Now,
	}
	if err := enable.Enable(eOpts); err != nil {
		rollbackSuppression(opts.RepoDir, identity, settingsCreated)
		return nil, fmt.Errorf("enable failed; suppression rolled back, repo unchanged: %w", err)
	}

	out := &Outcome{Identity: identity, Version: adoptVersion, Suppressed: true}

	// Step 3: consented agent flip. Only the foreign form of THIS
	// pack's persona is eligible; anything else is the user's choice.
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		return out, err
	}
	row := led.RepoRow(opts.RepoDir, opts.Pack)
	if row == nil {
		return out, fmt.Errorf("enable reported success but no ledger row exists for %s; refusing to continue", opts.RepoDir)
	}
	prefix := opts.Prefix
	if prefix == "" {
		// Resolve the binding prefix the way enable did: from the
		// store's activation record (falling back to the pack name).
		prefix = opts.Pack
		if act, actErr := pack.LoadActivation(row.StorePath); actErr == nil {
			prefix = act.Prefix(opts.Pack)
		}
	}
	if opts.RewriteAgent && strings.HasPrefix(agentBefore, opts.Pack+":") {
		bound := prefix + "-orchestrator"
		if err := writeAgentKey(opts.RepoDir, bound); err != nil {
			return out, err
		}
		out.AgentRewritten = true
		fmt.Printf("default agent rewritten: %q -> %q\n", agentBefore, bound)
	} else if agentBefore != "" && strings.HasPrefix(agentBefore, opts.Pack+":") {
		fmt.Printf("note: default agent %q still names the suppressed foreign channel; flip it with 'sideshow activate %s --repo %s'\n", agentBefore, opts.Pack, opts.RepoDir)
	}

	// Step 4: honest equivalence, sideshow store vs foreign tree.
	out.Equivalence = equivalenceReport(row, install)
	for _, line := range out.Equivalence {
		fmt.Println(line)
	}

	// Step 5: prove adopt touched no machine-level harness state.
	footAfter, err := coexistcheck.SnapshotForeignFootprint(opts.ConfigDir)
	if err != nil {
		return out, fmt.Errorf("post-adopt footprint snapshot: %w", err)
	}
	out.FootprintDrift = coexistcheck.DiffFootprints(footBefore, footAfter)
	if len(out.FootprintDrift) > 0 {
		fmt.Printf("WARNING: machine-level harness state changed during adoption (it must not): %s\n", strings.Join(out.FootprintDrift, ", "))
	}

	fmt.Printf("adopted: %s@%s now serves %s via repo bindings; foreign identity %s is suppressed in this repo only\n", opts.Pack, adoptVersion, opts.RepoDir, identity)
	fmt.Println("Retreat: 'sideshow disable' removes the bindings; the suppression override in .claude/settings.local.json can then be deleted to restore the foreign channel exactly as found.")
	fmt.Printf("Machine-level residue (install, cache, marketplace) is untouched; review it with 'sideshow adopt %s --finish'.\n", opts.Pack)
	return out, nil
}

// Finish is the residue sweep: report every remaining foreign trace
// and the operator commands that retire each one. Print-only — the
// foreign channel is never modified by sideshow.
func Finish(opts Options) error {
	if err := opts.normalize(); err != nil {
		return err
	}
	census, err := foreign.TakeCensus(opts.ConfigDir, opts.Pack)
	if err != nil {
		return err
	}
	view, err := census.ResolveRepo(opts.RepoDir)
	if err != nil {
		return err
	}

	if len(census.Installs) == 0 && len(view.Orphans) == 0 {
		fmt.Printf("no foreign %s residue found under %s; nothing to finish\n", opts.Pack, opts.ConfigDir)
		return nil
	}

	fmt.Printf("foreign %s residue (nothing below is executed by sideshow; run what you consent to):\n", opts.Pack)
	for _, in := range census.Installs {
		fmt.Printf("  install %s scope=%s tree=%s version=%s\n", in.Identity, in.Scope, in.InstallPath, firstNonEmpty(in.TreeVersion, in.Version, "unknown"))
		fmt.Printf("    retire: claude plugin uninstall %s --scope %s\n", in.PluginName, in.Scope)
		if in.InstallPath != "" {
			fmt.Printf("    then remove the cache tree if uninstall leaves it: rm -rf %s\n", in.InstallPath)
		}
	}
	for _, orphan := range view.Orphans {
		fmt.Printf("  orphaned enable %s scope=%s in %s (no install behind it; the harness is silent about these, trial T14)\n", orphan.Identity, orphan.Scope, orphan.Path)
		fmt.Printf("    remove the entry from that settings file by hand\n")
	}
	fmt.Println("  marketplace removal: only after confirming it serves no other plugin — claude plugin marketplace remove <name>")
	fmt.Println("Re-run this command after acting; it reports until the census is clean.")
	return nil
}

func findInstall(c *foreign.Census, identity string) *foreign.Install {
	var fallback *foreign.Install
	for i := range c.Installs {
		if c.Installs[i].Identity != identity {
			continue
		}
		if c.Installs[i].InstallPath != "" {
			return &c.Installs[i]
		}
		if fallback == nil {
			fallback = &c.Installs[i]
		}
	}
	return fallback
}

func rollbackSuppression(repoDir, identity string, settingsCreated bool) {
	if settingsCreated {
		// The suppression was the file's only reason to exist.
		if err := os.Remove(filepath.Join(repoDir, ".claude", "settings.local.json")); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove the suppression settings file: %v\n", err)
		}
		return
	}
	if _, err := foreign.UnsuppressInRepo(repoDir, identity); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not roll back the suppression override for %s: %v\n", identity, err)
	}
}

// equivalenceReport is E1-E4 from the bead: content hash parity over
// the discovery trees (E1), count parity (E2), a live-session note
// for dispatcher behavior (E3), and dispatcher binary identity (E4).
// Invocation names, addressing form, and harness plugin details are
// declared NOT comparable across channels and are not checked.
func equivalenceReport(row *ledger.Row, install *foreign.Install) []string {
	var lines []string
	cache := install.InstallPath
	if cache == "" || !dirExists(cache) {
		return []string{fmt.Sprintf("equivalence: foreign tree absent at %q (cache-resident installs can be pruned by the harness, trial T13); E1/E2/E4 not provable — the store artifact's cosign verification is the remaining integrity evidence", cache)}
	}

	storeHashes, storeErr := hashTrees(row.StorePath, "skills", "agents")
	cacheHashes, cacheErr := hashTrees(cache, "skills", "agents")
	if storeErr != nil || cacheErr != nil {
		return []string{fmt.Sprintf("equivalence: tree walk failed (store: %v, cache: %v)", storeErr, cacheErr)}
	}

	mismatches := diffHashes(storeHashes, cacheHashes)
	if len(mismatches) == 0 {
		lines = append(lines, fmt.Sprintf("E1 content parity: PASS — %d files identical across store and foreign tree (unrewritten originals; the bound variant's renames are by-design divergence)", len(storeHashes)))
	} else {
		show := mismatches
		if len(show) > 10 {
			show = show[:10]
		}
		lines = append(lines, fmt.Sprintf("E1 content parity: FAIL — %d differing paths (first %d: %s); same-version trees should be identical — verify the store artifact before trusting the adoption", len(mismatches), len(show), strings.Join(show, ", ")))
	}

	lines = append(lines, fmt.Sprintf("E2 count parity: store=%d cache=%d discovery files", len(storeHashes), len(cacheHashes)))

	lines = append(lines, "E3 dispatcher behavior: not provable headless — start a session in the repo and confirm hook events reach the dispatcher (the enable pipeline already verified the settings hook chain)")

	dispatcher := filepath.Join("hooks", "dispatcher", "bin", row.Platform, dispatcherName(row.Platform))
	storeSha, sErr := fileSha(filepath.Join(row.StorePath, dispatcher))
	cacheSha, cErr := fileSha(filepath.Join(cache, dispatcher))
	switch {
	case sErr != nil || cErr != nil:
		lines = append(lines, fmt.Sprintf("E4 dispatcher identity: not provable (store: %v, cache: %v)", sErr, cErr))
	case storeSha == cacheSha:
		lines = append(lines, "E4 dispatcher identity: PASS — byte-identical binary for "+row.Platform)
	default:
		lines = append(lines, fmt.Sprintf("E4 dispatcher identity: FAIL — store %s vs cache %s for %s", storeSha[:12], cacheSha[:12], row.Platform))
	}
	return lines
}

func dispatcherName(platform string) string {
	if strings.HasPrefix(platform, "windows") {
		return "factory-dispatcher.exe"
	}
	return "factory-dispatcher"
}

// hashTrees maps <subdir>/<relpath> -> sha256 for regular files under
// the named subdirs of root. A missing subdir contributes nothing.
func hashTrees(root string, subdirs ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, sub := range subdirs {
		base := filepath.Join(root, sub)
		if !dirExists(base) {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			sha, err := fileSha(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = sha
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func diffHashes(a, b map[string]string) []string {
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

func fileSha(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// readAgentKey reads the default-agent key from the repo's settings
// chain, local over project (the same precedence the harness applies).
func readAgentKey(repoDir string) string {
	for _, name := range []string{"settings.local.json", "settings.json"} {
		data, err := os.ReadFile(filepath.Join(repoDir, ".claude", name))
		if err != nil {
			continue
		}
		var m struct {
			Agent string `json:"agent"`
		}
		if json.Unmarshal(data, &m) == nil && m.Agent != "" {
			return m.Agent
		}
	}
	return ""
}

// writeAgentKey sets the agent key in settings.local.json,
// read-merge-write, preserving siblings.
func writeAgentKey(repoDir, agent string) error {
	path := filepath.Join(repoDir, ".claude", "settings.local.json")
	settings := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse settings %s (refusing to merge into a file that cannot round-trip): %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read settings %s: %w", path, err)
	}
	settings["agent"] = agent
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}
