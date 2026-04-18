# Schema Versioning Rule

**Status:** committed, session-029 (aae-orc-xe7l)

## The rule

Every declarative contract sideshow ships or consumes MUST declare
`schema_version:` as the first key (or near the top) of its YAML/TOML
document. Sideshow validates the declared version against a compatibility
table at parse time.

Applies to:

- `pack.yaml` — pack manifest
- `sideshow.toml` — orc-declared pack pinning
- `sideshow.lock` — lockfile
- `overlay.yaml` — overlay artifact manifest
- Known-defects registry feed documents
- Any future declarative contract sideshow introduces

## Why

Without an explicit schema version, a schema change is silently
ambiguous — consumers read an old document under new rules (or vice
versa) and fail with confusing errors or, worse, silent data
corruption. This is the protocol-crisis-prevention argument from
session-029 party 1, round 3 (Winston's point).

## Version format

Semantic: `major.minor`.

- **Major** bump for breaking changes (removed/renamed fields, changed
  semantics of existing fields, different file format entirely).
- **Minor** bump for additive changes (new optional fields, new
  permitted values in an enum).

Sideshow ships a compatibility table that declares for each contract:

- Current version
- Minimum supported version
- Refusal behavior on unknown versions (`error` vs `warn-and-proceed`)

## Default behaviors

- **Unknown version** → refuse to parse, error with actionable message
  pointing at the compatibility table.
- **Old supported version** → parse with legacy rules; emit a warning
  suggesting upgrade.
- **Current version** → parse cleanly.

## Examples

```yaml
# pack.yaml
schema_version: 1.0
name: bmad
version: 6.3.0
# ...rest of fields
```

```toml
# sideshow.lock
schema_version = "1.0"

[[packs]]
name = "bmad"
version = "6.3.0"
# ...
```

## Migration handling

When a major version bumps, sideshow ships a migration helper:
`sideshow migrate <file> --to <version>`. Old versions remain readable
(with warning) for one minor release past the migration before being
removed from the compatibility table.

## References

- Analog: JSON Schema `$schema` URI, aqua registry version pinning,
  semver throughout the language-tooling ecosystem.
- aae-orc-xe7l — the issue this rule was filed against.
- aae-orc-mezl — pluggable source backends rely on schema versioning
  to evolve the `install.source` contract.
