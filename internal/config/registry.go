package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/prompt"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/tui"
)

// The declarative key registry: ONE table describing every key of the on-disk config
// schema (fileConfig), so the surfaces that need to talk ABOUT a key — the /settings
// screen's rows, resolution's env-var and flag names, the scalar splice writer's target
// paths — all read the same description instead of restating it. Before this table each
// of those facts lived wherever it was needed (a string literal in ResolveSettings, a
// sentence in the template's comments, nothing at all for the screen), which is exactly
// how a config surface drifts from its schema.
//
// The anti-drift guarantee is mechanical rather than editorial: the bijection guard
// (TestRegistryIsBijectionWithFileConfig) walks fileConfig's yaml tags with reflection and
// fails when a leaf key has no row, or a row names a path the schema does not have. Adding
// a key to fileConfig therefore breaks the test gate until the key is described here too.

// Kind is what a key HOLDS, which is what decides how it is displayed and whether
// the settings surface can edit it in place. It is deliberately coarser than the Go type:
// enum is a string with a closed vocabulary (the parse site owns the set — see EnumValues),
// and structured is the terminator — a map, a nested block, or a list of blocks, whose editor
// is a separate concern (no field on a row can express one, so the surface opens the file).
type Kind string

const (
	KindBool       Kind = "bool"
	KindInt        Kind = "int"
	KindString     Kind = "string"
	KindEnum       Kind = "enum"
	KindStructured Kind = "structured"
	// KindFloat is a fractional number written on one line — `response-reserve: 0.2`, the schema's
	// one SHARE. It is a kind of its own rather than an int because rounding it to a whole number
	// is the same as deleting it, and rather than a string because the value has a range the
	// surface must refuse outside of (the row's Validate); the pane edits it in the same caret
	// buffer an int uses, since a share is typed rather than picked.
	KindFloat Kind = "float"
	// KindText is a multi-line text value — prose, not a setting written on a line: the system
	// prompt is the one the schema has. It is a kind of its own rather than structured because the
	// surface DOES have an editor for it (a multi-line field replacing the pane's key list, ADR 0037
	// decision 10), and rather than a string because no single line holds it — the writer carries it
	// as a block scalar (`system-prompt-text: |` and the indented lines under it) and replaces that
	// whole block, where every other kind replaces the one line its key sits on.
	KindText Kind = "text"
	// KindStringList is a list of plain names written on ONE line as a flow sequence
	// (`names: [AGENTS.md, CLAUDE.md]`). It is a kind of its own rather than structured because a
	// single-line text field CAN express it — the surface edits the line as the comma-separated text
	// it reads as, and the writer renders the list back through the YAML marshaller. Only a list of
	// SCALARS qualifies; a list of blocks (`servers:`, `unconfined-hosts:`) stays structured.
	KindStringList Kind = "string-list"
	// KindServer is a string whose vocabulary is this config's OWN `servers:` block: a closed set
	// like an enum's, but one no static table can hold, so EnumValues stays empty and the surface
	// asks the session for the list (tui.SettingServer). It is the writer's KindString in every
	// respect — one name on one line — and it is declared apart from it so a surface can offer the
	// choice as a picker rather than as a text field.
	KindServer Kind = "server"
	// KindScheme is a colour-scheme NAME, and KindServer's twin in every respect that matters here:
	// a closed vocabulary no static table can hold, because it is the built-ins plus whatever
	// `*.yaml` files the user has put in `<apogee-home>/schemes/` right now. So EnumValues stays
	// empty and the surface asks the session for the list, it is the writer's KindString (one name
	// on one line), and it is declared apart from KindString so a surface can offer the choice as a
	// picker rather than as a text field.
	//
	// It is NOT KindEnum for the reason KindServer is not: the enum kind means a vocabulary this
	// table lists, and the writer refuses anything outside it (renderSettingValue) — which would
	// refuse every user scheme ever written.
	KindScheme Kind = "scheme"
)

