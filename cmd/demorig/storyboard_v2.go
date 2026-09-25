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
)

// StoryboardV2 is one clip's storyboard in the v2 schema: the single source of a clip. The
// header names the clip, where it ships, the cassette its model replies replay from, the font
// directory and the terminal frame; each beat says what the recorder does (Do), how long the
// beat lasts on screen (Duration, Hold, Cut, Zoom) and what a keeper take must show (Expect).
// LoadV2 is the only way to get one: a StoryboardV2 in hand has passed validation, and its
// Ship, Cassette and Fonts are already resolved against the file's directory.
type StoryboardV2 struct {
	// Path is the storyboard file LoadV2 read, as the caller named it.
	Path string `yaml:"-"`
	Clip string `yaml:"clip"`
	// Ship, Cassette and Fonts are written relative to the storyboard's own directory and
	// resolved by LoadV2, so a caller in any working directory finds them.
	Ship     string     `yaml:"ship"`
	Cassette string     `yaml:"cassette"`
	Fonts    string     `yaml:"fonts"`
	Frame    FrameV2    `yaml:"frame"`
	Expect   TakeExpect `yaml:"expect"`
	Beats    []BeatV2   `yaml:"beats"`
}

// FrameV2 is the recorded terminal and the shipped geometry: the pty's Cols × Rows, the
// rasterizer's Padding (pixels), FontSize (points) and LineHeight (a multiple of the font
// size), the Scale frames are rasterized at, the shipped GIF's Width in pixels, its FPS and
// its palette size. Pixel size is derived from the font metrics by the rasterizer, not here.
type FrameV2 struct {
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

// BeatV2 is one storyboard beat: the actions the recorder performs, the section's target
// Duration in the render (Hold is its leading stretch shown at 1×; a beat with no hold has
// hold 0), an optional Zoom onto an on-screen target, and what a keeper shows. A Cut beat is
// recorded but dropped from the render, so its Duration and Hold are not required.
type BeatV2 struct {
	ID       int           `yaml:"id"`
	Title    string        `yaml:"title"`
	Why      string        `yaml:"why"`
	Notes    string        `yaml:"notes"`
	Do       []Action      `yaml:"do"`
	Duration time.Duration `yaml:"duration"`
	Hold     time.Duration `yaml:"hold"`
	Cut      bool          `yaml:"cut"`
	Zoom     *ZoomV2       `yaml:"zoom"`
	Expect   []ExpectV2    `yaml:"expect"`
}

// ZoomV2 is a push into an on-screen target: Factor is the magnification — absent (0), the
// compositor fits it to the target — and In and Out the ramps, 400 ms each when absent.
type ZoomV2 struct {
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

// ExpectV2 is one thing a keeper take must show at a beat: Entry selects the session entry
// judged (a beat has no anchor in v2, so it is required), Contains is a substring of its text,
// tool label or stat, and Before and After order it against another beat.
type ExpectV2 struct {
	Entry    *EntrySelector `yaml:"entry"`
	Contains string         `yaml:"contains"`
	Before   int            `yaml:"before"`
	After    int            `yaml:"after"`
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

// LoadV2 reads and validates a v2 storyboard. The decode is strict — an unknown field, which
// includes every field retired from v1, is an error — and Ship, Cassette and Fonts come back
// resolved against the storyboard's directory. Every validation rule that fails is reported,
// in a *ValidationError.
func LoadV2(path string) (*StoryboardV2, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	var board StoryboardV2
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
func (s *StoryboardV2) validate() []error {
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
func (s *StoryboardV2) validateHeader(report reporter) {
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
func (s *StoryboardV2) validateIDs(report reporter) map[int]bool {
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
func (b BeatV2) validate(ids map[int]bool, report reporter) {
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
func (z ZoomV2) validate(report reporter) {
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

// validate checks that the expect names its entry, asserts something and orders against real
// beats.
func (e ExpectV2) validate(beatID int, ids map[int]bool, report reporter) {
	if e.Contains == "" && e.Before == 0 && e.After == 0 {
		report("want at least one of contains, before or after")
	}
	for _, order := range []struct {
		name string
		id   int
	}{{"before", e.Before}, {"after", e.After}} {
		if order.id != 0 && (!ids[order.id] || order.id == beatID) {
			report("%s: %d is not another beat", order.name, order.id)
		}
	}
	if e.Entry == nil {
		report("entry: required — a v2 beat has no anchor to default to")
		return
	}
	if !slices.Contains(sessionKinds, e.Entry.Kind) {
		report("entry: kind: unknown entry kind %q", e.Entry.Kind)
	}
}
