# CCMP parity fixture

`before/` is a synthetic fresh-install tree: the pack-owned files a bmad
install produces, with no CCMP customization applied.

`after/` is the output of running the real
`i-orc/ccmp/scripts/bmad-post-update.sh` (240 lines of shell, the prior art
this engine replaces) against a byte-identical copy of `before/`. It was
captured by execution, not written by hand.

`weave.yaml` is a copy of `examples/ccmp/weave.yaml`.

The parity test applies the declaration to `before/` and asserts the result is
byte-identical to `after/`. It is a golden fixture rather than a live shell
invocation so the test does not require the ccmp checkout to be present.

Verified separately at capture time, and not reproducible from these fixtures:

- Both sides are idempotent. A second run of each leaves both trees
  byte-identical to the first.
- With the `# Add custom menu` anchor absent from the customize files, the
  shell deletes the existing memories block, never reaches an insertion point,
  and exits 0 reporting `[OK] All custom content verified`. The engine leaves
  both files byte-identical and reports two failures. This is the one
  intentional divergence; see docs/pack-weaving-spec.md, memory_injections.
