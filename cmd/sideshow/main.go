package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/distribute"
	sideshowinit "github.com/ArcavenAE/sideshow/internal/init"
	"github.com/ArcavenAE/sideshow/internal/pack"
	"github.com/ArcavenAE/sideshow/internal/permissions"
	"github.com/ArcavenAE/sideshow/internal/project"
)

// Set by ldflags at build time. Defaults are for local builds.
// CI injects: -X main.version=... -X main.channel=alpha
var (
	version = "dev"
	channel = "" //nolint:unused // set via ldflags, used in future updater
)

func usage() {
	fmt.Fprintf(os.Stderr, `sideshow — content pack manager for AI CLI tools

Usage:
  sideshow install <pack> --from <path>   Install a pack from a local path
  sideshow init [--user <name>] [--project <path>]
                                          Create config shim for BMAD agents
  sideshow init --scope project [--manifest <path>] [--pack <name>] [--dry-run]
                                          Distribute pack artifacts to subrepos
  sideshow list                           List installed packs
  sideshow commands sync                  Sync commands to ~/.claude/commands/
  sideshow status                         Show installation status
  sideshow version                        Show version

Install options:
  --from <path>          Source directory (required for now)
  --yes, -y              Skip confirmation prompts
  --no-permissions       Don't configure Claude Code read permissions
  --scope user|project   Where to add permissions (default: user)

Init options:
  --user <name>          Name agents should call you (default: from pack config)
  --project <path>       Project directory to init (default: current directory)
  --scope project        Distribute artifacts to subrepos via repos.yaml
  --manifest <path>      Path to repos.yaml (default: repos.yaml in cwd)
  --pack <name>          Pack to distribute (default: all packs with distribute section)
  --dry-run              Show what would change without writing (default for first run)

Examples:
  sideshow install bmad --from ~/work/ftc/_bmad
  sideshow install bmad --from ~/work/ftc/_bmad --yes
  sideshow init
  sideshow init --user "Michael"
  sideshow init --scope project --dry-run
  sideshow init --scope project
  sideshow commands sync
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error

	switch os.Args[1] {
	case "init":
		err = runInit(os.Args[2:])
	case "install":
		err = runInstall(os.Args[2:])
	case "list":
		err = runList()
	case "commands":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: sideshow commands sync")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "sync":
			err = runCommandsSync()
		default:
			fmt.Fprintf(os.Stderr, "unknown commands subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
	case "project":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: sideshow project <subcommand>")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "init":
			err = runProjectInitForPack(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "unknown project subcommand: %s\n", os.Args[2])
			os.Exit(1)
		}
	case "status":
		err = runStatus()
	case "version":
		fmt.Printf("sideshow %s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runInit(args []string) error {
	var userName string
	var projectRoot string
	var scope string
	var manifestPath string
	var packName string
	var dryRun bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--user":
			if i+1 < len(args) {
				userName = args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(args) {
				projectRoot = args[i+1]
				i++
			}
		case "--scope":
			if i+1 < len(args) {
				scope = args[i+1]
				i++
			}
		case "--manifest":
			if i+1 < len(args) {
				manifestPath = args[i+1]
				i++
			}
		case "--pack":
			if i+1 < len(args) {
				packName = args[i+1]
				i++
			}
		case "--dry-run":
			dryRun = true
		}
	}

	if scope == "project" {
		return runInitProject(projectRoot, manifestPath, packName, dryRun)
	}

	// Default: repo-scope init (existing behavior)
	return sideshowinit.Run(projectRoot, userName)
}

func runInitProject(projectRoot, manifestPath, packFilter string, dryRun bool) error {
	// Resolve project root
	if projectRoot == "" {
		var err error
		projectRoot, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}
	projectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("resolve project root: %w", err)
	}

	// Find repos.yaml
	if manifestPath == "" {
		manifestPath = project.FindReposManifest(projectRoot)
		if manifestPath == "" {
			return fmt.Errorf("repos.yaml not found in %s (use --manifest to specify)", projectRoot)
		}
	}

	// Parse repos.yaml
	manifest, err := project.LoadReposManifest(manifestPath)
	if err != nil {
		return err
	}

	// Resolve subrepos
	repos := project.ResolveSubrepos(projectRoot, manifest)
	var present, absent int
	for _, r := range repos {
		if r.Present {
			present++
		} else {
			absent++
		}
	}
	fmt.Printf("Project: %s\n", filepath.Base(projectRoot))
	fmt.Printf("Repos: %d present, %d not cloned\n\n", present, absent)

	// Initialize project identity
	id, err := project.InitIdentity(projectRoot, filepath.Base(projectRoot), filepath.Base(manifestPath))
	if err != nil {
		return fmt.Errorf("init project identity: %w", err)
	}
	fmt.Printf("Project ID: %s\n\n", id.ID)

	// Load registry
	reg, err := pack.LoadRegistry()
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}

	// Find packs with distribute manifests
	type packInfo struct {
		name     string
		version  string
		root     string
		manifest distribute.Manifest
	}
	var packs []packInfo

	for _, p := range reg.Packs {
		if packFilter != "" && p.Name != packFilter {
			continue
		}

		// Resolve the pack root through the "current" symlink
		packRoot, err := filepath.EvalSymlinks(p.Path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: cannot resolve pack %s: %v\n", p.Name, err)
			continue
		}

		packYAML, err := distribute.LoadPackYAML(packRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: cannot read pack.yaml for %s: %v\n", p.Name, err)
			continue
		}
		if packYAML == nil {
			continue // no pack.yaml — skip silently
		}
		if packYAML.Distribute.IsEmpty() {
			continue // no distribute section
		}

		packs = append(packs, packInfo{
			name:     p.Name,
			version:  p.Version,
			root:     packRoot,
			manifest: packYAML.Distribute,
		})
	}

	if len(packs) == 0 {
		fmt.Println("No packs with distribute sections found.")
		fmt.Println("Add a 'distribute' section to your pack's pack.yaml to enable project distribution.")
		return nil
	}

	// Distribute to each repo
	var allResults []distribute.Result

	for _, p := range packs {
		fmt.Printf("Pack: %s %s\n", p.name, p.version)

		opts := distribute.Options{
			DryRun:      dryRun,
			PackName:    p.name,
			PackVersion: p.version,
			PackRoot:    p.root,
		}

		for _, repo := range repos {
			result := distribute.ToRepo(repo, &p.manifest, opts)
			allResults = append(allResults, result)

			if result.Skipped {
				fmt.Printf("  %s/ — not cloned (skipped)\n", repo.Name)
				continue
			}

			fmt.Printf("  %s/\n", repo.Name)
			for _, action := range result.Actions {
				icon := statusIcon(action.Status)
				fmt.Printf("    %s %s — %s\n", icon, action.Path, action.Detail)
			}
		}

		// Record results to registry (even in dry-run, for tracking)
		if !dryRun {
			distribute.RecordResults(reg, id.ID, projectRoot, filepath.Base(manifestPath), allResults, opts)
		}

		fmt.Println()
	}

	// Save registry
	if !dryRun {
		// Update project last_seen
		proj := reg.FindOrCreateProject(id.ID)
		inst := proj.FindOrCreateInstallation(projectRoot, filepath.Base(manifestPath))
		inst.LastSeen = time.Now().UTC().Format(time.RFC3339)

		if err := reg.Save(); err != nil {
			return fmt.Errorf("save registry: %w", err)
		}
	}

	// Print summary
	var wrote, merged, skipped, conflicts, errors, skippedRepos int
	for _, r := range allResults {
		if r.Skipped {
			skippedRepos++
			continue
		}
		for _, a := range r.Actions {
			switch a.Status {
			case "wrote":
				wrote++
			case "merged":
				merged++
			case "skipped":
				skipped++
			case "conflict":
				conflicts++
			case "error":
				errors++
			}
		}
	}

	if dryRun {
		fmt.Println("=== DRY RUN (no files changed) ===")
	}
	fmt.Printf("Summary: %d repos processed, %d not cloned\n", present, absent)
	fmt.Printf("  %d wrote, %d merged, %d skipped, %d conflicts, %d errors\n",
		wrote, merged, skipped, conflicts, errors)

	// Session restart warning
	if wrote > 0 || merged > 0 || dryRun {
		fmt.Println()
		fmt.Println("┌──────────────────��──────────────────────────────┐")
		fmt.Println("│  RESTART REQUIRED                               │")
		fmt.Println("│                                                 │")
		fmt.Println("│  Running Claude Code and forestage sessions     │")
		fmt.Println("│  will NOT see these changes until restarted.    │")
		fmt.Println("│                                                 │")
		fmt.Println("│  .claude/settings.json → hooks load at start    │")
		fmt.Println("│  .claude/rules/*.md    → read at session start  │")
		fmt.Println("│  CLAUDE.md             → read at session start  │")
		fmt.Println("│                                                 │")
		fmt.Println("│  Restart any active sessions in affected repos. │")
		fmt.Println("└─────────────────────────────────────────────────┘")
	}

	return nil
}

func statusIcon(status string) string {
	switch status {
	case "wrote":
		return "+"
	case "merged":
		return "~"
	case "skipped":
		return "-"
	case "conflict":
		return "!"
	case "error":
		return "X"
	default:
		return "?"
	}
}

func runInstall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: sideshow install <pack> --from <path> [--yes] [--no-permissions] [--scope user|project]")
	}

	name := args[0]
	var fromPath string
	autoYes := false
	noPerms := false
	scope := permissions.ScopeUser

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--from":
			if i+1 < len(args) {
				fromPath = args[i+1]
				i++
			}
		case "--yes", "-y":
			autoYes = true
		case "--no-permissions":
			noPerms = true
		case "--scope":
			if i+1 < len(args) {
				switch args[i+1] {
				case "user":
					scope = permissions.ScopeUser
				case "project":
					scope = permissions.ScopeProject
				default:
					return fmt.Errorf("unknown scope: %s (use 'user' or 'project')", args[i+1])
				}
				i++
			}
		}
	}

	if fromPath == "" {
		return fmt.Errorf("--from <path> is required (git install not yet implemented)")
	}

	if err := pack.InstallFromLocal(name, fromPath); err != nil {
		return err
	}

	// Configure Claude Code permissions
	if noPerms {
		return nil
	}

	packPath := pack.PacksDir()
	settingsFile := permissions.SettingsPath(scope, ".")

	if !autoYes {
		fmt.Printf("\nConfigure Claude Code to read from %s?\n", packPath)
		fmt.Printf("  This adds Read(%s/) to %s\n", packPath, settingsFile)
		fmt.Printf("  [Y/n]: ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "n" || answer == "no" {
			fmt.Println("Skipped. You may be prompted by Claude Code when accessing pack files.")
			return nil
		}
	}

	if err := permissions.ConfigureForScope(scope, packPath, "."); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to configure permissions: %v\n", err)
		fmt.Println("You may need to add the permission manually or accept prompts in Claude Code.")
	}

	return nil
}

func runList() error {
	packs, err := pack.List()
	if err != nil {
		return err
	}

	if len(packs) == 0 {
		fmt.Println("No packs installed.")
		return nil
	}

	fmt.Printf("%-20s %-15s %s\n", "PACK", "VERSION", "PATH")
	for _, p := range packs {
		fmt.Printf("%-20s %-15s %s\n", p.Name, p.Version, p.Path)
	}
	return nil
}

func runCommandsSync() error {
	return bindings.Sync()
}

// runProjectInitForPack applies a pack's distribute manifest to the
// current working directory — chiefly gitignore entries so consumer
// repos don't commit sideshow-managed scaffolding.
// Implements aae-orc-f6ei.
func runProjectInitForPack(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: sideshow project init <pack> [--dry-run]")
	}
	packName := args[0]
	dryRun := false
	for _, a := range args[1:] {
		if a == "--dry-run" || a == "-n" {
			dryRun = true
		}
	}

	// Resolve pack user-install path via the current symlink.
	packLink := filepath.Join(pack.PacksDir(), packName, "current")
	packRoot, err := filepath.EvalSymlinks(packLink)
	if err != nil {
		return fmt.Errorf("pack %s not installed at %s: run 'sideshow install %s --from <path>' first", packName, packLink, packName)
	}

	// Load pack.yaml — this is what carries consumer-repo convention.
	packYAML, err := distribute.LoadPackYAML(packRoot)
	if err != nil {
		return fmt.Errorf("load pack.yaml: %w", err)
	}
	if packYAML == nil {
		return fmt.Errorf("pack %s has no pack.yaml at %s — the pack does not declare a consumer-repo convention and cannot be project-init'd", packName, packRoot)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	fmt.Printf("Applying %s %s consumer-repo convention to %s\n", packYAML.Name, packYAML.Version, cwd)
	if dryRun {
		fmt.Println("(dry run — no changes written)")
	}

	// Create per-repo directories (checked in by convention).
	customDir := filepath.Join(cwd, fmt.Sprintf("_%s-custom", packName))
	outputDir := filepath.Join(cwd, fmt.Sprintf("_%s-output", packName))
	for _, d := range []string{customDir, outputDir} {
		if _, statErr := os.Stat(d); os.IsNotExist(statErr) {
			if dryRun {
				fmt.Printf("  would create %s/\n", filepath.Base(d))
			} else {
				if err := os.MkdirAll(d, 0o755); err != nil {
					return fmt.Errorf("create %s: %w", d, err)
				}
				fmt.Printf("  created %s/\n", filepath.Base(d))
			}
		} else {
			fmt.Printf("  %s/ already present\n", filepath.Base(d))
		}
	}

	// Apply the distribute manifest (gitignore entries, etc.) to cwd.
	repo := project.Subrepo{
		Name:    filepath.Base(cwd),
		AbsPath: cwd,
		Present: true,
	}

	result := distribute.ToRepo(repo, &packYAML.Distribute, distribute.Options{
		DryRun:      dryRun,
		PackName:    packYAML.Name,
		PackVersion: packYAML.Version,
		PackRoot:    packRoot,
	})

	if result.Error != nil {
		return fmt.Errorf("distribute: %w", result.Error)
	}

	wrote := 0
	skipped := 0
	for _, a := range result.Actions {
		switch a.Status {
		case "wrote", "merged":
			wrote++
			fmt.Printf("  %s: %s %s\n", a.Type, a.Status, a.Path)
		case "skipped":
			skipped++
		case "error":
			fmt.Fprintf(os.Stderr, "  %s: error %s: %s\n", a.Type, a.Path, a.Detail)
		}
	}

	fmt.Printf("Done: %d wrote, %d already present\n", wrote, skipped)
	return nil
}

func runStatus() error {
	packs, err := pack.List()
	if err != nil {
		return err
	}

	if len(packs) == 0 {
		fmt.Println("No packs installed.")
		return nil
	}

	for _, p := range packs {
		fmt.Printf("%s %s\n", p.Name, p.Version)
		available, err := bindings.CountForPack(p.Name, p.Path)
		if err != nil {
			fmt.Printf("  available: error: %v\n", err)
		} else {
			fmt.Printf("  available: %d\n", available)
		}
		synced, err := bindings.SyncedCount(p.Name)
		if err != nil {
			fmt.Printf("  synced:    error: %v\n", err)
		} else {
			fmt.Printf("  synced:    %d\n", synced)
		}
	}
	return nil
}
