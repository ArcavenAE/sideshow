// Package coexistcheck is the per-repo preflight for enable and adopt
// (aae-orc-d3nq.41): ten read-only checks composing the foreign
// census (paqn), the running-factory guard (.40), the binding
// planners (.48/.51), and the repo-bindings ledger (.5). The verbs
// (.7 enable, .22 adopt) call Run as a precondition; doctor consumes
// the same predicates read-only. Nothing here mutates repo, store, or
// harness state.
package coexistcheck

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/factoryguard"
	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// Result grades one check.
type Result struct {
	Check    int // 1..10 per the bead's numbering
	Name     string
	Severity foreign.Severity // reuse INFO/WARN/ERROR
	Detail   string
}

// Report is the full preflight outcome.
type Report struct {
	RepoDir string
	Pack    string
	Results []Result
	// RetreatAnchor is check (7): the pre-trial factory-artifacts tip
	// SHA and .factory worktree status, recorded so adopt/retreat can
	// prove the trial changed nothing it should not have.
	RetreatAnchor *Anchor
}

// Anchor is the retreat reference point.
type Anchor struct {
	FactoryArtifactsSHA string
	FactoryDirty        bool
	CapturedAt          time.Time
}

// Refuse reports whether any ERROR-grade result fired.
func (r *Report) Refuse() bool {
	for _, res := range r.Results {
		if res.Severity == foreign.Error {
			return true
		}
	}
	return false
}

// Options configures a run.
type Options struct {
	RepoDir         string
	Pack            string
	Prefix          string // binding prefix (activation.Prefix)
	StoreRoot       string // resolved version dir being enabled; "" when unknown
	Inventory       *bindings.PluginInventory
	ConfigDir       string // harness config dir (foreign.ConfigDir())
	LedgerPath      string // "" means ledger.Path()
	PerRepoRequired bool
	Now             time.Time
}

// Run executes the preflight.
func Run(opts Options) (*Report, error) {
	rep := &Report{RepoDir: opts.RepoDir, Pack: opts.Pack}
	if opts.LedgerPath == "" {
		opts.LedgerPath = ledger.Path()
	}

	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		return nil, err
	}
	row := led.RepoRow(opts.RepoDir, opts.Pack)

	// (1) + (10): foreign effective-enable resolution; same-repo
	// double-dispatch is a HARD refusal (T11: both chains fired at one
	// SessionStart). Orphaned enables surface as WARN (T14).
	census, err := foreign.TakeCensus(opts.ConfigDir, opts.Pack)
	if err != nil {
		return nil, err
	}
	view, err := census.ResolveRepo(opts.RepoDir)
	if err != nil {
		return nil, err
	}
	sideshowActive := row != nil
	for _, f := range foreign.Diagnose(census, view, sideshowActive, opts.PerRepoRequired) {
		rep.add(1, "foreign-enable-resolution", f.Severity, f.Message)
	}
	// Enabling OUR channel where a foreign identity is already
	// effectively enabled is the same class-1 double-dispatch even
	// before any sideshow binding exists.
	if !sideshowActive {
		for _, id := range view.EffectivelyEnabled {
			rep.add(10, "same-repo-dual-enable", foreign.Error,
				fmt.Sprintf("%s is effectively enabled in this repo; enabling the sideshow channel would run two hook chains against one .factory state. %s",
					id, foreign.RefusalOptions(id, opts.RepoDir)))
		}
	}

	// (2) running-factory census: guard signals plus the git-level
	// in-flight indicators the guard's fs view cannot see.
	guard := factoryguard.CheckRepo(opts.RepoDir, opts.Now)
	for _, s := range guard.Signals {
		sev := foreign.Warn
		if s.Hard {
			sev = foreign.Error
		}
		rep.add(2, "running-factory", sev, fmt.Sprintf("[%s] %s", s.Kind, s.Detail))
	}
	dirty, dirtyKnown := factoryWorktreeDirty(opts.RepoDir)
	if dirtyKnown && dirty {
		rep.add(2, "running-factory", foreign.Warn, ".factory worktree has uncommitted changes")
	}
	if stories := storyWorktrees(opts.RepoDir); len(stories) > 0 {
		rep.add(2, "running-factory", foreign.Warn,
			fmt.Sprintf("story worktrees present: %s", strings.Join(stories, ", ")))
	}

	// (3) remote-safety: a factory-artifacts ref on origin means this
	// checkout shares CAS state with another (bin/factory-cas-push.sh
	// uses force-with-lease; a trial repo pushing that ref can clobber
	// a production checkout).
	if ref := originFactoryArtifactsRef(opts.RepoDir); ref != "" {
		rep.add(3, "remote-safety", foreign.Error,
			fmt.Sprintf("origin carries %s; this checkout shares factory-artifacts state with another checkout — do not run trials here", ref))
	}

	// (4) platform bind: only meaningful once the ledger says this
	// repo is bound; verifies the env shim resolves and the dispatcher
	// is executable at its store path.
	if row != nil {
		checkPlatformBind(rep, opts, row)
	}

	// (5) agent-key audit: which channel's agent copies live in the
	// repo. Prefixed names are ours; a foreign enable means the
	// harness ALSO loads namespace-qualified agents from the plugin.
	checkAgentKeys(rep, opts, view)

	// (6) version skew: ledger row vs the store version being enabled.
	if row != nil && opts.StoreRoot != "" {
		want := filepath.Base(opts.StoreRoot)
		if row.Version != want {
			rep.add(6, "version-skew", foreign.Warn,
				fmt.Sprintf("repo is bound to %s %s but %s is being enabled; use the version-toggle path (disable+enable) rather than re-enable", opts.Pack, row.Version, want))
		}
	}

	// (7) retreat anchor.
	rep.RetreatAnchor = captureAnchor(opts.RepoDir, opts.Now)

	// (9) pre-existing content collision: everything materialization
	// would write that already exists. Refuse rather than clobber.
	if opts.Inventory != nil {
		for _, rel := range plannedPaths(opts) {
			if _, err := os.Lstat(filepath.Join(opts.RepoDir, filepath.FromSlash(rel))); err == nil {
				rep.add(9, "content-collision", foreign.Error,
					fmt.Sprintf("%s already exists; sideshow never overwrites pre-existing repo content", rel))
			}
		}
	}

	sort.SliceStable(rep.Results, func(i, j int) bool { return rep.Results[i].Check < rep.Results[j].Check })
	return rep, nil
}

