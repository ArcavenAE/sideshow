# sideshow Charter

Probe: Can a content pack manager eliminate BMAD duplication across repos
while preserving per-repo customization and making packs available globally?

## Bedrock

- Go CLI, single binary
- Content packs installed to ~/.local/share/sideshow/packs/{name}/{version}/
- Commands synced to ~/.claude/commands/{pack}/
- Per-repo customization via _bmad-custom/ (or _{pack}-custom/)
- Per-repo output via _bmad-output/ (or _{pack}-output/)

## Frontier

- Does global command installation work with Claude Code?
- Does path resolution (~/.local/share/...) work in command templates?
- Can we version packs and roll back?
- How does this interact with marvel workspaces?

## Graveyard

(none yet)
