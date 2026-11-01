# Installing marvel companies

Status: design, for a ruling. Nothing here is built.

## Why

marvel now has a native object for a library of team, workflow and agent definitions: the company, read from roots marvel never writes (marvel `docs/design/companies.md`). Something has to put versions on disk, check them, and move the active one forward or back. That is a package manager's job, and sideshow already has the model: several versions side by side, one `current`, a flip that rolls back. This design lets sideshow install a company library the way it installs any pack, into its own store, and lets marvel read that store. marvel does not need sideshow; sideshow is one way to fill a root.

## Within the current phase

marvel's CLAUDE.md summarizes the phasing this works inside: "**Now:** sideshow installs packs; marvel consumes what is on the host filesystem as-is. ... **Later:** self-contained workspaces over a subPath-like virtual filesystem, with sideshow gaining a marvel-aware install target; gated on a workspace-VFS study."

So this design stays in the Now phase:

- sideshow installs a company library into **its own store**, `~/.local/share/sideshow/packs/<name>/<version>/` with its existing `current` symlink, exactly as for any other pack;
- marvel reads that directory because the user lists it in marvel's `companies.roots`;
- sideshow does not write into any marvel path, does not validate marvel's schema, and does not call marvel.

The marvel-aware parts are listed under "Gated" below, as one operator call. Nothing here argues past the gate.

## Sources

| source | for | how it is checked |
|---|---|---|
| `--from <dir>` | a local tree, as today | tree hash recorded at install |
| a signed release tarball | public libraries, through the existing signed-release path | cosign and attestation, as for other packs, plus the tree hash |
| `--from-git <url> --ref <ref>` | a library kept in a private repository | `git verify-commit` on the resolved commit, run with `gpg.minTrustLevel=fully` (an OpenPGP key of undefined trust otherwise passes) or, for SSH signatures, the user's `gpg.ssh.allowedSignersFile`. Refused unverified unless `--allow-unsigned` |

The git source uses the user's own git credential helper. sideshow refuses a URL that carries userinfo (`https://user:secret@host/...` or `https://token@host/...`), because recording that URL in its ledger would put a credential at rest. It keeps one bare mirror per URL in its cache. The mirror is a cache: mutable, never read by marvel, safe to delete.

## Master, reference and working copies

marvel's design defines three tiers; sideshow handles the first two.

- **Master:** the library's git repository, edited by its owner's review flow. It stays the source. sideshow never writes it.
- **Mirror:** sideshow's cache of the master; fetched, never edited, never read by marvel. Not a tier: it is a cache.
- **Reference copy:** one installed version, `git archive <commit>` of the mirror (no `.git`), or the unpacked tarball, in sideshow's store. Immutable. **Only sideshow writes its store.**
- **Working copy:** what a session runs from. marvel derives it at launch, with a digest back to the reference copy. sideshow never installs from one: `install` refuses a source that contains a file named `origin.json` at any depth, and a source inside its own store. It reads no marvel config to do so, which keeps it inside the current phase.

### How "only sideshow writes its store" is enforced

- **Against seats:** every session marvel launches runs under marvel's write-deny profile for every company root (marvel design, E1), and sideshow's store is one, so a seat's write fails with the sandbox's own error.
- **Against people and other tools (opt-in):** the owner can give the store to a dedicated user and run sideshow's writing verbs as that user (`sudo -u`), with the store readable by everyone else and writable by no one else. That takes elevated rights once per host. `sideshow doctor` reports the store's owner, and warns when it is the invoking user's (E2 not in use).
- **Detection, always:** read-only modes and the ledger's tree hash, re-checked by `sideshow doctor`. These detect a change; they do not prevent one.

## Install

All steps run under a per-name lock, `<store>/<name>/.lock` (`flock`, non-blocking). A second install or update of the same name refuses while the lock is held.

**The install ledger is per name,** `<store>/<name>/.ledger`: one row per installed version (version, commit, ref, source URL, tree hash, install time) and the `--track` flag. It is not sideshow's shared `repo-bindings.yaml`, which one per-name lock could not protect, since two names would write it at once. Only a holder of that name's lock writes it, by writing `.ledger.tmp` and renaming it over `.ledger`, so a reader sees the old file or the new one. The dot name keeps it out of marvel's listing, and the version listings already skip files. Every verb that writes it (install, update, activate, remove) takes the lock first; `doctor` and `list` only read it and take no lock.