func (r *Report) add(check int, name string, sev foreign.Severity, detail string) {
	r.Results = append(r.Results, Result{Check: check, Name: name, Severity: sev, Detail: detail})
}

// plannedPaths mirrors the materialization plan (.48/.51/.58): the
// prefixed skill dirs, prefixed agent top-level entries, and the
// compat symlink.
func plannedPaths(opts Options) []string {
	var out []string
	for _, s := range opts.Inventory.SkillDirs {
		out = append(out, ".claude/skills/"+opts.Prefix+"-"+filepath.Base(s))
	}
	seenTop := map[string]bool{}
	for _, a := range opts.Inventory.AgentFiles {
		parts := strings.Split(filepath.ToSlash(a), "/")
		if len(parts) < 2 {
			continue
		}
		top := ".claude/agents/" + opts.Prefix + "-" + parts[1]
		if !seenTop[top] {
			seenTop[top] = true
			out = append(out, top)
		}
	}
	out = append(out, "plugins/"+opts.Pack)
	return out
}

func checkPlatformBind(rep *Report, opts Options, row *ledger.Row) {
	scopeFile := ".claude/settings.local.json"
	if row.SettingsScope == "project" {
		scopeFile = ".claude/settings.json"
	}
	settingsPath := filepath.Join(opts.RepoDir, filepath.FromSlash(scopeFile))
	if err := bindings.VerifyEnvShim(settingsPath, "CLAUDE_PLUGIN_ROOT", row.StorePath); err != nil {
		rep.add(4, "platform-bind", foreign.Error, err.Error())
	}
	dispatcher := findDispatcher(row.StorePath)
	if dispatcher == "" {
		rep.add(4, "platform-bind", foreign.Warn,
			"no dispatcher binary found under the bound store path; hook commands referencing it will fail")
		return
	}
	if info, err := os.Stat(dispatcher); err != nil || info.Mode().Perm()&0o100 == 0 {
		rep.add(4, "platform-bind", foreign.Error,
			fmt.Sprintf("dispatcher %s is not executable (zero-hooks trap: registrations that silently do nothing)", dispatcher))
	}
}

