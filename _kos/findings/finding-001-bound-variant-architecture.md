# Finding 001: The bound variant, and how far the binding prefix reaches

Date: 2026-08-01
Scope: sideshow (internal/bindings). Extracted from orc finding-096
per the charter scope rule; the orc finding keeps the summary.
Upstream ground truth: orc finding-091 addenda 2+3 (trials T16/T17/
T19/T20); decisions: orc finding-094 (D1) and aae-orc-d3nq.51.

## F001-a: The bound variant resolves frozen-store-vs-transforms

Two ratified requirements looked mutually exclusive. D1 local scope
symlinks bindings into the store, whose content is signed reference
and byte-frozen. The .51 naming policy rewrites content at bind time
(prefix in frontmatter `name:`, namespace references
`<pack>:<name>` to `<prefix>-<name>`). You cannot rewrite content
through a symlink into frozen bytes.

They compose through a machine-scoped derived tree: transforms render
ONCE per (pack, version) into `<sideshow-data>/bound/<pack>/
<version>/` (`BoundVariantDir`), and repos materialize FROM the
variant. Every D1 symlink property survives: one copy per machine,
exec bits live in one place, a version flip is one repoint. The store
stays frozen; the variant is a deterministic derived cache (rebuild
is `RemoveAll` + render, proven idempotent by test) and is the
declared derivative the rewrite manifest (aae-orc-d3nq.61) records.
The exec-manifest census is translated into variant paths so
copy-mode verification (unshaping-spec rule 3) holds end to end.

Code: `internal/bindings/bound_variant.go`. The variant inventory
feeds `MaterializeRepo` unchanged, which is what kept .48's
materializer free of transform knowledge.

## F001-b: The prefix reaches agent frontmatter

The .51 bead carried "NO CHANGE: agent frontmatter name: is already
bare (34/34)", which reads two ways. The implemented reading: that
line was the upstream survey result (nothing namespace-qualified to
strip), not a prohibition. The rule "prefix applies to bound names
and every internal reference", combined with the T16 correction
(a user-scope skill shadows a same-name repo skill) and 34 generic
upstream agent names, forces prefixing the addressable name itself:
`name: github-ops` becomes `name: vsdd-github-ops`, and the 23
`subagent_type` call sites rewrite to the prefixed bare form
(repo scope has no namespace qualifier, trial T20).

Verification: the env-gated real-tree test renders the actual
upstream tree (124 skills, 44 agent files) and asserts zero surviving
namespace-qualified references in rewrite-extension files. Recorded
as a design call in aae-orc-d3nq.51's notes; reversible if the
interpretation is wrong. Known consequence either way: upstream
Grafana LogQL predicates on `vsdd-factory:pr-manager` match nothing
on this channel (divergence-register entry 3).

## Cross-references

- docs/unshaping-spec.md (dispositions and validation rules)
- docs/divergence-register.md (entries 2 and 3 price these choices)
- orc: finding-091 addenda (trial ground truth), finding-094 (D1-D5),
  finding-096 (session record, keeps the summary of this finding)
