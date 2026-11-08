package domain

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// SidePanelShow is `ui.side-panel.show`: when the side panel's column is drawn beside the
// transcript (ADR 0110 decision 8). The vocabulary is declared once, here, so the config layer
// validates a value against the same names the renderer gates the column by.
type SidePanelShow string

const (
	// SidePanelShowAuto draws the column while the transcript keeps its comfortable width beside it.
	SidePanelShowAuto SidePanelShow = "auto"
	// SidePanelShowOn draws the column whenever the transcript keeps its floor width beside it.
	SidePanelShowOn SidePanelShow = "on"
	// SidePanelShowOff never draws the column.
	SidePanelShowOff SidePanelShow = "off"
)

// DefaultSidePanelShow is what a config that names no `ui.side-panel.show:` resolves to, and what
// `""` reads as — Glyphs' posture, so a block assembled field by field still validates.
const DefaultSidePanelShow = SidePanelShowAuto

// sidePanelShowNames is the vocabulary [ParseSidePanelShow] accepts, in the order its error lists
// them.
var sidePanelShowNames = []SidePanelShow{SidePanelShowAuto, SidePanelShowOn, SidePanelShowOff}

// SidePanelShowNames returns the show modes this build knows, in the order [ParseSidePanelShow]'s
// error lists them. It hands back a fresh slice on every call, as [SpinnerStyleNames] does.
func SidePanelShowNames() []SidePanelShow { return slices.Clone(sidePanelShowNames) }

// ParseSidePanelShow maps a value onto a show mode. "" ⇒ the default; an unknown value is an error
// naming the modes this build knows. The caller names the key it read the value from, as for
// [ParseSpinnerStyle].
func ParseSidePanelShow(s string) (SidePanelShow, error) {
	if s == "" {
		return DefaultSidePanelShow, nil
	}
	if slices.Contains(sidePanelShowNames, SidePanelShow(s)) {
		return SidePanelShow(s), nil
	}
	names := make([]string, 0, len(sidePanelShowNames))
	for _, mode := range sidePanelShowNames {
		names = append(names, string(mode))
	}
	return "", fmt.Errorf("unknown side-panel mode %q (known modes: %s)", s, strings.Join(names, ", "))
}

// DefaultSidePanelWidth is the side panel's width in columns for a config that names no
// `ui.side-panel.width:`, and what a zero [UIPrefs.SidePanelWidth] resolves to.
const DefaultSidePanelWidth = 36

// MinSidePanelWidth is the narrowest `ui.side-panel.width` a config may state: below it a section's
// header and its rows no longer fit beside their marks.
const MinSidePanelWidth = 24

// ParseSidePanelWidth reads a `ui.side-panel.width:` value: a number of columns, at least
// [MinSidePanelWidth], or — trimmed — the empty value, which is the key's way of saying "the
// default". Text that is no number, and a width below the minimum (0 included: a stated width is
// a width, and the "unset" a zero field carries is a block's, never a value's), are refused in the
// sentences every surface that reads the key refuses them in.
func ParseSidePanelWidth(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return DefaultSidePanelWidth, nil
	}
	width, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("apogee: invalid ui.side-panel.width %q: want a number of columns, %d or more",
			text, MinSidePanelWidth)
	}
	if width < MinSidePanelWidth {
		return 0, sidePanelTooNarrow(width)
	}
	return width, nil
}

// validSidePanelWidth is [UIPrefs.Validate]'s judgement on the width: 0 is the unset field, which
// resolves to [DefaultSidePanelWidth], so only a stated width below the minimum is refused — in
// the parser's sentence.
func validSidePanelWidth(width int) error {
	if width != 0 && width < MinSidePanelWidth {
		return sidePanelTooNarrow(width)
	}
	return nil
}

// sidePanelTooNarrow is the one refusal of a width below [MinSidePanelWidth], spoken by the parser
// and by Validate alike.
func sidePanelTooNarrow(width int) error {
	return fmt.Errorf("apogee: invalid ui.side-panel.width %d: want %d columns or more",
		width, MinSidePanelWidth)
}

// SidePanelSection names one section of the side panel, as `ui.side-panel.sections` spells it.
type SidePanelSection string

const (
	// SidePanelSubAgents is the sub-agents section: the delegations of this session.
	SidePanelSubAgents SidePanelSection = "sub-agents"
	// SidePanelTasks is the task-list section.
	SidePanelTasks SidePanelSection = "tasks"
	// SidePanelChangedFiles is the section naming the files changed this session.
	SidePanelChangedFiles SidePanelSection = "changed-files"
	// SidePanelUsage is the usage section: the context fill and the spend.
	SidePanelUsage SidePanelSection = "usage"
)

// sidePanelSectionNames is the vocabulary [ParseSidePanelSections] accepts, in the default display
// order — the order a section the list leaves out is appended in.
var sidePanelSectionNames = []SidePanelSection{
	SidePanelSubAgents, SidePanelTasks, SidePanelChangedFiles, SidePanelUsage,
}

// SidePanelSectionNames returns the sections this build knows, in their default display order. It
// hands back a fresh slice on every call, as [SpinnerStyleNames] does.
func SidePanelSectionNames() []SidePanelSection { return slices.Clone(sidePanelSectionNames) }

// SidePanelMode is one section's mode (ADR 0110 decision 9).
type SidePanelMode string

const (
	// SidePanelModePinned shows the section always.
	SidePanelModePinned SidePanelMode = "pinned"
	// SidePanelModeAuto shows the section while its live rule holds.
	SidePanelModeAuto SidePanelMode = "auto"
	// SidePanelModeHidden never shows the section.
	SidePanelModeHidden SidePanelMode = "hidden"
)

