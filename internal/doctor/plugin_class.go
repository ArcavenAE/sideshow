package doctor

import (
	"fmt"
	"os"
	"path/filepath"
)

// pluginClassChecks is the mechanism-keyed check set for packs that
// activate per repo (repo-bindings channel). The seam for
// aae-orc-d3nq.9: the per-repo bind battery (env shim, hook chain,
// dispatcher exec, local-scope symlinks, replay preflight, dangling
// foreign registrations) registers here as further checks without
// touching Run, the output layout, or the exit policy. This PR ships
// one check so the seam is proven by use.
func pluginClassChecks() []Check {
	return []Check{
		{ID: "ledger-row-coherence", Layer: 1, Run: checkLedgerRows},
	}
}

// checkLedgerRows verifies every repo-bindings ledger row against
// disk: the pinned store path exists and is a version dir (the
// ledger contract forbids `current`), the row's version matches the
// dir, and the bound repo still exists.
func checkLedgerRows(ctx *Context) []Finding {
	if ctx.LedgerErr != nil {
		return []Finding{{
			Layer: 1, ID: "ledger-row-coherence", Status: Unavailable, Class: Structural,
			Detail: fmt.Sprintf("ledger does not load: %v (readers fail closed rather than reporting a repo as unbound)", ctx.LedgerErr),
			Next:   "inspect the repo-bindings ledger by hand; do not delete it",
		}}
	}
	var out []Finding
	rows := 0
	for repoDir, packs := range ctx.Ledger.Repos {
		for packName, row := range packs {
			if ctx.PackFilter != "" && packName != ctx.PackFilter {
				continue
			}
			rows++
			if _, err := os.Stat(repoDir); err != nil {
				out = append(out, Finding{
					Layer: 1, ID: "ledger-row-coherence", Pack: packName, Subject: repoDir, Status: Warn, Class: Advisory,
					Detail: "ledger row for a repo path that no longer exists (a distinct condition, not a failure)",
					Next:   fmt.Sprintf("sideshow disable %s --repo %s to retire the row if the repo is gone for good", packName, repoDir),
				})
				continue
			}
			if filepath.Base(row.StorePath) == "current" {
				out = append(out, Finding{
					Layer: 1, ID: "ledger-row-coherence", Pack: packName, Subject: repoDir, Status: Fail, Class: Structural,
					Detail: "ledger row pins `current` instead of a version dir; the ledger contract records the resolved version so version flips cannot silently retarget enabled repos",
					Next:   fmt.Sprintf("sideshow disable %s --repo %s && sideshow enable %s --repo %s", packName, repoDir, packName, repoDir),
				})
				continue
			}
			if _, err := os.Stat(row.StorePath); err != nil {
				out = append(out, Finding{
					Layer: 1, ID: "ledger-row-coherence", Pack: packName, Subject: repoDir, Status: Fail, Class: Structural,
					Detail: fmt.Sprintf("pinned store path %s is gone; every binding in this repo is dangling", row.StorePath),
					Next:   fmt.Sprintf("reinstall %s %s, or disable and re-enable against an installed version", packName, row.Version),
				})
				continue
			}
			if filepath.Base(row.StorePath) != row.Version {
				out = append(out, Finding{
					Layer: 1, ID: "ledger-row-coherence", Pack: packName, Subject: repoDir, Status: Fail, Class: Structural,
					Detail: fmt.Sprintf("row records version %s but pins store dir %s; two records of the bound version disagree", row.Version, row.StorePath),
					Next:   fmt.Sprintf("sideshow disable %s --repo %s && sideshow enable %s --repo %s", packName, repoDir, packName, repoDir),
				})
				continue
			}
			out = append(out, Finding{
				Layer: 1, ID: "ledger-row-coherence", Pack: packName, Subject: repoDir, Status: OK, Class: Structural,
				Detail: fmt.Sprintf("row pins %s and disk agrees", row.Version),
			})
		}
	}
	if rows == 0 {
		out = append(out, Finding{
			Layer: 1, ID: "ledger-row-coherence", Status: OK, Class: Structural,
			Detail: "no repo-bindings ledger rows to verify",
		})
	}
	return out
}
