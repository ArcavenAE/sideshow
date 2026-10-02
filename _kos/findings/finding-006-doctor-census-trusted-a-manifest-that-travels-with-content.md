# finding-006: doctor's content census trusted a manifest that travels with the content it checks

**Date:** 2026-10-02
**Subject:** how `sideshow doctor` decides an installed store version is intact
**Occasion:** aae-orc-xorml half 2 (sideshow#138, #139) and aae-orc-mobz8
**Evidence:** `internal/doctor/layer1.go` `checkContentCensus` at `c33fdd2`; an end-to-end run in a fresh
store with a bmad 6.12.0 pack built after sideshow-packs#40; the operator ruling relayed on 2026-09-30.

## Why this is worth writing down

An integrity check is only as good as the reference it compares against. If the reference arrives with the
content, replacing the content replaces the reference too, and the check passes.

## What was observed

- `store-content-census` reads bmad's own `_config/files-manifest.csv`. That file is part of the pack
  content, so an `install --from` over an installed version (mobz8's case) brings or keeps a census that
  agrees with whatever was copied. mobz8 recorded `store-content-census [ok]` after such an overwrite.
- #139 added `store-file-manifest`, which recomputes every store file against the pipeline's
  `file-manifest.csv` (inside the tarball since sideshow-packs#40). In a fresh store: a clean install reports
  `2023 files match`; an overwrite from a copy with one edited `SKILL.md` and no manifest reports
  `1 differ (first: .claude/skills/bmad-agent-architect/SKILL.md)` while `store-freeze` still reports ok;
  the published r2 reports unavailable.

## The remaining gap, and the ruling

The pipeline manifest is still only as trustworthy as the install source: an overwrite from a tree that
ships its own consistent `file-manifest.csv` is not detected. sideshow does not see the release signature
or `install.meta` until install fetches and verifies signed tarballs (aae-orc-wk92). The operator ruled
"yes default" on 2026-09-30: ship #139 as is, and anchoring to the signed copy waits on wk92.
