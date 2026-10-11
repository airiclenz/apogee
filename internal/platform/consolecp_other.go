//go:build !windows

package platform

// ConsoleOutputCodepage returns 65001 (UTF-8) on every host but Windows: a POSIX terminal has no
// console codepage, and 65001 is the value DecodeConsoleOutput passes through unchanged.
func ConsoleOutputCodepage() uint32 {
	return codepageUTF8
}
