package main

// The wire side of the /settings surface: it projects the declarative key registry (internal/config/registry.go)
// plus the values THIS run resolved (options) onto the renderer's plain-data rows (tui.SettingRow).
//
// It sits here, in the composition root, for the reason every other display seam does: the schema,
// the precedence that decided which source won, the config file's own spelling of a value and the
// masking of a secret are all the binary's knowledge, and the renderer that draws the pane holds
// none of it (ADR 0011's thin renderer). What crosses the seam is a list of rows a pane can paint
// and select in, and nothing else.
//
// Two tables do the work, and both are covered by tests that pin them to the registry so a key
// added there cannot quietly render as a blank row: settingSections names the runs of keys the
// pane groups under a header, and settingValues formats each key's effective value.

import (
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/tui"
)

// maskedSettingValue is what a masked row shows INSTEAD of its value. The secret is replaced here,
// at the seam, so the renderer is never handed it at all: a value the pane does not hold cannot
// reach a paint cache, a transcript, or a crash dump, and it has no use for it — the editor buffers
// what the human types rather than revealing what was stored.
//
// No row is masked since ADR 0036 retired the top-level `api-key:`: the only secret the schema
// still carries is a `servers:` entry's own key, which sits inside a structured block the pane
// summarizes ("2 servers") rather than renders. The seam stays because the registry's Masked flag
// is the schema's way of saying "never show this", and the day a surface can edit a server entry
// is the day it is needed again — not something to rediscover with a secret already on screen.
const maskedSettingValue = "••••"

// noneSettingValue is what a structured row with nothing in it shows. Blank would read as "this
// row failed to render"; "none" is the answer to the question the row asks.
const noneSettingValue = "none"

// The three pointers a non-editable row carries — where the key IS edited. A structured block is
// edited in the human's own editor, which the row's ⏎ opens for them on that key's line (ADR 0037
// decision 5): the pane cannot hold a list of servers or a profile block on a row, but it can put
// the cursor on the line that holds one, which is a good deal more than telling somebody the name of
// a file. confine-to-workspace and unconfined-hosts are the pair that do NOT go there: their
// acknowledgement interlock (a distinct affirmative act, never a default-yes) stays single-homed in
// /confine (ADR 0012), so the pane sends the human there rather than growing a second way to loosen
// a blast radius.
//
// mechanisms is the third, and the one row that is neither: its block's children are SWITCHES
// — one bool per catalogued Mechanism — and a list of switches is a shape the pane holds perfectly
// well, so ⏎ opens a sub-list of them rather than the file (tui.Options.ListMechanisms). Raw block
// edits are still made in config.yaml by hand; what this row's ⏎ no longer does is open it there.
const (
	pointerExternalEdit  = "⏎ opens $EDITOR"
	pointerConfine       = "use /confine"
	pointerMechanismList = "⏎ opens toggle list"
)

// settingKeyMechanisms is the registry path of that row. It is spelled here because the pointer and
// the affordance below both have to recognise the one key whose read-only-ness means something else.
const settingKeyMechanisms = "mechanisms"

// settingSection is one header the pane groups rows under: a NAME and the registry path that opens
// it. Sections are runs over the registry's own order rather than a per-key label, because that
// order is the config template's order and the template's `# ===` dividers are per-key — a header
// per divider would be one header per row. Every row from Opens up to the next section's Opens
// belongs to this section, so a key inserted into the registry inherits the section it was
// inserted into and no second table has to be edited for it.
type settingSection struct {
	Name  string
	Opens string
}

// settingSections is that table, in registry order (pinned by
// TestSettingSectionsOpenInRegistryOrder). context-files sits under the system prompt because that
// is what it IS — the workspace files are folded into the standing system content — and bypass
// opens Mechanisms because it is the Mechanisms off-switch, the floor the whole surface is measured
// against.
var settingSections = []settingSection{
	{Name: "Upstream", Opens: "servers"},
	{Name: "Autonomy", Opens: "mode"},
	{Name: "System prompt", Opens: "system-prompt-text"},
	{Name: "Confinement", Opens: "confine-to-workspace"},
	{Name: "Tools & skills", Opens: "web-search-endpoint"},
	{Name: "Session", Opens: "auto-compact"},
	{Name: "Presentation", Opens: "present.auto-open"},
	{Name: "Interface", Opens: "ui.spinner"},
	{Name: "Mechanisms", Opens: "bypass"},
	{Name: "Model profiles", Opens: "model-profiles"},
}

