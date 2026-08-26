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

// chainedClobberLine returns the multi-command script that reproduces the 2026-08-22
// workspace-clobber incident shape: mkdir/cd into an out-of-box directory, heredoc a
// file there, then an unguarded RELATIVE write that lands wherever the cwd still is
// when the earlier commands were denied. The sleep between the denied chain and the
// clobber write stands in for the incident's own intervening commands: the harness's
// kill-on-denial watch is asynchronous, and the probe asserts it wins within that
// margin (confinetest.Probe carries the full reasoning). ok is always true on POSIX;
// the Windows variant declines — cmd.exe has neither a fail-fast analogue nor
// EPERM-shaped denials for the watch to match.
func chainedClobberLine(sh Shell, outside string) (string, bool) {
	dir := sh.Quote(filepath.Join(outside, "srtest"))
	return "mkdir -p " + dir + " && cd " + dir + " && cat > SConstruct <<'EOF'\n" +
		"env = Environment()\n" +
		"EOF\n" +
		"sleep 2\n" +
		"echo clobbered > inside.txt\n", true
}

// truncateLine returns the shell line that truncates target to zero bytes through the
// coreutils `truncate` program, and whether the platform has it. It is the row-#12 probe: the
// one write-class access landlock cannot fence before ABI 3, spelled as its own program rather
// than as a shell redirect so the syscall under test is unambiguously truncate(2) — a `> file`
// redirect opens with O_TRUNC, which is a different landlock right on some ABIs and would make
// the row assert something other than what it claims.
func truncateLine(sh Shell, target string) (string, bool) {
	if _, err := exec.LookPath("truncate"); err != nil {
		return "", false // macOS ships no coreutils `truncate`; the row skips rather than lying.
	}
	return "truncate -s 0 " + sh.Quote(target), true
}

// setRawCommandLine is a no-op off Windows: execve takes a real argv, so os/exec's joining
// is faithful and syscall.SysProcAttr has no CmdLine field to set.
func setRawCommandLine(_ *exec.Cmd, _ string) {}
