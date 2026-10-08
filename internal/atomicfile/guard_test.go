package atomicfile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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
	"internal/foreign/enables.go:CanWriteSettings":             "out: opens O_WRONLY to check writability, writes and truncates nothing",
	"internal/atomicfile/atomicfile.go:createTempBeside":       "out: the helper itself; creates a fresh temp file with O_EXCL",
	"internal/pack/pack.go:Install":                            "out: install-level staging, its own issue if wanted (#185 ruling)",
}

// findInPlaceWriters returns "<path>:<function>" (or "<path>:var <name>"
// for a package-level initializer) for each non-test reference under
// root's internal/ and cmd/ to a function that writes a file in place.
//
// What it flags, by resolving each file's own import names for "os" and
// "io/ioutil" (so a renamed import and a dot import are followed):
//   - any reference to WriteFile, Create or Truncate in those packages,
//     called or not, so an alias, a method value and ioutil.WriteFile are
//     all seen;
//   - any reference to OpenFile, unless it is a call whose flag argument is
//     os.O_RDONLY or the literal 0; a flag held in a variable, or any write
//     bit with or without O_TRUNC, is flagged;
//   - function bodies and package-level initializers alike.
//
// What it does not see, so a reader knows the limit: a write through any
// other package (syscall, golang.org/x/sys, a vendored writer), a shell-out
// to cp or tee, a write through an *os.File opened elsewhere (the open is
// what it flags), and a writer reached by reflection or a plugin. os.CreateTemp
// and os.Rename are not in-place writes and are not flagged.
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
			file, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if perr != nil {
				return perr
			}
			rel, _ := filepath.Rel(root, path)
			scanFile(file, filepath.ToSlash(rel), found)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return found
}

var writerNames = map[string]bool{"WriteFile": true, "Create": true, "Truncate": true, "OpenFile": true}

func scanFile(file *ast.File, rel string, found map[string]bool) {
	pkgs := map[string]string{} // local name -> "os" or "ioutil"
	dot := false
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		var base string
		switch path {
		case "os":
			base = "os"
		case "io/ioutil":
			base = "ioutil"
		default:
			continue
		}
		switch {
		case imp.Name == nil:
			pkgs[base] = base
		case imp.Name.Name == ".":
			dot = true
		case imp.Name.Name != "_":
			pkgs[imp.Name.Name] = base
		}
	}
	if len(pkgs) == 0 && !dot {
		return
	}
	for _, decl := range file.Decls {
		var key string
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Body == nil {
				continue
			}
			key = rel + ":" + d.Name.Name
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			key = rel + ":var"
			for _, sp := range d.Specs {
				if vs, ok := sp.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
					key = rel + ":var " + vs.Names[0].Name
				}
			}
		default:
			continue
		}
		flagged := false
		okRefs := map[ast.Node]bool{}
		ast.Inspect(decl, func(n ast.Node) bool {
			if n == nil || okRefs[n] {
				return false
			}
			if call, ok := n.(*ast.CallExpr); ok {
				if _, name, ok := writerRef(call.Fun, pkgs, dot); ok && name == "OpenFile" && len(call.Args) >= 2 && readOnlyFlag(call.Args[1], pkgs) {
					okRefs[call.Fun] = true
				}
				return true
			}
			if _, _, ok := writerRef(n, pkgs, dot); ok {
				flagged = true
			}
			return true
		})
		if flagged {
			found[key] = true
		}
	}
}

// writerRef reports whether n is a reference to a writer from os or
// ioutil: pkg.Name through a local import name, or a bare Name when the
// file dot-imports one of them.
func writerRef(n ast.Node, pkgs map[string]string, dot bool) (ast.Expr, string, bool) {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && writerNames[x.Sel.Name] {
			if _, isPkg := pkgs[id.Name]; isPkg {
				return x, x.Sel.Name, true
			}
		}
	case *ast.Ident:
		if dot && writerNames[x.Name] {
			return x, x.Name, true
		}
	}
	return nil, "", false
}

// readOnlyFlag reports whether an OpenFile flag argument is exactly
// os.O_RDONLY (through any local name for os) or the literal 0.
func readOnlyFlag(e ast.Expr, pkgs map[string]string) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Value == "0"
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && pkgs[id.Name] == "os" && x.Sel.Name == "O_RDONLY"
	}
	return false
}

