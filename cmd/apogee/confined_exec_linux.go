//go:build linux

package main

import (
	"fmt"
	"os"

	"github.com/airiclenz/apogee/internal/platform"
)

// maybeDispatchConfinedExec intercepts the __confined-exec sentinel before Cobra parses
// the CLI (confinement-execution-contract §2.3 / §2.6). On Linux the landlock backend
// confines a subprocess by re-invoking THIS binary in the hidden helper mode: as a
// separate process it applies the landlock domain to itself and exec's the real argv,
// because Go cannot run code between fork and execve under CGO_ENABLED=0. This is the
// product-binary counterpart of the landlock test's runConfinedExecChild.
//
// argv after the sentinel is [<encoded-box>, "--", <real argv...>]. On success
// ApplyLandlockAndExec replaces the process image and never returns; on any failure this
// prints to stderr and exits non-zero, so a botched confinement fails closed (the command
// does not run unconfined). When the sentinel is absent it returns and the normal CLI runs.
func maybeDispatchConfinedExec() {
	if len(os.Args) < 2 || os.Args[1] != platform.ConfinedExecSentinel() {
		return
	}
	args := os.Args[2:]
	if len(args) < 2 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "apogee: confined-exec: malformed argv")
		os.Exit(2)
	}
	box, err := platform.DecodeConfinedBox(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := platform.ApplyLandlockAndExec(box, args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Unreachable on success: ApplyLandlockAndExec exec's the real argv in place.
}
