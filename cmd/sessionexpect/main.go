// Command sessionexpect judges an expect on a saved apogee session: it locates one transcript
// entry by a selector and checks what the entry contains and where it sits against another,
// exiting 0 on a pass and 1 with a one-line detail on a fail. It is the session half of
// cmd/demorig's `check`, carried over for photon's `run:` expects.
//
// It is a dev tool, not a release asset.
package main

import (
	"fmt"
	"os"
)

// exitBadUsage is the exit status of a command line this binary cannot run.
const exitBadUsage = 2

func main() {
	fmt.Fprintln(os.Stderr, "usage: sessionexpect --entry <selector> [--contains <s>] [--before <selector>] [--after <selector>] [--sessions <dir>]")
	os.Exit(exitBadUsage)
}
