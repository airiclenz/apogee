package confinetest

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// Shell is the platform shell surface the battery drives its probes through:
// platform.Host satisfies it, and the caller passes platform.Current() alongside the
// backend under test. It is redeclared here rather than imported because
// internal/platform's own _test.go files are IN package platform, so importing the
// platform package from this one would be an import cycle — and because a harness that
// asks for exactly the three methods it uses can be handed anything shell-shaped.
//
// Taking the shell from the caller is what makes the battery run natively on Windows: the
// probes used to hard-code `sh -c`, which does not exist on a stock Windows host
// (confinement-execution-contract §9.3).
type Shell interface {
	// Command returns the argv that runs a line through the platform shell.
	Command(line string) []string
	// CommandLine returns the verbatim process command line that argv must be launched
	// with, or "" where the platform's own argv joining is faithful (POSIX).
	CommandLine(line string) string
	// Quote returns an argument quoted for the platform shell.
	Quote(arg string) string
}

// Probe drives c through the filesystem escape battery (confinement-execution-contract
// §6.2 rows #1–#6) under a box rooted at fresh temp dirs. The caller passes the OS-specific
// backend (any domain.Confiner) and the platform shell (platform.Current()); the battery and
// its assertions are identical across backends, so "confined" means the same thing on
// landlock, seatbelt and the Windows token backend.
//
// When c reports FSWrite==false (e.g. a kernel built without landlock) enforcement cannot be
// exercised on this host, so Probe skips — the backend is still constructed and Capabilities
// is still honest; only the OS-denial assertions are unrunnable. This is the standard
// capability-gated test idiom; the denials run for real on a confinement-capable runner.
func Probe(t *testing.T, c domain.Confiner, sh Shell) {
	t.Helper()
	if !c.Capabilities().FSWrite {
		t.Skip("confinetest: backend reports FSWrite==false (confinement unenforceable on this host); skipping enforcement battery")
	}

	ws := t.TempDir()
	writable := t.TempDir()
	outside := t.TempDir()
	box := domain.ConfinementBox{
		WorkspaceRoot: ws,
		WritablePaths: []string{writable},
	}

	t.Run("write_in_workspace_succeeds", func(t *testing.T) {
		target := filepath.Join(ws, "probe.txt")
		runWriteProbe(t, c, sh, box, target)
		assertFileExists(t, target)
	})

	t.Run("write_in_writable_path_succeeds", func(t *testing.T) {
		target := filepath.Join(writable, "probe.txt")
		runWriteProbe(t, c, sh, box, target)
		assertFileExists(t, target)
	})

	t.Run("write_outside_box_denied", func(t *testing.T) {
		target := filepath.Join(outside, "escape.txt")
		err := runWriteProbe(t, c, sh, box, target)
		assertDenied(t, err, target)
	})

	t.Run("write_under_user_profile_denied", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			t.Skip("no home dir resolvable; skipping user-profile escape probe")
		}
		// Row #4's claim is "a path under the user's profile, outside the box" — a
		// credential-adjacent location every OS has, spelled ~/.ssh on POSIX and the
		// profile root itself on Windows (where .ssh is not a meaningful credential path,
		// and where a missing parent directory would make the shell fail for the wrong
		// reason instead of being OS-denied). The write must be OS-denied; it never
		// reaches a real file.
		target := userProfileEscapeTarget(home)
		err = runWriteProbe(t, c, sh, box, target)
		assertDenied(t, err, target)
	})

	t.Run("parent_unrestricted_after_confined_child", func(t *testing.T) {
		// The parent (this test process) writes outside the box after confined children
		// have run: it must succeed, proving no per-thread/in-process restriction leaked
		// to the parent (contract §6.2 row #5). It is free on Windows — the restricted
		// token is a copy and the parent's own token is never touched — and asserted
		// anyway, because "free by construction" is what a regression quietly stops being.
		target := filepath.Join(outside, "parent.txt")
		if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
			t.Fatalf("parent write %q failed, want success (parent must stay unrestricted): %v", target, err)
		}
	})

	t.Run("inherits_domain_across_exec_denied", func(t *testing.T) {
		// The confined child execs a second program (a nested shell) that writes outside
		// the box: the restriction must survive that and deny it (contract §6.2 row #6).
		// On Linux it proves a landlock domain survives execve; on Windows it proves a
		// descendant created by the confined child inherits the restricted token — exactly
		// as load-bearing a claim, and exactly as unproven until asserted (ADR 0020 §7).
		target := filepath.Join(outside, "inherited-escape.txt")
		err := runNestedWriteProbe(t, c, sh, box, target)
		assertDenied(t, err, target)
	})
}

