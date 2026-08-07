package tui

import (
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
