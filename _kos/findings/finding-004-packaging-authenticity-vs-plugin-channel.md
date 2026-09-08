# finding-004: ten ways the sideshow-packaged vsdd-factory diverges from the plugin install, measured at rc.24 and rc.25

**Date:** 2026-09-08
**Subject:** sideshow's packaging of vsdd-factory on the repo-bindings channel
**Occasion:** packaging 1.0.0-rc.25 (upstream commit `51023185658350afaacaa931a175103d915d14ba`)
**Evidence:** CI test build `34280184572`; local rebuilds; the rc.24 published
pack; the upstream trees at both tags read through the GitHub compare and tree
APIs. No local mirror of upstream was taken.

## Why this is worth writing down

`sideshow/docs/divergence-register.md` already prices twelve divergences the
unshaping delivery accepts on purpose. This finding is the other list: the
places where the packaged form does not do what the plugin install does and
nobody chose that. Six of the ten are ours, three are upstream's, one is a
shared seam. Three are fixed in this session; the rest are named with a shape.

The organizing observation, which is the finding under the finding:

> Every gate the pipeline had compared the captured tree against **the same
> tag it came from**. That proves capture was faithful and says nothing about
> the tree. Every defect below lived in the gap between those two questions.

## The ten

### 1. Namespace references the bind rewrite can never reach

The channel rewrites `vsdd-factory:<name>` to `vsdd-<name>`, but only inside
materialize units (`skills/`, `agents/`). The store is byte-frozen by design,
so store-reference units keep the upstream form. At rc.25 that is **25
references across 13 files**, and they are not all equal:

- `docs/VSDD.md`, `docs/FACTORY.md`, `docs/not-portable.md` are read at
  runtime by the model.
- Four hook scripts emit the wrong command *when they fire*:
  `validate-state-size.sh` tells the model to "Run /vsdd-factory:compact-state".
- `templates/state-template.md` and `templates/story-template.md` are **copied
  into the consumer's own `.factory/` state**, so the unresolvable instruction
  becomes durable user-owned content. `state-template.md:30` seeds every new
  STATE.md with "Run /vsdd-factory:compact-state if this file grows past 200
  lines."
- Two are **compiled into wasm** and no bind-time transform can reach them at
  all: `validate-artifact-path.wasm` ("use /vsdd-factory:register-artifact")
  and `warn-pending-wave-gate.wasm` ("Invoke /vsdd-factory:wave-gate").

Fixed here: measured per release into `unrewritten-refs.txt` and
`install.meta.channel_divergence`. Not fixed: the references themselves. The
real repair is upstream-shaped, and the compiled two make the case: emit
command names from a variable the channel can set, or ship a name map that
skills and plugins read.

### 2. The unshape declaration is pinned to rc.23, and its gate was never built

`docs/unshaping-spec.md` carries a worked inventory taken at rc.23: 34 wasm,
126 skill dirs, 564 test files, 176 slash-form references, 23 `subagent_type`
call sites. rc.25 ships 40 wasm, 127 skill dirs, 646 test files, 28 namespaced
`subagent_type` call sites. The spec claims a deny-by-default per-release
census (`aae-orc-d3nq.59`); it was specified and never implemented, so nothing
noticed two releases of drift.

Fixed here: the register declares `unshape_units` and the build refuses any
undeclared top-level unit, with a negative control proving it refuses. Counts
stay out of the gate on purpose — they drift every release, so a count gate
either blocks routine growth or gets widened until it means nothing. Growth is
measured in `unit-census.txt` instead and read as a delta.

### 3. Orphan wasm ships undetected, twice

