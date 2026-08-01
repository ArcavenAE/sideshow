package doctor

// layer5Checks is the known-defects surface. Both inputs are
// unshipped, so both checks report unavailable with the tickets that
// supply them. Doctor is offline by design in this phase: no network
// call anywhere, so a future feed lands as a cached file read, not a
// fetch.
func layer5Checks() []Check {
	return []Check{
		{ID: "known-defects", Layer: 5, Run: checkKnownDefects},
		{ID: "unapplied-overlays", Layer: 5, Run: checkUnappliedOverlays},
	}
}

func checkKnownDefects(_ *Context) []Finding {
	return []Finding{{
		Layer: 5, ID: "known-defects", Status: Unavailable, Class: Structural,
		Detail: "no known-defects feed exists yet; installed versions cannot be cross-referenced against publisher advisories",
		Next:   "aae-orc-ztg5 (publisher feed), then aae-orc-e1jj (this check's implementation)",
	}}
}

func checkUnappliedOverlays(_ *Context) []Finding {
	return []Finding{{
		Layer: 5, ID: "unapplied-overlays", Status: Unavailable, Class: Structural,
		Detail: "no overlay artifact has ever been published and no applied-overlay record exists; the check has no input at all",
		Next:   "aae-orc-10vq (overlay artifact spec) and aae-orc-333y (applied-overlay record in the lockfile)",
	}}
}
