package atomicfile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// inPlaceAllowlist names every non-test function under internal/ and cmd/
// that writes a file in place (os.WriteFile, os.Create, or os.OpenFile
// with O_TRUNC) and has not been converted to WriteFile. The key is
// "<path>:<function>"; the value is why it is allowed. The rule for what
// stays in is sideshow#185: a file is in scope when a torn write would
// lose bytes nothing regenerates, or leave a file the next run will not
// rewrite. Each tier PR of #185 deletes its own entries; an entry whose
// call has been converted fails the stale check below, so the list can
// only shrink.
var inPlaceAllowlist = map[string]string{
	"cmd/sideshow/project_identity.go:seedIdentity":            "pending: #185 tier 2 (state)",
	"internal/bindings/custom_sources.go:saveCustomSources":    "pending: #185 tier 2 (state)",
	"internal/project/project.go:InitIdentity":                 "pending: #185 tier 2 (state)",
	"internal/init/init.go:Run":                                "pending: #185 tier 2 (state)",
	"internal/distribute/distribute.go:distributeRule":         "pending: #185 tier 3 (user files)",
	"internal/distribute/distribute.go:distributeClaudeMD":     "pending: #185 tier 3 (user files)",
	"internal/distribute/distribute.go:distributeGitignore":    "pending: #185 tier 3 (user files)",
	"internal/distribute/distribute.go:seedCustomTemplate":     "pending: #185 tier 3, to classify (seed)",
	"internal/distribute/distribute.go:distributeFile":         "pending: #185 tier 3, to classify (pack file)",
	"internal/weave/ops_csv.go:writeFilePreservingMode":        "pending: #185 tier 3 (user files)",
	"internal/distribute/distribute.go:distributeCustomBridge": "out: an empty .gitkeep; nothing to lose (#185 ruling)",
	"internal/bindings/bound_variant.go:RenderBoundVariant":    "out: rebuilt from the store on every run (#185 ruling)",
	"internal/bindings/bound_variant.go:translateExecManifest": "out: rebuilt from the store on every run (#185 ruling)",
	"internal/bindings/write_mode.go:writeWithSourceMode":      "out: installed copy rewritten from its source on every run (#185 ruling)",
	"internal/pack/pack.go:Install":                            "out: install-level staging, its own issue if wanted (#185 ruling)",
}

// findInPlaceWriters returns "<path>:<function>" for each non-test call
// under root's internal/ and cmd/ that writes in place.
func findInPlaceWriters(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" {
						return true
					}
					switch sel.Sel.Name {
					case "WriteFile", "Create":
						found[rel+":"+fn.Name.Name] = true
					case "OpenFile":
						if len(call.Args) >= 2 && mentionsTrunc(call.Args[1]) {
							found[rel+":"+fn.Name.Name] = true
						}
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return found
}

func mentionsTrunc(e ast.Expr) bool {
	trunc := false
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "O_TRUNC" {
			trunc = true
		}
		return true
	})
	return trunc
}

// Every in-place writer is on the allowlist with a reason, and every
// allowlist entry still names a writer (sideshow#185).
func TestNoUnlistedInPlaceWriters(t *testing.T) {
	found := findInPlaceWriters(t, filepath.Join("..", ".."))
	var unlisted, stale []string
	for k := range found {
		if _, ok := inPlaceAllowlist[k]; !ok {
			unlisted = append(unlisted, k)
		}
	}
	for k, why := range inPlaceAllowlist {
		if !found[k] {
			stale = append(stale, k)
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("allowlist entry %s has no reason", k)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	if len(unlisted) > 0 {
		t.Errorf("in-place writers not on the allowlist; use atomicfile.WriteFile, or list them with a reason:\n  %s", strings.Join(unlisted, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("allowlist entries that no longer write in place; delete them:\n  %s", strings.Join(stale, "\n  "))
	}
}
