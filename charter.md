# sideshow Charter

Content pack manager for Arcaven's BYOA platform. Installs and manages
multi-source content packs (BMAD, VSDD, etc.) across a fleet of
orchestrators and subrepos. Session-029 graduated the original probe
question — "can a content pack manager eliminate BMAD duplication
across repos?" — to shipped direction. Active frontier is now about
*how* sideshow evolves into install + audit infrastructure for the
platform.

## Bedrock

- Go CLI, single binary.
- **user-install** shape: content packs installed to
  `~/.local/share/sideshow/packs/{name}/{version}/` (user-scoped; true
  global/OS-wide is not a goal).
- **orc-declared** shape: `aae-orc/sideshow.toml` at orchestrator root
  declares which subrepos get which packs at which versions.
- Per-repo customization via `_{pack}-custom/` (checked in). For packs
  that ship an upstream customization surface at `_{pack}/custom/`
  (bmad 6.4+), sideshow renders a symlink
  `_{pack}/custom/ → ../_{pack}-custom/` so upstream's runtime
  resolver and authoring skills (`bmad-customize`) write into the
  checked-in per-repo dir transparently. See
  [`docs/customization-bridge.md`](docs/customization-bridge.md).
- Per-repo output via `_{pack}-output/` (**checked in** — corrected
  session-029). Previously labeled "gitignored" which conflated
  scaffolding with deliverables. The dir holds agent-produced
  artifacts (PRDs, stories, architectural decisions) which are the
  project's deliverables.
- The pack content itself (`_{pack}/`) is installed at user-scope
  (`~/.local/share/sideshow/packs/`) and **gitignored** from consumer
  repos. See `docs/consumer-repo-convention.md` for the full
  check-in/gitignore table.
- Commands synced to `~/.claude/commands/{pack}/` via idempotent
  rewrites with fallback-resolution footer (cwd-relative first, then
  user-install pack path) so slash commands work at orchestrator roots
  that have no pack directory.
- **Gitflow not adopted yet** — trunk `main` until the distribution
  story (cosign + GitHub Releases) goes live.

## Frontier (session-029 commitments)

The following shapes are committed direction but not yet implemented.
Each is tracked as a bd issue under the orc-level registry. See also
orc `charter.md` F24 and `_kos/nodes/frontier/question-sideshow-install-architecture.yaml`.

- **6.3.0-first.** Sideshow support targets bmad 6.3.0 and forward.
  6.2.2 is legacy; we will not build new infrastructure against it.
  `aae-orc-2lma` shipped session-029 (skill-dir bindings for the 6.3.0
  `.claude/skills/` distribution); subsequent binding work for 6.4+
  customization tracked under the bridge entry below.
- **Customization bridge for upstream-defined `custom/` surfaces.**
  bmad 6.4 introduced `_bmad/custom/` TOML customization. Sideshow
  bridges this to its per-repo `_<pack>-custom/` convention via a
  gitignored symlink `_bmad/custom/ → ../_bmad-custom/` created at
  project init. Customization survives version switches (lives in
  checked-in territory). Other packs without a `custom/` sub-
  convention are unaffected. Tracked by `aae-orc-5g9m` (this
  boundary doc) and `aae-orc-mkpo` (implementation). Required before
  `aae-orc-10vq` overlay spec ships. See
  [`docs/customization-bridge.md`](docs/customization-bridge.md).
- **Shared pack store + alternatives-style escape.** One
  `~/.local/share/sideshow/packs/` store across orchestrators; when two
  orchestrators pin different versions, fall back to the alternatives
  model (we kept from session-029 party 1 as an escape hatch).
- **sideshow.lock at orc root** (TOML, Cargo-model precedence;
  `sideshow lock --check` as CI gate). Tracked by `aae-orc-333y`.
- **Five-layer doctor.** Sideshow-native integrity, cwd discoverability
  (warn-only), fleet drift vs lockfile, known-defects check. Pack-
  declared validation deferred to probe. `aae-orc-xteh` + dependents.
- **Frozen-composition publishing pipeline.** CI resolves multi-source
  packs (bmad is 5 modules from 5 sources) and emits cosign-signed
  tarballs + cosign attest attestations (SLSA/in-toto) with upstream
  provenance. Lives at private `ArcavenAE/sideshow-packs` with a
  stripped-down public mirror later (some source materials cannot be
  redistributed). `aae-orc-ibil`.
- **Signing: cosign + Sigstore.** Chosen over minisign specifically for
  the Rekor transparency log — the compliance artifact that proves
  signing time (minisign signatures can't disprove backdating).
- **Immutable base + signed overlays + re-issue.** Never mutate
  published artifacts. Overlays patch point-release defects; re-issue
  handles structural migrations (6.2.2 → 6.3.0 is a re-issue — diff
  would require 2600+ file ops per finding-025). Tracked by
  `aae-orc-10vq` (overlay spec) + `aae-orc-ztg5` (known-defects
  registry).
- **Unified Binding abstraction** (`aae-orc-f13j`). `.claude/commands/`
  (bmad 6.2.2) and `.claude/skills/` (bmad 6.3.0) are concrete
  instances of a `Binding` interface — along with future bindings for
  Cursor, Windsurf, aider, and curated variants per `aae-orc-oy8c`.
- **Pluggable source backends** (`aae-orc-mezl`). `pack.yaml
  install.source` is a typed backend reference. Initial backends:
  GitHub Releases (default), git-clone-at-sha, apt-style signed index,
  CDN object storage, Claude plugin marketplace. Plugin interface:
  fetch, verify, listAvailable.
- **Source-material provenance chain** (`aae-orc-entu`). Every
  artifact sideshow produces carries a signed attestation tracing
  every upstream source material (git commits, npm shas, URLs +
  checksums). Implemented via cosign attest + SLSA/in-toto.
  Load-bearing requirement, not a nice-to-have.
- **Schema versioning rule** (`aae-orc-xe7l`). Every declarative
  contract sideshow ships declares `schema_version:`. See
  `docs/schema-versioning.md`.
- **Cross-orchestrator precedence** (`aae-orc-59dt`). Shared store by
  default; alternatives escape when orchestrators diverge.

## Graveyard

- **"Global" as the name for user-scoped install.** Session-029 user
  correction — true global is OS-wide and we have no purpose for that.
  Renamed user-install everywhere.
- **Build sideshow infrastructure against bmad 6.2.2.** Session-029
  sequencing decision. 6.3.0 is the target; 6.2.2 is legacy.
- **Minisign for signing.** Rejected in session-029 in favor of cosign
  because the Sigstore transparency log (Rekor) is the compliance
  artifact the signature alone cannot be.
- **npm installer on user machines.** Priya's hard rule in
  session-029 party 2: never execute untrusted JS on a user machine.
  npm resolution runs in controlled CI only.
- **update-alternatives-style side-by-side as default.** Considered
  and rejected as the default; kept as escape hatch only when
  orchestrators pin divergent versions.
