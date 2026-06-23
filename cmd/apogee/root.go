package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/tui"
)

// options holds the parsed root-command flags. It is a plain value bound to the
// cobra flag set so the wiring functions (resolveRoots, buildAgent, runRoot) are
// testable without a live command.
type options struct {
	endpoint  string
	model     string
	mode      string
	workspace string
	bypass    bool
	resume    string
	configDir string
}

// launcher starts the interactive UI over the constructed engine. It is injected so
// tests can assert clean construction and a clean quit without a real terminal; main
// passes tui.Run. The seam also keeps cmd from depending on Bubble Tea directly.
type launcher func(ctx context.Context, eng tui.Engine, opts tui.Options) error

// newRootCommand builds the apogee root command. The root launches the TUI; it carries
// the minimal, reviewable flag set (phase-2 detail plan P2.0). Subcommands (headless,
// probe) are deferred (§6) — the tree shape leaves room for them.
func newRootCommand(launch launcher) *cobra.Command {
	var opts options

	cmd := &cobra.Command{
		Use:   "apogee",
		Short: "Terminal coding agent for small local LLMs",
		Long: "apogee is a terminal coding agent for small local LLMs. The root command\n" +
			"opens an interactive session against a local OpenAI-compatible model:\n" +
			"hold a coding conversation, watch tools run, and approve writes.",
		Args: cobra.NoArgs,
		// On a runtime (RunE) error, print just the error — not the full usage dump,
		// which is noise for a misconfiguration rather than a syntax mistake. main owns
		// printing and the non-zero exit.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoot(cmd.Context(), opts, launch)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.endpoint, "endpoint", "", "OpenAI-compatible LLM server URL")
	flags.StringVar(&opts.model, "model", "", "model name to request")
	flags.StringVar(&opts.mode, "mode", string(modeAskBefore),
		"autonomy: plan | ask-before (auto requires Confinement, lands in Phase 3)")
	flags.StringVar(&opts.workspace, "workspace", "",
		"workspace root the file tools are scoped to (default: current directory)")
	flags.BoolVar(&opts.bypass, "bypass", false,
		"run with Mechanisms off; structural context reducers stay on (ADR 0006)")
	flags.StringVar(&opts.resume, "resume", "", "resume a saved session file")
	flags.StringVar(&opts.configDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	return cmd
}
