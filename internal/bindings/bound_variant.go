package bindings

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ArcavenAE/sideshow/internal/pack"
)

// The bound variant is the transformed derivative of a plugin pack's
// materialize-disposition units (aae-orc-d3nq.51): store content is
// signed reference and stays byte-frozen, so the binding-prefix and
// namespace-rewrite transforms render ONCE per pack version into a
// machine-scoped derived tree under the sideshow data dir. Repos then
// materialize FROM the variant: local scope symlinks into it (still
// one copy per machine, exec bits in one place, version flip is one
// repoint), project scope copies from it. The transforms applied here
// are the declared derivative the rewrite manifest records
// (aae-orc-d3nq.61) and the equivalence proof subtracts.
//
// Why the transforms exist (finding-091):
//   - T16 corrected: a USER-scope skill shadows a same-name repo
//     skill, so unprefixed generic names (upstream ships `jira`, 34
//     generic agent names) would be silently hijacked on consumer
//     machines. Prefixing is correctness, not hygiene.
//   - T20: repo-scope addressing is the bare frontmatter name; the
//     namespace-qualified `<pack>:<name>` form cannot occur on this
//     channel, so every internal reference is rewritten to the
//     prefixed bare name.

// BoundVariantDir returns the canonical location of a pack version's
// bound variant inside the sideshow data dir (sibling of packs/).
func BoundVariantDir(packName, version string) string {
	return filepath.Join(filepath.Dir(pack.PacksDir()), "bound", packName, version)
}

