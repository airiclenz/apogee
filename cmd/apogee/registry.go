package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/tui"
)

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
// and structured is the terminator — a map, a nested block, a list of blocks, or a multi-line
// text value whose editor is a separate concern (a single-line text field cannot express it,
// so the surface points at config.yaml instead).
type configKind string

const (
	kindBool       configKind = "bool"
	kindInt        configKind = "int"
	kindString     configKind = "string"
	kindEnum       configKind = "enum"
	kindStructured configKind = "structured"
	// kindStringList is a list of plain names written on ONE line as a flow sequence
	// (`names: [AGENTS.md, CLAUDE.md]`). It is a kind of its own rather than structured because a
	// single-line text field CAN express it — the surface edits the line as the comma-separated text
	// it reads as, and the writer renders the list back through the YAML marshaller. Only a list of
	// SCALARS qualifies; a list of blocks (`servers:`, `unconfined-hosts:`) stays structured.
	kindStringList configKind = "string-list"
	// kindServer is a string whose vocabulary is this config's OWN `servers:` block: a closed set
	// like an enum's, but one no static table can hold, so EnumValues stays empty and the surface
	// asks the session for the list (tui.SettingServer). It is the writer's kindString in every
	// respect — one name on one line — and it is declared apart from it so a surface can offer the
	// choice as a picker rather than as a text field.
	kindServer configKind = "server"
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
// written (saveConfigSetting) — the check the kind alone cannot make: a URL that has no host,
// a port outside 0-65535, a launcher path written as a URL. It is nil for the keys whose kind
// is the whole of their contract (a bool, a plain name), and where a validator already exists
// for the same key at startup it is THAT function this points at, so a value refused at a
// settings surface and a value refused at launch are refused by one implementation. Startup
// resolution does not call it — it validates the parsed block it already builds — so this is
// the write path's guard rather than a second schema.
type configKey struct {
	Path       string
	Kind       configKind
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
		Path: "servers", Kind: kindStructured,
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
		// this key through saveConfigSetting on the surface's behalf.
		Path: "server", Kind: kindServer,
		EnvVar: envServer, FlagName: "server",
		Editable: true,
		Desc:     "Which servers: entry a session starts on; /server records the last one chosen.",
	},
	{
		Path: "llama-launcher", Kind: kindString,
		Editable: true,
		Validate: validateLlamaLauncher, // the startup check itself, unwrapped: ADR 0029's three shapes
		Desc:     "Which llama-launcher config the local-server verbs read; unset auto-detects, off disables.",
	},
	{
		Path: "mode", Kind: kindEnum, Default: string(modeAskBefore), EnumValues: modeValues,
		EnvVar: envMode, FlagName: "mode",
		Editable: true, // Shift+Tab drives the same seam this key's live apply does
		Validate: validateSettingMode,
		Desc:     "Autonomy mode: how tool calls are gated, from least to most autonomous.",
	},
	{
		Path: "system-prompt-text", Kind: kindStructured,
		Desc: "The system prompt written inline — the standing instructions sent ahead of your first message.",
	},
	{
		// A plain path on a plain line, so the pane types it like any other string. What it names is
		// still the whole system prompt: the write persists the path and the apply RE-READS the block
		// and re-resolves it per model (the dispatcher's system-prompt entry), because these three keys
		// are one prompt and only the file can spell all of it (ADR 0023).
		Path: "system-prompt-file", Kind: kindString,
		Editable: true,
		Validate: validateSystemPromptFile,
		Desc:     "A file to read the system prompt from instead of writing it inline.",
	},
	{
		Path: "system-prompt-models", Kind: kindStructured,
		Desc: "Per-model system prompts, keyed by resolved model name; a match replaces the global prompt whole.",
	},
	{
		Path: "context-files.enable", Kind: kindBool, Default: "true",
		Editable: true,
		Desc:     "Fold the workspace context files below into the system prompt at session start.",
	},
	{
		// The one list the file writes on a single line, and therefore the one a text field can edit
		// (kindStringList): the row is typed as the comma-separated names it shows, and the writer
		// renders them back as the flow sequence the template documents.
		Path: "context-files.names", Kind: kindStringList, Default: "[AGENTS.md]",
		Editable: true,
		Validate: validateContextFileNames, // the startup check itself: contextFilesSettings.validate
		Desc:     "Workspace-root file names folded into the system prompt, in list order.",
	},
	{
		Path: "confine-to-workspace", Kind: kindBool, Default: "true",
		GlobalOnly: true,
		Editable:   false, // the acknowledgement interlock stays single-homed in /confine (ADR 0012)
		Desc:       "Auto's blast radius: filesystem writes fenced to the workspace under OS confinement.",
	},
	{
		Path: "unconfined-hosts", Kind: kindStructured,
		GlobalOnly: true,
		Desc:       "Machines acknowledged as disposable, where auto mode runs unconfined.",
	},
	{
		Path: "web-search-endpoint", Kind: kindString,
		Editable: true,
		Validate: validateSearchEndpoint,
		Desc:     "Search endpoint the web_search tool queries; unset uses DuckDuckGo, off disables it.",
	},
	{
		Path: "mcp-servers", Kind: kindStructured,
		Desc: "External MCP servers connected at startup; their tools always ask in auto.",
	},
	{
		Path: "use-project-skills", Kind: kindBool, Default: "true",
		Editable: true,
		Desc:     "Discover skills from the workspace's bare skills/ folder as well as the libraries.",
	},
	{
		Path: "auto-compact", Kind: kindBool, Default: "true",
		Editable: true,
		Desc:     "Fold older turns into a compact brief before the context window overflows.",
	},
	{
		Path: "auto-title", Kind: kindBool, Default: "true",
		Editable: true,
		Desc:     "Name a new session from its first prompt with one small extra completion.",
	},
	{
		Path: "context-window", Kind: kindInt, Default: "0",
		Editable: true,
		Validate: validateContextWindow,
		Desc:     "Pin the model context window in tokens; 0 discovers it from the server, live.",
	},
	{
		Path: "present.auto-open", Kind: kindBool, Default: "true",
		Editable: true,
		Desc:     "Open a presented document in its application on a local desktop run.",
	},
	{
		Path: "present.command", Kind: kindString,
		Editable: true,
		Desc:     "Open presented documents with this application instead of the OS default ({path} = the file).",
	},
	{
		Path: "present.port", Kind: kindInt, Default: "0",
		Editable: true,
		Validate: validatePresentPort,
		Desc:     "The built-in document server's port; 0 picks a free one per session.",
	},
	{
		Path: "present.host", Kind: kindString,
		Editable: true,
		Desc:     "Address the printed document URL advertises; empty is detected from the SSH connection.",
	},
	{
		Path: "ui.spinner", Kind: kindEnum, Default: "snake", EnumValues: spinnerValues,
		Editable: true,
		Validate: validateSpinnerName,
		Desc:     "The status-line spinner animation shown while a turn runs.",
	},
	{
		Path: "ui.spinner-color", Kind: kindBool, Default: "true",
		Editable: true,
		Desc:     "Run the ten-second colour loop over the spinner glyph.",
	},
	{
		Path: "ui.show-scrollbar", Kind: kindBool, Default: "true",
		Editable: true,
		Desc:     "Paint the transcript's scroll bar and reserve the column it hangs in.",
	},
	{
		Path: "cursor-shape", Kind: kindEnum, Default: "block", EnumValues: cursorShapeValues,
		Editable: true,
		Validate: validateCursorShapeName,
		Desc:     "The shape the prompt's caret is drawn with; it is always steady.",
	},
	{
		Path: "bypass", Kind: kindBool, Default: "false",
		EnvVar: envBypass, FlagName: "bypass",
		Editable: true,
		Desc:     "Run with Mechanisms off; the structural context reducers stay on.",
	},
	{
		Path: "mechanisms", Kind: kindStructured,
		Desc: "Catalogued small-model Mechanisms to enable by canonical ID; every one defaults off.",
	},
	{
		Path: "validated-sets", Kind: kindStructured,
		Desc: "The per-model Validated-set surface: its off-switch and explicit model aliases.",
	},
	{
		Path: "model-profile", Kind: kindStructured,
		Desc: "How the configured model speaks the wire: tool-call format and thinking-channel style.",
	},
}

