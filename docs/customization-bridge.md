# Customization Bridge — `_<pack>-custom/` ↔ upstream `_<pack>/custom/`

**Status:** committed direction (boundary doc). Implementation tracked
in `aae-orc-mkpo`. Charter overlay-spec work (`aae-orc-10vq`) depends
on this resolution.

## The collision

Two concepts that were once orthogonal now share a name:

| Convention | Owner | Purpose | Persistence |
|---|---|---|---|
| `_<pack>-custom/` | sideshow | Per-repo overlay over the user-scope pack content. Sideshow's original convention from session-029. | **Checked in** to the consumer repo. |
| `_<pack>/custom/` | upstream pack runtime | Pack-internal customization layer (TOML configs, agent overrides, workflow tweaks) the runtime resolver merges with pack defaults. | Implicitly lives next to pack content. |

Concrete instance: bmad 6.4.0 introduced TOML-based agent and workflow
customization with a four-file architecture rooted at `_bmad/custom/`
plus a `bmad-customize` skill that authors files there interactively.
Before 6.4 there was no upstream customization surface; sideshow's
`_<pack>-custom/` was the only customization concept and there was no
collision.

## Why this matters

If sideshow does nothing:

1. A user runs `/bmad-customize` to override an agent. The skill writes
   to `_bmad/custom/agents/...toml`.
2. `_bmad/` is **gitignored** at the project root (per
   `consumer-repo-convention.md`) — the customization either lands in
   gitignored space (lost on commit) or in `~/.local/share/sideshow/`
   (globally polluting every consumer of that pack version).
3. On `sideshow install bmad@<next-version>`, the user-scope pack
   directory is replaced. Customization vanishes silently.

In all three cases the user ends up surprised. The customization is
either lost, scoped wrong, or non-portable across machines.

## The bridge

At `sideshow project init <pack>` (or equivalent) time, sideshow
creates a sentinel-tracked symlink in the consumer repo:

```
<project>/_bmad/custom/  →  ../_bmad-custom/
```

Effects:

- `_bmad/` exists at the project root as a directory containing **only
  sideshow-managed symlinks** (today: just `custom/`). It is still
  gitignored.
- `_bmad-custom/` is **checked in**, unchanged from the existing
  convention.
- Upstream's `bmad-customize` skill writes to `_bmad/custom/...toml`,
  which the symlink resolves to `_bmad-custom/...toml`. The actual
  bytes live in checked-in territory.
- `bmad`'s runtime resolver (which expects to read from
  `_bmad/custom/`) gets exactly what it expects. No bmad code change.
- `sideshow install bmad@<next-version>` swaps the user-scope pack
  content but does NOT touch the symlink or the `_bmad-custom/` tree.
  Customization survives version transitions for free.

## Why `_<pack>-custom/` keeps its name

Sideshow's `_<pack>-custom/` convention is **broader** than upstream's
`_<pack>/custom/`. Sideshow customization may include:

- Files that don't fit upstream's TOML schema (per-project README
  fragments, decision-record templates, prompt overrides).
- Customization for packs that have **no upstream customization
  surface at all** (e.g. spectacle, future packs).
- Sideshow-managed scaffolding the upstream pack doesn't know exists.

The bridge applies *only* where an upstream pack defines a `custom/`
sub-convention. Other packs use `_<pack>-custom/` exactly as before,
with no symlink, no bridge, no collision.

## Boundary table

| Path | Owner | Persistence | Bridge applies? |
|---|---|---|---|
| `_<pack>-custom/` (project root) | sideshow + user | checked in | source of truth |
| `_<pack>/custom/` (project root, symlink) | sideshow scaffolding | gitignored symlink → `../_<pack>-custom/` | applies for packs with a custom sub-convention (bmad 6.4+) |
| `_<pack>/` (project root, beyond the `custom/` symlink) | nobody — should not exist | gitignored | n/a |
| `~/.local/share/sideshow/packs/<pack>/<v>/custom/` | nobody — pack content is immutable | should be empty in installed packs | n/a |
| `~/.local/share/sideshow/packs/<pack>/<v>/` | sideshow (immutable) | user-scope, version-isolated | n/a |

## Implementation notes (for `aae-orc-mkpo`)

1. **Detection.** A pack opts into the bridge by declaring it in
   `pack.yaml`:

   ```yaml
   schema_version: 1
   name: bmad
   distribute:
     custom_bridge:
       upstream_path: _bmad/custom
       per_repo_dir: _bmad-custom
   ```

   No `custom_bridge` declaration → no bridge created. Backward
   compatible with packs that don't ship a custom sub-convention.

2. **Install action (`sideshow project init <pack>` /
   `--scope project`):**
   - Create `<project>/_bmad/` if absent (gitignored shim dir).
   - Create `<project>/_bmad-custom/` if absent (checked in).
   - Create symlink `<project>/_bmad/custom/ → ../_bmad-custom/` if
     absent. If a regular file or non-matching symlink exists at
     that path, refuse and report (per the per-repo overlay safety
     model).
   - Append `_bmad/` to `.gitignore` (idempotent, marker-bracketed).

3. **Doctor check.** `sideshow doctor` (`aae-orc-xteh`) verifies the
   symlink exists and points to the right target. `--fix` recreates
   if missing.

4. **Removal.** `sideshow project remove <pack>` removes the symlink
   but leaves `_<pack>-custom/` (it's the user's data) and the
   gitignore marker (let the user clean up explicitly).

5. **Cross-platform.** Symlinks work on macOS, Linux, and modern
   Windows (Developer Mode or admin). For Windows-without-symlink
   environments, fall back to a directory-junction or — last resort —
   a one-shot `bmad-customize` interceptor that rewrites paths.
   Decide when a Windows user reports the problem; YAGNI today.

## Why a symlink and not a write-through interceptor

Considered: have sideshow intercept writes to `_bmad/custom/...` and
redirect to `_<pack>-custom/`. Rejected because:

- Sideshow would need to be a runtime presence, not a one-shot CLI.
- bmad's writer is upstream code; intercepting its writes is fragile.
- Symlink is OS-native, transparent to upstream, and free.

The symlink approach also matches the existing per-repo overlay model
(symlink-or-copy is already a sideshow primitive in
`internal/distribute/`).

## Why the bridge isn't bedrock yet

This doc is committed direction, not bedrock, because:

1. `aae-orc-mkpo` (the implementation issue) hasn't shipped.
2. We have no empirical evidence that the symlink works under bmad's
   actual 6.4 customization workflow — finding-035 only documented the
   collision, not a fix attempt.
3. Windows behavior is unverified (see implementation note 5).

Promotion path: ship `aae-orc-mkpo`, run `bmad-customize` end-to-end
on a sideshow-managed bmad@6.4+ install, validate the customization
survives a version switch (`sideshow install bmad@<next>`), then
graduate this doc's content into charter bedrock.

## Related

- orc charter F24 — sideshow install + audit infrastructure.
- `aae-orc-5g9m` — this doc.
- `aae-orc-mkpo` — implementation.
- `aae-orc-10vq` — overlay artifact spec (depends on this boundary).
- `_kos/findings/finding-035-bmad-65-sideshow-readiness.md` — where
  the collision was first observed.
- bmad 6.4.0 release notes — TOML customization framework, four-file
  architecture, `bmad-customize` skill (#2284, #2285, #2287, #2289).
- `docs/consumer-repo-convention.md` — the broader per-repo
  convention this bridge plugs into.
