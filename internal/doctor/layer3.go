package doctor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/coexistcheck"
	"github.com/ArcavenAE/sideshow/internal/factoryguard"
	"github.com/ArcavenAE/sideshow/internal/foreign"
	"github.com/ArcavenAE/sideshow/internal/pack"
)

// layer3Checks is the cwd discoverability probe. Warn-only by
// ratified decision; the runner clamps everything here to advisory
// and to warn or below, so nothing in this layer can gate CI or
// abort a run.
func layer3Checks() []Check {
	return []Check{
		{ID: "cwd-known", Layer: 3, Run: checkCwdKnown},
		{ID: "cwd-coexistence", Layer: 3, Run: checkCwdCoexistence},
		{ID: "cwd-factory-guard", Layer: 3, Run: checkCwdFactoryGuard},
	}
}

func checkCwdKnown(ctx *Context) []Finding {
	if ctx.RepoDir == "" {
		return []Finding{{
			Layer: 3, ID: "cwd-known", Status: Unavailable, Class: Advisory,
			Detail: "working directory could not be resolved",
			Next:   "re-run with --repo <path>",
		}}
	}
	var how []string
	if ctx.Ledger != nil {
		for _, dir := range ctx.Ledger.RepoDirs() {
			if dir == ctx.RepoDir {
				how = append(how, "repo-bindings ledger row")
				break
			}
		}
	}
	if ctx.Registry != nil {
		for _, proj := range ctx.Registry.Projects {
			for _, inst := range proj.Installations {
				if ctx.RepoDir == inst.Root || strings.HasPrefix(ctx.RepoDir, inst.Root+string(filepath.Separator)) {
					how = append(how, "project installation root "+inst.Root)
				}
			}
		}
	}
	if len(how) == 0 {
		return []Finding{{
			Layer: 3, ID: "cwd-known", Subject: ctx.RepoDir, Status: Warn, Class: Advisory,
			Detail: "sideshow has no record of this directory; an agent started here finds no sideshow-managed content",
			Next:   "sideshow enable <pack> --repo . (plugin-class) or sideshow init (project distribution)",
		}}
	}
	return []Finding{{
		Layer: 3, ID: "cwd-known", Subject: ctx.RepoDir, Status: OK, Class: Advisory,
		Detail: "known via " + strings.Join(how, "; "),
	}}
}

// checkCwdCoexistence runs the read-only coexist-check battery for
// every installed pack against the subject directory. ERROR grades
// are reported with their original grade named but land as warn:
// this layer informs, it does not gate.
func checkCwdCoexistence(ctx *Context) []Finding {
	if ctx.RepoDir == "" {
		return nil // cwd-known already reported the missing subject
	}
	var out []Finding
	for _, p := range ctx.Packs {
		opts := coexistcheck.Options{
			RepoDir:         ctx.RepoDir,
			Pack:            p.Name,
			Prefix:          p.Name,
			ConfigDir:       foreign.ConfigDir(),
			PerRepoRequired: true,
			Now:             ctx.Now,
		}
		if act, err := pack.LoadActivation(p.Path); err == nil && act != nil {
			opts.Prefix = act.Prefix(p.Name)
			opts.PerRepoRequired = act.PerRepoRequired
		}
		if resolved, err := filepath.EvalSymlinks(p.Path); err == nil {
			opts.StoreRoot = resolved
		}
		report, err := coexistcheck.Run(opts)
		if err != nil {
			out = append(out, Finding{
				Layer: 3, ID: "cwd-coexistence", Pack: p.Name, Status: Unavailable, Class: Advisory,
				Detail: fmt.Sprintf("coexist-check did not run: %v", err),
				Next:   fmt.Sprintf("sideshow coexist-check %s --repo %s", p.Name, ctx.RepoDir),
			})
			continue
		}
		clean := true
		for _, res := range report.Results {
			if res.Severity == foreign.Info {
				continue
			}
			clean = false
			detail := fmt.Sprintf("check %d (%s): %s", res.Check, res.Name, res.Detail)
			if res.Severity == foreign.Error {
				detail += " (graded ERROR by coexist-check; enable would refuse here)"
			}
			out = append(out, Finding{
				Layer: 3, ID: "cwd-coexistence", Pack: p.Name, Subject: ctx.RepoDir, Status: Warn, Class: Advisory,
				Detail: detail,
				Next:   fmt.Sprintf("sideshow coexist-check %s --repo %s (full battery output)", p.Name, ctx.RepoDir),
			})
		}
		if clean {
			out = append(out, Finding{
				Layer: 3, ID: "cwd-coexistence", Pack: p.Name, Subject: ctx.RepoDir, Status: OK, Class: Advisory,
				Detail: "coexistence battery clean",
			})
		}
	}
	return out
}

// checkCwdFactoryGuard reports in-flight factory activity. The
// signal is TTL-based, so the observation time is printed.
func checkCwdFactoryGuard(ctx *Context) []Finding {
	if ctx.RepoDir == "" {
		return nil
	}
	v := factoryguard.CheckRepo(ctx.RepoDir, ctx.Now)
	if v == nil || !v.InFlight() {
		return []Finding{{
			Layer: 3, ID: "cwd-factory-guard", Subject: ctx.RepoDir, Status: OK, Class: Advisory,
			Detail: "no in-flight factory activity",
		}}
	}
	return []Finding{{
		Layer: 3, ID: "cwd-factory-guard", Subject: ctx.RepoDir, Status: Warn, Class: Advisory,
		Detail: fmt.Sprintf("%s (observed at %s; the signal is TTL-based)", v.Refusal(), ctx.Now.Format("15:04:05 MST")),
		Next:   "wait for the run to finish, or confirm the lock is stale before overriding anything",
	}}
}
