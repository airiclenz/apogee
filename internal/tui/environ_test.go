package tui

import (
	"context"
	"testing"
)

// TestProgramOptionsInstallTheEnvironmentWhenOneIsBuilt: a non-nil environment becomes a
// tea.WithEnvironment option beside the context, which is the whole of the wiring between
// [programEnviron] and the painter. The nil half — the option list unchanged — is already pinned
// by TestProgramOptionsInstallNoTraceWhenTheFlagIsUnset, which passes nil for exactly that
// reason.
func TestProgramOptionsInstallTheEnvironmentWhenOneIsBuilt(t *testing.T) {
	t.Parallel()
	opts, traced, err := programOptions(context.Background(), Options{}, []string{"TERM=xterm-256color"}, false)
	if err != nil {
		t.Fatalf("programOptions: %v", err)
	}
	if traced != nil {
		t.Errorf("a traced output was installed with --tui-trace unset")
	}
	if len(opts) != 2 { // tea.WithContext plus tea.WithEnvironment
		t.Errorf("got %d program options, want 2 (context plus environment)", len(opts))
	}
}
