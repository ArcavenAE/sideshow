# sideshow doctor

Read-only health surface over the state sideshow itself has written:
the pack store, the registry receipts, the sync manifest, and the
repo-bindings ledger. Every check is the mechanical verification of a
promise some receipt, ledger row, marker, or doc sentence already
makes. Doctor proposes; it never mutates (orc ADR-007, automation
boundary).

Ticket: bd `aae-orc-xteh`. Layer vocabulary is fixed by orc charter
F24: layers 1, 3, 4, 5. Layer 2 (pack-declared validation) is excluded
and deferred to the weave engine (`aae-orc-a44c`); doctor prints one
line saying so, so the numbering gap reads as a decision rather than a
bug.

## Command

```
sideshow doctor [<pack>] [--layer <n[,n...]>] [--repo <path>]
                [--json] [--strict]
```

- `<pack>` narrows every layer to one pack (positional, matching
  coexist-check, enable, disable, adopt).
- `--layer` limits to a comma list drawn from 1,3,4,5. `2` is rejected
  with a pointer to the deferral.
- `--repo` sets the layer-3 subject directory. Default: cwd.
- `--json` emits the machine report (schema_version carried) on
  stdout and suppresses the text report.
- `--strict` promotes structural WARN findings to the failing exit.
  Advisory findings are never promoted, by invariant.

Layer 4 resolves fleet repos through what the receipts already
record: each installation's root and repos manifest, plus the
repo-bindings ledger. There is no separate `--manifest` flag; when
the lockfile ships (`aae-orc-333y`), pin comparison keys off the orc
root the same way.

## Result model

Every check produces findings with two orthogonal fields:

- **Status**: `ok`, `warn`, `fail`, `unavailable`.
- **Class**: `structural` (sideshow's own records disagree with disk;
  well-formedness) or `advisory` (health signals: drift a human may
  have intended, staleness, in-flight activity).

Exit policy, which is the orc `diagnostic-not-gate` rule expressed as
code rather than etiquette:

| Condition | Exit |
|---|---|
| no structural `fail` | 0 |
| any structural `fail` | 2 |
| structural `warn` under `--strict` | 2 |
| advisory findings, any status, any flags | never affects exit |
| `unavailable`, any flags | never affects exit |
| doctor itself cannot run (bad flag, etc.) | 1 |

Structural validity may gate CI; health may not. Wiring
`sideshow doctor --strict` into CI is legitimate; nothing advisory can
ever turn that gate red.

Two invariants are enforced by the runner and by tests, not by
convention:

1. Every non-`ok` finding carries a `next:` line naming the command to
   run, the doc to read, or the bd ticket that supplies the missing
   input.
2. Layer 3 findings are clamped to advisory and to `warn` or below
   (ratified warn-only decision). A layer-3 check that returns `fail`
   is a programming error the clamp corrects and reports.

## Absent-input discipline

Same rule as the pack consumer side (sideshow-packs
docs/release-url-format.md): an absent input makes a check
`unavailable` with a named reason, never a failure and never a silent
skip. Silence must not read as cleanliness. Concretely:

- no `sideshow.lock` at the orc root: layer 4 pin comparison is
  `unavailable`, reason names `aae-orc-333y`; version skew against the
  installed store is reported instead as the honest partial answer.
- no known-defects feed: layer 5 is `unavailable`, reason names
  `aae-orc-ztg5` (feed) and `aae-orc-10vq` (overlay artifacts).
- a pack whose store copy ships no `_config/files-manifest.csv`
  (vsdd-factory does not; bmad does): content census is `unavailable`,
  reason names `aae-orc-wk92` (release-asset manifests are not
  retained on install today).
- a malformed ledger or registry: dependent checks are `unavailable`
  carrying the parse error verbatim, never reported as "no repos
  bound" (the lie `internal/ledger/ledger.go` warns about).

## Check inventory

Availability legend: **today** ships in this PR; **d3nq.9**,
**receipt-change**, **lockfile (333y)**, **feed (ztg5)** are the
follow-on tickets that light the check up.

### Layer 1: sideshow-native integrity

