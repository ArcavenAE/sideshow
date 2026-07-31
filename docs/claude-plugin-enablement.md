# Claude-plugin enablement runbook (per-repo packs)

Manual path from a signed sideshow-packs release to a working
plugin-class pack in one repo, and back out again. Written for
vsdd-factory, the first pack with `per_repo_required: true`; the shape
generalizes to any pack whose register declares
`activation.mechanism: claude-plugin`.

Every step here uses documented `claude` CLI verbs. Sideshow never
hand-edits harness state, and neither should you. The behavior this
runbook relies on was verified by executed trials on Claude Code
2.1.220 (aae-orc finding-091); that is the validated floor.

Sideshow verbs will absorb steps 3 through 5 (`aae-orc-d3nq.7`); until
then this document is the contract.

## Why per-repo

vsdd-factory is per-repo-operation software: it runs in a repo, not
from an orchestrator, not across repos. Its hooks are executable
content; enabled at user scope they would fire in every repo on the
machine. So the pack installs to the user-scope store (multi-version,
bmad model) but is never active by default; activation is an explicit
per-repo act.

Choosing sideshow delivery also supersedes the claude-mp marketplace
binding by ratified contract: the marketplace offers neither
multi-version install nor supply-chain verification, and the user
selecting sideshow changes the consumer contract by that choice.
Do not mix the two mechanisms in one machine for the same pack;
migration is tracked in `aae-orc-d3nq.22`.

## Prerequisites

