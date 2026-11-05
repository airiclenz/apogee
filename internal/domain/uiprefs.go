package domain

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// UIPrefs is the `ui:` block as ONE value: what a Driver is configured to draw — the status-line
// spinner and its colour loop, the scroll bar, the palette, the quiet threshold, the Inspector's
// wire capture, the skill-suggestion band, the two fold states the transcript writes back, the
// glyph set the screen's symbols are drawn from, and how the terminal is told apogee waits on the
// human, and the rows the footer's segments are laid out in.
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
	// Glyphs names the set the screen's symbols are drawn from: [GlyphsUnicode], the look apogee
	// has always had, or [GlyphsSafe], which swaps every symbol a terminal font may lack for one
	// it carries. Default unicode, and `""` reads as unicode too — [ParseSpinnerStyle]'s posture,
	// so a block assembled field by field with Glyphs left zero (a one-key validation) still
	// validates. Any other name is refused by [UIPrefs.Validate] naming the key.
	Glyphs GlyphSet
	// Notify names how the terminal is told that apogee is waiting on the human: [NotifyBell], an
	// [NotifyOSC9] desktop notification, [NotifyBoth], or [NotifyOff]. Default bell, which arrives
	// only through [DefaultUIPrefs] and the shipped template: `""` reads as OFF, unlike Glyphs, so a
	// zero block — a test harness's, or a Driver that seeds no prefs — never rings the terminal.
	// Any other name is refused by [UIPrefs.Validate] naming the key.
	Notify NotifyMode
	// GitFetch lets the TUI run a periodic background `git fetch` in a git workspace, so the
	// footer's behind count sees upstream commits that have not been fetched yet. Default TRUE.
	// Unlike every other key of the block it reaches past the screen — a fetch touches the network
	// and writes remote-tracking refs — though nothing about it reaches the model. false keeps the
	// footer's counts to what the repository already knows.
	GitFetch bool
	// PointerShapes lets the TUI set the terminal's mouse-pointer shape over what a click there
	// does — an I-beam over text, a hand over a clickable row — through OSC 22, and only once the
	// terminal has answered a capability query for the shapes (ADR 0106). Default FALSE: the
	// feature ships off until real use turns it on, and off apogee writes no OSC 22 byte at all.
	// Purely visual — nothing about it reaches the model.
	PointerShapes bool
	// FooterRows names the rows the footer's segments are laid out in, top to bottom, and the
	// segments of each, left to right — carried in its one canonical spelling
	// (`[[host, model, effort, mode], [workdir, git, spend]]`) so the block stays comparable with
	// `==`, and read into rows through [ParseFooterRows]. Default two rows, the upstream above the
	// workspace; `""` reads as the default too, Glyphs' posture, so a block assembled field by field
	// still validates. A shape [ParseFooterRows] refuses is refused by [UIPrefs.Validate] naming
	// the key.
	FooterRows FooterRows
}

// FooterSegment names one segment of the footer, as `ui.footer.rows` spells it.
type FooterSegment string

const (
	// FooterHost is the upstream's host.
	FooterHost FooterSegment = "host"
	// FooterModel is the model the session talks to.
	FooterModel FooterSegment = "model"
	// FooterEffort is the reasoning effort.
	FooterEffort FooterSegment = "effort"
	// FooterMode is the permission-mode marker, which sits at the right edge of its row.
	FooterMode FooterSegment = "mode"
	// FooterWorkdir is the working directory.
	FooterWorkdir FooterSegment = "workdir"
	// FooterGit is the branch and its counts.
	FooterGit FooterSegment = "git"
	// FooterSpend is the session's spend.
	FooterSpend FooterSegment = "spend"
)

// footerSegmentNames is the vocabulary [ParseFooterRows] accepts, in the order its error lists them.
var footerSegmentNames = []FooterSegment{
	FooterHost, FooterModel, FooterEffort, FooterMode, FooterWorkdir, FooterGit, FooterSpend,
}

// maxFooterRows is how many rows the footer may be laid out in.
const maxFooterRows = 2

// FooterRows is `ui.footer.rows` in its one canonical spelling: the rows in brackets, each a
// bracketed list of segment names joined by `, ` — the text [SpellFooterRows] writes and
// [ParseFooterRows] reads back. A string rather than a slice so [UIPrefs] stays comparable.
type FooterRows string

// DefaultFooterRows is what a config that names no `ui.footer.rows:` resolves to: two rows, the
// upstream (host, model, effort and the mode marker at its right edge) above the workspace (workdir,
// git and spend).
const DefaultFooterRows FooterRows = "[[host, model, effort, mode], [workdir, git, spend]]"

