package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// fakeTeardown is a platform.ProcessTeardown that records its own lifecycle and nothing else, so
// the ownership rule — runSubprocess releases the teardown on EVERY exit path, including the two
// that never reach Wait, and reaps the tree on the one that completes — is provable on every OS
// instead of only where a Job Object exists.
type fakeTeardown struct {
	mu        sync.Mutex
	contained int
	reaped    int
	released  int
}

func (t *fakeTeardown) Contain(*exec.Cmd) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.contained++
}

func (t *fakeTeardown) Reap(*exec.Cmd) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reaped++
}

func (t *fakeTeardown) Release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.released++
}

func (t *fakeTeardown) counts() (contained, reaped, released int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.contained, t.reaped, t.released
}

// installFakeTeardown substitutes the platform teardown constructor with one handing out td
// for the duration of the test, restoring the real one afterwards. Tests using it must not run
// in parallel — the seam is a package var.
func installFakeTeardown(t *testing.T) *fakeTeardown {
	t.Helper()
	td := &fakeTeardown{}
	prev := newProcessTeardown
	newProcessTeardown = func(*exec.Cmd) platform.ProcessTeardown { return td }
	t.Cleanup(func() { newProcessTeardown = prev })
	return td
}

// TestRunSubprocessReleasesTheTeardownOnEveryExitPath pins the handle-ownership rule: the
// teardown is built before the command is confined and before it is started, so the two routine
// early exits — a Confine refusal and a Start failure — must release it just as the normal path
// does. Exactly once each: the count also proves the release is in one place, not two. The reap
// counts ride along, pinning the other half — a tree is reaped only where one can exist.
func TestRunSubprocessReleasesTheTeardownOnEveryExitPath(t *testing.T) {
	t.Run("a confine refusal releases the handle it never used", func(t *testing.T) {
		td := installFakeTeardown(t)
		ctx := domain.WithConfinement(context.Background(), domain.Confinement{
			Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true},
			Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
		})

		_, err := runSubprocess(ctx, subprocessSpec{argv: []string{os.Args[0], "-test.list=^$"}})
		if !errors.Is(err, domain.ErrConfinementUnavailable) {
			t.Fatalf("runSubprocess err = %v, want ErrConfinementUnavailable", err)
		}
		contained, reaped, released := td.counts()
		if contained != 0 || reaped != 0 {
			t.Errorf("contain called %d times and reap %d, want 0 and 0 — the process never started", contained, reaped)
		}
		if released != 1 {
			t.Errorf("release called %d times, want 1 — the confine-failure path must not leak the handle", released)
		}
	})

	t.Run("a start failure releases the handle", func(t *testing.T) {
		td := installFakeTeardown(t)
		missing := filepath.Join(t.TempDir(), "no-such-binary")

		res, err := runSubprocess(context.Background(), subprocessSpec{argv: []string{missing}})
		if err != nil {
			t.Fatalf("runSubprocess err = %v, want nil (a failed start is a result, not a Go error)", err)
		}
		if res.exitCode != -1 {
			t.Errorf("exitCode = %d, want -1 for a process that never started", res.exitCode)
		}
		contained, reaped, released := td.counts()
		if contained != 0 || reaped != 0 {
			t.Errorf("contain called %d times and reap %d, want 0 and 0 — the process never started", contained, reaped)
		}
		if released != 1 {
			t.Errorf("release called %d times, want 1 — the start-failure path must not leak the handle", released)
		}
	})

	t.Run("a clean run contains then releases exactly once", func(t *testing.T) {
		td := installFakeTeardown(t)

		// The test binary itself is the one executable every host is guaranteed to have;
		// -test.list with a regexp matching nothing prints nothing and exits 0.
		res, err := runSubprocess(context.Background(), subprocessSpec{argv: []string{os.Args[0], "-test.list=^$"}})
		if err != nil {
			t.Fatalf("runSubprocess err = %v, want nil", err)
		}
		if res.exitCode != 0 {
			t.Fatalf("exitCode = %d, want 0 (output %q)", res.exitCode, res.combinedOutput)
		}
		contained, reaped, released := td.counts()
		if contained != 1 {
			t.Errorf("contain called %d times, want 1", contained)
		}
		if reaped != 1 {
			t.Errorf("reap called %d times, want 1 — a completed run must reap its tree, not only a cancelled one", reaped)
		}
		if released != 1 {
			t.Errorf("release called %d times, want 1 — two releases would mean two owners", released)
		}
	})
}
