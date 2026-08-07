//go:build !windows

package tui

import "testing"

// TestProgramNeverDeclinesSyncOutputOffWindows: the mode-2026 filter answers a ConPTY defect and
// must be invisible everywhere there is no ConPTY. Off Windows the mode is honoured, the frame
// really is presented atomically, and declining it would buy the tearing it exists to prevent — so
// the wrapper is never installed and tea.NewProgram's options are unchanged.
func TestProgramNeverDeclinesSyncOutputOffWindows(t *testing.T) {
	t.Parallel()
	if programDeclinesSyncOutput() {
		t.Error("programDeclinesSyncOutput() = true off Windows, want false")
	}
}
