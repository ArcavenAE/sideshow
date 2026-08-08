# finding-003: the frozen store is the read/write classifier

**Probe:** aae-orc-c8v8 (P1 bug) asked for a sync-time check that no synced
artifact resolves a write or delete argument inside pack territory, to be
built from write-verb, flag-name, and shell-redirect signals.

**Result:** the heuristic is unnecessary. Existence in the frozen store
answers the same question exactly, with no lexical guessing.

---

## The defect

`rewritePaths` substituted every `{project-root}/_bmad/` reference with the
absolute user-install path. The assumption underneath is that everything
under the shim dir is pack content. It is not. Pack content is what the
installer put in the store; everything else under `_bmad/` is the project's
own state, which upstream expects to create and write at runtime.

Blind substitution therefore redirected project state into immutable shared
storage, in instructions an agent then follows.

## Measurement

Controlled traversal of the synced surface (2,205 files, 173 skill
directories) found **30 distinct pack references, of which 14 do not exist
in the store**, carried by **32 files**. The ticket's write-verb audit had
recorded 4 files.

Every one of the 14 originates as `{project-root}/_bmad/...` in the pack
source. None arrived absolute. Our rewrite created all of them.

Three classes, only the first of which the ticket had named:

| Class | Examples | Hits |
|---|---|---|
| Config write targets | `config.yaml`, `config.user.yaml`, `module-help.csv` | 22, 22, 1 |
| Runtime state | `memory/{skillName}/` (the agent-builder sanctum, described in-file as "the place it reloads on every waking"), `planning/prd.md` | 14, 2 |
| Customization | `custom/*.toml`, the surface `bmad-customize` exists to write | 1+ |

## The rule

The store is installed read-only (`pack.FreezeTree`, 0555/0444, shipped in
`aae-orc-dihj`). A path that is not in the store can never come into being
there: a read through it fails, a write through it fails with EACCES.

So the read-versus-write distinction the ticket asked us to detect collapses
into a question with a definite answer:

> Rewrite `{project-root}/<prefix>/X` to `<store>/X` only if `<store>/X`
> exists. Otherwise leave the token literal.

Freezing turned an intent question into an existence test. The classifier
carries no opinion about write verbs, flag names, or redirects, which is
what makes it reliable. It separated all 30 references correctly: 16 reads
rewritten, 14 defects left literal.

Left-literal references are not orphaned. The fallback-resolution footer
already instructs the reader to try cwd-relative first, which is the correct
resolution for project state.

## One case existence cannot decide

`{project-root}/_bmad/custom/` exists in the store, so existence alone would
rewrite it. It must not be rewritten: `pack.yaml` declares
`custom_bridge: _bmad/custom -> _bmad-custom`, making it the repo's own
writable territory.

That case is settled by declaration, not heuristic. `custom_bridge`'s
`upstream_path` is read at sync time and its shim-relative segment is
preserved alongside `_bmad-custom/` and `_bmad-output/`. The two rules are
complementary: existence covers what the store lacks, declaration covers
what the store has but does not own.

## The post-condition

`packRefRules.verify` asserts, after rewrite, that no path resolving inside
the store is absent from it. It runs independently of the rewrite rather
than trusting it, so a reference that arrived absolute in the pack source is
caught too. Structural, so it may gate per
`.claude/rules/diagnostic-not-gate.md`; a violation fails the binding and
skips stale reconcile, per the loud-failure path from sideshow#108/#113.

## Verification

End-to-end sync of bmad 6.10.0 into a sandboxed HOME: 119 artifacts, zero
dangling references, down from 14 across 32 files. Legitimate reads such as
`scripts/resolve_customization.py` still resolve to the store.

## Method note, which is this finding's own lesson recurring

`grep -r` under `~/.claude/skills` returns zero matches for a string present
in 120 files, with empty stderr and an exit status indistinguishable from a
clean run. `find -print0 | xargs -0 grep` returns the correct count. The
first passes of the ticket's audit were run with `grep -r` and reported a
clean bill that was false.

Any scan of this surface must use controlled traversal and must assert a
known-positive fixture, or it will pass by failing to look. The scan behind
this finding aborts if its traversal returns zero.

## Refs

- bd: `aae-orc-c8v8`; `aae-orc-dihj` (freeze, shipped), `aae-orc-mkpo`
  (custom bridge), `aae-orc-3mci` (closed premise-false, folded into c8v8)
- Nodes: `elem-frozen-store-classifies-refs`, `elem-command-sync`,
  `elem-custom-bridge`, `elem-runtime-links`
- Upstream: bmad `bmb#96` covers upstream's own legacy-file deletion. The
  write redirection is ours alone and is unfileable upstream: vanilla bmad
  has no shared immutable store, so upstream cannot reproduce the class.
