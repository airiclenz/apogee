package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/airiclenz/apogee/internal/session"
)

// Storyboard is one clip's storyboard (`graphics/demo/storyboards/<clip>.yaml`): the single
// source of a clip. The schema is documented in graphics/demo/README.md, "Storyboards". The
// header names the clip, where it ships, the cassette its model replies replay from, the font
// directory and the terminal frame; each beat says what the recorder does (Do), how long the
// beat lasts on screen (Duration, Hold, Cut, Zoom) and what a keeper take must show (Expect).
// Load is the only way to get one: a Storyboard in hand has passed validation, and its
// Ship, Cassette and Fonts are already resolved against the file's directory.
type Storyboard struct {
	// Path is the storyboard file Load read, as the caller named it.
	Path string `yaml:"-"`
	Clip string `yaml:"clip"`
	// Ship, Cassette and Fonts are written relative to the storyboard's own directory and
	// resolved by Load, so a caller in any working directory finds them.
	Ship     string     `yaml:"ship"`
	Cassette string     `yaml:"cassette"`
	Fonts    string     `yaml:"fonts"`
	Frame    Frame      `yaml:"frame"`
	Expect   TakeExpect `yaml:"expect"`
	Beats    []Beat     `yaml:"beats"`
}

// Frame is the recorded terminal and the shipped geometry: the pty's Cols × Rows, the
// rasterizer's Padding (pixels), FontSize (points) and LineHeight (a multiple of the font
// size), the Scale frames are rasterized at, the shipped GIF's Width in pixels, its FPS and
// its palette size. Pixel size is derived from the font metrics by the rasterizer, not here.
type Frame struct {
	Cols       int     `yaml:"cols"`
	Rows       int     `yaml:"rows"`
	Padding    int     `yaml:"padding"`
	FontSize   float64 `yaml:"font_size"`
	LineHeight float64 `yaml:"line_height"`
	Scale      int     `yaml:"scale"`
	Width      int     `yaml:"width"`
	FPS        int     `yaml:"fps"`
	MaxColors  int     `yaml:"max_colors"`
}

// Beat is one storyboard beat: the actions the recorder performs, the section's target
// Duration in the render (Hold is its leading stretch shown at 1×; a beat with no hold has
// hold 0), an optional Zoom onto an on-screen target, and what a keeper shows. A Cut beat is
// recorded but dropped from the render, so its Duration and Hold are not required.
type Beat struct {
	ID       int           `yaml:"id"`
	Title    string        `yaml:"title"`
	Why      string        `yaml:"why"`
	Notes    string        `yaml:"notes"`
	Do       []Action      `yaml:"do"`
	Duration time.Duration `yaml:"duration"`
	Hold     time.Duration `yaml:"hold"`
	Cut      bool          `yaml:"cut"`
	Zoom     *Zoom         `yaml:"zoom"`
	Expect   []Expect      `yaml:"expect"`
}

// Zoom is a push into an on-screen target: Factor is the magnification — absent (0), the
// compositor fits it to the target — and In and Out the ramps, 400 ms each when absent.
type Zoom struct {
	Target Target        `yaml:"target"`
	Factor float64       `yaml:"factor"`
	In     time.Duration `yaml:"in"`
	Out    time.Duration `yaml:"out"`
}

// Action is one step of a beat's `do` list. Exactly one of its forms is set.
type Action struct {
	Type  *TypeAction  `yaml:"type"`
	Key   *KeyAction   `yaml:"key"`
	Click *ClickAction `yaml:"click"`
	Wait  *WaitAction  `yaml:"wait"`
	Pause *PauseAction `yaml:"pause"`
}

// TypeAction types Text, with the humanized keystroke profile unless Humanize is false.
type TypeAction struct {
	Text string `yaml:"text"`
	// Humanize is a pointer so an absent key reads as the default, true.
	Humanize *bool `yaml:"humanize"`
}

// IsHumanized reports whether the text is typed with the humanized profile: true unless the
// storyboard sets `humanize: false`.
func (t TypeAction) IsHumanized() bool { return t.Humanize == nil || *t.Humanize }

// KeyAction presses the named key Repeat times; a Repeat of 0 presses it once.
type KeyAction struct {
	Name   string `yaml:"name"`
	Repeat int    `yaml:"repeat"`
}

// ClickAction clicks the resolved Target Times times; a Times of 0 clicks once.
type ClickAction struct {
	Target Target `yaml:"target"`
	Times  int    `yaml:"times"`
}

// WaitAction blocks until the Screen regex matches the terminal (or, with Gone, no longer
// matches) and fails the beat once Timeout passes.
type WaitAction struct {
	Screen  string        `yaml:"screen"`
	Gone    bool          `yaml:"gone"`
	Timeout time.Duration `yaml:"timeout"`
}

