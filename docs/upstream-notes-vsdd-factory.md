# Upstream notes: vsdd-factory

Running record of upstream state that constrains this channel, keyed by
release, newest first. Each entry says what we observed, where it was
verified, and — the part that usually matters most — what does *not*
change here, because most defects on the plugin channel are ones this
channel already routes around.

Scope: `drbothen/vsdd-factory` and the `drbothen/claude-mp` marketplace
that serves it. Our deliberate divergences from upstream are the other
document, [`divergence-register.md`](divergence-register.md); that one
prices *our* choices, this one records *their* state.

## 1.0.0-rc.24 — tag `v1.0.0-rc.24`, commit `89f6f87` (2026-08-25)

Observed upgrading a plugin-channel install rc.22 → rc.24 on
darwin-arm64 with three activated repos. Three issues filed upstream.

### The rendered hooks.json does not survive a plugin upgrade

Upstream: [`drbothen/vsdd-factory#788`](https://github.com/drbothen/vsdd-factory/issues/788)

Upstream's `activate` renders `hooks/hooks.json` into
`${CLAUDE_PLUGIN_ROOT}` — the version-scoped cache dir. A `claude
plugin update` installs into a *new* version dir, and the rendered file
is gitignored and absent from the release: the tag ships five
`hooks.json.<platform>` variants plus `hooks.json.template` and no
canonical file. Every activated repo loses all ten hook bindings on
upgrade while its `settings.local.json` still records it as activated.
Nothing warns; `activated_plugin_version` is written by activate and
read nowhere in the plugin tree.

**What changes on this channel: nothing, and that is the point.** We
never render a per-machine `hooks.json`
([`unshaping-spec.md`](unshaping-spec.md), rows for `skills/activate/`
and `hooks/`) — hook commands are synthesized into the repo settings
chain with absolute store paths, and enable is per-repo against a
pinned store version with no machine-wide upgrade lever
([`repo-bindings-enablement.md`](repo-bindings-enablement.md) §4). An
upgrade cannot silently disarm a bound repo, because there is no shared
rendered artifact for it to leave behind. #788 is the first in-the-wild
instance of the failure that design decision was avoiding.

What *does* change is the conversion surface — see the correction
landed in [`claude-plugin-conversion-reference.md`](claude-plugin-conversion-reference.md)
alongside this note.

### `policy15-attestation-gate.wasm` ships unreferenced

Upstream: [`drbothen/vsdd-factory#789`](https://github.com/drbothen/vsdd-factory/issues/789)

At the tag, 76 `[[hooks]]` entries reference 37 distinct modules while
38 ship. `policy15-attestation-gate.wasm` (330K) is referenced by
neither registry, and `policy15`/`attestation-gate` appear zero times
in either. Upstream's own POLICY 20 orphan gate
(`bundle_orphan_check.rs`, added rc.23 S-19.04) did not catch it.

Consequence here is one dead 330K module inside a frozen store version
— worth a known-defects row (`aae-orc-ztg5`) so that a later integrity
pass does not read it as our corruption. No doctor change is needed:
`store-content-census` verifies the tree against the census the pack
ships, and the pack ships the orphan, so census and disk agree.

Worth borrowing if we ever check registry↔module referential integrity
ourselves: the mapping is not 1:1, so a count-equality check produces
false failures. The invariant that holds is two set comparisons — every
referenced module is present, and every present module is referenced at
least once.

### The marketplace declares a version but sources `ref: main`

Upstream: [`drbothen/claude-mp#20`](https://github.com/drbothen/claude-mp/issues/20)

Filed to give an upstream citation to the fact
[`claude-plugin-conversion-reference.md`](claude-plugin-conversion-reference.md)
already records as "the marketplace version field lies by omission."
Both entries in the manifest (`vsdd-factory`, `secops-factory`) pin
`ref: main`. At filing time `main` and the rc.24 tag are the same
commit, so the label is accurate today and guarantees nothing tomorrow.
Version authority stays what this repo already says it is: the
installed tree's `plugin.json` plus the recorded `gitCommitSha`.

### Packaging facts for the rc.24 store version

| Fact | Value at `v1.0.0-rc.24` |
|---|---|
| WASM modules shipped | 38 (37 referenced, 1 orphan) |
| `[[hooks]]` registry entries | 76 |
| Resolver registry entries | 1 |
| Platform dispatcher binaries | 5 (darwin-arm64, darwin-x64, linux-arm64, linux-x64, windows-x64) |
| `hooks.json` forms | 5 platform variants + 1 template; no canonical file |
| Wired hook events | 10, each dispatching to `hooks/dispatcher/bin/<platform>/factory-dispatcher` |
| Executable files in tree | 117 (80 `.sh`, 9 of them under `bin/`) |
| Installed tree size | 101M |

Exec bits are load-bearing on the five dispatcher binaries and the
`.sh` set — upstream's own applier exits 3 specifically on "binary not
executable." Those 117 files are the population `store-exec-census`
needs a census for (`aae-orc-wk92`).

## Verifying these against a tag

Every count above comes from the contents API at the pinned tag, so any
of it can be re-derived without a clone:

```sh
R=drbothen/vsdd-factory; T=v1.0.0-rc.24
gh api "repos/$R/contents/plugins/vsdd-factory/hooks?ref=$T" \
  --jq '.[].name | select(startswith("hooks.json"))'
for f in hooks-registry.toml resolvers-registry.toml; do
  gh api "repos/$R/contents/plugins/vsdd-factory/$f?ref=$T" --jq '.content' | base64 -d > "$f"
done
grep -ohE 'hook-plugins/[a-z0-9._-]+\.wasm' ./*.toml | sed 's|hook-plugins/||' | sort -u > ref.txt
gh api "repos/$R/contents/plugins/vsdd-factory/hook-plugins?ref=$T" \
  --jq '.[].name | select(endswith(".wasm"))' | sort -u > shipped.txt
comm -13 ref.txt shipped.txt   # shipped but unreferenced
```
