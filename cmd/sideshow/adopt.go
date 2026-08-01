package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/adopt"
	"github.com/ArcavenAE/sideshow/internal/bindings"
)

// runAdopt is the conversion verb (aae-orc-d3nq.22):
//
//	sideshow adopt <pack>[@<version>] [--repo <path>] [--scope local|project]
//	               [--allow-version-change] [--rewrite-agent] [--dry-run]
//	               [--override-stale-lock] [--finish]
//
// --finish runs the residue report instead of a conversion: it lists
// remaining foreign traces and the operator commands that retire
// them, and executes nothing.
func runAdopt(args []string) error {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		return fmt.Errorf("usage: sideshow adopt <pack>[@<version>] [--repo <path>] [--scope local|project] [--allow-version-change] [--rewrite-agent] [--dry-run] [--override-stale-lock] [--finish]")
	}
	packName, version, _ := strings.Cut(args[0], "@")
	repoDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	opts := adopt.Options{Pack: packName, Version: version, RepoDir: repoDir}
	finish := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 >= len(args) {
				return fmt.Errorf("--repo requires a path")
			}
			i++
			opts.RepoDir = args[i]
		case "--scope":
			if i+1 >= len(args) {
				return fmt.Errorf("--scope requires local or project")
			}
			i++
			if args[i] == "user" {
				return fmt.Errorf("--scope user is refused: a per-repo-required pack never activates machine-wide (containment mandate); use local or project")
			}
			opts.Scope = bindings.RepoScope(args[i])
		case "--allow-version-change":
			opts.AllowVersionChange = true
		case "--rewrite-agent":
			opts.RewriteAgent = true
		case "--dry-run":
			opts.DryRun = true
		case "--override-stale-lock":
			opts.OverrideStaleLock = true
		case "--finish":
			finish = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	if finish {
		return adopt.Finish(opts)
	}
	_, err = adopt.Adopt(opts)
	return err
}
