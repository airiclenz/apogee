package main

// The declarative key registry: ONE table describing every key of the on-disk config
// schema (fileConfig), so the surfaces that need to talk ABOUT a key — the /settings
// screen's rows, resolution's env-var and flag names, the scalar splice writer's target
// paths — all read the same description instead of restating it. Before this table each
// of those facts lived wherever it was needed (a string literal in resolveSettings, a
// sentence in the template's comments, nothing at all for the screen), which is exactly
// how a config surface drifts from its schema.
//
// The anti-drift guarantee is mechanical rather than editorial: the bijection guard
// (TestRegistryIsBijectionWithFileConfig) walks fileConfig's yaml tags with reflection and
// fails when a leaf key has no row, or a row names a path the schema does not have. Adding
// a key to fileConfig therefore breaks the test gate until the key is described here too.

// configKind is what a key HOLDS, which is what decides how it is displayed and whether
// the settings surface can edit it in place. It is deliberately coarser than the Go type:
// enum is a string with a closed vocabulary (the parse site owns the set — see EnumValues),
// and structured is the terminator — a list, a map, a nested block, or a multi-line text
// value whose editor is a separate concern (a text field cannot express it, so the surface
// points at config.yaml instead).
type configKind string

const (
	kindBool       configKind = "bool"
	kindInt        configKind = "int"
	kindString     configKind = "string"
	kindEnum       configKind = "enum"
	kindStructured configKind = "structured"
)

// configKey describes one key of the config schema: what it is, where it may be set from,
// and whether a surface may edit it.
//
// Path is the yaml path with `.` between levels (`ui.spinner`), which is both the display
// key and the identity every other surface addresses the key by. Default is the key's
// built-in default rendered AS THE FILE WOULD SPELL IT ("true", "ask-before", "0"), with
// the empty string meaning the key defaults to unset — so a caller can show it and a
// writer can write it without a second table of literals. EnumValues is non-empty exactly
// for kindEnum and lists the vocabulary in the order the parse site lists it.
//
// EnvVar and FlagName are the higher-precedence sources for the key, empty when there is
// none: most keys are file-only on purpose (a per-machine or per-model fact does not
// belong to one invocation), and api-key has an env var but deliberately no flag — a
// secret typed on a command line lands in shell history and in `ps` output.
//
// GlobalOnly marks the keys ADR 0012 fences to the global config: no flag, no env, and no
// project config may set them, because a hostile repo's invocation environment must not be
// able to loosen Auto's blast radius. RestartRequired says a change takes effect on the
// next launch; it is false only where a runtime path to apply the new value already exists
// (today: mode, through the same seam Shift+Tab drives). Editable is whether the settings
// surface may write the key at all — false for every structured kind, and false for the
// confinement keys, whose acknowledgement interlock stays single-homed in /confine. Masked
// says the value must not be rendered in full (api-key only). Desc is the one-line
// description a surface shows, condensed from the template's own comments.
type configKey struct {
	Path            string
	Kind            configKind
	Default         string
	EnumValues      []string
	EnvVar          string
	FlagName        string
	GlobalOnly      bool
	RestartRequired bool
	Editable        bool
	Masked          bool
	Desc            string
}

// The closed vocabularies of the three enum keys, in the order their parse sites list them.
// They are restated here rather than imported because two of the three live in internal/tui
// as unexported sets (spinnerStyleNames, cursorShapeNames) — that package owns the
// vocabulary and validates against it, and TestRegistryEnumValuesMatchParseSites pins these
// lists to those parse functions so a divergence is a test failure, not a silent one.
var (
	modeValues        = []string{string(modePlan), string(modeAskBefore), string(modeAllowEdits), string(modeAuto)}
	spinnerValues     = []string{"snake", "glitter", "classic"}
	cursorShapeValues = []string{"block", "underline", "bar"}
)

