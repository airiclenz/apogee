//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

// setRawCommandLine hands raw to CreateProcess verbatim as the child's command line.
//
// Windows takes a single command-line STRING, not an argv, so os/exec joins the argv with
// syscall.EscapeArg — which escapes an embedded double quote as \". cmd.exe does not
// understand that escape: `echo "hi"` arrives as `echo \"hi\"`, and a quoted path with a
// space fails outright with "The filename, directory name, or volume label syntax is
// incorrect". Since a model-written command line quotes routinely, the shell line has to
// bypass the joining entirely, which SysProcAttr.CmdLine is exactly for
// (platform.Shell.CommandLine builds the string; a nil/empty raw leaves cmd untouched).
//
// It only sets the command line: SysProcAttr is shared with the §2.4 teardown and, on a
// confined call, with the Confiner, so the struct is created if absent and never replaced.
func setRawCommandLine(cmd *exec.Cmd, raw string) {
	if raw == "" {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = raw
}
