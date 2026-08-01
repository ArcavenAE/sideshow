package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// layer4Checks is the fleet-drift set: what sideshow distributed
// across repos, verified against its receipts. The pinned-version
// comparison against sideshow.lock stays unavailable until the
// lockfile ships (aae-orc-333y); receipt re-hashing and version skew
// are the honest signals that need no new input.
func layer4Checks() []Check {
	return []Check{
		{ID: "receipt-drift", Layer: 4, Run: checkReceiptDrift},
		{ID: "version-skew", Layer: 4, Run: checkVersionSkew},
		{ID: "lock-pins", Layer: 4, Run: checkLockPins},
	}
}

// checkReceiptDrift re-hashes every receipted artifact that carries a
// checksum against disk (the sideshow#92 signal). A missing receipted
// file is structural: the receipt claims ownership of a path that is
// gone. A changed one is advisory: a user edit is a deliberate act,
// not malformed state. The skip census rides along so "no findings"
// cannot read as "no drift".
func checkReceiptDrift(ctx *Context) []Finding {
	if ctx.RegistryErr != nil {
		return []Finding{{
			Layer: 4, ID: "receipt-drift", Status: Unavailable, Class: Structural,
			Detail: fmt.Sprintf("registry does not load: %v", ctx.RegistryErr),
			Next:   fmt.Sprintf("inspect %s", registryPath()),
		}}
	}
	var out []Finding
	receipts, current, drifted := 0, 0, 0
	skips := forEachReceiptRepo(ctx, func(repoDir string, pd pack.PackDistribution) {
		for _, a := range pd.Artifacts {
			if a.Checksum == "" || a.Path == "" {
				continue
			}
			receipts++
			path := filepath.Join(repoDir, a.Path)
			got, err := sha256File(path)
			if err != nil {
				out = append(out, Finding{
					Layer: 4, ID: "receipt-drift", Pack: pd.Pack, Subject: path, Status: Warn, Class: Structural,
					Detail: "receipted artifact is gone; the receipt claims sideshow placed it here",
					Next:   "re-run 'sideshow init' for this project, or retire the receipt by redistributing",
				})
				continue
			}
			want := strings.TrimPrefix(a.Checksum, "sha256:")
			if got != want {
				drifted++
				out = append(out, Finding{
					Layer: 4, ID: "receipt-drift", Pack: pd.Pack, Subject: path, Status: Warn, Class: Advisory,
					Detail: fmt.Sprintf("differs from the %s receipt (a user edit is a deliberate act; distribution preserves it)", pd.Version),
					Next:   "diff against the pack copy if the divergence is unintended",
				})
				continue
			}
			current++
		}
	})

	// Coverage: what the drift signal could not see, and why the
	// receipt set under-covers even where repos are reachable.
	for _, s := range skips {
		out = append(out, Finding{
			Layer: 4, ID: "receipt-drift", Subject: s.Subject, Status: Warn, Class: Advisory,
			Detail: "skipped: " + s.Reason,
			Next:   "remove dead registry entries by redistributing, or ignore if the checkout is expected to be absent",
		})
	}
	if receipts == 0 {
		out = append(out, Finding{
			Layer: 4, ID: "receipt-drift", Status: Unavailable, Class: Structural,
			Detail: "no checksummed receipts exist; there is no drift signal (which is not the same as no drift). Two known under-coverage causes: RecordResults keeps only wrote/merged actions and replaces artifact lists wholesale, and 'sideshow project init' writes no receipts",
			Next:   "sideshow#92 tracks receipt fidelity; run 'sideshow init' distribution to create receipts",
		})
		return out
	}
	out = append(out, Finding{
		Layer: 4, ID: "receipt-drift", Status: OK, Class: Structural,
		Detail: fmt.Sprintf("%d checksummed receipts: %d current, %d drifted, %d skipped subtrees (coverage caveat: partial distribute runs drop receipts for files they skipped; sideshow#92)", receipts, current, drifted, len(skips)),
	})
	return out
}

// checkVersionSkew compares every receipt version and ledger row
// version against the pack's active store version. Advisory: running
// behind is a state to know about, not malformed state.
func checkVersionSkew(ctx *Context) []Finding {
	active := map[string]string{}
	for _, p := range ctx.Packs {
		active[p.Name] = p.Version
	}
	var out []Finding
	forEachReceiptRepo(ctx, func(repoDir string, pd pack.PackDistribution) {
		if v, ok := active[pd.Pack]; ok && v != pd.Version {
			out = append(out, Finding{
				Layer: 4, ID: "version-skew", Pack: pd.Pack, Subject: repoDir, Status: Warn, Class: Advisory,
				Detail: fmt.Sprintf("distributed at %s; active store version is %s", pd.Version, v),
				Next:   "re-run 'sideshow init' distribution to bring this repo current",
			})
		}
	})
	if ctx.Ledger != nil {
		for repoDir, packs := range ctx.Ledger.Repos {
			for packName, row := range packs {
				if ctx.PackFilter != "" && packName != ctx.PackFilter {
					continue
				}
				if _, err := os.Stat(repoDir); err != nil {
					continue // ledger-row-coherence owns dead rows
				}
				if v, ok := active[packName]; ok && v != row.Version {
					out = append(out, Finding{
						Layer: 4, ID: "version-skew", Pack: packName, Subject: repoDir, Status: Warn, Class: Advisory,
						Detail: fmt.Sprintf("enabled at %s; active store version is %s (enables pin deliberately; skew is information, not an error)", row.Version, v),
						Next:   fmt.Sprintf("sideshow disable %s --repo %s && sideshow enable %s --repo %s to move the pin", packName, repoDir, packName, repoDir),
					})
				}
			}
		}
	}
	if len(out) == 0 {
		out = append(out, Finding{
			Layer: 4, ID: "version-skew", Status: OK, Class: Advisory,
			Detail: "every receipt and ledger row matches its pack's active store version",
		})
	}
	return out
}

// checkLockPins is the pinned-version comparison. No sideshow.lock
// exists yet, so the check reports its missing input by name instead
// of pretending the fleet is pinned.
func checkLockPins(_ *Context) []Finding {
	return []Finding{{
		Layer: 4, ID: "lock-pins", Status: Unavailable, Class: Structural,
		Detail: "sideshow.lock is not implemented; the pinned-version comparison has no input. Version skew against the installed store is reported instead",
		Next:   "aae-orc-333y (lockfile spec and feature)",
	}}
}
