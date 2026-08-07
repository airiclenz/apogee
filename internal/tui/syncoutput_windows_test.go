//go:build windows

package tui

import "testing"

// TestProgramDeclinesSyncOutputOnlyOnATerminal: with stdout redirected there is no presentation to
// synchronize and nobody to see a frame tear, so the file gets the bytes bubbletea produced and the
// program keeps the options it has always had. A `go test` run is that case, which is what lets the
// assertion be direct — the terminal branch is the owner's to confirm, exactly as programEnviron's
// is.
func TestProgramDeclinesSyncOutputOnlyOnATerminal(t *testing.T) {
	t.Parallel()
	if stdoutIsTerminal() {
		t.Skip("stdout is a character device in this run; the redirected branch cannot be exercised")
	}
	if programDeclinesSyncOutput() {
		t.Error("programDeclinesSyncOutput() = true with stdout redirected, want false")
	}
}
