package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/coexistcheck"
	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// runCoexistCheck is the read-only per-repo preflight:
//
//	sideshow coexist-check <pack> [--repo <path>]
//
// The same Run the enable/adopt verbs call as a precondition, exposed
// standalone so an operator (or doctor) can ask "would enable refuse
// here, and why" without touching anything.
func runCoexistCheck(args []string) error {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		return fmt.Errorf("usage: sideshow coexist-check <pack> [--repo <path>]")
	}
	packName := args[0]
	repoDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 >= len(args) {
				return fmt.Errorf("--repo requires a path")
			}
			i++
			repoDir = args[i]
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	opts := coexistcheck.Options{
		RepoDir:         repoDir,
		Pack:            packName,
		Prefix:          packName,
		ConfigDir:       foreign.ConfigDir(),
		PerRepoRequired: true,
		Now:             time.Now().UTC(),
	}
	if p := findInstalledPack(packName); p != nil {
		act, actErr := pack.LoadActivation(p.Path)
		if actErr == nil {
			opts.Prefix = act.Prefix(packName)
			if act != nil {
				opts.PerRepoRequired = act.PerRepoRequired
			}
		}
		// Resolve through the `current` symlink so check 6 compares
		// version dirs, not the symlink's basename.
		opts.StoreRoot = p.Path
		if resolved, rErr := filepath.EvalSymlinks(p.Path); rErr == nil {
			opts.StoreRoot = resolved
		}
		if inv, invErr := bindings.DiscoverPluginLayout(*p); invErr == nil {
			opts.Inventory = inv
		}
	}

	rep, err := coexistcheck.Run(opts)
	if err != nil {
		return err
	}

	fmt.Printf("coexist-check: %s in %s\n", packName, repoDir)
	if len(rep.Results) == 0 {
		fmt.Println("  all checks clean")
	}
	for _, r := range rep.Results {
		fmt.Printf("  %s [%d %s]: %s\n", r.Severity, r.Check, r.Name, r.Detail)
	}
	if a := rep.RetreatAnchor; a != nil && a.FactoryArtifactsSHA != "" {
		fmt.Printf("retreat anchor: factory-artifacts %s, .factory dirty=%v, captured %s\n",
			a.FactoryArtifactsSHA, a.FactoryDirty, a.CapturedAt.Format(time.RFC3339))
	}
	if rep.Refuse() {
		return fmt.Errorf("preflight refused: enable/adopt would not proceed in this repo")
	}
	fmt.Println("preflight clean: enable/adopt may proceed")
	return nil
}
