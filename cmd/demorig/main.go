// Command demorig is the developer-facing tool behind graphics/demo: it records a clip from its
// storyboard (`graphics/demo/storyboards/<clip>.yaml`), judges the take, and renders the shipped
// GIF from it, so a re-record is a repeatable loop rather than a hand-tuned session.
//
//	demorig lint graphics/demo/storyboards/hero.yaml
//	demorig record graphics/demo/storyboards/hero.yaml [--work <dir>]
//	demorig capture graphics/demo/storyboards/hero.yaml --upstream <url> [--key-env <VAR>] [--work <dir>]
//	demorig check graphics/demo/storyboards/hero.yaml [<take>] [--stage <dir>]
//	demorig render graphics/demo/storyboards/hero.yaml [<take>] [-o out.gif] [--dry-run]
//
// `lint` checks the storyboard against its schema, printing every problem and exiting 1 on any.
// `record` resets the rig's stage, replays the storyboard's cassette as the model, runs apogee in
// a pty through every beat, writes <work>/<clip>.take and checks it; `capture` does the same
// against a live model behind a recording proxy and saves the cassette. Both record on unix only.
// `check` judges a take by the session it saved, the screens it recorded and the stage repo:
// every expect as a PASS/FAIL row, exit 1 on any FAIL. `render` lays the take's beats onto the
// storyboard's section durations, rasterizes and composes every frame — zoom and click cursor
// included — and encodes the GIF through ffmpeg, then gifsicle when it is on PATH. A take
// argument left off defaults to <work>/<clip>.take, the file `record` writes.
//
// It is a dev tool, not a release asset: `make demorig` builds it, and `make dist` does not
// ship it.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// The exit codes. A bad command line is told apart from a run that started and failed, the way
// `apogee headless` and `stubllm` distinguish them, so a script driving this binary can react
// to each.
const (
	exitRunFailed = 1
	exitBadUsage  = 2
)

func main() {
	if err := newRootCommand().ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "demorig:", err)
		os.Exit(exitCodeFor(err))
	}
}

// newRootCommand builds the command tree. Errors are printed by main, not by Cobra, so a
// failure reads the same whichever subcommand produced it; usage still prints for a bad
// command line, because that is the one failure the usage text actually answers.
func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "demorig",
		Short: "Record, judge and render a demo clip from its storyboard",
		Long: "demorig drives graphics/demo from a clip's storyboard. See graphics/demo/README.md,\n" +
			"\"Storyboards\".",
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	cmd.AddCommand(newLintCommand())
	cmd.AddCommand(newCheckCommand())
	cmd.AddCommand(newRenderCommand())
	cmd.AddCommand(newRecordCommand())
	cmd.AddCommand(newCaptureCommand())
	return cmd
}

// newLintCommand validates one storyboard file against its schema.
func newLintCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lint <storyboard.yaml>",
		Short: "Check a storyboard against its schema",
		Args:  cobra.ExactArgs(1),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			board, err := Load(args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: ok — clip %s, %d beats, cassette %s\n",
				board.Path, board.Clip, len(board.Beats), board.Cassette)
			return err
		}),
	}
}

// The rig's work dir: the directory setup.sh builds the rig in and record writes takes to. It
// follows APOGEE_DEMO_WORK and defaults to ~/.cache/apogee-demo, exactly as the rig's scripts
// resolve it.
const (
	workDirEnv     = "APOGEE_DEMO_WORK"
	defaultWorkDir = ".cache/apogee-demo"
)

// workDir resolves the rig's work dir: the flag's value when set, else $APOGEE_DEMO_WORK, else
// ~/.cache/apogee-demo.
func workDir(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if work := os.Getenv(workDirEnv); work != "" {
		return work, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve the rig's work dir: %w", err)
	}
	return filepath.Join(home, defaultWorkDir), nil
}

// takeFile is where a clip's take lives in a work dir: the one path record writes and check
// and render read by default.
func takeFile(work, clip string) string { return filepath.Join(work, clip+".take") }

// takeArg is the take a check or render reads: the argument when one was given, else the clip's
// take in the default work dir.
func takeArg(args []string, clip string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	work, err := workDir("")
	if err != nil {
		return "", err
	}
	return takeFile(work, clip), nil
}

// runError marks a failure that happened after the command line was accepted.
type runError struct{ err error }

func (e runError) Error() string { return e.err.Error() }
func (e runError) Unwrap() error { return e.err }

// runE adapts a subcommand's body: usage stops being printed the moment the command line has
// been accepted, and every failure from here on is marked as a run failure rather than a usage
// one. Cobra's own flag and argument errors never pass through here, which is what makes them
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