- Claude Code 2.1.220 or later (`claude --version`)
- sideshow with mode-preserving install (PR #60 or later)
- `cosign` for artifact verification

## 1. Obtain and verify the artifact

Fetch the tarball, its signature bundle, and the manifests from the
release (URL contract: `sideshow-packs/docs/release-url-format.md`):

```sh
V=1.0.0-rc.23
BASE=https://github.com/ArcavenAE/sideshow-packs/releases/download/vsdd-factory-v$V
curl -fsSLO "$BASE/vsdd-factory-$V-arcaven.tar.gz"
curl -fsSLO "$BASE/vsdd-factory-$V-arcaven.tar.gz.bundle"
curl -fsSLO "$BASE/exec-manifest.txt"

cosign verify-blob \
  --bundle "vsdd-factory-$V-arcaven.tar.gz.bundle" \
  --certificate-identity-regexp "https://github.com/ArcavenAE/sideshow-packs/.github/workflows/.*" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  "vsdd-factory-$V-arcaven.tar.gz"
```

`Verified OK` or stop.

## 2. Install to the store

Extract, then place `exec-manifest.txt` at the extracted pack root:
it is a sibling release asset, and sideshow's install verifies the
installed tree against it only when it sits inside the source root.

```sh
tar -xzf "vsdd-factory-$V-arcaven.tar.gz"
cp exec-manifest.txt "vsdd-factory-$V/"
sideshow install vsdd-factory --from "vsdd-factory-$V"
```

Install fails loudly if any of the pack's 112 executables lost its
exec bit. Do not proceed past a failed install.

## 3. Register the plugin identity (once per machine)

The identity surface is a sideshow-managed directory marketplace.
Create it once, with one entry per plugin-class pack whose `source`
is a relative symlink into the store:

```sh
MP=~/.local/share/sideshow/claude-mp
mkdir -p "$MP/.claude-plugin"
ln -sfn ~/.local/share/sideshow/packs/vsdd-factory/current "$MP/vsdd-factory"
cat > "$MP/.claude-plugin/marketplace.json" <<'JSON'
{
  "name": "sideshow",
  "owner": { "name": "sideshow" },
  "plugins": [
    { "name": "vsdd-factory", "source": "./vsdd-factory",
      "description": "VSDD dark factory (sideshow-verified store)" }
  ]
}
JSON
claude plugin marketplace add "$MP"
```

The source must be a relative path; absolute paths are rejected by
the harness. The harness loads plugin content live through this
symlink, so the store version it points at must stay on disk for as
long as the identity exists.

Never use the `~/.claude/skills/` drop-in path for a
`per_repo_required` pack: skills-dir plugins are default-ON the
moment the directory exists (verified, finding-091 T4).

## 4. Install the identity, disabled by default

```sh
claude plugin install vsdd-factory@sideshow --scope user
claude plugin disable vsdd-factory@sideshow --scope user
```

The install verb default-enables at user scope, which violates the
per-repo mandate until the disable lands; run the two commands
together. The user-scope `false` is the machine-wide default that
per-repo enables override.

## 5. Enable in one repo

```sh
cd /path/to/repo
claude plugin enable vsdd-factory@sideshow --scope project   # shared, .claude/settings.json
# or
claude plugin enable vsdd-factory@sideshow --scope local     # personal, .claude/settings.local.json
```

Project-scope `true` overrides the user-scope `false`; `claude
plugin list` will say so explicitly. Restart the session to load.

## 6. One-time platform bind (per machine)

A freshly installed plugin loads 126 skills and 34 agents but
registers ZERO hooks: upstream gitignores the canonical
`hooks/hooks.json` and renders it per platform at activation. Verify
the gap and close it:

```sh
PLUGIN_ROOT=~/.local/share/sideshow/packs/vsdd-factory/current
PLATFORM=darwin-arm64   # one of: darwin-arm64 darwin-x64 linux-x64 linux-arm64 windows-x64
"$PLUGIN_ROOT/skills/activate/apply-platform.sh" --check "$PLATFORM"   # dry run
"$PLUGIN_ROOT/skills/activate/apply-platform.sh" "$PLATFORM"
```

Post-bind checks, both required:

```sh
test -f "$PLUGIN_ROOT/hooks/hooks.json" && echo hooks.json present
"$PLUGIN_ROOT/hooks/dispatcher/bin/$PLATFORM/factory-dispatcher" --help >/dev/null && echo dispatcher executes
claude plugin details vsdd-factory@sideshow | grep "Hooks"   # expect a non-zero count
```

The bind writes inside the store version dir and serves every repo on
the machine (upstream's ship-all-five, bind-at-activation model).
Placement of this per-machine state relative to the planned read-only
store freeze is an open decision (`aae-orc-d3nq.6`); until it lands,
the store copy stays writable at `hooks/hooks.json`.

In-session, upstream's `/vsdd-factory:activate` skill performs the
same bind plus factory workspace setup; this section is the headless
equivalent of the bind portion only.

## 7. Exit ordering

Order matters: deactivate needs the plugin's own skills, so it must
run while the plugin is still enabled.

1. In the repo, run `/vsdd-factory:deactivate` (in a session).
2. Then `claude plugin disable vsdd-factory@sideshow --scope project`
   (or `--scope local`, matching how it was enabled).
3. Optional removal: `claude plugin uninstall vsdd-factory@sideshow
   --scope user`, then check the residue list below.

## Residue and troubleshooting

- **Uninstall leaves residue** (finding-091 T9): harness cache copies
  under `~/.claude/plugins/cache/sideshow/vsdd-factory/<version>/`
  survive uninstall; a `settings.local.json` enable survives both
  project and user uninstall and will silently re-enable the pack on
  reinstall; the marketplace entry needs its own
  `claude plugin marketplace remove sideshow`.
- **"failed to load" naming a missing path**: the marketplace symlink
  dangles, usually because the store version it pointed at was
  removed. Repoint the symlink and run
  `claude plugin marketplace update sideshow`.
- **Hooks (0) in `plugin details`**: the platform bind never ran in
  this store version. Redo section 6.
- **Version updates**: sideshow flips the store `current` symlink,
  then `claude plugin marketplace update sideshow` and
  `claude plugin update vsdd-factory@sideshow` per scope. Updates
  resolve only through this marketplace; nothing can silently
  re-fetch claude-mp content under the sideshow identity.

## References

- aae-orc `_kos/findings/finding-091-claude-plugin-identity-directory-marketplace.md`
  (trial log behind every claim here)
- `sideshow-packs/registry/vsdd-factory-pack-support.yaml` (activation
  policy, wrinkles)
- `sideshow-packs/docs/release-url-format.md` (artifact URLs and
  verification recipe)
- bd: `aae-orc-d3nq.3` (this doc), `.5`/`.6` (pending decisions),
  `.7` (sideshow verbs that absorb sections 3 through 5)
