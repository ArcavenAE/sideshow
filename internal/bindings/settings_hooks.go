package bindings

import "fmt"

// Settings-hooks binding (aae-orc-d3nq.52): render a pack's hook
// registrations into ONE repo settings file and remove exactly them
// on disable. The settings path is a parameter — the verbs pass
// settings.local.json at local scope (the finding-093 D3 default) and
// settings.json at project scope (consent-gated).
//
// Schema facts from the trials and the upstream tree:
//   - NO matcher key is emitted. Omitted matcher is equivalent to
//     matcher:"" (T19), and all upstream hook groups carry only a
//     hooks key.
//   - Entries carry timeout (ms) and once where the pack declares
//     them (accepted by the harness per T18).
//   - Every rule group carries _managed_by: "sideshow:<pack>" — the
//     ownership marker unmerge keys on. Foreign groups (the user's
//     own hooks, other packs) are never touched.
//   - The command string carries the inline env belt (InlineEnvBelt)
//     upstream of this writer; nothing here depends on the settings
//     env leg.

// HookEntry is one synthesized hook registration.
type HookEntry struct {
	Event   string
	Command string
	Timeout int  // milliseconds; 0 omits the key
	Once    bool // emitted only when true
}

// settingsHooksManagedBy mirrors the user-scope writer's marker
// convention (internal/distribute).
func settingsHooksManagedBy(packName string) string {
	return "sideshow:" + packName
}

// MergeHookChain upserts the pack's rule groups into the settings
// file at path: every existing group carrying this pack's marker is
// replaced by the declared entries (so re-enable converges instead of
// duplicating), and groups without the marker are preserved
// byte-for-byte in their positions' order. Returns the number of
// groups written.
func MergeHookChain(path, packName string, entries []HookEntry) (int, error) {
	settings, _, err := readSettings(path)
	if err != nil {
		return 0, err
	}
	hooks, err := hooksObject(settings, path)
	if err != nil {
		return 0, err
	}

	marker := settingsHooksManagedBy(packName)
	stripManagedGroups(hooks, marker)

	for _, e := range entries {
		if e.Event == "" || e.Command == "" {
			return 0, fmt.Errorf("hook entry missing event or command: %+v", e)
		}
		hook := map[string]any{"type": "command", "command": e.Command}
		if e.Timeout > 0 {
			hook["timeout"] = e.Timeout
		}
		if e.Once {
			hook["once"] = true
		}
		group := map[string]any{
			"hooks":       []any{hook},
			"_managed_by": marker,
		}
		list, _ := hooks[e.Event].([]any)
		hooks[e.Event] = append(list, group)
	}
	if len(hooks) > 0 {
		settings["hooks"] = hooks
	} else {
		delete(settings, "hooks")
	}
	return len(entries), writeSettings(path, settings)
}

// RemoveHookChain removes every rule group carrying the pack's marker
// from the settings file, pruning emptied event lists and an emptied
// hooks object. A missing file removes nothing. Returns the number of
// groups removed.
func RemoveHookChain(path, packName string) (int, error) {
	settings, existed, err := readSettings(path)
	if err != nil {
		return 0, err
	}
	if !existed {
		return 0, nil
	}
	hooks, err := hooksObject(settings, path)
	if err != nil {
		return 0, err
	}
	removed := stripManagedGroups(hooks, settingsHooksManagedBy(packName))
	if removed == 0 {
		return 0, nil
	}
	if len(hooks) > 0 {
		settings["hooks"] = hooks
	} else {
		delete(settings, "hooks")
	}
	return removed, writeSettings(path, settings)
}

// VerifyHookChain checks the post-write settings against the declared
// entry set — enable refuses success if the hook-entry count is short
// of the declared events (unshaping-spec validation rule 4), and
// doctor re-runs the same check.
func VerifyHookChain(path, packName string, entries []HookEntry) error {
	settings, existed, err := readSettings(path)
	if err != nil {
		return err
	}
	if !existed {
		return fmt.Errorf("hook chain missing: %s does not exist", path)
	}
	hooks, err := hooksObject(settings, path)
	if err != nil {
		return err
	}

	marker := settingsHooksManagedBy(packName)
	got := map[string]int{} // event -> managed group count
	for event, raw := range hooks {
		list, _ := raw.([]any)
		for _, g := range list {
			group, _ := g.(map[string]any)
			if group != nil && group["_managed_by"] == marker {
				got[event]++
			}
		}
	}

	want := map[string]int{}
	for _, e := range entries {
		want[e.Event]++
	}
	var missing []string
	for event, n := range want {
		if got[event] < n {
			missing = append(missing, fmt.Sprintf("%s (%d of %d)", event, got[event], n))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("hook chain in %s is short of the declared event set: %v", path, missing)
	}
	return nil
}

// hooksObject returns the settings' hooks block as a mutable map,
// failing closed when it exists with the wrong shape.
func hooksObject(settings map[string]any, path string) (map[string]any, error) {
	raw, ok := settings["hooks"]
	if !ok {
		return map[string]any{}, nil
	}
	hooks, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings %s: hooks is not an object; refusing to merge", path)
	}
	return hooks, nil
}

// stripManagedGroups deletes every rule group carrying the marker,
// pruning emptied event lists. Returns the number removed.
func stripManagedGroups(hooks map[string]any, marker string) int {
	removed := 0
	for event, raw := range hooks {
		list, ok := raw.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, g := range list {
			group, _ := g.(map[string]any)
			if group != nil && group["_managed_by"] == marker {
				removed++
				continue
			}
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	return removed
}
