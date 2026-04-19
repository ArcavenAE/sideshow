package bindings

import (
	"strings"
)

// rewritePaths replaces pack content references with the absolute pack
// installation path while preserving per-repo paths that must stay
// relative to the invoking project.
//
// Rewrites (pack content — read-only, user-install):
//
//	{project-root}/_<pack>/module/path → /absolute/packs/<pack>/<v>/module/path
//
// Preserves (per-repo — stays relative to cwd):
//
//	{project-root}/_<pack>-custom/ → unchanged (per-repo customization)
//	{project-root}/_<pack>-output/ → unchanged (per-repo output)
//	{project-root}/              → unchanged (any other project-relative path)
//
// The implementation uses nul-byte sentinels to protect the per-repo
// prefixes from being caught by the pack-content rewrite.
//
// Currently the pack prefix is hardcoded as "_bmad" because bmad is the
// only pack sideshow manages. When a second pack with a different prefix
// lands, this will need parameterizing.
func rewritePaths(content, packPath string) string {
	const customSentinel = "\x00BMAD_CUSTOM\x00"
	const outputSentinel = "\x00BMAD_OUTPUT\x00"

	content = strings.ReplaceAll(content, "{project-root}/_bmad-custom/", customSentinel)
	content = strings.ReplaceAll(content, "{project-root}/_bmad-output/", outputSentinel)

	content = strings.ReplaceAll(content, "{project-root}/_bmad/", packPath+"/")

	content = strings.ReplaceAll(content, customSentinel, "{project-root}/_bmad-custom/")
	content = strings.ReplaceAll(content, outputSentinel, "{project-root}/_bmad-output/")

	return content
}

// appendFallbackFooter adds LLM-executable guidance so that pack-internal
// workflow/step/skill files retaining literal {project-root}/_bmad/...
// references can resolve at orchestrator roots that have no _bmad/
// directory.
//
// The top-level entry file (slash command or skill SKILL.md) gets its
// {project-root}/_bmad/ references rewritten by rewritePaths. Files the
// entry file loads are not rewritten — they live inside the installed
// pack and stay literal. The footer tells the reading LLM to resolve
// such references via a two-step fallback chain:
//
//  1. Try cwd-relative first (works for per-project installs).
//  2. Substitute with the pack user-install path (works at orc roots).
//
// Wrapped in sentinel markers for idempotency — re-syncing doesn't
// duplicate content.
func appendFallbackFooter(content, packPath string) string {
	const beginMarker = "<!-- sideshow:fallback-resolution:begin -->"

	if strings.Contains(content, beginMarker) {
		return content
	}

	const tmpl = `

---

<!-- sideshow:fallback-resolution:begin -->
## Sideshow Fallback Resolution for pack-internal references

Workflow files inside this pack may reference paths of the form X{project-root}/_bmad/...X. Those references are not rewritten at sync time. When you follow this command into such a file and encounter such a reference, resolve it as:

1. First try X{project-root}/_bmad/...X relative to the current working directory (per-project install, if any).
2. If that path does not exist, substitute X{project-root}/_bmad/X with the pack user-install location:

   X__PACK_PATH__/X

3. Per-repo paths (X{project-root}/_bmad-custom/X, X{project-root}/_bmad-output/X) stay relative to the invoking project — these are per-repo, not pack content.
<!-- sideshow:fallback-resolution:end -->
`
	footer := strings.ReplaceAll(tmpl, "X", "`")
	footer = strings.ReplaceAll(footer, "__PACK_PATH__", packPath)

	return content + footer
}
