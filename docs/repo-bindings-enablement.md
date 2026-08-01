# Enabling vsdd-factory via repo bindings

This is the native enablement runbook for plugin-shaped packs on the
sideshow channel (aae-orc-d3nq.54). It is the document the pack.yaml
`runbook:` pointer resolves to. A reader who has never seen a claude
plugin can enable, verify, toggle versions, and remove with this page
alone; converting an existing claude-mp install is a different
document (docs/claude-plugin-conversion-reference.md).

## Why this channel

What a consumer gets here that the plugin form cannot give:

- **Multi-version install.** The store holds every installed version
  side by side; each repo pins the exact version dir it was enabled
  with. Two repos on two versions is the representable normal case.
- **Supply-chain verification.** The pack tarball is cosign-signed
  with a Rekor transparency-log entry and a source-provenance
  attestation; verification happens at install, before any repo sees
  a byte.
- **Per-repo pinning with no machine-wide activation.** Enabling in
  one repo changes nothing anywhere else. There is no user-scope
  enable on this channel at all; a factory pack never activates in
  repos you did not name.
- **Exact removal.** Everything enable writes is recorded; disable
  replays the record in reverse and is byte-exact, proven by test.

Deliberate differences from the plugin form are priced in
[docs/divergence-register.md](divergence-register.md) — read it once
before your first enable.

## 1. Obtain and verify

