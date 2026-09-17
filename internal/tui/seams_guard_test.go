package tui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestNoParallelTestSwapsAPackageSeam is cmd/apogee's guard (seams_guard_test.go there) mirrored
// for this package, which the apogee-11s sweep made parallel by default. The seam set is derived,
// never listed — every identifier a top-level `var` in this package's non-test files declares
// (writeSystemClipboard, commandSpecs, approvalMenu, …) — so a seam added tomorrow is guarded the
// day it lands. A test function or t.Run literal that runs under t.Parallel (its own call or an
// enclosing scope's — a serial subtest of a parallel test still races every other parallel test)
// and assigns to one of them fails here with its file:line.
//
// Unlike cmd/apogee's, this package's one swap lives in a named helper — recordSystemClipboard
// (mouse_test.go) assigns writeSystemClipboard for the two clipboard tests that call it — so the
// walk resolves calls to this package's test-file functions transitively: a helper whose body
// assigns a seam, directly or through another helper, makes every parallel caller a finding,
// reported at the call. The helpers are copied from cmd/apogee rather than shared: two packages,
// two guards, and the code is test code.
func TestNoParallelTestSwapsAPackageSeam(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	seams := packageSeams(t, fset)
	if len(seams) == 0 {
		t.Fatal("no package-level vars found — the seam set is derived from the non-test files, so an empty set means the scan read nothing")
	}
	if !seams["writeSystemClipboard"] {
		t.Error("seam set lacks \"writeSystemClipboard\" — the scan of the non-test files missed a known seam")
	}

	var files []*ast.File
	for _, path := range packageGoFiles(t, true) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, file)
	}
	viaHelper := helperSeamWrites(testFuncs(files), seams)

	var findings []string
	for _, file := range files {
		findings = append(findings, parallelSeamWrites(fset, file, seams, viaHelper)...)
	}

	for _, finding := range findings {
		t.Error(finding)
	}
}

