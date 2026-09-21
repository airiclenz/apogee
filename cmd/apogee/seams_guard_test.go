package main

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

// TestNoParallelTestSwapsAPackageSeam pins the shape apogee-ku1 was: a test that called
// t.Parallel() and swapped the then package-level runner while a Scheduler firing in a sibling
// test read it (fixed at 52e0d677 by dropping that test's t.Parallel). That runner and the
// Confiner constructor are dependencies now (headlessDeps, daemonDeps, rootDeps,
// scheduleWiring.runner) and no longer vars, but nine function and clock globals remain in this
// package (hardExit, interruptSignals, prewarmLabelWalk, daemonExecutable, daemonUserHome,
// acquireDaemonLock, daemonClock, watchSchedules, tuiScheduleClock), so the guard stays. The seam
// set is derived, never listed — every identifier a top-level `var` in this package's non-test
// files declares — so a seam added tomorrow is guarded the day it lands; only hardExit is
// hard-required, as the one the fixture below is written against. A test function or t.Run
// literal that runs under t.Parallel (its own call or an enclosing scope's — a serial subtest of a
// parallel test still races every other parallel test) and assigns to one of them fails here with
// its file:line.
func TestNoParallelTestSwapsAPackageSeam(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	seams := packageSeams(t, fset)
	if len(seams) == 0 {
		t.Fatal("no package-level vars found — the seam set is derived from the non-test files, so an empty set means the scan read nothing")
	}
	if !seams["hardExit"] {
		t.Error(`seam set lacks "hardExit" — the scan of the non-test files missed a known seam`)
	}

	var findings []string
	for _, path := range packageGoFiles(t, true) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		findings = append(findings, parallelSeamWrites(fset, file, seams)...)
	}

	for _, finding := range findings {
		t.Error(finding)
	}
}

// ...and the detector proven on a fixture rather than assumed: a parallel test assigning hardExit
// is reported at its line, a serial one and a `_ =` write are not, and a subtest inherits its
// parent's parallelism.
func TestNoParallelTestSwapsAPackageSeamBites(t *testing.T) {
	t.Parallel()

	const fixture = `package main

import "testing"

var _ = 0

func TestParallelSwap(t *testing.T) {
	t.Parallel()
	prev := hardExit
	hardExit = nil
	t.Cleanup(func() { hardExit = prev })
}

func TestSerialSwap(t *testing.T) {
	prev := hardExit
	hardExit = nil
	t.Cleanup(func() { hardExit = prev })
}

func TestParallelBlankWrite(t *testing.T) {
	t.Parallel()
	_ = hardExit
}

func TestParallelParentSerialSubtest(t *testing.T) {
	t.Parallel()
	t.Run("sub", func(t *testing.T) {
		hardExit = nil
	})
}

func TestSerialParentParallelSubtest(t *testing.T) {
	t.Run("sub", func(st *testing.T) {
		st.Parallel()
		interruptSignals = nil
	})
}

func TestParallelShadow(t *testing.T) {
	t.Parallel()
	hardExit := 1
	hardExit = 2
	_ = hardExit
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture_test.go", fixture, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	seams := map[string]bool{"hardExit": true, "interruptSignals": true}

	got := parallelSeamWrites(fset, file, seams)

	want := []string{
		"fixture_test.go:10: TestParallelSwap swaps the package seam hardExit under t.Parallel — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:11: TestParallelSwap swaps the package seam hardExit under t.Parallel — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:28: TestParallelParentSerialSubtest/sub swaps the package seam hardExit under t.Parallel — a sibling test may be reading it (apogee-ku1)",
		"fixture_test.go:35: TestSerialParentParallelSubtest/sub swaps the package seam interruptSignals under t.Parallel — a sibling test may be reading it (apogee-ku1)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("detector findings differ\n got:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// packageSeams returns every identifier a top-level `var` declares across this package's
// non-test files. The blank identifier is excluded: `var _ io.Writer = daemonLogWriter{}` is an
// interface assertion, not a seam, and `_ = …` in a parallel test writes nothing.
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

// parallelSeamWrites reports every assignment to a name in seams made by a test scope — a
// `func TestXxx(t *testing.T)` or a t.Run func literal — that runs under t.Parallel, whether
// the scope calls it itself or inherits it from an enclosing scope. A name the test function
// declares locally (`hardExit := …`, `var hardExit …`, a parameter or range variable) shadows the
// seam and is not counted. Findings are `file:line: <scope> swaps …`, in source order.
func parallelSeamWrites(fset *token.FileSet, file *ast.File, seams map[string]bool) []string {
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
		findings = append(findings, seamWritesInScope(fset, fn.Name.Name, fn.Body, false, seams, shadowed)...)
	}
	return findings
}

// seamWritesInScope walks one test scope's body. Nested t.Run literals open their own scope
// (named parent/child) and inherit parallel; every other func literal (a t.Cleanup closure, a
// helper) is part of the scope that holds it.
func seamWritesInScope(fset *token.FileSet, scope string, body *ast.BlockStmt, parallel bool, seams, shadowed map[string]bool) []string {
	parallel = parallel || callsParallel(body)

	var findings []string
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if lit, name, isRun := runLiteral(n); isRun {
				findings = append(findings, seamWritesInScope(fset, scope+"/"+name, lit.Body, parallel, seams, shadowed)...)
				return false
			}
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
