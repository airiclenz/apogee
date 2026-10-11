package auditsplit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/airiclenz/apogee/internal/gitexec"
)

// The command's argv: the option that selects the flags form, and the bounds of the split form.
const (
	flagsOption  = "--flags"
	flagsArgs    = 2
	minSplitArgs = 2
	maxSplitArgs = 4
)

// The command's exit codes: split.sh's, which exited 1 on every failure, a malformed argv included.
const (
	exitSucceeded = 0
	exitFailed    = 1
)

// The usage of each form, the refusal a malformed argv prints as its summary.
var (
	errSplitUsage = errors.New("usage: apogee audit-split <RUN> <SCOPE> [FOCUS] [PART_BYTES]")
	errFlagsUsage = errors.New("usage: apogee audit-split --flags <RUN>")
)

// Main is the `apogee audit-split` command over args, the words after the command's name, run with
// the working directory as the workspace. `<RUN> <SCOPE> [FOCUS] [PART_BYTES]` splits as Split
// does — PART_BYTES in digits, anything else an unknown budget — and `--flags <RUN>` answers as
// Flags does. The receipt goes to stdout, a failure's `summary=audit-split: <cause>` line
// included; the failure's cause also goes to stderr as one line. It returns the process exit
// code: 0, or 1 on any failure.
//
// It is the one entry every caller shares: the cobra command of the shipped binary and the test
// binary that stands in for apogee in a recipe's script stage.
func Main(args []string, stdout, stderr io.Writer) int {
	err := runCommand(args, stdout)
	if err == nil {
		return exitSucceeded
	}
	// stderr is the last place a failure can be told; one that cannot be written there has
	// nowhere left to go, and the exit code still carries it.
	_, _ = fmt.Fprintln(stderr, err)
	return exitFailed
}

// runCommand dispatches args to the form it names and returns the failure Main reports.
func runCommand(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == flagsOption {
		if len(args) != flagsArgs {
			return fail(stdout, errFlagsUsage)
		}
		return Flags(args[1], stdout)
	}
	if len(args) < minSplitArgs || len(args) > maxSplitArgs {
		return fail(stdout, errSplitUsage)
	}
	workspace, err := os.Getwd()
	if err != nil {
		return fail(stdout, fmt.Errorf("locate the workspace: %w", err))
	}
	return split(context.Background(), splitRequest{
		plan: planRequest{
			root:      workspace,
			scope:     args[1],
			partBytes: argAt(args, 3),
			knobs:     KnobsFromEnv(os.Getenv),
			git:       gitexec.OS(),
		},
		run:   args[0],
		focus: argAt(args, 2),
	}, stdout)
}

// argAt is the optional argument at index, or empty when args stops short of it.
func argAt(args []string, index int) string {
	if index < len(args) {
		return args[index]
	}
	return ""
}
