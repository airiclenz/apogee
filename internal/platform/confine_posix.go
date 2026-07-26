//go:build !windows

package platform

import (
	"errors"
	"os/exec"
	"syscall"
)

// POSIX confinement plumbing — the argv wrap both POSIX backends share (ADR 0012;
// confinement-execution-contract §2.2/§2.3/§2.4).
//
// landlock (landlock_linux.go) and seatbelt (seatbelt.go) fence by the same mechanism:
// neither runs the command, both rewrite it to run under a launcher — the apogee binary
// itself in its __confined-exec helper mode on Linux, /usr/bin/sandbox-exec on macOS — and
// both put the wrapped child in its own process group. Only the launcher and the arguments
// between it and the original argv differ, so the rewrite itself lives here once and each
// backend keeps only the part that is genuinely its own (the ruleset, the profile, the
// capability probe).
//
// This file is //go:build !windows because that is the only tag both backends compile
// under: landlock is linux-only, and seatbelt is !windows so its profile generator stays
// hermetically testable on the Linux dev host. It is deliberately NOT platform_posix.go —
// that file is the Host rule set (shell, quoting, path semantics), a different concern.
// Windows fences with a restricted low-integrity token instead and wraps nothing (ADR 0020;
// contract §9.2), so there is no Windows counterpart to share with.

// errNoArgv is the refusal for a command with no argv: there is nothing to wrap, and
// rewriting it would produce a launch line whose program is the launcher's own first
// argument. It is one value so the message reads identically wherever it surfaces —
// wrapArgvUnderLauncher returns it, and each backend checks it first so the argv refusal
// precedes that backend's host probe (see landlockConfiner.Confine, seatbeltConfiner.Confine).
var errNoArgv = errors.New("apogee: confine: cmd has no argv")

// wrapArgvUnderLauncher rewrites cmd to run under launcher, with prefix inserted between the
// launcher and the original argv. It prepares in place and does NOT run cmd
// (confinement-execution-contract §2.2); the caller's Stdin/Stdout/Stderr/Dir/Env stay as they
// are and are inherited by the launcher, which execs the real child.
//
// The rewritten argv carries the RESOLVED program path (cmd.Path), not the bare cmd.Args[0],
// falling back to Args[0] only when Path is unset: the wrapped child re-execs with no PATH
// lookup — syscall.Exec in the landlock helper, sandbox-exec's own exec on macOS — so a bare
// "sh" would fail with ENOENT (contract §2.3).
//
// The caller's own argv slice is left untouched, and on the empty-argv refusal cmd is not
// modified at all: a backend that returns an error has prepared nothing.
func wrapArgvUnderLauncher(cmd *exec.Cmd, launcher string, prefix ...string) error {
	if len(cmd.Args) == 0 {
		return errNoArgv
	}

	prog := cmd.Path
	if prog == "" {
		prog = cmd.Args[0]
	}

	args := make([]string, 0, 1+len(prefix)+len(cmd.Args))
	args = append(args, launcher)
	args = append(args, prefix...)
	args = append(args, prog)
	args = append(args, cmd.Args[1:]...)

	cmd.Path = launcher
	cmd.Args = args
	return nil
}

// setConfinedPgid puts the wrapped child and its descendants in one process group so the
// execution tool's negative-PID kill reaps the whole group (confinement-execution-contract
// §2.4); the tool sets cmd.Cancel/WaitDelay (P3.8). An existing SysProcAttr is kept rather
// than replaced — the caller may have set other fields — and only Setpgid is turned on.
func setConfinedPgid(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