// keyRegistry is the table: one row per leaf key of fileConfig, plus one row per structured
// block, in the order the seeded template (cmd/apogee/defaults/config.yaml) presents them —
// so a surface that renders the registry top to bottom reads like the file the user edits.
// model-profile is the one key the template does not document, so it sits last.
var keyRegistry = []configKey{
	{
		Path: "endpoint", Kind: kindString,
		EnvVar: envEndpoint, FlagName: "endpoint",
		RestartRequired: true, Editable: true,
		Desc: "The OpenAI-compatible LLM server URL a session starts on.",
	},
	{
		Path: "api-key", Kind: kindString,
		EnvVar:          envAPIKey, // no flag, deliberately: a secret must not reach shell history or `ps`
		RestartRequired: true, Editable: true, Masked: true,
		Desc: "Bearer token sent as Authorization on every upstream request; unset sends no header.",
	},
	{
		Path: "host-alias", Kind: kindString,
		RestartRequired: true, Editable: true,
		Desc: "A short, friendly name for the upstream host, shown in the status footer.",
	},
	{
		Path: "model", Kind: kindString,
		EnvVar: envModel, FlagName: "model",
		RestartRequired: true, Editable: true,
		Desc: "The model to request — a hint: apogee follows the model the server actually serves.",
	},
	{
		Path: "servers", Kind: kindStructured,
		RestartRequired: true,
		Desc:            "The other servers a running session can be moved to with /server.",
	},
	{
		Path: "llama-launcher", Kind: kindString,
		RestartRequired: true, Editable: true,
		Desc: "Which llama-launcher config the local-server verbs read; unset auto-detects, off disables.",
	},
	{
		Path: "mode", Kind: kindEnum, Default: string(modeAskBefore), EnumValues: modeValues,
		EnvVar: envMode, FlagName: "mode",
		Editable: true, // the one live-appliable key: the Shift+Tab seam already switches mode mid-session
		Desc:     "Autonomy mode: how tool calls are gated, from least to most autonomous.",
	},
	{
		Path: "system-prompt-text", Kind: kindStructured,
		RestartRequired: true,
		Desc:            "The system prompt written inline — the standing instructions sent ahead of your first message.",
	},
	{
		Path: "system-prompt-file", Kind: kindStructured,
		RestartRequired: true,
		Desc:            "A file to read the system prompt from instead of writing it inline.",
	},
	{
		Path: "system-prompt-models", Kind: kindStructured,
		RestartRequired: true,
		Desc:            "Per-model system prompts, keyed by resolved model name; a match replaces the global prompt whole.",
	},
	{
		Path: "context-files.enable", Kind: kindBool, Default: "true",
		RestartRequired: true, Editable: true,
		Desc: "Fold the workspace context files below into the system prompt at session start.",
	},
	{
		Path: "context-files.names", Kind: kindStructured, Default: "[AGENTS.md]",
		RestartRequired: true,
		Desc:            "Workspace-root file names folded into the system prompt, in list order.",
	},
	{
		Path: "confine-to-workspace", Kind: kindBool, Default: "true",
		GlobalOnly: true, RestartRequired: true,
		Editable: false, // the acknowledgement interlock stays single-homed in /confine (ADR 0012)
		Desc:     "Auto's blast radius: filesystem writes fenced to the workspace under OS confinement.",
	},
	{
		Path: "unconfined-hosts", Kind: kindStructured,
		GlobalOnly: true, RestartRequired: true,
		Desc: "Machines acknowledged as disposable, where auto mode runs unconfined.",
	},
	{
		Path: "web-search-endpoint", Kind: kindString,
		RestartRequired: true, Editable: true,
		Desc: "Search endpoint the web_search tool queries; unset uses DuckDuckGo, off disables it.",
	},
	{
		Path: "mcp-servers", Kind: kindStructured,
		RestartRequired: true,
		Desc:            "External MCP servers connected at startup; their tools always ask in auto.",
	},
	{
		Path: "use-project-skills", Kind: kindBool, Default: "true",
		RestartRequired: true, Editable: true,
		Desc: "Discover skills from the workspace's bare skills/ folder as well as the libraries.",
	},
	{
		Path: "auto-compact", Kind: kindBool, Default: "true",
		RestartRequired: true, Editable: true,
		Desc: "Fold older turns into a compact brief before the context window overflows.",
	},
	{
		Path: "auto-title", Kind: kindBool, Default: "true",
		RestartRequired: true, Editable: true,
		Desc: "Name a new session from its first prompt with one small extra completion.",
	},
	{
		Path: "context-window", Kind: kindInt, Default: "0",
		RestartRequired: true, Editable: true,
		Desc: "Pin the model context window in tokens; 0 discovers it from the server, live.",
	},
	{
		Path: "present.auto-open", Kind: kindBool, Default: "true",
		RestartRequired: true, Editable: true,
		Desc: "Open a presented document in its application on a local desktop run.",
	},
	{
		Path: "present.command", Kind: kindString,
		RestartRequired: true, Editable: true,
		Desc: "Open presented documents with this application instead of the OS default ({path} = the file).",
	},
	{
		Path: "present.port", Kind: kindInt, Default: "0",
		RestartRequired: true, Editable: true,
		Desc: "The built-in document server's port; 0 picks a free one per session.",
	},
	{
		Path: "present.host", Kind: kindString,
		RestartRequired: true, Editable: true,
		Desc: "Address the printed document URL advertises; empty is detected from the SSH connection.",
	},
	{
		Path: "ui.spinner", Kind: kindEnum, Default: "snake", EnumValues: spinnerValues,
		RestartRequired: true, Editable: true,
		Desc: "The status-line spinner animation shown while a turn runs.",
	},
	{
		Path: "ui.spinner-color", Kind: kindBool, Default: "true",
		RestartRequired: true, Editable: true,
		Desc: "Run the ten-second colour loop over the spinner glyph.",
	},
	{
		Path: "ui.show-scrollbar", Kind: kindBool, Default: "true",
		RestartRequired: true, Editable: true,
		Desc: "Paint the transcript's scroll bar and reserve the column it hangs in.",
	},
	{
		Path: "cursor-shape", Kind: kindEnum, Default: "block", EnumValues: cursorShapeValues,
		RestartRequired: true, Editable: true,
		Desc: "The shape the prompt's caret is drawn with; it is always steady.",
	},
	{
		Path: "bypass", Kind: kindBool, Default: "false",
		EnvVar: envBypass, FlagName: "bypass",
		RestartRequired: true, Editable: true,
		Desc: "Run with Mechanisms off; the structural context reducers stay on.",
	},
	{
		Path: "mechanisms", Kind: kindStructured,
		RestartRequired: true,
		Desc:            "Catalogued small-model Mechanisms to enable by canonical ID; every one defaults off.",
	},
	{
		Path: "validated-sets", Kind: kindStructured,
		RestartRequired: true,
		Desc:            "The per-model Validated-set surface: its off-switch and explicit model aliases.",
	},
	{
		Path: "model-profile", Kind: kindStructured,
		RestartRequired: true,
		Desc:            "How the configured model speaks the wire: tool-call format and thinking-channel style.",
	},
}

// lookupKey returns the registry row for a yaml path. A linear scan is the right shape at
// this size — the table is a few dozen rows and every caller looks a key up once per render
// or once per write — and it keeps the table a plain ordered literal with no index to keep
// in step with it.
func lookupKey(path string) (configKey, bool) {
	for _, k := range keyRegistry {
		if k.Path == path {
			return k, true
		}
	}
	return configKey{}, false
}

// mustKey returns the registry row for a path and panics when the table has none. It exists for
// the package-level tables built OVER the registry — resolution's multiSourceKeys binding — where
// a missing row is a defect in this package's own literals rather than anything an input can
// cause, exactly the regexp.MustCompile-on-a-literal-pattern case. Every such table is
// initialised at process start, so the panic can only ever fire on the first run after the
// edit that removed the row, and TestMultiSourceKeysBindDescribedKeys names it before then.
func mustKey(path string) configKey {
	k, ok := lookupKey(path)
	if !ok {
		panic("apogee: no config registry row for " + path + " (a table built over the registry names it)")
	}
	return k
}