// RenderBoundVariant renders the transformed derivative of the
// inventory's materialize units from storeRoot into destDir, and
// returns the inventory of the variant (prefixed names, same shape)
// for MaterializeRepo to consume. An existing destDir is rebuilt from
// scratch — it is a derived cache; rebuilding the same version is
// deterministic and repos symlinked into it see identical content.
func RenderBoundVariant(storeRoot string, inv *PluginInventory, packName, prefix, destDir string) (*PluginInventory, error) {
	if prefix == "" {
		return nil, fmt.Errorf("empty binding prefix for pack %s", packName)
	}
	if err := os.RemoveAll(destDir); err != nil {
		return nil, fmt.Errorf("clear bound variant dir: %w", err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create bound variant dir: %w", err)
	}

	nsRewrite, err := namespaceRewriter(packName, prefix)
	if err != nil {
		return nil, err
	}

	out := &PluginInventory{}

	for _, s := range inv.SkillDirs {
		name := filepath.Base(s)
		dstRel := filepath.Join("skills", prefix+"-"+name)
		// The skill's own SKILL.md carries the harness identifier that
		// must agree with the prefixed directory name; deeper files
		// (including any the skill nests) only get the namespace
		// rewrite.
		skillTransform := func(rel string, data []byte) []byte {
			if !shouldRewrite(rel) {
				return data
			}
			content := nsRewrite(string(data))
			if rel == "SKILL.md" {
				content = rewriteFrontmatterName(content, prefix)
			}
			return []byte(content)
		}
		if err := copyTransformed(
			filepath.Join(storeRoot, filepath.FromSlash(s)),
			filepath.Join(destDir, dstRel),
			skillTransform,
		); err != nil {
			return nil, fmt.Errorf("render skill %s: %w", name, err)
		}
		out.SkillDirs = append(out.SkillDirs, filepath.ToSlash(dstRel))
	}

	for _, a := range inv.AgentFiles {
		dstRel, err := prefixTopLevel(a, "agents", prefix)
		if err != nil {
			return nil, err
		}
		src := filepath.Join(storeRoot, filepath.FromSlash(a))
		dst := filepath.Join(destDir, filepath.FromSlash(dstRel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("create agent dir: %w", err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("read agent %s: %w", a, err)
		}
		if shouldRewrite(src) {
			content := nsRewrite(string(data))
			content = rewriteFrontmatterName(content, prefix)
			data = []byte(content)
		}
		if err := writeWithSourceMode(dst, data, src); err != nil {
			return nil, fmt.Errorf("render agent %s: %w", a, err)
		}
		out.AgentFiles = append(out.AgentFiles, dstRel)
	}

	if err := translateExecManifest(storeRoot, destDir, inv, prefix); err != nil {
		return nil, err
	}

	return out, nil
}

// namespaceRewriter builds the `<pack>:<name>` → `<prefix>-<name>`
// content rewrite. The qualifier must be immediately followed by an
// artifact-name character, so prose like "the pack: a plugin" is
// untouched. One rule covers slash-form skill references and
// subagent_type call sites alike.
func namespaceRewriter(packName, prefix string) (func(string) string, error) {
	re, err := regexp.Compile(regexp.QuoteMeta(packName) + `:([A-Za-z0-9][A-Za-z0-9_-]*)`)
	if err != nil {
		return nil, fmt.Errorf("compile namespace rewrite for %s: %w", packName, err)
	}
	repl := prefix + "-$1"
	return func(content string) string {
		return re.ReplaceAllString(content, repl)
	}, nil
}

// frontmatterNameLine matches the name: field of a YAML frontmatter
// block's first line occurrence.
var frontmatterNameLine = regexp.MustCompile(`(?m)^name:[ \t]*(\S+)[ \t]*$`)

// rewriteFrontmatterName prefixes the frontmatter name: field so the
// harness identifier agrees with the prefixed bound location. Only
// the leading frontmatter block is touched; body content is left to
// the namespace rewrite.
func rewriteFrontmatterName(content, prefix string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return content
	}
	head := content[:4+end]
	rest := content[4+end:]
	head = frontmatterNameLine.ReplaceAllStringFunc(head, func(line string) string {
		m := frontmatterNameLine.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(m[1], prefix+"-") {
			return line
		}
		return "name: " + prefix + "-" + m[1]
	})
	return head + rest
}

// prefixTopLevel prefixes the first path segment under root: flat
// files rename (agents/reviewer.md → agents/<p>-reviewer.md), nested
// layouts rename their top directory (agents/orchestrator/planner.md
// → agents/<p>-orchestrator/planner.md) so one gitignore glob per
// pack covers everything and nesting is preserved (bare-name
// addressing per trial T20).
func prefixTopLevel(rel, root, prefix string) (string, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 || parts[0] != root {
		return "", fmt.Errorf("unexpected %s path %q", root, rel)
	}
	parts[1] = prefix + "-" + parts[1]
	return strings.Join(parts, "/"), nil
}

// copyTransformed copies a directory tree applying the transform
// (keyed by unit-relative path) to each file's content while
// preserving source permission bits.
func copyTransformed(src, dst string, transform func(string, []byte) []byte) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeWithSourceMode(target, transform(filepath.ToSlash(rel), data), path)
	})
}

// translateExecManifest writes the variant's exec-manifest.txt:
// census entries under materialized units, re-rooted to the variant's
// prefixed paths, so project-scope copies keep validation rule 3 of
// the unshaping spec end-to-end. A store without the census emits
// none.
func translateExecManifest(storeRoot, destDir string, inv *PluginInventory, prefix string) error {
	data, err := os.ReadFile(filepath.Join(storeRoot, execManifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", execManifestName, err)
	}

	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		rel := strings.TrimSpace(line)
		if rel == "" || strings.HasPrefix(rel, "#") {
			continue
		}
		for _, s := range inv.SkillDirs {
			if rel == s || strings.HasPrefix(rel, s+"/") {
				name := filepath.Base(s)
				out = append(out, "skills/"+prefix+"-"+name+strings.TrimPrefix(rel, s))
				break
			}
		}
		for _, a := range inv.AgentFiles {
			if rel == a {
				prefixed, err := prefixTopLevel(a, "agents", prefix)
				if err == nil {
					out = append(out, prefixed)
				}
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return os.WriteFile(filepath.Join(destDir, execManifestName), []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