// Key describes one key of the config schema: what it is, where it may be set from,
// and whether a surface may edit it.
//
// Path is the yaml path with `.` between levels (`ui.spinner`), which is both the display
// key and the identity every other surface addresses the key by. Default is the key's
// built-in default rendered AS THE FILE WOULD SPELL IT ("true", "ask-before", "0"), with
// the empty string meaning the key defaults to unset — so a caller can show it and a
// writer can write it without a second table of literals. EnumValues is non-empty exactly
// for KindEnum and lists the vocabulary in the order the parse site lists it.
//
// EnvVar and FlagName are the higher-precedence sources for the key, empty when there is
// none: most keys are file-only on purpose (a per-machine or per-model fact does not
// belong to one invocation), and api-key has an env var but deliberately no flag — a
// secret typed on a command line lands in shell history and in `ps` output.
//
// GlobalOnly marks the keys ADR 0012 fences to the global config: no flag, no env, and no
// project config may set them, because a hostile repo's invocation environment must not be
// able to loosen Auto's blast radius. Editable is whether the settings surface may write the
// key at all — false for every structured kind, and false for the confinement keys, whose
// acknowledgement interlock stays single-homed in /confine — and, since ADR 0037, it is also
// whether that surface APPLIES it: a key it writes takes effect in the running session on the
// same keypress, so no key is gated on a restart and no row says one is. Masked
// says the value must not be rendered in full — no row carries it today (the schema's one
// secret is a `servers:` entry's api-key, nested inside a structured block). Desc is the
// one-line description a surface shows, condensed from the template's own comments.
//
// Validate is the key's own admission test, run on a value a SURFACE offers before it is
// written (SaveConfigSetting) — the check the kind alone cannot make: a URL that has no host,
// a port outside 0-65535, a launcher path written as a URL. It is nil for the keys whose kind
// is the whole of their contract (a bool, a plain name), and where a validator already exists
// for the same key at startup it is THAT function this points at, so a value refused at a
// settings surface and a value refused at launch are refused by one implementation. Startup
// resolution does not call it — it validates the parsed block it already builds — so this is
// the write path's guard rather than a second schema.
type Key struct {
	Path       string
	Kind       Kind
	Default    string
	EnumValues []string
	EnvVar     string
	FlagName   string
	GlobalOnly bool
	Editable   bool
	Masked     bool
	Validate   func(value string) error
	Desc       string
}

// The closed vocabularies of the three enum keys, in the order their parse sites list them.
// They are restated here rather than imported because two of the three live in internal/tui
// as unexported sets (spinnerStyleNames, cursorShapeNames) — that package owns the
// vocabulary and validates against it, and TestRegistryEnumValuesMatchParseSites pins these
// lists to those parse functions so a divergence is a test failure, not a silent one.
var (
	modeValues        = []string{string(domain.ModePlan), string(domain.ModeAskBefore), string(domain.ModeAllowEdits), string(domain.ModeAuto)}
	spinnerValues     = []string{"snake", "glitter", "classic"}
	cursorShapeValues = []string{"block", "underline", "bar"}
)