// ...and the detector proven on a fixture rather than assumed: a parallel test assigning
// writeSystemClipboard is reported at its line, a serial one and a `_ =` write are not, a subtest
// inherits its parent's parallelism, and a parallel test that reaches the seam through a helper —
// one level down or two — is reported at the call, while the same call from a serial test is not.
func TestNoParallelTestSwapsAPackageSeamBites(t *testing.T) {
	t.Parallel()

	const fixture = `package tui

import "testing"

var _ = 0

func TestParallelSwap(t *testing.T) {
	t.Parallel()
	prev := writeSystemClipboard
	writeSystemClipboard = nil
	t.Cleanup(func() { writeSystemClipboard = prev })
}

func TestSerialSwap(t *testing.T) {
	prev := writeSystemClipboard
	writeSystemClipboard = nil
	t.Cleanup(func() { writeSystemClipboard = prev })
}

func TestParallelBlankWrite(t *testing.T) {
	t.Parallel()
	_ = writeSystemClipboard
}

func TestParallelParentSerialSubtest(t *testing.T) {
	t.Parallel()
	t.Run("sub", func(t *testing.T) {
		writeSystemClipboard = nil
	})
}

func TestSerialParentParallelSubtest(t *testing.T) {
	t.Run("sub", func(st *testing.T) {
		st.Parallel()
		commandSpecs = nil
	})
}

func TestParallelShadow(t *testing.T) {
	t.Parallel()
	writeSystemClipboard := 1
	writeSystemClipboard = 2
	_ = writeSystemClipboard
}

func TestParallelViaHelper(t *testing.T) {
	t.Parallel()
	swapClipboard(t)
	viaTwoHelpers(t)
	readsOnly(t)
}

func TestSerialViaHelper(t *testing.T) {
	swapClipboard(t)
}

func TestParallelShadowedHelper(t *testing.T) {
	t.Parallel()
	swapClipboard := func(t *testing.T) {}
	swapClipboard(t)
}

func swapClipboard(t *testing.T) {
	t.Helper()
	prev := writeSystemClipboard
	t.Cleanup(func() { writeSystemClipboard = prev })
	writeSystemClipboard = nil
}

func viaTwoHelpers(t *testing.T) {
	swapClipboard(t)
	swapMenu(t)
}

func swapMenu(t *testing.T) {
	commandSpecs = nil
	viaTwoHelpers(t)
}

func readsOnly(t *testing.T) {
	_ = writeSystemClipboard
	commandSpecs := 1
	commandSpecs = 2
	_ = commandSpecs
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture_test.go", fixture, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	seams := map[string]bool{"writeSystemClipboard": true, "commandSpecs": true}

	got := parallelSeamWrites(fset, file, seams, helperSeamWrites(testFuncs([]*ast.File{file}), seams))

	want := []string{
		"fixture_test.go:10: TestParallelSwap swaps the package seam writeSystemClipboard under t.Parallel — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:11: TestParallelSwap swaps the package seam writeSystemClipboard under t.Parallel — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:28: TestParallelParentSerialSubtest/sub swaps the package seam writeSystemClipboard under t.Parallel — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:35: TestSerialParentParallelSubtest/sub swaps the package seam commandSpecs under t.Parallel — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:48: TestParallelViaHelper swaps the package seam writeSystemClipboard under t.Parallel through swapClipboard — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:49: TestParallelViaHelper swaps the package seam commandSpecs under t.Parallel through viaTwoHelpers — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:49: TestParallelViaHelper swaps the package seam writeSystemClipboard under t.Parallel through viaTwoHelpers — a sibling test may be reading it (apogee-ku1)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("detector findings differ\n got:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// packageSeams returns every identifier a top-level `var` declares across this package's
// non-test files. The blank identifier is excluded: `var _ domain.Approver = (*uiApprover)(nil)`
// is an interface assertion, not a seam, and `_ = …` in a parallel test writes nothing.
func packageSeams(t *testing.T, fset *token.FileSet) map[string]bool {
	t.Helper()

	seams := map[string]bool{}
	for _, path := range packageGoFiles(t, false) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gen, isGen := decl.(*ast.GenDecl)
			if !isGen || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					if name.Name != "_" {
						seams[name.Name] = true
					}
				}
			}
		}
	}
	return seams
}

// packageGoFiles lists this package directory's Go files — the `_test.go` ones when tests is
// set, the rest otherwise — sorted so findings come out in a stable order.
func packageGoFiles(t *testing.T, tests bool) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}
		if strings.HasSuffix(name, "_test.go") == tests {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	return paths
}

// testFuncs indexes every top-level function (no receiver, with a body) the given test files
// declare, by name — the helpers a test may reach a seam through. A name declared twice (a
// build-tagged pair) keeps the first file's body; both are parsed, so a seam write in either
// still lands in the seam set the parallel scan reads.
func testFuncs(files []*ast.File) map[string]*ast.FuncDecl {
	funcs := map[string]*ast.FuncDecl{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			if !isFunc || fn.Recv != nil || fn.Body == nil {
				continue
			}
			if _, seen := funcs[fn.Name.Name]; !seen {
				funcs[fn.Name.Name] = fn
			}
		}
	}
	return funcs
}

// helperSeamWrites returns, for every function in funcs that assigns a seam — in its own body
// (nested literals included, since a t.Cleanup closure restoring the seam is as much a write as
// the swap) or through another of funcs it calls, at any depth — the sorted seam names it
// writes. A name the function declares locally shadows the seam, as in a test body. Only plain
// `name(...)` calls resolve; a cycle among helpers is walked once.
func helperSeamWrites(funcs map[string]*ast.FuncDecl, seams map[string]bool) map[string][]string {
	direct := map[string]map[string]bool{}
	calls := map[string][]string{}
	for name, fn := range funcs {
		shadowed := locallyDeclared(fn.Body)
		for _, param := range fn.Type.Params.List {
			for _, pname := range param.Names {
				shadowed[pname.Name] = true
			}
		}
		writes := map[string]bool{}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			var targets []ast.Expr
			switch n := node.(type) {
			case *ast.AssignStmt:
				if n.Tok != token.DEFINE {
					targets = n.Lhs
				}
			case *ast.IncDecStmt:
				targets = []ast.Expr{n.X}
			case *ast.CallExpr:
				if ident, isIdent := n.Fun.(*ast.Ident); isIdent && !shadowed[ident.Name] {
					if _, isHelper := funcs[ident.Name]; isHelper {
						calls[name] = append(calls[name], ident.Name)
					}
				}
			}
			for _, target := range targets {
				if ident := rootIdent(target); ident != nil && seams[ident.Name] && !shadowed[ident.Name] {
					writes[ident.Name] = true
				}
			}
			return true
		})
		direct[name] = writes
	}

	memo := map[string][]string{}
	var resolve func(name string, visiting map[string]bool) map[string]bool
	resolve = func(name string, visiting map[string]bool) map[string]bool {
		writes := map[string]bool{}
		for seam := range direct[name] {
			writes[seam] = true
		}
		if visiting[name] {
			return writes
		}
		visiting[name] = true
		for _, callee := range calls[name] {
			for seam := range resolve(callee, visiting) {
				writes[seam] = true
			}
		}
		delete(visiting, name)
		return writes
	}
	for name := range funcs {
		var names []string
		for seam := range resolve(name, map[string]bool{}) {
			names = append(names, seam)
		}
		if len(names) > 0 {
			sort.Strings(names)
			memo[name] = names
		}
	}
	return memo
}

// parallelSeamWrites reports every assignment to a name in seams made by a test scope — a
// `func TestXxx(t *testing.T)` or a t.Run func literal — that runs under t.Parallel, whether
// the scope calls it itself or inherits it from an enclosing scope; and every call from such a
// scope to a function in viaHelper, reported at the call for each seam that helper writes. A
// name the test function declares locally (`writeSystemClipboard := …`, `var …`, a parameter or
// range variable) shadows the seam — or the helper — and is not counted. Findings are
// `file:line: <scope> swaps …`, in source order.
func parallelSeamWrites(fset *token.FileSet, file *ast.File, seams map[string]bool, viaHelper map[string][]string) []string {
	var findings []string
	for _, decl := range file.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if !isFunc || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
			continue
		}
		shadowed := locallyDeclared(fn.Body)
		for _, param := range fn.Type.Params.List {
			for _, name := range param.Names {
				shadowed[name.Name] = true
			}
		}
		findings = append(findings, seamWritesInScope(fset, fn.Name.Name, fn.Body, false, seams, shadowed, viaHelper)...)
	}
	return findings
}

// seamWritesInScope walks one test scope's body. Nested t.Run literals open their own scope
// (named parent/child) and inherit parallel; every other func literal (a t.Cleanup closure, a
// helper) is part of the scope that holds it.
func seamWritesInScope(fset *token.FileSet, scope string, body *ast.BlockStmt, parallel bool, seams, shadowed map[string]bool, viaHelper map[string][]string) []string {
	parallel = parallel || callsParallel(body)

	var findings []string
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if lit, name, isRun := runLiteral(n); isRun {
				findings = append(findings, seamWritesInScope(fset, scope+"/"+name, lit.Body, parallel, seams, shadowed, viaHelper)...)
				return false
			}
			findings = append(findings, helperSeamWrite(fset, scope, n, parallel, shadowed, viaHelper)...)
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				return true
			}
			for _, lhs := range n.Lhs {
				findings = append(findings, seamWrite(fset, scope, lhs, parallel, seams, shadowed)...)
			}
		case *ast.IncDecStmt:
			findings = append(findings, seamWrite(fset, scope, n.X, parallel, seams, shadowed)...)
		}
		return true
	})
	return findings
}

// seamWrite reports lhs when it is (or is rooted at — `seam.field = …`, `seam[k] = …`) a seam
// identifier and the scope is parallel.
func seamWrite(fset *token.FileSet, scope string, lhs ast.Expr, parallel bool, seams, shadowed map[string]bool) []string {
	ident := rootIdent(lhs)
	if ident == nil || !parallel || !seams[ident.Name] || shadowed[ident.Name] {
		return nil
	}
	pos := fset.Position(ident.Pos())
	return []string{fmt.Sprintf("%s:%d: %s swaps the package seam %s under t.Parallel — a sibling test may be reading it (apogee-ku1)",
		pos.Filename, pos.Line, scope, ident.Name)}
}

// helperSeamWrite reports call when it names a function in viaHelper — a plain `name(...)`
// call, not shadowed by a local — and the scope is parallel: one finding per seam the helper
// writes, at the call's line.
func helperSeamWrite(fset *token.FileSet, scope string, call *ast.CallExpr, parallel bool, shadowed map[string]bool, viaHelper map[string][]string) []string {
	ident, isIdent := call.Fun.(*ast.Ident)
	if !isIdent || !parallel || shadowed[ident.Name] || len(viaHelper[ident.Name]) == 0 {
		return nil
	}
	pos := fset.Position(ident.Pos())
	var findings []string
	for _, seam := range viaHelper[ident.Name] {
		findings = append(findings, fmt.Sprintf("%s:%d: %s swaps the package seam %s under t.Parallel through %s — a sibling test may be reading it (apogee-ku1)",
			pos.Filename, pos.Line, scope, seam, ident.Name))
	}
	return findings
}

// rootIdent returns the identifier an assignment target is rooted at: the ident itself, or the
// innermost X of a selector / index / star chain.
func rootIdent(expr ast.Expr) *ast.Ident {
	for {
		switch e := expr.(type) {
		case *ast.Ident:
			return e
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		default:
			return nil
		}
	}
}

// callsParallel reports whether body calls `<x>.Parallel()` directly — not inside a nested
// func literal, whose Parallel belongs to the subtest it runs.
func callsParallel(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		switch n := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if sel, isSel := n.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "Parallel" && len(n.Args) == 0 {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// runLiteral recognises `<x>.Run(name, func(...) {...})` and returns the literal and a label
// for it: the string literal's value when the name is one, else `run`.
func runLiteral(call *ast.CallExpr) (*ast.FuncLit, string, bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "Run" || len(call.Args) != 2 {
		return nil, "", false
	}
	lit, isLit := call.Args[1].(*ast.FuncLit)
	if !isLit || lit.Body == nil {
		return nil, "", false
	}
	name := "run"
	if basic, isBasic := call.Args[0].(*ast.BasicLit); isBasic && basic.Kind == token.STRING {
		name = strings.Trim(basic.Value, "`\"")
	}
	return lit, name, true
}

// locallyDeclared collects every name a function body declares — `:=`, `var`, range variables
// (nested literals included) — so a local that shadows a seam name is not mistaken for the seam.
func locallyDeclared(body *ast.BlockStmt) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, lhs := range n.Lhs {
					if ident, isIdent := lhs.(*ast.Ident); isIdent {
						names[ident.Name] = true
					}
				}
			}
		case *ast.GenDecl:
			if n.Tok == token.VAR {
				for _, spec := range n.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						names[name.Name] = true
					}
				}
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				for _, expr := range []ast.Expr{n.Key, n.Value} {
					if ident, isIdent := expr.(*ast.Ident); isIdent {
						names[ident.Name] = true
					}
				}
			}
		case *ast.FuncLit:
			for _, param := range n.Type.Params.List {
				for _, name := range param.Names {
					names[name.Name] = true
				}
			}
		}
		return true
	})
	return names
}
