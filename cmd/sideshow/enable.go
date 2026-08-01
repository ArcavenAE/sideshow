package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/enable"
)

// runEnable / runDisable are the per-repo activation verbs:
//
//	sideshow enable <pack>[@<version>] [--repo <path>] [--scope local|project] [--override-stale-lock]
//	sideshow disable <pack> [--repo <path>] [--override-stale-lock]
func runEnable(args []string) error {
	opts, err := parseVerbArgs("enable", args)
	if err != nil {
		return err
	}
	return enable.Enable(*opts)
}

func runDisable(args []string) error {
	opts, err := parseVerbArgs("disable", args)
	if err != nil {
		return err
	}
	return enable.Disable(*opts)
}

func parseVerbArgs(verb string, args []string) (*enable.Options, error) {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		return nil, fmt.Errorf("usage: sideshow %s <pack>[@<version>] [--repo <path>] [--scope local|project] [--override-stale-lock]", verb)
	}
	packName, version, _ := strings.Cut(args[0], "@")
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	opts := &enable.Options{Pack: packName, Version: version, RepoDir: repoDir}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--repo requires a path")
			}
			i++
			opts.RepoDir = args[i]
		case "--scope":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--scope requires local or project")
			}
			i++
			if args[i] == "user" {
				return nil, fmt.Errorf("--scope user is refused: a per-repo-required pack never activates machine-wide (containment mandate); use local or project")
			}
			opts.Scope = bindings.RepoScope(args[i])
		case "--override-stale-lock":
			opts.OverrideStaleLock = true
		default:
			return nil, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	return opts, nil
}