// KeyRegistry is the table: one row per leaf key of fileConfig, plus one row per structured
// block, in the order the seeded template (internal/config/defaults/config.yaml) presents them —
// so a surface that renders the registry top to bottom reads like the file the user edits.
// model-profiles is the template's closing block, so it sits last here too.
var KeyRegistry = []Key{
	{
		Path: "servers", Kind: KindStructured,
		Desc: "The servers you run models on — name, endpoint, and what each one needs.",
	},
	{
		// A kind of its own rather than an enum, even though its values ARE a closed set: EnumValues
		// is static and TestRegistryEnumValuesMatchParseSites recovers each vocabulary from its parse
		// site, while the names this key takes are whatever THIS config's `servers:` list spells. A
		// name that matches no entry is therefore refused where the list is known — at selection —
		// rather than by a vocabulary this table cannot hold.
		//
		// Editable, and the edit is the `/server` SWITCH itself (ADR 0037 decision 4): the surface
		// offers the configured entries, the session moves to the one chosen, and the move records it
		// as the entry the next session starts on (ADR 0036 decision 2) — which is why nothing writes
		// this key through SaveConfigSetting on the surface's behalf.
		Path: "server", Kind: KindServer,
		EnvVar: EnvServer, FlagName: "server",
		Editable: true,
		Desc:     "Which servers: entry a session starts on; /server records the last one chosen.",
	},
	{
		Path: "mode", Kind: KindEnum, Default: string(domain.ModeAskBefore), EnumValues: modeValues,
		EnvVar: EnvMode, FlagName: "mode",
		Editable: true, // Shift+Tab drives the same seam this key's live apply does
		Validate: validateSettingMode,
		Desc:     "Autonomy mode: how tool calls are gated, from least to most autonomous.",
	},
	{
		// Prose rather than a value on a line, so it is the one key the pane edits in a field of its
		// own: ⏎ replaces the key list with a multi-line editor and ctrl+s commits it (ADR 0037
		// decision 10). The write persists the block and the apply RE-READS it with its two siblings,
		// for the reason system-prompt-file's does — these three keys are one prompt (ADR 0023).
		Path: "system-prompt-text", Kind: KindText,
		Editable: true,
		Validate: validateSystemPromptText,
		Desc:     "The system prompt written inline — the standing instructions sent ahead of your first message.",
	},
	{
		// A plain path on a plain line, so the pane types it like any other string. What it names is
		// still the whole system prompt: the write persists the path and the apply RE-READS the block
		// and re-resolves it per model (the dispatcher's system-prompt entry), because these three keys
		// are one prompt and only the file can spell all of it (ADR 0023).
		Path: "system-prompt-file", Kind: KindString,
		Editable: true,
		Validate: validateSystemPromptFile,
		Desc:     "A file to read the system prompt from instead of writing it inline.",
	},
	{
		Path: "system-prompt-models", Kind: KindStructured,
		Desc: "Per-model system prompts, keyed by resolved model name; a match replaces the global prompt whole.",
	},
	{
		Path: "context-files.enable", Kind: KindBool, Default: "true",
		Editable: true,
		Desc:     "Fold the workspace context files below into the system prompt at session start.",
	},
	{
		// The one list the file writes on a single line, and therefore the one a text field can edit
		// (KindStringList): the row is typed as the comma-separated names it shows, and the writer
		// renders them back as the flow sequence the template documents.
		Path: "context-files.names", Kind: KindStringList, Default: "[AGENTS.md]",
		Editable: true,
		Validate: validateContextFileNames, // the startup check itself: contextFilesSettings.validate
		Desc:     "Workspace-root file names folded into the system prompt, in list order.",
	},
	{
		Path: "confine-to-workspace", Kind: KindBool, Default: "true",
		GlobalOnly: true,
		Editable:   false, // the acknowledgement interlock stays single-homed in /confine (ADR 0012)
		Desc:       "Auto's blast radius: filesystem writes fenced to the workspace under OS confinement.",
	},
	{
		Path: "unconfined-hosts", Kind: KindStructured,
		GlobalOnly: true,
		Desc:       "Machines acknowledged as disposable, where auto mode runs unconfined.",
	},
	{
		Path: "web-search-endpoint", Kind: KindString,
		Editable: true,
		Validate: validateSearchEndpoint,
		Desc:     "Search endpoint the web_search tool queries; unset uses DuckDuckGo, off disables it.",
	},
	{
		Path: "mcp-servers", Kind: KindStructured,
		Desc: "External MCP servers connected at startup; their tools always ask in auto.",
	},
	{
		// The roster switch, a name list on one line like context-files.names — so the pane edits it
		// in a field and the writer renders it back as the flow sequence the template documents. No
		// validate hook, deliberately: a name matching no tool is a startup NOTICE rather than a
		// refusal (unknownToolNotice), and a hook here would make the settings surface stricter than
		// the file it writes.
		Path: "tools.disabled", Kind: KindStringList,
		Editable: true,
		Desc:     "Built-in tools to take off the menu, by name; the model is neither offered nor able to call them.",
	},
	{
		// The host layer over the network tools' url-safety guard, a name list on one line like the
		// roster above it — same kind, same field editor, and no validate hook for a reason of its own:
		// an entry is normalized permissively where the guard is built (trim, IDNA, lowercase, trailing
		// root dot stripped), so what a hook here would refuse is a host spelling the guard itself
		// accepts.
		Path: "url-safety.allow-hosts", Kind: KindStringList,
		Editable: true,
		Desc:     "Hosts the network tools may reach, with their subdomains; empty means every host.",
	},
	{
		Path: "url-safety.deny-hosts", Kind: KindStringList,
		Editable: true,
		Desc:     "Hosts the network tools may never reach, with their subdomains; deny wins over the allow list.",
	},
	{
		Path: "use-project-skills", Kind: KindBool, Default: "true",
		Editable: true,
		Desc:     "Discover skills from the workspace's bare skills/ folder as well as the libraries.",
	},
	{
		Path: "auto-compact", Kind: KindBool, Default: "true",
		Editable: true,
		Desc:     "Fold older turns into a compact brief before the context window overflows.",
	},
	{
		Path: "auto-title", Kind: KindBool, Default: "true",
		Editable: true,
		Desc:     "Name a new session from its first prompt with one small extra completion.",
	},
	{
		Path: "remember-model", Kind: KindBool, Default: "false",
		Editable: true,
		Desc:     "Record the model you pick into its servers: entry and come back on it next start.",
	},
	{
		Path: "context-window", Kind: KindInt, Default: "0",
		Editable: true,
		Validate: validateContextWindow,
		Desc:     "Pin the model context window in tokens; 0 discovers it from the server, live.",
	},
	{
		Path: "response-reserve", Kind: KindFloat, Default: "0",
		Editable: true,
		Validate: validateResponseReserve,
		Desc:     "Share of the window held back for the reply, above 0 and below 1; 0 takes apogee's own 0.20.",
	},
	{
		Path: "present.auto-open", Kind: KindBool, Default: "true",
		Editable: true,
		Desc:     "Open a presented document in its application on a local desktop run.",
	},
	{
		Path: "present.command", Kind: KindString,
		Editable: true,
		Desc:     "Open presented documents with this application instead of the OS default ({path} = the file).",
	},
	{
		Path: "present.port", Kind: KindInt, Default: "0",
		Editable: true,
		Validate: validatePresentPort,
		Desc:     "The built-in document server's port; 0 picks a free one per session.",
	},
	{
		Path: "present.host", Kind: KindString,
		Editable: true,
		Desc:     "Address the printed document URL advertises; empty is detected from the SSH connection.",
	},
	{
		Path: "ui.spinner", Kind: KindEnum, Default: "snake", EnumValues: spinnerValues,
		Editable: true,
		Validate: validateSpinnerName,
		Desc:     "The status-line spinner animation shown while a turn runs.",
	},
	{
		Path: "ui.spinner-color", Kind: KindBool, Default: "true",
		Editable: true,
		Desc:     "Run the ten-second colour loop over the spinner glyph.",
	},
	{
		Path: "ui.show-scrollbar", Kind: KindBool, Default: "true",
		Editable: true,
		Desc:     "Paint the scroll bar on the transcript and on any overflowing popup, and reserve its column.",
	},
	{
		// A dynamic vocabulary (KindScheme), so EnumValues is empty and the surface asks the session
		// which schemes exist — the built-ins plus every `*.yaml` in <apogee-home>/schemes/, where a
		// user file shadows a built-in of the same name.
		Path: "ui.color-scheme", Kind: KindScheme, Default: scheme.DefaultName,
		Editable: true,
		Validate: validateColorSchemeName,
		Desc:     "Palette the screen is drawn in; ~/.apogee/schemes/<name>.yaml shadows a built-in.",
	},
	{
		// A length of time, which this table has no kind for — and one key is not a vocabulary, so it
		// is the writer's plain string with a hook that parses it, the posture present.port takes with
		// its range: the kind carries the shape, and the hook carries the contract the kind cannot.
		Path: "ui.stall-after", Kind: KindString, Default: "90s",
		Editable: true,
		Validate: validateStallAfter,
		Desc:     "Engine silence after which a running turn is marked quiet on the status line; 0 turns it off.",
	},
	{
		// No validate hook and none possible: a bool's kind IS its whole contract. Editable, and the
		// edit is honoured at the NEXT start — the observer is installed while the engine is
		// constructed — which the description says out loud so a row that took the write without
		// changing the session reads as the key's contract rather than as a failure.
		Path: "ui.inspector", Kind: KindBool, Default: "false",
		Editable: true,
		Desc:     "Capture raw request/response traffic for /inspect; takes effect at the next start.",
	},
	{
		Path: "cursor-shape", Kind: KindEnum, Default: "block", EnumValues: cursorShapeValues,
		Editable: true,
		Validate: validateCursorShapeName,
		Desc:     "The shape the prompt's caret is drawn with; it is always steady.",
	},
	{
		// Free text with no validate hook, like present.command: the value is a command LINE, and
		// whether this machine has that program is answered at launch, not at the field.
		Path: "editor", Kind: KindString,
		Editable: true,
		Desc:     "Command an external edit opens in; unset falls back to $VISUAL, $EDITOR, the OS opener.",
	},
	{
		Path: "bypass", Kind: KindBool, Default: "false",
		EnvVar: EnvBypass, FlagName: "bypass",
		Editable: true,
		Desc:     "Run with Mechanisms off; the structural context reducers stay on.",
	},
	{
		Path: "mechanisms", Kind: KindStructured,
		Desc: "Catalogued small-model Mechanisms to enable by canonical ID; every one defaults off.",
	},
	{
		// The block's off-switch is a row of its own, for the `context-files.*` reason: it is a bool
		// the pane can write, and leaving it inside a structured summary would send a human to their
		// editor to flip a single true/false. The alias map below stays structured — a map of model
		// labels to entry keys is a shape no row holds.
		Path: "validated-sets.enable", Kind: KindBool, Default: "true",
		Editable: true,
		Desc:     "Apply the Validated Mechanism set measured for the bound model when one matches.",
	},
	{
		Path: "validated-sets.alias", Kind: KindStructured,
		Desc: "Explicit carry-over from a runtime model label to the Validated-set entry it applies.",
	},
	{
		Path: "model-profiles", Kind: KindStructured,
		Desc: "How a model speaks the wire — tool-call format and thinking style — per name pattern.",
	},
}

