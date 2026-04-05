package main

import (
	"fmt"
	"os"

	"bufio"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/commands"
	sideshowinit "github.com/ArcavenAE/sideshow/internal/init"
	"github.com/ArcavenAE/sideshow/internal/pack"
	"github.com/ArcavenAE/sideshow/internal/permissions"
)

func usage() {
	fmt.Fprintf(os.Stderr, `sideshow — content pack manager for AI CLI tools

Usage:
  sideshow install <pack> --from <path>   Install a pack from a local path
  sideshow init [--user <name>] [--project <path>]
                                          Create config shim for BMAD agents
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

Examples:
  sideshow install bmad --from ~/work/ftc/_bmad
  sideshow install bmad --from ~/work/ftc/_bmad --yes
  sideshow init
  sideshow init --user "Michael"
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
	case "status":
		err = runStatus()
	case "version":
		fmt.Println("sideshow 0.1.0-dev")
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
		}
	}

	return sideshowinit.Run(projectRoot, userName)
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
	return commands.Sync()
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
		cmdCount, err := commands.CountForPack(p.Name, p.Path)
		if err != nil {
			fmt.Printf("  commands: error: %v\n", err)
		} else {
			fmt.Printf("  commands: %d\n", cmdCount)
		}
		synced, err := commands.SyncedCount(p.Name)
		if err != nil {
			fmt.Printf("  synced:   error: %v\n", err)
		} else {
			fmt.Printf("  synced:   %d\n", synced)
		}
	}
	return nil
}
