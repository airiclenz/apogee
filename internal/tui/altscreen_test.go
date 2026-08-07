package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"
)

// TestPrepareAltScreenConsoleReturnsACallableRestore pins the one half of this rule a portable test
// can reach: the restore is never nil and is always safe to call.
//
// That is not a formality. [Run] defers the closure unconditionally, before claimAltScreen and so
// before every error return and every panic in the rest of the function — a nil on any path would
// turn a clean shutdown into a nil-dereference panic during unwinding, on the exact paths that
// exist to survive one. The handle here is deliberately not a console, which is the branch a
// non-console handle takes on Windows and the only branch there is anywhere else; both must still
// yield something callable.
//
// What no test in this package can reach is the console path itself — a two-call Win32 sequence on
// a real console handle, whose effect is observable only in the console's own mode word. There is
// no portable seam for it and no seam a `go test` run has a console handle to use. It is pinned by
// measurement instead (plan findings 43-46), each reproducible from the debug harness.
func TestPrepareAltScreenConsoleReturnsACallableRestore(t *testing.T) {
	t.Parallel()

	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() { _ = f.Close() }()

	restore := prepareAltScreenConsole(f)
	if restore == nil {
		t.Fatal("prepareAltScreenConsole returned a nil restore; Run defers it unconditionally")
	}
	restore()
}

// TestClaimTerminalScreenPreparesTheConsoleBeforeTheSwitch pins the ORDER of apogee's terminal
// prologue: the console mode is prepared before the alternate screen is claimed, and not after.
//
// That order is the entire fix (altscreen_windows.go). Both calls are still there when it is
// reversed, both still succeed, and on most hosts nothing looks wrong — which is exactly how the
// bug arrived in the first place, and exactly why the ordering needs a guard of its own rather than
// a comment. Reversing the two lines is a plausible tidy-up for someone who reads the pair as
// independent set-up steps.
//
// It reads the ordering out of the SOURCE, which is unusual and is a considered second choice. The
// first choice is conpty_windows_test.go, which drives claimTerminalScreen inside a real
// pseudoconsole — but the defect the reversal causes is invisible on the pseudoconsole a test host
// can create (the system conhost keeps one mode word for both screen buffers, so a late
// SetConsoleMode still lands on the live one; plan findings 27, 41 and this plan's item 7 NOTES).
// It reproduces only under the console hosts Windows Terminal and VS Code ship, which no test may
// assume is installed. So the behavioural half is measured where it can be measured, and the
// ordering half — the half that has no observable difference on any host a test can build — is
// asserted directly, here.
func TestClaimTerminalScreenPreparesTheConsoleBeforeTheSwitch(t *testing.T) {
	t.Parallel()

	const (
		home    = "tui.go"
		subject = "claimTerminalScreen"
		first   = "prepareAltScreenConsole"
		second  = "claimAltScreen"
	)

	file, err := parser.ParseFile(token.NewFileSet(), home, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", home, err)
	}
	body := funcBody(file, subject)
	if body == nil {
		t.Fatalf("%s no longer declares %s; the prologue this guards has moved and the guard has to move with it",
			home, subject)
	}

	// The calls in source order. A prologue that names each step once is the only shape this
	// assertion is meaningful over, so a duplicate is reported rather than silently taking one.
	var called []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if fn, ok := call.Fun.(*ast.Ident); ok && (fn.Name == first || fn.Name == second) {
			called = append(called, fn.Name)
		}
		return true
	})
	if want := []string{first, second}; len(called) != len(want) || called[0] != want[0] || called[1] != want[1] {
		t.Errorf("%s calls %v, want exactly %v in that order: the console mode word is per screen "+
			"buffer, so a flag set after the switch lands on the buffer nobody paints to and the "+
			"console rewrites every bare LF the renderer emits (see altscreen_windows.go)",
			subject, called, want)
	}
}

// funcBody returns the body of the named top-level function in file, or nil when it declares no
// such function.
func funcBody(file *ast.File, name string) *ast.BlockStmt {
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == name {
			return fn.Body
		}
	}
	return nil
}