// checkAllowlist compares what the detector found with the allowlist and
// returns the unlisted writers, the stale entries and the entries with no
// reason.
func checkAllowlist(found map[string]bool, allow map[string]string) (unlisted, stale, noReason []string) {
	for k := range found {
		if _, ok := allow[k]; !ok {
			unlisted = append(unlisted, k)
		}
	}
	for k, why := range allow {
		if !found[k] {
			stale = append(stale, k)
		}
		if strings.TrimSpace(why) == "" {
			noReason = append(noReason, k)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	sort.Strings(noReason)
	return unlisted, stale, noReason
}

// Every in-place writer is on the allowlist with a reason, and every
// allowlist entry still names a writer (sideshow#185).
func TestNoUnlistedInPlaceWriters(t *testing.T) {
	found := findInPlaceWriters(t, filepath.Join("..", ".."))
	unlisted, stale, noReason := checkAllowlist(found, inPlaceAllowlist)
	if len(noReason) > 0 {
		t.Errorf("allowlist entries with no reason:\n  %s", strings.Join(noReason, "\n  "))
	}
	if len(unlisted) > 0 {
		t.Errorf("in-place writers not on the allowlist; use atomicfile.WriteFile, or list them with a reason:\n  %s", strings.Join(unlisted, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("allowlist entries that no longer write in place; delete them:\n  %s", strings.Join(stale, "\n  "))
	}
}

// The three checks fire: an unlisted writer, a stale entry, an entry with
// no reason.
func TestCheckAllowlist_FiresOnEachProblem(t *testing.T) {
	found := map[string]bool{"a.go:New": true, "a.go:Old": true}
	allow := map[string]string{"a.go:Old": "out: reason", "a.go:Gone": "pending", "a.go:Blank": " "}
	unlisted, stale, noReason := checkAllowlist(found, allow)
	if strings.Join(unlisted, ",") != "a.go:New" {
		t.Errorf("unlisted = %v", unlisted)
	}
	if strings.Join(stale, ",") != "a.go:Blank,a.go:Gone" {
		t.Errorf("stale = %v", stale)
	}
	if strings.Join(noReason, ",") != "a.go:Blank" {
		t.Errorf("noReason = %v", noReason)
	}
}

// The detector itself: every form below is flagged and the controls are
// not. Each form is one function (or one package-level var) in one file.
func TestFindInPlaceWriters_Forms(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/a/a.go", `package a
import ("os"; "io/ioutil")
func Plain() { _ = os.WriteFile("x", nil, 0o644) }
func Create() { _, _ = os.Create("x") }
func Trunc() { _, _ = os.OpenFile("x", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) }
func NoTrunc() { _, _ = os.OpenFile("x", os.O_WRONLY|os.O_CREATE, 0o644) }
func FlagVar() { fl := os.O_WRONLY | os.O_CREATE | os.O_TRUNC; _, _ = os.OpenFile("x", fl, 0o644) }
func Alias() { w := os.WriteFile; _ = w("x", nil, 0o644) }
func MethodValue() { run(os.WriteFile) }
func run(f func(string, []byte, os.FileMode) error) {}
func Ioutil() { _ = ioutil.WriteFile("x", nil, 0o644) }
func Truncate() { _ = os.Truncate("x", 0) }
var F = func() { _ = os.WriteFile("x", nil, 0o644) }
func ReadOnly() { _, _ = os.OpenFile("x", os.O_RDONLY, 0) }
func SelectorWrite() { _, _ = os.OpenFile("x", os.O_WRONLY, 0) }
func LiteralWrite() { _, _ = os.OpenFile("x", 1, 0) }
func ReadOnlyZero() { _, _ = os.OpenFile("x", 0, 0) }
func Read() { _, _ = os.ReadFile("x") }
func Temp() { _, _ = os.CreateTemp("", "x") }
`)
	write("internal/b/b.go", "package b\nimport osx \"os\"\nfunc Renamed() { _ = osx.WriteFile(\"x\", nil, 0o644) }\n")
	write("internal/c/c.go", "package c\nimport . \"os\"\nfunc Dot() { _ = WriteFile(\"x\", nil, 0o644) }\n")
	write("internal/d/d.go", "package d\ntype T struct{}\nfunc (T) WriteFile() {}\nfunc Local() { T{}.WriteFile() }\nfunc WriteFile() {}\nfunc Own() { WriteFile() }\n")
	write("cmd/e/e.go", "package main\nimport \"os\"\nfunc main() { _ = os.WriteFile(\"x\", nil, 0o644) }\n")
	write("internal/a/a_test.go", "package a\nimport \"os\"\nfunc TestX() { _ = os.WriteFile(\"x\", nil, 0o644) }\n")
	got := findInPlaceWriters(t, root)
	want := []string{
		"cmd/e/e.go:main",
		"internal/a/a.go:Alias", "internal/a/a.go:Create", "internal/a/a.go:FlagVar",
		"internal/a/a.go:Ioutil", "internal/a/a.go:LiteralWrite", "internal/a/a.go:MethodValue", "internal/a/a.go:NoTrunc",
		"internal/a/a.go:Plain", "internal/a/a.go:SelectorWrite", "internal/a/a.go:Trunc", "internal/a/a.go:Truncate",
		"internal/a/a.go:var F", "internal/b/b.go:Renamed", "internal/c/c.go:Dot",
	}
	var names []string
	for k := range got {
		names = append(names, k)
	}
	sort.Strings(names)
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Errorf("detected:\n  %s\nwant:\n  %s", strings.Join(names, "\n  "), strings.Join(want, "\n  "))
	}
}
