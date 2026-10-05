//go:build !windows

package subprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// attrRecorder is a platform.ProcessTeardown that snapshots the cmd's SysProcAttr in Contain —
// the first hook after Start, so the snapshot is the attribute set the child was forked with.
// inner, when set, is the real teardown it wraps; nil leaves SysProcAttr exactly as the run
// builds it, which is how a test reaches startConfinedSession with no SysProcAttr at all.
type attrRecorder struct {
	inner platform.ProcessTeardown

	mu    sync.Mutex
	attrs *syscall.SysProcAttr
}

func (r *attrRecorder) Contain(cmd *exec.Cmd) {
	r.mu.Lock()
	if cmd.SysProcAttr != nil {
		snapshot := *cmd.SysProcAttr
		r.attrs = &snapshot
	}
	r.mu.Unlock()
	if r.inner != nil {
		r.inner.Contain(cmd)
	}
}

func (r *attrRecorder) Reap(cmd *exec.Cmd) {
	if r.inner != nil {
		r.inner.Reap(cmd)
	}
}

func (r *attrRecorder) Release() {
	if r.inner != nil {
		r.inner.Release()
	}
}

func (r *attrRecorder) startAttrs() *syscall.SysProcAttr {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attrs
}

// withAttrRecorder rides an attrRecorder on spec's teardown seam. real wraps the platform
// teardown; otherwise the recorder touches nothing and SysProcAttr stays as the run leaves it.
func withAttrRecorder(spec SubprocessSpec, real bool) (SubprocessSpec, *attrRecorder) {
	rec := &attrRecorder{}
	spec.NewTeardown = func(cmd *exec.Cmd) platform.ProcessTeardown {
		if real {
			rec.inner = platform.NewProcessTeardown(cmd)
		}
		return rec
	}
	return spec, rec
}

func confinedCtx(t *testing.T) context.Context {
	t.Helper()
	return domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})
}

// TestRunSubprocessConfinedRunStartsANewSession pins the attribute rule: a confined run is
// forked as a session leader (Setsid) with Setpgid cleared — Go's setpgid on a session leader is
// EPERM — whether SysProcAttr arrived nil or carrying the teardown's Setpgid, while an
// unconfined run keeps the teardown's plain process group.
func TestRunSubprocessConfinedRunStartsANewSession(t *testing.T) {
	t.Parallel()
	base := SubprocessSpec{Argv: []string{"/bin/sh", "-c", "true"}}

	cases := []struct {
		name        string
		confined    bool
		realTD      bool
		wantSetsid  bool
		wantSetpgid bool
	}{
		{name: "confined, nil SysProcAttr", confined: true, wantSetsid: true},
		{name: "confined, teardown's Setpgid", confined: true, realTD: true, wantSetsid: true},
		{name: "unconfined keeps the group", realTD: true, wantSetpgid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tc.confined {
				ctx = confinedCtx(t)
			}
			spec, rec := withAttrRecorder(base, tc.realTD)
			res, err := RunSubprocess(ctx, spec)
			if err != nil {
				t.Fatalf("RunSubprocess err = %v, want nil", err)
			}
			if res.ExitCode != 0 {
				t.Fatalf("exit = %d, output %q; want a clean start and exit", res.ExitCode, res.CombinedOutput)
			}
			attrs := rec.startAttrs()
			if attrs == nil {
				t.Fatal("SysProcAttr was nil at start, want one carrying the session/group rule")
			}
			if attrs.Setsid != tc.wantSetsid || attrs.Setpgid != tc.wantSetpgid {
				t.Errorf("at start Setsid=%v Setpgid=%v, want Setsid=%v Setpgid=%v",
					attrs.Setsid, attrs.Setpgid, tc.wantSetsid, tc.wantSetpgid)
			}
		})
	}
}

// TestRunSubprocessConfinedRunHasNoControllingTerminal is the audit finding's end-to-end proof:
// a confined child that opens /dev/tty fails with ENXIO, so it has no terminal to TIOCSTI
// keystrokes into, while the same command unconfined still opens it — the control that shows
// the test can tell the two apart. It needs a controlling terminal of its own to inherit.
func TestRunSubprocessConfinedRunHasNoControllingTerminal(t *testing.T) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Skipf("the test process has no controlling terminal: %v", err)
	}
	_ = tty.Close()
	t.Parallel()

	spec := SubprocessSpec{Argv: []string{"/bin/sh", "-c", ": </dev/tty && echo opened"}}

	res, err := RunSubprocess(context.Background(), spec)
	if err != nil {
		t.Fatalf("unconfined RunSubprocess err = %v, want nil", err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.CombinedOutput, "opened") {
		t.Fatalf("unconfined run: exit %d, output %q; want /dev/tty opened", res.ExitCode, res.CombinedOutput)
	}

	res, err = RunSubprocess(confinedCtx(t), spec)
	if err != nil {
		t.Fatalf("confined RunSubprocess err = %v, want nil", err)
	}
	if res.ExitCode == 0 || strings.Contains(res.CombinedOutput, "opened") {
		t.Fatalf("confined run: exit %d, output %q; want the /dev/tty open refused", res.ExitCode, res.CombinedOutput)
	}
	if want := syscall.ENXIO.Error(); !strings.Contains(strings.ToLower(res.CombinedOutput), want) {
		t.Errorf("confined run output %q, want the open to fail with ENXIO (%q)", res.CombinedOutput, want)
	}
}

// TestRunSubprocessTimeoutKillsAConfinedGrandchild proves the session upgrade kept the §2.4
// teardown: a confined run's backgrounded grandchild sits in the session leader's group, so the
// negative-PID kill on timeout reaps it with the leader.
func TestRunSubprocessTimeoutKillsAConfinedGrandchild(t *testing.T) {
	t.Parallel()

	spec := SubprocessSpec{
		Argv:    []string{"/bin/sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $!; wait"},
		Timeout: 500 * time.Millisecond,
	}
	res, err := RunSubprocess(confinedCtx(t), spec)
	if err != nil {
		t.Fatalf("confined RunSubprocess err = %v, want nil", err)
	}
	if !res.TimedOut {
		t.Fatalf("TimedOut = false, output %q; want the run to hit its timeout", res.CombinedOutput)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(res.CombinedOutput))
	if err != nil {
		t.Fatalf("grandchild pid from output %q: %v", res.CombinedOutput, err)
	}

	// The killed grandchild is reparented and reaped by init (or a subreaper) asynchronously.
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d still exists after the timeout (kill(0) = %v), want it reaped with the group", pid, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