rc.24 shipped `policy15-attestation-gate.wasm`, a native CLI crate swept into
a workspace-wide `wasm32-wasip1` build (upstream #786). Upstream removed it in
rc.25 and added recurrence prevention to `release.yml`. rc.25 then ships
`last-amended-migrate.wasm` the same way, and that crate's own `Cargo.toml`
says so in as many words:

> Standalone native CLI binary — NOT a WASM hook plugin ... not built for the
> wasm32-wasip1 target

rc.25 also retains `verify-state-timestamp-refresh.wasm` after deleting its
registry entry (superseded by `stamp-state-timestamp` per ADR-046), with the
mode dropped 755 to 644. Our pipeline counted wasm files and never asked which
of them any registry dispatches.

Fixed here: reconciliation against both registries, `wasm-orphans.txt`, and the
set recorded in `install.meta` so it travels under the signature. Records
rather than refuses by default, because packaging must not block on upstream's
bug; `REQUIRE_NO_ORPHAN_WASM=1` makes it a hard gate. Worth stating plainly:
orphans are inert weight, not executed content — the dispatcher loads by
registry path.

### 4. The exec-bit gate is a count, not a set

The build compares the tag's `100755` count to the staged count. Both sides
come from the same tag, so it proves capture fidelity and cannot see upstream
drift. rc.24 to rc.25 was +2 / -1, netting +1: two new plugins gained the bit
and `verify-state-timestamp-refresh.wasm` lost it. The rc.24 bracket's evidence
line reads "exec-bit delta additive, zero removals"; that is no longer true and
nothing in the pipeline would have said so.

Recorded here as a set diff in the rc.25 bracket's evidence. The mechanical
version belongs in `verify-artifact.sh`, comparing exec manifests across
versions the way it already compares file counts.

### 5. `tests/` is excluded, so the pack cannot self-verify after binding

Priced in the divergence register as decision D3, and the price rises: rc.25
grew `tests/` from 564 to 646 files including two new bats covering exactly the
new gates. The honest half is that the shipped suite could not validate an
unshaped install anyway — 151 of 247 `.bats` reconstruct the plugin root by a
fixed four-level parent walk. The upstream locator-rewrite offer
(`aae-orc-d3nq.31`) is the fix that would make the exclusion optional.

### 6. Upstream's own upgrade instruction does not exist on this channel

rc.25's release notes end with: "Operators should update via
`/plugin update vsdd-factory@claude-mp`". Divergence #6 accepts having no
machine-wide upgrade by design, and the reasoning holds. What changes at rc.25
is the stake: the headline of this release is a **HIGH sandbox-escape fix**
(wasmtime 46.0.2 to 46.0.3, RUSTSEC-2026-0269). "Version toggle is
`disable(vA)` + `enable(vB)`, per repo" is an ergonomic cost on a feature
release and a patch-latency problem on this one.

### 7. A consumer cannot ask which of their repos is running a vulnerable engine

The wasmtime fix lives inside committed wasm and dispatcher binaries.
`install.meta` records `binary_provenance` as prose. Nothing in the artifact or
the ledger answers "which bound repos are below rc.25", which is the only
question that matters on a security release.

Shape of the fix: an advisory field in the register (`security_relevant: true`
plus the advisory ids on a bracket), carried into `install.meta`, and a
`sideshow doctor` layer that reads the repo-bindings ledger and reports bound
versions against it. The ledger already holds the per-repo version; nothing
reads it for this.

### 8. Registry and dispatcher must move together, and nothing asserts it

rc.25 says it directly: an operator running a pre-rc.25 dispatcher against a
post-rc.25 registry "would fail registry-load validation", because
`on_error = "block_if_marker"` is a new registry variant the older binary
cannot parse. Sideshow is structurally safe here — bindings pin absolute store
version directories, never a `current` symlink, so a store flip cannot retarget
a bound repo. But `sideshow adopt` reads a foreign tree, coexistence is
supported per machine, and no artifact field records which registry variants a
dispatcher understands, so no check can detect skew when one appears.

Shape of the fix: record the registry's `on_error` variant set in
`install.meta`, and have `coexist-check` compare it against the dispatcher the
repo will actually invoke.

### 9. Project scope is not portable, and rc.25 makes the platform half sharper

Divergence #12 prices this: three pieces of a project-scope enable stay
machine-bound (the `plugins/<pack>` compat symlink, the hook command in
committed settings, `env.CLAUDE_PLUGIN_ROOT`). The platform-specific dispatcher
path is the part that bites hardest — a macOS-committed enable names
`darwin-arm64/factory-dispatcher` in a file a Linux teammate checks out. The
structural fix is the second-developer setup verb (`aae-orc-d3nq.21`), open.

### 10. This channel executes upstream code the plugin channel never runs

Divergence #8 wires all 12 hook events where upstream's templates wire 10, so
PreCompact and PostCompact fire here and are dead as shipped upstream
(`aae-orc-d3nq.63`). Framed as "fix over fidelity," which it is. The
consequence is worth stating in the other direction too: rc.25 changes
`precompact-flush` behavior (S-17.07 identity-gated lock renewal, a six-outcome
decision tree), and **this channel is the only one where that code path
executes at all**. We are the first and only exerciser of upstream logic that
has never run in its own production. That is a testing obligation we inherited
by fixing their bug.

## What changed in this session

- Register: rc.25 bracket, plus wrinkles `wasm-orphans-ship-undetected`,
  `namespace-refs-unreachable-in-store`, `unshape-declaration-pinned-to-rc23`.
- `scripts/build-vsdd-factory.sh`: deny-by-default unit census (refuses),
  wasm/registry reconciliation (records, gate-able), unrewritten-reference
  measurement (records). Three new manifests, all hashed into `install.meta`
  so they travel under the signature.
- Negative control run: removing one unit from the declaration makes the build
  refuse with the unit named.

Problems 1 and 3 have an upstream half worth offering; 4, 7, and 8 are
mechanical and belong in `verify-artifact.sh`, the register schema, and
`coexist-check`; 5, 6, 9, 10 are accepted divergences whose price moved.

## The generalizable rule

A packaging pipeline that only ever compares an artifact to its own source can
verify transport and nothing else. Every check worth having compares against
something the source cannot control: the previous version, a declaration
written down in advance, or the artifact's own internal cross-references. The
three gates added here are one of each.

## Cross-references

- `docs/divergence-register.md` (the twelve accepted divergences)
- `docs/unshaping-spec.md` (dispositions; worked inventory still at rc.23)
- `sideshow-packs/registry/vsdd-factory-pack-support.yaml`
- upstream `drbothen/vsdd-factory` #786, release notes for 1.0.0-rc.25