// findDispatcher locates the platform dispatcher under the store
// tree (hooks/dispatcher/<binary> per the upstream layout).
func findDispatcher(storeRoot string) string {
	dir := filepath.Join(storeRoot, "hooks", "dispatcher")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

func checkAgentKeys(rep *Report, opts Options, view *foreign.RepoView) {
	agentsDir := filepath.Join(opts.RepoDir, ".claude", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return
	}
	var ours, bare []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".md")
		if strings.HasPrefix(name, opts.Prefix+"-") {
			ours = append(ours, name)
		} else {
			bare = append(bare, name)
		}
	}
	if len(ours) > 0 {
		rep.add(5, "agent-key-audit", foreign.Info,
			fmt.Sprintf("%d prefixed agent entries in .claude/agents (sideshow channel)", len(ours)))
	}
	if len(bare) > 0 && len(view.EffectivelyEnabled) > 0 {
		rep.add(5, "agent-key-audit", foreign.Warn,
			fmt.Sprintf("unprefixed .claude/agents entries (%s) beside an enabled foreign identity; bare-name agent calls may resolve to either channel", strings.Join(bare, ", ")))
	}
}

func captureAnchor(repoDir string, now time.Time) *Anchor {
	a := &Anchor{CapturedAt: now}
	if sha, err := gitOutput(repoDir, "rev-parse", "--verify", "--quiet", "refs/heads/factory-artifacts"); err == nil {
		a.FactoryArtifactsSHA = sha
	} else if sha, err := gitOutput(repoDir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/factory-artifacts"); err == nil {
		a.FactoryArtifactsSHA = sha
	}
	if dirty, known := factoryWorktreeDirty(repoDir); known {
		a.FactoryDirty = dirty
	}
	return a
}

// factoryWorktreeDirty reports uncommitted changes under .factory.
// known=false when the repo is not a git checkout or .factory is
// absent.
func factoryWorktreeDirty(repoDir string) (dirty, known bool) {
	if _, err := os.Stat(filepath.Join(repoDir, ".factory")); err != nil {
		return false, false
	}
	out, err := gitOutput(repoDir, "status", "--porcelain", "--", ".factory")
	if err != nil {
		return false, false
	}
	return out != "", true
}

// storyWorktrees lists .worktrees/STORY-* entries — the factory's own
// worktree pattern.
func storyWorktrees(repoDir string) []string {
	matches, _ := filepath.Glob(filepath.Join(repoDir, ".worktrees", "STORY-*"))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, filepath.Base(m))
	}
	return out
}

// originFactoryArtifactsRef returns the local record of an origin
// factory-artifacts ref, empty when absent. Deliberately reads local
// remote-tracking state only — a preflight must not hit the network.
func originFactoryArtifactsRef(repoDir string) string {
	if _, err := gitOutput(repoDir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/factory-artifacts"); err == nil {
		return "refs/remotes/origin/factory-artifacts"
	}
	return ""
}

func gitOutput(repoDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoDir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// SnapshotForeignFootprint is check (8)'s instrument: a content hash
// of the foreign claude-mp footprint (registry, settings, marketplace
// and plugin cache trees) under configDir. The footprint is fully
// disjoint from everything sideshow writes, so identical before and
// after a trial is clean proof the trial left the foreign channel
// untouched.
func SnapshotForeignFootprint(configDir string) (map[string]string, error) {
	roots := []string{
		filepath.Join(configDir, "plugins"),
		filepath.Join(configDir, "settings.json"),
	}
	snap := map[string]string{}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if err := hashInto(snap, configDir, root); err != nil {
				return nil, err
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil || d.IsDir() {
				return werr
			}
			return hashInto(snap, configDir, path)
		})
		if err != nil {
			return nil, err
		}
	}
	return snap, nil
}

func hashInto(snap map[string]string, base, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return err
	}
	snap[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(data))
	return nil
}

// DiffFootprints reports drift between two snapshots.
func DiffFootprints(before, after map[string]string) []string {
	var out []string
	for k, v := range after {
		if bv, ok := before[k]; !ok {
			out = append(out, "added "+k)
		} else if bv != v {
			out = append(out, "changed "+k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			out = append(out, "removed "+k)
		}
	}
	sort.Strings(out)
	return out
}
