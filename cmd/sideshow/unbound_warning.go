package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ArcavenAE/sideshow/internal/distribute"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// declaresConfigLink reports whether the pack links config.toml into the
// repo's shim directory, which is what makes a missing link worth a warning.
func declaresConfigLink(py *distribute.PackYAML) bool {
	if py == nil {
		return false
	}
	for _, l := range py.Distribute.RuntimeLinks {
		if filepath.Clean(l.Link) == "config.toml" {
			return true
		}
	}
	return false
}

// warnIfConfigUnresolved prints one warning when the repo's
// _<pack>/config.toml is absent or a dangling link. It never changes the
// exit status. Skills read that file through the pack's resolvers, so
// without it they fall back to neutral defaults.
func warnIfConfigUnresolved(cwd, packName string, py *distribute.PackYAML) {
	if !declaresConfigLink(py) {
		return
	}
	shim := "_" + packName
	if _, err := os.Stat(filepath.Join(cwd, shim, "config.toml")); err == nil {
		return
	}
	fmt.Printf("warning: %s/config.toml does not resolve in this repo, so %s skills fall back to neutral defaults. Run 'sideshow project init %s' here to bind it.\n",
		shim, packName, packName)
}

// warnIfCurrentRepoUnbound is the sync-side check. Sync is user-scope and
// runs from any directory, so it speaks only when the working directory
// shows signs of being a bmad consumer (a _bmad, _bmad-custom or
// _bmad-output directory) and the installed bmad pack expects a config link.
func warnIfCurrentRepoUnbound() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	const packName = "bmad"
	consumer := false
	for _, d := range []string{"_bmad", "_bmad-custom", "_bmad-output"} {
		if _, err := os.Lstat(filepath.Join(cwd, d)); err == nil {
			consumer = true
			break
		}
	}
	if !consumer {
		return
	}
	root, err := filepath.EvalSymlinks(filepath.Join(pack.PacksDir(), packName, "current"))
	if err != nil {
		return
	}
	py, err := distribute.LoadPackYAML(root)
	if err != nil {
		return
	}
	warnIfConfigUnresolved(cwd, packName, py)
}
