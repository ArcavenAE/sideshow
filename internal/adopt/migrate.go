package adopt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArcavenAE/sideshow/internal/factoryguard"
	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/ledger"
	"github.com/ArcavenAE/sideshow/internal/preserve"
)

// MigrateUserScope moves a machine-wide (user-scope) enable of the
// foreign channel to per-repo enables, then removes the machine-wide
// entry (aae-orc-d3nq.22 clause b).
//
// Adopt refuses to run against a user-scope enable, because a
// per-repo-required pack activating in every repo on the machine is
// itself the containment defect, and adopting one repo would leave the
// rest of the machine in it. This is the consented path out: pin the
// foreign channel in each repo that currently depends on the
// machine-wide enable, drop the machine-wide entry, and prove per repo
// that dropping it changed nothing. Afterwards the target repo is an
// ordinary per-repo adoption.
//
// The invariant is that effective enablement is IDENTICAL in every
// swept repo before and after. That is what makes the migration safe to
// consent to, and it is verified against disk, not predicted.
//
// The honest limit, stated in the plan and in the report: sideshow
// cannot enumerate every repo on the machine. Repos outside the sweep
// set that relied on the machine-wide enable lose the foreign channel.
// Only the operator can complete that list, so the sweep sources are
// disclosed and extensible rather than guessed at.
type MigrateOptions struct {
	// RepoDir is the repo the operator intends to adopt next. It joins
	// the sweep set like any other candidate.
	RepoDir    string
	Pack       string
	ConfigDir  string
	LedgerPath string
	// Scope is where per-repo enables are written: local (default,
	// untracked) or project (committed, and gated on CommitConsent).
	Scope string
	// CommitConsent is required for project scope: the write lands in a
	// tracked file every clone of that repo inherits.
	CommitConsent bool
	// AlsoRepos are operator-named repos to include in the sweep.
	AlsoRepos []string
	// SweepRoots are directories to scan for git checkouts (depth 3,
	// not descending into a checkout once found).
	SweepRoots []string
	// OverrideStaleLock accepts expired factory locks across the sweep,
	// matching enable's flag. Unexpired locks never yield.
	OverrideStaleLock bool
	// DryRun prints the plan and writes nothing.
	DryRun bool
	// Consented records that the operator approved the printed plan.
	// Without it the real run refuses after printing.
	Consented bool
	Now       time.Time
}

// RepoPlan is one swept repo's part in the migration.
type RepoPlan struct {
	Dir string
	// Source records how the repo entered the sweep, so the operator can
	// judge the set's completeness.
	Source string
	// Enabled are the pack's foreign identities this repo loads today.
	Enabled []string
	// Pin are the identities that would STOP loading here if the
	// machine-wide entry were removed, so they get a per-repo enable.
	Pin []string
	// SettingsPath is the file a pin writes, empty when Pin is empty.
	SettingsPath string
}

// MigrationOutcome reports the plan and, for a real run, the result.
type MigrationOutcome struct {
	Identities []string
	Repos      []RepoPlan
	// Blockers are reasons the real run refuses. A dry run prints them
	// and returns them as an error; a real run never starts with any.
	Blockers []string
	Applied  bool
	// Pinned counts the per-repo enables written.
	Pinned int
}

