//go:build !windows

package tui

import "testing"

// TestProgramEnvironInjectsNothingOffWindows: the terminal-naming rule is Windows-only, so
// everywhere else the builder must hand back nil and leave the program options exactly as they
// have always been. TERM is the shell's business on these hosts.
func TestProgramEnvironInjectsNothingOffWindows(t *testing.T) {
	t.Parallel()
	if env := programEnviron(); env != nil {
		t.Errorf("programEnviron() = %v off Windows, want nil", env)
	}
}
