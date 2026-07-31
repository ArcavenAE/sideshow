# Unshaping spec: install-unit inventory and translation contract

Status: v0.1.0, ratified decisions D1-D5 encoded (aae-orc finding-094 +
ratification addendum, 2026-07-31). Owning bead: aae-orc-d3nq.47.

## What unshaping is

A claude plugin is an upstream SOURCE FORMAT, not a delivery mechanism.
Unshaping takes a plugin-shaped tree and removes the limiting shape:
sideshow enables the pack by writing repo bindings into one named
repository, and creates no harness plugin state of any kind (no
marketplace, no installed_plugins.json entry, no enabledPlugins key,
no plugin cache). The full contract is finding-094; this document is
the translation layer that makes it mechanical.

The one principle every disposition below derives from:

> **Materialize the discovery surface; store-reference the engine.**

The harness discovers skills, agents, and settings hooks by location
inside the repo, so those units must exist there. Everything else the
pack reaches through `${CLAUDE_PLUGIN_ROOT}` or an engine-relative
path, and stays in the immutable signed store, referenced by absolute
path through two repo artifacts: the settings env shim (trial T15:
the env block reaches hook execution, the Bash tool, and subagent
processes) and the engine-path compat symlink (decision D2).

## Disposition vocabulary

Every unit of an upstream tree gets exactly one disposition:

| Disposition | Meaning |
|---|---|
| `materialize` | Copied or symlinked into the repo per the D1 strategy (symlink at local scope; verified full copy at project scope). Named per the binding-prefix policy. Recorded in the per-repo ownership manifest; removed exactly on disable. |
| `store-reference` | Never enters the repo. Reached at runtime by absolute path through the env shim or the compat symlink. The store copy is the only copy; exec bits live there. |
| `exclude` | Not part of the consumable at all. Each exclusion is priced in the divergence register (aae-orc-d3nq.55). |
| `replace` | The upstream unit is excluded AND sideshow ships its own functional replacement (aae-orc-d3nq.60). |

Repo artifacts that come from sideshow rather than the upstream tree
(env shim, compat symlink, settings hook entries, ownership manifest,
gitignore entries) are declared separately; they are derivatives,
recorded in the rewrite manifest (aae-orc-d3nq.61).

**Deny by default.** Nothing materializes by accident of a copy
filter: every top-level entry in the pack tree must match exactly one
declared unit, and the build fails on an unmatched entry. A new
upstream release that adds a top-level directory fails the
census-diff gate (aae-orc-d3nq.59) until a human adjudicates its
disposition.

**Precedence.** Longest declared path wins: `skills/activate/` under
a materialized `skills/` is excluded because the deeper declaration
overrides the parent.

## The declarative unshape block

Lives in the pack.yaml that sideshow-packs emits into the signed
tarball; consumed by `sideshow enable`. Schema versioned per the
fleet rule (aae-orc-xe7l).

```yaml
unshape:
  schema_version: "0.1.0"
  mechanism: repo-bindings        # D4; must be in knownMechanisms
  root_env: CLAUDE_PLUGIN_ROOT    # the name kept, the value set (T15)
  units:
    - path: skills/
      disposition: materialize
      target: .claude/skills/
      transforms: [binding-prefix, namespace-rewrite]
    - path: skills/activate/
      disposition: replace
      replacement: sideshow-enable      # aae-orc-d3nq.60
    - path: skills/deactivate/
      disposition: replace
      replacement: sideshow-disable     # aae-orc-d3nq.60
    - path: agents/
      disposition: materialize
      target: .claude/agents/
      transforms: [binding-prefix, namespace-rewrite]
    - path: hooks/
      disposition: store-reference
      register: settings-hooks          # aae-orc-d3nq.52
    - path: tests/
      disposition: exclude
      reason: divergence:in-repo-self-test   # aae-orc-d3nq.55
    # ... every remaining top-level unit, one line each
  repo_artifacts:
    - kind: env-shim
      name: CLAUDE_PLUGIN_ROOT
      value: "{store-version-root}"
    - kind: compat-symlink            # D2
      path: plugins/vsdd-factory
      target: "{store-version-root}"
```

### Validation rules

1. Every top-level tree entry matches exactly one unit (build-time,
   deny by default).
2. Every declared unit path exists in the recorded file manifest
   (install.meta v0.2.0 `file-manifest.csv`); a declaration pointing
   at nothing is a build error.
3. Copy-mode materialization verifies modes against
   `exec-manifest.txt`; symlink mode needs no mode handling (the
   store copy is the only copy).
4. Enable refuses success if the post-write settings hook-entry count
   is short of the declared event set (aae-orc-d3nq.7).

## Worked inventory: vsdd-factory v1.0.0-rc.23

Counts verified against the tagged tree, 2026-07-31. Live-reference
counts are files under `skills/`, `agents/`, `bin/`, and the two
registries that mention the unit.