func (o *MigrateOptions) normalize() error {
	if o.ConfigDir == "" {
		o.ConfigDir = foreign.ConfigDir()
	}
	if o.LedgerPath == "" {
		o.LedgerPath = ledger.Path()
	}
	if o.Scope == "" {
		o.Scope = foreign.ScopeLocal
	}
	if o.Scope != foreign.ScopeLocal && o.Scope != foreign.ScopeProject {
		return fmt.Errorf("per-repo enables go to local or project scope, not %q; user scope is what this migration removes", o.Scope)
	}
	if o.Scope == foreign.ScopeProject && !o.CommitConsent {
		return fmt.Errorf("project scope writes .claude/settings.json, a tracked file every clone of the repo inherits; re-run with --commit-consent to accept that, or use the default local scope")
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

// MigrateUserScope runs the migration (or, with DryRun, prints the plan
// it would run).
func MigrateUserScope(opts MigrateOptions) (*MigrationOutcome, error) {
	if err := opts.normalize(); err != nil {
		return nil, err
	}

	census, err := foreign.TakeCensus(opts.ConfigDir, opts.Pack)
	if err != nil {
		return nil, err
	}
	identities := census.UserEnabledIdentities()
	if len(identities) == 0 {
		return nil, fmt.Errorf("no user-scope enable of %s found in %s; there is no machine-wide posture to migrate (run 'sideshow coexist %s' to see the current one)", opts.Pack, filepath.Join(opts.ConfigDir, "settings.json"), opts.Pack)
	}

	out := &MigrationOutcome{Identities: identities}

	// The counterfactual census: the machine as it would be with the
	// user-scope entries gone. Resolving a repo against both censuses is
	// what identifies the repos that depend on the machine-wide enable,
	// rather than guessing from install records.
	after := census.WithoutUserEnables(identities)

	candidates, sweepNotes := collectCandidates(opts)
	out.Blockers = append(out.Blockers, sweepNotes...)

	for _, cand := range candidates {
		plan := RepoPlan{Dir: cand.dir, Source: cand.source}
		before, err := census.ResolveRepo(cand.dir)
		if err != nil {
			out.Blockers = append(out.Blockers, fmt.Sprintf("%s: %v", cand.dir, err))
			continue
		}
		lost, err := after.ResolveRepo(cand.dir)
		if err != nil {
			out.Blockers = append(out.Blockers, fmt.Sprintf("%s: %v", cand.dir, err))
			continue
		}
		plan.Enabled = before.EffectivelyEnabled
		plan.Pin = missing(before.EffectivelyEnabled, lost.EffectivelyEnabled)
		if len(plan.Pin) > 0 {
			path, err := foreign.SettingsPath(cand.dir, opts.ConfigDir, opts.Scope)
			if err != nil {
				return nil, err
			}
			if protected, component, reason := preserve.IsProtected(path); protected {
				out.Blockers = append(out.Blockers, fmt.Sprintf("%s sits under protected state (%s: %s); sideshow does not write settings there", path, component, reason))
				continue
			}
			plan.SettingsPath = path
		}
		out.Repos = append(out.Repos, plan)
	}

	// The session and lock sweep covers EVERY swept repo, not just the
	// target: a machine-scoped flip changes what dispatches in all of
	// them, so a wave in flight anywhere is a reason to wait.
	for _, plan := range out.Repos {
		v := factoryguard.CheckRepo(plan.Dir, opts.Now)
		switch {
		case v.HardRefusal():
			out.Blockers = append(out.Blockers, fmt.Sprintf("%s has a factory run in flight: %s", plan.Dir, oneLine(v.Refusal())))
		case v.InFlight() && !opts.OverrideStaleLock:
			out.Blockers = append(out.Blockers, fmt.Sprintf("%s shows factory activity: %s (pass --override-stale-lock to accept an expired lock)", plan.Dir, oneLine(v.Refusal())))
		}
	}

	// Validate every write the plan names, so a dry run reports the
	// failure the real run would hit instead of promising success
	// (finding-099 F099-a).
	userSettings := filepath.Join(opts.ConfigDir, "settings.json")
	if err := foreign.CanWriteSettings(userSettings); err != nil {
		out.Blockers = append(out.Blockers, fmt.Sprintf("cannot update the machine-wide entry: %v", err))
	}
	for _, plan := range out.Repos {
		if plan.SettingsPath == "" {
			continue
		}
		if err := foreign.CanWriteSettings(plan.SettingsPath); err != nil {
			out.Blockers = append(out.Blockers, fmt.Sprintf("cannot pin %s: %v", plan.Dir, err))
		}
		out.Pinned += len(plan.Pin)
	}

	printMigrationPlan(opts, out)

	if len(out.Blockers) > 0 {
		return out, fmt.Errorf("migration refused: %d blocker(s) reported above", len(out.Blockers))
	}
	if opts.DryRun {
		fmt.Println("every step of the plan above resolves: the real run would proceed")
		return out, nil
	}
	if !opts.Consented {
		return out, fmt.Errorf("this changes machine-wide harness posture and needs consent; re-run with --yes after reading the plan above")
	}

	if err := applyMigration(opts, out, identities, census); err != nil {
		return out, err
	}
	out.Applied = true

	fmt.Printf("migrated: %s no longer carries a machine-wide enable of %s; %d per-repo enable(s) written at %s scope\n",
		userSettings, strings.Join(identities, ", "), out.Pinned, opts.Scope)
	fmt.Printf("Adopt the target repo now: sideshow adopt %s --repo %s\n", opts.Pack, opts.RepoDir)
	fmt.Println("Reverse: remove the per-repo enables listed above and restore the machine-wide entry (this is the posture the containment mandate grades an ERROR, so reversing brings the defect back).")
	return out, nil
}

// applyMigration writes the pins, drops the machine-wide entries, and
// verifies against disk that effective enablement is unchanged in every
// swept repo. Any mismatch rolls the whole migration back.
func applyMigration(opts MigrateOptions, out *MigrationOutcome, identities []string, before *foreign.Census) error {
	pre := map[string][]string{}
	for _, plan := range out.Repos {
		pre[plan.Dir] = plan.Enabled
	}

	var undo undoLog
	// Pins first: no repo may lose the channel even momentarily.
	for _, plan := range out.Repos {
		for _, id := range plan.Pin {
			prior, err := foreign.ReadEnable(plan.SettingsPath, id)
			if err != nil {
				undo.run()
				return fmt.Errorf("read %s before pinning: %w", plan.SettingsPath, err)
			}
			created, err := foreign.SetEnable(plan.SettingsPath, id, true)
			if err != nil {
				undo.run()
				return fmt.Errorf("pin %s in %s: %w", id, plan.Dir, err)
			}
			undo.add(plan.SettingsPath, id, prior, created)
		}
	}
	for _, id := range identities {
		path := before.UserEnablePath(id)
		if path == "" {
			path = filepath.Join(opts.ConfigDir, "settings.json")
		}
		prior, err := foreign.ReadEnable(path, id)
		if err != nil {
			undo.run()
			return fmt.Errorf("read %s before removing the machine-wide entry: %w", path, err)
		}
		if _, err := foreign.DeleteEnable(path, id); err != nil {
			undo.run()
			return fmt.Errorf("remove the machine-wide enable of %s: %w", id, err)
		}
		undo.add(path, id, prior, false)
	}

	// Verify against disk. This is the "user-scope disable verified a
	// no-op per repo" clause, and it is the reason the migration is
	// consentable: it either held everywhere or it is reverted.
	fresh, err := foreign.TakeCensus(opts.ConfigDir, opts.Pack)
	if err != nil {
		undo.run()
		return fmt.Errorf("re-read the census to verify the migration: %w", err)
	}
	if ids := fresh.UserEnabledIdentities(); len(ids) > 0 {
		undo.run()
		return fmt.Errorf("the machine-wide enable of %s survived removal; migration rolled back", strings.Join(ids, ", "))
	}
	for _, plan := range out.Repos {
		view, err := fresh.ResolveRepo(plan.Dir)
		if err != nil {
			undo.run()
			return fmt.Errorf("verify %s: %w; migration rolled back", plan.Dir, err)
		}
		if !sameSet(pre[plan.Dir], view.EffectivelyEnabled) {
			undo.run()
			return fmt.Errorf("removing the machine-wide entry was NOT a no-op in %s (was [%s], now [%s]); migration rolled back",
				plan.Dir, strings.Join(pre[plan.Dir], " "), strings.Join(view.EffectivelyEnabled, " "))
		}
	}
	return nil
}

// undoLog reverses settings writes in the order that restores the
// starting state: last write first, prior values restored exactly, and
// a file sideshow created removed only when its removal passes the
// preserve floor and it is empty again.
type undoLog struct {
	entries []undoEntry
	created map[string]bool
}

type undoEntry struct {
	path     string
	identity string
	prior    foreign.EnableEntry
}

func (u *undoLog) add(path, identity string, prior foreign.EnableEntry, created bool) {
	if created {
		if u.created == nil {
			u.created = map[string]bool{}
		}
		u.created[path] = true
	}
	u.entries = append(u.entries, undoEntry{path: path, identity: identity, prior: prior})
}

func (u *undoLog) run() {
	for i := len(u.entries) - 1; i >= 0; i-- {
		e := u.entries[i]
		var err error
		if e.prior.Present {
			_, err = foreign.SetEnable(e.path, e.identity, e.prior.Value)
		} else {
			_, err = foreign.DeleteEnable(e.path, e.identity)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not restore %s in %s: %v\n", e.identity, e.path, err)
		}
	}
	for path := range u.created {
		if err := preserve.Check(path); err != nil {
			fmt.Fprintf(os.Stderr, "warning: leaving %s in place: %v\n", path, err)
			continue
		}
		if !settingsFileIsEmpty(path) {
			continue
		}
		if err := os.Remove(path); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove the settings file sideshow created at %s: %v\n", path, err)
		}
	}
}

// settingsFileIsEmpty reports whether the file holds an empty JSON
// object, which is the state a rolled-back pin leaves behind in a file
// sideshow created. Anything else is content sideshow did not write.
func settingsFileIsEmpty(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "{}"
}

type candidate struct {
	dir    string
	source string
}

// collectCandidates builds the sweep set from disclosed sources, in
// precedence order (first source to name a repo owns the label), and
// returns notes for anything it could not use.
func collectCandidates(opts MigrateOptions) ([]candidate, []string) {
	var notes []string
	seen := map[string]bool{}
	var out []candidate

	add := func(dir, source string) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			notes = append(notes, fmt.Sprintf("cannot resolve %s (%s): %v", dir, source, err))
			return
		}
		if seen[abs] {
			return
		}
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			// A ledger row for a deleted checkout is stale bookkeeping,
			// not a blocker. A path the operator typed is a typo, and
			// silently sweeping one repo fewer than they asked for is the
			// worst outcome here: the entry still goes.
			if source == sourceOperator || source == sourceTarget {
				notes = append(notes, fmt.Sprintf("%s: %s is not a directory", source, dir))
			}
			return
		}
		seen[abs] = true
		out = append(out, candidate{dir: abs, source: source})
	}

	add(opts.RepoDir, sourceTarget)
	for _, dir := range opts.AlsoRepos {
		add(dir, sourceOperator)
	}
	if led, err := ledger.Load(opts.LedgerPath); err == nil {
		dirs := led.RepoDirs()
		sort.Strings(dirs)
		for _, dir := range dirs {
			add(dir, "sideshow ledger")
		}
	} else {
		notes = append(notes, fmt.Sprintf("cannot read the repo-bindings ledger: %v", err))
	}
	if census, err := foreign.TakeCensus(opts.ConfigDir, opts.Pack); err == nil {
		for _, in := range census.Installs {
			if in.ProjectPath != "" {
				add(in.ProjectPath, "harness project-scope install")
			}
		}
	}
	for _, root := range opts.SweepRoots {
		found, err := findGitCheckouts(root, 3)
		if err != nil {
			notes = append(notes, fmt.Sprintf("--sweep-root %s: %v", root, err))
			continue
		}
		for _, dir := range found {
			add(dir, "sweep "+root)
		}
	}
	return out, notes
}