// ----------------------------------------------------------------------------
// The rows' validate hooks
// ----------------------------------------------------------------------------
//
// One function per key whose kind is not the whole of its contract (Key.Validate). Each is
// the check the STARTUP path already makes for that key, called with the value as the file would
// spell it: UISettings.Validate for the spinner name, ParseCursorShape for the caret, domain.ParseMode for
// the ladder, PresentSettings.Validate for the port.
// Reusing them is the point — a value refused when it is typed at a surface and the same value
// refused at launch are refused by one implementation, so the surface can never persist a config
// the next run will not start on.
//
// Their messages read as this package's errors do ("apogee: invalid <key> …") and are worded to be
// read on a narrow row: they name the key, the value, and what the key takes, in that order, with
// no file path in front — the settings pane shows them inline, where a prefix would push the reason
// out of the cell.

// validateSearchEndpoint refuses a `web-search-endpoint:` the web_search tool could make nothing of.
// It is deliberately loose, because this key has four documented shapes
// (tools.NewWebSearch): empty for the built-in provider, the off/none/disabled sentinels, a
// scheme-less host the tool itself heals to https://, and a full URL. So what is refused is text no
// url.Parse accepts even after that heal — and the sentinels pass on their own merits (`off` heals
// to a host), which is why this does not restate a list the tool owns.
func validateSearchEndpoint(value string) error {
	if value == "" {
		return nil // the built-in provider
	}
	if _, err := url.Parse(value); err == nil {
		return nil
	}
	if _, err := url.Parse("https://" + value); err == nil {
		return nil
	}
	return fmt.Errorf("apogee: invalid web-search-endpoint %q: it is not a URL — give a search "+
		"endpoint, or off to disable web search", value)
}

