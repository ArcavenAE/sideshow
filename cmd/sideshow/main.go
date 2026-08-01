package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
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
	"github.com/ArcavenAE/sideshow/internal/weave"
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
  sideshow use <pack> <version>           Activate an installed version + sync bindings
  sideshow list                           List installed packs (all versions, active marked)
  sideshow commands sync                  Sync commands + skills bindings (pack content
                                          and registered _<pack>-custom/skills/ sources)
  sideshow project init <pack>            Apply consumer-repo convention to cwd and
                                          register it as a custom-skills source
  sideshow status                         Show installation status
  sideshow coexist <pack> [--repo <path>]  Read-only foreign-install census and coexistence findings
  sideshow enable <pack>[@<ver>] [--repo <path>] [--scope local|project]  Activate a pack in one repo (repo-bindings)
  sideshow disable <pack> [--repo <path>] [--override-stale-lock]  Reverse an enable exactly (ledger replay)
  sideshow activate <pack> [--repo <path>] [--agent <name>]  Consented persona flip (repo default agent)
  sideshow deactivate <pack> [--repo <path>]  Remove the persona flip only (prefix-guarded)
  sideshow coexist-check <pack> [--repo <path>]  Read-only enable/adopt preflight (ten checks)
  sideshow doctor [<pack>] [--layer <n,...>] [--repo <path>] [--json] [--strict]
                                          Read-only health report over store, receipts,
                                          and ledger (layers 1,3,4,5; docs/doctor-spec.md)
  sideshow adopt <pack> [--repo <path>] [--rewrite-agent] [--dry-run]  Convert a repo from the foreign (claude-mp) channel
  sideshow adopt <pack> --finish          Report remaining foreign residue (print-only)
  sideshow adopt <pack> --migrate-user-scope [--also-repo <path>] [--sweep-root <dir>] [--yes]
                                          Move a machine-wide foreign enable to per-repo enables
  sideshow version                        Show version

Install options:
  --from <path>          Source directory (required for now)
  --yes, -y              Skip confirmation prompts
  --no-activate          Install without flipping the active version
                         (first install of a pack always activates)
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

	// Help anywhere on the command line prints usage and performs no
	// action. This runs before dispatch so no subcommand can execute
	// as a side effect of asking for help (sideshow#57).
	for _, a := range os.Args[1:] {
		if a == "-h" || a == "--help" {
			usage()
			return
		}
	}

	var err error

	switch os.Args[1] {
	case "init":
		err = runInit(os.Args[2:])
	case "install":
		err = runInstall(os.Args[2:])
	case "use":
		err = runUse(os.Args[2:])
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
	case "activate":
		err = runActivate(os.Args[2:])
	case "deactivate":
		err = runDeactivate(os.Args[2:])
	case "enable":
		err = runEnable(os.Args[2:])
	case "disable":
		err = runDisable(os.Args[2:])
	case "coexist-check":
		err = runCoexistCheck(os.Args[2:])
	case "doctor":
		err = runDoctor(os.Args[2:])
	case "adopt":
		err = runAdopt(os.Args[2:])
	case "coexist":
		err = runCoexist(os.Args[2:])
	case "version", "--version", "-V":
		fmt.Printf("sideshow %s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		var ec exitCodeError
		if errors.As(err, &ec) {
			// The command already printed its report; the code is
			// the whole message.
			os.Exit(ec.code)
		}
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
		default:
			return fmt.Errorf("unknown flag for init: %s (see 'sideshow --help')", args[i])
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
			// File artifacts have no in-file ownership marker, so the prior
			// receipt is how distributeFile tells its own unmodified output
			// from a file the user has since edited. Per repo, per pack.
			repoOpts := opts
			repoOpts.PriorChecksums = distribute.PriorChecksums(
				reg, id.ID, projectRoot, filepath.Base(manifestPath), repo.Name, p.name)

			result := distribute.ToRepo(repo, &p.manifest, repoOpts)
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
	noActivate := false
	scope := permissions.ScopeUser
	scopeExplicit := false

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--from":
			if i+1 < len(args) {
				fromPath = args[i+1]
				i++
			}
		case "--yes", "-y":
			autoYes = true
		case "--no-activate":
			noActivate = true
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
				scopeExplicit = true
				i++
			}
		}
	}

	if fromPath == "" {
		return fmt.Errorf("--from <path> is required (git install not yet implemented)")
	}

	if err := pack.InstallFromLocal(name, fromPath, !noActivate); err != nil {
		return err
	}

	// Configure Claude Code permissions.
	if noPerms {
		return nil
	}

	// SIDESHOW_HOME signals a non-default data dir — typically a probe,
	// test, or scoped install. Don't write to user-global permissions
	// unless the caller explicitly asked for it via --scope.
	if os.Getenv("SIDESHOW_HOME") != "" && !scopeExplicit {
		fmt.Printf("\nSIDESHOW_HOME is set; skipping Claude Code permission configuration.\n")
		fmt.Printf("Pass --scope user or --scope project to configure permissions explicitly.\n")
		return nil
	}

	packPath := pack.PacksDir()
	settingsFile := permissions.SettingsPath(scope, ".")

	if !autoYes && !consentToPermissions(os.Stdin, os.Stdout, stdinIsTerminal(), packPath, settingsFile) {
		return nil
	}

	if err := permissions.ConfigureForScope(scope, packPath, "."); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to configure permissions: %v\n", err)
		fmt.Println("You may need to add the permission manually or accept prompts in Claude Code.")
	}

	return nil
}

