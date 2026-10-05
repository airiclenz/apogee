package platform

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// landlockSourceFile is the file ApplyLandlockAndExec lives in. The guard parses it rather than
// compiling it, so it runs on every OS — the landlock backend itself builds only on Linux.
const landlockSourceFile = "landlock_linux.go"

// TestApplyLandlockAndExecLocksItsThreadFirst pins the audit fix "Landlock helper does not pin
// its OS thread": PR_SET_NO_NEW_PRIVS and landlock_restrict_self bind to the calling thread
// only, so ApplyLandlockAndExec must lock its goroutine to the OS thread before anything else
// and never unlock it, or the thread that execs may not be the thread that was restricted.
func TestApplyLandlockAndExecLocksItsThreadFirst(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, landlockSourceFile, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", landlockSourceFile, err)
	}
	if violation := threadLockViolation(file); violation != "" {
		t.Errorf("%s: %s", landlockSourceFile, violation)
	}
}

// ...and the detector proven on fixtures rather than assumed: the lock as the first statement
// passes, and a late lock, a missing lock, an unlock (deferred or not), an aliased or missing
// runtime import and a missing function each fail.
func TestApplyLandlockAndExecLocksItsThreadFirstBites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		wantErr bool
	}{
		{
			name: "lock first",
			source: `package platform
import "runtime"
func ApplyLandlockAndExec() error {
	runtime.LockOSThread()
	return nil
}`,
		},
		{
			name: "lock second",
			source: `package platform
import "runtime"
func ApplyLandlockAndExec(argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	runtime.LockOSThread()
	return nil
}`,
			wantErr: true,
		},
		{
			name: "no lock",
			source: `package platform
import "runtime"
func ApplyLandlockAndExec() error {
	runtime.Gosched()
	return nil
}`,
			wantErr: true,
		},
		{
			name: "deferred unlock",
			source: `package platform
import "runtime"
func ApplyLandlockAndExec() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return nil
}`,
			wantErr: true,
		},
		{
			name: "unlock inside a closure",
			source: `package platform
import "runtime"
func ApplyLandlockAndExec() error {
	runtime.LockOSThread()
	func() { runtime.UnlockOSThread() }()
	return nil
}`,
			wantErr: true,
		},
		{
			name: "aliased runtime import",
			source: `package platform
import rt "runtime"
func ApplyLandlockAndExec() error {
	rt.LockOSThread()
	return nil
}`,
			wantErr: true,
		},
		{
			name: "method of the same name",
			source: `package platform
import "runtime"
type c struct{}
func (c) ApplyLandlockAndExec() error {
	runtime.LockOSThread()
	return nil
}`,
			wantErr: true,
		},
		{
			name: "empty body",
			source: `package platform
import "runtime"
var _ = runtime.GOOS
func ApplyLandlockAndExec() error`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", tc.source, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			violation := threadLockViolation(file)
			if gotErr := violation != ""; gotErr != tc.wantErr {
				t.Errorf("threadLockViolation() = %q, want violation: %v", violation, tc.wantErr)
			}
		})
	}
}

// threadLockViolation reports why file's package-level ApplyLandlockAndExec does not open with
// runtime.LockOSThread() and keep the lock, or "" when it does. It requires the plain
// `import "runtime"` so the selector it matches is unambiguous.
func threadLockViolation(file *ast.File) string {
	if !importsRuntimeUnaliased(file) {
		return `does not import "runtime" under its own name`
	}
	fn := packageFunc(file, "ApplyLandlockAndExec")
	if fn == nil {
		return "no package-level ApplyLandlockAndExec function"
	}
	if fn.Body == nil || len(fn.Body.List) == 0 {
		return "ApplyLandlockAndExec has no body"
	}
	if !isRuntimeCall(fn.Body.List[0], "LockOSThread") {
		return "the first statement of ApplyLandlockAndExec is not runtime.LockOSThread()"
	}
	unlocked := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && isRuntimeSelector(sel, "UnlockOSThread") {
			unlocked = true
		}
		return !unlocked
	})
	if unlocked {
		return "ApplyLandlockAndExec calls runtime.UnlockOSThread — the lock must outlive the exec"
	}
	return ""
}

// importsRuntimeUnaliased reports whether file imports "runtime" without a rename.
func importsRuntimeUnaliased(file *ast.File) bool {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err == nil && path == "runtime" && spec.Name == nil {
			return true
		}
	}
	return false
}

// packageFunc returns file's package-level (receiver-less) function named name, or nil.
func packageFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// isRuntimeCall reports whether stmt is the bare, argument-less call runtime.<name>().
func isRuntimeCall(stmt ast.Stmt, name string) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && isRuntimeSelector(sel, name)
}

// isRuntimeSelector reports whether sel is runtime.<name>.
func isRuntimeSelector(sel *ast.SelectorExpr, name string) bool {
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "runtime" && sel.Sel.Name == name
}