const (
	sourceTarget   = "adoption target"
	sourceOperator = "operator (--also-repo)"
)

// findGitCheckouts lists directories containing .git under root, to
// maxDepth levels, without descending into a checkout once found.
// Hidden directories are skipped: a sweep is for working repos, and
// caches full of vendored checkouts are noise at best.
func findGitCheckouts(root string, maxDepth int) ([]string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("not a directory")
	}
	var found []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			found = append(found, dir)
			return
		}
		if depth >= maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			walk(filepath.Join(dir, e.Name()), depth+1)
		}
	}
	walk(abs, 0)
	sort.Strings(found)
	return found, nil
}

func printMigrationPlan(opts MigrateOptions, out *MigrationOutcome) {
	verb := "migration plan"
	if opts.DryRun {
		verb = "migration plan (dry run, nothing written)"
	}
	fmt.Printf("%s for %s: move the machine-wide enable of %s to per-repo enables at %s scope\n",
		verb, opts.Pack, strings.Join(out.Identities, ", "), opts.Scope)

	fmt.Printf("  swept %d repo(s):\n", len(out.Repos))
	for _, plan := range out.Repos {
		switch {
		case len(plan.Pin) > 0:
			fmt.Printf("    PIN  %s [%s]: enable %s in %s\n", plan.Dir, plan.Source, strings.Join(plan.Pin, ", "), plan.SettingsPath)
		case len(plan.Enabled) > 0:
			fmt.Printf("    keep %s [%s]: already enabled independently of user scope; no write\n", plan.Dir, plan.Source)
		default:
			fmt.Printf("    skip %s [%s]: the foreign channel does not dispatch here; no write\n", plan.Dir, plan.Source)
		}
	}
	fmt.Printf("  then remove the %s entries for %s from %s\n",
		foreign.EnableKey, strings.Join(out.Identities, ", "), filepath.Join(opts.ConfigDir, "settings.json"))
	fmt.Println("  then verify per repo, against disk, that the removal changed nothing; any mismatch rolls the whole migration back")

	fmt.Println("LIMIT: sideshow cannot enumerate every repo on this machine. Any repo NOT listed above that")
	fmt.Println("  relied on the machine-wide enable stops loading the foreign channel. Add repos with")
	fmt.Println("  --also-repo <path> or --sweep-root <dir>, or re-add the entry per repo afterwards.")

	for _, b := range out.Blockers {
		fmt.Printf("  BLOCKER: %s\n", b)
	}
}

// missing returns the elements of a that are absent from b.
func missing(a, b []string) []string {
	have := make(map[string]bool, len(b))
	for _, v := range b {
		have[v] = true
	}
	var out []string
	for _, v := range a {
		if !have[v] {
			out = append(out, v)
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
