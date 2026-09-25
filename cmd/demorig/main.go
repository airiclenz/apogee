// Command demorig is the developer-facing tool behind graphics/demo: it reads a clip's
// storyboard (`graphics/demo/storyboards/<clip>.yaml`) and turns a raw take into the shipped
// GIF from it, so a re-record is a repeatable loop rather than a hand-tuned ffmpeg session.
//
//	demorig lint graphics/demo/storyboards/hero.yaml
//	demorig beats graphics/demo/storyboards/hero.yaml <take.mp4> <session.json> [--json]
//	demorig check graphics/demo/storyboards/hero.yaml <session.json> [--stage <dir>]
//	demorig render graphics/demo/storyboards/hero.yaml <take.mp4> <session.json> [-o out.gif] [--dry-run]
//	demorig record graphics/demo/storyboards/hero.yaml [--work <dir>]
//	demorig capture graphics/demo/storyboards/hero.yaml --upstream <url> [--key-env <VAR>] [--work <dir>]
//
// `lint` checks the storyboard against its schema and the tape it names, printing every
// problem and exiting 1 on any. `beats` locates each beat in a raw take from the saved
// session's timestamps (ffmpeg and ffprobe on PATH). `check` judges a take by its saved
// session and the stage repo: every expect as a PASS/FAIL row, exit 1 on any FAIL. `render`
// cuts the GIF from the take as the storyboard frames each beat — speed, hold, zoom, cut —
// through one ffmpeg filtergraph, then gifsicle when it is on PATH. `record` resets the rig's
// stage, replays the storyboard's cassette as the model, runs apogee in a pty through every beat,
// writes <work>/<clip>.take and checks it; `capture` does the same against a live model behind a
// recording proxy and saves the cassette. Both record on unix only.
//
// It is a dev tool, not a release asset: `make demorig` builds it, and `make dist` does not
// ship it.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

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
		Short: "Lint a demo storyboard and cut the clip it describes",
		Long: "demorig drives graphics/demo from a clip's storyboard. See graphics/demo/README.md,\n" +
			"\"Storyboards\".",
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	cmd.AddCommand(newLintCommand())
	cmd.AddCommand(newBeatsCommand())
	cmd.AddCommand(newCheckCommand())
	cmd.AddCommand(newRenderCommand(ffmpegTools{}))
	cmd.AddCommand(newRecordCommand())
	cmd.AddCommand(newCaptureCommand())
	return cmd
}

// newLintCommand validates one storyboard file against its schema and its tape.
func newLintCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "lint <storyboard.yaml>",
		Short: "Check a storyboard against its schema and the tape it names",
		Args:  cobra.ExactArgs(1),
		RunE: runE(func(cmd *cobra.Command, args []string) error {
			board, err := Load(args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: ok — clip %s, %d beats, tape %s\n",
				board.Path, board.Clip, len(board.Beats), board.Tape)
			return err
		}),
	}
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