// footerRowsShape is the refusal for text that is not a list of rows at all.
const footerRowsShape = "want a list of 1 or 2 rows of segment names, like [[host, model, effort, mode], " +
	"[workdir, git, spend]]"

// SpellFooterRows writes rows in the canonical spelling, judging nothing: a config file's rows are
// spelled as written so [UIPrefs.Validate] can quote them in its refusal.
func SpellFooterRows[S ~string](rows [][]S) FooterRows {
	spelled := make([]string, 0, len(rows))
	for _, row := range rows {
		names := make([]string, 0, len(row))
		for _, name := range row {
			names = append(names, string(name))
		}
		spelled = append(spelled, "["+strings.Join(names, ", ")+"]")
	}
	return FooterRows("[" + strings.Join(spelled, ", ") + "]")
}

// ParseFooterRows reads a `ui.footer.rows` value into its rows. "" (trimmed) ⇒ the default's rows.
// It refuses, in a sentence naming the key and quoting the text, text that is not a bracketed list of
// bracketed rows, fewer than 1 or more than 2 rows, an empty row, a name that is not a segment, a
// name given twice, and a list without `mode`.
func ParseFooterRows(text string) ([][]FooterSegment, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		text = string(DefaultFooterRows)
	}
	refuse := func(reason string) error {
		return fmt.Errorf("apogee: invalid ui.footer.rows %q: %s", text, reason)
	}
	names, ok := splitFooterRows(text)
	if !ok {
		return nil, refuse(footerRowsShape)
	}
	if len(names) == 0 || len(names) > maxFooterRows {
		return nil, refuse(fmt.Sprintf("want 1 or %d rows, not %d", maxFooterRows, len(names)))
	}
	rows, err := footerSegments(names)
	if err != nil {
		return nil, refuse(err.Error())
	}
	return rows, nil
}

// splitFooterRows cuts canonical-shaped text into its rows of trimmed names, answering false for
// text that is not `[` rows `]` with each row `[` names `]`. An empty row is kept, as no names.
func splitFooterRows(text string) ([][]string, bool) {
	inner, ok := strings.CutPrefix(text, "[")
	if !ok {
		return nil, false
	}
	inner, ok = strings.CutSuffix(inner, "]")
	if !ok {
		return nil, false
	}
	var rows [][]string
	rest := strings.TrimSpace(inner)
	for rest != "" {
		opened, ok := strings.CutPrefix(rest, "[")
		if !ok {
			return nil, false
		}
		body, after, ok := strings.Cut(opened, "]")
		if !ok || strings.Contains(body, "[") {
			return nil, false
		}
		rows = append(rows, footerRowNames(body))
		rest = strings.TrimSpace(after)
		if rest == "" {
			break
		}
		if rest, ok = strings.CutPrefix(rest, ","); !ok {
			return nil, false
		}
		rest = strings.TrimSpace(rest)
	}
	return rows, true
}

// footerRowNames splits one row's bracketed body into its trimmed names; a blank body is no names.
func footerRowNames(body string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	names := strings.Split(body, ",")
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	return names
}

// footerSegments judges the rows' names: no row empty, every name a segment, none given twice, and
// `mode` among them.
func footerSegments(names [][]string) ([][]FooterSegment, error) {
	seen := make(map[FooterSegment]bool, len(footerSegmentNames))
	rows := make([][]FooterSegment, 0, len(names))
	for i, row := range names {
		if len(row) == 0 {
			return nil, fmt.Errorf("row %d is empty; want at least one segment", i+1)
		}
		segments := make([]FooterSegment, 0, len(row))
		for _, name := range row {
			segment := FooterSegment(name)
			if !slices.Contains(footerSegmentNames, segment) {
				return nil, fmt.Errorf("unknown segment %q (known segments: %s)", name, footerSegmentList())
			}
			if seen[segment] {
				return nil, fmt.Errorf("segment %q is named twice; want each segment once", name)
			}
			seen[segment] = true
			segments = append(segments, segment)
		}
		rows = append(rows, segments)
	}
	if !seen[FooterMode] {
		return nil, fmt.Errorf("want %q in exactly one row", FooterMode)
	}
	return rows, nil
}

// footerSegmentList spells the segment vocabulary for a refusal.
func footerSegmentList() string {
	names := make([]string, 0, len(footerSegmentNames))
	for _, segment := range footerSegmentNames {
		names = append(names, string(segment))
	}
	return strings.Join(names, ", ")
}

