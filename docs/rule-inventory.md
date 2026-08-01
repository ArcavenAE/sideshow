# Rule inventory: verification rule-types from upstream prior art

Status: v0.1.0. Owning bead: `aae-orc-036p`. Feeds
[`pack-weaving-spec.md`](pack-weaving-spec.md) § verification and
`aae-orc-hd84` (subprocess rule-type). Sibling: `aae-orc-a44c`.

## Why this document exists

The weave engine's verification stage needs to know what kinds of
assertion it must express. Rather than invent a rule taxonomy, I
inventoried the checkers that already validate BMAD content, catalogued
every check each one performs, and reduced them.

Sources read at pinned states, with claims verified by execution where
the reader could execute them:

| Source | State | What it is |
|---|---|---|
| BMAD-METHOD PR #1494 | merged `ba89077` | `tools/validate-file-refs.js`, 480 lines, the validator that shipped |
| BMAD-METHOD PR #1573 | merged `24cf444` | +175/-101, adds CSV support |
| BMAD-METHOD PR #1490 | closed unmerged, draft | the two-minute predecessor of #1494 |
| bmad-builder issue #7 | closed completed | 18 requested checks, 6 of which have no implementation |
| bmad-builder `tools/validate-file-refs.mjs` | on `main`, 657 lines | the fork, with 10 checks issue #7 never asked for |
| BOHICA-LABS/mdcheck | local clone `fef3f76` | markdown link checker, upstream repo now returns 404 |

## The reduction

Thirty-one catalogued checks reduce to **three rule-types plus two
engine features**. Everything else is table data.

### Rule-type 1: `ref-exists`

One assertion: a reference extracted from a file, rewritten through a
path map, resolves to something that exists. Twenty-four of the
thirty-one checks are this rule with different parameters. Parameters:
file glob, extraction site, rewrite chain, and a three-way outcome.

**Extraction sites needed**, in ascending parsing cost. This is the
list; nothing in either validator requires a markdown AST.

1. Line-wise regex over raw text.
2. Regex over text with fenced code blocks and bare-JSON blocks
   blanked. Blanking must preserve newline counts or line numbers lie.
   PR #1490 stripped without preserving them and reported no line
   numbers, which was at least self-consistent; #1494 fixed both
   together.
3. YAML scalar walk with byte ranges, so a finding can name both a line
   and a key path (`a.b[0].c`).
4. YAML frontmatter keyed lookup against an allowlist.
5. CSV named-column projection.

**Three-way outcome, not two.** Pass, fail, and skip, with skip
reported and counted. Every silent skip in the prior art hides real
breakage: extensionless unresolved paths, unparseable YAML, and
unresolvable `<invoke-task>` targets were all silently dropped in
#1490. #1573 promoted the first of those three to a reported
`[UNRESOLVED]` class and left the other two.

### Rule-type 2: `forbidden-pattern`

Per-line regex that must not match. One instance in the prior art:
absolute-path leak, catching `/Users/`, `/home/`, and Windows drive
paths in content that ships.

