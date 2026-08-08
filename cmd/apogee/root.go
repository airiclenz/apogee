package main

import (
	"context"
	"errors"
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
	// continueSession resumes this workspace's most recent saved session without naming an id
	// (the --continue flag). Mutually exclusive with resume.
	continueSession bool
	configDir       string

	// Resolved display values handed to the TUI (not bound to flags). hostAlias is the
	// footer's friendly host name — the startup `servers:` entry's own name, which IS a server's
	// alias since ADR 0036, else the endpoint host; contextWindow is the
	// `context-window:` PIN in tokens, and nothing else — startup no longer probes, so a
	// non-zero value means the user pinned the window and the heartbeat must never override it
	// (ADR 0024, decision 9), while 0 means "discover it, live" and the first landed beat binds
	// whatever the server reports.
	hostAlias     string
	contextWindow int

	// apiKey is the upstream bearer token (the startup `servers:` entry's own `api-key` field,
	// which `APOGEE_API_KEY` overlays), resolved-not-flag-bound like hostAlias above — but for a
	// different reason:
	// there is deliberately no --api-key, because a secret on the command line lands in shell
	// history and in `ps` output. applyConfig sets it from the resolved settings (env > file);
	// empty ⇒ no Authorization header, which is the keyless local-server default. Its VALUE is
	// never logged, persisted, or displayed — only its presence is ever reported.
	apiKey string

	// servers is the resolved `servers:` list — the named upstream endpoints besides the one this
	// session started on, in file order. Loaded from the config file only (default-empty ⇒ no
	// alternatives are configured); applyConfig sets it from the resolved settings, having already
	// refused an entry that could never be switched to.
	servers []serverEntry

	// startupServer is the resolved `server:` key — the NAME of the servers entry this session
	// starts on, which /server records after a switch. It is the one key of this pair with sources
	// above the file (--server > APOGEE_SERVER > `server:`), so the flag below binds it directly
	// and applyConfig then overwrites it with the resolved value. Empty ⇒ no entry is named.
	startupServer string

	// serverFlagBound says the command being run registers the `--server` flag. Only the root
	// command does: `apogee headless` and `apogee probe` declare their own flag surface and take
	// the startup name from `APOGEE_SERVER` / `server:` alone. Exactly one thing reads it — the
	// startup refusal (selectStartupServer), which must offer a remedy the command printing it
	// actually has, since a flag the parser would reject is worse than no suggestion at all. It is
	// set beside the registration itself so the claim cannot drift away from the flag it describes.
	serverFlagBound bool

	// startupEphemeral records that the server this session started on is the EPHEMERAL unnamed
	// entry a raw `--endpoint`/`APOGEE_ENDPOINT` override builds (ADR 0036 decision 6) rather than
	// an entry out of `servers:`. It is a fact about the invocation, not about the file, and it is
	// exactly what upstreamChoices needs to decide whether the switch list must synthesize a row
	// for the startup server: a configured startup is already IN the list, an ephemeral one is
	// nowhere. Resolved-not-flag-bound; applyConfig sets it.
	startupEphemeral bool

	// startupLauncher is the SELECTED startup entry's own `llama-launcher:` value, exactly as the
	// user wrote it: empty (the key absent) ⇒ the launcher integration is off for this server,
	// `auto` ⇒ the launcher's own default config, anything else ⇒ the config file to read. It is a
	// fact about the entry this session starts ON rather than a global one, which is the whole
	// point of the per-entry key: only the server the launcher actually fronts offers `/model`'s
	// Launch profiles, and every other entry keeps its advertised-model discovery. The ephemeral
	// `--endpoint`/`APOGEE_ENDPOINT` override entry carries none, so an override run starts with
	// the integration off. Resolved-not-flag-bound like the two fields above; applyConfig sets it
	// from the startup entry. The composition root resolves the value — including where the
	// launcher's own default config lives — because only it knows the launcher.
	startupLauncher string

	// prebound says this session starts with NO upstream bound, and why — the zero value being the
	// ordinary start, on the server selection determined. It is the one resolution outcome that
	// does not come out of applyConfig's write-back but out of its refusal: the root command
	// recognises startupUndetermined and turns it into this value (ADR 0036 decisions 3 and 8),
	// while every other Driver returns the refusal and stops. runRoot reads it to decide whether to
	// construct the engine before the TUI starts or to hand the TUI the bind seam instead.
	prebound tui.PreboundStart

	// llamaLauncher is the resolved `llama-launcher:` key (ADR 0029), exactly as the user wrote it:
	// empty ⇒ auto-detect the launcher's own config, `off` ⇒ the local-server verbs stay off, any
	// other value ⇒ the launcher config file to read. Loaded from the config file only, like the
	// servers list above; applyConfig sets it from the resolved settings, having already refused a
	// value that is no shape the key has. The composition root resolves the ladder — including
	// whether an auto-detected config is actually there — because only it knows the launcher.
	llamaLauncher string

	// editor is the resolved `editor:` key (ADR 0041), exactly as the user wrote it: the command line
	// an external edit is opened with, flags included (`code -w`). Loaded from the config file only,
	// like the launcher key above; applyConfig sets it from the resolved settings. Empty ⇒ the key
	// names no editor, and the launch site walks the rest of the ladder ($VISUAL, $EDITOR, then the
	// OS default opener) — which is why nothing is substituted for emptiness here.
	editor string

	// confineToWorkspace tunes Auto's blast radius (ADR 0012); default true. It is NOT a
	// flag — it is loaded from the GLOBAL config file only (a project config cannot loosen
	// it), so applyConfig sets it from the resolved settings. It is the EFFECTIVE value:
	// either an explicit `confine-to-workspace: false` or a Host acknowledgement naming this
	// machine resolves it to false (ADR 0012, amendment 2026-07-21).
	confineToWorkspace bool

	// unconfinedHosts is the Host acknowledgement list as configured — the machines recorded
	// as disposable (`unconfined-hosts:`). Global-config-file only, like the flag above;
	// applyConfig sets it from the resolved settings, having already collapsed a match for
	// THIS host into confineToWorkspace. It is carried so the session can report the list
	// back and extend it.
	unconfinedHosts []unconfinedHost

	// webSearchEndpoint is the config'd search backend for the web_search tool (P3.11),
	// loaded from the config file only (empty ⇒ the built-in DuckDuckGo default; "off"
	// disables the tool). applyConfig sets it from settings.
	webSearchEndpoint string

	// useProjectSkills gates discovery of the workspace's bare skills/ folder (default true),
	// loaded from the config file only. applyConfig sets it from the resolved settings.
	useProjectSkills bool

	// autoCompact gates the automatic, budget-driven generative Compaction trigger (default true),
	// loaded from the config file only. applyConfig sets it from settings; runRoot folds it into
	// apogee.Config.Context.CompactionEnabled.
	autoCompact bool

	// autoTitle gates the automatic session-naming call — the cosmetic out-of-band completion that
	// names a new Session record from its first prompt (default true), loaded from the config file
	// only. applyConfig sets it from settings; runRoot folds it into tui.Options.AutoTitle. It gates
	// only the AUTOMATIC firing: the GenerateTitle seam stays wired either way, so /rename can still
	// regenerate a title on demand when this is false.
	autoTitle bool

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
	// sets it from settings; runRoot validates every key against the catalogue and folds the enabled
	// IDs into apogee.Config.EnableMechanisms, which the engine builds catalogue rows from and merges
	// into apogee.Config.Mechanisms (ADR 0015 §1).
	mechanisms map[string]bool

	// validatedSetsEnable is the Validated-set surface's off-switch (ADR 0016 §5; default true)
	// and validatedSetsAlias its explicit carry-over map (§3: runtime fingerprint label → entry
	// key — an identity mapping is the low-confidence confirm, a differing one the transfer).
	// Both are loaded from the config file only (`validated-sets:` block, no flag/env, like
	// mechanisms). applyConfig sets them from settings; runRoot matches and folds an applying
	// set into apogee.Config.EnableMechanisms.
	validatedSetsEnable bool
	validatedSetsAlias  map[string]string

	// present is the resolved `present:` block (ADR 0019) — the presentation ladder's config:
	// auto-open, the command override, and the doc server's port and advertised host. Loaded from
	// the config file only, like the blocks above. applyConfig sets it from the resolved settings;
	// runRoot turns it into this host's actual mechanisms (presentationRungs) and installs them on
	// the TUI bridge, which is what supplies Config.Presenter and registers present_document.
	present presentSettings

	// systemPrompt is the resolved system-prompt block (ADR 0023) — the global prompt (inline or
	// a file) and the per-model overrides. Loaded from the config file only, like present above.
	// applyConfig sets it from the resolved settings, having already refused the structural
	// defects; runRoot collapses it into the one template for THIS session's model
	// (resolveSystemPrompt, after model resolution) and folds that into apogee.Config.SystemPrompt.
	systemPrompt systemPromptSettings

	// contextFiles is the RESOLVED workspace context-file name list (the `context-files:` block,
	// file-only like the system prompt above): the names looked for in the workspace root, in
	// inclusion order, or nil when the feature is off (`enable: false`, or an empty `names:`).
	// applyConfig sets it from the resolved settings, having already refused a name that cannot be
	// a workspace file; runRoot folds it straight into apogee.Config.ContextFiles, which the engine
	// reads at every session boundary.
	contextFiles []string

	// ui is the resolved `ui:` block — the status-line spinner's animation and its colour loop.
	// Loaded from the config file only, like present above. applyConfig sets it from the resolved
	// settings (already validated against the styles this build knows); runRoot hands both values
	// straight to the renderer as tui.Options.
	ui uiSettings

	// cursorShape is the resolved `cursor-shape:` key — the shape the prompt's caret is drawn with,
	// as the user spelled it (empty ⇒ the renderer's default). Loaded from the config file only,
	// like ui above, and already validated by applyConfig; runRoot parses it into the renderer's
	// own tea.CursorShape for tui.Options.
	cursorShape string

	// tuiTrace and tuiDiag are the two hidden rendering-diagnostic flags (--tui-trace and
	// --tui-diag), each a file path and each empty by default. They are flag-only — there is
	// deliberately no config key and no environment variable, because a permanently-on trace is a
	// growing file nobody asked for; a diagnostic is something you turn on for one run. runRoot
	// hands both straight to tui.Options, which owns what they mean.
	tuiTrace string
	tuiDiag  string

	// overrides records which HIGHER-precedence source beat the config file for a key this run,
	// keyed by registry path (`mode` → sourceFlag when --mode was given, `model` → sourceEnv when
	// APOGEE_MODEL was set). It is the one resolution fact the resolved values above cannot carry:
	// precedence collapses the layers into a single value, and the /settings pane has to mark a row
	// whose value this run is not taking from the file. applyConfig fills it (overrideSources);
	// absent from the map ⇒ the file or the built-in default, which is the majority of keys.
	overrides map[string]configSource
}

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
	var opts options

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
			if created, path, err := seedDefaultConfig(opts, changed, os.Getenv); err != nil {
				cmd.PrintErrln("apogee: could not create default config:", err)
			} else if created {
				cmd.PrintErrln("apogee: created a starter config at", path)
			}
			// Resolve the upstream/autonomy settings by precedence (flag > env > file >
			// default) before construction; the flag set tells us which flags were
			// explicitly set so an unset flag's default never shadows a lower layer.
			// A soft notice from resolution (a malformed unconfined-hosts entry) prints here,
			// on stderr before the alt-screen, like the seeding line above.
			if err := applyConfig(&opts, changed, os.Getenv, os.ReadFile, func(msg string) { cmd.PrintErrln(msg) }); err != nil {
				// The one refusal this Driver answers instead of printing: resolution could not say
				// which server to start on. The TUI can ASK — it has a picker, and a human in front
				// of it — so it starts pre-bound and gets its engine from the answer (ADR 0036
				// decisions 3 and 8). Every other error, and every other Driver, stops here.
				var undetermined *startupUndetermined
				if !errors.As(err, &undetermined) {
					return err
				}
				opts.prebound = undetermined.start
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
	flags.StringVar(&opts.endpoint, "endpoint", "",
		"OpenAI-compatible LLM server URL to start on, unlisted and not saved (overrides --server)")
	flags.StringVar(&opts.startupServer, "server", "",
		"name of the servers: entry to start on (default: the last one /server switched to)")
	// The claim beside the registration above: this command has the flag, so its startup refusal
	// may offer it as the fix. The commands that do not register it say APOGEE_SERVER instead.
	opts.serverFlagBound = true
	flags.StringVar(&opts.model, "model", "",
		"model name to request (default: the startup server's model: hint, else ask the server)")
	flags.StringVar(&opts.mode, "mode", string(modeAskBefore),
		"autonomy ladder: plan | ask-before | allow-edits | auto "+
			"(auto needs filesystem confinement; tuned by confine-to-workspace in config.yaml)")
	flags.StringVar(&opts.workspace, "workspace", "",
		"workspace root the file tools are scoped to (default: current directory)")
	flags.BoolVar(&opts.bypass, "bypass", false,
		"run with Mechanisms off; structural context reducers stay on (ADR 0006)")
	flags.StringVar(&opts.resume, "resume", "", "resume a saved session (id from /sessions, or a file path)")
	flags.BoolVar(&opts.continueSession, "continue", false,
		"resume this workspace's most recent saved session (mutually exclusive with --resume)")
	flags.StringVar(&opts.configDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")

	// The two rendering-diagnostic seams. Hidden, because they are for debugging apogee rather
	// than for using it, and the root's advertised flag set is deliberately minimal — but real
	// flags on the shipped binary, so a rendering bug can be captured from a stock build instead
	// of from a patched renderer nobody runs.
	flags.StringVar(&opts.tuiTrace, "tui-trace", "",
		"append every byte the renderer writes to the terminal to this file (one quoted string per write)")
	flags.StringVar(&opts.tuiDiag, "tui-diag", "",
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
