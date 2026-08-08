package bindings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ArcavenAE/sideshow/internal/distribute"
)

// ErrDanglingPackRefs reports that synced content carries a path inside the
// frozen pack store that does not exist there. The store is installed
// read-only (pack.FreezeTree), so such a path can never come into being:
// a read through it fails, a write through it fails with EACCES.
var ErrDanglingPackRefs = errors.New("dangling pack references")

// defaultShimPrefix is the project-root shim dir assumed for packs that
// do not declare another. Packs with a different prefix (e.g. "_vsdd")
// construct their rules with it explicitly.
const defaultShimPrefix = "_bmad"

// packRefRules classifies {project-root}/<prefix>/ references for one
// installed pack: which name pack content (rewritable to the absolute
// store path) and which name project state (left literal).
//
// The classifier is the frozen store itself. Pack content is exactly what
// exists under packPath; everything else under the shim dir is the
// project's own state, whether the reference reads it or writes it. That
// collapses the read-vs-write question into an existence test with no
// lexical guessing about write verbs, flag names, or shell redirects —
// the heuristics a synced-surface audit measured as unreliable
// (aae-orc-c8v8).
//
// Two reference classes are therefore left literal, for the reading LLM
// to resolve cwd-relative via the fallback footer:
//
//   - a path absent from the store (project state: config.yaml,
//     planning/prd.md, the agent-builder's memory/ sanctum)
//   - a path under a declared per-repo surface, even though it exists in
//     the store (pack.yaml custom_bridge — _bmad/custom is symlinked to
//     the checked-in _bmad-custom, so it must stay project-relative)
type packRefRules struct {
	packPath string
	prefix   string

	// perRepo holds first path segments under the shim dir that the pack
	// declares as per-repo territory (e.g. "custom" from custom_bridge).
	perRepo map[string]struct{}

	// exists caches store lookups. The store is frozen for the duration
	// of a sync, so a cached answer cannot go stale. The mutex is not for
	// staleness but for callers: bindings sync sequentially today, and
	// guarding the map means that invariant is not load-bearing. An
	// earlier revision asserted it in a comment instead, and the race
	// detector found the assertion through a parallel test.
	mu     sync.Mutex
	exists map[string]bool
}

// newPackRefRules builds the classifier for a pack rooted at packPath
// whose project-root shim dir is prefix (e.g. "_bmad").
func newPackRefRules(packPath, prefix string) *packRefRules {
	return &packRefRules{
		packPath: packPath,
		prefix:   prefix,
		perRepo:  declaredPerRepoDirs(packPath, prefix),
		exists:   make(map[string]bool),
	}
}

// declaredPerRepoDirs reads the pack's custom_bridge declaration and
// returns the shim-relative directories it makes per-repo territory. A
// pack with no pack.yaml, or none declaring a bridge, yields an empty
// set — no reference is preserved on this ground.
func declaredPerRepoDirs(packPath, prefix string) map[string]struct{} {
	out := make(map[string]struct{})

	p, err := distribute.LoadPackYAML(packPath)
	if err != nil || p == nil || p.Distribute.CustomBridge == nil {
		return out
	}

	// upstream_path is repo-relative and starts with the shim dir
	// (e.g. _bmad/custom). The segment after it is what a
	// {project-root}/<prefix>/ reference would name.
	rel := filepath.ToSlash(filepath.Clean(p.Distribute.CustomBridge.UpstreamPath))
	rel = strings.TrimPrefix(rel, prefix+"/")
	if seg := firstSegment(rel); seg != "" {
		out[seg] = struct{}{}
	}
	return out
}

// rewrite replaces pack-content references with the absolute store path
// while leaving project-state references literal.
func (r *packRefRules) rewrite(content string) string {
	token := "{project-root}/" + r.prefix + "/"

	var b strings.Builder
	rest := content
	for {
		i := strings.Index(rest, token)
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}

		b.WriteString(rest[:i])
		tail := rest[i+len(token):]

		if r.isPackContent(refCandidate(tail)) {
			b.WriteString(r.packPath + "/")
		} else {
			b.WriteString(token)
		}
		rest = tail
	}
}

// isPackContent reports whether a shim-relative reference names content
// that lives in the frozen store and is not declared per-repo territory.
func (r *packRefRules) isPackContent(ref string) bool {
	if _, ok := r.perRepo[firstSegment(ref)]; ok {
		return false
	}
	return r.storeHas(literalPrefix(ref))
}

