// Command apogee is the terminal coding agent for small local LLMs.
//
// This is a Phase-0 scaffold entrypoint: it exists so the module builds and
// `apogee --help` runs (a plan Phase-0 deliverable). The real command surface —
// the Bubble Tea TUI and the run / probe / headless subcommands on a Cobra
// command tree — lands in P0.3, once the CLI dependencies are pinned
// (see docs/plans/implementation-plan-apogee-merge.md, §3a).
package main

import (
	"fmt"
	"os"
)

const usage = `apogee — terminal coding agent for small local LLMs

Usage:
  apogee [command]

Phase-0 scaffold build: the command surface (the Bubble Tea TUI and the
run / probe / headless subcommands) is not wired up yet. The Cobra command
tree lands in P0.3 — see docs/plans/implementation-plan-apogee-merge.md.

Flags:
  -h, --help   show this help and exit
`

func main() {
	// Phase-0 scaffold: there are no subcommands yet, so every invocation
	// prints usage. -h/--help are accepted explicitly and exit 0; an
	// unrecognised argument prints usage to stderr and exits non-zero.
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-h", "--help", "help":
			fmt.Print(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "apogee: unknown argument %q\n\n%s", arg, usage)
			os.Exit(2)
		}
	}
	fmt.Print(usage)
}
