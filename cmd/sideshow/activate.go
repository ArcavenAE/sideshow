package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/enable"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// runActivate / runDeactivate are the consented persona flip:
//
//	sideshow activate <pack> [--repo <path>] [--agent <name>]
//	sideshow deactivate <pack> [--repo <path>]
func runActivate(args []string) error {
	opts, agent, err := parseActivateArgs("activate", args, true)
	if err != nil {
		return err
	}
	return enable.Activate(*opts, agent)
}

func runDeactivate(args []string) error {
	opts, _, err := parseActivateArgs("deactivate", args, false)
	if err != nil {
		return err
	}
	return enable.Deactivate(*opts)
}

func parseActivateArgs(verb string, args []string, allowAgent bool) (*enable.Options, string, error) {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		return nil, "", fmt.Errorf("usage: sideshow %s <pack> [--repo <path>]", verb)
	}
	packName, version, _ := strings.Cut(args[0], "@")
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("resolve working directory: %w", err)
	}
	opts := &enable.Options{Pack: packName, Version: version, RepoDir: repoDir}
	agent := ""
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("--repo requires a path")
			}
			i++
			opts.RepoDir = args[i]
		case "--agent":
			if !allowAgent {
				return nil, "", fmt.Errorf("--agent applies to activate only")
			}
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("--agent requires a name")
			}
			i++
			agent = args[i]
		default:
			return nil, "", fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	// The prefix guards both verbs; resolve it from the installed
	// pack's activation contract when present.
	if p := findInstalledPack(opts.Pack); p != nil {
		if act, actErr := pack.LoadActivation(p.Path); actErr == nil {
			opts.Prefix = act.Prefix(opts.Pack)
		}
	}
	return opts, agent, nil
}
