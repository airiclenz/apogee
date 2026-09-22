package tools

import (
	"context"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/subprocess"
)

// capturedRunHost returns the real host with its run replaced by a recorder: the spec a tool
// hands it lands in the returned pointer and nothing is launched, so a test pins the exact argv
// and environment the tool builds on every platform. The host is a value the test owns, so the
// test can run in parallel with every other reader of the real operating system.
func capturedRunHost(t *testing.T) (execHost, *subprocess.SubprocessSpec) {
	t.Helper()
	h := defaultExecHost()
	var captured subprocess.SubprocessSpec
	h.run = func(_ context.Context, spec subprocess.SubprocessSpec) (subprocess.SubprocessResult, error) {
		captured = spec
		return subprocess.SubprocessResult{}, nil
	}
	return h, &captured
}

// fakeLook is a PATH lookup that answers path for EVERY name (found), or exec.LookPath's
// not-found error for every name (!found). It fakes the LOOK alone, never the fence
// security.ResolveProgram applies to what the look answers — a planted path still gets refused.
// A test that needs a per-name answer sets the host's look itself; the field is a func.
func fakeLook(found bool, path string) func(string) (string, error) {
	return func(string) (string, error) {
		if !found {
			return "", exec.ErrNotFound
		}
		return path, nil
	}
}

// fakeLookHost returns the real host with its look replaced by fakeLook(found, path), so a tool
// resolves its program without depending on the test host's PATH.
func fakeLookHost(found bool, path string) execHost {
	h := defaultExecHost()
	h.look = fakeLook(found, path)
	return h
}

// markerHost is a platform.Host a test can recognise by identity: two execHost values cannot be
// compared (func fields, and the real Host's rule table carries a slice), but an interface
// holding a POINTER compares by that pointer, so the tools' shells are compared instead.
type markerHost struct{ platform.Host }

// TestBuiltinToolsShareOneExecHost pins the reason execHost exists: builtinTools builds ONE host
// and every tool that takes one — the five execution tools, the six git tools and the two
// git-staging file operations — launches and resolves through that same value, so a test — or
// later a Driver — that supplies a host has supplied it to all thirteen, not to whichever tools
// happened to take it.
func TestBuiltinToolsShareOneExecHost(t *testing.T) {
	t.Parallel()
	marker := &markerHost{Host: platform.Current()}
	h := defaultExecHost()
	h.shell = marker

	shells := map[string]platform.Host{}
	for _, tool := range builtinToolsWith(t.TempDir(), HostTools{}, h) {
		switch typed := tool.(type) {
		case *Terminal:
			shells[tool.Name()] = typed.host.shell
		case *PythonExec:
			shells[tool.Name()] = typed.host.shell
		case *RunTests:
			shells[tool.Name()] = typed.host.shell
		case *Diagnostics:
			shells[tool.Name()] = typed.host.shell
		case *ConsoleOpen:
			shells[tool.Name()] = typed.host.shell
		case *GitBranch:
			shells[tool.Name()] = typed.host.shell
		case *GitCommit:
			shells[tool.Name()] = typed.host.shell
		case *GitDiffRange:
			shells[tool.Name()] = typed.host.shell
		case *GitStatus:
			shells[tool.Name()] = typed.host.shell
		case *GitLog:
			shells[tool.Name()] = typed.host.shell
		case *GitShow:
			shells[tool.Name()] = typed.host.shell
		case *MoveFile:
			shells[tool.Name()] = typed.host.shell
		case *DeleteFile:
			shells[tool.Name()] = typed.host.shell
		}
	}

	if len(shells) != 13 {
		t.Fatalf("found %d host-holding tools in the build, want 13: %v", len(shells), shells)
	}
	for name, shell := range shells {
		if shell != platform.Host(marker) {
			t.Errorf("%s launches through a shell other than the one host builtinTools was handed", name)
		}
	}
}

// TestNoPackageLevelExecSeam pins that execHost is the ONLY way the operating system reaches
// an execution tool: no top-level `var` in this package's non-test files holds a function or a
// platform.Host. Those were the shapes the retired per-tool seams had — a var initialised from
// exec.LookPath, one initialised from runSubprocess, one declared with the named gitexec.LookFunc,
// one holding the platform Host — and a syntactic guard cannot see them: none carries a
// *ast.FuncType, and the named LookFunc is not a func literal either. So the guard judges the
// TYPE, through go/types with the source importer, and looks at Type().Underlying(), so a
// Signature under a named type counts too. A seam that reappears is reported at its file:line
// with its type.
func TestNoPackageLevelExecSeam(t *testing.T) {
	t.Parallel()

	findings := packageExecSeams(t, nil)

	for _, finding := range findings {
		t.Error(finding)
	}
}

