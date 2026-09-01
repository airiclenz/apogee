package config

// The root command's flag/option value: the parsed invocation plus every value config
// resolution writes back onto it. It is the one struct the composition root fills before
// it builds anything, so it lives with the resolution that fills it rather than with the
// cobra command that binds a handful of its fields (ADR 0043).

import (
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/profiles"
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

	// workingWindow is the `working-window:` key in tokens — how much of the context window above a
	// session actually works in. Resolved-not-flag-bound like the pin beside it, and file-only for
	// the same reason: how much room is affordable on a machine is a per-machine tuning fact. 0 means
	// the key is unset and the working room IS the advertised window, which is what every session did
	// before this key existed; a positive value bounds the Budget and every reducer that reads it,
	// while overflow detection and the reply-cap derivation keep measuring against the window. The
	// composition root hands it to ContextConfig.WorkingWindow.
	WorkingWindow int

	// responseReserve is the `response-reserve:` share — how much of the context window, as a
	// fraction of it, is held back for the model's reply. Resolved-not-flag-bound like the pin
	// above, and file-only for the same reason: how a window is divided is a per-machine tuning
	// fact. 0 means the key is unset and apogee's built-in 0.20 share stands; the loader accepts
	// nothing between 0 and a usable share, so a non-zero value here always is one. The
	// composition root hands it to ContextConfig.ResponseReserveFraction, where the Budget applies
	// it whenever no reply reserve in TOKENS is pinned.
	ResponseReserve float64

	// apiKey is the upstream bearer token (the startup `servers:` entry's own `api-key` field,
	// which `APOGEE_API_KEY` overlays), resolved-not-flag-bound like hostAlias above — but for a
	// different reason:
	// there is deliberately no --api-key, because a secret on the command line lands in shell
	// history and in `ps` output. ApplyConfig sets it from the resolved settings (env > file);
	// empty ⇒ no Authorization header, which is the keyless local-server default. Its VALUE is
	// never logged, persisted, or displayed — only its presence is ever reported.
	APIKey string

	// apiKeyCmd and apiKeyEnv are the startup entry's other two KEY SOURCES, carried exactly as the
	// entry wrote them: the command whose output IS the key, and the NAME of the variable holding
	// it. They are flattened beside apiKey above for the reason every other per-entry fact is —
	// the composition root re-assembles the startup ServerEntry out of these fields (startupEntry)
	// — and they stay UNRESOLVED here on purpose: a source runs at the seam that needs the key,
	// never at load, so a config listing six servers runs no command for the five this session
	// never talks to (KeyResolver). At most one of the three is ever set, which ValidateServers has
	// already refused a file for breaking; `APOGEE_API_KEY` overlays apiKey over whichever of them
	// the entry named (ADR 0036 decision 6), and a literal beats the other two at resolution, which
	// is what makes that overlay work without erasing what the file says.
	APIKeyCmd string
	APIKeyEnv string

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

	// SubAgentsServer is the resolved `sub-agents-server:` key — the NAME of the servers entry
	// that takes the DELEGATIONS this session spawns, which /sub-agents-server records after a
	// change. File-only, unlike the pointer above it (no flag, no variable): which machine runs the
	// children is a config act rather than an invocation. Empty ⇒ no target is named, and
	// delegations run on the session's own upstream (SubAgentsServerTarget).
	SubAgentsServer string

	// SubAgentsChoice is the resolved `sub-agents-choice:` key — who picks the server a delegation
	// runs on (ADR 0069). File-only like the pointer above it, and never empty once resolution has
	// run: an absent key resolves to [SubAgentsChoiceFixed], the behaviour that predates the key.
	SubAgentsChoice SubAgentsChoice

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

	// startupMaxOutputTokens is the SELECTED startup entry's own `max-output-tokens:` value, exactly
	// as the user wrote it: 0 (the key absent) ⇒ nothing is pinned for this server, so the engine
	// derives the reply cap from the room its Budget already reserves (ADR 0046). It is a fact about
	// the entry this session starts ON for the reason startupParallelAgents beside it is — the
	// ceiling belongs to the slot, not to the run — and the ephemeral
	// `--endpoint`/`APOGEE_ENDPOINT` override entry carries none, which leaves an override run
	// deriving. Resolved-not-flag-bound; ApplyConfig sets it from the startup entry, and the
	// composition root carries it into the engine's ContextConfig.
	StartupMaxOutputTokens int

	// startupContextWindow is the SELECTED startup entry's own `context-window:` value, exactly as
	// the user wrote it: 0 (the key absent) ⇒ that entry pins nothing, so the top-level
	// `context-window:` key answers and, unpinned there too, the first beat's observation binds the
	// window (ADR 0045 decision 3). It is a fact about the entry this session starts ON for the
	// reason the two fields above are — the window bounds the SLOT, not the run — and the ephemeral
	// `--endpoint`/`APOGEE_ENDPOINT` override entry carries none, which leaves an override run on
	// the top-level key. Resolved-not-flag-bound; ApplyConfig sets it from the startup entry, and the
	// composition root resolves it over that key (ResolveContextWindow) at the bind, so a session
	// that STARTS on a pinned entry budgets against the pin from its first Turn rather than from its
	// first beat.
	StartupContextWindow int

	// startupWorkingWindow is the SELECTED startup entry's own `working-window:` value, exactly as
	// the user wrote it: 0 (the key absent) ⇒ that entry bounds nothing, so the top-level
	// `working-window:` key answers and, unbounded there too, the session works in the whole
	// advertised window. It is a fact about the entry this session starts ON for the reason the field
	// above is — the room is a property of the SLOT, not of the run — and the ephemeral
	// `--endpoint`/`APOGEE_ENDPOINT` override entry carries none, which leaves an override run on the
	// top-level key. Resolved-not-flag-bound; ApplyConfig sets it from the startup entry, and the
	// composition root resolves it over that key (ResolveWorkingWindow) at the bind, so a session
	// that STARTS on a bounded entry works in that room from its first Turn.
	StartupWorkingWindow int

	// startupResponseReserve is the SELECTED startup entry's own `response-reserve:` value, exactly
	// as the user wrote it: 0 (the key absent) ⇒ that entry states no share, so the top-level
	// `response-reserve:` key answers and, unset there too, apogee's built-in 0.20 stands. It is a
	// fact about the entry this session starts ON for the reason the three fields above are — how a
	// window is divided is a statement about the SLOT the reply must fit in — and the ephemeral
	// `--endpoint`/`APOGEE_ENDPOINT` override entry carries none, which leaves an override run on
	// the top-level key. Resolved-not-flag-bound; ApplyConfig sets it from the startup entry, and the
	// composition root resolves it over that key (ResolveResponseReserve) at the bind, so a session
	// that STARTS on an entry stating its own share budgets by it from its first Turn.
	StartupResponseReserve float64

	// StartupEffortDialect is the SELECTED startup entry's own `effort-dialect:` value, exactly as
	// the user wrote it: "" (the key absent, or the explicit `auto`) ⇒ that entry forces nothing, so
	// passive detection answers for the shape (ADR 0060 decision 3). It is a fact about the entry
	// this session starts ON for the reason the four fields above are — the dialect is a property of
	// the SERVER, not of the run — and the ephemeral `--endpoint`/`APOGEE_ENDPOINT` override entry
	// forces none, which leaves an override run on what discovery sees. Resolved-not-flag-bound;
	// ApplyConfig sets it from the startup entry, and the composition root hands it to the beat that
	// answers the dial (provider.WithEffortDialect) and to every unattended run's construction seed,
	// so a session that STARTS on a dialled entry speaks that dialect from its first request.
	StartupEffortDialect string

	// prebound says this session starts with NO upstream bound, and why — the zero value being the
	// ordinary start, on the server selection determined. It is the one resolution outcome that
	// does not come out of ApplyConfig's write-back but out of its refusal: the root command
	// recognises StartupUndetermined and turns it into this value (ADR 0036 decisions 3 and 8),
	// while every other Driver returns the refusal and stops. runRoot reads it to decide whether to
	// construct the engine before the TUI starts or to hand the TUI the bind seam instead.
	Prebound domain.PreboundStart

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

	// useShippedSkills gates the skill source compiled into the binary (default true, config file
	// only) — the lowest-priority source, which every folder on disk shadows (ADR 0065 §4).
	// ApplyConfig sets it from the resolved settings; the composition roots fold it into the
	// skills.Sources they hand the Provider, sessions and Firings alike.
	UseShippedSkills bool

	// useDefaultPrompt allows the EMBEDDED default system prompt when nothing is configured
	// (default true, config file only) — the last rung of the resolution ladder (ADR 0064 §2).
	// ApplyConfig sets it from the resolved settings; the composition root hands it to
	// ResolveSystemPrompt beside the block above, which is where it is consulted or ignored.
	UseDefaultPrompt bool

	// autoCompact gates the automatic, budget-driven generative Compaction trigger (default true),
	// loaded from the config file only. ApplyConfig sets it from settings; runRoot folds it into
	// apogee.Config.Context.CompactionEnabled.
	AutoCompact bool

	// delegateMaxSteps bounds a CHILD agent's one Exchange, in Turns (default 80; 0 = unbounded),
	// loaded from the config file only. ApplyConfig sets it from settings; the composition root
	// folds it into apogee.Config.Delegation.MaxSteps. It never bounds the main loop, which is the
	// human's to stop.
	DelegateMaxSteps int

	// autoTitle gates the automatic session-naming call — the cosmetic out-of-band completion that
	// names a new Session record from its first prompt (default true), loaded from the config file
	// only. ApplyConfig sets it from settings; runRoot folds it into tui.Options.AutoTitle. It gates
	// only the AUTOMATIC firing: the GenerateTitle seam stays wired either way, so /rename can still
	// regenerate a title on demand when this is false.
	AutoTitle bool

	// rememberModel gates remembering the model choice (default false), loaded from the config file
	// only: with it on, an explicit model pick is written back into the session's `servers:` entry —
	// `model:` on a plain server, `launch-profile:` on a launcher-fronted one — and the interactive
	// TUI restores what was recorded at the next startup. ApplyConfig sets it from settings.
	RememberModel bool

	// mcpServers is the set of external MCP servers to connect on startup (P3.15), loaded from
	// the config file only (default-empty ⇒ MCP dormant). ApplyConfig sets it from settings.
	MCPServers []mcp.ServerConfig

	// toolsDisabled and toolsEnabled are the resolved GLOBAL roster deltas (ADR 0057) — the built-in
	// tools this config takes off the menu, and the ones it puts back on it. Loaded from the config
	// file only (default-empty ⇒ the whole roster the build offers). ApplyConfig sets them from
	// settings, having already reported any name that matches no tool and any name written under
	// both; runRoot folds ToolsDisabled into apogee.Config.DisabledTools, which the registry assembly
	// subtracts, so a disabled tool is neither offered to the model nor dispatchable.
	//
	// ToolsEnabled is the middle rung's ADD direction: a name here lifts a tool the build registers
	// default-off (tools.DefaultOffTool), so "I want this tool everywhere" never forces a catch-all
	// `model-profiles:` entry. A matching profile's own `tools:` axis outranks both, per tool.
	ToolsDisabled []string
	ToolsEnabled  []string

	// urlAllowHosts / urlDenyHosts are the resolved `url-safety:` host layer — the hosts the network
	// tools (web_fetch, http_request, web_search) may reach, and the hosts they may not. Loaded from
	// the config file only (default-empty ⇒ every host, subject to the guard's always-on SSRF floor),
	// like toolsDisabled above. ApplyConfig sets them from settings; the composition root builds the
	// network tools' security.URLGuard from them, where they can only ever TIGHTEN it — the floor is
	// not reachable from configuration at all.
	URLAllowHosts []string
	URLDenyHosts  []string

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

// SubAgentsChoice is who gets to pick the server a delegation runs on (ADR 0069) — the `fixed` |
// `model` vocabulary of the root `sub-agents-choice:` key, as a type of its own so a seam taking the
// answer cannot be handed any other string. It is a string underneath because that is what the file
// spells and what the registry row offers; the two values below are the whole vocabulary.
type SubAgentsChoice string

const (
	// SubAgentsChoiceFixed leaves the seat to the `sub-agents-server:` key alone — the behaviour of
	// every session before this key existed, and what an absent key resolves to.
	SubAgentsChoiceFixed SubAgentsChoice = "fixed"
	// SubAgentsChoiceModel lets the top-level model name a seat per delegation (`run_on`), with the
	// key above as the default it falls back to.
	SubAgentsChoiceModel SubAgentsChoice = "model"
)

// ParseSubAgentsChoice resolves the file's spelling of `sub-agents-choice:` into the typed value,
// refusing any word outside the two. The EMPTY string is not a defect and not a third state: it is
// the key left out — and the reset a settings surface commits — so it resolves to
// [SubAgentsChoiceFixed], the behaviour every session had before the key existed.
//
// The refusal spells both words and says what each one does, because the value is a choice between
// two named behaviours rather than a magnitude or a name this process could look up: only saying
// what would have worked tells a typo apart from a misunderstanding.
func ParseSubAgentsChoice(value string) (SubAgentsChoice, error) {
	switch choice := SubAgentsChoice(value); choice {
	case "":
		return SubAgentsChoiceFixed, nil
	case SubAgentsChoiceFixed, SubAgentsChoiceModel:
		return choice, nil
	}
	return "", fmt.Errorf("apogee: invalid sub-agents-choice: %q — it takes %q (the sub-agents-server: "+
		"key alone picks where a delegation runs) or %q (the top-level model may say run_on per "+
		"delegation)", value, SubAgentsChoiceFixed, SubAgentsChoiceModel)
}
