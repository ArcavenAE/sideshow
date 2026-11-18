# Installing marvel companies

Status: design, for a ruling. Nothing here is built.

## Why

marvel now has a native object for a library of team, workflow and agent definitions: the company, read from roots marvel never writes (marvel `docs/design/companies.md`). Something has to put versions on disk, check them, and move the active one forward or back. That is a package manager's job, and sideshow already has the model: several versions side by side, one `current`, a switch that rolls back. This design lets sideshow install a company library the way it installs any pack, into its own store, and lets marvel read that store. marvel does not need sideshow; sideshow is one way to fill a root.

## Within the current phase

marvel's CLAUDE.md summarizes the phasing this works inside: "**Now:** sideshow installs packs; marvel consumes what is on the host filesystem as-is. ... **Later:** self-contained workspaces over a subPath-like virtual filesystem, with sideshow gaining a marvel-aware install target; gated on a workspace-VFS study."

So this design stays in the Now phase:

- sideshow installs a company library into **its own store**, `~/.local/share/sideshow/packs/<name>/<version>/` with its existing `current` symlink, exactly as for any other pack;
- marvel reads that directory because the user lists it in marvel's `companies.roots`;
- sideshow does not write into any marvel path, does not validate marvel's schema, and does not call marvel.

The marvel-aware parts are listed under "Gated" below, as one operator call. Nothing here argues past the gate.

## Versions

A library's versions are semantic versions. A release is `1.2.0`: the first number changes when a change can break a team that uses it, the second when roles are added, the third for fixes. An early build for testing is a prerelease, `1.3.0-alpha.1`, the form vsdd-factory already uses (`1.0.0-rc.23`). A prerelease is never picked unless asked for.

## Install kinds

There are three, and each records what it came from.

| kind | command | for | how it is checked |
|---|---|---|---|
| release | `sideshow install <name>@<version>` (or `@latest`, the newest release) | a published release of a public library | the checksum and the cosign signature of the release tarball, plus the tree hash |
| alpha | `sideshow install <name>@alpha` | the newest prerelease; still a built, signed release, marked early | the same checks as a release |
| developer | `sideshow install <name> --dev <checkout>` | a library's author testing a change with no release, no tag and no network | none beyond the tree hash; it is the operator's own checkout |

**Release and alpha.** The release build makes a tarball of the pack, signs it with a short-lived certificate tied to the build job (cosign keyless signing), publishes the tarball's checksum and attaches both to a release, as the bmad releases already do. sideshow downloads the tarball and its checksum file, checks both, and refuses a tarball that fails either. Today a person downloads and checks it, then runs `sideshow install <name> --from <folder>`; the `@` forms make that one command. A prerelease is chosen only by `@alpha` or by its exact version.

**Developer.** `--dev <checkout>` copies the checkout's `company/` folder into the store as its own version, named `<next version>-dev.<date and time>.<short commit id>`, for example `1.3.0-dev.20261118193000.90e8904`. It refuses a checkout with uncommitted changes. sideshow marks it as a developer install, update never picks it, and `sideshow use` moves back to a release. It copies; it does not link to the live checkout, since a link would let an edit change what running seats read, which the "never changed in place" rule below forbids.

**`--from <dir>`** stays as today: a local tree, with the tree hash recorded at install. It is also what the installs above run through once their checks pass.

**What an install records:** the version, the full commit id the version was built from, and the source: the release download address and the tarball's checksum, or the local checkout path for a developer install. Before installing, sideshow refuses a source that holds a file named `origin.json` at any depth, and a source inside its own store.

## Master, reference and working copies

marvel's design defines three tiers; sideshow handles the first two.

- **Master:** the library's git repository, edited by its owner's review flow. It stays the source. sideshow never writes it.
- **Reference copy:** one installed version, the unpacked release tarball or the copied developer folder, in sideshow's store. Immutable. **Only sideshow writes its store.**
- **Working copy:** what a session runs from. marvel derives it at launch, with a digest back to the reference copy. sideshow never installs from one: `install` refuses a source that contains a file named `origin.json` at any depth, and a source inside its own store. It reads no marvel config to do so, which keeps it inside the current phase.

### How "only sideshow writes its store" is enforced

- **Against seats:** every session marvel launches runs under marvel's write-deny profile for every company root (marvel design, E1), and sideshow's store is one, so a seat's write fails with the sandbox's own error.
- **Detection, always:** read-only modes and the recorded tree hash, re-checked by `sideshow doctor`. These detect a change; they do not prevent one.

Nothing installed is ever changed in place: a running seat keeps the copy it loaded, and a new version is a new directory.

## Install steps

All steps run under a per-name lock, `<store>/<name>/.lock` (`flock`, non-blocking). A second install of the same name refuses while the lock is held. Writes to the shared `registry.yaml` take the store-wide registry lock as well.

**The install record is per name,** `<store>/<name>/.ledger`: one row per installed version (version, commit, kind, source, tree hash, install time). It is not sideshow's shared `repo-bindings.yaml`, which one per-name lock could not protect, since two names would write it at once. Only a holder of that name's lock writes it, by writing `.ledger.tmp` and renaming it over `.ledger`, so a reader sees the old file or the new one. The dot name keeps it out of marvel's listing, and the version listings already skip files. Every verb that writes it (install, use, remove) takes the lock first; `doctor` and `list` only read it and take no lock.