| Unit | Size / count | Disposition | Reason |
|---|---|---|---|
| `skills/` (126 dirs, 274 files) | 1.9M | materialize | Harness discovers by location. Prefix `vsdd-` per naming policy; 176 slash-form namespace references rewritten at bind. Two subtrees carved out below. |
| `skills/activate/` | | replace | Copies `hooks.json.<platform>` INTO the store tree (writes frozen shared content); on our channel registration is the settings chain, so the render step itself is retired. Replacement: `sideshow enable`. |
| `skills/deactivate/` | | replace | `rm -f`s the rendered hooks.json inside the store; with a shared store that disarms every repo on the machine. Replacement: `sideshow disable` (ledger replay). |
| `agents/` (34 flat .md + `orchestrator/` with 10 files) | 584K | materialize | Harness discovers by location; nested layout loads with bare-name addressing (trial T20). 23 `subagent_type` call sites rewritten at bind. |
| `hooks/` top level (38 scripts + `dim2-gates/`, 50 .sh total; 6 `hooks.json.*`) | ~1M | store-reference | Hook COMMANDS are synthesized into the repo settings chain with absolute store paths plus the inline env belt; the platform templates are read at enable time to derive the event set. No per-machine hooks.json is ever rendered on this channel. |
| `hooks/dispatcher/` (5 platform binaries) | 77M | store-reference | Never copy 77M per repo. Platform resolved at enable via upstream's detect-platform contract; dispatcher invoked by absolute path. |
| `hook-plugins/` (34 wasm) | 13M | store-reference | Loaded by the dispatcher through `hooks-registry.toml` paths that resolve from the root env. |
| `bin/` (21 executables) | 200K | store-reference | Reached as `${CLAUDE_PLUGIN_ROOT}/bin/...` from skills and hooks. |
| `rules/` (10 files, 6 live refs) | 56K | store-reference | Deliberately NOT written into `.claude/rules/` (enable-contract exclusion, aae-orc-d3nq.7); content is read through the root env. |
| `templates/` (136 files, 97 live refs) | 664K | store-reference | Read-only reference content, heaviest live-reference count in the tree. |
| `workflows/` (17 files, 22 live refs) | 316K | store-reference | Engine content (.lobster pipelines). |
| `config/artifact-path-registry.yaml` (20 live refs) | 16K | store-reference | Engine configuration. |
| `tools/` (15 files, 2 live refs) | 148K | store-reference | Operator tooling, root-env reachable. |
| `docs/` (6 files, 55 live refs) | 144K | store-reference | Read at runtime (AGENT-SOUL.md and peers); heavily referenced, never repo-resident. |
| `fixtures/smoke-project` (2 files, 1 live ref) | 8K | store-reference | Test fixture consumed in place. |
| `tests/` (564 files, 247 .bats) | 4M | exclude | Decision D3. Cost priced in the divergence register: installs lose the in-repo self-test surface; doctor may run the suite from the store copy; 151 of the .bats reconstruct plugin root via a fixed four-level parent walk, so the shipped suite could not validate an unshaped install anyway. Upstream locator-rewrite offer is aae-orc-d3nq.31. |
| `hooks-registry.toml`, `resolvers-registry.toml` | | store-reference | The dispatcher reads them beside itself; three live `read_file` capability grants in hooks-registry.toml stay byte-identical behind the compat symlink. |
| `.claude-plugin/plugin.json` | | exclude | Contract item 2: no harness plugin state on this channel. Retained in the store as upstream provenance only. |

### Repo artifacts for this pack

| Artifact | Source | Notes |
|---|---|---|
| env shim `CLAUDE_PLUGIN_ROOT={store-version-root}` | .50 route (a) | Resolves all 419 non-test references with zero rewriting; byte identity preserved for the equivalence proof. |
| compat symlink `plugins/vsdd-factory -> {store-version-root}` | D2 | Resolves the 451 repo-relative references including the three capability grants. Ownership-manifest recorded, gitignored, never committed. |
| settings hook entries | .52 | All 12 registered events per D5: the 10 wired in `hooks.json.<platform>` (PermissionRequest, PostToolUse, PostToolUseFailure, PreToolUse, SessionEnd, SessionStart, Stop, SubagentStop, WorktreeCreate, WorktreeRemove) plus PreCompact and PostCompact, which upstream registers but never wires (defect bead aae-orc-d3nq.63). Every synthesized command carries the inline `CLAUDE_PLUGIN_ROOT='<abs>'` prefix as the belt. |
| ownership manifest + ledger row | .48 / .5 | Exact removal and orphan detection. |
| gitignore entries | .53 | Bindings and the compat symlink never enter the repo's history. |

## Known edges the consumers of this spec must not lose

- **Worktrees start empty.** Everything above lands under gitignored
  paths, so a fresh worktree (`.worktrees/STORY-*` is the factory's
  own pattern) has no bindings under any strategy. Enable and the
  hygiene design own this edge (aae-orc-d3nq.7 / .53); the
  WorktreeCreate hook is the candidate lever (accepted per trial T18,
  firing unverified).
- **Grant resolution is a behavior change.** The three capability
  grants are broken today for every claude-mp consumer; the compat
  symlink makes them resolve for the first time. That is a trial
  before it is a claim.
- **Store content is signed reference; repo content is a declared
  derivative.** Every transform applied during materialization is
  recorded in the rewrite manifest (aae-orc-d3nq.61); the equivalence
  proof compares store bytes to repo bytes modulo declared
  transforms. Feeds the trust-scope block (aae-orc-d3nq.16).

## Cross-references

- aae-orc finding-094 (the unshaping contract + D1-D5 ratification)
- aae-orc finding-093 (coexistence, containment, preserve taxonomy)
- aae-orc finding-091 addendum 2 (trials T15-T21, the harness ground
  truth this spec stands on)
- Consumers: aae-orc-d3nq.48 (materialization), .49 (discovery), .50
  (root resolution), .51 (naming), .52 (settings hooks), .7
  (enable/disable), .59 (census-diff gate), .32/.13 (register and
  pack.yaml emission), .55 (divergence register), .60 (replacement
  skills)
