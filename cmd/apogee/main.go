// Command apogee is the terminal coding agent for small local LLMs.
//
// It is the product binary and the composition root (phase-2 detail plan §3 C5): it
// resolves the state roots (no implicit ~/.apogee in the library — ADR 0001 / C7),
// builds a Config, constructs the Agent through the public apogee package (dogfooding
// the shipped surface), and hands it to the internal/tui renderer. The root command
// launches the TUI; subcommands (headless, probe) slot in later (phase-2 detail plan
// §6).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/airiclenz/apogee/internal/tui"
)

func main() {
	cmd := newRootCommand(tui.Run)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