1. **Sweep:** remove any `<store>/<name>/.*.partial` left by an earlier run, restoring the owner's write bits first, since a run killed after step 4 leaves a read-only partial that cannot otherwise be removed. The lock makes this safe: no other writer can own one.
2. Check the source as its kind requires (see Install kinds).
3. Write the tree to `<store>/<name>/.<version>.partial`. The leading dot keeps it out of marvel's listing, which skips dot entries. The version string is the release version, or the developer version above.
4. Compute the tree hash (v1, as marvel's design defines it, so the two recorders agree) and record it in the install record. Clear every write bit on every file and directory. Mode bits only detect a change, since an owner can set them back; `sideshow doctor` re-hashes against the record.
5. Move `.<version>.partial` to `<version>` with a rename that refuses an existing target: `renameat2(RENAME_NOREPLACE)` on Linux, `renamex_np(RENAME_EXCL)` on macOS. Plain `rename(2)` would replace an empty directory. A version that exists is never overwritten, and `--force` does not apply to this kind.
6. The first install of a name becomes the active version, since marvel refuses a name with no `current`. A later install activates too, unless `--no-activate` is given; activating points `current` at the new version by renaming a new symlink over the old one.

A kill at any step leaves the previous `current` live. A partial left by a kill is invisible to marvel (dot name) and removed by the next run's sweep.

## Update

`sideshow update <name>` installs the newest release in the channel the install chose: `@latest` follows releases, `@alpha` follows prereleases, and an exact version never moves. It never picks a developer install. It writes nothing when the newest release is already installed, so a user-owned scheduled job can run it.

Moving the active version is always `sideshow use <name> <version>`. It flips `current` under the same per-name lock as install, so it cannot interleave with an update, re-syncs bindings, and is how a rollback is done: `sideshow use <name> <previous>`. (`sideshow activate` is a different command: it flips a pack's persona per repo and does not move `current`.) On a fleet host every `use` is a host step that director approves, because every seat restart takes the active version.

sideshow reads no XDG variable: its store is `SIDESHOW_HOME`, else `$HOME/.local/share/sideshow` (`internal/pack/pack.go`, `SideshowDir`). Under launchd or cron the risk is a different `HOME` or a missing `SIDESHOW_HOME`, so a job should set `SIDESHOW_HOME` explicitly, and `sideshow doctor` prints the path it resolved. A job that registers the store with marvel registers `"$SIDESHOW_HOME/packs"` from that same value, so the root marvel reads is the store sideshow writes; it never names a literal path. marvel reads its roots from its own config, not the environment, so it is unaffected.

## Verbs

| verb | does |
|---|---|
| `sideshow install <name>@<version>` | release install (also `@latest`, `@alpha`) |
| `sideshow install <name> --dev <checkout>` | developer install |
| `sideshow install <name> --from <dir>` | install from a local tree |
| `sideshow use <name> <version>` | flips `current`, under the same per-name lock as install, so it cannot interleave with an update |
| `sideshow update <name>` | as above |
| `sideshow list <name>` | installed versions, `*` on current |
| `sideshow remove <name> <version>` | under the same per-name lock; refuses the current version. A marvel team pinned to an exact version may still use another; the command prints that `marvel get companies` shows which |
| `sideshow doctor` | re-hashes installed versions against the record; lists stale partials and held locks |

marvel owns reading, pinning, refusing a changed tree and recording a cast. The contract between the two is a directory layout (`<name>/<version>/`, `current`, dot entries skipped) and the tree hash definition. Neither calls the other.

## Gated: a marvel-aware sideshow

These wait on the operator lifting the workspace-VFS gate, and are not part of this design's build:

- installing directly into a marvel-owned path;
- validating `company.toml` against marvel's schema at install;
- `remove` asking marvel which versions are in use, and refusing them.

## Build note

The version listing skips dot entries, so a `.<version>.partial` directory is not listed as a version: `InstalledVersions` (`internal/pack/pack.go`) and doctor's layer 1 version walk (`internal/doctor/layer1.go`) both skip them. (`.lock` is a file, and both already skip files.)

## Tests

- Given a release tarball with a wrong checksum, or a signature that does not verify, or a signer other than the release build job, when `install` runs, then it refuses and the store is unchanged.
- Given releases 1.2.0 and 1.3.0-alpha.1, then `@latest` installs 1.2.0 and `@alpha` installs 1.3.0-alpha.1, and no other form picks the prerelease.
- Given a checkout, then `--dev` installs `<next>-dev.<time>.<sha7>`, records the checkout path, and refuses a checkout with uncommitted changes; `update` skips that version, and `use` moves back to a release.
- Given an install killed after step 3, and again after step 4 (a read-only partial), then the only entry it left is dot-prefixed (so a reader that skips dot entries, as marvel does, sees nothing), `sideshow list` does not show it, and the next `install` sweeps it and completes.
- Given installs of two different names started together, then each name's record holds its own row, `registry.yaml` lists both, and neither run lost the other's.
- Given a `remove` holding a name's lock, when `update` of that name runs, then `update` refuses on the lock; `doctor` run at the same time reads the old or the new record, never a partial one.
- Given an `update` holding the lock, when `use` runs, then `use` refuses on the lock and `current` is unchanged.
- Given a source directory holding an `origin.json`, or inside sideshow's own store, then `install` refuses it.
- Given two installs of one name started together, then the second refuses on the lock and the first completes.
- Given an empty directory already at `<version>`, then step 5 refuses and the directory is unchanged.
- Given `update` with no new release, then nothing is written (store metadata hash equal before and after).
- Given a reader that reads `current` while `use` runs, then it never finds the link missing.
- Given an installed version with one directory made writable, then `doctor` reports it; given one file changed, then `doctor` reports the hash mismatch.
- All of the above in a scratch `SIDESHOW_HOME`.

## Open for the ruling

1. **The workspace-VFS gate:** whether to lift it for the items under "Gated". I recommend keeping it, since this design works without them.
2. **The developer install copies rather than links.** If a live link to the checkout is what was wanted, that is a separate question, since a link would let an edit change what running seats read.
