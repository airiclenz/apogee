package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// UIPrefs is the `ui:` block as ONE value: what a Driver is configured to draw — the status-line
// spinner and its colour loop, the scroll bar, the palette, the quiet threshold, the Inspector's
// wire capture, the skill-suggestion band, and the two fold states the transcript writes back.
// It is one struct because the keys describe one subsystem and travel together, from the on-disk
// block through resolution (internal/config) to the wire (tui.Options) to the live apply — and it
// lives here, beside the vocabularies its fields are spelled in, so every layer that carries the
// block reads and writes the same value through the same parser ([UIPrefs.Set]) rather than each
// keeping a mapper of its own (ADR 0043).
//
// Every bool spells config's POSITIVE polarity (ShowScrollbar, TaskListOpen): a renderer that wants
// "hide" or "folded" negates at the point of use. The zero value is NOT the default — a Driver
// seeds from [DefaultUIPrefs] — and nothing downstream special-cases it.
//
// The two spinner keys are deliberately INDEPENDENT. The colour loop is not a property of a style
// and is not folded into the style name: it applies to whichever style Spinner names, so all three
// styles × colour on/off are valid combinations. Nothing here or downstream may key one off the
// other.
type UIPrefs struct {
	// Spinner names the status-line animation. An unknown style is refused by [UIPrefs.Validate]
	// naming the key, rather than silently falling back to the default.
	Spinner SpinnerStyle
	// SpinnerColor runs the slow colour loop over whichever style Spinner names. Default true;
	// false leaves the glyph in the terminal's own text colour, which is the escape hatch for a
	// terminal whose colour depth turns the gradient into steps.
	SpinnerColor bool
	// ShowScrollbar paints the transcript's scroll bar and reserves the column it hangs in. Default
	// true; false takes both away together — a hidden bar that still ate a column would read as a
	// bug — and the transcript body takes that width instead. It is process-constant, so the wrap
	// width it decides never changes mid-run.
	ShowScrollbar bool
	// ColorScheme names the palette every coloured thing on the screen takes its colour from: a
	// built-in (dark, light) or a `<apogee-home>/schemes/<name>.yaml` the user wrote, which shadows
	// a built-in of the same name. It is carried as a NAME rather than a resolved palette because
	// resolution reads a file, which the composition root does at wiring time — and, unlike
	// Spinner, an unknown name is deliberately NOT a startup error: a scheme is cosmetic, so a typo
	// costs a warning and the default palette rather than the session (ADR 0040 design call 8).
	ColorScheme string
	// StallAfter is how long the ENGINE may go silent mid-turn before the status line says so: past
	// it the running phrase gains a bare `quiet` qualifier in front of its clock, which is the honest
	// fact rather than a verdict — a slow turn and a dead one look identical from out here, and only
	// the human can tell them apart. Default 120s, which clears the ingestion of a large prompt
	// (legitimately silent for a minute or two on a local model); 0 turns it off. A DURATION rather
	// than the text it was spelled in, because the renderer takes a duration and nothing downstream
	// would gain from a second parse of the same text.
	StallAfter time.Duration
	// Inspector arms the Inspector's wire capture: with it on, the engine reports the raw request
	// and response bytes of every model call as WireEvents, which `/inspect` shows. Default FALSE,
	// and the off-state is a true zero — nothing is captured, accumulated or emitted — so the key
	// costs a session that leaves it alone exactly nothing. It is read at STARTUP only (the
	// observer is installed while the engine is constructed), so a mid-session edit takes effect at
	// the next start.
	Inspector bool
	// SkillSuggestions paints the suggestion band above the input box: while the user types, the
	// catalog's skills are ranked against the draft and the closest are named there (ADR 0061).
	// Default TRUE, and the whole of what it gates is on the screen — nothing about the catalog
	// reaches the model either way, so this key changes what the human is offered and never what
	// the session sends. false leaves the band unpainted and the Tab that opens it inert.
	SkillSuggestions bool
	// TaskListOpen is whether the transcript's task-list cards start open (every row painted) or
	// folded to their counted header. Default TRUE. ONE shared state for every task-list card in
	// the transcript: the renderer applies it to itself, and the fold gesture on any card writes
	// the flip back silently through the settings seam, so the choice outlives the session
	// (ADR 0035 addendum). Screen-only, like SkillSuggestions: nothing about it reaches the model.
	TaskListOpen bool
	// ToolsOpen is whether a LARGE Tools umbrella in the transcript — one with more type rows than
	// ToolsFoldOver, by size alone, whatever the Turn is doing — starts open or folded to its
	// counted `✦ Tools (N calls)` header. Default FALSE: a large umbrella is a burst of calls, and
	// folding it out of the box is the point. ONE shared state for every large umbrella, and
	// TaskListOpen's whole posture otherwise. A small umbrella never reads it: it folds on its own
	// session-only flag, which is written nowhere (ADR 0035, 2026-09-18 addendum).
	ToolsOpen bool
	// ToolsFoldOver is how many type rows a Tools umbrella may show before it counts as large and
	// obeys ToolsOpen. Default 5. 0 is the documented "never": no umbrella has fewer than zero rows,
	// so none is ever large and every one folds on its own session-only flag alone. Negative is
	// meaningless and Validate refuses it.
	ToolsFoldOver int
}