// settingValues formats each key's EFFECTIVE value — what this run actually resolved, spelled the
// way the config file spells it, so the row and the file read the same. A structured block is
// summarized instead ("3 servers"): the pane cannot edit one, so a YAML fragment would be noise.
//
// It is keyed by registry path and covers every row (TestSettingValuesCoverEveryRegistryKey). A
// path with no formatter renders as an empty value rather than panicking — a table gap must not
// cost the user the whole surface mid-session — and that test is what keeps the gap from shipping.
var settingValues = map[string]func(config.Options) string{
	"servers": func(o config.Options) string { return countSummary(len(o.Servers), "server") },
	"server":  func(o config.Options) string { return o.StartupServer },
	"mode":    func(o config.Options) string { return o.Mode },
	// A SUMMARY of the prompt, since no row holds prose — the text itself travels beside it
	// (settingTexts) for the editor to open on. Blank when nothing is set inline, the answer every
	// other row whose value seeds a field gives: "none" would be a word standing where the prompt goes.
	"system-prompt-text": func(o config.Options) string { return countSummary(lineCount(o.SystemPrompt.Global.Text), "line") },
	// The path AS WRITTEN, blank when the key names no file — the three other editable string rows'
	// answer (present.command, present.host, web-search-endpoint), and the only safe one: this row's
	// value is what the edit field is SEEDED with, so a word standing in for emptiness would be a word
	// the next ⏎ persisted as a path, and a row reading "none" against a blank default would arm a
	// reset for a key with no line to remove.
	"system-prompt-file": func(o config.Options) string { return o.SystemPrompt.Global.File },
	"system-prompt-models": func(o config.Options) string {
		return countSummary(len(o.SystemPrompt.Models), "model")
	},
	// The `context-files:` block resolves to the NAME LIST in force (nil when the feature is off,
	// however it got there — `enable: false`, or an empty `names:`), which is why enable is reported
	// from the list rather than from a flag: the effective outcome is what the row is asked about.
	"context-files.enable": func(o config.Options) string { return boolValue(len(o.ContextFiles) > 0) },
	"context-files.names":  func(o config.Options) string { return listValue(o.ContextFiles) },
	"confine-to-workspace": func(o config.Options) string { return boolValue(o.ConfineToWorkspace) },
	"unconfined-hosts":     func(o config.Options) string { return countSummary(len(o.UnconfinedHosts), "host") },
	"web-search-endpoint":  func(o config.Options) string { return o.WebSearchEndpoint },
	"mcp-servers":          func(o config.Options) string { return countSummary(len(o.MCPServers), "server") },
	// The roster switch shows the NAMES it holds, not a count: the list is short, and which tools
	// are off is the whole of what the row is asked. Blank when nothing is disabled — the answer
	// every editable field row gives, since this value seeds the edit field.
	"tools.disabled": func(o config.Options) string { return listValue(o.ToolsDisabled) },
	// The url-safety host layer shows its NAMES for the roster's reason: the lists are short, which
	// hosts are permitted or denied is the whole of what each row is asked, and the value seeds the
	// edit field — so it reads back as the flow sequence the file carries, "[]" for a list nobody has
	// set (an empty allow list means every host, not none).
	"url-safety.allow-hosts": func(o config.Options) string { return listValue(o.URLAllowHosts) },
	"url-safety.deny-hosts":  func(o config.Options) string { return listValue(o.URLDenyHosts) },
	"use-project-skills":     func(o config.Options) string { return boolValue(o.UseProjectSkills) },
	"auto-compact":           func(o config.Options) string { return boolValue(o.AutoCompact) },
	"auto-title":             func(o config.Options) string { return boolValue(o.AutoTitle) },
	"remember-model":         func(o config.Options) string { return boolValue(o.RememberModel) },
	"context-window":         func(o config.Options) string { return strconv.Itoa(o.ContextWindow) },
	// The share in the SHORTEST spelling that reads back as the same number, which is the spelling
	// the writer persists too — so the value this row seeds its edit field with is one the next ⏎
	// can write back unchanged.
	"response-reserve": func(o config.Options) string {
		return strconv.FormatFloat(o.ResponseReserve, 'g', -1, 64)
	},
	"present.auto-open": func(o config.Options) string { return boolValue(o.Present.AutoOpen) },
	"present.command":   func(o config.Options) string { return o.Present.Command },
	"present.port":      func(o config.Options) string { return strconv.Itoa(o.Present.Port) },
	"present.host":      func(o config.Options) string { return o.Present.Host },
	"ui.spinner":        func(o config.Options) string { return string(o.UI.Spinner) },
	"ui.spinner-color":  func(o config.Options) string { return boolValue(o.UI.SpinnerColor) },
	"ui.show-scrollbar": func(o config.Options) string { return boolValue(o.UI.ShowScrollbar) },
	"ui.color-scheme":   func(o config.Options) string { return o.UI.ColorScheme },
	// The threshold as a DURATION prints itself (`1m30s`), which is a spelling the key takes back —
	// so the value this row seeds its edit field with is one the next ⏎ can persist unchanged.
	"ui.stall-after": func(o config.Options) string { return o.UI.StallAfter.String() },
	"cursor-shape":   func(o config.Options) string { return o.CursorShape },
	// The command AS WRITTEN, blank when the key names none — the answer every other editable string
	// row gives, and the only safe one here too: this value SEEDS the edit field, so a word standing
	// in for emptiness ("$EDITOR", "the OS opener") would be a word the next ⏎ persisted as a command.
	"editor":                func(o config.Options) string { return o.Editor },
	"bypass":                func(o config.Options) string { return boolValue(o.Bypass) },
	"mechanisms":            func(o config.Options) string { return countSummary(enabledCount(o.Mechanisms), "mechanism") },
	"validated-sets.enable": func(o config.Options) string { return boolValue(o.ValidatedSetsEnable) },
	"validated-sets.alias":  func(o config.Options) string { return countSummary(len(o.ValidatedSetsAlias), "alias") },
	"model-profiles":        func(o config.Options) string { return countSummary(len(o.ModelProfiles), "model profile") },
}

