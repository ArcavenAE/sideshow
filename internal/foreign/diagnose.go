package foreign

import "fmt"

// Severity grades a coexistence finding for the doctor surface.
type Severity string

const (
	Info  Severity = "INFO"
	Warn  Severity = "WARN"
	Error Severity = "ERROR"
)

// Finding is one doctor-shaped coexistence observation.
type Finding struct {
	Severity Severity
	Code     string
	Message  string
}

// Diagnose grades the coexistence posture of one repo against the
// ratified severities (aae-orc-paqn clause e, finding-093):
//
//   - dual-channel presence at MACHINE level is INFO — the supported
//     testing-the-waters posture, not a defect;
//   - a foreign identity effectively enabled in a repo where sideshow
//     bindings are active is ERROR — same-repo double-dispatch, two
//     hook chains against one .factory state (verified live, trial
//     T11), refused on class-1 grounds;
//   - a user-scope enable of a per_repo_required pack is ERROR — the
//     containment mandate;
//   - orphaned enables are WARN — silent to the harness (trial T14),
//     visible only here.
//
// sideshowActive reports whether sideshow bindings for the pack are
// effective in this repo (the caller reads it from the repo-bindings
// ledger once .7 lands; until then from the presence of materialized
// bindings).
func Diagnose(c *Census, view *RepoView, sideshowActive, perRepoRequired bool) []Finding {
	var out []Finding

	if len(c.Installs) > 0 && sideshowActive {
		out = append(out, Finding{
			Info, "dual-channel-machine",
			fmt.Sprintf("%s is installed via %d foreign identity/identities beside the sideshow channel; machine-level coexistence is supported", c.Pack, len(c.Installs)),
		})
	}

	if perRepoRequired {
		for id, e := range c.userEnables {
			if plugin, _ := splitIdentity(id); plugin == c.Pack && e.value {
				out = append(out, Finding{
					Error, "user-scope-enable",
					fmt.Sprintf("%s is enabled at USER scope (%s): a per-repo-required pack would activate in every repo on this machine; suppress or move the enable per repo", id, e.path),
				})
			}
		}
	}

	if sideshowActive {
		for _, id := range view.EffectivelyEnabled {
			out = append(out, Finding{
				Error, "same-repo-double-dispatch",
				fmt.Sprintf("%s is effectively enabled in %s where sideshow bindings are active: two hook chains would fire per event against one .factory state; refuse enable/adopt until one channel is suppressed here", id, view.RepoDir),
			})
		}
	}

	for _, o := range view.Orphans {
		state := "disabled"
		if o.Enabled {
			state = "enabled"
		}
		out = append(out, Finding{
			Warn, "orphaned-enable",
			fmt.Sprintf("%s is %s at %s scope (%s) with no install behind it; the harness is silent about this entry", o.Identity, state, o.Scope, o.Path),
		})
	}

	return out
}

// RefusalOptions is the consent-gated menu offered when enable or
// adopt refuses on same-repo double-dispatch (paqn clause c). Nothing
// here acts; the caller presents and the operator chooses.
func RefusalOptions(identity, repoDir string) string {
	return fmt.Sprintf(`another %s channel is effectively enabled in this repo (%s).
Sideshow will not run two dispatch chains against one .factory state. Options:
  1. Convert this repo to the sideshow channel (sideshow adopt; reversible, dry-runnable).
  2. Suppress the foreign identity in THIS repo only (one settings line, no uninstall):
       {"enabledPlugins": {%q: false}} in .claude/settings.local.json
  3. Abort and leave the repo as it is.
Sideshow never auto-disables or auto-uninstalls a foreign install.`, identity, repoDir, identity)
}