// NotifyMode names how the terminal is told that apogee waits on the human. The vocabulary is
// declared once, here, so the config layer validates a `ui.notify:` value against the same names
// the renderer picks its signal by.
type NotifyMode string

const (
	// NotifyOff sends nothing.
	NotifyOff NotifyMode = "off"
	// NotifyBell writes the terminal bell (BEL).
	NotifyBell NotifyMode = "bell"
	// NotifyOSC9 writes an OSC 9 desktop notification carrying a short text.
	NotifyOSC9 NotifyMode = "osc9"
	// NotifyBoth writes the bell and the OSC 9 notification.
	NotifyBoth NotifyMode = "both"
)

// DefaultNotifyMode is what a config that names no `ui.notify:` resolves to. It is NOT what `""`
// parses to — see [ParseNotifyMode].
const DefaultNotifyMode = NotifyBell

// notifyModeNames is the vocabulary [ParseNotifyMode] accepts, in the order its error lists them.
var notifyModeNames = []NotifyMode{NotifyOff, NotifyBell, NotifyOSC9, NotifyBoth}

// NotifyModeNames returns the notify modes this build knows, in the order [ParseNotifyMode]'s error
// lists them. It hands back a fresh slice on every call, as [SpinnerStyleNames] does.
func NotifyModeNames() []NotifyMode { return slices.Clone(notifyModeNames) }

// ParseNotifyMode maps a value onto a notify mode. "" ⇒ [NotifyOff] — the zero value stays silent,
// so a block assembled field by field with Notify left zero validates and rings nothing; the
// shipped default is [DefaultUIPrefs]'s to carry, and [UIPrefs.Set] lands it for `""`. An unknown
// value is an error naming the modes this build knows. The caller names the key it read the value
// from, as for [ParseSpinnerStyle].
func ParseNotifyMode(s string) (NotifyMode, error) {
	if s == "" {
		return NotifyOff, nil
	}
	for _, mode := range notifyModeNames {
		if NotifyMode(s) == mode {
			return mode, nil
		}
	}
	names := make([]string, 0, len(notifyModeNames))
	for _, mode := range notifyModeNames {
		names = append(names, string(mode))
	}
	return "", fmt.Errorf("unknown notify mode %q (known modes: %s)", s, strings.Join(names, ", "))
}

// GlyphSet names the set of symbols the screen is drawn with. The vocabulary is declared once,
// here, so the config layer validates a `ui.glyphs:` value against the same names the renderer
// picks its symbols by.
type GlyphSet string

const (
	// GlyphsUnicode is the full set: the symbols apogee has always drawn.
	GlyphsUnicode GlyphSet = "unicode"
	// GlyphsSafe is the font-safe set, for a terminal font that lacks some of the unicode set.
	GlyphsSafe GlyphSet = "safe"
)

// DefaultGlyphSet is what an unset value resolves to: the look apogee has always had.
const DefaultGlyphSet = GlyphsUnicode

// glyphSetNames is the vocabulary [ParseGlyphSet] accepts, in the order its error lists them.
var glyphSetNames = []GlyphSet{GlyphsUnicode, GlyphsSafe}

// GlyphSetNames returns the glyph sets this build knows, in the order [ParseGlyphSet]'s error lists
// them. It hands back a fresh slice on every call, as [SpinnerStyleNames] does.
func GlyphSetNames() []GlyphSet { return slices.Clone(glyphSetNames) }

// ParseGlyphSet maps a value onto a glyph set. "" ⇒ the default; an unknown value is an error
// naming the sets this build knows. The caller names the key it read the value from, as for
// [ParseSpinnerStyle].
func ParseGlyphSet(s string) (GlyphSet, error) {
	if s == "" {
		return DefaultGlyphSet, nil
	}
	for _, set := range glyphSetNames {
		if GlyphSet(s) == set {
			return set, nil
		}
	}
	names := make([]string, 0, len(glyphSetNames))
	for _, set := range glyphSetNames {
		names = append(names, string(set))
	}
	return "", fmt.Errorf("unknown glyph set %q (known sets: %s)", s, strings.Join(names, ", "))
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
	UIKeyGlyphs           = "ui.glyphs"
	UIKeyShowScrollbar    = "ui.show-scrollbar"
	UIKeyColorScheme      = "ui.color-scheme"
	UIKeyStallAfter       = "ui.stall-after"
	UIKeyInspector        = "ui.inspector"
	UIKeySkillSuggestions = "ui.skill-suggestions"
	UIKeyTaskListOpen     = "ui.task-list-open"
	UIKeyToolsOpen        = "ui.tools-open"
	UIKeyToolsFoldOver    = "ui.tools-fold-over"
	UIKeyNotify           = "ui.notify"
	UIKeyGitFetch         = "ui.git-fetch"
	UIKeyPointerShapes    = "ui.pointer-shapes"
	// UIKeyFooterRows is a key of the nested `footer:` block, and like every key of that block it
	// is not one of [UIKeys]: it is a structured value, edited in the file rather than on a row.
	UIKeyFooterRows = "ui.footer.rows"
)