Signed releases are published by
[sideshow-packs](https://github.com/ArcavenAE/sideshow-packs) — built
in CI from the pinned upstream source, cosign-signed,
Sigstore-attested. Direct fetch-and-verify by `sideshow install` is
not yet shipped (aae-orc-wk92); until then, download and verify by
hand, then install from the extracted tree:

```sh
mkdir -p /tmp/vsdd-install && cd /tmp/vsdd-install
gh release download vsdd-factory-v1.0.0-rc.23 -R ArcavenAE/sideshow-packs \
  -p 'vsdd-factory-1.0.0-rc.23-arcaven.tar.gz*' -p 'install.meta.json'

# Integrity: tarball sha256 must match the provenance manifest
shasum -a 256 -c <(python3 -c \
  "import json; m=json.load(open('install.meta.json')); \
   print(m['artifact']['tarball_sha256'], ' vsdd-factory-1.0.0-rc.23-arcaven.tar.gz')")

# Authenticity: cosign keyless verification against the CI identity
cosign verify-blob \
  --bundle vsdd-factory-1.0.0-rc.23-arcaven.tar.gz.bundle \
  --certificate-identity-regexp 'github.com/ArcavenAE/sideshow-packs' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  vsdd-factory-1.0.0-rc.23-arcaven.tar.gz     # expect: Verified OK

tar -xzf vsdd-factory-1.0.0-rc.23-arcaven.tar.gz
sideshow install vsdd-factory --from /tmp/vsdd-install/vsdd-factory-1.0.0-rc.23
```

Install verifies the file manifest and the executable census
(`exec-manifest.txt`); a store copy that fails verification is not
usable. Nothing is active anywhere after install — the store copy is
producer-validated content only.

## 2. Enable in one repo

```sh
cd /path/to/your/repo
sideshow enable vsdd-factory                  # current registered version
sideshow enable vsdd-factory@1.0.0-rc.23      # or pin explicitly
```

Flags: `--repo <path>` (instead of cwd), `--scope local|project`
(default `local`), `--override-stale-lock` (see below).

What the scopes mean:

- **local** (default): bindings are symlinks into the machine store
  and registration goes to `.claude/settings.local.json`. Nothing
  enters your repo's git history. This is the testing-the-waters
  posture.
- **project**: bindings are full self-contained copies (absolute
  symlinks do not cross machines) and registration goes to the
  committed `.claude/settings.json`. Choose this only when the whole
  team is meant to get the pack on checkout, and expect the diff.

What materializes where (the store-vs-repo split):

| Location | Content |
|---|---|
| `.claude/skills/vsdd-*` | The pack's skills, prefixed |
| `.claude/agents/vsdd-*` | The pack's agents, prefixed, nesting preserved |
| `.claude/settings.local.json` (or `settings.json`) | `env.CLAUDE_PLUGIN_ROOT` shim + the hook chain (all 12 events), every entry marked `_managed_by: sideshow:vsdd-factory` |
| `plugins/vsdd-factory` | Compat symlink to the pinned store version (resolves the pack's repo-relative engine paths) |
| the store (never your repo) | The engine: dispatcher binaries, wasm hook plugins, `bin/`, templates, workflows, rules |

Enable runs a preflight first and refuses rather than proceeding into
a hazard: another vsdd-factory channel effectively enabled in the
same repo (two hook chains against one `.factory` state), an
in-flight factory run, pre-existing content it would overwrite, or a
missing/non-executable dispatcher (the zero-hooks trap). An unexpired
factory lock is never overridable; a stale lock or recent-activity
signal passes only with `--override-stale-lock`.

## 3. Verify

```sh
sideshow coexist-check vsdd-factory
```

Clean output ends with `preflight clean`. For a bound repo it also
verifies the env shim resolves to the pinned store path and the
dispatcher is executable. Quick manual checks:

```sh
grep -c 'sideshow:vsdd-factory' .claude/settings.local.json   # hook groups
ls .claude/skills | grep -c '^vsdd-'                          # skill census
readlink plugins/vsdd-factory                                 # pinned store path
```

Enable itself refuses to report success if the post-write hook chain
is short of the declared event set, so a successful enable already
implies the full chain.

## 4. Disable and re-enable

```sh
sideshow disable vsdd-factory
```

Disable replays the enable record exactly: hook chain unmerged, env
shim removed, bindings and the compat symlink removed, ledger row
deleted. Your own settings entries, hooks, and skills are untouched
(removal never guesses; it only removes what enable recorded).
Re-enable is a fresh `sideshow enable`.

## 5. Per-repo version toggle

```sh
sideshow disable vsdd-factory
sideshow enable vsdd-factory@<other-version>
```

Only this repo changes. Other repos keep their pins; the store keeps
every version. There is no machine-wide upgrade lever on this channel
by design.

## 6. Testing-the-waters trial recipe

To trial the pack beside an existing claude-mp install on the same
machine (machine-level coexistence is supported; same-REPO dual
enable is refused):

1. Pick a trial repo the plugin is NOT enabled in. If the repo's
   origin carries a `factory-artifacts` ref shared with a production
   checkout, do not trial there — the preflight refuses this
   (remote-safety).
2. Snapshot the foreign footprint if you want drift proof:
   `sideshow coexist vsdd-factory` before and after; the claude-mp
   footprint is fully disjoint from everything sideshow writes.
3. `sideshow enable vsdd-factory` (local scope), work the trial.
4. Exit paths: `sideshow disable vsdd-factory` restores the repo
   exactly (the preflight records a retreat anchor — the
   `factory-artifacts` tip and `.factory` status — so you can prove
   the trial changed nothing it should not have).

## 7. Activate and deactivate (the persona flip)

Enable never touches your default agent (no-hijack-on-enable, kept
from upstream). Making the pack's orchestrator drive plain sessions
is a separate, explicit act:

```sh
sideshow activate vsdd-factory                # default agent -> vsdd-orchestrator
sideshow activate vsdd-factory --agent vsdd-<name>   # a different bound agent
sideshow deactivate vsdd-factory              # remove the flip only
```

Activate requires an enabled repo and refuses to overwrite an agent
key it does not own; deactivate removes the key only when it carries
the pack's `vsdd-` prefix and refuses a foreign persona. Bindings are
unaffected either way — `sideshow disable` is the verb that removes
those.

## 8. Adopting a repo from the claude-mp channel

If a repo already runs vsdd-factory through the claude plugin
channel, `adopt` converts it in one reversible step:

```sh
cd /path/to/adopting/repo
sideshow adopt vsdd-factory --dry-run    # print the plan, write nothing
sideshow adopt vsdd-factory              # convert
```

What it does, in order: refuses on hazards (nothing dispatching,
double-dispatch, orphaned enables, user-scope enables, version
drift); suppresses the foreign identity in THIS repo only (one
settings override — the plugin install itself is never touched);
enables the sideshow channel at the version the foreign tree is
actually running; then prints an equivalence report (content parity,
counts, dispatcher byte identity) against the foreign tree, plus a
proof that no machine-level harness state changed.

Flags: `--rewrite-agent` consents to flipping a foreign-form default
agent (`vsdd-factory:...`) to the bound orchestrator;
`--allow-version-change` accepts adopting at a version other than the
running one (equivalence is only provable at version equality);
`--repo`, `--scope`, `--override-stale-lock` as on enable.

Retreat: `sideshow disable vsdd-factory` removes the bindings, and
deleting the suppression override from `.claude/settings.local.json`
restores the foreign channel exactly as found.

**Machine-level retirement is not automated.** After adopting the
repos you care about:

```sh
sideshow adopt vsdd-factory --finish
```

prints every remaining foreign trace (installs per scope, orphaned
enables, the cache tree, the marketplace) with the operator command
that retires each one. Sideshow executes none of them — uninstalling
the plugin channel is your act, run only what you consent to.

## Divergence notice

This channel deliberately diverges from the plugin form in named,
priced ways — prefixed artifact names, settings-chain registration,
ledger-based updates, twelve wired hook events, excluded `tests/`.
The register is [docs/divergence-register.md](divergence-register.md);
defects reproducible only on this channel belong to sideshow, not
upstream.
