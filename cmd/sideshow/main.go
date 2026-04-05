package main

import (
	"fmt"
	"os"

	"github.com/ArcavenAE/sideshow/internal/commands"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

func usage() {
	fmt.Fprintf(os.Stderr, `sideshow — content pack manager for AI CLI tools

Usage:
  sideshow install <pack> --from <path>   Install a pack from a local path
  sideshow list                           List installed packs
  sideshow commands sync                  Sync commands to ~/.claude/commands/
  sideshow status                         Show installation status
  sideshow version                        Show version

Examples:
  sideshow install bmad --from ~/work/ftc/_bmad
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

func runInstall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: sideshow install <pack> --from <path>")
	}

	name := args[0]
	var fromPath string

	for i := 1; i < len(args); i++ {
		if args[i] == "--from" && i+1 < len(args) {
			fromPath = args[i+1]
			i++
		}
	}

	if fromPath == "" {
		return fmt.Errorf("--from <path> is required (git install not yet implemented)")
	}

	return pack.InstallFromLocal(name, fromPath)
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