// sidePanelModeNames is the mode vocabulary, in the order a refusal lists it.
var sidePanelModeNames = []SidePanelMode{SidePanelModePinned, SidePanelModeAuto, SidePanelModeHidden}

// SidePanelEntry is one entry of `ui.side-panel.sections`: a section and its mode. An entry spelled
// from a config file holds the names as written, unjudged, until [ParseSidePanelSections] reads it.
type SidePanelEntry struct {
	// Section names the section.
	Section SidePanelSection
	// Mode is the section's mode.
	Mode SidePanelMode
}

// SidePanelSections is `ui.side-panel.sections` in its one canonical spelling: the entries in
// brackets, each `section: mode`, joined by `, ` — the text [SpellSidePanelSections] writes and
// [ParseSidePanelSections] reads back, and a YAML flow list the config file takes as written. A
// string rather than a slice so [UIPrefs] stays comparable with `==`.
type SidePanelSections string

// DefaultSidePanelSections is what a config that names no `ui.side-panel.sections:` resolves to:
// every section in auto, in the default display order.
const DefaultSidePanelSections SidePanelSections = "[sub-agents: auto, tasks: auto, changed-files: auto, usage: auto]"

// sidePanelSectionsShape is the refusal for text that is not a list of entries at all.
const sidePanelSectionsShape = "want a list of section: mode entries, like " +
	"[sub-agents: auto, tasks: pinned, changed-files: auto, usage: hidden]"

// SpellSidePanelSections writes entries in the canonical spelling, judging nothing: a config file's
// list is spelled through it as written so [ParseSidePanelSections] can quote it in its refusal.
func SpellSidePanelSections(entries []SidePanelEntry) SidePanelSections {
	spelled := make([]string, 0, len(entries))
	for _, entry := range entries {
		spelled = append(spelled, string(entry.Section)+": "+string(entry.Mode))
	}
	return SidePanelSections("[" + strings.Join(spelled, ", ") + "]")
}

// ParseSidePanelSections reads a `ui.side-panel.sections` value into every section this build knows
// with its mode, in display order. "" (trimmed) ⇒ the default. The entries the text names come
// first, in its order; a section it leaves out follows them in auto, in the default order — so a
// list written before a section existed still loads, and the new section shows while it is live.
// It refuses, in a sentence naming the key and quoting the text, text that is not a bracketed list
// of `section: mode` entries, an entry naming no mode, a name that is not a section, a mode that is
// not pinned, auto or hidden, and a section given twice.
func ParseSidePanelSections(text string) ([]SidePanelEntry, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		text = string(DefaultSidePanelSections)
	}
	refuse := func(reason string) error {
		return fmt.Errorf("apogee: invalid ui.side-panel.sections %q: %s", text, reason)
	}
	pairs, ok := splitSidePanelSections(text)
	if !ok {
		return nil, refuse(sidePanelSectionsShape)
	}
	entries, err := sidePanelEntries(pairs)
	if err != nil {
		return nil, refuse(err.Error())
	}
	return entries, nil
}

// splitSidePanelSections cuts bracketed text into its trimmed `section: mode` pairs, answering
// false for text that is not `[` entries `]`, or an entry without its colon.
func splitSidePanelSections(text string) ([][2]string, bool) {
	inner, ok := strings.CutPrefix(text, "[")
	if !ok {
		return nil, false
	}
	inner, ok = strings.CutSuffix(inner, "]")
	if !ok || strings.ContainsAny(inner, "[]{}") {
		return nil, false
	}
	if strings.TrimSpace(inner) == "" {
		return nil, true
	}
	items := strings.Split(inner, ",")
	pairs := make([][2]string, 0, len(items))
	for _, item := range items {
		name, mode, ok := strings.Cut(item, ":")
		if !ok {
			return nil, false
		}
		pairs = append(pairs, [2]string{strings.TrimSpace(name), strings.TrimSpace(mode)})
	}
	return pairs, true
}

// sidePanelEntries judges the pairs — every name a section, every mode one of the three, no section
// twice — and appends, in auto, each section they leave out.
func sidePanelEntries(pairs [][2]string) ([]SidePanelEntry, error) {
	entries := make([]SidePanelEntry, 0, len(sidePanelSectionNames))
	for _, pair := range pairs {
		section, mode := SidePanelSection(pair[0]), SidePanelMode(pair[1])
		if !slices.Contains(sidePanelSectionNames, section) {
			return nil, fmt.Errorf("unknown section %q (known sections: %s)", pair[0],
				joinNames(sidePanelSectionNames))
		}
		if mode == "" {
			return nil, fmt.Errorf("section %q names no mode; want %s", pair[0], joinNames(sidePanelModeNames))
		}
		if !slices.Contains(sidePanelModeNames, mode) {
			return nil, fmt.Errorf("unknown mode %q for section %q (known modes: %s)", pair[1], pair[0],
				joinNames(sidePanelModeNames))
		}
		if slices.ContainsFunc(entries, func(e SidePanelEntry) bool { return e.Section == section }) {
			return nil, fmt.Errorf("section %q is named twice; want each section once", pair[0])
		}
		entries = append(entries, SidePanelEntry{Section: section, Mode: mode})
	}
	for _, section := range sidePanelSectionNames {
		if !slices.ContainsFunc(entries, func(e SidePanelEntry) bool { return e.Section == section }) {
			entries = append(entries, SidePanelEntry{Section: section, Mode: SidePanelModeAuto})
		}
	}
	return entries, nil
}

// joinNames spells a vocabulary for a refusal, `, ` between the names.
func joinNames[S ~string](names []S) string {
	spelled := make([]string, 0, len(names))
	for _, name := range names {
		spelled = append(spelled, string(name))
	}
	return strings.Join(spelled, ", ")
}
