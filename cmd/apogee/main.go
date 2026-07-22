// Command apogee is the terminal coding agent for small local LLMs.
//
// It is the product binary and the composition root (phase-2 detail plan §3 C5): it
// resolves the state roots (no implicit ~/.apogee in the library — ADR 0001 / C7),
// builds a Config, constructs the Agent through the public apogee package (dogfooding
// the shipped surface), and hands it to the internal/tui renderer. The root command
// launches the TUI; the subcommands it also carries are assembled here, from the
// subcommands() registration seam (empty until `apogee probe` lands — `headless` stays
// deferred, phase-2 detail plan §6).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/airiclenz/apogee/internal/tui"
)

func main() {
	// Intercept the __confined-exec sentinel before Cobra: on Linux this binary re-invokes
	// itself in the landlock helper mode to confine a subprocess (confinement-execution-
	// contract §2.3 / §2.6). The normal CLI never surfaces the sentinel; off Linux this is a
	// no-op. This MUST stay the first thing main does — the sentinel is not a Cobra command
	// and no subcommand may be reachable before it, whatever the tree grows into.
	maybeDispatchConfinedExec()

	cmd := newRootCommand(tui.Run, subcommands()...)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
