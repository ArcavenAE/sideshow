package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/adopt"
	"github.com/ArcavenAE/sideshow/internal/bindings"
)

const adoptUsage = "usage: sideshow adopt <pack>[@<version>] [--repo <path>] [--scope local|project] " +
	"[--allow-version-change] [--rewrite-agent] [--dry-run] [--override-stale-lock] [--finish] " +
	"[--migrate-user-scope [--also-repo <path>] [--sweep-root <dir>] [--commit-consent] [--yes]]"

// runAdopt is the conversion verb (aae-orc-d3nq.22). Three modes:
//
//   - default: convert this repo from the foreign channel.
//   - --finish: report remaining foreign residue and the operator
//     command that retires each trace. Executes nothing.
//   - --migrate-user-scope: move a machine-wide enable to per-repo
//     enables, the consented precondition for adopting a repo whose
//     foreign channel comes from user scope.
func runAdopt(args []string) error {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		return fmt.Errorf("%s", adoptUsage)
	}
	packName, version, _ := strings.Cut(args[0], "@")
	repoDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	opts := adopt.Options{Pack: packName, Version: version, RepoDir: repoDir}
	mig := adopt.MigrateOptions{Pack: packName, RepoDir: repoDir}
	finish, migrate, autoYes := false, false, false

	need := func(i int, flag, what string) error {
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires %s", flag, what)
		}
		return nil
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if err := need(i, "--repo", "a path"); err != nil {
				return err
			}
			i++
			opts.RepoDir, mig.RepoDir = args[i], args[i]
		case "--scope":
			if err := need(i, "--scope", "local or project"); err != nil {
				return err
			}
			i++
			if args[i] == "user" {
				return fmt.Errorf("--scope user is refused: a per-repo-required pack never activates machine-wide (containment mandate); use local or project")
			}
			opts.Scope = bindings.RepoScope(args[i])
			mig.Scope = args[i]
		case "--allow-version-change":
			opts.AllowVersionChange = true
		case "--rewrite-agent":
			opts.RewriteAgent = true
		case "--dry-run":
			opts.DryRun, mig.DryRun = true, true
		case "--override-stale-lock":
			opts.OverrideStaleLock, mig.OverrideStaleLock = true, true
		case "--finish":
			finish = true
		case "--migrate-user-scope":
			migrate = true
		case "--also-repo":
			if err := need(i, "--also-repo", "a path"); err != nil {
				return err
			}
			i++
			mig.AlsoRepos = append(mig.AlsoRepos, args[i])
		case "--sweep-root":
			if err := need(i, "--sweep-root", "a directory"); err != nil {
				return err
			}
			i++
			mig.SweepRoots = append(mig.SweepRoots, args[i])
		case "--commit-consent":
			mig.CommitConsent = true
		case "--yes", "-y":
			autoYes = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	if finish && migrate {
		return fmt.Errorf("--finish reports residue and --migrate-user-scope changes machine posture; run them one at a time")
	}
	if finish {
		return adopt.Finish(opts)
	}
	if migrate {
		// Consent for a machine-wide posture change is --yes or a typed
		// answer, never an unanswered prompt (sideshow#94). The plan is
		// printed by MigrateUserScope itself, so the operator sees it
		// before the question either way: a dry run first, then --yes.
		mig.Consented = autoYes
		_, err := adopt.MigrateUserScope(mig)
		return err
	}
	if autoYes {
		return fmt.Errorf("--yes applies to --migrate-user-scope; a conversion asks for consent through its explicit flags (--rewrite-agent, --allow-version-change)")
	}
	_, err = adopt.Adopt(opts)
	return err
}
