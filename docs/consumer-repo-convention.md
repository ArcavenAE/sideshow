# Consumer-Repo Convention

**Status:** committed, session-029 (aae-orc-794h)

How to use a sideshow-managed content pack (bmad, vsdd, etc.) in a
project repo, without conflicting with sideshow's user-scope
installation or with other people collaborating on the same project.

## Audience

Someone who has installed sideshow, pulled in a pack via `sideshow
install <pack>`, and now wants to *use* that pack in a specific
repo.

## The split: user-scope vs project-scope

```
~/                                       USER-SCOPE (sideshow-managed)
├── .local/share/sideshow/packs/         ── pack content, versioned
│   └── bmad/
│       ├── 6.3.0/                       ── immutable pack install
│       └── current -> 6.3.0
└── .claude/skills/                      ── tool bindings, sideshow-synced
    └── bmad-*/                          ── bmad skills (all 97 for 6.3.0)

project-repo/                            PROJECT-SCOPE (per-repo)
├── _bmad-custom/                        ── CHECKED IN: overrides
│   ├── agents/                          ──   project-specific agents
│   └── memories/                        ──   project-specific context
├── _bmad-output/                        ── CHECKED IN: the project's deliverables
│   ├── planning-artifacts/              ──   PRDs, stories, decisions
│   └── implementation-artifacts/        ──   implementation docs
├── _bmad/                               ── GITIGNORED: pack content
│                                            (sideshow writes to user-scope; this
│                                             only exists if upstream installer
│                                             ran locally; do not commit)
└── .claude/                             ── GITIGNORED: tool binding duplicates
    └── skills/bmad-*/                        (sideshow syncs user-scope; these
                                              are redundant if present)
```

## What goes where

| Path in consumer repo | Action | Why |
|---|---|---|
| `_<pack>-custom/`     | **Check in**  | Project-specific overrides. These ARE the project's choices. |
| `_<pack>-output/`     | **Check in**  | Agent-produced deliverables (PRDs, stories, architecture, implementation docs). These ARE the project's output. |
| `_<pack>/`            | **Gitignore** | Pack content lives at `~/.local/share/sideshow/packs/<pack>/<v>/`. If present in-project, it's stale from an upstream installer run — do not commit. |
| `.claude/commands/<pack>-*.md` | **Gitignore** | sideshow syncs to `~/.claude/commands/` at user-scope. Per-project duplicates conflict. |
| `.claude/skills/<pack>-*/`     | **Gitignore** | sideshow syncs to `~/.claude/skills/` at user-scope. Per-project duplicates conflict. |
| `sideshow.lock`       | **Check in**  | Pins the pack version the repo was last known to work against. See aae-orc-333y. |

**Previous design note:** sideshow's original charter said
`_<pack>-output/` was *gitignored*. That was wrong — it confused
scaffolding with deliverables. The dir holds the user's generated
artifacts, and those belong in version control. Session-029 corrected
this.

## Installer flag guidance

When you run an upstream installer (e.g. `npx bmad-method install`)
inside a sideshow-managed repo — which you may need to do for some
workflows — **always pass `--tools none`**. Otherwise the installer
writes `.claude/skills/<pack>-*/` into the project, which duplicates
what sideshow already syncs to user-scope.

Running the upstream installer AT ALL in a sideshow-managed repo is
discouraged. sideshow install is the canonical path. The ArcavenAE
frozen-composition pipeline (aae-orc-ibil) eliminates the need for
local npx entirely — signed tarballs are fetched and verified, never
executed.

## Two-system conflict model

Scenario: person A on machine 1, person B on machine 2, both working
on the same repo. Both use sideshow to consume bmad.

Each person's sideshow writes to **their own** user-scope:
- A's `~/.local/share/sideshow/packs/bmad/` is entirely separate from B's.
- A's `~/.claude/skills/` is entirely separate from B's.

The only state that ends up in the repo is per-project:
- `_<pack>-custom/` — regular mergeable files; conflicts resolved the
  usual way.
- `_<pack>-output/` — same.

Conflicts across systems happen ONLY where they'd happen without
sideshow (editing the same project file). sideshow itself cannot cause
cross-machine corruption because it doesn't commit anything.

**Version drift** is a separate issue. If A has bmad@6.3.0 and B has
bmad@6.2.2, `_<pack>-custom/` content may reference pack features
only one version supports. `sideshow.lock` (aae-orc-333y) pins
the exact version at repo level so collaborators converge.

## Template `.gitignore` fragment

Drop this at the bottom of your project's `.gitignore`, or let
`sideshow project init <pack>` (aae-orc-f6ei) add it for you with
marker sentinels:

```gitignore
# managed by sideshow — do not edit within markers
# sideshow:gitignore:bmad:begin
/_bmad/
/.claude/commands/bmad-*.md
/.claude/skills/bmad-*/
# sideshow:gitignore:bmad:end
```

The markers let sideshow update entries idempotently when pack
versions change.

Per-pack packs ship their own gitignore entries via `pack.yaml`'s
`distribute.gitignore` list (f6ei). The fragment above is derived
from bmad's pack.yaml once it's declared.

## FAQ

**Q: If I committed `_bmad/` by accident, should I remove it?**
Yes. `git rm -rf _bmad/` + add the gitignore entry. The content lives
at `~/.local/share/sideshow/packs/bmad/current/` — nothing lost.

**Q: What about `docs/` that bmad's installer writes?**
That's a placeholder for your project knowledge. Check in, gitignore,
or remove per your preference. Not a pack artifact.

**Q: What about `_bmad-output/planning-artifacts/` subdirectories the
installer creates empty?**
Check them in. They'll fill up as your workflows run. The directory
structure conveys intent even if initial contents are empty.

**Q: Can I use sideshow-managed bmad AND keep a local `_bmad/`
install for offline work?**
You can, but it defeats the point. The user-scope install is designed
to be the single source of truth. If you need offline: cache the
sideshow user-scope pack store (`~/.local/share/sideshow/packs/`) on
the offline machine via rsync or similar.

## Related

- `aae-orc-794h` — this doc (shipped).
- `aae-orc-f6ei` — sideshow distributes the gitignore fragment (next).
- `aae-orc-333y` — `sideshow.lock` for cross-user version pinning.
- `aae-orc-d9a3` — stale-binding cleanup on pack version transition.
- sideshow/charter.md — canonical design shapes.
