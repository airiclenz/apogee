//go:build windows

package tools

// syscallKill0 is the Windows stub for the liveness probe used by the process-group teardown
// tests. Those tests skip on Windows at run time (POSIX process groups), so this is never
// called; it exists only so the test file compiles for the windows cross-build.
func syscallKill0(_ int) error { return nil }
