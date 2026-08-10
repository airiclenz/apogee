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
		// The exit status is the failure's own when it carries one — `apogee headless`
		// distinguishes "the run started and failed" (1) from "the run never started" (2) —
		// and 1 for everything else, which is what every command exited with before.
		os.Exit(exitCodeFor(err))
	}
}
