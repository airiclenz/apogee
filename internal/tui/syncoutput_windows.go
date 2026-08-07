//go:build windows

package tui

// programDeclinesSyncOutput reports whether [Run] should filter bubbletea's mode-2026 question out
// of the program's output (syncoutput.go carries the measurement and the whole argument).
//
// On Windows it does, on a real terminal. The terminal gate is programEnviron's, for the same
// reason: with stdout redirected there is no presentation to synchronize and no frame for anyone to
// see tear, so the honest thing is to hand the file exactly the bytes bubbletea produced. It also
// keeps every piped-stdout run — `go test`, a shell pipeline, headless mode — on precisely the
// tea.NewProgram options it has always had.
func programDeclinesSyncOutput() bool { return stdoutIsTerminal() }
