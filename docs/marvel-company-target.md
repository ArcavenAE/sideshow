# Installing marvel companies

Status: design, for a ruling. Nothing here is built.

## Why

marvel now has a native object for a library of team, workflow and agent definitions: the company, read from a store marvel never writes (marvel `docs/design/companies.md`). Something has to put versions into that store, check them, and move the active one forward or back. That is a package manager's job, and sideshow already has the model: several versions side by side, one active, a flip that rolls back. This design adds the marvel company as a pack kind with marvel's store as its install target. marvel does not need sideshow; sideshow is one way to fill the store.

## The pack kind

A pack of kind `marvel-company` is a tree with a `company.toml` at its root (marvel's layout, schema 1). While sideshow handles it, it is a pack. Once installed, marvel calls it a company. sideshow validates the layout against marvel's schema version and refuses one it does not know.

## Sources

| source | for | how it is checked |
|---|---|---|
| `--from <dir>` | a local tree, as today | content hash recorded at install |
| a signed release tarball | public libraries, through the existing signed-release path | cosign and attestation, as for other packs, plus the content hash |
| `--from-git <url> --ref <ref>` | a library kept in a private repository | `git verify-commit` on the resolved commit against the signers the user trusts; refused unverified unless `--allow-unsigned` |

The git source uses the user's own git credentials. sideshow stores none. It keeps one bare mirror per URL under its own cache. The mirror is a cache: mutable, never read by marvel, safe to delete.

## Master, mirror and installed copies

- **Master:** the source repository, edited by its owner's review flow. sideshow never writes it.
- **Mirror:** sideshow's cache of the master. It is fetched, never edited by hand, and never installed from directly.
- **Installed version:** `git archive <commit>` of the mirror (no `.git`), or the unpacked tarball, written into marvel's store. It is immutable.

## Install, atomically

1. Resolve the ref to a commit and verify it (git source), or verify the tarball.
2. Write the tree to `<store>/<name>/<version>.partial`. The version string is `<commit date>-<short sha>` for a git source, or the release version.
3. Validate `company.toml`, make the tree read-only, and record its content hash in sideshow's ledger.
4. Rename `<version>.partial` to `<version>`. A version that already exists is never overwritten. The rename is refused, and `--force` does not apply to this kind.
5. With `--activate`, point `<store>/<name>/current` at the new version by renaming a new symlink over the old one.

A failure at any step leaves the previous `current` live and no partial directory behind.

## Verbs, and which side owns each

| verb | owner | does |
|---|---|---|
| `sideshow install <name> ...` | sideshow | steps 1 to 4 |
| `sideshow activate <name> <version>` | sideshow | flips `current` |
| `sideshow update <name>` | sideshow | re-resolves the recorded ref (for example `main`). If it names a newer verified commit, it installs it and activates it when the name was installed with `--track` |
| `sideshow list <name>` | sideshow | installed versions, `*` on current |
| `sideshow remove <name> <version>` | sideshow | refuses the current version. When `marvel` is on PATH, it also refuses a version `marvel get companies` reports in use; without marvel it warns and asks |
| `sideshow doctor` | sideshow | re-hashes installed versions against the ledger |
| reading, pinning, refusing a changed tree, recording a cast | marvel | as in marvel's design |

The contract between the two is the store layout, the `current` symlink, and `company.toml` schema 1. Neither calls the other on the launch path.

## Updates with no one at the host

`sideshow update <name>` is idempotent and safe to run from a scheduled job owned by the user: it changes nothing unless a newer verified commit exists. Running sessions keep the version they launched with; the next launch or shift takes the new `current`. Rollback is `sideshow activate <name> <previous>` or a revert in the master, which the next update installs.

## Tests

- Given a git source whose ref resolves to an unsigned commit, when `install` runs without `--allow-unsigned`, then it refuses and the store is unchanged.
- Given an install killed after step 2, when `list` runs, then the partial version is absent and `current` is unchanged.
- Given version V installed, when `install` produces V again, then it refuses and V's bytes are unchanged.
- Given `update` with no newer commit, then nothing is written (store metadata hash equal before and after).
- Given `remove` of the current version, then it refuses.
- All of the above in a scratch `SIDESHOW_HOME` and a scratch `XDG_DATA_HOME`.

## Open for the ruling

- `--track` (activate on update) as the default for git sources. I recommend opt-in, so that a first install never moves a running fleet's `current` by surprise.

This recommendation is valid until 2026-11-08; the architect re-checks it then.