// MaxWaitTimeout bounds a wait's timeout, so a storyboard typo cannot stall a take for long.
const MaxWaitTimeout = 180 * time.Second

// PauseAction idles for For.
type PauseAction struct {
	For time.Duration `yaml:"for"`
}

// Target is an on-screen place a click or zoom resolves on the current screen: Text is a
// regex over the screen's rows, Nth picks which match counts and Area limits the rows searched
// (empty means any).
type Target struct {
	Text string    `yaml:"text"`
	Nth  TargetNth `yaml:"nth"`
	Area string    `yaml:"area"`
}

// The screen areas a target may be confined to.
const (
	AreaFooter     = "footer"
	AreaStatus     = "status"
	AreaTranscript = "transcript"
	AreaAny        = "any"
)

// targetAreas are the spellings Target.Area accepts besides empty.
var targetAreas = []string{AreaFooter, AreaStatus, AreaTranscript, AreaAny}

// TargetNth picks which of a target's matches counts: the zero value and TargetFirst are the
// first match, a positive value the Nth, and TargetLast the last one. It is its own scalar
// rather than Nth because a target may say `first`, which an expect's Nth refuses.
type TargetNth int

// The named spellings of TargetNth.
const (
	TargetFirst TargetNth = 1
	TargetLast  TargetNth = -1
)

// UnmarshalYAML accepts `first`, `last` or a positive integer.
func (n *TargetNth) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("nth: want first, last or a positive integer, got a %s", nodeKindName(value.Kind))
	}
	switch value.Value {
	case "first":
		*n = TargetFirst
		return nil
	case "last":
		*n = TargetLast
		return nil
	}
	parsed, err := strconv.Atoi(value.Value)
	if err != nil || parsed < 1 {
		return fmt.Errorf("nth: want first, last or a positive integer, got %q", value.Value)
	}
	*n = TargetNth(parsed)
	return nil
}

// Expect is one thing a keeper take must show at a beat, judged on the take's saved session,
// on its screens, or both. Entry selects the session entry judged; Contains is a substring of its
// text, tool label or stat, and Before and After order it against the entry another beat's first
// entry expect locates — all three need an Entry. Seen is a regex that must match the screen of
// at least one snapshot inside the beat. An expect sets Entry, Seen or both.
type Expect struct {
	Entry    *EntrySelector `yaml:"entry"`
	Contains string         `yaml:"contains"`
	Before   int            `yaml:"before"`
	After    int            `yaml:"after"`
	Seen     string         `yaml:"seen"`
}

// EntrySelector is the session-anchor grammar an expect selects its entry with: Kind is an
// entry kind as the session JSON spells it, Text, Tool and Target are prefix matches, and Nth
// picks among the matches.
type EntrySelector struct {
	Kind   string `yaml:"kind"`
	Text   string `yaml:"text"`
	Tool   string `yaml:"tool"`
	Target string `yaml:"target"`
	Nth    Nth    `yaml:"nth"`
}

// Load reads and validates a storyboard. The decode is strict — an unknown field, which
// includes every field the VHS-era schema had (tape, align, anchor, regions, …), is an error —
// and Ship, Cassette and Fonts come back resolved against the storyboard's directory. Every
// validation rule that fails is reported, in a *ValidationError.
func Load(path string) (*Storyboard, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	var board Storyboard
	if err := decoder.Decode(&board); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	board.Path = path
	dir := filepath.Dir(path)
	for _, field := range []*string{&board.Ship, &board.Cassette, &board.Fonts} {
		if *field != "" {
			*field = filepath.Join(dir, *field)
		}
	}
	if problems := board.validate(); len(problems) > 0 {
		return nil, &ValidationError{Path: path, Problems: problems}
	}
	return &board, nil
}

// validate applies every schema rule and returns the ones the storyboard breaks.
func (s *Storyboard) validate() []error {
	var problems []error
	report := func(format string, args ...any) {
		problems = append(problems, fmt.Errorf(format, args...))
	}

	s.validateHeader(report)
	if len(s.Beats) == 0 {
		report("beats: none declared")
		return problems
	}
	ids := s.validateIDs(report)
	for _, beat := range s.Beats {
		beat.validate(ids, report)
	}
	return problems
}

