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
)

// TestPlanTreeKill covers the one §2.4 teardown decision both backends share, on every OS:
// what a cancelled run must kill. The syscalls differ per platform; this table does not.
func TestPlanTreeKill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		started  bool
		treeHeld bool
		want     treeKillAction
	}{
		{
			name:    "a process that never started is not killed at all",
			started: false, treeHeld: false,
			want: treeKillNothing,
		},
		{
			name:    "a container held before the process started is still nothing to kill",
			started: false, treeHeld: true,
			want: treeKillNothing,
		},
		{
			name:    "a held tree is reaped whole — the contract's intent",
			started: true, treeHeld: true,
			want: treeKillTree,
		},
		{
			name:    "an unheld tree degrades to the leader rather than nothing",
			started: true, treeHeld: false,
			want: treeKillLeader,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := planTreeKill(tt.started, tt.treeHeld); got != tt.want {
				t.Errorf("planTreeKill(%v, %v) = %v, want %v", tt.started, tt.treeHeld, got, tt.want)
			}
		})
	}
}

// TestNoTeardownIsInert pins the POSIX implementation: the process group needs no post-start
// step and owns no handle, so both hooks must be safe to call — including with a cmd that
// never started.
func TestNoTeardownIsInert(t *testing.T) {
	t.Parallel()

	var td processTeardown = noTeardown{}
	td.contain(nil)
	td.release()
	td.release()
}

// fakeTeardown is a processTeardown that records its own lifecycle and nothing else, so the
// ownership rule — runSubprocess releases the teardown on EVERY exit path, including the two
// that never reach Wait — is provable on every OS instead of only where a Job Object exists.
type fakeTeardown struct {
	mu        sync.Mutex
	contained int
	released  int
}

func (t *fakeTeardown) contain(*exec.Cmd) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.contained++
}

func (t *fakeTeardown) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.released++
}

func (t *fakeTeardown) counts() (contained, released int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.contained, t.released
}

// installFakeTeardown substitutes the platform teardown constructor with one handing out td
// for the duration of the test, restoring the real one afterwards. Tests using it must not run
// in parallel — the seam is a package var.
func installFakeTeardown(t *testing.T) *fakeTeardown {
	t.Helper()
	td := &fakeTeardown{}
	prev := newProcessTeardown
	newProcessTeardown = func(*exec.Cmd) processTeardown { return td }
	t.Cleanup(func() { newProcessTeardown = prev })
	return td
}

// TestRunSubprocessReleasesTheTeardownOnEveryExitPath pins the handle-ownership rule: the
// teardown is built before the command is confined and before it is started, so the two routine
// early exits — a Confine refusal and a Start failure — must release it just as the normal path
// does. Exactly once each: the count also proves the release is in one place, not two.
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
		contained, released := td.counts()
		if contained != 0 {
			t.Errorf("contain called %d times, want 0 — the process never started", contained)
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
		contained, released := td.counts()
		if contained != 0 {
			t.Errorf("contain called %d times, want 0 — the process never started", contained)
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
		contained, released := td.counts()
		if contained != 1 {
			t.Errorf("contain called %d times, want 1", contained)
		}
		if released != 1 {
			t.Errorf("release called %d times, want 1 — two releases would mean two owners", released)
		}
	})
}
