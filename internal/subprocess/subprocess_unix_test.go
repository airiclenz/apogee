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
	"github.com/airiclenz/apogee/internal/testtime"
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
	expired := testtime.Expiry(testtime.Budget(5 * time.Second))
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if expired() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d still exists after the timeout (kill(0) = %v), want it reaped with the group", pid, err)
		}
		time.Sleep(testtime.Fixed(20 * time.Millisecond))
	}
}

// TestRunSubprocessNoTerminalStartsANewSession pins the opt-in rule: an UNCONFINED run whose spec
// sets NoTerminal is forked as a session leader (Setsid) with Setpgid cleared, whether
// SysProcAttr arrived nil or carrying the teardown's Setpgid, while the same run without it keeps
// the teardown's plain process group.
func TestRunSubprocessNoTerminalStartsANewSession(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		noTerminal  bool
		realTD      bool
		wantSetsid  bool
		wantSetpgid bool
	}{
		{name: "NoTerminal, nil SysProcAttr", noTerminal: true, wantSetsid: true},
		{name: "NoTerminal, teardown's Setpgid", noTerminal: true, realTD: true, wantSetsid: true},
		{name: "without it keeps the group", realTD: true, wantSetpgid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := SubprocessSpec{Argv: []string{"/bin/sh", "-c", "true"}, NoTerminal: tc.noTerminal}
			spec, rec := withAttrRecorder(base, tc.realTD)
			res, err := RunSubprocess(context.Background(), spec)
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

// TestRunSubprocessNoTerminalChildLeadsItsOwnSession is the end-to-end proof that holds with or
// without a terminal on the test process: a NoTerminal child's session id is its own pid — a new
// session, which no controlling terminal follows into — while the same child without it shares
// the test's session. The ids are read from /proc, so the proof runs where /proc is.
func TestRunSubprocessNoTerminalChildLeadsItsOwnSession(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skipf("no /proc to read a session id from: %v", err)
	}
	t.Parallel()

	for _, noTerminal := range []bool{true, false} {
		spec := SubprocessSpec{Argv: []string{"/bin/sh", "-c", "cat /proc/$$/stat"}, NoTerminal: noTerminal}
		res, err := RunSubprocess(context.Background(), spec)
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("NoTerminal=%v: exit %d, output %q (err %v); want the shell's stat line", noTerminal, res.ExitCode, res.CombinedOutput, err)
		}
		pid, sid := statPidAndSession(t, res.CombinedOutput)
		if leads := pid == sid; leads != noTerminal {
			t.Errorf("NoTerminal=%v: child pid %d, session %d; want it to lead its own session = %v", noTerminal, pid, sid, noTerminal)
		}
	}
}

// statPidAndSession reads the pid (field 1) and the session id (field 6) out of a /proc/<pid>/stat
// line. The command name in field 2 is parenthesised and may hold spaces, so the fields after it
// are counted from its closing parenthesis.
func statPidAndSession(t *testing.T, stat string) (pid, sid int) {
	t.Helper()
	open, closing := strings.IndexByte(stat, '('), strings.LastIndexByte(stat, ')')
	if open < 0 || closing < open {
		t.Fatalf("stat line %q has no parenthesised command name", stat)
	}
	rest := strings.Fields(stat[closing+1:]) // state, ppid, pgrp, session, ...
	if len(rest) < 4 {
		t.Fatalf("stat line %q is too short", stat)
	}
	pid, pidErr := strconv.Atoi(strings.TrimSpace(stat[:open]))
	sid, sidErr := strconv.Atoi(rest[3])
	if pidErr != nil || sidErr != nil {
		t.Fatalf("stat line %q: pid %v, session %v", stat, pidErr, sidErr)
	}
	return pid, sid
}

// TestRunSubprocessNoTerminalRunHasNoControllingTerminal is the /dev/tty half of the proof, run
// where the test process has a terminal to inherit: the NoTerminal child's open fails with ENXIO
// while the same command without it opens /dev/tty — the control that shows the test can tell
// the two apart.
func TestRunSubprocessNoTerminalRunHasNoControllingTerminal(t *testing.T) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Skipf("the test process has no controlling terminal: %v", err)
	}
	_ = tty.Close()
	t.Parallel()

	spec := SubprocessSpec{Argv: []string{"/bin/sh", "-c", ": </dev/tty && echo opened"}}
	res, err := RunSubprocess(context.Background(), spec)
	if err != nil || res.ExitCode != 0 || !strings.Contains(res.CombinedOutput, "opened") {
		t.Fatalf("plain run: exit %d, output %q (err %v); want /dev/tty opened", res.ExitCode, res.CombinedOutput, err)
	}

	spec.NoTerminal = true
	res, err = RunSubprocess(context.Background(), spec)
	if err != nil {
		t.Fatalf("NoTerminal RunSubprocess err = %v, want nil", err)
	}
	if res.ExitCode == 0 || strings.Contains(res.CombinedOutput, "opened") {
		t.Fatalf("NoTerminal run: exit %d, output %q; want the /dev/tty open refused", res.ExitCode, res.CombinedOutput)
	}
	if want := syscall.ENXIO.Error(); !strings.Contains(strings.ToLower(res.CombinedOutput), want) {
		t.Errorf("NoTerminal run output %q, want the open to fail with ENXIO (%q)", res.CombinedOutput, want)
	}
}

// lineSignal is a stdout writer that closes ready once its first full line has arrived, so a test
// can act on what a still-running child printed.
type lineSignal struct {
	mu    sync.Mutex
	buf   strings.Builder
	once  sync.Once
	ready chan struct{}
}

func (w *lineSignal) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if strings.Contains(w.buf.String(), "\n") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func (w *lineSignal) firstLine() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	line, _, _ := strings.Cut(w.buf.String(), "\n")
	return line
}

// TestRunSubprocessCancelKillsANoTerminalGrandchild proves the session start kept the §2.4
// teardown on an unconfined run: a NoTerminal child's backgrounded grandchild sits in the
// session leader's group, so the negative-PID kill a cancelled ctx fires reaps it with the leader.
func TestRunSubprocessCancelKillsANoTerminalGrandchild(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spec := SubprocessSpec{
		Argv:       []string{"/bin/sh", "-c", "sleep 30 >/dev/null 2>&1 & echo $!; wait"},
		Timeout:    testtime.Budget(60 * time.Second),
		NoTerminal: true,
	}
	out := &lineSignal{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := RunSubprocessTo(ctx, spec, out)
		done <- err
	}()

	select {
	case <-out.ready:
	case err := <-done:
		t.Fatalf("the run ended (err %v) before printing the grandchild's pid", err)
	case <-time.After(testtime.Budget(10 * time.Second)):
		t.Fatal("the child printed no grandchild pid in time")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out.firstLine()))
	if err != nil {
		t.Fatalf("grandchild pid from output %q: %v", out.firstLine(), err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(testtime.Budget(10 * time.Second)):
		t.Fatal("the cancelled run did not return")
	}

	// The killed grandchild is reparented and reaped by init (or a subreaper) asynchronously.
	expired := testtime.Expiry(testtime.Budget(5 * time.Second))
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if expired() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d still exists after the cancel (kill(0) = %v), want it reaped with the group", pid, err)
		}
		time.Sleep(testtime.Fixed(20 * time.Millisecond))
	}
}
