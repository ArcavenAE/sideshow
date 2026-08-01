# Finding 002: The repo-bindings channel survives a consumer-side pilot on a machine the producer does not drive

**Date:** 2026-08-01
**Session:** 064 (aae-orc#128, two rounds + one unasked addendum)
**Scope:** sideshow — install/list/status/use, repo bindings
(enable/disable/activate/deactivate/coexist/coexist-check/adopt),
both scopes, headless hook execution
**Status:** finding. Every claim below was executed on the consumer
machine; nothing is inferred from source reading alone except where
a mechanism is named with a file:line.
**Class:** delivery-channel validation
**Machine:** `mokuzai` (consumer; the producer machine is `kinu`)
**Versions:** sideshow `0.1.0-alpha.20260801.030225.044e574` (round 1)
→ `…040642.a1962f7` (round 2, carrying the round-1 fixes);
packs bmad 6.9.0/6.10.0 and vsdd-factory 1.0.0-rc.23; forestage
built from `5b6665f`

## Headline

The delivery journey works end to end on a machine that did not build
it, and the one question that could have forced a design change is
answered: **repo-scoped skills, agents, and settings hooks all
register and fire in a headless forestage NDJSON session, including a
blocking `PreToolUse` that was honored.** Six distinct hook events
dispatched. The delivery design does not need to change on that
account.

Fifteen defects surfaced across the two rounds (nine round 1, four
round 2, two in the addendum). None was a data-loss or
wrong-state defect; every refusal path refused *before* writing, and
every reversal verb reversed exactly. The failure class was
consistently **prediction and disclosure**, not corruption — the tool
does the right thing and sometimes describes it wrongly.

## What the headless result actually proves

The producer's open question was whether hooks fire outside an
interactive session. Evidence, three independent ways:

1. **NDJSON stream** — `hook_started`/`hook_response` frames for
   `SessionStart`, carrying the dispatcher's own stdout.
2. **The honored block** — a `Read` of `.env` came back
   `is_error=True` with `PreToolUse:Read hook error: …`, and the
   block reason reached the model, which quoted it back:
   `BLOCKED by protect-secrets … Code: env_file_read_direct`.
3. **The pack's own logs** — `.factory/logs/dispatcher-internal-*.jsonl`
   recorded `PreToolUse` 21, `SubagentStop` 12, `PostToolUse` 11,
   `Stop` 10, `SessionStart` 6, `SessionEnd` 6; and
   `events-*.jsonl` recorded two `{"type":"hook.block",…}` rows.

Also confirmed headless: a repo skill invoked (`vsdd-factory-health`),
and a repo agent dispatched (`vsdd-technical-writer`, responding in
its pack persona).

**F002-a — the NDJSON hook-frame gap is observability, not
execution.** Only `SessionStart` produced `hook_started`/
`hook_response` frames. The other five events executed but surfaced
no lifecycle frames; the `PreToolUse` block was visible *only* as a
`tool_result` error. Anything downstream that plans to observe hook
activity by parsing NDJSON will under-report. Filed at forestage#115
(hedged: possibly upstream stream-json behavior forestage passes
through), recorded as divergence-register entry 11.

**F002-b — a blocking-hook probe must not collide with agent
instructions.** The first attempt used `block-ai-attribution` by
asking the session to commit with a `Co-Authored-By: Claude` trailer.
The model refused *on its own* — the operator's global CLAUDE.md
forbids that trailer — so `Bash` was never invoked and the hook never
ran. The test measured the agent's compliance, not the hook. Switching
to `protect-secrets` (a `Read` of `.env`, benign to the model) made
the hook the only possible cause of the block. **Generalization: to
test an enforcement boundary, pick a probe the agent has no
independent reason to refuse.**

## Scope coverage: local vs project

Round 1 exercised local scope only. Round 2 closed project scope.

| Claim (runbook §2) | Local | Project |
|---|---|---|
| bindings shape | symlinks into store | **full copies — 0 symlinks found** |
| registration target | `settings.local.json` | **committed `settings.json`** |
| exec bits survive the copy | n/a | **yes — 10 `.sh` files `+x`, 0 mismatches vs store across 20 sampled** |
| dispatcher executes | yes | **yes** (direct probe + headless block) |
| disable reverses | exact, 0 residue | **exact, 0 residue — including removing the `settings.json` it created** |

**F002-c — "exec bits survive the copy" is the wrong worry; the
engine is never copied.** Even at project scope the registered hook
command and `CLAUDE_PLUGIN_ROOT` point into the machine store, per the
runbook's own store-vs-repo table. So the exec-bit question applies
only to copied *content* (where it holds), and project scope is **not
self-contained** the way §2's rationale implies: `plugins/<pack>` is
still an absolute symlink into the store, and the hook command is an
absolute store path. A committed project-scope enable dangles on a
teammate's machine. Filed as N2 — either reword the rationale or add
machine-independent indirection. **Untestable here** (needs a second
machine); the reasoning is verified, the cross-machine failure is not.

## The refusal architecture is the strongest part

Every guard the docs claim was probed. All hold, and all refuse
*before* writing anything (verified by `git status` + filesystem
inspection after each refusal), exiting 1.

| Guard | Claim | Result |
|---|---|---|
| `content-collision` | "refuse rather than clobber" | hand-written `SKILL.md` **preserved byte-for-byte** |
| unexpired `factory_lock` | "never overridable" | refuses **even with `--override-stale-lock`** |
| stale `factory_lock` | "passes only with the override" | refuses bare, proceeds with flag |
| `remote-safety` | do not trial on a shared `factory-artifacts` origin | refuses unconditionally, no flag bypasses |
| `same-repo-dual-enable` | never two chains on one `.factory` | refuses, offers three named options |

**F002-d — the target repo named in the test plan was ineligible, and
that was the correct outcome.** `run/ftc-blue` could not be enabled:
`remote-safety` fires because origin carries `factory-artifacts`
shared with a sibling `ftc` checkout (verified via `git ls-remote`),
and it is emitted unconditionally at
`internal/coexistcheck/coexistcheck.go:139-142` with no override flag.
Runbook §6 step 1 independently says not to trial there. The pilot
moved to a clean scratch repo (round 1) and the forestage checkout
(round 2) rather than forcing it. The producer ratified this and
retargeted the pilot. **The lesson is that an unconditional guard
beats a documented warning** — the docs said "don't," and the code
made it impossible.

## The recurring defect class: prediction, not corruption

Three of the four highest-severity findings are the same shape — a
command tells the operator what will happen and is wrong.

- **D1** (round 1, fixed in #89): `adopt --dry-run` printed a
  confident plan for a repo `coexist-check` errors on. Fixed: the
  dry-run now runs the same preflight and exits 1 with "the real run
  would REFUSE." Verified on the original repo.
- **N1** (round 2, open, **highest-value open defect**): the fixed
  dry-run *still* mispredicts, in the opposite direction. It printed
  "the real run would proceed" and the real run failed. Mechanism:
  `internal/adopt/adopt.go:148-157` defaults `adoptVersion` to the
  **running** version, so `adoptVersion == runningVersion` and the
  version-equality gate at `:155` never fires; adoption proceeds to
  `enable <pack>@<running>` which fails at `internal/enable/enable.go:327`
  when the store lacks that version. Two consequences: the documented
  version-equality refusal is **unreachable in the default
  invocation** (it fires only when a version is named explicitly), and
  the dry-run never validates its own step 2 against the store.
- **D9** (round 1, docs half fixed): `disable` can refuse, and its
  escape hatch was undocumented. Triggered by *sideshow's own
  dispatcher* writing `.factory/logs/` during the headless test —
  `LockTTL` = 2700s (`internal/factoryguard/factoryguard.go:36`), so
  any hook activity blocks enable *and* disable for 45 minutes. The
  retreat path in §6 showed a bare `sideshow disable`. Now documented
  in help, §4, and §6; **round 2 re-tested the documented path live
  and it works as written.** The behavior question (should disable
  downgrade soft signals?) is open at #88.

**F002-e — self-inflicted guard trips are a real ergonomic cost.**
The burst-log signal fired on *our own* test traffic in four separate
places across both rounds (disable, re-enable, adopt, and
adopt --rewrite-agent). Any operator who trials the pack and then
backs out within 45 minutes hits it. This is the single most
frequently encountered friction in the whole pilot.

## Two defects filed proactively (addendum)

- **sideshow#94 (High) — the consent prompt grants consent when
  nobody can answer.** `cmd/sideshow/main.go:497-510` reads stdin with
  **no TTY detection anywhere in the codebase** (`grep -rn
  "IsTerminal\|isatty\|ModeCharDevice" --include='*.go'` → zero
  matches); on EOF the answer is neither `n` nor `no`, so the
  default-yes branch writes the permission. Reproduced under a
  throwaway `HOME`: `install … < /dev/null` wrote
  `Read(.../packs/)` to `settings.json` unattended. `--yes` already
  covers the intentional case, making the silent default redundant.
  Same family as the `SIDESHOW_HOME` contamination bug guarded two
  lines above. **This was visible in round 1 and glossed as "auto-
  accepted on non-TTY stdin" — the miss was mine, not the tool's.**
- **sideshow#95 (Med) — adopt without `--rewrite-agent` leaves a
  dangling default agent, silently.** With `"agent":
  "vsdd-factory:orchestrator"` in a repo, adopt succeeds, suppresses
  the foreign identity, and leaves the default agent pointing at a
  namespace that no longer resolves — a live session offers only
  `vsdd-orchestrator`, no `vsdd-factory:`-prefixed agents exist at
  all. The dry-run warns; the real run prints nothing at that step;
  `coexist-check` layer 5 reports the 35 prefixed *bindings* and never
  inspects the active agent key. That looks like a natural layer-5
  check.

`--rewrite-agent` itself is well-designed and should be left alone:
it writes the flip to `settings.local.json`, leaving committed
`settings.json` untouched, so `disable` + deleting the override
restored the original **byte-exact**.

## Adopt round-trips, and the equivalence report reads honestly

Foreign → adopt → sideshow → retreat → foreign, verified with a
genuine `claude-mp` rc.22 install:

```
E1 content parity: FAIL — 1 differing paths (first 1: agents/pr-manager.md)
E2 count parity: store=318 cache=318 discovery files
E3 dispatcher behavior: not provable headless — start a session …
E4 dispatcher identity: FAIL — store 4ab8578f6ccc vs cache 8f6fbef360b0
```

E1 is **not** a false positive — diffed by hand, rc.23 added a
stale-verdict gate to `pr-manager.md`. E2 passing while E1 fails is
the correct shape for a content edit. E3 was discharged empirically:
the adopted channel dispatches with **exactly one** `PreToolUse` — no
double-dispatch from the suppressed channel, which is the hazard the
suppression mechanism exists to prevent.

**F002-f — E1's explanatory clause is wrong under
`--allow-version-change`.** It reads "same-version trees should be
identical — verify the store artifact before trusting the adoption"
even when the operator explicitly opted into version drift, where FAIL
is the expected result. As written it tells the operator to distrust a
healthy adoption. Should branch on version equality (N4).

Retreat verified: `disable` removed bindings and left exactly the
suppression override; deleting it restored
`effectively enabled: vsdd-factory@claude-mp / suppressed here: none`.
**The machine-level plugin channel was byte-identical throughout all
three passes** (rc.22, sha `a04cb30`, unchanged `lastUpdated`).

## Context cost: prefixing is cheaper, not a tax

`system/init` frame bytes, measured as
`len(line.encode())` on the single init NDJSON line, tokens ≈ bytes/4:

| State | bytes | ~tokens | skills | agents | slash_commands |
|---|---|---|---|---|---|
| bare repo | 6,983 | ~1,746 | 133 | 5 | 156 |
| repo-bindings enabled | 14,444 | ~3,611 | 258 | 49 | 281 |
| plugin channel enabled | 17,085 | ~4,271 | 258 | 49 | 281 |

**F002-g — identical artifact counts, 2,641-byte difference, and the
entire delta is naming form.** Field-level diff: skills +1000,
slash_commands +1000, agents +482, `plugins[]` +168. The cause is
`vsdd-factory:activate` (namespace-qualified) vs `vsdd-activate`
(prefixed) at ~8 bytes × ~250 entries. **So the divergence register's
prefixing decision has a small measurable upside (~15% smaller init
frame here), not a cost.** Caveats: "bare" carries 133 pre-existing
user-scope bmad skills, so it is a floor for this machine rather than
a universal baseline; this is the announcement cost only, not system
prompt or skill bodies; one sample per state, stable across repeats.

## The synthesized replacement skills work

`vsdd-activate` and `vsdd-deactivate` route to the sideshow verbs,
observed as tool calls: `Skill` → `Bash(sideshow coexist-check)` →
`Bash(sideshow activate)`, and `Skill` → `Bash(sideshow deactivate)`.
Agent key went `None → vsdd-orchestrator → None`. **Store byte-
identical across both invocations** — same aggregate hash over 1191
files, method
`find <store> -type f | sort | xargs shasum -a 256 | shasum -a 256`.
The upstream problem (activate writing into the store) does not
reproduce on this channel.

Operational note: step 1's `coexist-check` needs `Bash(sideshow:*)`
allowed, or the skill stalls at step 1 in a default-permission
session.

## Two near-misses worth recording as method

Both would have been damaging false reports had they been published.

1. **"Unexpired lock is bypassable."** My first lock probe wrote a
   `.factory/factory_lock` *file*; enable proceeded. The lock actually
   lives in `.factory/STATE.md` frontmatter
   (`internal/factoryguard/factoryguard.go:12`, `readFactoryLock`).
   Reading the source before filing turned an alarming false positive
   into a confirmed-sound guard.
2. **"Refusals exit 0."** Briefly measured `exit=0` on a collision
   refusal — that was the shell pipeline reporting `head`'s status,
   not sideshow's. Refusals exit 1 consistently.

**Generalization: when a probe of a safety guard *passes* (i.e. the
guard appears absent), suspect the probe before the guard.** A guard
that silently does nothing is a much rarer defect than a test that
fails to trigger it.

## What this pilot does not prove

- **Cross-machine project scope** (the F002-c / N2 hazard) — needs a
  second machine checking out a committed enable.
- **N1's dry-run fix** — the defect is diagnosed with a mechanism and
  file:line, but no fix has been verified.
- **`run/ftc-blue` as a target** — ineligible by design until its
  origin's `factory-artifacts` ref is cleaned up.
- **`sideshow remove`** — no such verb; the `bmad unknown` malformed
  store row from an older sideshow remains unfixable without hand-
  deleting, which the pilot was instructed not to do. Bad store rows
  are currently permanent (D-round-1 step 4).

## Defect ledger

**Round 1 (nine).** D1 dry-run mispredicts (fixed #89) · D2 plan
numbering skips 3 (fixed) · D3 undocumented prerequisites
(`cosign`/`gh`/`python3` — the pilot's first hard stop; fixed) ·
D4 stale install notice (fixed) · D5 `status` reports
`available/synced` with no verdict (#85) · D6 enable/disable count
off-by-one (#86) · D7 disable residue + ungitignored `.factory/`
(#87) · D8 NDJSON hook-frame gap (forestage#115) · D9 undocumented
disable escape hatch (docs fixed; behavior question #88).

**Round 2 (four + one root cause).** N1 adopt default path cannot
reach its own refusal, dry-run still mispredicts (**open, highest
value**) · N2 project scope not machine-portable · N3 local-scope
"nothing enters git history" leaves untracked paths · N4 E1 wording
wrong under version drift · **D6 root cause diagnosed**: enable
counts `len(artifacts)` including the settings file
(`enable.go:237-238`); disable's `parseArtifactStrings` splits it into
`removeSettingsFile` and excludes it from `removed`
(`enable.go:274-291`). The off-by-one is exactly the settings file and
appears only when enable created it — hence 176/175 in fresh repos and
symmetric 175/175 where `.claude/` already existed.

**Addendum (two filed).** #94 non-TTY consent · #95 dangling default
agent. Plus two nits not worth issues: `sideshow -v` is unknown while
`--version`/`-h`/`--help` work; no per-subcommand help, so
`--scope`/`--override-stale-lock`/`--allow-version-change` are
discoverable only via the runbook or a full usage dump — which is
what made D9 and the adopt-flag omission bite.

## Producer disposition (round 1, for the record)

D1/D2/D3/D4/D9-docs fixed and merged as sideshow #89; D5→#85,
D6→#86, D7→#87, D9-behavior→#88, D8→forestage#115. The pilot's
capability-grant observation and the D8 caveat were recorded as
divergence-register entries 9 and 11.

## Cross-references

- `_kos/findings/finding-001-bound-variant-architecture.md` — the
  bound-variant mechanism this pilot exercised from the consumer side
- orc `_kos/findings/finding-096-repo-bindings-spine-implementation.md`
  — the producer-side implementation round this validates
- orc `_kos/findings/finding-094-unshaping-over-plugin-delivery.md` —
  the contract; F002-g gives its prefixing decision a measured upside
- `docs/repo-bindings-enablement.md`, `docs/divergence-register.md`
- aae-orc#128 — the pilot request and all three reports
