# Claude-plugin conversion reference (foreign installs)

How a claude-mp marketplace install of a pack registers with the
harness, so sideshow's conversion surface (the coexistence guard,
`coexist-check`, and `adopt`) can read it, suppress it per repo, and
reverse it cleanly.

**This is not an enablement path.** Sideshow delivers by repo
bindings (`docs/unshaping-spec.md`); the native enablement runbook is
`docs/repo-bindings-enablement.md` (aae-orc-d3nq.54). Claude plugin
verbs appear below only to read and reverse FOREIGN state, with
consent. Sideshow never invokes `claude plugin marketplace add` or
`claude plugin install`, and never writes an `enabledPlugins` entry,
for its own channel.

Every claim here was verified by executed trials on Claude Code
2.1.220 (aae-orc finding-091, addenda 2 and 3).

## Anatomy of a foreign install

For vsdd-factory the upstream instruction is:

```
/plugin marketplace add drbothen/claude-mp
/plugin install vsdd-factory@claude-mp
```

which creates, in order:

1. **Marketplace registration**: an `extraKnownMarketplaces` entry in
   the user `settings.json` plus a thin metadata clone under
   `~/.claude/plugins/marketplaces/<mp>/` (140K for claude-mp; it
   carries `.claude-plugin/marketplace.json`, not content).
2. **Install record**: an entry in
   `~/.claude/plugins/installed_plugins.json` (format version 2) with
   `scope`, `installPath`, `version`, `installedAt`, `gitCommitSha`,
   and for project-scope installs a `projectPath`.
3. **Content cache**: git-subdir sources are CACHE-RESIDENT. The
   installPath is `~/.claude/plugins/cache/<mp>/<pack>/<version>/`
   (98M for vsdd-factory) and is the tree sessions load. The rendered
   `hooks/hooks.json` (upstream activate's output) lands inside this
   cache version dir.
4. **Enablement**: **install auto-enables at its scope.** A
   user-scope install is immediately live machine-wide for newly
   started sessions; a `--scope project` install writes
   `enabledPlugins` into that repo's git-tracked
   `.claude/settings.json` and nothing anywhere else.

Facts that shape the census:

- **The marketplace version field lies by omission.** claude-mp pins
  `git-subdir` sources to `ref: main`, so the content is whatever
  `main` held at install time regardless of the declared version.
  Version authority for a foreign install is the installed tree's
  `.claude-plugin/plugin.json` plus the recorded `gitCommitSha`.
- **Two identities exist in the wild**: `vsdd-factory@claude-mp`
  (current) and a legacy pre-rc.7 identity from a
  `drbothen/vsdd-factory` marketplace. Detection matches marketplace
  NAME fields, never filesystem paths, and must match both.
- **`plugin list` is registry-driven.** It does not stat the cache
  (a hidden cache still shows "enabled"), while a missing marketplace
  clone fails the whole marketplace loudly ("cache-miss").
  `plugin details` serves the full component inventory even with the
  cache hidden.
- **A fresh install registers ZERO hooks** (the zero-hooks trap):
  126 skills and 34 agents load, but hooks appear only after
  upstream's activate renders `hooks.json` into the installPath.
- **Foreign skills are always namespace-qualified**
  (`vsdd-factory:<name>`), so sideshow's prefixed bound names cannot
  collide with them. The double-dispatch hazard is the two HOOK
  chains, not the namespaces.
- **Orphaned enables are silent.** An `enabledPlugins` entry with no
  install behind it draws no CLI mention and no session warning;
  only a census finds it.

## Per-repo suppression (the keep-your-plugin branch)

Scoped enablement precedence is the native lever, verified in both
directions: project-scope `true` overrides user-scope `false`, and
project-scope `false` overrides user-scope `true` (`Status: disabled`
inside that repo, enabled everywhere else). So a foreign install can
be suppressed in exactly the repos where sideshow bindings are active
with one settings line per repo:

```json
{ "enabledPlugins": { "vsdd-factory@claude-mp": false } }
```

No uninstall, no machine-wide change, fully reversible. This is the
coexistence guard's preferred offer. The one configuration that is
REFUSED outright is a foreign enable and sideshow bindings effective
in the SAME repo: both hook chains fire per event against one
`.factory/` state directory (verified live), and no dedupe layer can
see across the two registration files.

## Reversing a foreign install

Order matters: upstream's deactivate needs the plugin's own skills,
so it must run while the plugin is still enabled.

1. In the repo, run `/vsdd-factory:deactivate` (in a session).
2. `claude plugin disable vsdd-factory@claude-mp --scope project`
   (or `--scope local`, matching how it was enabled).
3. Optional removal: `claude plugin uninstall vsdd-factory@claude-mp
   --scope <scope>`, then `claude plugin marketplace remove claude-mp`,
   then sweep the residue below.

## Residue checklist

Verified residue after uninstall and marketplace removal:

- **Cache version dirs survive everything**:
  `~/.claude/plugins/cache/<mp>/<pack>/<version>/` (98M each) until
  removed by hand.
- **A `settings.local.json` enable survives** both project-scope and
  user-scope uninstall and will silently re-enable the pack in that
  repo on reinstall.
- **`marketplace remove` leaves an empty `extraKnownMarketplaces`
  key** in the user settings.json.
- **Uninstall `--scope project` DOES clean its own project-scope
  enable** (leaves an empty `enabledPlugins` map in the repo
  settings).
- **Orphaned enables** anywhere (see above) are invisible except to a
  census.

## Troubleshooting

- **"failed to load ... cache-miss" naming the marketplace**: the
  marketplace clone under `~/.claude/plugins/marketplaces/` is
  missing or broken; re-add the marketplace or remove the identity.
- **Hooks (0) in `plugin details`**: the install was never activated
  (no rendered hooks.json). For a foreign install being converted,
  this means the factory's guard surface was never live in the first
  place.
- **Version reported does not match tree behavior**: the marketplace
  serves `ref: main`; fingerprint the installed tree (plugin.json +
  gitCommitSha) rather than trusting any version label.

## References

- aae-orc `_kos/findings/finding-091-claude-plugin-identity-directory-marketplace.md`
  (trial log: T0-T9 directory-marketplace mechanics, addendum 2
  repo-bindings trials, addendum 3 foreign-install trials)
- aae-orc finding-093 (coexistence contract), finding-094 (unshaping
  contract)
- `docs/unshaping-spec.md` (the delivery mechanism this document is
  NOT)
- bd: `aae-orc-paqn` (guard), `aae-orc-d3nq.22` (adopt),
  `aae-orc-d3nq.41` (coexist-check), `aae-orc-d3nq.54` (native
  enablement runbook)
