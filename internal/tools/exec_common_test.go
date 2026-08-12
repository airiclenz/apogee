package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestRunSubprocessNilConfinerFailsClosed pins the §2.2 posture on the one handle shape the
// confine guard used to wave through: a Confinement installed with no Confiner behind it. That
// is broken wiring, not permission to run free — it must surface as ErrConfinementUnavailable,
// which dispatch turns into the truthful demote to Approval, and the command must never run.
func TestRunSubprocessNilConfinerFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the guard it pins is platform-independent")
	}
	t.Parallel()

	// The canary is a file the command would create: its absence is the proof that nothing
	// ran, which an error alone cannot give.
	canary := filepath.Join(t.TempDir(), "ran")
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: nil,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := runSubprocess(ctx, subprocessSpec{
		argv: []string{"/bin/sh", "-c", fmt.Sprintf("touch %s", strconv.Quote(canary))},
	})
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("runSubprocess err = %v, want ErrConfinementUnavailable (a handle with no Confiner must fail closed)", err)
	}
	if _, statErr := os.Stat(canary); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("stat %s = %v, want not-exist — the command must not have run unconfined", canary, statErr)
	}
}
