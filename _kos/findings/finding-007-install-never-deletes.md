# finding-007: install copies over an existing store version and never deletes

**Date:** 2026-10-02
**Subject:** `InstallFromLocal` over an already-installed version
**Occasion:** the same end-to-end run as finding-006
**Evidence:** `internal/pack/pack.go` `InstallFromLocal` at `1b5d716` (a WalkDir copy with no removal step);
the overwrite run in finding-006, where a manifest from the first install survived an overwrite from a tree
that had none.

## Why this is worth writing down

A reinstall reads as "the store now holds this source". It does not: the store holds the union of every
source installed at that version. That changes what a doctor finding means and what a reinstall can fix.

## What was observed

- The copy walks the source and writes each file; nothing removes store files the source does not have.
- In finding-006's overwrite, the source had no `file-manifest.csv`, and doctor still read one: the file
  from the earlier install had survived. That is why `store-file-manifest` caught the edit.
- The same property means a file dropped between two builds of one version stays in the store after a
  reinstall, and binding sync may keep serving it.

## Open

Whether a reinstall should replace the version directory (stage, then swap) rather than copy over it. That
is install-path design, close to mobz8's refuse-or-force question, and is not decided here. doctor's
`unlisted` count in `store-file-manifest` is the current way to see leftovers.
