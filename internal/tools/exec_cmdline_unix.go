//go:build !windows

package tools

import "os/exec"

// setRawCommandLine is a no-op on POSIX: execve takes a real argv, so the argv
// platform.Shell.Command returns reaches the shell exactly as built and Shell.CommandLine
// hands back "" here. The seam exists because Windows has no argv at the syscall boundary
// (exec_cmdline_other.go).
func setRawCommandLine(_ *exec.Cmd, _ string) {}