// DefaultColorSchemeName is the built-in scheme every fallback lands on: the palette apogee has
// always drawn with. internal/scheme reads its DefaultName from here so the default a config
// resolves to and the palette the loader falls back on are one name.
const DefaultColorSchemeName = "dark"

// DefaultStallAfter is the quiet threshold a config that names no `ui.stall-after:` resolves to:
// long enough that ingesting a large prompt never trips it, short enough that a turn which really
// has died is named while the human is still at the screen.
const DefaultStallAfter = 120 * time.Second

// DefaultToolsFoldOver is how many type rows a Tools umbrella may show before it is large, for a
// config that names no `ui.tools-fold-over:`.
const DefaultToolsFoldOver = 5

// The `ui:` block's keys as the config file spells them, dotted under their block — the one
// spelling [UIPrefs.Set] takes, the settings surface offers and the registry rows are described by.
const (
	UIKeySpinner          = "ui.spinner"
	UIKeySpinnerColor     = "ui.spinner-color"
	UIKeyShowScrollbar    = "ui.show-scrollbar"
	UIKeyColorScheme      = "ui.color-scheme"
	UIKeyStallAfter       = "ui.stall-after"
	UIKeyInspector        = "ui.inspector"
	UIKeySkillSuggestions = "ui.skill-suggestions"
	UIKeyTaskListOpen     = "ui.task-list-open"
	UIKeyToolsOpen        = "ui.tools-open"
	UIKeyToolsFoldOver    = "ui.tools-fold-over"
)

// uiKeys is the block's keys in the order the starter template presents them, declared once so a
// caller sweeping the block reads it in the order the user edits it.
var uiKeys = []string{
	UIKeySpinner, UIKeySpinnerColor, UIKeyShowScrollbar, UIKeyColorScheme, UIKeyStallAfter,
	UIKeyInspector, UIKeySkillSuggestions, UIKeyTaskListOpen, UIKeyToolsOpen, UIKeyToolsFoldOver,
}

// UIKeys returns the `ui:` block's keys in template order. It hands back a fresh slice on every
// call, as [SpinnerStyleNames] does, so a caller cannot reorder the one this package reads.
func UIKeys() []string {
	keys := make([]string, len(uiKeys))
	copy(keys, uiKeys)
	return keys
}

// DefaultUIPrefs is the `ui:` block with nothing configured: the default spinner style with the
// colour loop on, the scroll bar shown, the default colour scheme, the shipped quiet threshold,
// the Inspector disarmed, the skill-suggestion band on, the task-list cards open, and the Tools
// umbrella folding past five type rows with large umbrellas starting folded. Every `""` handed to
// [UIPrefs.Set] lands the field this value carries for that key.
func DefaultUIPrefs() UIPrefs {
	return UIPrefs{
		Spinner:          DefaultSpinnerStyle,
		SpinnerColor:     true,
		ShowScrollbar:    true,
		ColorScheme:      DefaultColorSchemeName,
		StallAfter:       DefaultStallAfter,
		SkillSuggestions: true,
		TaskListOpen:     true,
		ToolsFoldOver:    DefaultToolsFoldOver,
	}
}

// ParseStallAfter reads a `ui.stall-after:` value: a length of time as time.ParseDuration spells
// it (`90s`, `2m`), `0` to turn the quiet qualifier off, or — trimmed — the empty value, which is
// the key's way of saying "the default". Text no duration can be made of, and a negative wait,
// are refused in the sentences every surface that reads the key refuses them in, quoting the text
// as it was written.
func ParseStallAfter(text string) (time.Duration, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return DefaultStallAfter, nil
	}
	after, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("apogee: invalid ui.stall-after %q: want a length of time like 90s or 2m, "+
			"or 0 to turn the quiet qualifier off", text)
	}
	if err := validStallAfter(after); err != nil {
		return 0, err
	}
	return after, nil
}