| ID | Verifies | Class/Status on hit | Availability |
|---|---|---|---|
| `registry-parse` | registry.yaml loads | structural fail | today |
| `store-coherence` | registry version, `current` symlink target, and version dir agree; `current` not dangling | structural fail | today |
| `store-shape` | installed version looks like installer output, not a source tree (closes `aae-orc-xbkl`) | structural warn | today |
| `store-freeze` | no write bits under a version dir (PR #106 invariant). Wording names the two benign causes (pre-freeze install; interrupted unlock-write-refreeze); never "tampered" | advisory warn | today |
| `store-content-census` | per-file sha256 of the store tree against the census the pack ships (`_config/files-manifest.csv`) | structural fail | today where shipped; unavailable otherwise |
| `sync-manifest` | every sync-manifest entry path exists; entry version matches the active store version (entries recording `custom` are project content and exempt from currency) | structural warn | today |
| `receipt-markers` | receipted claude_md sections have paired begin/end markers (a lone begin silently duplicates the section on the next distribute); receipted rules files keep their managed-by first line | structural fail / warn | today |
| `receipt-symlinks` | receipted symlink artifacts exist, are symlinks, and point at their recorded target | structural fail | today |
| `store-exec-census` | exec-manifest census against the installed tree | structural fail | receipt-change (census is a release asset, not retained; `aae-orc-wk92`) |
| `sync-content` | content hashes of synced bindings | unavailable | receipt-change (sync manifest records paths only) |

### Layer 1, plugin-class check set (mechanism-keyed seam)

Packs whose `activation.mechanism` declares a plugin class get an
extra check set, dispatched from the same activation contract that
`status` and `bindings.Sync` already branch on. This PR ships one
check to prove the seam by use:

| ID | Verifies | Class/Status on hit | Availability |
|---|---|---|---|
| `ledger-row-coherence` | ledger row's store path exists and is a version dir (never `current`); row version matches the dir; repo dir still exists | structural fail; missing repo dir is advisory warn (a distinct condition, not a failure) | today |
| env-shim, hook-chain, dispatcher exec, local-scope symlinks, replay preflight, dangling foreign registrations | the per-repo bind battery | mixed | d3nq.9 |

### Layer 3: cwd discoverability (warn-only, advisory by invariant)

| ID | Verifies | Availability |
|---|---|---|
| `cwd-known` | whether sideshow has any record of this directory (ledger row, receipt repo, installation root) | today |
| `cwd-coexistence` | the coexist-check battery per installed pack, ERROR grades clamped to warn with the original grade named; a battery error becomes unavailable, never a crash | today |
| `cwd-factory-guard` | unexpired factory lock with holder and age (TTL-based, so the observation time is printed) | today |

### Layer 4: fleet drift

| ID | Verifies | Class/Status on hit | Availability |
|---|---|---|---|
| `receipt-drift` | re-hash every receipted `files` and `rules` artifact against its recorded checksum (the sideshow#92 signal). Missing receipted file is structural (the receipt claims ownership); a changed one is advisory (a user edit is a deliberate act) | structural warn / advisory warn | today |
| `receipt-coverage` | dead installation roots and empty receipt sets, reported as skipped-with-reason so "no findings" cannot read as "no drift". Names the two under-coverage causes: RecordResults drops receipts for files a partial run skipped, and `project init` writes no receipts | advisory | today |
| `version-skew` | ledger row versions and receipt versions against the active store version | advisory warn | today |
| `lock-pins` | declared pins vs installed and receipted versions | unavailable | lockfile (333y) |

### Layer 5: known defects

| ID | Verifies | Availability |
|---|---|---|
| `known-defects` | installed pack versions against the publisher defects feed | feed (ztg5) |
| `unapplied-overlays` | overlays applying to an installed base, not yet applied | feed (ztg5) + overlay spec (10vq) |

A future stopgap may read the sideshow-packs pack-support registers
(wrinkle brackets) with an operator-supplied path; those rows are
"upstream packaging wrinkles", not known defects, and must be labeled
as such. Deliberately not in this PR: doctor is offline here, no
network call anywhere.

## Receipt fidelity (separate PR)

Two write-path changes are deliberately not in the doctor PR, so
doctor stays read-only and reviewable:

1. `distributeFile` observes drift (path, on-disk hash, receipted
   hash) and throws the observation away (sideshow#92,
   `internal/distribute/distribute.go`). Keeping it turns "drift
   exists" into "drift first seen at T".
2. `RecordResults` replaces the artifact list wholesale, so a run
   that wrote one file and skipped another drops the skipped file's
   receipt, converting a sideshow-written file into "user-authored"
   forever. Doctor's `receipt-coverage` check names this ambiguity in
   its output until the fix lands.

## JSON output

`--json` emits one object: `schema_version` (0.1.0), `generated_at`
(UTC), `findings[]` (layer, id, pack, subject, status, class, detail,
next), and `summary` (counts per status, exit code). Consumers follow
the absent-field discipline: unknown fields are ignored, absent fields
mean the field's documented default, and `schema_version` gates shape
expectations.
