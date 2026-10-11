//go:build !windows

package platform

import "testing"

// TestConsoleOutputCodepageOffWindows pins the pass-through value off Windows, where there is no
// console codepage to read.
func TestConsoleOutputCodepageOffWindows(t *testing.T) {
	t.Parallel()

	got := ConsoleOutputCodepage()

	if got != 65001 {
		t.Fatalf("ConsoleOutputCodepage() = %d, want 65001", got)
	}
}