// validateHeader checks the top-level scalars: clip, ship, cassette, fonts, frame and expect.
func (s *Storyboard) validateHeader(report reporter) {
	for _, required := range []struct{ name, value string }{
		{"clip", s.Clip}, {"ship", s.Ship}, {"cassette", s.Cassette}, {"fonts", s.Fonts},
	} {
		if required.value == "" {
			report("%s: missing", required.name)
		}
	}
	frame := s.Frame
	for _, positive := range []struct {
		name  string
		value int
	}{
		{"cols", frame.Cols}, {"rows", frame.Rows}, {"scale", frame.Scale},
		{"width", frame.Width}, {"fps", frame.FPS},
	} {
		if positive.value < 1 {
			report("frame.%s: want ≥1, got %d", positive.name, positive.value)
		}
	}
	if frame.Padding < 0 {
		report("frame.padding: want ≥0, got %d", frame.Padding)
	}
	if frame.FontSize <= 0 {
		report("frame.font_size: want >0, got %g", frame.FontSize)
	}
	if frame.LineHeight <= 0 {
		report("frame.line_height: want >0, got %g", frame.LineHeight)
	}
	if frame.MaxColors < 2 || frame.MaxColors > 256 {
		report("frame.max_colors: want 2..256, got %d", frame.MaxColors)
	}
	if s.Expect.Stage != "" && s.Expect.Stage != StageDirty {
		report("expect.stage: want %s, got %q", StageDirty, s.Expect.Stage)
	}
}

// validateIDs checks that ids are positive, unique and ascending, and returns the set.
func (s *Storyboard) validateIDs(report reporter) map[int]bool {
	ids := make(map[int]bool, len(s.Beats))
	previous := 0
	for _, beat := range s.Beats {
		switch {
		case beat.ID < 1:
			report("beat %d: id: want ≥1", beat.ID)
		case ids[beat.ID]:
			report("beat %d: id: duplicate", beat.ID)
		case beat.ID < previous:
			report("beat %d: id: not ascending (follows %d)", beat.ID, previous)
		}
		ids[beat.ID] = true
		previous = beat.ID
	}
	return ids
}

// Zoom factor bounds: 1 is no magnification, beyond 3 the terminal text breaks into pixels.
const (
	minZoomFactor = 1
	maxZoomFactor = 3
)

// validate applies the per-beat rules: the section timing, the zoom, the actions and the
// expects.
func (b Beat) validate(ids map[int]bool, report reporter) {
	at := func(format string, args ...any) {
		report("beat %d: "+format, append([]any{b.ID}, args...)...)
	}
	if b.Title == "" {
		at("title: missing")
	}
	if b.Hold < 0 {
		at("hold: want ≥0, got %s", b.Hold)
	}
	if !b.Cut {
		switch {
		case b.Duration <= 0:
			at("duration: want >0, got %s", b.Duration)
		case b.Hold >= b.Duration:
			at("hold: want less than duration %s, got %s", b.Duration, b.Hold)
		}
	}
	if b.Zoom != nil {
		b.Zoom.validate(func(format string, args ...any) {
			at("zoom."+format, args...)
		})
	}
	for index, action := range b.Do {
		action.validate(func(format string, args ...any) {
			at("do[%d]: "+format, append([]any{index}, args...)...)
		})
	}
	for index, expect := range b.Expect {
		expect.validate(b.ID, ids, func(format string, args ...any) {
			at("expect[%d]: "+format, append([]any{index}, args...)...)
		})
	}
}

// validate checks the zoom's target, factor and ramps.
func (z Zoom) validate(report reporter) {
	z.Target.validate(func(format string, args ...any) {
		report("target."+format, args...)
	})
	// An absent factor is auto-fit by the compositor.
	if z.Factor != 0 && (z.Factor < minZoomFactor || z.Factor > maxZoomFactor) {
		report("factor: want in [%d, %d], got %g", minZoomFactor, maxZoomFactor, z.Factor)
	}
	if z.In < 0 {
		report("in: want ≥0, got %s", z.In)
	}
	if z.Out < 0 {
		report("out: want ≥0, got %s", z.Out)
	}
}

// validate checks that exactly one action form is set and that its fields fit that form.
func (a Action) validate(report reporter) {
	forms := 0
	for _, isSet := range []bool{a.Type != nil, a.Key != nil, a.Click != nil, a.Wait != nil, a.Pause != nil} {
		if isSet {
			forms++
		}
	}
	if forms != 1 {
		report("want exactly one of type, key, click, wait or pause")
		return
	}
	switch {
	case a.Type != nil:
		if a.Type.Text == "" {
			report("type.text: missing")
		}
	case a.Key != nil:
		if a.Key.Name == "" {
			report("key.name: missing")
		}
		if a.Key.Repeat < 0 {
			report("key.repeat: want ≥0, got %d", a.Key.Repeat)
		}
	case a.Click != nil:
		a.Click.Target.validate(func(format string, args ...any) {
			report("click.target."+format, args...)
		})
		if a.Click.Times < 0 {
			report("click.times: want ≥0, got %d", a.Click.Times)
		}
	case a.Wait != nil:
		a.Wait.validate(func(format string, args ...any) {
			report("wait."+format, args...)
		})
	case a.Pause != nil:
		if a.Pause.For <= 0 {
			report("pause.for: want >0, got %s", a.Pause.For)
		}
	}
}

