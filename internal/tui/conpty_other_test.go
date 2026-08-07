//go:build !windows

package tui

import "testing"

// TestConPTYPaintsTheIntendedFrame is the off-Windows twin of the pseudoconsole regression test in
// conpty_windows_test.go: a skip, and a named one.
//
// It exists so `go test -run ConPTY` says the same thing on every platform — that the check was
// looked for and was not applicable — rather than a silent "no tests to run" that reads the same
// whether the harness is inapplicable or has been deleted. The defect it guards is a Windows
// console-mode defect (see altscreen_windows.go for why it cannot exist anywhere else), and there
// is no pseudoconsole off Windows to host it in.
func TestConPTYPaintsTheIntendedFrame(t *testing.T) {
	t.Skip("the ConPTY harness is Windows-only: the ghosting it guards is a Windows console-mode defect")
}
