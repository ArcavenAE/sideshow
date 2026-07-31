# Repo-bindings ledger: the per-repo activation record

Status: decided 2026-07-31 (aae-orc-d3nq.5, absorbing the identity
half of the retired .46; part of the finding-094 unshaping rework).
Implementation lands with the enable/disable verbs (aae-orc-d3nq.7).

## The problem this solves

Activation state used to be store-global: one `current` symlink and
one registry version per pack, with nothing recording which repo
enabled which version. For per-repo software that either makes
version flips lie (registration points at version dirs, so flipping
`current` changes nothing) or yanks every repo at once, including one
mid-factory-run. Two repos on two versions, an explicitly desired
journey, had no representation at all.

## The decision

Sideshow keeps a per-repo ledger at
`<sideshow-data>/repo-bindings.yaml`, keyed by absolute repo path,
one row per (repo, pack):

```yaml
schema_version: "0.1.0"
repos:
  /path/to/repo:
    vsdd-factory:
      version: 1.0.0-rc.23
      store_path: /abs/store/packs/vsdd-factory/1.0.0-rc.23   # never `current`
      channel: sideshow-native        # or claude-mp (foreign, census-observed)
      platform: darwin-arm64
      settings_scope: local           # local | project
      enabled_at: "2026-07-31T00:00:00Z"
      artifacts:                      # exact removal set, ManifestEntry shape
        - .claude/skills/vsdd-pr-manager/
        - .claude/agents/vsdd-adversary.md
        - plugins/vsdd-factory        # the compat symlink
      selection: full                 # reserved for selective materialization (.56)
```

Load-bearing properties:

1. **The ledger is the removal authority.** Disable replays the
   recorded artifact list in reverse, exactly; removal never guesses,
   and reconcile logic is shared with the user-scope binding manifest
   (same entry shape).
2. **Version pin is not a separate concept.** Repo bindings reference
   `packs/<pack>/<version>/` absolutely and never read the `current`
   symlink, so two repos on two versions is representable by
   construction, and a store-level flip can never yank a running
   factory.
3. **Casual version toggle** is `disable(vA)` + `enable(vB)` as one
   transaction (the .7 verb), which is one ledger-row rewrite.
4. **`vsdd-factory@sideshow` is a LEDGER CHANNEL LABEL only.** It
   names our channel inside sideshow's own records and is never
   written into harness state of any kind (finding-094 contract
   item 3). `channel: claude-mp` rows record census-observed foreign
   installs so doctor and coexist-check read one surface.
5. **Doctor can enumerate every repo sideshow ever touched** from one
   file, which is what makes fleet-wide audit and the
   last-repo-standing rules cheap.

## What producers declare

The pack-support register declares `activation.per_repo_required`
and the mechanism; the ledger is consumer-side state. The register's
rationale (rewritten 2026-07-31) is the shared declaration both sides
read; no register field points at the ledger because the ledger is
not a contract surface, it is sideshow's own bookkeeping.

## Cross-references

- `docs/unshaping-spec.md` (what gets materialized; the artifact list
  a ledger row records)
- aae-orc finding-094 (contract), finding-093 (casual per-repo
  version toggling as a requirement)
- bd: aae-orc-d3nq.5 (this decision), .7 (verbs that write it), .48
  (ownership manifest sharing the entry shape), .56 (selection
  field), aae-orc-dihj (store freeze; the ledger's absolute paths are
  what make freezing `current`-free)