// settingTexts is the RAW value of the keys whose displayed value is only a summary of it — the
// KindText rows, whose editor is seeded with the prose itself (tui.SettingRow.Text). It is a second,
// deliberately tiny table rather than a second return from settingValues: exactly one key of the
// schema is prose, and every other row would have to answer a question it has no answer to.
// TestSettingTextsCoverEveryTextKey pins it to the registry.
var settingTexts = map[string]func(config.Options) string{
	"system-prompt-text": func(o config.Options) string { return o.SystemPrompt.Global.Text },
}

// settingsRows builds the /settings pane's rows: one per registry key, in registry order (which is
// the config template's order), each carrying its section, its effective value, and the metadata
// the pane needs in order to render and later edit it.
//
// Three things are applied HERE rather than in the per-key formatters, so no formatter can forget
// one: a masked key's value is replaced by the mask, a structured key with nothing in it reads
// "none", and a key whose effective value came out empty while the registry declares a default
// shows that default — an unset `cursor-shape:` is a block caret, and saying so is the honest
// answer to "what is this set to".
func settingsRows(opts config.Options) []tui.SettingRow {
	rows := make([]tui.SettingRow, 0, len(config.KeyRegistry))
	section := ""
	next := 0
	for _, k := range config.KeyRegistry {
		if next < len(settingSections) && settingSections[next].Opens == k.Path {
			section = settingSections[next].Name
			next++
		}
		value := ""
		if format, ok := settingValues[k.Path]; ok {
			value = format(opts)
		}
		switch {
		case k.Masked && value != "":
			value = maskedSettingValue
		case value == "" && k.Kind == config.KindStructured:
			value = noneSettingValue
		case value == "" && k.Default != "":
			value = k.Default
		}
		text := ""
		if raw, ok := settingTexts[k.Path]; ok {
			text = raw(opts)
		}
		source, sourceName := settingSource(k, opts.Overrides)
		rows = append(rows, tui.SettingRow{
			Path:         k.Path,
			Section:      section,
			Kind:         settingKind(k.Kind),
			Value:        value,
			Text:         text,
			Default:      k.Default,
			Source:       source,
			SourceName:   sourceName,
			EnumValues:   k.EnumValues,
			Editable:     k.Editable,
			Masked:       k.Masked,
			EditPointer:  editPointer(k),
			ExternalEdit: externallyEdited(k),
			Desc:         k.Desc,
		})
	}
	return rows
}

// settingKind projects a registry kind onto the renderer's vocabulary. The two vocabularies are
// spelled alike on purpose — they mostly describe the same shapes — but the mapping is explicit so
// that a kind added to the registry has to be given an edit idiom deliberately rather than reaching
// the pane as an unhandled string. An unmapped kind falls back to structured, the read-only end:
// the surface that cannot say what a value is must not offer to write it.
//
// KindStringList is the one kind that does NOT get a renderer kind of its own, and deliberately: what
// the pane does with a name list is exactly what it does with a string — open a field on the row and
// type the one line the value is written on. The list-ness is the WRITER's business (KindStringList,
// ParseSettingList), and giving the renderer a kind for it would only be a second name for the same
// idiom.
func settingKind(kind config.Kind) tui.SettingKind {
	switch kind {
	case config.KindBool:
		return tui.SettingBool
	case config.KindInt:
		return tui.SettingInt
	case config.KindString, config.KindStringList:
		return tui.SettingString
	case config.KindFloat:
		// A share is TYPED, so it reaches the renderer as the caret-buffer idiom an int uses — the
		// pane's int and string rows open the same buffer, and giving the renderer a float kind of
		// its own would only be a third name for it. The range is the registry row's Validate.
		return tui.SettingInt
	case config.KindText:
		return tui.SettingText
	case config.KindEnum:
		return tui.SettingEnum
	case config.KindServer:
		return tui.SettingServer
	case config.KindScheme:
		// The pane picks a scheme from a list exactly as it picks an enum value, so it reaches the
		// renderer as the enum idiom; what makes it KindScheme on this side is only that the LIST
		// comes from the session rather than from the row (settingsVocabulary).
		return tui.SettingEnum
	default:
		return tui.SettingStructured
	}
}