// storeHas reports whether a shim-relative path exists in the store.
func (r *packRefRules) storeHas(rel string) bool {
	r.mu.Lock()
	hit, ok := r.exists[rel]
	r.mu.Unlock()
	if ok {
		return hit
	}

	_, err := os.Lstat(filepath.Join(r.packPath, filepath.FromSlash(rel)))
	hit = err == nil

	r.mu.Lock()
	r.exists[rel] = hit
	r.mu.Unlock()
	return hit
}

// verify is the sync-time post-condition: no path resolving inside the
// frozen store may be absent from it. Structural — an existence test over
// declared content, not a health metric — so it may gate
// (.claude/rules/diagnostic-not-gate.md).
//
// It runs independently of rewrite rather than trusting it, so a
// reference that arrived absolute in the pack source is caught too.
func (r *packRefRules) verify(content string) error {
	token := r.packPath + "/"
	dangling := make(map[string]struct{})

	rest := content
	for {
		i := strings.Index(rest, token)
		if i < 0 {
			break
		}
		rest = rest[i+len(token):]

		rel := literalPrefix(refCandidate(rest))
		if rel != "" && !r.storeHas(rel) {
			dangling[rel] = struct{}{}
		}
	}

	if len(dangling) == 0 {
		return nil
	}

	refs := make([]string, 0, len(dangling))
	for rel := range dangling {
		refs = append(refs, rel)
	}
	sort.Strings(refs)

	return fmt.Errorf("%w: %s resolves %s inside the read-only pack store, which does not exist there",
		ErrDanglingPackRefs, r.packPath, strings.Join(refs, ", "))
}

// refCandidate extracts the path that follows a reference token, ending
// at the first character that cannot continue a path in prose, code
// fences, or shell arguments. Placeholder braces are kept — literalPrefix
// trims them.
func refCandidate(s string) string {
	end := strings.IndexFunc(s, func(rn rune) bool {
		switch rn {
		case ' ', '\t', '\n', '\r', '`', '"', '\'', ')', ']', '>', '<', '|', ',', ';', ':', '=':
			return true
		}
		return false
	})
	if end >= 0 {
		s = s[:end]
	}
	return strings.TrimRight(s, ".")
}

// literalPrefix reduces a reference to the longest leading portion that
// contains no template placeholder, backing up to the last path
// separator so a partially-templated segment is not tested as a literal.
//
//	bmm/agents/pm.md      → bmm/agents/pm.md
//	memory/{skillName}/   → memory/
//	bmm/agent-{code}.md   → bmm/
//	{skill-name}.toml     → ""   (checks the pack root)
func literalPrefix(ref string) string {
	i := strings.IndexByte(ref, '{')
	if i < 0 {
		return ref
	}
	ref = ref[:i]
	j := strings.LastIndexByte(ref, '/')
	if j < 0 {
		return ""
	}
	return ref[:j+1]
}

// firstSegment returns the first path segment of a relative reference.
func firstSegment(ref string) string {
	ref = strings.TrimPrefix(ref, "/")
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		return ref[:i]
	}
	return ref
}

// appendFallbackFooter adds LLM-executable guidance so that pack-internal
// workflow/step/skill files retaining literal {project-root}/_bmad/...
// references can resolve at orchestrator roots that have no _bmad/
// directory.
//
// The top-level entry file (slash command or skill SKILL.md) gets its
// pack-content references rewritten by packRefRules.rewrite. Files the
// entry file loads are not rewritten — they live inside the installed
// pack and stay literal. So do project-state references anywhere, which
// the classifier deliberately leaves alone. The footer tells the reading
// LLM to resolve such references via a two-step fallback chain:
//
//  1. Try cwd-relative first (works for per-project installs, and is the
//     correct resolution for project state).
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

**This substitution is READ-ONLY.** Never write, move, rename, or delete through the substituted pack path — pack content is immutable shared state, not project state. Project-local writes belong in X{project-root}/_bmad-custom/X or X{project-root}/_bmad-output/X. If a workflow requires a writable X{project-root}/_bmad/X, STOP and tell the user instead of substituting.
<!-- sideshow:fallback-resolution:end -->
`
	footer := strings.ReplaceAll(tmpl, "X", "`")
	footer = strings.ReplaceAll(footer, "__PACK_PATH__", packPath)

	return content + footer
}