// ----------------------------------------------------------------------------
// The rows' validate hooks
// ----------------------------------------------------------------------------
//
// One function per key whose kind is not the whole of its contract (configKey.Validate). Each is
// the check the STARTUP path already makes for that key, called with the value as the file would
// spell it: uiSettings.validate for the spinner name, ParseCursorShape for the caret, parseMode for
// the ladder, presentSettings.validate for the port, validateLlamaLauncher for the launcher path.
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
//   - a named file that is not there, or that cannot be opened — the check resolveSystemPrompt makes
//     when it selects the prompt, moved ahead of the write so a path typed with a finger-slip is
//     refused on the row rather than at the next launch.
//
// The existence half runs only on a path this function can resolve ON ITS OWN: an absolute one, or a
// `~` one, which expandUserPath makes absolute. A RELATIVE path is resolved against the apogee home —
// the directory config.yaml lives in, so a prompt file travels with the configuration
// (resolveSystemPrompt) — which a validator holding nothing but the value cannot know. Such a value is
// accepted here and answered by the APPLY a moment later, whose failure lands on the same row: better
// than refusing a path that is perfectly good, and better than resolving it against a second base.
func validateSystemPromptFile(value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return errors.New(`apogee: invalid system-prompt-file "": name a file to read the prompt from, ` +
			"or reset the key to take the file away")
	}
	path, err := expandUserPath(v)
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

// validateContextFileNames refuses a `context-files.names:` list the next launch would refuse, through
// the startup check itself (contextFilesSettings.validate): an empty entry, a name that is not
// workspace-relative, one that climbs out with "..", and the same name listed twice.
//
// The value arrives as the FILE spells it — the one-line flow sequence — so it is read back into a
// list by the same parse the writer renders from (parseSettingList), and what is checked is therefore
// the list a reader takes out of the file rather than a second reading of the keystrokes. An EMPTY
// list is a value, not an error: `names: []` is the second documented spelling of "off".
func validateContextFileNames(value string) error {
	return contextFilesSettings{names: parseSettingList(value)}.validate()
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

// validatePresentPort refuses a port the document server could not bind, through the same check
// startup makes on the parsed block (presentSettings.validate) rather than a second range literal.
func validatePresentPort(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("apogee: invalid present.port %q: want a TCP port in 0-65535 "+
			"(0 — the default — takes an ephemeral port)", value)
	}
	return presentSettings{port: n}.validate()
}

// validateSpinnerName refuses a spinner style this build has no animation for, through the startup
// check itself — uiSettings.validate, which asks internal/tui (the package that owns the
// vocabulary) and names the key the value was read from.
func validateSpinnerName(value string) error {
	return uiSettings{spinner: tui.SpinnerStyle(value)}.validate()
}

// validateCursorShapeName refuses a caret shape internal/tui cannot draw, wrapped exactly as
// applyConfig wraps the same parse at startup (config.go) so both say the same thing.
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
	_, err := parseMode(value)
	return err
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