// ...and the detector proven on a fixture rather than assumed: the three retired shapes, added
// to the package as one more file, are each reported — the func-typed var, the var inferred from
// a package func, and the one declared with the NAMED gitexec.LookFunc — and the package's real
// vars (specs, tables, sentinel errors, the interface assertions) stay silent.
func TestNoPackageLevelExecSeamBites(t *testing.T) {
	t.Parallel()

	const fixture = `package tools

import (
	"os/exec"

	"github.com/airiclenz/apogee/internal/gitexec"
	"github.com/airiclenz/apogee/internal/platform"
)

var lookX = exec.LookPath

var runX = runSubprocess

var lookY gitexec.LookFunc = exec.LookPath

var shellZ platform.Host = platform.Current()

var notASeam = 3
`

	got := packageExecSeams(t, map[string]string{"fixture.go": fixture})

	want := []string{
		"fixture.go:10: lookX is a package-level exec seam of type func(file string) (string, error) — hand the tool an execHost instead",
		"fixture.go:12: runX is a package-level exec seam of type func(ctx context.Context, spec github.com/airiclenz/apogee/internal/subprocess.SubprocessSpec) (github.com/airiclenz/apogee/internal/subprocess.SubprocessResult, error) — hand the tool an execHost instead",
		"fixture.go:14: lookY is a package-level exec seam of type github.com/airiclenz/apogee/internal/gitexec.LookFunc — hand the tool an execHost instead",
		"fixture.go:16: shellZ is a package-level exec seam of type github.com/airiclenz/apogee/internal/platform.Host — hand the tool an execHost instead",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("detector findings differ\n got:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// packageExecSeams type-checks this package's non-test files — plus extra, fixture sources keyed
// by file name, type-checked as members of the package — and reports every package-level var
// whose underlying type is a function signature or whose type is platform.Host, sorted by
// position. The package's own funcs are in scope, so a fixture may refer to runSubprocess as the
// retired seams did. A type-check error fails the test: a guard that skipped what it could not
// type would pass the very shape it exists to catch.
func packageExecSeams(t *testing.T, extra map[string]string) []string {
	t.Helper()

	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range packageNonTestGoFiles(t) {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, file)
	}
	extraNames := make([]string, 0, len(extra))
	for name := range extra {
		extraNames = append(extraNames, name)
	}
	sort.Strings(extraNames)
	for _, name := range extraNames {
		file, err := parser.ParseFile(fset, name, extra[name], 0)
		if err != nil {
			t.Fatalf("parse fixture %s: %v", name, err)
		}
		files = append(files, file)
	}

	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := conf.Check("github.com/airiclenz/apogee/internal/tools", fset, files, nil)
	if err != nil {
		t.Fatalf("type-check internal/tools: %v", err)
	}
	if pkg.Scope().Lookup("defaultExecHost") == nil {
		t.Fatal("package scope lacks defaultExecHost — the scan of the non-test files read the wrong package")
	}

	var seams []*types.Var
	for _, name := range pkg.Scope().Names() {
		if v, isVar := pkg.Scope().Lookup(name).(*types.Var); isVar && isExecSeamType(v.Type()) {
			seams = append(seams, v)
		}
	}
	sort.Slice(seams, func(i, j int) bool { return seams[i].Pos() < seams[j].Pos() })

	findings := make([]string, 0, len(seams))
	for _, v := range seams {
		pos := fset.Position(v.Pos())
		findings = append(findings, fmt.Sprintf("%s:%d: %s is a package-level exec seam of type %s — hand the tool an execHost instead",
			filepath.Base(pos.Filename), pos.Line, v.Name(), v.Type()))
	}
	return findings
}

// isExecSeamType reports whether typ is one of the shapes a retired exec seam had: any function
// signature, named or not (Underlying sees through gitexec.LookFunc), or the platform.Host
// interface itself.
func isExecSeamType(typ types.Type) bool {
	if _, isFunc := typ.Underlying().(*types.Signature); isFunc {
		return true
	}
	named, isNamed := typ.(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "github.com/airiclenz/apogee/internal/platform" && named.Obj().Name() == "Host"
}

// packageNonTestGoFiles lists this package directory's non-test Go files, sorted so findings and
// type-check errors come out in a stable order.
func packageNonTestGoFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return paths
}
