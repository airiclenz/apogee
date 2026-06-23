//go:build windows

package platform

// windowsHost is the Phase-0 Windows stub for Host (plan §P0.5). The values are
// the correct trivial ones so the package builds and is forward-usable, but the
// real Windows shell/path behaviour is validated in Phase 5 (Windows fast-
// follow — plan §6 #3); until then it ships unexercised.
//
// TODO(phase-5): validate on a real Windows target.
type windowsHost struct{}

// Command wraps line in `cmd /c`, the Windows shell invocation.
func (windowsHost) Command(line string) []string { return []string{"cmd", "/c", line} }

// ExecExt is ".exe" on Windows.
func (windowsHost) ExecExt() string { return ".exe" }

// Current returns the platform Host for this Windows build target.
func Current() Host { return windowsHost{} }

var _ Host = windowsHost{}
