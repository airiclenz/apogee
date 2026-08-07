//go:build !windows

package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestProgramEnvironInjectsNothingOffWindows: the terminal-naming rule is Windows-only, so
// everywhere else the builder must hand back nil and leave the program options exactly as they
// have always been. TERM is the shell's business on these hosts.
func TestProgramEnvironInjectsNothingOffWindows(t *testing.T) {
	t.Parallel()
	if env := programEnviron(); env != nil {
		t.Errorf("programEnviron() = %v off Windows, want nil", env)
	}
}

// TestDiagLogReportsThePlainEnvironmentOffWindows is the other side of the same contract: nothing
// is injected here, so nothing may be reported as injected. The log stays the plain "key: value"
// it has always been, and it is fed [programEnviron]'s real answer rather than a literal nil so
// that a rule which ever started injecting on this host would be caught by this test.
func TestDiagLogReportsThePlainEnvironmentOffWindows(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "diag.txt")
	diag, err := newDiagLog(path)
	if err != nil {
		t.Fatalf("newDiagLog: %v", err)
	}
	process := map[string]string{"TERM": "xterm-ghostty", "COLORTERM": "truecolor"}

	diag.start(func(key string) string { return process[key] }, programEnviron(), ansi.WcWidth)
	if err := diag.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readFileString(t, path)
	for _, want := range []string{"TERM: xterm-ghostty", "COLORTERM: truecolor", "WT_SESSION: (unset)"} {
		if !strings.Contains(got, want) {
			t.Errorf("diag log is missing %q; log was:\n%s", want, got)
		}
	}
	if strings.Contains(got, "injected") {
		t.Errorf("the log claims a value was injected on a host that injects nothing; log was:\n%s", got)
	}
}