// validateSystemPromptFile refuses a `system-prompt-file:` the next launch could not read. Two
// refusals and no third:
//
//   - EMPTY, which is not how this key is cleared. An empty string is a SET of nothing, and the
//     deliberate way to take the prompt file away is the reset backspace arms, which removes the
//     line (ADR 0035) and hands the key back to the commented example that documents it.
//   - a named file that is not there, or that cannot be opened — the check ResolveSystemPrompt makes
//     when it selects the prompt, moved ahead of the write so a path typed with a finger-slip is
//     refused on the row rather than at the next launch.
//
// The existence half runs only on a path this function can resolve ON ITS OWN: an absolute one, or a
// `~` one, which ExpandUserPath makes absolute. A RELATIVE path is resolved against the apogee home —
// the directory config.yaml lives in, so a prompt file travels with the configuration
// (ResolveSystemPrompt) — which a validator holding nothing but the value cannot know. Such a value is
// accepted here and answered by the APPLY a moment later, whose failure lands on the same row: better
// than refusing a path that is perfectly good, and better than resolving it against a second base.
func validateSystemPromptFile(value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return errors.New(`apogee: invalid system-prompt-file "": name a file to read the prompt from, ` +
			"or reset the key to take the file away")
	}
	path, err := ExpandUserPath(v)
	if err != nil || !filepath.IsAbs(path) {
		return nil // resolved against the apogee home, which this check does not hold
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("apogee: invalid system-prompt-file %q: there is no such file", value)
		}
		return fmt.Errorf("apogee: invalid system-prompt-file %q: it cannot be read", value)
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.IsDir() {
		return fmt.Errorf("apogee: invalid system-prompt-file %q: it is a directory, not a prompt file", value)
	}
	return nil
}

