package config

// The root command's flag/option value: the parsed invocation plus every value config
// resolution writes back onto it. It is the one struct the composition root fills before
// it builds anything, so it lives with the resolution that fills it rather than with the
// cobra command that binds a handful of its fields (ADR 0043).

import (
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/tui"
)

// Options holds the parsed root-command flags plus every value resolution writes back onto
// them. It is a plain value bound to the cobra flag set so the wiring functions (resolveRoots,
// buildAgent, runRoot) are testable without a live command.
type Options struct {
	Endpoint  string
	Model     string
	Mode      string
	Workspace string
	Bypass    bool
	Resume    string
	// continueSession resumes this workspace's most recent saved session without naming an id
	// (the --continue flag). Mutually exclusive with resume.
	ContinueSession bool
	ConfigDir       string

	// Resolved display values handed to the TUI (not bound to flags). hostAlias is the
	// footer's friendly host name — the startup `servers:` entry's own name, which IS a server's
	// alias since ADR 0036, else the endpoint host; contextWindow is the
	// `context-window:` PIN in tokens, and nothing else — startup no longer probes, so a
	// non-zero value means the user pinned the window and the heartbeat must never override it
	// (ADR 0024, decision 9), while 0 means "discover it, live" and the first landed beat binds
	// whatever the server reports.
	HostAlias     string
	ContextWindow int

	// apiKey is the upstream bearer token (the startup `servers:` entry's own `api-key` field,
	// which `APOGEE_API_KEY` overlays), resolved-not-flag-bound like hostAlias above — but for a
	// different reason:
	// there is deliberately no --api-key, because a secret on the command line lands in shell
	// history and in `ps` output. ApplyConfig sets it from the resolved settings (env > file);
	// empty ⇒ no Authorization header, which is the keyless local-server default. Its VALUE is
	// never logged, persisted, or displayed — only its presence is ever reported.
	APIKey string

	// servers is the resolved `servers:` list — the named upstream endpoints besides the one this
	// session started on, in file order. Loaded from the config file only (default-empty ⇒ no
	// alternatives are configured); ApplyConfig sets it from the resolved settings, having already
	// refused an entry that could never be switched to.
	Servers []ServerEntry

	// startupServer is the resolved `server:` key — the NAME of the servers entry this session
	// starts on, which /server records after a switch. It is the one key of this pair with sources
	// above the file (--server > APOGEE_SERVER > `server:`), so the flag below binds it directly
	// and ApplyConfig then overwrites it with the resolved value. Empty ⇒ no entry is named.
	StartupServer string

	// serverFlagBound says the command being run registers the `--server` flag. Only the root
	// command does: `apogee headless` and `apogee probe` declare their own flag surface and take
	// the startup name from `APOGEE_SERVER` / `server:` alone. Exactly one thing reads it — the
	// startup refusal (selectStartupServer), which must offer a remedy the command printing it
	// actually has, since a flag the parser would reject is worse than no suggestion at all. It is
	// set beside the registration itself so the claim cannot drift away from the flag it describes.
	ServerFlagBound bool

	// startupEphemeral records that the server this session started on is the EPHEMERAL unnamed
	// entry a raw `--endpoint`/`APOGEE_ENDPOINT` override builds (ADR 0036 decision 6) rather than
	// an entry out of `servers:`. It is a fact about the invocation, not about the file, and it is
	// exactly what upstreamChoices needs to decide whether the switch list must synthesize a row
	// for the startup server: a configured startup is already IN the list, an ephemeral one is
	// nowhere. Resolved-not-flag-bound; ApplyConfig sets it.
	StartupEphemeral bool

	// startupLauncher is the SELECTED startup entry's own `llama-launcher:` value, exactly as the
	// user wrote it: empty (the key absent) ⇒ the launcher integration is off for this server,
	// `auto` ⇒ the launcher's own default config, anything else ⇒ the config file to read. It is a
	// fact about the entry this session starts ON rather than a global one, which is the whole
	// point of the per-entry key: only the server the launcher actually fronts offers `/model`'s
	// Launch profiles, and every other entry keeps its advertised-model discovery. The ephemeral
	// `--endpoint`/`APOGEE_ENDPOINT` override entry carries none, so an override run starts with
	// the integration off. Resolved-not-flag-bound like the two fields above; ApplyConfig sets it
	// from the startup entry. The composition root resolves the value — including where the
	// launcher's own default config lives — because only it knows the launcher.
	StartupLauncher string

	// startupParallelAgents is the SELECTED startup entry's own `parallel-agents:` value, exactly as
	// the user wrote it: 0 (the key absent) ⇒ nothing is pinned for this server, so the cap is
	// discovered from what it advertises and falls back to 1. It is a fact about the entry this
	// session starts ON for the same reason startupLauncher is — the width belongs to the server,
	// not to the run — and the ephemeral `--endpoint`/`APOGEE_ENDPOINT` override entry carries none,
	// which leaves an override run discovering. Resolved-not-flag-bound; ApplyConfig sets it from
	// the startup entry, and the composition root resolves it into the engine's cap (ADR 0039).
	StartupParallelAgents int

	// prebound says this session starts with NO upstream bound, and why — the zero value being the
	// ordinary start, on the server selection determined. It is the one resolution outcome that
	// does not come out of ApplyConfig's write-back but out of its refusal: the root command
	// recognises StartupUndetermined and turns it into this value (ADR 0036 decisions 3 and 8),
	// while every other Driver returns the refusal and stops. runRoot reads it to decide whether to
	// construct the engine before the TUI starts or to hand the TUI the bind seam instead.
	Prebound tui.PreboundStart

	// editor is the resolved `editor:` key (ADR 0041), exactly as the user wrote it: the command line
	// an external edit is opened with, flags included (`code -w`). Loaded from the config file only,
	// like the servers list above; ApplyConfig sets it from the resolved settings. Empty ⇒ the key
	// names no editor, and the launch site walks the rest of the ladder ($VISUAL, $EDITOR, then the
	// OS default opener) — which is why nothing is substituted for emptiness here.
	Editor string

	// confineToWorkspace tunes Auto's blast radius (ADR 0012); default true. It is NOT a
	// flag — it is loaded from the GLOBAL config file only (a project config cannot loosen
	// it), so ApplyConfig sets it from the resolved settings. It is the EFFECTIVE value:
	// either an explicit `confine-to-workspace: false` or a Host acknowledgement naming this
	// machine resolves it to false (ADR 0012, amendment 2026-07-21).
	ConfineToWorkspace bool

	// unconfinedHosts is the Host acknowledgement list as configured — the machines recorded
	// as disposable (`unconfined-hosts:`). Global-config-file only, like the flag above;
	// ApplyConfig sets it from the resolved settings, having already collapsed a match for
	// THIS host into confineToWorkspace. It is carried so the session can report the list
	// back and extend it.
	UnconfinedHosts []UnconfinedHost

	// webSearchEndpoint is the config'd search backend for the web_search tool (P3.11),
	// loaded from the config file only (empty ⇒ the built-in DuckDuckGo default; "off"
	// disables the tool). ApplyConfig sets it from settings.
	WebSearchEndpoint string

	// useProjectSkills gates discovery of the workspace's bare skills/ folder (default true),
	// loaded from the config file only. ApplyConfig sets it from the resolved settings.
	UseProjectSkills bool

	// autoCompact gates the automatic, budget-driven generative Compaction trigger (default true),
	// loaded from the config file only. ApplyConfig sets it from settings; runRoot folds it into
	// apogee.Config.Context.CompactionEnabled.
	AutoCompact bool

	// autoTitle gates the automatic session-naming call — the cosmetic out-of-band completion that
	// names a new Session record from its first prompt (default true), loaded from the config file
	// only. ApplyConfig sets it from settings; runRoot folds it into tui.Options.AutoTitle. It gates
	// only the AUTOMATIC firing: the GenerateTitle seam stays wired either way, so /rename can still
	// regenerate a title on demand when this is false.
	AutoTitle bool

	// mcpServers is the set of external MCP servers to connect on startup (P3.15), loaded from
	// the config file only (default-empty ⇒ MCP dormant). ApplyConfig sets it from settings.
	MCPServers []mcp.ServerConfig

	// toolsDisabled is the resolved `tools.disabled:` roster switch — the built-in tools this
	// config takes off the menu, by name. Loaded from the config file only (default-empty ⇒ the
	// whole roster). ApplyConfig sets it from settings, having already reported any name that
	// matches no tool; runRoot folds it into apogee.Config.DisabledTools, which the registry
	// assembly subtracts, so a disabled tool is neither offered to the model nor dispatchable.
	ToolsDisabled []string

	// profile is the model profile (CONTEXT: Model profile) — the model's tool-call format and
	// inline thinking-channel style — loaded from the config file only (a per-model concern, no
	// flag/env). ApplyConfig sets it from settings; a zero profile is native tool calls with no
	// inline thinking (today's behaviour). runRoot folds it into apogee.Config.Profile.
	Profile domain.ModelProfile

	// modelProfiles is the user's `model-profiles:` map (ADR 0044) — the Model profiles they keyed
	// by a pattern the model name contains — ordered by pattern, loaded from the config file only
	// (default-empty). ApplyConfig sets it from settings; the composition root matches the BOUND
	// model against it (profiles.Resolve, user entries first, then apogee's shipped shape table),
	// because which model is bound is not a fact this file holds.
	ModelProfiles []profiles.Entry

	// mechanisms enables catalogued small-model Mechanisms by canonical ID (Phase 4), loaded from
	// the config file only (default-empty ⇒ no Mechanism enabled; all default OFF, D1). ApplyConfig
	// sets it from settings; runRoot validates every key against the catalogue and folds the enabled
	// IDs into apogee.Config.EnableMechanisms, which the engine builds catalogue rows from and merges
	// into apogee.Config.Mechanisms (ADR 0015 §1).
	Mechanisms map[string]bool

	// validatedSetsEnable is the Validated-set surface's off-switch (ADR 0016 §5; default true)
	// and validatedSetsAlias its explicit carry-over map (§3: runtime fingerprint label → entry
	// key — an identity mapping is the low-confidence confirm, a differing one the transfer).
	// Both are loaded from the config file only (`validated-sets:` block, no flag/env, like
	// mechanisms). ApplyConfig sets them from settings; runRoot matches and folds an applying
	// set into apogee.Config.EnableMechanisms.
	ValidatedSetsEnable bool
	ValidatedSetsAlias  map[string]string

	// present is the resolved `present:` block (ADR 0019) — the presentation ladder's config:
	// auto-open, the command override, and the doc server's port and advertised host. Loaded from
	// the config file only, like the blocks above. ApplyConfig sets it from the resolved settings;
	// runRoot turns it into this host's actual mechanisms (presentationRungs) and installs them on
	// the TUI bridge, which is what supplies Config.Presenter and registers present_document.
	Present PresentSettings

	// systemPrompt is the resolved system-prompt block (ADR 0023) — the global prompt (inline or
	// a file) and the per-model overrides. Loaded from the config file only, like present above.
	// ApplyConfig sets it from the resolved settings, having already refused the structural
	// defects; runRoot collapses it into the one template for THIS session's model
	// (ResolveSystemPrompt, after model resolution) and folds that into apogee.Config.SystemPrompt.
	SystemPrompt SystemPromptSettings

	// contextFiles is the RESOLVED workspace context-file name list (the `context-files:` block,
	// file-only like the system prompt above): the names looked for in the workspace root, in
	// inclusion order, or nil when the feature is off (`enable: false`, or an empty `names:`).
	// ApplyConfig sets it from the resolved settings, having already refused a name that cannot be
	// a workspace file; runRoot folds it straight into apogee.Config.ContextFiles, which the engine
	// reads at every session boundary.
	ContextFiles []string

	// ui is the resolved `ui:` block — the status-line spinner's animation and its colour loop.
	// Loaded from the config file only, like present above. ApplyConfig sets it from the resolved
	// settings (already validated against the styles this build knows); runRoot hands both values
	// straight to the renderer as tui.Options.
	UI UISettings

	// cursorShape is the resolved `cursor-shape:` key — the shape the prompt's caret is drawn with,
	// as the user spelled it (empty ⇒ the renderer's default). Loaded from the config file only,
	// like ui above, and already validated by ApplyConfig; runRoot parses it into the renderer's
	// own tea.CursorShape for tui.Options.
	CursorShape string

	// tuiTrace and tuiDiag are the two hidden rendering-diagnostic flags (--tui-trace and
	// --tui-diag), each a file path and each empty by default. They are flag-only — there is
	// deliberately no config key and no environment variable, because a permanently-on trace is a
	// growing file nobody asked for; a diagnostic is something you turn on for one run. runRoot
	// hands both straight to tui.Options, which owns what they mean.
	TUITrace string
	TUIDiag  string

	// overrides records which HIGHER-precedence source beat the config file for a key this run,
	// keyed by registry path (`mode` → SourceFlag when --mode was given, `model` → SourceEnv when
	// APOGEE_MODEL was set). It is the one resolution fact the resolved values above cannot carry:
	// precedence collapses the layers into a single value, and the /settings pane has to mark a row
	// whose value this run is not taking from the file. ApplyConfig fills it (overrideSources);
	// absent from the map ⇒ the file or the built-in default, which is the majority of keys.
	Overrides map[string]Source
}
