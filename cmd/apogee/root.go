package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
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

	// Resolved display values handed to the TUI (not bound to flags). hostAlias is the
	// footer's friendly host name (config key, else the endpoint host); contextWindow is the
	// active model's window in tokens, captured during model discovery (0 when unknown).
	hostAlias     string
	contextWindow int

	// confineToWorkspace tunes Auto's blast radius (ADR 0012); default true. It is NOT a
	// flag — it is loaded from the GLOBAL config file only (a project config cannot loosen
	// it), so applyConfig sets it from the resolved settings.
	confineToWorkspace bool

	// webSearchEndpoint is the config'd search backend for the web_search tool (P3.11),
	// loaded from the config file only (empty ⇒ the built-in DuckDuckGo default; "off"
	// disables the tool). applyConfig sets it from settings.
	webSearchEndpoint string

	// useProjectSkills gates discovery of the workspace's bare skills/ folder (default true),
	// loaded from the config file only. applyConfig sets it from the resolved settings.
	useProjectSkills bool

	// mcpServers is the set of external MCP servers to connect on startup (P3.15), loaded from
	// the config file only (default-empty ⇒ MCP dormant). applyConfig sets it from settings.
	mcpServers []mcp.ServerConfig

	// profile is the model profile (CONTEXT: Model profile) — the model's tool-call format and
	// inline thinking-channel style — loaded from the config file only (a per-model concern, no
	// flag/env). applyConfig sets it from settings; a zero profile is native tool calls with no
	// inline thinking (today's behaviour). runRoot folds it into apogee.Config.Profile.
	profile apogee.ModelProfile

	// mechanisms enables catalogued small-model Mechanisms by canonical ID (Phase 4), loaded from
	// the config file only (default-empty ⇒ no Mechanism enabled; all default OFF, D1). applyConfig
	// sets it from settings; runRoot drives the mechanisms catalogue's constructor table for each
	// enabled ID and folds the built registry into apogee.Config.Mechanisms.
	mechanisms map[string]bool
}

// launcher starts the interactive UI over the constructed engine. It carries the Bridge
// whose Sink/Approver were installed in the Agent's Config, so the launcher can bind it to
// the running program (resolving the construction chicken-and-egg — phase-2 detail plan §3
// C2/C3). It is injected so tests can assert clean construction and a clean quit without a
// real terminal; main passes tui.Run.
type launcher func(ctx context.Context, eng tui.Engine, br *tui.Bridge, opts tui.Options) error

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
			"hold a coding conversation, watch tools run, and approve writes.\n\n" +
			"Settings resolve by precedence: a flag overrides an APOGEE_* environment\n" +
			"variable (APOGEE_ENDPOINT, APOGEE_MODEL, APOGEE_MODE, APOGEE_BYPASS), which\n" +
			"overrides ~/.apogee/config.yaml, which overrides the built-in default. With no\n" +
			"model set anywhere, apogee asks the server for its active model, so a single-\n" +
			"model server (e.g. llama.cpp's llama-server) needs only --endpoint. A clean\n" +
			"quit snapshots the conversation under ~/.apogee/sessions for --resume.",
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
			if created, path, err := seedDefaultConfig(opts, changed, os.Getenv); err != nil {
				cmd.PrintErrln("apogee: could not create default config:", err)
			} else if created {
				cmd.PrintErrln("apogee: created a starter config at", path)
			}
			// Resolve the upstream/autonomy settings by precedence (flag > env > file >
			// default) before construction; the flag set tells us which flags were
			// explicitly set so an unset flag's default never shadows a lower layer.
			if err := applyConfig(&opts, changed, os.Getenv, os.ReadFile); err != nil {
				return err
			}
			// With no model configured by any layer, ask the server for its active model
			// (the lowest-priority layer) so a single-model server runs with no --model set.
			// The notice prints before the alt-screen, on stderr.
			if model, err := resolveModel(cmd.Context(), &opts, discoverUpstreamModel); err != nil {
				return err
			} else if model != "" {
				cmd.PrintErrln("apogee: discovered model", model, "at", opts.endpoint)
			}
			return runRoot(cmd.Context(), opts, launch)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.endpoint, "endpoint", "", "OpenAI-compatible LLM server URL")
	flags.StringVar(&opts.model, "model", "", "model name to request (default: ask the server for its active model)")
	flags.StringVar(&opts.mode, "mode", string(modeAskBefore),
		"autonomy ladder: plan | ask-before | allow-edits | auto "+
			"(auto needs filesystem confinement; tuned by confine-to-workspace in config.yaml)")
	flags.StringVar(&opts.workspace, "workspace", "",
		"workspace root the file tools are scoped to (default: current directory)")
	flags.BoolVar(&opts.bypass, "bypass", false,
		"run with Mechanisms off; structural context reducers stay on (ADR 0006)")
	flags.StringVar(&opts.resume, "resume", "", "resume a saved session file")
	flags.StringVar(&opts.configDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	return cmd
}