// validateSystemPromptText refuses an inline prompt the next launch could not send. Two refusals, the
// two validateSystemPromptFile makes for the other spelling of the same prompt:
//
//   - EMPTY, which is not how this key is cleared. A prompt of nothing is a SET of nothing, and the
//     deliberate way to send no prompt at all is the reset backspace arms, which removes the block
//     (ADR 0035) and hands the key back to the commented paragraph that documents it.
//   - a template carrying a placeholder that is not one of the known three — the check
//     ResolveSystemPrompt makes when it selects the prompt (prompt.Validate), moved ahead of the
//     write so a mistyped `{{ workspace }}` is refused on the row rather than at the next launch.
//
// The value is deliberately NOT quoted into either message, where every other validator quotes what
// it refused: a prompt is many lines of prose and the pane renders this sentence on one row.
func validateSystemPromptText(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("apogee: invalid system-prompt-text: write the prompt inline, " +
			"or reset the key to send none")
	}
	if err := prompt.Validate(value); err != nil {
		return fmt.Errorf("apogee: invalid system-prompt-text: %w", err)
	}
	return nil
}

// validateContextFileNames refuses a `context-files.names:` list the next launch would refuse, through
// the startup check itself (contextFilesSettings.validate): an empty entry, a name that is not
// workspace-relative, one that climbs out with "..", and the same name listed twice.
//
// The value arrives as the FILE spells it — the one-line flow sequence — so it is read back into a
// list by the same parse the writer renders from (ParseSettingList), and what is checked is therefore
// the list a reader takes out of the file rather than a second reading of the keystrokes. An EMPTY
// list is a value, not an error: `names: []` is the second documented spelling of "off".
func validateContextFileNames(value string) error {
	return contextFilesSettings{names: ParseSettingList(value)}.validate()
}

// validateContextWindow refuses a negative token pin. Zero is the default and means "follow the
// server's own window, live" (ADR 0024), and a positive value pins it; a negative one would reach
// the Budget as a window no context fits in, where every Turn is over budget before it starts.
func validateContextWindow(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fmt.Errorf("apogee: invalid context-window %q: want a token count of 0 or more "+
			"(0 — the default — follows the window the server reports)", value)
	}
	return nil
}

// validateResponseReserve refuses a reply share the Budget could not spend, through the same check
// the loader makes on the parsed file (validateResponseReserveFraction) rather than a second range
// literal. Zero is the default and means "hold apogee's own share back"; anything else has to sit
// strictly between 0 and 1, and a value that is not a number at all fails the parse below before
// the range is ever asked about.
func validateResponseReserve(value string) error {
	fraction, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return fmt.Errorf("apogee: invalid response-reserve %q: want a share of the context window "+
			"above 0 and below 1 (0 — the default — holds apogee's own 0.20 back)", value)
	}
	return validateResponseReserveFraction(fraction)
}

