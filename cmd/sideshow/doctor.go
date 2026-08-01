package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/doctor"
)

// exitCodeError carries a non-message exit code out of a command.
// main's error handler exits with the code without printing an
// "error:" line: the report already said everything.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit %d", e.code) }

// runDoctor is the read-only health surface:
//
//	sideshow doctor [<pack>] [--layer <n[,n...]>] [--repo <path>]
//	                [--json] [--strict]
//
// Spec: docs/doctor-spec.md. Exit 0 unless a structural failure is
// present (exit 2); --strict promotes structural warns. Advisory and
// unavailable findings never affect the exit code.
func runDoctor(args []string) error {
	var opts doctor.Options
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		opts.Pack = args[0]
		args = args[1:]
	}
	var asJSON bool
	if wd, err := os.Getwd(); err == nil {
		opts.RepoDir = wd
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--layer":
			if i+1 >= len(args) {
				return fmt.Errorf("--layer requires a comma list from 1,3,4,5")
			}
			i++
			for _, part := range strings.Split(args[i], ",") {
				n, err := strconv.Atoi(strings.TrimSpace(part))
				if err != nil {
					return fmt.Errorf("--layer: %q is not a layer number", part)
				}
				opts.Layers = append(opts.Layers, n)
			}
		case "--repo":
			if i+1 >= len(args) {
				return fmt.Errorf("--repo requires a path")
			}
			i++
			opts.RepoDir = args[i]
		case "--json":
			asJSON = true
		case "--strict":
			opts.Strict = true
		default:
			return fmt.Errorf("unknown flag for doctor: %s (see 'sideshow --help')", args[i])
		}
	}

	report, layers, err := doctor.Run(opts)
	if err != nil {
		return err
	}
	if asJSON {
		if err := report.WriteJSON(os.Stdout); err != nil {
			return err
		}
	} else {
		report.WriteText(os.Stdout, layers)
	}
	if report.Summary.ExitCode != 0 {
		return exitCodeError{code: report.Summary.ExitCode}
	}
	return nil
}