// stdinIsTerminal reports whether stdin is an interactive terminal.
// Fails closed: a Stat error means "not a terminal", because the
// consequence of guessing wrong is writing a permission nobody
// approved.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// consentToPermissions asks whether to write Claude Code permission
// entries and reports the answer.
//
// It never infers consent from an absent answer (sideshow#94). The
// prompt documents a [Y/n] default, and the original read treated
// anything that was not "n"/"no" as yes — including the empty string
// an EOF returns. A piped or redirected install therefore granted
// Read permissions with nobody answering. Two cases now mean no: a
// non-interactive stdin, which cannot answer at all, and an EOF with
// nothing typed. Same family as the SIDESHOW_HOME contamination fix
// just above: the install path must not assume an environment it did
// not check.
//
// A bare Enter on a terminal is still yes. The bug is answers that
// were never given, not answers given tersely. --yes remains the
// intentional automation path and is handled by the caller.
// The writer is injected rather than written through fmt.Printf so the
// tests can run in parallel without swapping os.Stdout. Write errors on
// the prompt stream are discarded explicitly: a consent decision must
// not turn on whether the terminal accepted the question.
func consentToPermissions(in io.Reader, out io.Writer, isTerminal bool, packPath, settingsFile string) bool {
	if !isTerminal {
		_, _ = fmt.Fprintf(out, "\nstdin is not a terminal, so the permission prompt cannot be answered.\n")
		_, _ = fmt.Fprintf(out, "Skipping Claude Code permission configuration; re-run with --yes to configure it without prompting.\n")
		return false
	}

	_, _ = fmt.Fprintf(out, "\nConfigure Claude Code to read from %s?\n", packPath)
	_, _ = fmt.Fprintf(out, "  This adds Read(%s/) to %s\n", packPath, settingsFile)
	_, _ = fmt.Fprintf(out, "  [Y/n]: ")

	answer, readErr := bufio.NewReader(in).ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if readErr != nil && answer == "" {
		_, _ = fmt.Fprintf(out, "\nNo answer read (input closed). Skipping; re-run with --yes to configure permissions.\n")
		return false
	}
	if answer == "n" || answer == "no" {
		_, _ = fmt.Fprintf(out, "Skipped. You may be prompted by Claude Code when accessing pack files.\n")
		return false
	}
	return true
}

