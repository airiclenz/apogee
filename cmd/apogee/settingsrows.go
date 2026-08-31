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
// The values themselves come off the registry rows (config.Key.Read and its two siblings), so a key
// added to the schema renders by the act of being described rather than by a table here somebody has
// to remember. What this file owns is the PRESENTATION: settingSections names the runs of keys the
// pane groups under a header, and settingsRows applies the rules that hold for every row alike —
// the mask over a secret, the word an empty block shows, the default a row falls back to.

import (
	"strconv"

	"github.com/airiclenz/apogee"
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

// The two registry paths whose value the ENGINE holds rather than the resolution — the pair
// overlayLiveSettings below replaces. They are spelled here for settingKeyMechanisms' reason: a row
// that has to be recognised at all is recognised by its path.
const (
	settingKeyMode               = "mode"
	settingKeyConfineToWorkspace = "confine-to-workspace"
	// And the row whose PROSE the pane seeds an editor from rather than shows (seedPromptEditor).
	settingKeySystemPromptText = "system-prompt-text"
)

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

// settingsRows builds the /settings pane's rows: one per registry key, in registry order (which is
// the config template's order), each carrying its section, its effective value, and the metadata
// the pane needs in order to render and later edit it.
//
// The value and the prose behind it are the ROW's answers (config.Key.Read, config.Key.Text) — a nil
// projection renders as an empty value rather than panicking, because a gap in the registry must not
// cost the user the whole surface mid-session. Three rules are applied HERE, on top of whatever the
// row answered, so no per-key projection can forget one: a masked key's value is replaced by the
// mask, a structured key with nothing in it reads "none", and a key whose effective value came out
// empty while the registry declares a default shows that default — an unset `cursor-shape:` is a
// block caret, and saying so is the honest answer to "what is this set to".
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
		if k.Read != nil {
			value = k.Read(opts)
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
		if k.Text != nil {
			text = k.Text(opts)
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

// runningSettings is the running session's answer for the two keys a projection of config.Options
// cannot answer honestly: the autonomy mode and Auto's blast radius are both moved DURING a session
// — Shift+Tab and the mode row move the first, /confine the second — and neither writes the config
// file, so rows built from the boot resolution go on reporting the values this run launched with.
// The pane would then mark a rung the session has left as "(current)" and re-apply it on ⏎.
//
// It is an interface rather than the holder itself for settingsEngine's reason (wire.go): the
// overlay is then exercisable against a fake with no Agent behind it, and this file goes on knowing
// nothing about the engine but the two questions it asks.
type runningSettings interface {
	Mode() apogee.Mode
	ConfineToWorkspace() bool
}

// overlayLiveSettings answers those two rows from the engine and leaves every other row exactly as
// the projection built it. It is the BINARY's overlay, beside the projection it corrects, because
// the value a row shows is composed on this side of the seam and the renderer holds neither the
// schema nor an engine mutator (ADR 0037 decision 2).
//
// A nil live overlays nothing: a Driver that composed the settings host without an engine (ADR
// 0031) still gets the file's own answers, which is what "no engine to ask" honestly reads as. The
// rows are copied rather than written through, so the projection a caller kept is never moved under
// it — the two lists are the FILE's answer and the SESSION's, and a test comparing them needs both.
func overlayLiveSettings(rows []tui.SettingRow, live runningSettings) []tui.SettingRow {
	if live == nil {
		return rows
	}
	overlaid := make([]tui.SettingRow, len(rows))
	copy(overlaid, rows)
	for i := range overlaid {
		switch overlaid[i].Path {
		case settingKeyMode:
			overlaid[i].Value = string(live.Mode())
		case settingKeyConfineToWorkspace:
			overlaid[i].Value = strconv.FormatBool(live.ConfineToWorkspace())
		}
	}
	return overlaid
}

// seedPromptEditor pre-fills the `system-prompt-text` row's prose with the embedded default when
// nothing is written for that key — the field ⏎ opens the multi-line editor over ([SettingRow.Text],
// ADR 0037 decision 10), so what the human starts editing is the prompt the session is actually
// sending instead of an empty buffer (ADR 0064 §1).
//
// It sits BESIDE the projection like overlayLiveSettings above, and for the same reason: this is the
// pane's row feed, not the file's answer. settingsRows keeps its blank-when-unset contract, so the
// external-edit diff (settingsedit.go) still compares two reads of config.yaml in the file's own
// spelling and never mistakes a seeded field for a prompt somebody wrote.
//
// An empty seed seeds nothing — the settings holder answers that way whenever the global prompt IS
// set, including by `system-prompt-file` alone — and a row that already carries prose is left alone,
// because a seed is what stands where nothing was written and never what replaces it. Only the ROW's
// prose moves: the value cell still summarizes what the config says, since the row's job is to
// report the setting and the seed's is to open an editor.
func seedPromptEditor(rows []tui.SettingRow, seed string) []tui.SettingRow {
	if seed == "" {
		return rows
	}
	seeded := make([]tui.SettingRow, len(rows))
	copy(seeded, rows)
	for i := range seeded {
		if seeded[i].Path == settingKeySystemPromptText && seeded[i].Text == "" {
			seeded[i].Text = seed
		}
	}
	return seeded
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
