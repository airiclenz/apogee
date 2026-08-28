package main

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tui"
)

// launcher starts the interactive UI over the constructed engine. It carries the Bridge
// whose Sink/Approver were installed in the Agent's Config, so the launcher can bind it to
// the running program (resolving the construction chicken-and-egg — phase-2 detail plan §3
// C2/C3). It is injected so tests can assert clean construction and a clean quit without a
// real terminal; main passes tui.Run.
type launcher func(ctx context.Context, eng tui.Engine, br *tui.Bridge, opts tui.Options) error

// newRootCommand builds the apogee root command. The root launches the TUI; it carries
// the minimal, reviewable flag set (phase-2 detail plan P2.0).
//
// subs are the subcommands to register — main passes the shipped set (subcommands()),
// tests pass fakes. Registering children never changes the bare invocation: the root
// keeps its own RunE and `Args: cobra.NoArgs`, so `apogee` with no arguments opens the
// TUI exactly as before and an unrecognised word still fails as an unknown command.
func newRootCommand(launch launcher, subs ...*cobra.Command) *cobra.Command {
	var opts config.Options

	cmd := &cobra.Command{
		Use:   "apogee",
		Short: "Terminal coding agent for small local LLMs",
		// Cobra auto-adds the --version flag and its print template from this field,
		// reading the single source of truth (the embedded top-level VERSION file, via
		// apogee.Version). The in-TUI /version command reads the same full value via
		// Options.Version; the start-up box shows the shorter Options.BaseVersion instead.
		Version: apogee.Version(),
		Long: "apogee is a terminal coding agent for small local LLMs. The root command\n" +
			"opens an interactive session against a local OpenAI-compatible model:\n" +
			"hold a coding conversation, watch tools run, and approve writes.\n\n" +
			"The servers: list in ~/.apogee/config.yaml defines the servers apogee can\n" +
			"talk to, and the session starts on the one server: names. Config keys resolve\n" +
			"by precedence: a flag overrides an APOGEE_* environment variable\n" +
			"(APOGEE_SERVER, APOGEE_MODE, APOGEE_BYPASS), which overrides the config file,\n" +
			"which overrides the built-in default -- so --server or APOGEE_SERVER starts\n" +
			"this run on another listed server. --endpoint / APOGEE_ENDPOINT are not\n" +
			"config keys: they start the run on an UNLISTED server, which is never saved.\n" +
			"APOGEE_API_KEY and --model / APOGEE_MODEL carry the bearer token and the model\n" +
			"hint for that unlisted server, or overlay those two fields of the listed one.\n" +
			"APOGEE_API_KEY has no flag on purpose: a secret on the command line lands in\n" +
			"shell history and in process lists. With no model set anywhere, apogee asks\n" +
			"the server for its active model, so a single-model server (e.g. llama.cpp's\n" +
			"llama-server) needs no model named at all. The session is saved continuously\n" +
			"under ~/.apogee/sessions; resume the most recent with --continue, or browse\n" +
			"and pick one with /sessions inside apogee.",
		Args: cobra.NoArgs,
		// On a runtime (RunE) error, print just the error — not the full usage dump,
		// which is noise for a misconfiguration rather than a syntax mistake. main owns
		// printing and the non-zero exit.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			changed := cmd.Flags().Changed
			// First run: drop a documented starter config so the file is discoverable.
			// Best-effort — a run still works off flags/env/defaults if the home is
			// unwritable — and only the first run creates it (an existing config is never
			// overwritten). The notice prints before the alt-screen, on stderr.
			if created, path, err := config.SeedDefaultConfig(opts, changed, os.Getenv); err != nil {
				cmd.PrintErrln("apogee: could not create default config:", err)
			} else if created {
				cmd.PrintErrln("apogee: created a starter config at", path)
			}
			// Resolve the upstream/autonomy settings by precedence (flag > env > file >
			// default) before construction; the flag set tells us which flags were
			// explicitly set so an unset flag's default never shadows a lower layer.
			// A soft notice from resolution (a malformed unconfined-hosts entry) prints here,
			// on stderr before the alt-screen, like the seeding line above.
			if err := config.ApplyConfig(&opts, changed, os.Getenv, os.ReadFile, func(msg string) { cmd.PrintErrln(msg) }); err != nil {
				// The one refusal this Driver answers instead of printing: resolution could not say
				// which server to start on. The TUI can ASK — it has a picker, and a human in front
				// of it — so it starts pre-bound and gets its engine from the answer (ADR 0036
				// decisions 3 and 8). Every other error, and every other Driver, stops here.
				var undetermined *config.StartupUndetermined
				if !errors.As(err, &undetermined) {
					return err
				}
				opts.Prebound = undetermined.Start
			}
			// Nothing asks the server anything here. Startup used to stall for up to five seconds
			// on a discovery probe — and hard-fail when no model was configured and the server was
			// down — which made a coding tool unusable exactly when the server needed attention.
			// The TUI now paints immediately and the heartbeat fires its first beat from Init, so
			// discovery is late and continuous rather than early and once (ADR 0024, decision 8):
			// the same beat that late-seeds a cold start refreshes a running one.
			return runRoot(cmd.Context(), opts, launch)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Endpoint, "endpoint", "",
		"OpenAI-compatible LLM server URL to start on, unlisted and not saved (overrides --server)")
	flags.StringVar(&opts.StartupServer, "server", "",
		"name of the servers: entry to start on (default: the last one /server switched to)")
	// The claim beside the registration above: this command has the flag, so its startup refusal
	// may offer it as the fix. The commands that do not register it say APOGEE_SERVER instead.
	opts.ServerFlagBound = true
	flags.StringVar(&opts.Model, "model", "",
		"model name to request (default: the startup server's model:, else ask the server)")
	flags.StringVar(&opts.Mode, "mode", string(domain.ModeAskBefore),
		"autonomy ladder: plan | ask-before | allow-edits | auto "+
			"(auto needs filesystem confinement; tuned by confine-to-workspace in config.yaml)")
	flags.StringVar(&opts.Workspace, "workspace", "",
		"workspace root the file tools are scoped to (default: current directory)")
	flags.BoolVar(&opts.Bypass, "bypass", false,
		"run with Mechanisms off; structural context reducers stay on (ADR 0006)")
	flags.StringVar(&opts.Resume, "resume", "", "resume a saved session (id from /sessions, or a file path)")
	flags.BoolVar(&opts.ContinueSession, "continue", false,
		"resume this workspace's most recent saved session (mutually exclusive with --resume)")
	flags.StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	// The two rendering-diagnostic seams. Hidden, because they are for debugging apogee rather
	// than for using it, and the root's advertised flag set is deliberately minimal — but real
	// flags on the shipped binary, so a rendering bug can be captured from a stock build instead
	// of from a patched renderer nobody runs.
	flags.StringVar(&opts.TUITrace, "tui-trace", "",
		"append every byte the renderer writes to the terminal to this file (one quoted string per write)")
	flags.StringVar(&opts.TUIDiag, "tui-diag", "",
		"log the terminal's reported capabilities, size, colour profile and mode answers to this file")
	// MarkHidden's only error is "no such flag", which the two registrations directly above make
	// impossible; there is nothing for a caller to do about it and nowhere to report it.
	_ = flags.MarkHidden("tui-trace")
	_ = flags.MarkHidden("tui-diag")

	// --resume names one session, --continue picks the newest; asking for both is a flag error.
	cmd.MarkFlagsMutuallyExclusive("resume", "continue")

	// The root's flags are its own, not persistent: a subcommand declares what it needs
	// rather than inheriting the TUI session's surface.
	cmd.AddCommand(subs...)

	return cmd
}
