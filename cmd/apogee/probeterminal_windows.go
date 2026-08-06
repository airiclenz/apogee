//go:build windows

package main

import (
	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/windows"
)

// prepareTerminalOutput puts the output handle into exactly the console mode a bubbletea session
// runs it in — ENABLE_VIRTUAL_TERMINAL_PROCESSING so the sequences the probe writes are
// interpreted at all, and DISABLE_NEWLINE_AUTO_RETURN because that flag is precisely what changes
// the console's end-of-line wrapping (it "adds an additional state to end-of-line wrapping that
// can delay the cursor move and buffer scroll"), which is the behaviour the last-column section
// exists to measure.
//
// Matching bubbletea's own initInput (tty_windows.go:36-53) is the requirement, not a convenience:
// a probe that measured a differently-configured console would answer a question nobody asked.
func prepareTerminalOutput(fd uintptr) (func(), error) {
	previous, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}

	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(fd), &mode); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(windows.Handle(fd),
		mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN); err != nil {
		return nil, err
	}
	return func() { _ = term.SetState(fd, previous) }, nil
}