// runUse activates an installed pack version and re-syncs bindings so
// the flip is honest end-to-end (stale artifacts from the previously
// active version are reconciled away by the sync manifest).
func runUse(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: sideshow use <pack> <version>")
	}
	name, version := args[0], args[1]

	if err := pack.Activate(name, version); err != nil {
		return err
	}
	fmt.Printf("Activated %s %s\n", name, version)

	return bindings.Sync()
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

	fmt.Printf("%-20s %-15s %-7s %s\n", "PACK", "VERSION", "ACTIVE", "PATH")
	for _, p := range packs {
		versions, active, vErr := pack.InstalledVersions(p.Name)
		if vErr != nil || len(versions) == 0 {
			// Fall back to the registry's single-version view.
			fmt.Printf("%-20s %-15s %-7s %s\n", p.Name, p.Version, "*", p.Path)
			continue
		}
		for _, v := range versions {
			mark := ""
			if v == active {
				mark = "*"
			}
			fmt.Printf("%-20s %-15s %-7s %s\n", p.Name, v, mark,
				filepath.Join(pack.PacksDir(), p.Name, v))
		}
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

	// PriorChecksums is deliberately nil here. This path operates on cwd alone
	// and has no project installation context (no project id, no repos.yaml), so
	// there is no receipt to read. The effect on `files:` artifacts is fail-safe
	// but incomplete: sideshow creates a missing file and then leaves it alone on
	// every later run, because without a receipt it cannot distinguish its own
	// output from a user's file. Refreshing a file artifact needs the
	// `init --scope project` path. Tracked as a follow-on to aae-orc-rx3.
	result := distribute.ToRepo(repo, &packYAML.Distribute, distribute.Options{
		DryRun:      dryRun,
		PackName:    packYAML.Name,
		PackVersion: packYAML.Version,
		PackRoot:    packRoot,
	})

	if result.Error != nil {
		return fmt.Errorf("distribute: %w", result.Error)
	}

	// Register this repo as a custom source so `commands sync` binds
	// _<pack>-custom/skills/ from anywhere, not just from this cwd.
	if !dryRun {
		added, regErr := bindings.RegisterCustomSource(cwd, packName)
		if regErr != nil {
			fmt.Fprintf(os.Stderr, "  warning: register custom source: %v\n", regErr)
		} else if added {
			fmt.Printf("  registered custom source (binds %s/skills/ on sync)\n", filepath.Base(customDir))
		}
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

	// Weaving runs last, because it operates on the tree the steps above
	// produce. See docs/pack-weaving-spec.md.
	if err := runWeave(cwd, packName, dryRun); err != nil {
		return err
	}
	return nil
}

// runWeave applies the repo's weave declaration, if it has one.
//
// Failures are reported and do not fail the install by default. That default is
// deliberate: see docs/pack-weaving-spec.md § verification for the three lines
// of evidence behind it.
func runWeave(cwd, packName string, dryRun bool) error {
	res, found, err := weave.ApplyForPack(cwd, packName, weave.Options{DryRun: dryRun})
	if err != nil {
		// A malformed or unsupported declaration is an install error: the repo
		// asked for weaving and did not get it.
		return fmt.Errorf("weave: %w", err)
	}
	if !found {
		return nil
	}

	applied, skipped, failed := res.Counts()
	fmt.Printf("Weave: %d applied, %d skipped, %d failed\n", applied, skipped, failed)
	for _, a := range res.Actions {
		switch a.Outcome {
		case weave.Applied:
			fmt.Printf("  %s %s: %s\n", a.Type, a.Name, a.Detail)
		case weave.Failed:
			fmt.Fprintf(os.Stderr, "  %s %s: %s\n", a.Type, a.Name, a.Detail)
		}
	}
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
		act, actErr := pack.LoadActivation(p.Path)
		if actErr != nil {
			// Fail closed, matching the sync loop: no binding counts
			// for a pack whose activation contract cannot be read.
			fmt.Printf("  activation: ERROR: %v; excluded from user-scope sync\n", actErr)
			continue
		}
		if act.PluginClass() {
			scope := act.DefaultScope
			if scope == "" {
				scope = "per-repo"
			}
			fmt.Printf("  activation: %s (%s); bindings do not apply\n", act.Mechanism, scope)
			continue
		}
		if act != nil && act.PerRepoRequired {
			fmt.Printf("  activation: per-repo required; user-scope bindings do not apply\n")
			continue
		}
		available, err := bindings.CountForPack(p.Name, p.Path)
		if err != nil {
			fmt.Printf("  available: error: %v\n", err)
		} else {
			fmt.Printf("  available: %d\n", available)
		}
		synced, err := bindings.SyncedCount(p.Name, p.Path)
		if err != nil {
			fmt.Printf("  synced:    error: %v\n", err)
		} else {
			fmt.Printf("  synced:    %d\n", synced)
		}
	}

	sources, err := bindings.ListCustomSources()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: read custom sources: %v\n", err)
		return nil
	}
	if len(sources) > 0 {
		fmt.Println("\nCustom sources:")
		for _, s := range sources {
			fmt.Printf("  %s (pack %s)\n", s.Project, s.Pack)
		}
	}
	return nil
}
