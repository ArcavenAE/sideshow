package weave

import "gopkg.in/yaml.v3"

// applyUpstreamPatches re-applies project-local edits to installer-owned files.
//
// Deliberately unimplemented. finding-029's per-project table lists ccmp as
// applying patches C1 C2 E1 E2 I1 I3; reading ccmp's script shows it contains
// no patch code at all, so the first port does not exercise this operation and
// there is no verified prior art to port from. Writing the schema now, from the
// table rather than from working code, is how the table's error would propagate
// into the engine.
//
// The declaration accepts `none` (or omission) and refuses anything else with a
// message naming the bead, so a project cannot quietly believe its patches are
// being applied. One of the four remaining scripts (aae-orc-ll31, 8hu9, ojmn,
// l2w3) will supply the shape.
func applyUpstreamPatches(d *Declaration, repoRoot string, opts Options) []Action {
	node := d.ApplyUpstreamPatches
	if node.IsZero() {
		return nil
	}

	// `none` and the empty string are the only accepted scalars.
	if node.Kind == yaml.ScalarNode && (node.Value == "none" || node.Value == "" || node.Value == "~" || node.Value == "null") {
		return nil
	}

	return []Action{{
		Type:    "patch",
		Name:    "apply_upstream_patches",
		Outcome: Failed,
		Detail: "upstream patching is not implemented; declare `none` until a " +
			"port supplies the shape (aae-orc-ll31, 8hu9, ojmn, l2w3)",
	}}
}
