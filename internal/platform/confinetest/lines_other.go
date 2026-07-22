//go:build !windows

package confinetest

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// The POSIX spelling of the three things the battery needs that are shell-dialect, not
// platform-facility: how you write a byte to a path, how you nest a shell inside a shell,
// and which path under the user's profile stands for "outside the box but somewhere that
// matters". They live here rather than in platform.Host because they are test-fixture
// knowledge — platform.Host models the shell's INVOCATION, deliberately not its built-ins.

// writeLine returns the shell line that writes one byte to target.
func writeLine(sh Shell, target string) string { return "printf x > " + sh.Quote(target) }

// nestedWriteLine returns a line that runs writeLine inside a nested shell, so the write
// happens in a program the confined shell exec'd.
func nestedWriteLine(sh Shell, target string) string {
	return strings.Join(sh.Command(sh.Quote(writeLine(sh, target))), " ")
}

// userProfileEscapeTarget names the out-of-box path under the user's home that row #4
// attempts: the conventional credential directory on POSIX.
func userProfileEscapeTarget(home string) string {
	return filepath.Join(home, ".ssh", "apogee-confinetest-escape")
}

// setRawCommandLine is a no-op off Windows: execve takes a real argv, so os/exec's joining
// is faithful and syscall.SysProcAttr has no CmdLine field to set.
func setRawCommandLine(_ *exec.Cmd, _ string) {}
