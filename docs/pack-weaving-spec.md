# Pack-weaving spec: declarative post-install operations

Status: v0.1.0 draft. Owning beads: `aae-orc-n8w7` (this spec),
`aae-orc-036p` (rule inventory, see
[`rule-inventory.md`](rule-inventory.md)), `aae-orc-kugb` (Go
implementation and first port). Umbrella: `aae-orc-a44c`. Empirical
grounding: aae-orc `finding-029`.

## What weaving is

A content pack installs into a repo, and then the repo needs work done
that the pack itself cannot know about. A project has its own custom
agent, its own memories, its own slash commands, its own patched
upstream files. The installer does not know they exist, and on the next
upgrade it overwrites the directory they lived in.

Five projects solved this the same way and independently: a
`bmad-post-update.sh` script, run by hand after every install, that
re-applies the project's local additions to the freshly installed tree.
About 2500 lines of shell across switchboard, ftc, and three further
projects. Weaving is that script turned into data.

The one principle everything below derives from:

> **The installer owns the pack directory. The repo owns everything it
> adds to it. Weaving is how the repo's additions survive the
> installer.**

## Scope

In scope: operations that run after `sideshow install` has placed pack
content, and that reconcile repo-local additions into it. The pack
directory is treated as replaceable; the weave declaration and the
`_{pack}-custom/` tree are the durable state.

Not in scope, and deliberately so:

- Mutating published pack artifacts. Overlays handle retroactive
  correction (`aae-orc-10vq`); weaving handles repo-local addition.
  Different problems, different artifacts.
- Executing project-supplied code. A weave declaration is data.
  Sideshow does not run project shell. The one exception is the
  subprocess rule-type in verification, which invokes a pinned,
  named external checker and never an inline script.
- Deciding what a project wants. Weaving re-applies a declaration the
  project authored. It does not infer, merge, or resolve intent.

## The declaration

One file per repo, at `_{pack}-custom/weave.yaml`. Keyed by pack, so a
repo with two packs has two files. Per the
[schema-versioning rule](schema-versioning.md), `schema_version` is
required.

```yaml
schema_version: 0.1.0
pack: bmad
vars:
  project: midway
custom_agents: [...]
csv_injections: [...]
memory_injections: [...]
slash_commands: [...]
apply_upstream_patches: none
verification: {...}
```

`vars` is a flat string map, substituted into `{{name}}` occurrences in
target paths, guards, and ids. Substitution is textual and happens
before any operation runs. A `{{name}}` with no matching var is an
error, not an empty string: the five shell scripts all interpolated
`$PROJECT` style variables, and a silent empty substitution there would
write to a path one segment short of the intended one.

A worked example is at
[`examples/midway/weave.yaml`](../examples/midway/weave.yaml). `midway` is a
synthetic project standing in for one of the five, so the published example
carries no site-specific operational detail; it was derived from a real script
by substituting the project name and the memory strings, then executing the
result. See
[`internal/weave/testdata/midway/PROVENANCE.md`](../internal/weave/testdata/midway/PROVENANCE.md)
for what that substitution changed and what the fixture does and does not
establish.

## Operation types

Five, plus a verification stage. The count is not a design target: it
is what the five scripts between them actually do. Config-patches (a
sixth candidate, editing `_{pack}/_config/*.yaml` in place) is deferred
until a second project needs it, per gradual elaboration.

### 1. `csv_injections`

Append rows to a pack-owned CSV that the installer regenerates.

```yaml
- name: agent-manifest
  target: _bmad/_config/agent-manifest.csv
  guard: '"platform-engineer"'
  rows:
    - |-
      "platform-engineer","Sam",...
```

`guard` is a fixed substring. If the target already contains it, the
whole injection is skipped. This is the shell prior art's idempotency
mechanism verbatim (`grep -q`), and it is worth keeping because it is
the projects' own declared identity for the row set: they chose what
substring means "mine is already here."

Rows are appended verbatim, byte for byte, with no CSV parsing. This is
deliberate. The upstream installer's own parser accumulates fields
without trimming (see `rule-inventory.md`, the no-trim contract), so a
row that a well-behaved CSV writer would normalize is a row that
behaves differently at runtime. Sideshow appends what the project
wrote.

Requirements the engine adds over the shell:

- Target must exist. The scripts printed `[ERROR]` and carried on with
  `set -e` active but the failing command inside an `if`, so a missing
  manifest silently produced a partial weave.
- Trailing-newline normalization. A file whose last byte is not `\n`
  gets one before the append, otherwise the first appended row fuses
  onto the last existing row. None of the five scripts handled this.

### 2. `memory_injections`

Replace a YAML block in one or more customize files.

```yaml
- targets:
    - _bmad/_config/agents/bmm-architect.customize.yaml
    - _bmad/_config/agents/bmm-dev.customize.yaml
  anchor: '# Add custom menu'
  on_missing_anchor: fail
  memories:
    - Midway runs its services on a managed Kubernetes cluster behind a single ingress
```

Semantics, matching the awk the scripts use: delete any existing
`memories:` key and every line under it up to the next top-level key,
then insert a fresh `memories:` block immediately before the anchor
line, followed by one blank line. Idempotent by construction, since the
delete precedes the insert.

`on_missing_anchor` exists because the shell version has no such
choice, and its behavior when the anchor is absent is worse than a
no-op: the awk deletes the old memories block and then never reaches an
insertion point, so the memories are gone and the run still reports
success. `fail` is the default. `skip` (leave the file untouched) is
available for targets that are optional. There is no `append` mode
until a project needs one.

Memory strings are emitted double-quoted. No script escaped the value,
so a memory containing a quote or a backslash would have produced
invalid YAML; none does today, and the engine escapes properly rather
than reproducing a latent break.

