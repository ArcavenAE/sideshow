package enable

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/bindings"
	"github.com/ArcavenAE/sideshow/internal/ledger"
)

// Activate and Deactivate are the consented persona flip
// (aae-orc-d3nq.60), porting the logic of the upstream
// activate/deactivate skills without their store writes. Enable
// deliberately does not touch the agent key (the upstream
// no-hijack-on-enable property, kept); running activate is the
// explicit opt-in that makes a plain session the pipeline driver.
//
// Reframes from the upstream pair:
//   - activated_platform / activated_plugin_version are NOT written
//     into settings. The ledger row is the single record of both;
//     the platform drift warning reads it from there.
//   - The agent value is the bound (prefixed) bare name per trial
//     T20 — no namespace-qualified form exists on this channel.
//   - Nothing here can write into the store: the upstream pair's
//     hooks.json copy/remove steps are the enable/disable verbs'
//     settings-chain work.

// defaultAgentSuffix is the upstream persona the flip selects.
const defaultAgentSuffix = "orchestrator"

// Activate sets the repo's default agent to the pack's bound
// orchestrator (or --agent override). Requires an enabled ledger row;
// refuses to overwrite an agent key it does not own.
func Activate(opts Options, agentOverride string) error {
	if err := opts.normalize(); err != nil {
		return err
	}
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		return err
	}
	row := led.RepoRow(opts.RepoDir, opts.Pack)
	if row == nil {
		return fmt.Errorf("%s is not enabled in %s; run 'sideshow enable %s' first (activate only flips the persona)", opts.Pack, opts.RepoDir, opts.Pack)
	}

	prefix := opts.Prefix
	if prefix == "" {
		prefix = opts.Pack
	}
	agent := agentOverride
	if agent == "" {
		agent = prefix + "-" + defaultAgentSuffix
	}
	if !strings.HasPrefix(agent, prefix+"-") {
		return fmt.Errorf("agent %q does not carry the %s- binding prefix; activate only sets agents this pack ships", agent, prefix)
	}

	// Platform drift warning (upstream re-activation check, reframed
	// against the ledger record).
	if detected, derr := detectPlatform(); derr == nil && row.Platform != "" && row.Platform != detected {
		fmt.Fprintf(os.Stderr, "warning: repo was enabled for platform %s but this host is %s; the bound dispatcher may not run here — re-enable on this host (disable + enable) to rebind\n",
			row.Platform, detected)
	}

	settings := settingsFile(opts.RepoDir, bindings.RepoScope(row.SettingsScope))
	changed, prev, err := setAgentKey(settings, agent, prefix)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Printf("agent already %s in %s; nothing to do\n", prev, opts.RepoDir)
		return nil
	}
	fmt.Printf("activated: default agent is now %s in %s (scope %s)\n", agent, opts.RepoDir, row.SettingsScope)
	fmt.Println("Reverse with 'sideshow deactivate'; this only affects this repo.")
	return nil
}

// Deactivate removes the default-agent key, guarded: if the key does
// not point at one of this pack's bound (prefixed) agents, it is
// someone else's choice and deactivate refuses rather than cleaning
// it (the upstream step-2 guard, re-expressed against the .51
// naming).
func Deactivate(opts Options) error {
	if err := opts.normalize(); err != nil {
		return err
	}
	led, err := ledger.Load(opts.LedgerPath)
	if err != nil {
		return err
	}
	prefix := opts.Prefix
	if prefix == "" {
		prefix = opts.Pack
	}
	scope := bindings.ScopeLocal
	if row := led.RepoRow(opts.RepoDir, opts.Pack); row != nil {
		scope = bindings.RepoScope(row.SettingsScope)
	}

	settings := settingsFile(opts.RepoDir, scope)
	removed, prev, err := clearAgentKey(settings, prefix)
	if err != nil {
		return err
	}
	switch {
	case prev == "":
		fmt.Printf("no default agent set in %s; nothing to do\n", opts.RepoDir)
	case !removed:
		return fmt.Errorf("default agent is %q, which is not a %s- agent; refusing to remove a persona this pack did not set", prev, prefix)
	default:
		fmt.Printf("deactivated: default agent %s removed in %s (bindings stay enabled; 'sideshow disable' removes those)\n", prev, opts.RepoDir)
	}
	return nil
}

// setAgentKey writes the agent key, refusing a foreign value.
// Returns changed=false when the key already holds agent.
func setAgentKey(path, agent, prefix string) (changed bool, prev string, err error) {
	settings, _, err := readSettingsJSON(path)
	if err != nil {
		return false, "", err
	}
	if raw, ok := settings["agent"]; ok {
		s, _ := raw.(string)
		if s == agent {
			return false, s, nil
		}
		if !strings.HasPrefix(s, prefix+"-") {
			return false, s, fmt.Errorf("default agent is already %q (not a %s- agent); sideshow never overwrites a persona choice it does not own — remove it yourself first if intended", s, prefix)
		}
	}
	settings["agent"] = agent
	return true, agent, writeSettingsJSON(path, settings)
}

// clearAgentKey removes the agent key only when it carries the
// pack's prefix. Returns prev="" when no key exists.
func clearAgentKey(path, prefix string) (removed bool, prev string, err error) {
	settings, existed, err := readSettingsJSON(path)
	if err != nil {
		return false, "", err
	}
	if !existed {
		return false, "", nil
	}
	raw, ok := settings["agent"]
	if !ok {
		return false, "", nil
	}
	s, _ := raw.(string)
	if !strings.HasPrefix(s, prefix+"-") {
		return false, s, nil
	}
	delete(settings, "agent")
	return true, s, writeSettingsJSON(path, settings)
}

// readSettingsJSON / writeSettingsJSON mirror the bindings package's
// settings helpers (read-merge-write, fail closed on malformed).
func readSettingsJSON(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, false, nil
		}
		return nil, false, fmt.Errorf("read settings %s: %w", path, err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, false, fmt.Errorf("parse settings %s (refusing to merge into a file that cannot round-trip): %w", path, err)
	}
	if settings == nil {
		settings = map[string]any{}
	}
	return settings, true, nil
}

func writeSettingsJSON(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write settings %s: %w", path, err)
	}
	return nil
}
