// Command sessionexpect judges an expect on a saved apogee session: it locates one transcript
// entry by a selector and checks what the entry contains and where it sits against another,
// exiting 0 on a pass and 1 with a one-line detail on a fail. photon's `run:` expects call it to
// judge a take's saved session.
//
//	sessionexpect --entry 'kind=interjected' --contains CHANGELOG
//	sessionexpect --entry 'kind=toolCall,target=CHANGELOG.md' --after 'kind=interjected'
//
// It is a dev tool, not a release asset: `make sessionexpect` builds it, and `make dist` does
// not ship it.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// The exit codes, as cmd/stubllm spells them: a judged fail and a run that could not judge exit
// 1, a command line that never started a run exits 2, so a `run:` expect tells a red take from
// a mistyped one.
const (
	exitRunFailed = 1
	exitBadUsage  = 2
)

// photonWorkEnv names photon's per-clip work dir; the rig's apogee home sits under it, so the
// sessions dir defaults to `$PHOTON_WORK/home/.apogee/sessions`.
const photonWorkEnv = "PHOTON_WORK"

// errExpectFailed is what a judged fail returns. Its detail has already been printed on stdout,
// so main prints nothing more for it.
var errExpectFailed = errors.New("the expect failed")

func main() {
	if err := newRootCommand().ExecuteContext(context.Background()); err != nil {
		if !errors.Is(err, errExpectFailed) {
			fmt.Fprintln(os.Stderr, "sessionexpect:", err)
		}
		os.Exit(exitCodeFor(err))
	}
}

// flagValues are the command line as given, before it is parsed into an Expect.
type flagValues struct {
	entry    string
	contains string
	before   string
	after    string
	sessions string
}

// newRootCommand builds the command. Everything the command line decides — the selectors, the
// clause rule and the sessions dir — is settled in PreRunE, so a bad invocation is a usage
// error (exit 2) and only the judging itself runs inside runE (exit 1 on a failure).
func newRootCommand() *cobra.Command {
	var given flagValues
	var expect Expect
	var sessionsDir string

	cmd := &cobra.Command{
		Use:   "sessionexpect --entry <selector> [flags]",
		Short: "Judge an expect on the newest saved apogee session",
		Long: "sessionexpect locates one transcript entry of the newest session saved under the\n" +
			"sessions dir and judges it: --contains is a substring of its text, tool label, tool\n" +
			"stat or tool summary; --before and --after order it against the entry another\n" +
			"selector locates. Every clause given must hold, and at least one is required.\n\n" +
			"A selector is comma-separated key=value pairs: kind (required, a session entry kind),\n" +
			"text, tool and target (prefix matches), and nth (a positive integer or last), e.g.\n" +
			"  kind=toolCall,tool=Replace,target=task.go,nth=last\n\n" +
			"The detail is printed on stdout. Exit 0 on a pass, 1 on a fail or a session that\n" +
			"could not be read, 2 on a bad command line.",
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			// Cobra checks required flags only after PreRunE: check them first, so a missing
			// --entry is named as such rather than as an empty selector.
			if err := cmd.ValidateRequiredFlags(); err != nil {
				return err
			}
			var err error
			if expect, err = given.expect(); err != nil {
				return err
			}
			sessionsDir, err = given.sessionsDir()
			return err
		},
		RunE: runE(func(cmd *cobra.Command, _ []string) error {
			return judgeNewest(cmd.OutOrStdout(), expect, sessionsDir)
		}),
	}
	flags := cmd.Flags()
	flags.StringVar(&given.entry, "entry", "", "selector of the entry judged (required)")
	flags.StringVar(&given.contains, "contains", "", "substring the entry must contain")
	flags.StringVar(&given.before, "before", "", "selector of an entry the judged one must come before")
	flags.StringVar(&given.after, "after", "", "selector of an entry the judged one must come after")
	flags.StringVar(&given.sessions, "sessions", "", "dir of saved sessions (default $"+photonWorkEnv+"/home/.apogee/sessions)")
	must(cmd.MarkFlagRequired("entry"))
	return cmd
}

// expect parses the selectors and applies the rule that an entry is judged by at least
// one of contains, before or after.
func (given flagValues) expect() (Expect, error) {
	entry, err := parseSelector(given.entry)
	if err != nil {
		return Expect{}, fmt.Errorf("--entry: %w", err)
	}
	expect := Expect{Entry: entry, Contains: given.contains}
	if expect.Before, err = optionalSelector("before", given.before); err != nil {
		return Expect{}, err
	}
	if expect.After, err = optionalSelector("after", given.after); err != nil {
		return Expect{}, err
	}
	if expect.Contains == "" && expect.Before == nil && expect.After == nil {
		return Expect{}, errors.New("--entry: want at least one of --contains, --before or --after with it")
	}
	return expect, nil
}

// optionalSelector parses an ordering flag's selector; an empty one is not given.
func optionalSelector(flag, spelled string) (*EntrySelector, error) {
	if spelled == "" {
		return nil, nil
	}
	selector, err := parseSelector(spelled)
	if err != nil {
		return nil, fmt.Errorf("--%s: %w", flag, err)
	}
	return &selector, nil
}

// sessionsDir is --sessions, else the rig home's sessions dir under $PHOTON_WORK. With neither
// it is an error, never a path relative to the current directory.
func (given flagValues) sessionsDir() (string, error) {
	if given.sessions != "" {
		return given.sessions, nil
	}
	work := os.Getenv(photonWorkEnv)
	if work == "" {
		return "", fmt.Errorf("--sessions: not given and $%s is unset — want one of them", photonWorkEnv)
	}
	return filepath.Join(work, "home", ".apogee", "sessions"), nil
}

// judgeNewest judges expect on the newest session under dir and prints the one-line detail on
// w, returning errExpectFailed on a fail.
func judgeNewest(w io.Writer, expect Expect, dir string) error {
	path, err := newestSession(dir)
	if err != nil {
		return err
	}
	entries, err := loadEntries(path)
	if err != nil {
		return err
	}
	pass, detail := expect.judge(entries)
	if _, err := fmt.Fprintln(w, detail); err != nil {
		return err
	}
	if !pass {
		return errExpectFailed
	}
	return nil
}

// newestSession is the most recently written session JSON in dir: the take's, since the rig's
// reset wipes the sessions dir before every take.
func newestSession(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return "", err
	}
	var newest string
	var newestAt time.Time
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if newest == "" || info.ModTime().After(newestAt) {
			newest, newestAt = path, info.ModTime()
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no session saved under %s", dir)
	}
	return newest, nil
}

// runError marks a failure that happened after the command line was accepted, so exitCodeFor
// can tell a mistyped invocation from a run that started and failed.
type runError struct{ err error }

func (e runError) Error() string { return e.err.Error() }
func (e runError) Unwrap() error { return e.err }

// runE adapts the command's body: usage stops being printed the moment the command line has
// been accepted, and every failure from here on is marked as a run failure rather than a usage
// one. Cobra's own flag errors and PreRunE's never pass through here, which is what makes them
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

// must panics on a Cobra wiring error. The only errors these calls return are "no such flag",
// which is a typo in the lines above and cannot be produced at runtime.
func must(err error) {
	if err != nil {
		panic(err)
	}
}
