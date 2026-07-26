//go:build !windows

package platform

import (
	"os/exec"
	"reflect"
	"syscall"
	"testing"
)

// These tests are hermetic and host-agnostic: the argv wrap and the process-group flag are
// pure preparation of an *exec.Cmd with no process started, so they run on every non-Windows
// host. They pin the shape both POSIX backends now inherit — the landlock re-exec wrapper
// (landlock_linux.go) and the seatbelt sandbox-exec launch (seatbelt.go) — whose own argv
// assertions must keep passing unchanged; that pair is the "the wrapper moved, the argv did
// not" oracle. The prefixes below are spelled literally rather than through
// confinedExecSentinel, which is linux-tagged and does not exist in a !windows file.

func TestWrapArgvUnderLauncher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string   // cmd.Path, as Go's LookPath resolved it ("" => unresolved)
		args     []string // cmd.Args
		launcher string
		prefix   []string
		wantArgs []string
	}{
		{
			// The seatbelt shape: launcher, -p <profile>, then the original argv.
			name:     "resolved_path_is_carried_with_prefix",
			path:     "/bin/echo",
			args:     []string{"echo", "hello", "world"},
			launcher: "/usr/bin/sandbox-exec",
			prefix:   []string{"-p", "(version 1)\n(allow default)\n"},
			wantArgs: []string{"/usr/bin/sandbox-exec", "-p", "(version 1)\n(allow default)\n", "/bin/echo", "hello", "world"},
		},
		{
			// The landlock shape: launcher, sentinel, encoded box, "--", then the original argv.
			name:     "prefix_order_is_preserved",
			path:     "/bin/sh",
			args:     []string{"sh", "-c", "echo hi"},
			launcher: "/path/to/apogee",
			prefix:   []string{"__confined-exec", "eyJ3cyI6MX0=", "--"},
			wantArgs: []string{"/path/to/apogee", "__confined-exec", "eyJ3cyI6MX0=", "--", "/bin/sh", "-c", "echo hi"},
		},
		{
			// Path unset (a hand-built cmd, not one from exec.Command): fall back to Args[0].
			// The wrapped child re-execs with no PATH lookup, so this is the only case where a
			// bare name may travel — and it travels because there is nothing better.
			name:     "args0_fallback_when_path_unset",
			path:     "",
			args:     []string{"sh", "-c", "true"},
			launcher: "/path/to/apogee",
			prefix:   []string{"__confined-exec", "box", "--"},
			wantArgs: []string{"/path/to/apogee", "__confined-exec", "box", "--", "sh", "-c", "true"},
		},
		{
			name:     "no_prefix_wraps_argv_directly",
			path:     "/bin/echo",
			args:     []string{"echo", "hi"},
			launcher: "/opt/launcher",
			prefix:   nil,
			wantArgs: []string{"/opt/launcher", "/bin/echo", "hi"},
		},
		{
			name:     "single_arg_command",
			path:     "/bin/true",
			args:     []string{"true"},
			launcher: "/usr/bin/sandbox-exec",
			prefix:   []string{"-p", "(version 1)"},
			wantArgs: []string{"/usr/bin/sandbox-exec", "-p", "(version 1)", "/bin/true"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			callerArgs := append([]string(nil), tt.args...)
			cmd := &exec.Cmd{Path: tt.path, Args: callerArgs}

			if err := wrapArgvUnderLauncher(cmd, tt.launcher, tt.prefix...); err != nil {
				t.Fatalf("wrapArgvUnderLauncher: %v", err)
			}

			if cmd.Path != tt.launcher {
				t.Errorf("cmd.Path = %q, want launcher %q", cmd.Path, tt.launcher)
			}
			if !reflect.DeepEqual(cmd.Args, tt.wantArgs) {
				t.Errorf("cmd.Args = %q, want %q", cmd.Args, tt.wantArgs)
			}
			// The caller's own argv slice must not be rewritten under it: the wrap builds a new
			// slice, so a caller holding the original still sees the original.
			if !reflect.DeepEqual(callerArgs, tt.args) {
				t.Errorf("caller's argv slice was mutated: got %q, want %q", callerArgs, tt.args)
			}
		})
	}
}

func TestWrapArgvUnderLauncherRejectsEmptyArgv(t *testing.T) {
	t.Parallel()

	// A cmd with no argv has nothing to wrap: rewriting it would make the launcher's own
	// first argument the program. The refusal is deterministic, carries the message both
	// backends report, and leaves cmd untouched — an error means nothing was prepared.
	cmd := &exec.Cmd{}
	err := wrapArgvUnderLauncher(cmd, "/path/to/apogee", "-p", "profile")
	if err == nil {
		t.Fatal("wrapArgvUnderLauncher with empty argv returned nil, want error")
	}
	if got, want := err.Error(), "apogee: confine: cmd has no argv"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if cmd.Path != "" || cmd.Args != nil {
		t.Errorf("cmd was modified on the refusal: Path = %q, Args = %q", cmd.Path, cmd.Args)
	}
}

func TestSetConfinedPgid(t *testing.T) {
	t.Parallel()

	t.Run("fresh_cmd_gains_a_sysprocattr", func(t *testing.T) {
		t.Parallel()

		cmd := exec.Command("/bin/echo", "hi")
		setConfinedPgid(cmd)

		if cmd.SysProcAttr == nil {
			t.Fatal("setConfinedPgid left SysProcAttr nil, want one carrying Setpgid")
		}
		if !cmd.SysProcAttr.Setpgid {
			t.Error("Setpgid = false, want true for process-group teardown (contract §2.4)")
		}
	})

	t.Run("existing_sysprocattr_is_kept", func(t *testing.T) {
		t.Parallel()

		// The non-nil branch: a caller that already set SysProcAttr fields must keep them —
		// only Setpgid is turned on, and the struct is not replaced.
		attr := &syscall.SysProcAttr{}
		cmd := exec.Command("/bin/echo", "hi")
		cmd.SysProcAttr = attr

		setConfinedPgid(cmd)

		if cmd.SysProcAttr != attr {
			t.Error("setConfinedPgid replaced the caller's SysProcAttr instead of setting Setpgid on it")
		}
		if !cmd.SysProcAttr.Setpgid {
			t.Error("Setpgid = false, want true for process-group teardown (contract §2.4)")
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()

		cmd := exec.Command("/bin/echo", "hi")
		setConfinedPgid(cmd)
		first := cmd.SysProcAttr
		setConfinedPgid(cmd)

		if cmd.SysProcAttr != first || !cmd.SysProcAttr.Setpgid {
			t.Error("a second setConfinedPgid must be a no-op on an already-grouped cmd")
		}
	})
}
