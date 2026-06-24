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

// Confiner is the prepare-in-place backend shape the harness drives
// (confinement-execution-contract §2.2): report capabilities, and rewrite a *exec.Cmd
// to launch confined to box. It is the P3.4 domain.Confiner signature, named here as a
// local interface so the harness can drive the backends before domain adopts it.
type Confiner interface {
	Capabilities() domain.ConfinementCaps
	Confine(ctx context.Context, box domain.ConfinementBox, cmd *exec.Cmd) error
}

// Probe drives c through the filesystem escape battery (confinement-execution-contract
// §6.2 rows #1–#6) under a box rooted at fresh temp dirs. The caller passes the
// OS-specific backend; the battery and its assertions are identical across backends, so
// "confined" means the same thing on landlock and seatbelt.
//
// When c reports FSWrite==false (e.g. a kernel built without landlock), enforcement
// cannot be exercised on this host, so Probe skips — the backend is still constructed
// and Capabilities is still honest; only the OS-denial assertions are unrunnable. This
// is the standard kernel-feature-gated test idiom; the denials run for real on a
// confinement-capable runner.
func Probe(t *testing.T, c Confiner) {
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
		runWriteProbe(t, c, box, target)
		assertFileExists(t, target)
	})

	t.Run("write_in_writable_path_succeeds", func(t *testing.T) {
		target := filepath.Join(writable, "probe.txt")
		runWriteProbe(t, c, box, target)
		assertFileExists(t, target)
	})

	t.Run("write_outside_box_denied", func(t *testing.T) {
		target := filepath.Join(outside, "escape.txt")
		err := runWriteProbe(t, c, box, target)
		assertDenied(t, err, target)
	})

	t.Run("write_home_ssh_denied", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			t.Skip("no home dir resolvable; skipping ~/.ssh escape probe")
		}
		// A path under $HOME that is outside the box. The write must be OS-denied; it
		// never reaches a real ~/.ssh file (the parent dir need not exist for the
		// landlock/seatbelt denial to fire on the write attempt).
		target := filepath.Join(home, ".ssh", "apogee-confinetest-escape")
		err = runWriteProbe(t, c, box, target)
		assertDenied(t, err, target)
	})

	t.Run("parent_unrestricted_after_confined_child", func(t *testing.T) {
		// The parent (this test process) writes outside the box after confined children
		// have run: it must succeed, proving no per-thread/in-process restriction leaked
		// to the parent (contract §6.2 row #5).
		target := filepath.Join(outside, "parent.txt")
		if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
			t.Fatalf("parent write %q failed, want success (parent must stay unrestricted): %v", target, err)
		}
	})

	t.Run("inherits_domain_across_exec_denied", func(t *testing.T) {
		// The confined child exec's a second program (sh runs another sh) that writes
		// outside the box: the domain must inherit across execve and deny it (contract
		// §6.2 row #6, landlock-specific). runWriteProbe already wraps the write in an
		// inner `sh -c`, so the write itself runs in an exec'd child of the confined sh.
		target := filepath.Join(outside, "inherited-escape.txt")
		err := runNestedWriteProbe(t, c, box, target)
		assertDenied(t, err, target)
	})
}

// ProbeNetwork runs the network arm (confinement-execution-contract §6.2 rows #7–#8). It
// is split from Probe so the fs battery runs on every fs-capable host while the net arm
// runs only where the backend can enforce network egress (NetworkEgress==true).
func ProbeNetwork(t *testing.T, c Confiner) {
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
		err := runConnectProbe(t, c, box, addr)
		if err == nil {
			t.Fatalf("connect to %s succeeded under network-deny box, want OS denial", addr)
		}
	})

	t.Run("connect_allowed_when_network_open", func(t *testing.T) {
		// Default box (empty NetworkAllow) leaves the network open (ADR 0012); the
		// connect must succeed.
		box := domain.ConfinementBox{WorkspaceRoot: ws}
		if err := runConnectProbe(t, c, box, addr); err != nil {
			t.Fatalf("connect to %s failed under network-open box, want success: %v", addr, err)
		}
	})
}

// runWriteProbe builds `sh -c 'printf x > target'`, confines it to box via c, runs it,
// and returns the run error (nil on a successful in-box write, non-nil on an OS-denied
// out-of-box write).
func runWriteProbe(t *testing.T, c Confiner, box domain.ConfinementBox, target string) error {
	t.Helper()
	return runConfined(t, c, box, "printf x > "+shellQuote(target))
}

// runNestedWriteProbe runs the write inside a nested `sh -c`, so the write executes in a
// program exec'd by the confined shell — exercising domain inheritance across execve.
func runNestedWriteProbe(t *testing.T, c Confiner, box domain.ConfinementBox, target string) error {
	t.Helper()
	inner := "printf x > " + shellQuote(target)
	return runConfined(t, c, box, "sh -c "+shellQuote(inner))
}

// runConnectProbe re-execs a tiny dialer inside a confined `sh -c` that uses the host's
// own toolchain-free TCP check. It shells out to the platform's /dev/tcp redirection,
// which sh implements without any external program, so the probe stays hermetic.
func runConnectProbe(t *testing.T, c Confiner, box domain.ConfinementBox, addr string) error {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host/port %q: %v", addr, err)
	}
	// bash/sh /dev/tcp opens a TCP connection; landlock denies connect with EPERM.
	line := "exec 3<>/dev/tcp/" + host + "/" + port
	return runConfinedShell(t, c, box, "bash", line)
}

// runConfined builds `sh -c <line>`, confines and runs it, returning the run error.
func runConfined(t *testing.T, c Confiner, box domain.ConfinementBox, line string) error {
	t.Helper()
	return runConfinedShell(t, c, box, "sh", line)
}

// runConfinedShell builds `<shell> -c <line>`, confines it to box via c, and runs it.
func runConfinedShell(t *testing.T, c Confiner, box domain.ConfinementBox, shell, line string) error {
	t.Helper()
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, shell, "-c", line)
	if err := c.Confine(ctx, box, cmd); err != nil {
		t.Fatalf("Confine(%s -c %q): %v", shell, line, err)
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

// shellQuote wraps s in single quotes for safe inclusion in an sh -c command line,
// escaping any embedded single quotes. Test paths come from t.TempDir, so this is a
// safety belt, not an adversarial quoter.
func shellQuote(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\\', '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	out = append(out, '\'')
	return string(out)
}
