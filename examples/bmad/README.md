# Example: bmad end-to-end — verified install, activation, and a working agent roster

The full path from a signed release to `/bmad-party-mode` seating your
real installed agents. Every step is copy-paste; every artifact is
verifiable.

## 1. Download and verify a signed release

Releases are published by
[sideshow-packs](https://github.com/ArcavenAE/sideshow-packs) — built
in CI from pinned upstream sources, cosign-signed, Sigstore-attested.

Prerequisites: `gh`, `cosign`, and `python3` (macOS:
`brew install gh cosign`). Every verification step below fails
without them.

The `-r2` in the tag and artifact name is a packaging revision: the
same upstream bmad, re-issued with corrected packaging (the original
releases predate the pack.yaml `runtime_links` emit; sideshow#109).
Always prefer the highest revision of a version. The extracted
directory carries no revision.

```bash
mkdir -p /tmp/bmad-install && cd /tmp/bmad-install
gh release download bmad-v6.10.0-r2 -R ArcavenAE/sideshow-packs --clobber \
  -p 'bmad-6.10.0-r2-arcaven.tar.gz*' -p 'install.meta.json'

# Integrity: tarball sha256 must match the provenance manifest
shasum -a 256 -c <(python3 -c \
  "import json; m=json.load(open('install.meta.json')); \
   print(m['artifact']['tarball_sha256'], ' bmad-6.10.0-r2-arcaven.tar.gz')")

# Authenticity: cosign keyless verification against the CI identity
cosign verify-blob \
  --bundle bmad-6.10.0-r2-arcaven.tar.gz.bundle \
  --certificate-identity-regexp 'github.com/ArcavenAE/sideshow-packs' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  bmad-6.10.0-r2-arcaven.tar.gz       # expect: Verified OK

tar -xzf bmad-6.10.0-r2-arcaven.tar.gz
```

## 2. Install and activate

```bash
sideshow install bmad --from /tmp/bmad-install/bmad-6.10.0 --no-activate
sideshow list                 # new version present; active version unchanged
sideshow use bmad 6.10.0      # flip + re-sync; stale bindings from the
                              # previous version are removed automatically
sideshow status               # available == synced means fully wired
```

## 3. Wire a repo (the runtime shim)

bmad's resolvers (party-mode roster, the four-file config chain) expect
content at `{project-root}/_bmad/`. One command makes that true:

```bash
cd ~/work/myrepo
sideshow project init bmad
```

This creates, gitignored: `_bmad/scripts`, `_bmad/_config`,
`_bmad/config.toml`, `_bmad/config.user.toml` (symlinks into the store,
through `current` — version flips keep working) and
`_bmad/custom -> ../_bmad-custom` (checked-in customization that
survives upgrades, seeded with the customization templates).

## 4. Test the roster

Cheap check, no session needed:

```bash
uv run ~/.claude/skills/bmad-party-mode/scripts/resolve_party.py \
  --project-root "$PWD" --skill ~/.claude/skills/bmad-party-mode \
  | python3 -m json.tool | head -20
```

`"installed_agents_resolved": true` with a populated `members` list is
the win. Then launch `/bmad-party-mode` in your AI session — the
installed agents (Mary, Winston, Amelia, Murat, …) walk in, not
improvised stand-ins.

## 5. Rollback, any time

```bash
sideshow use bmad 6.3.0   # flip back; bindings reconcile; customization
                          # in _bmad-custom/ is untouched either way
```

## Notes

- The store is read-only by design intent — if a skill ever tries to
  write into `_bmad/` through the shim, it fails loudly instead of
  corrupting shared content. Project-local writes belong in
  `_bmad-custom/` and `_bmad-output/`.
- Repos that already carry a real upstream `_bmad/` install get
  conflict reports instead of links — existing content is never
  replaced.
- Resolution model in depth: [`../../docs/path-resolution.md`](../../docs/path-resolution.md).

## FAQ: do I re-run `project init` when skills change?

Almost never. Pack upgrades and version flips are handled by
`sideshow use` (bindings) and the through-`current` symlinks (shim).
Your own additions — persona cards in `_bmad-custom/agents/`, party
members in customize overrides — flow through the bridge live.

Re-run `sideshow project init bmad` only when a newer pack version
*declares new surfaces* (an added `runtime_links` entry, new gitignore
lines). It is idempotent: adds what's missing, skips what exists,
never replaces real files.
