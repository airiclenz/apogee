//go:build !windows

package tui

// programEnviron is the no-op the Windows terminal-naming rule collapses to on every other host.
// Off Windows the shell sets TERM — that IS the convention there — so the painter already reads
// a real terminal type out of bubbletea's default environment (os.Environ(), tea.go:622), and
// there is nothing to correct. Returning nil is what keeps the [tea.NewProgram] options
// byte-identical to what they have always been off Windows; environ_windows.go carries the rule
// and the reasoning behind it.
func programEnviron() []string { return nil }
