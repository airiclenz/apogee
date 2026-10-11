package main

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/auditsplit"
)

// auditSplitFailed is the message of a failure whose diagnostic line came back empty, so main
// never prints a blank line in its place.
const auditSplitFailed = "audit-split: failed"

// newAuditSplitCommand builds `apogee audit-split`, the shipped `audit` recipe's scope split in Go:
// `apogee audit-split <RUN> <SCOPE> [FOCUS] [PART_BYTES]` and `apogee audit-split --flags <RUN>`,
// run in the working directory, the receipt on stdout. It is a thin shell over [auditsplit.Main]
// and takes no config, no wiring and no network.
//
// It is hidden: the recipe's script stages are its only intended caller, so it is not a public
// contract and `apogee --help` does not list it. Flag parsing is off, so `--flags` and any scope
// word that starts with a dash reach the entry as written. main owns the error print and the exit
// code, so the entry's stderr line is carried back as the error, with the entry's exit code.
func newAuditSplitCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "audit-split <RUN> <SCOPE> [FOCUS] [PART_BYTES] | --flags <RUN>",
		Short:              "Split an audit scope into parts (the audit recipe's script stage)",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var diagnostics strings.Builder
			code := auditsplit.Main(args, cmd.OutOrStdout(), &diagnostics)
			if code == 0 {
				return nil
			}
			message := strings.TrimSpace(diagnostics.String())
			if message == "" {
				message = auditSplitFailed
			}
			return exitError{code: code, err: errors.New(message)}
		},
	}
}