### 3. `slash_commands`

Generate a harness command file that points at a custom agent.

```yaml
- id: bmad-agent-custom-{{project}}-sam
  target: .claude/commands/bmad-agent-custom-{{project}}-sam.md
  on_exists: skip
  body: |
    ---
    name: 'sam-platform-engineer'
    ...
```

`on_exists: skip` matches the scripts, which all check for the file and
leave it alone. `overwrite` is available for projects that want the
body to be authoritative. The default stays `skip`: a human who edited
the shim should not lose the edit to a reinstall.

The body is carried verbatim rather than generated from a template
because the five scripts' shims are not identical and the differences
are not systematic. Once sideshow owns a canonical shim shape, a
`template:` alternative to `body:` can render it, and `body:` becomes
the escape hatch. That shape does not exist in sideshow today; the
`bindings` package writes command files but not agent-activation shims.

### 4. `apply_upstream_patches`

Re-apply project-local edits to installer-owned files.

```yaml
apply_upstream_patches: none        # or a list of patch declarations
```

No surface in the first port. finding-029's per-project table lists that
project as applying patches C1 C2 E1 E2 I1 I3; reading its script shows it
contains no patch code at all. The correction is recorded in `finding-097`. This
operation therefore has no verified prior art in the first port, and
its declaration shape stays a stub until one of the other four scripts
supplies it. Writing the schema now, from the table rather than from
code, is how the table's error would propagate into the engine.

### 5. `verification`

Assert the woven state is present.

```yaml
verification:
  on_failure: warn
  required_files: [...]
  csv_contains:
    - target: _bmad/_config/agent-manifest.csv
      needle: '"platform-engineer"'
```

**Warn is the default, and this is the most consequential default in
the spec.** Three independent lines of evidence point the same way.
The first ported script computes a `VERIFY_OK` flag, prints `[OK]` or
`[WARN]`, and contains no `exit` statement anywhere in its 240 lines: it
always exits 0. BMAD-METHOD's own file-reference validator shipped blocking in
PR #1490 and was reverted to warning before merge in #1494, precisely
because it turned CI red against an existing corpus. Two months later,
strict promotion is still an unshipped step in both that repo and
bmad-builder. A checker introduced against content that predates it
needs a grace period, and bolting `--strict` on afterward is the shape
that does not get adopted.

`strict` is available per-run and per-repo. It is opt-in.

## Orchestration

Order is fixed, not declaration order, because the operations have real
dependencies:

1. `csv_injections` (registers the agent so later steps can reference it)
2. `memory_injections`
3. `slash_commands`
4. `apply_upstream_patches`
5. `verification`

Within a type, declaration order holds. This matches the numbered
section order every one of the five scripts converged on independently,
which is reasonable evidence that the ordering is load-bearing rather
than incidental.

Weaving runs at the end of `runProjectInitForPack`, after content
distribution and binding registration, since it operates on the tree
those steps produce.

## Error semantics

Three outcomes per operation, not two. The third one is the point.

| Outcome | Meaning | Effect on exit |
|---|---|---|
| applied | the operation changed the tree | none |
| skipped | a guard matched, or the target was optional and absent | none, but reported and counted |
| failed | a required target was missing, an anchor was absent under `fail`, a var was unresolved | warn by default, error under strict |

Skipped is reported and counted rather than silent. Both Node
validators upstream learned this the hard way and in the same place:
their silent-skip paths (extensionless refs, unparseable YAML,
unresolvable invoke-task targets) hid real breakage behind a clean run.
A weave that skipped every operation because a guard matched something
unintended should look different from a weave that applied all of them.

Failures do not abort the remaining operations. A missing
`bmad-help.csv` should not prevent the memories from being injected.
Failures accumulate and are reported together at the end.

## Version compatibility

`schema_version` follows the [schema-versioning rule](schema-versioning.md).
For 0.x, sideshow accepts an exact minor match and refuses a higher
minor with a message naming the sideshow version that added it. An
unknown top-level key is a warning, not an error, so a repo can carry a
declaration for a newer sideshow without breaking an older one. An
unknown key *inside* a recognized operation is an error, since silently
ignoring, for example, a misspelled `on_missing_anchor` would mean
running the destructive default.

## Open questions

- **Multi-pack ordering.** A repo with two packs has two weave files.
  Nothing yet says which runs first, or what happens when both inject
  into the same CSV.
- **Weave and overlay interaction.** An overlay corrects published pack
  content; a weave adds repo-local content. If both touch the same
  file, order and precedence are undefined. This blocks nothing today,
  because no overlay exists yet (`aae-orc-10vq`).
- **Doctor layer 2.** The engine's verification stage is the natural
  implementation of sideshow doctor's pack-declared validation layer
  (`aae-orc-xteh`, `aae-orc-zf2a`). Whether doctor calls the weave
  engine or the two share a findings type is not decided.
- **`config-patches`.** Deferred, per above.
- **Per-repo vs per-pack authority.** `weave.yaml` lives in the repo,
  so the repo decides. A pack that wants to require a weave operation
  has no way to say so. This may be correct.

## Cross-references

- [`rule-inventory.md`](rule-inventory.md): the verification rule-types,
  inventoried from upstream prior art (`aae-orc-036p`)
- [`examples/midway/weave.yaml`](../examples/midway/weave.yaml): worked
  example, derived from the first port's shell original
- [`schema-versioning.md`](schema-versioning.md): the `schema_version` rule
- [`customization-bridge.md`](customization-bridge.md): how
  `_{pack}-custom/` reaches upstream's own customization surface
- aae-orc `finding-029`: the discovery that a file-ref validator ticket
  was actually a post-install weaving engine