// ParseToolsFoldOver reads a `ui.tools-fold-over:` value: a number of type rows, `0` for "never
// folds", or — trimmed — the empty value, which is the key's way of saying "the default". Text
// that is no number, and a count below zero, are refused in the sentences every surface that reads
// the key refuses them in.
func ParseToolsFoldOver(text string) (int, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return DefaultToolsFoldOver, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("apogee: invalid ui.tools-fold-over %q: want a number of type rows "+
			"(0 never folds)", text)
	}
	if err := validToolsFoldOver(n); err != nil {
		return 0, err
	}
	return n, nil
}

// Validate rejects a block naming a spinner style this build has no animation for, a StallAfter
// below zero, or a ToolsFoldOver below zero — the same three refusals the parsers make, so a
// block assembled field by field is judged as one read through [UIPrefs.Set] would be. Catching
// them here makes a typo a startup error that names the key; left to the renderer they would
// silently resolve to some other style and to the shipped threshold, and the user would be left
// wondering why their setting did nothing.
func (u UIPrefs) Validate() error {
	if _, err := ParseSpinnerStyle(string(u.Spinner)); err != nil {
		return fmt.Errorf("apogee: invalid ui.spinner: %w", err)
	}
	if err := validStallAfter(u.StallAfter); err != nil {
		return err
	}
	return validToolsFoldOver(u.ToolsFoldOver)
}

// Set lands one key's value, spelled as the config file spells it, onto the block: the text is
// trimmed, `""` lands that key's default (what a fresh start resolves from a file that does not
// carry the key), and the value is parsed per key — the spinner through [ParseSpinnerStyle], the
// bools through strconv.ParseBool, the threshold and the count through their parsers, the scheme
// name as it is. The edit runs on a COPY, [UIPrefs.Validate] judges the copy, and only a copy it
// accepts is written back: a Set that fails has touched nothing. A key this block does not carry
// is refused.
func (u *UIPrefs) Set(key, value string) error {
	value = strings.TrimSpace(value)
	edited := *u
	if err := edited.land(key, value); err != nil {
		return err
	}
	if err := edited.Validate(); err != nil {
		return err
	}
	*u = edited
	return nil
}

// land writes one key's parsed value onto the receiver, `""` landing the key's default. It is
// Set's edit without Set's guard, so it only ever runs on the copy Set holds.
func (u *UIPrefs) land(key, value string) error {
	defaults := DefaultUIPrefs()
	switch key {
	case UIKeySpinner:
		style, err := ParseSpinnerStyle(value)
		if err != nil {
			return fmt.Errorf("apogee: invalid ui.spinner: %w", err)
		}
		u.Spinner = style
	case UIKeySpinnerColor:
		return landBool(&u.SpinnerColor, key, value, defaults.SpinnerColor)
	case UIKeyShowScrollbar:
		return landBool(&u.ShowScrollbar, key, value, defaults.ShowScrollbar)
	case UIKeyColorScheme:
		u.ColorScheme = value
		if value == "" {
			u.ColorScheme = defaults.ColorScheme
		}
	case UIKeyStallAfter:
		after, err := ParseStallAfter(value)
		if err != nil {
			return err
		}
		u.StallAfter = after
	case UIKeyInspector:
		return landBool(&u.Inspector, key, value, defaults.Inspector)
	case UIKeySkillSuggestions:
		return landBool(&u.SkillSuggestions, key, value, defaults.SkillSuggestions)
	case UIKeyTaskListOpen:
		return landBool(&u.TaskListOpen, key, value, defaults.TaskListOpen)
	case UIKeyToolsOpen:
		return landBool(&u.ToolsOpen, key, value, defaults.ToolsOpen)
	case UIKeyToolsFoldOver:
		n, err := ParseToolsFoldOver(value)
		if err != nil {
			return err
		}
		u.ToolsFoldOver = n
	default:
		return fmt.Errorf("apogee: unknown ui key %q", key)
	}
	return nil
}

// landBool parses one bool key's value onto its field, `""` landing the default, refusing in a
// sentence that names the key.
func landBool(field *bool, key, value string, fallback bool) error {
	if value == "" {
		*field = fallback
		return nil
	}
	v, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("apogee: invalid %s %q: want true or false", key, value)
	}
	*field = v
	return nil
}

// validStallAfter is the one range judgement on the quiet threshold, spoken by the parser and by
// Validate in one sentence.
func validStallAfter(after time.Duration) error {
	if after < 0 {
		return fmt.Errorf("apogee: invalid ui.stall-after %s: want 0 or more, where 0 turns the "+
			"quiet qualifier off", after)
	}
	return nil
}

// validToolsFoldOver is the one range judgement on the fold-over count, spoken by the parser and by
// Validate in one sentence.
func validToolsFoldOver(n int) error {
	if n < 0 {
		return fmt.Errorf("apogee: invalid ui.tools-fold-over %d: want 0 or more, where 0 never "+
			"folds a Tools umbrella", n)
	}
	return nil
}