Use single-backslash Windows semantics. BMAD-METHOD's regex is
`[A-Z]:\\\\`, which in a JS literal matches a drive letter followed by
*two* literal backslashes. The reader executed it: `C:\Users\dev`, the
normal form in prose, does **not** match; only JSON-escaped forms do.
Lowercase drive letters are also missed. The file's own doc comment
advertises `C:\`. bmad-builder's fork uses `[A-Z]:\\` and matches
correctly. Two readers found this independently.

### Rule-type 3: `subprocess`

Invoke a pinned external checker, parse structured output, map to
findings. One instance: `markdown-links` via mdcheck. This is
`aae-orc-hd84`.

**Material change to hd84's premise: `github.com/BOHICA-LABS/mdcheck`
returns 404.** The reader could not distinguish deleted from private
using the `arcaven` identity; `gh release list` returns nothing and the
repo is absent from both the BOHICA-LABS and drbothen repo lists. The
only pin available is the local clone at commit `fef3f76`, built from
source. Do not reach for `cargo install mdcheck`: the crates.io package
of that name is an unrelated CommonMark linter by a different author,
and that install path silently gets the wrong tool.

Constraints the subprocess rule-type must carry, all measured rather
than inferred:

- **Invoke per tree, never per file.** mdcheck builds its anchor index
  only from discovered files, so running it on one file reports valid
  cross-file anchors as `AnchorNotFound`. Path scoping is a post-filter
  on results, not a narrower invocation.
- **Force offline.** External checking is on by default. Pass
  `--no-external` unconditionally for determinism, and scrub
  `GITHUB_TOKEN` from the child environment: mdcheck injects it as an
  `Authorization` header on an https allowlist when present.
- **Isolate config.** Auto-discovery reads `./mdcheck.toml` relative to
  the working directory. The reader measured the identical invocation
  exiting 1 with such a file present and 0 without it. Unknown keys and
  type mismatches produce warnings that the tool computes and then
  discards, so a partially-ignored config gives no signal.
- **Do not trust exit 0 alone.** An empty directory exits 0 with
  `All 0 links OK`, and a discovery failure is swallowed into
  `NoFilesFound` and also exits 0. Require exit in {0,1} **and**
  parseable JSON **and** `summary.files_scanned > 0` **and** envelope
  `version == "1.0.0"`.
- **Identity is not probeable.** There is no `--version` flag, and
  `--help` prints to stderr with an `error:` prefix and exits 2. Pin by
  path plus checksum, or by recorded commit.
- **Findings have no stable codes**, only a free-text `reason` whose
  prefix carries the class. Parse the prefix, treat the class list as
  undocumented, and pin the tool.

The engine supplies what mdcheck does not: URL-level and link-level
ignores, inline suppression, and per-check toggles. None exist upstream;
all three are post-filters over the results array.

### Engine feature 1: suppression

Not a rule. An engine capability, and one that neither upstream source
specified up front while both ended up needing.

bmad-builder shipped inline `<!-- validate-file-refs:ignore -->` and
`ignore-next-line` comments, matched against the original lines rather
than the stripped ones, and reports a suppressed count in the summary.
That last part matters: a suppression that disappears from the output is
indistinguishable from a check that never ran.

### Engine feature 2: ownership scope

The single largest false-positive class in the prior art was 95 of 154
findings, 62 percent, all from validating references that resolve only
at install time. A rule needs to know which content it owns, and
references outside that ownership need an informational outcome rather
than pass or fail.

bmad-builder auto-detects ownership by reading `src/module.yaml` for a
`code` key, skips non-matching modules by default, and offers
`--include-external` to surface them as `[INFO]`. It fails open: a
missing or unparseable module file disables external detection rather
than blocking. Note that issue #7 asked for the opposite default
(`--skip-external`, off by default); what shipped inverted it, which is
the right call and is worth not re-litigating.

## Table data, not code

Every place the two Node validators diverged is a place a hardcoded
constant should have been configuration. This list is the argument for
making the rewrite chain declarative:

| Table | BMAD-METHOD | bmad-builder | Notes |
|---|---|---|---|
| installed-to-source prefix map | changed three times: `src/modules/` fallback, then plain `src/`, then `core/`→`src/core-skills/` and `bmm/`→`src/bmm-skills/` | drops the owning module segment entirely | any hardcoding of this is wrong within one release |
| unresolvable-variable list | 18 literal substrings | 23 different ones | naive substring containment, so a path merely *containing* `{date}` is dropped |
| install-only prefixes | `_config/` | `docs/` | |
| frontmatter path keys | 6, via regex over raw text | 21, via YAML allowlist | the allowlist approach is sounder: no false hits in prose |
| scanned extensions | `.yaml .yml .md .xml .csv` | `.yaml .yml .md .csv`, no `.xml` | the fork silently dropped four rules with `.xml` |
| abs-path regex | double-backslash, under-detects | single-backslash, correct | |

## Severity

Per rule and per invocation, defaulting to warn. This is the same
conclusion the weave spec reaches from CCMP's always-exit-0 script, and
it arrives here from three more directions. PR #1490 shipped blocking
and was reverted to warning before merge. `--strict` exists in both
validators and **neither repo's CI invokes it**, two months on. Strict
promotion is an open step in both. Adoption against an existing corpus
requires a grace period; a checker that fails CI on introduction does
not get introduced.

## Verified defects, recorded so the port does not reproduce them

Each of these was established by reading or executing the source, not
inferred from documentation.

1. **Windows abs-path regex under-detects.** Covered above. Executed.
2. **The `OK-DIR` branch is unreachable.** #1573's extensionless
   handling sits inside a guard that already established the path does
   not exist, then calls `existsSync` on the same path again. So every
   extensionless missing reference reports as `[UNRESOLVED]` and the
   directory tolerance the comment describes never happens. Implement
   the stated intent as a genuine three-way outcome.
3. **CSV column detection keys on the first data record, not the
   header.** A short first row omits the key, and the entire file is
   skipped even when later rows carry references. Executed.
4. **Only the first match per YAML scalar is captured** for the two
   `{project-root}` and `{_bmad}` patterns, because the extractor calls
   `.match` without the global flag. A scalar carrying two references
   has the second unchecked.
5. **CSV parse errors cannot fail the build**, even under `--strict`.
   They print to stderr while their GitHub annotation goes to stdout,
   and they increment no counter. Deliberate, per the author's review
   reply, and parallel to the silent YAML-parse skip.
6. **Duplicate CSV headers: last column wins**, silently. And a header
   with a leading space produces a key with the space, so the column is
   not detected and the file is skipped. Both executed. These interact
   with the no-trim contract below.

## One upstream contract to preserve

**Do not trim CSV values.** A reviewer asked for `.trim()` on
`workflow-file`; the author declined, because the installer's own
`parseCsvLine` accumulates fields verbatim, so trimming in the
validator would bless data that fails at runtime. This is a stated
contract, not an oversight, and it is the reason
[`pack-weaving-spec.md`](pack-weaving-spec.md) appends CSV rows byte for
byte with no parsing.

Practical consequence for a Go port: `encoding/csv` errors on variable
field counts by default. Real BMAD data has trailing commas, and the
Node validator relies on `relax_column_count`. Set `FieldsPerRecord` to
`-1`. More generally, CSV results depend on the specific library's
behavior under relaxation (short rows omit keys rather than filling
empties) and on last-wins duplicate headers. This is the one place where
porting checks as data needs an explicit compatibility decision rather
than a transcription.

## Gaps: requested and never built

Six items from bmad-builder issue #7 have no implementation the reader
could find, and the issue is closed as completed. Named here because
"upstream does X" is a claim, and for these six it is false:

- `--installed` mode, checking `_bmad/` paths against a real installed
  tree instead of remapping to `src/`. This is the load-bearing gap:
  the entire post-generation validation layer depends on it.
- Post-generation validation for the agent, workflow, and module
  builders. Three items, none built.
- PR review comments via a `workflow_run`-triggered job. Not built; the
  stated reason is that `pull-requests: write` carries fork-PR
  considerations.
- A `validate:refs` CI step in bmad-builder. The validator file exists
  and is wired to nothing: no npm script, no workflow invokes it. Its
  `AGENTS.md` documents five npm scripts that do not exist.

Also note the stated architecture was one validator with a mode flag
and shared extraction patterns. What exists is two forked files, one
CJS and one ESM, with divergent patterns, module mapping, variable
lists, and abs-path regex. There is no upstream implementation of
schema-level checks (column types, enums, required fields, command
uniqueness) or graph checks (step reachability, goto and invoke targets
as a graph). If the engine needs those rule-types, there is nothing to
inventory.

## What this means for the engine

Nothing in any catalogued check requires executing JavaScript. The
whole surface is expressible as data: a file-selection rule, a
preprocessing chain per extension, a list of extractors, one rewrite
table, three skip lists, and two assertions.

Emit structured findings (file, line, key path, rule id, raw reference,
resolved path, severity, outcome) and render presentations from them.
Both Node validators log at the point of detection, which is why their
GitHub annotation escaping (`%`, CR, LF) and step-summary cell escaping
(`|`) are scattered rather than centralized.
