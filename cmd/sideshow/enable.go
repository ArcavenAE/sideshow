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
	opts, err := parseEnableArgs(args)
	if err != nil {
		return err
	}
	return enable.Enable(*opts)
}

func runDisable(args []string) error {
	opts, err := parseDisableArgs(args)
	if err != nil {
		return err
	}
	return enable.Disable(*opts)
}

// The per-command usage strings. usage_drift_test.go reads them to check
// the top-level help lines and the parsers against them.
const (
	enableUsage  = "usage: sideshow enable <pack>[@<version>] [--repo <path>] [--scope local|project] [--override-stale-lock]"
	disableUsage = "usage: sideshow disable <pack> [--repo <path>] [--override-stale-lock]"
)

// parseEnableArgs and parseDisableArgs are the parsers the two runners call.
func parseEnableArgs(args []string) (*enable.Options, error) { return parseVerbArgs("enable", args) }

func parseDisableArgs(args []string) (*enable.Options, error) { return parseVerbArgs("disable", args) }

func parseVerbArgs(verb string, args []string) (*enable.Options, error) {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		if verb == "disable" {
			return nil, fmt.Errorf("%s", disableUsage)
		}
		return nil, fmt.Errorf("%s", enableUsage)
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
