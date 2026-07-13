# Path resolution — how pack content finds itself (and you)

Pack content is authored upstream assuming everything lives at
`{project-root}/_bmad/`. Under sideshow's user-install model it lives
in the store (`~/.local/share/sideshow/packs/<pack>/<version>/`,
activated via the `current` symlink). Three mechanisms translate, in
this order of preference. If you're confused about "which instruction
applies," it's this one, top-down:

## 1. Sync-time rewriting (machine, at `commands sync`)

When bindings are synced into `~/.claude/`, text files (.md/.yaml/
.yml/.csv/.txt/.json) get `{project-root}/_bmad/...` references
rewritten to absolute store paths. This is why a synced SKILL.md
"just knows" where its scripts are.

Limits: never touches code (`.py` etc. — rewriting code is how you
break it silently), and only covers what sync copies — not the
store-side files those skills go on to read.

## 2. The runtime shim (filesystem, at `sideshow init --scope project`
   / `sideshow project init <pack>`)

Packs declare the read surfaces their runtime expects at the project
root in pack.yaml:

```yaml
distribute:
  custom_bridge:
    upstream_path: _bmad/custom
    per_repo_dir: _bmad-custom
  runtime_links:
    - { link: scripts,          target: scripts }
    - { link: _config,          target: _config }
    - { link: config.toml,      target: config.toml }
    - { link: config.user.toml, target: config.user.toml }
```

Distribution then materializes a gitignored `_bmad/` shim in the
consumer repo: `custom/` symlinked to the checked-in `_bmad-custom/`
(see `customization-bridge.md`), everything in `runtime_links`
symlinked into the store **through `current`** (version flips keep
working). Upstream's hardcoded `{project-root}/_bmad/...` paths —
including the ones inside Python that mechanism 1 cannot touch —
simply become true. This is what makes bmad's resolvers (party-mode
roster, resolve_config's four-file chain) work.

Safety: a link whose target doesn't exist in the active version is
refused (dangling links half-work, worse than none); existing real
files/dirs are never replaced; the store is read-only, so any WRITE
through the shim fails loudly.

`runtime_links` is deliberately enumerated, not pattern-globbed: the
build-side reference scanner (sideshow-packs#2) proposes additions per
pack version; a human ratifies them into the declaration.

## 3. The fallback footer (LLM instructions, last resort)

Synced bindings carry an appended "Sideshow Fallback Resolution"
section for the case neither mechanism covered: you're in a directory
with no shim (no `sideshow init` ran there) and a workflow file
references `{project-root}/_bmad/...`. The footer tells the agent:
try the project path, then substitute the store path — **for reads
only**. Never write, move, rename, or delete through the substitution
(that's how a pack store got corrupted — finding-074 in the orc);
project-local writes belong in `_bmad-custom/` / `_bmad-output/`.

## Rule of thumb

- Repo you work in regularly → run `sideshow project init <pack>`
  once; mechanism 2 makes everything true and you can ignore the rest.
- Random directory → mechanisms 1 + 3 keep skills functional
  read-only.
- Anything asking to WRITE into `_bmad/` → it writes through the shim
  into a frozen store and fails loudly, or the footer tells the agent
  to stop. Either way: surfaced, not silent.