// validate checks the wait's regex and that its timeout is set and bounded.
func (w WaitAction) validate(report reporter) {
	validateRegex("screen", w.Screen, report)
	if w.Timeout <= 0 || w.Timeout > MaxWaitTimeout {
		report("timeout: want in (0, %s], got %s", MaxWaitTimeout, w.Timeout)
	}
}

// validate checks the target's regex and area.
func (t Target) validate(report reporter) {
	validateRegex("text", t.Text, report)
	if t.Area != "" && !slices.Contains(targetAreas, t.Area) {
		report("area: want one of %s, got %q", strings.Join(targetAreas, ", "), t.Area)
	}
}

// validateRegex reports a missing or non-compiling regex under the field's name.
func validateRegex(field, pattern string, report reporter) {
	if pattern == "" {
		report("%s: missing", field)
		return
	}
	if _, err := regexp.Compile(pattern); err != nil {
		report("%s: %v", field, err)
	}
}

// validate checks that the expect judges an entry, a screen or both; that the entry clauses
// have an entry to judge; that its regex compiles; and that it orders against real beats.
func (e Expect) validate(beatID int, ids map[int]bool, report reporter) {
	if e.Entry == nil && e.Seen == "" {
		report("want entry or seen")
		return
	}
	hasClause := e.Contains != "" || e.Before != 0 || e.After != 0
	switch {
	case e.Entry == nil && hasClause:
		report("contains, before and after judge an entry: set entry")
	case e.Entry != nil && !hasClause:
		report("entry: want at least one of contains, before or after with it")
	}
	for _, order := range []struct {
		name string
		id   int
	}{{"before", e.Before}, {"after", e.After}} {
		if order.id != 0 && (!ids[order.id] || order.id == beatID) {
			report("%s: %d is not another beat", order.name, order.id)
		}
	}
	if e.Seen != "" {
		validateRegex("seen", e.Seen, report)
	}
	if e.Entry != nil && !slices.Contains(sessionKinds, e.Entry.Kind) {
		report("entry: kind: unknown entry kind %q", e.Entry.Kind)
	}
}

// Nth picks which of an entry selector's matches counts: the zero value is the first match, a
// positive value the Nth, and NthLast the last one. It is a custom scalar because a strict
// yaml.v3 decode would refuse `nth: last` into a plain int before validation ever ran.
type Nth int

// NthLast is the `last` spelling of Nth.
const NthLast Nth = -1

// UnmarshalYAML accepts a positive integer or the string `last`.
func (n *Nth) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("nth: want a positive integer or last, got a %s", nodeKindName(value.Kind))
	}
	if value.Value == "last" {
		*n = NthLast
		return nil
	}
	parsed, err := strconv.Atoi(value.Value)
	if err != nil || parsed < 1 {
		return fmt.Errorf("nth: want a positive integer or last, got %q", value.Value)
	}
	*n = Nth(parsed)
	return nil
}

// nodeKindName spells a yaml.Node kind for an error message.
func nodeKindName(kind yaml.Kind) string {
	switch kind {
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	default:
		return "node"
	}
}

// TakeExpect is the top-level expectation on the take as a whole. Stage is the only one:
// `dirty` asserts the stage repo still carries the exchange's writes after the take.
type TakeExpect struct {
	Stage string `yaml:"stage"`
}

// StageDirty is the one take-level stage expectation: the stage repo has uncommitted changes
// after the take.
const StageDirty = "dirty"

// sessionKinds are the entry kinds an entry selector may name, as the session JSON spells them.
var sessionKinds = []string{
	session.EntryKindUser,
	session.EntryKindAssistant,
	session.EntryKindToolCall,
	session.EntryKindToolResult,
	session.EntryKindError,
	session.EntryKindNote,
	session.EntryKindPresented,
	session.EntryKindInterjected,
	session.EntryKindSchedule,
	session.EntryKindCompacted,
}

// ValidationError lists every rule a storyboard breaks. One Load reports all of them, so a
// lint pass fixes the file in one round rather than one problem per run.
type ValidationError struct {
	Path     string
	Problems []error
}

// Error joins the problems one per line, under the file's path.
func (e *ValidationError) Error() string {
	lines := make([]string, 0, len(e.Problems)+1)
	lines = append(lines, fmt.Sprintf("%s: %d problem(s)", e.Path, len(e.Problems)))
	for _, problem := range e.Problems {
		lines = append(lines, "  "+problem.Error())
	}
	return strings.Join(lines, "\n")
}

// Unwrap exposes the individual problems to errors.Is and errors.As.
func (e *ValidationError) Unwrap() []error { return e.Problems }

// reporter collects one validation problem.
type reporter func(format string, args ...any)
