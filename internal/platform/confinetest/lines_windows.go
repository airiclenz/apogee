//go:build windows

package confinetest

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

// The cmd.exe spelling of the three shell-dialect things the battery needs. See
// lines_other.go for why they live here rather than on platform.Host.

// writeLine returns the cmd.exe line that writes one byte to target. `echo x> "<path>"`
// creates the file through CreateFile, which is exactly what the mandatory integrity check
// denies for a Low child outside the box: cmd prints "Access is denied." and exits non-zero,
// so assertDenied's "non-zero exit AND no file" holds unchanged.
func writeLine(sh Shell, target string) string { return "echo x> " + sh.Quote(target) }

// nestedWriteLine returns a line that runs writeLine inside a nested cmd.exe, so the write
// happens in a process the confined shell created — which is how row #6 proves the
// restricted token is inherited by descendants. `cmd /c <line>` takes the remainder of the
// line verbatim, so the inner line is NOT re-quoted.
func nestedWriteLine(sh Shell, target string) string {
	return sh.CommandLine(writeLine(sh, target))
}

// userProfileEscapeTarget names the out-of-box path under the user's profile that row #4
// attempts. os.UserHomeDir already resolves %USERPROFILE%; the target is the profile root
// itself because .ssh is not a meaningful Windows credential path and, more practically, a
// missing parent directory would make cmd fail with "The system cannot find the path
// specified" — a non-zero exit for the wrong reason, which is not the denial being asserted.
func userProfileEscapeTarget(home string) string {
	return filepath.Join(home, "apogee-confinetest-escape")
}

// chainedClobberLine declines on Windows: the chained-script clobber probe reproduces the
// terminal tool's POSIX hardening — the fail-fast preamble (`set -e`), for which cmd.exe
// has no analogue (the terminal tool passes cmd lines through verbatim, see
// platform.FailFastPreamble), and the kill-on-denial watch, whose signatures are the POSIX
// EPERM spellings where cmd.exe prints "Access is denied." — and the script's heredoc has
// no cmd.exe spelling either. The probe skips loudly instead.
func chainedClobberLine(Shell, string) (string, bool) { return "", false }

// truncateLine declines on Windows: row #12 probes coreutils `truncate`, which cmd.exe does not
// have, and the Windows token backend fences by mandatory integrity label rather than by a
// per-access mask, so it has no truncate residual to disclose in the first place.
func truncateLine(Shell, string) (string, bool) { return "", false }

// setRawCommandLine hands raw to CreateProcess verbatim, bypassing os/exec's EscapeArg
// joining, which mangles the redirect's quotes (internal/tools/exec_cmdline_other.go carries
// the full reasoning; this is the same fix for the harness). It only sets the command line:
// SysProcAttr is shared with the Confiner, which appends its Token, so the struct is created
// if absent and never replaced.
func setRawCommandLine(cmd *exec.Cmd, raw string) {
	if raw == "" {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = raw
}