// validatePresentPort refuses a port the document server could not bind, through the same check
// startup makes on the parsed block (PresentSettings.Validate) rather than a second range literal.
func validatePresentPort(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("apogee: invalid present.port %q: want a TCP port in 0-65535 "+
			"(0 — the default — takes an ephemeral port)", value)
	}
	return PresentSettings{Port: n}.Validate()
}

// validateSpinnerName refuses a spinner style this build has no animation for, through the startup
// check itself — UISettings.Validate, which asks internal/tui (the package that owns the
// vocabulary) and names the key the value was read from.
func validateSpinnerName(value string) error {
	return UISettings{Spinner: tui.SpinnerStyle(value)}.Validate()
}

// validateStallAfter refuses a `ui.stall-after:` that is not a length of time to wait, through the
// startup path itself: the yaml seam that reads the text (toUISettings) and the check that judges
// what it made of it (UISettings.Validate). Going through the seam rather than calling
// time.ParseDuration a second time is what keeps the two answers one answer — the empty value that
// means "the default" and the `0` that means "off" are the seam's calls, not this hook's.
func validateStallAfter(value string) error {
	return uiConfig{StallAfter: &value}.toUISettings().Validate()
}

// validateColorSchemeName refuses a name that could not be a scheme's file name — empty, or one
// carrying a path separator. It deliberately does NOT check that the scheme EXISTS: the load is
// forgiving by design (ADR 0040 design call 8 — an unresolvable name costs a warning and the
// default palette), and a pane that refused to write a name the loader would happily warn about
// would be stricter than the thing it configures. What it does refuse is a name that is really a
// path, because the resolver joins it onto the schemes folder and a `../` would reach out of it.
// What counts as a name is [scheme.ValidName]'s call — the resolver's own rule, asked here so a bad
// name is refused at the keystroke instead of surviving in the file until the next start; the
// wording is this pane's, because only this pane knows the key it is about to write.
func validateColorSchemeName(value string) error {
	if value == "" {
		return fmt.Errorf("apogee: invalid ui.color-scheme: name a scheme, e.g. %q", scheme.DefaultName)
	}
	if !scheme.ValidName(value) {
		return fmt.Errorf("apogee: invalid ui.color-scheme %q: a scheme is named, not a path — "+
			"put the file in the schemes folder and name it without its .yaml", value)
	}
	return nil
}

// validateCursorShapeName refuses a caret shape internal/tui cannot draw, wrapped exactly as
// ApplyConfig wraps the same parse at startup (config.go) so both say the same thing.
func validateCursorShapeName(value string) error {
	if _, err := tui.ParseCursorShape(value); err != nil {
		return fmt.Errorf("apogee: invalid cursor-shape: %w", err)
	}
	return nil
}

// validateSettingMode refuses a mode outside the autonomy ladder, through the same parse the --mode
// flag goes through. The kind check above it already refuses anything outside EnumValues, so what
// this catches is the drift between the two: a vocabulary in this table that the ladder does not
// have (it is the write-path twin of TestRegistryEnumValuesMatchParseSites).
func validateSettingMode(value string) error {
	_, err := domain.ParseMode(value)
	return err
}

// LookupKey returns the registry row for a yaml path. A linear scan is the right shape at
// this size — the table is a few dozen rows and every caller looks a key up once per render
// or once per write — and it keeps the table a plain ordered literal with no index to keep
// in step with it.
func LookupKey(path string) (Key, bool) {
	for _, k := range KeyRegistry {
		if k.Path == path {
			return k, true
		}
	}
	return Key{}, false
}

// mustKey returns the registry row for a path and panics when the table has none. It exists for
// the package-level tables built OVER the registry — resolution's multiSourceKeys binding — where
// a missing row is a defect in this package's own literals rather than anything an input can
// cause, exactly the regexp.MustCompile-on-a-literal-pattern case. Every such table is
// initialised at process start, so the panic can only ever fire on the first run after the
// edit that removed the row, and TestMultiSourceKeysBindDescribedKeys names it before then.
func mustKey(path string) Key {
	k, ok := LookupKey(path)
	if !ok {
		panic("apogee: no config registry row for " + path + " (a table built over the registry names it)")
	}
	return k
}