// ProbeNetwork runs the network arm (confinement-execution-contract §6.2 rows #7–#8). It
// is split from Probe so the fs battery runs on every fs-capable host while the net arm
// runs only where the backend can enforce network egress (NetworkEgress==true) — which
// excludes Windows by construction (ADR 0020 §4), so the positive control #8 goes unproven
// there, acceptable because nothing is being enforced.
func ProbeNetwork(t *testing.T, c domain.Confiner, sh Shell) {
	t.Helper()
	if !c.Capabilities().NetworkEgress {
		t.Skip("confinetest: backend reports NetworkEgress==false; skipping network battery")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go acceptAll(ln)
	addr := ln.Addr().String()

	ws := t.TempDir()

	t.Run("connect_denied_when_network_deny", func(t *testing.T) {
		// NetworkAllow non-empty opts the box into network-deny (the tightening); a
		// non-allowlisted connect must be OS-denied.
		box := domain.ConfinementBox{WorkspaceRoot: ws, NetworkAllow: []string{"example.invalid:443"}}
		err := runConnectProbe(t, c, sh, box, addr)
		if err == nil {
			t.Fatalf("connect to %s succeeded under network-deny box, want OS denial", addr)
		}
	})

	t.Run("connect_allowed_when_network_open", func(t *testing.T) {
		// Default box (empty NetworkAllow) leaves the network open (ADR 0012); the
		// connect must succeed.
		box := domain.ConfinementBox{WorkspaceRoot: ws}
		if err := runConnectProbe(t, c, sh, box, addr); err != nil {
			t.Fatalf("connect to %s failed under network-open box, want success: %v", addr, err)
		}
	})
}

// runWriteProbe builds the platform's "write one byte to target" shell line, confines it to
// box via c, runs it, and returns the run error (nil on a successful in-box write, non-nil
// on an OS-denied out-of-box write).
func runWriteProbe(t *testing.T, c domain.Confiner, sh Shell, box domain.ConfinementBox, target string) error {
	t.Helper()
	return runConfined(t, c, sh, box, writeLine(sh, target))
}

// runNestedWriteProbe runs the write inside a nested shell, so the write executes in a
// program exec'd by the confined shell — exercising inheritance across exec.
func runNestedWriteProbe(t *testing.T, c domain.Confiner, sh Shell, box domain.ConfinementBox, target string) error {
	t.Helper()
	return runConfined(t, c, sh, box, nestedWriteLine(sh, target))
}

// runConnectProbe drives a TCP connect from inside the box using the shell's own facility,
// so the probe stays hermetic. It is POSIX-only in practice: bash's /dev/tcp redirection has
// no cmd.exe counterpart, and ProbeNetwork never reaches here on a backend that reports
// NetworkEgress==false, which every Windows host does.
func runConnectProbe(t *testing.T, c domain.Confiner, sh Shell, box domain.ConfinementBox, addr string) error {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host/port %q: %v", addr, err)
	}
	// bash/sh /dev/tcp opens a TCP connection; landlock denies connect with EPERM.
	line := "exec 3<>/dev/tcp/" + host + "/" + port
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "bash", "-c", line)
	if err := c.Confine(ctx, box, cmd); err != nil {
		t.Fatalf("Confine(bash -c %q): %v", line, err)
	}
	return cmd.Run()
}

// runConfined runs line through the platform shell, confined to box by c, and returns the
// run error. On Windows the argv must additionally be launched with the shell's verbatim
// command line: os/exec joins argv with syscall.EscapeArg, which escapes the redirect's
// quotes in a form cmd.exe does not understand, and every probe here redirects into a
// quoted path (platform.Shell.CommandLine exists for exactly this).
func runConfined(t *testing.T, c domain.Confiner, sh Shell, box domain.ConfinementBox, line string) error {
	t.Helper()
	ctx := context.Background()
	argv := sh.Command(line)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	setRawCommandLine(cmd, sh.CommandLine(line))
	if err := c.Confine(ctx, box, cmd); err != nil {
		t.Fatalf("Confine(%v): %v", argv, err)
	}
	return cmd.Run()
}

// assertDenied fails the test unless err is a non-nil run error and target was not
// created (the OS blocked the write). A nil error or a present file means the escape
// succeeded — a confinement failure.
func assertDenied(t *testing.T, err error, target string) {
	t.Helper()
	if err == nil {
		t.Fatalf("write to %q succeeded, want OS denial (escape not blocked)", target)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatalf("write to %q produced a file despite error %v; escape not blocked", target, err)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat %q: %v", target, statErr)
	}
}

// assertFileExists fails the test unless target exists (the in-box write landed).
func assertFileExists(t *testing.T, target string) {
	t.Helper()
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected in-box write %q to exist: %v", target, err)
	}
}

// acceptAll accepts and immediately closes connections so the network probe's dial
// completes (or is denied before reaching the listener).
func acceptAll(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		conn.Close()
	}
}
