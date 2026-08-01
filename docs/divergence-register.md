# Divergence register: vsdd-factory on the repo-bindings channel

Status: initial register, 2026-08-01 (aae-orc-d3nq.55, finding-094
round 3). The unshaping delivery accepts upstream-parity loss by
design; this register prices every deliberate drop so consumer docs,
the courtesy note, and the equivalence claim cite recorded facts
instead of assuming parity. One entry per divergence: what changes,
what it costs, what mitigates it.

Scope note: "upstream" is the claude-mp plugin form of vsdd-factory;
"this channel" is the sideshow repo-bindings delivery
(docs/unshaping-spec.md). Any defect reproducible only on this
channel is ours, not upstream's.

## 1. `tests/` is excluded from the binding set (decision D3)

Installs lose the in-repo self-test surface upstream ships since
rc.23. Cost stated plainly: 175 of 247 `.bats` files reference
`CLAUDE_PLUGIN_ROOT`, and 151 reconstruct the plugin root via a fixed
four-level parent walk, so the suite as shipped cannot validate an
unshaped install anyway. Mitigations: doctor may run the suite from
the store copy; the locator-rewrite offer upstream is tracked
(aae-orc-d3nq.31).

## 2. Addressing changes to prefixed bare names

Bound artifacts carry the `binding_prefix` (`vsdd-` for
vsdd-factory): skills become `vsdd-<name>`, agent identifiers become
`vsdd-<name>`, and namespace-qualified references
(`/vsdd-factory:<name>`, `subagent_type="vsdd-factory:<name>"`) are
rewritten at bind time. Cost: every upstream doc naming
`/vsdd-factory:X` is wrong for this channel. Why it is not optional:
a user-scope skill shadows a same-name repo skill (finding-091
addendum 3, T16 corrected), and upstream ships generic names (`jira`,
34 generic agent names) — unprefixed bound names would be silently
hijacked on consumer machines.

## 3. Upstream telemetry predicates match nothing here

Repo-scope addressing has no namespace qualifier (trial T20), so the
four upstream Grafana LogQL predicates on
`attributes_subagent=vsdd-factory:pr-manager` return zero on this
channel under any variant of the naming policy. The exact telemetry
attribute value emitted on this channel is one pending OTEL
observation (record here when telemetry is next live in an enabled
repo).

## 4. Upstream activate/deactivate skills do not drive this channel

Both are excluded from the binding set (disposition `replace`,
unshaping spec): activate renders `hooks.json` INTO the frozen shared
store; deactivate `rm -f`s it there, which with a shared store would
disarm every repo on the machine. Replacements: `sideshow enable` /
`sideshow disable` (shipped) and the consented persona-flip skill
(aae-orc-d3nq.60).

## 5. Hooks live in the repo settings chain

`claude plugin details` has no analogue here; there is no harness
plugin state to inspect. Replacements: `sideshow coexist` (foreign
census), `sideshow coexist-check` (preflight), and the ledger row.
The hook chain is visible in plain text in the repo settings file,
marked `_managed_by: sideshow:vsdd-factory`.

## 6. Update semantics move to the ledger

`claude plugin update` has no analogue; repo bindings pin absolute
store version dirs (never a `current` symlink), so two repos on two
versions is the representable normal case and a store flip can never
retarget a bound repo. Version toggle is `disable(vA)` + `enable(vB)`
per repo. Cost: no machine-wide one-command upgrade; that is the
point.

## 7. Installed-but-off state is lost (presence is activation)

The harness activates whatever is present at repo scope (trial T21):
there is no per-repo "installed but disabled" toggle on this channel;
removal is the off switch. Mitigations: local-scope symlink
materialization makes removal/re-add one repoint each way (D1);
selective materialization (aae-orc-d3nq.56) is the finer-grained
future instrument.

## 8. Hook event set: 12 wired here vs 10 live upstream (decision D5)

Upstream registers 12 events but its templates wire only 10;
PreCompact/PostCompact are registered-but-dead as shipped (upstream
defect, aae-orc-d3nq.63, filing held). This channel wires all 12 —
fix over fidelity. Language discipline: "registered" means
harness-accepted (T18); "verified firing" is observed only for
SessionStart, PreToolUse, and PostToolUseFailure.

## 9. Three dormant dispatcher capability grants become LIVE

The engine-path compat symlink (D2) makes the three
`hooks-registry.toml` read_file grants (:944, :1146, :1311 — the last
`on_error=block`) resolve for the first time in any consumer repo.
This is a behavior change, not a restoration: those grants have never
executed for a claude-mp consumer. Their first observed outcome is a
trial before it is a claim; record the outcome here, and verify the
:1311 blocker does not break tool use.

## 10. Standing per-release census-diff cost

Every upstream release must be census-diffed against the unshape
declaration (deny-by-default: a new top-level unit fails the build
until dispositioned) before a pack is cut (aae-orc-d3nq.59). This is
recurring maintainer work the plugin channel does not have, priced
here so release planning counts it.