1. **Sweep:** remove any `<store>/<name>/.*.partial` left by an earlier run, restoring the owner's write bits first, since a run killed after step 4 leaves a read-only partial that cannot otherwise be removed. The lock makes this safe: no other writer can own one.
2. Resolve the ref to a commit and verify it (git source), or verify the tarball.
3. Write the tree to `<store>/<name>/.<version>.partial`. The leading dot keeps it out of marvel's listing, which skips dot entries. The version string is `<commit date>-<short sha>` for a git source, or the release version.
4. Compute the tree hash (v1, as marvel's design defines it, so the two recorders agree) and record it with the commit in sideshow's ledger. Clear every write bit on every file and directory. Mode bits only detect a change, since an owner can set them back; `sideshow doctor` re-hashes against the ledger.
5. Move `.<version>.partial` to `<version>` with a rename that refuses an existing target: `renameat2(RENAME_NOREPLACE)` on Linux, `renamex_np(RENAME_EXCL)` on macOS. Plain `rename(2)` would replace an empty directory. A version that exists is never overwritten, and `--force` does not apply to this kind.
6. With `--activate`, point `current` at the new version by renaming a new symlink over the old one.

A kill at any step leaves the previous `current` live. A partial left by a kill is invisible to marvel (dot name) and removed by the next run's sweep.

## Update

`sideshow update <name>` re-resolves the ref recorded at install (for example `main`). It installs the result only when all of these hold:

- it verifies, as in Sources;
- it is a **descendant** of the installed commit (`git merge-base --is-ancestor <installed> <new>`), so an unattended update never moves sideways or backwards, whatever the ref now names. Exit 0 passes. Exit 1 (not an ancestor) and exit 128 (a commit missing from the mirror, or any git error) both refuse;
- it differs from the installed commit.

**The installed commit** is the commit of the newest installed version, by install order in the ledger, not the commit `current` points at.

Otherwise it writes nothing and says which rule stopped it. With `--track` recorded at install, it also activates. Rollback stays explicit: `sideshow activate <name> <previous>`, or a revert in the master, whose merge is a descendant and installs normally. **Activating any version older than the newest clears `--track`** and says so, so the next scheduled update cannot undo the rollback. With `--track` cleared, update still installs a new descendant but leaves `current` where the operator put it. `sideshow activate <name> <newest> --track` restores tracking.

Because update is idempotent and changes nothing on a no-op, a user-owned scheduled job can run it. sideshow reads no XDG variable: its store is `SIDESHOW_HOME`, else `$HOME/.local/share/sideshow` (`internal/pack/pack.go`, `SideshowDir`). Under launchd or cron the risk is a different `HOME` or a missing `SIDESHOW_HOME`, so a job should set `SIDESHOW_HOME` explicitly, and `sideshow doctor` prints the path it resolved. marvel reads its roots from its own config, not the environment, so it is unaffected.

## Verbs

| verb | does |
|---|---|
| `sideshow install <name> ...` | the install steps |
| `sideshow activate <name> <version>` | flips `current`, under the same per-name lock as install and update, so it cannot interleave with a scheduled update |
| `sideshow update <name>` | as above |
| `sideshow list <name>` | installed versions, `*` on current |
| `sideshow remove <name> <version>` | under the same per-name lock; refuses the current version. A marvel team pinned to an exact version may still use another; the command prints that `marvel get companies` shows which |
| `sideshow doctor` | re-hashes installed versions against the ledger; lists stale partials and held locks |

marvel owns reading, pinning, refusing a changed tree and recording a cast. The contract between the two is a directory layout (`<name>/<version>/`, `current`, dot entries skipped) and the tree hash definition. Neither calls the other.

## Gated: a marvel-aware sideshow

These wait on the operator lifting the workspace-VFS gate, and are not part of this design's build:

- installing directly into a marvel-owned path;
- validating `company.toml` against marvel's schema at install;
- `remove` asking marvel which versions are in use, and refusing them.

## Build note

Today's version listing does not skip dot entries. `InstalledVersions` (`internal/pack/pack.go`) and doctor's layer 1 version walk (`internal/doctor/layer1.go`) take every directory except `current`, so they would list a `.<version>.partial` directory as a version. (`.lock` is a file, and both already skip files.) Both need the dot-skip rule in the same PR that adds staging.

## Tests

- Given a git source whose ref resolves to an unsigned commit, or to one signed by a key of undefined trust, when `install` runs without `--allow-unsigned`, then it refuses and the store is unchanged.
- Given a URL with userinfo, then `install` refuses before any fetch.
- Given an install killed after step 3, and again after step 4 (a read-only partial), then the only entry it left is dot-prefixed (so a reader that skips dot entries, as marvel does, sees nothing), `sideshow list` does not show it, and the next `install` sweeps it and completes.
- Given versions 1 and 2 installed with `--track` and `current` at 2, when the operator activates 1 and a scheduled `update` then runs with no new commit, then `current` stays at 1 and the output says tracking is off. Given a new descendant 3 then arrives, `update` installs 3 and `current` still stays at 1.
- Given an installed commit C, a source repository whose history is then rewritten so that no ref reaches C, and the mirror deleted and re-created from it (so C is absent from the mirror, not merely unreferenced), when `update` runs, then `--is-ancestor` exits 128, `update` refuses, and nothing is written. Re-creating the mirror from an unchanged source would fetch C again and test nothing.
- Given installs of two different names started together, then each name's `.ledger` holds its own row and neither run lost the other's.
- Given a `remove` holding a name's lock, when `update` of that name runs, then `update` refuses on the lock; `doctor` run at the same time reads the old or the new `.ledger`, never a partial one.
- Given an `update` holding the lock, when `activate` runs, then `activate` refuses on the lock and `current` is unchanged.
- Given a source directory holding an `origin.json`, or inside sideshow's own store, then `install` refuses it.
- Given two installs of one name started together, then the second refuses on the lock and the first completes.
- Given an empty directory already at `<version>`, then step 5 refuses and the directory is unchanged.
- Given an installed commit C and a ref that now names a commit that is not a descendant of C, when `update` runs, then nothing is written and the output names the descendant rule.
- Given `update` with no new commit, then nothing is written (store metadata hash equal before and after).
- Given an installed version with one directory made writable, then `doctor` reports it; given one file changed, then `doctor` reports the hash mismatch.
- All of the above in a scratch `SIDESHOW_HOME`.

## Open for the ruling

1. **The workspace-VFS gate:** whether to lift it for the items under "Gated". I recommend keeping it, since this design works without them.
2. **`--track` default:** opt-in, so a first install never moves a running fleet's `current` by surprise. I recommend opt-in.
3. **A dedicated store owner (E2)** per host, the owner's choice. Without it, people and tools marvel does not launch are held back by detection only.

This recommendation is valid until 2026-11-08; the architect re-checks it then.
