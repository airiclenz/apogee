// Command release is apogee's release maintainer program (ADR 0098): it does the deterministic
// half of cutting a release — building the archives, and the verbs later items add — the same way
// on Linux, macOS and Windows, leaving the judgment half (release notes, the version to cut) to
// the committed release skills.
//
//	go run ./cmd/release dist [--src DIR] [--out DIR]
//	go run ./cmd/release status [vX.Y.Z] [--json]
//	go run ./cmd/release prepare vX.Y.Z
//	go run ./cmd/release publish vX.Y.Z --notes-file F [--dry-run] [--tag-timeout D]
//
// Every external command it runs goes through one runner seam (runner.go), so its tests never
// reach the network, a real `git` or a real toolchain.
//
// It is a dev tool, never a release asset: `make dist` does not ship it.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// The exit codes. A bad command line is told apart from a run that started and failed, so a
// script or skill driving this program can react to each.
const (
	exitRunFailed = 1
	exitBadUsage  = 2
)

func main() {
	if err := newRootCommand(execRunner{}).ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(exitCodeFor(err))
	}
}

// newRootCommand builds the command tree over commands, the runner every verb executes its
// external commands through. Errors are printed by main, not by Cobra, so a failure reads the
// same whichever verb produced it; usage still prints for a bad command line, because that is the
// one failure the usage text actually answers. The root declares no Args, so Cobra's own check
// refuses an unknown verb instead of printing help and exiting 0.
func newRootCommand(commands runner) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Build and publish an apogee release",
		Long: "release does the deterministic work of an apogee release (ADR 0098).\n" +
			"See docs/adr/0098-the-repo-owns-its-release-pipeline.md.",
		SilenceErrors: true,
	}
	cmd.AddCommand(newDistCommand(commands))
	cmd.AddCommand(newStatusCommand(commands))
	cmd.AddCommand(newPrepareCommand(commands))
	cmd.AddCommand(newPublishCommand(commands))
	return cmd
}

// runError marks a failure that happened after the command line was accepted, so exitCodeFor
// can tell a mistyped invocation from a run that started and failed.
type runError struct{ err error }

func (e runError) Error() string { return e.err.Error() }
func (e runError) Unwrap() error { return e.err }

// runE adapts a verb's body: usage stops being printed the moment the command line has been
// accepted, and every failure from here on is marked as a run failure rather than a usage one.
// Cobra's own flag and argument errors never pass through here, which is what makes them
// distinguishable.
func runE(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		if err := run(cmd, args); err != nil {
			return runError{err: err}
		}
		return nil
	}
}

// exitCodeFor reports the status an error asks for: a run that started and failed exits 1, a
// command line that never started one exits 2 with its usage printed.
func exitCodeFor(err error) int {
	var failed runError
	if errors.As(err, &failed) {
		return exitRunFailed
	}
	return exitBadUsage
}
