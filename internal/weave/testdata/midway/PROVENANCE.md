# midway parity fixture

`midway` is a **synthetic project**. It does not exist. It stands in for one of
the five real projects whose `bmad-post-update.sh` scripts are this engine's
prior art, so the published fixture carries no site-specific operational
detail.

## How this was made

The source is a real 240-line `bmad-post-update.sh`, the shortest of the five.
Two substitutions were applied to a copy of it:

1. the project name, throughout
2. the five `PLATFORM_MEMORIES` strings, and the one verification filename that
   tracks a renamed memory doc

Nothing else changed. Diffing the two scripts with the project name normalized
away shows four differing lines, all of them memory content. Operation order,
row shapes, guard strings, heredoc bodies, and the awk semantics are the
original's.

`before/` is a synthetic fresh-install tree: the pack-owned files a bmad
install produces, with no customization applied.

`after/` is the output of **executing** that modified script against a
byte-identical copy of `before/`. It was captured by running it, not written by
hand.

`weave.yaml` is a copy of `examples/midway/weave.yaml`, whose CSV rows,
memories, and command body were extracted programmatically from the same
script rather than retyped.

The parity test applies the declaration to `before/` and asserts the result is
byte-identical to `after/`.

## What this fixture does and does not establish

It establishes that the engine reproduces the behavior of a real
`bmad-post-update.sh`, on the same operations, in the same order, including the
awk block-replacement semantics.

It does not, by itself, establish byte-parity against the unmodified original.
That was verified separately at porting time against the real script and is
recorded in aae-orc `finding-097`. It is not reproducible from the contents of
this directory, by design.

Also verified at capture time and not reproducible here:

- Both the script and the engine are idempotent. A second run of each leaves
  both trees byte-identical to the first.
- With the `# Add custom menu` anchor absent from the customize files, the
  shell deletes the existing memories block, never reaches an insertion point,
  and exits 0 reporting `[OK] All custom content verified`. The engine leaves
  both files byte-identical and reports two failures. This is the one
  intentional divergence; see `docs/pack-weaving-spec.md`, `memory_injections`.