// settingSource reports the override marker for a row: which higher-precedence source beat the
// file for this key this run, and what that source is CALLED, so the pane's note can name it
// ("APOGEE_MODE", "--mode") instead of saying "something".
func settingSource(k config.Key, overrides map[string]config.Source) (tui.SettingSource, string) {
	switch overrides[k.Path] {
	case config.SourceEnv:
		return tui.SettingFromEnv, k.EnvVar
	case config.SourceFlag:
		return tui.SettingFromFlag, "--" + k.FlagName
	default:
		return tui.SettingFromFile, ""
	}
}

// editPointer says where a key this pane will not write is edited instead — empty for an editable
// key. The confinement pair is the one case that does not open an editor: their acknowledgement
// interlock stays single-homed in /confine (ADR 0012), and GlobalOnly is exactly the property that
// marks them, so the pointer follows the registry rather than a second list of paths.
func editPointer(k config.Key) string {
	switch {
	case k.Editable:
		return ""
	case k.Path == settingKeyMechanisms:
		return pointerMechanismList
	case externallyEdited(k):
		return pointerExternalEdit
	default:
		return pointerConfine
	}
}

// externallyEdited reports whether ⏎ on this key's row suspends into the human's own editor — the
// affordance the pointer above advertises, stated as a predicate so the row's flag and its wording
// cannot come to describe different sets of keys.
//
// It is every key the pane will not write except the confinement pair — whose interlock (ADR 0012)
// is what makes them unopenable, not their shape — and `mechanisms`, which the pane now edits in a
// list of its own. Both exceptions are subtractions from "read-only", not a shape test: a key that
// became read-only for some other reason tomorrow should reach the editor like the rest.
func externallyEdited(k config.Key) bool {
	return !k.Editable && !k.GlobalOnly && k.Path != settingKeyMechanisms
}

// boolValue spells a bool the way the config file spells it.
func boolValue(v bool) string { return strconv.FormatBool(v) }

// listValue spells a resolved name list the way the template's inline form spells it
// ("[AGENTS.md]"), so the row and the registry's default for the key read alike. An empty list is
// "[]" rather than "none": the key holds a list, and an empty one is a value the user can have set.
//
// It is also the CANONICAL spelling the splice writer verifies a written list against
// (internal/config/configwrite.go, which carries its own copy of this three-line spelling for the
// same reason): the row's text and the value a reader takes back out of the file are one string,
// or an edit would come back reading differently from what was typed.
func listValue(names []string) string { return "[" + strings.Join(names, ", ") + "]" }

// countSummary summarizes a structured block by how much is in it ("3 servers"). Zero returns
// EMPTY rather than "0 servers", so settingsRows' one central rule turns it into "none" — the
// same word every other empty structured row shows. The plural is the naive one because every
// noun this file counts ("server", "model", "host", "line", "mechanism", "alias") takes a bare s.
func countSummary(n int, noun string) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "1 " + noun
	default:
		return strconv.Itoa(n) + " " + noun + "s"
	}
}

// lineCount counts the lines of a multi-line value — how a system prompt is summarized, since its
// text is far too long to show on a row and its length is what the human recognizes it by.
func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))
}

// enabledCount counts the Mechanisms actually switched ON. A `mechanisms:` block may carry explicit
// `false` entries — that is how a user records a decision to leave one off — and those are not
// enabled Mechanisms, so counting map keys would overstate what the session is running.
func enabledCount(mechanisms map[string]bool) int {
	n := 0
	for _, on := range mechanisms {
		if on {
			n++
		}
	}
	return n
}

// The `validated-sets:` block is two rows rather than one summary of both: the off-switch is the
// fact that decides whether the rest of the block does anything, and it is a bool the pane writes
// (boolValue above), while the carry-over aliases stay a counted structured row (countSummary) that
// ⏎ opens the file on — a map of runtime labels to entry keys is a shape no row holds.

// The `model-profiles:` map is summarized by its COUNT (countSummary above), like every other block
// of entries the pane cannot hold on a row. What it does not say — which model each pattern matches,
// and what shape it gives it — is deliberate: a profile is per-model now (ADR 0044), so no single
// line can name the one in force without knowing which model is bound, and the row's job is to say
// how many the file carries and open the file on that key.
