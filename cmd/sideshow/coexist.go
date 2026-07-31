package main

import (
	"fmt"
	"os"

	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// runCoexist is the read-only coexistence census:
//
//	sideshow coexist <pack> [--repo <path>] [--sideshow-active]
//
// It reports foreign installs of the pack (any marketplace, matched
// by name), the repo-level effective/suppressed/orphaned enablement,
// and doctor-graded findings. It never mutates harness state.
// --sideshow-active asserts that sideshow bindings for the pack are
// effective in the repo; once the repo-bindings ledger ships (.7)
// that signal is read from the ledger instead.
func runCoexist(args []string) error {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		return fmt.Errorf("usage: sideshow coexist <pack> [--repo <path>] [--sideshow-active]")
	}
	packName := args[0]
	repoDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	sideshowActive := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 >= len(args) {
				return fmt.Errorf("--repo requires a path")
			}
			i++
			repoDir = args[i]
		case "--sideshow-active":
			sideshowActive = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	census, err := foreign.TakeCensus(foreign.ConfigDir(), packName)
	if err != nil {
		return err
	}
	view, err := census.ResolveRepo(repoDir)
	if err != nil {
		return err
	}

	// The per-repo mandate comes from the installed pack's activation
	// contract when the pack is in the store; a pack that is not
	// installed is graded conservatively (a guard that under-warns is
	// worse than one that over-warns).
	perRepoRequired := true
	if p := findInstalledPack(packName); p != nil {
		if act, actErr := pack.LoadActivation(p.Path); actErr == nil && act != nil {
			perRepoRequired = act.PerRepoRequired
		}
	}

	fmt.Printf("Foreign installs of %s (machine level):\n", packName)
	if len(census.Installs) == 0 {
		fmt.Println("  none")
	}
	for _, in := range census.Installs {
		legacy := ""
		if in.Legacy {
			legacy = "  [LEGACY pre-rc.7 identity]"
		}
		treeNote := in.TreeVersion
		if treeNote == "" {
			treeNote = "unreadable"
		}
		fmt.Printf("  %s  scope=%s  registry=%s  tree=%s  sha=%s%s\n",
			in.Identity, in.Scope, in.Version, treeNote, in.GitCommitSha, legacy)
		if in.TreeVersion != "" && in.TreeVersion != in.Version {
			fmt.Printf("    note: tree version differs from the registry label; the tree is the authority (git-subdir sources drift)\n")
		}
	}

	fmt.Printf("\nRepo %s:\n", repoDir)
	fmt.Printf("  effectively enabled: %v\n", orNone(view.EffectivelyEnabled))
	fmt.Printf("  suppressed here:     %v\n", orNone(view.Suppressed))
	if len(view.Orphans) > 0 {
		fmt.Println("  orphaned enables:")
		for _, o := range view.Orphans {
			fmt.Printf("    %s (%s scope, %s)\n", o.Identity, o.Scope, o.Path)
		}
	}

	findings := foreign.Diagnose(census, view, sideshowActive, perRepoRequired)
	if len(findings) > 0 {
		fmt.Println()
		for _, f := range findings {
			fmt.Printf("%s [%s]: %s\n", f.Severity, f.Code, f.Message)
		}
	}
	return nil
}

func orNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	out := items[0]
	for _, s := range items[1:] {
		out += ", " + s
	}
	return out
}

// findInstalledPack returns the registry entry for a pack in the
// sideshow store, or nil.
func findInstalledPack(name string) *pack.InstalledPack {
	packs, err := pack.List()
	if err != nil {
		return nil
	}
	for i := range packs {
		if packs[i].Name == name {
			return &packs[i]
		}
	}
	return nil
}