// uiKeys is the block's keys in the order the starter template presents them, declared once so a
// caller sweeping the block reads it in the order the user edits it.
var uiKeys = []string{
	UIKeySpinner, UIKeySpinnerColor, UIKeyGlyphs, UIKeyShowScrollbar, UIKeyColorScheme,
	UIKeyStallAfter, UIKeyInspector, UIKeySkillSuggestions, UIKeyTaskListOpen, UIKeyToolsOpen,
	UIKeyToolsFoldOver, UIKeyPointerShapes, UIKeyNotify, UIKeyGitFetch,
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
// the Inspector disarmed, the skill-suggestion band on, the task-list cards open, the Tools
// umbrella folding past five type rows with large umbrellas starting folded, pointer shapes off,
// the unicode glyph set, the terminal bell when apogee waits on the human, the footer's
// periodic git fetch on, and the footer laid out on one row.
// Every `""` handed to [UIPrefs.Set] lands the field this value carries for that key.
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
		Glyphs:           DefaultGlyphSet,
		Notify:           DefaultNotifyMode,
		GitFetch:         true,
		FooterRows:       DefaultFooterRows,
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
// below zero, a ToolsFoldOver below zero, a glyph set this build does not know, a notify mode it
// does not know, or footer rows [ParseFooterRows] refuses — the same six refusals the parsers make, so a block assembled field by field is judged as one read through
// [UIPrefs.Set] would be. Catching them here makes a typo a startup error that names the key; left
// to the renderer they would silently resolve to some other style, the shipped threshold or the
// unicode glyphs, and the user would be left wondering why their setting did nothing.
func (u UIPrefs) Validate() error {
	if _, err := ParseSpinnerStyle(string(u.Spinner)); err != nil {
		return fmt.Errorf("apogee: invalid ui.spinner: %w", err)
	}
	if err := validStallAfter(u.StallAfter); err != nil {
		return err
	}
	if _, err := ParseGlyphSet(string(u.Glyphs)); err != nil {
		return fmt.Errorf("apogee: invalid ui.glyphs: %w", err)
	}
	if _, err := ParseNotifyMode(string(u.Notify)); err != nil {
		return fmt.Errorf("apogee: invalid ui.notify: %w", err)
	}
	if _, err := ParseFooterRows(string(u.FooterRows)); err != nil {
		return err
	}
	return validToolsFoldOver(u.ToolsFoldOver)
}

// Set lands one key's value, spelled as the config file spells it, onto the block: the text is
// trimmed, `""` lands that key's default (what a fresh start resolves from a file that does not
// carry the key), and the value is parsed per key — the spinner through [ParseSpinnerStyle], the
// glyph set through [ParseGlyphSet], the notify mode through [ParseNotifyMode], the footer rows
// through [ParseFooterRows] (landing their canonical spelling), the bools through strconv.ParseBool, the threshold and the count through their parsers, the scheme
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
	case UIKeyGlyphs:
		set, err := ParseGlyphSet(value)
		if err != nil {
			return fmt.Errorf("apogee: invalid ui.glyphs: %w", err)
		}
		u.Glyphs = set
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
	case UIKeyNotify:
		// `""` lands the shipped default, the contract every key keeps here — not the off that
		// ParseNotifyMode reads the zero value as.
		if value == "" {
			u.Notify = defaults.Notify
			return nil
		}
		mode, err := ParseNotifyMode(value)
		if err != nil {
			return fmt.Errorf("apogee: invalid ui.notify: %w", err)
		}
		u.Notify = mode
	case UIKeyGitFetch:
		return landBool(&u.GitFetch, key, value, defaults.GitFetch)
	case UIKeyPointerShapes:
		return landBool(&u.PointerShapes, key, value, defaults.PointerShapes)
	case UIKeyFooterRows:
		rows, err := ParseFooterRows(value)
		if err != nil {
			return err
		}
		u.FooterRows = SpellFooterRows(rows)
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
