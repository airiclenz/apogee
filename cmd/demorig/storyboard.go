package main

import (
	"bufio"
	"bytes"
	"fmt"
	"maps"
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

// Storyboard is one clip's durable storyboard (`graphics/demo/storyboards/<clip>.yaml`): the
// beats, where each sits in a take, how it is framed in the shipped GIF, what a keeper take
// must contain, and the director's notes. The schema is documented in graphics/demo/README.md,
// "Storyboards". Load is the only way to get one: a Storyboard in hand has passed validation,
// and its Tape and Ship are already resolved against the file's directory.
type Storyboard struct {
	// Path is the storyboard file Load read, as the caller named it.
	Path string `yaml:"-"`
	Clip string `yaml:"clip"`
	// Tape and Ship are written relative to the storyboard's own directory and resolved by
	// Load, so a caller in any working directory finds them.
	Tape     string        `yaml:"tape"`
	Ship     string        `yaml:"ship"`
	Frame    Frame         `yaml:"frame"`
	Align    Align         `yaml:"align"`
	PaintLag time.Duration `yaml:"paint_lag"`
	// Regions are the named zoom targets, each a fractional x, y, w, h of the frame.
	Regions map[string][]float64 `yaml:"regions"`
	Expect  TakeExpect           `yaml:"expect"`
	Beats   []Beat               `yaml:"beats"`
}

// Frame is the shipped geometry: the GIF's width, the ratio the tape records at (the 2× rule),
// the frame rate and the palette size.
type Frame struct {
	Width     int `yaml:"width"`
	Scale     int `yaml:"scale"`
	FPS       int `yaml:"fps"`
	MaxColors int `yaml:"max_colors"`
}

// Align pins the session clock to the video clock: FirstPromptAt is the video time of the
// first prompt's Enter, fixed by the tape's head, and SceneThreshold feeds the ffmpeg
// scene-change detection that finds the shell→TUI first paint.
type Align struct {
	SceneThreshold float64       `yaml:"scene_threshold"`
	FirstPromptAt  time.Duration `yaml:"first_prompt_at"`
}

// TakeExpect is the top-level expectation on the take as a whole. Stage is the only one:
// `dirty` asserts the stage repo still carries the exchange's writes after the take.
type TakeExpect struct {
	Stage string `yaml:"stage"`
}

// Beat is one storyboard beat: its place in the tape (`# beat N` header), where it starts in a
// take (Anchor), how the segment up to the next beat is framed, and what a keeper shows.
type Beat struct {
	ID     int      `yaml:"id"`
	Title  string   `yaml:"title"`
	Why    string   `yaml:"why"`
	Tape   string   `yaml:"tape"`
	Anchor Anchor   `yaml:"anchor"`
	Frame  Framing  `yaml:"frame"`
	Expect []Expect `yaml:"expect"`
	Notes  string   `yaml:"notes"`
}

// Anchor says where a beat starts. Exactly one of its three forms is set: a session anchor
// (Kind, with the optional Text/Tool/Target prefix matches and Nth), a video anchor (Video is
// first-paint or end) or a beat anchor (Beat names an earlier beat's id). Offset is added last
// in every form.
type Anchor struct {
	Kind   string        `yaml:"kind"`
	Text   string        `yaml:"text"`
	Tool   string        `yaml:"tool"`
	Target string        `yaml:"target"`
	Nth    Nth           `yaml:"nth"`
	Video  string        `yaml:"video"`
	Beat   int           `yaml:"beat"`
	Offset time.Duration `yaml:"offset"`
}

// The two fixed points of a take a video anchor may name.
const (
	VideoFirstPaint = "first-paint"
	VideoEnd        = "end"
)

// IsSession reports whether the anchor selects a session entry.
func (a Anchor) IsSession() bool { return a.Kind != "" }

// IsVideo reports whether the anchor is one of the take's fixed points.
func (a Anchor) IsVideo() bool { return a.Video != "" }

// IsBeat reports whether the anchor is relative to another beat.
func (a Anchor) IsBeat() bool { return a.Beat != 0 }

// Nth picks which of a session anchor's matches counts: the zero value is the first match, a
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

// Framing is how a beat's segment is shaped in the render: Speed multiplies the segment (nil
// means 1×), Hold keeps its leading stretch at 1×, Zoom pushes into a named region, and Cut
// drops the segment altogether.
type Framing struct {
	Speed *float64      `yaml:"speed"`
	Hold  time.Duration `yaml:"hold"`
	Zoom  *Zoom         `yaml:"zoom"`
	Cut   bool          `yaml:"cut"`
}

// Rate is the segment's speed multiplier, 1 when the storyboard sets none.
func (f Framing) Rate() float64 {
	if f.Speed == nil {
		return 1
	}
	return *f.Speed
}

// Zoom is a push into one of the storyboard's Regions: Factor is the magnification, In and
// Out the ramps, Hold the time spent fully zoomed.
type Zoom struct {
	Region string        `yaml:"region"`
	Factor float64       `yaml:"factor"`
	In     time.Duration `yaml:"in"`
	Hold   time.Duration `yaml:"hold"`
	Out    time.Duration `yaml:"out"`
}

// Expect is one thing a keeper take must show at a beat: Contains is a substring of the
// anchored entry's text, tool label or stat; Before and After order the entry against another
// beat. Entry overrides the beat's anchor as the entry judged, and is required when that anchor
// is not a session anchor.
type Expect struct {
	Entry    *Anchor `yaml:"entry"`
	Contains string  `yaml:"contains"`
	Before   int     `yaml:"before"`
	After    int     `yaml:"after"`
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

// Load reads and validates a storyboard. The decode is strict — an unknown field is an error —
// and Tape and Ship come back resolved against the storyboard's directory. Every validation
// rule that fails is reported, in a *ValidationError.
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
	if board.Tape != "" {
		board.Tape = filepath.Join(dir, board.Tape)
	}
	if board.Ship != "" {
		board.Ship = filepath.Join(dir, board.Ship)
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
	headers, err := tapeHeaders(s.Tape)
	if err != nil {
		report("tape: %v", err)
	}
	if len(s.Beats) == 0 {
		report("beats: none declared")
		return problems
	}
	ids := s.validateIDs(report)
	for _, beat := range s.Beats {
		beat.validate(s, ids, headers, report)
	}
	s.validateBookends(report)
	return problems
}

// reporter collects one validation problem.
type reporter func(format string, args ...any)

// validateHeader checks the top-level scalars: clip, tape, ship, frame, align, paint_lag,
// regions and expect.
func (s *Storyboard) validateHeader(report reporter) {
	if s.Clip == "" {
		report("clip: missing")
	}
	if s.Tape == "" {
		report("tape: missing")
	}
	if s.Ship == "" {
		report("ship: missing")
	}
	if s.Frame.Width < 1 {
		report("frame.width: want ≥1, got %d", s.Frame.Width)
	}
	if s.Frame.Scale < 1 {
		report("frame.scale: want ≥1, got %d", s.Frame.Scale)
	}
	if s.Frame.FPS < 1 {
		report("frame.fps: want ≥1, got %d", s.Frame.FPS)
	}
	if s.Frame.MaxColors < 2 || s.Frame.MaxColors > 256 {
		report("frame.max_colors: want 2..256, got %d", s.Frame.MaxColors)
	}
	if s.Align.SceneThreshold <= 0 || s.Align.SceneThreshold > 1 {
		report("align.scene_threshold: want in (0, 1], got %g", s.Align.SceneThreshold)
	}
	if s.Align.FirstPromptAt <= 0 {
		report("align.first_prompt_at: want >0, got %s", s.Align.FirstPromptAt)
	}
	if s.PaintLag < 0 {
		report("paint_lag: want ≥0, got %s", s.PaintLag)
	}
	for _, name := range slices.Sorted(maps.Keys(s.Regions)) {
		region := s.Regions[name]
		if len(region) != 4 {
			report("regions.%s: want [x, y, w, h], got %d values", name, len(region))
			continue
		}
		for _, fraction := range region {
			if fraction < 0 || fraction > 1 {
				report("regions.%s: want fractions in [0, 1], got %g", name, fraction)
				break
			}
		}
	}
	if s.Expect.Stage != "" && s.Expect.Stage != StageDirty {
		report("expect.stage: want %s, got %q", StageDirty, s.Expect.Stage)
	}
}

// StageDirty is the one take-level stage expectation: the stage repo has uncommitted changes
// after the take.
const StageDirty = "dirty"

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

// validateBookends checks the two fixed points: exactly one bare `first-paint` anchor and
// exactly one bare `end` anchor, on the first and last beat. A video anchor carrying an
// offset (`{video: end, offset: -6.5s}`) is an ordinary anchor and not a bookend.
func (s *Storyboard) validateBookends(report reporter) {
	first, last := s.Beats[0], s.Beats[len(s.Beats)-1]
	if !isBookend(first.Anchor, VideoFirstPaint) {
		report("beat %d: anchor: the first beat must anchor {video: first-paint}", first.ID)
	}
	if !isBookend(last.Anchor, VideoEnd) {
		report("beat %d: anchor: the last beat must anchor {video: end}", last.ID)
	}
	for _, beat := range s.Beats[1:] {
		if isBookend(beat.Anchor, VideoFirstPaint) {
			report("beat %d: anchor: a second {video: first-paint}; only the first beat may anchor it", beat.ID)
		}
	}
	for _, beat := range s.Beats[:len(s.Beats)-1] {
		if isBookend(beat.Anchor, VideoEnd) {
			report("beat %d: anchor: a second {video: end}; only the last beat may anchor it", beat.ID)
		}
	}
}

// isBookend reports whether the anchor is the bare video anchor named.
func isBookend(a Anchor, video string) bool {
	return a.Video == video && a.Offset == 0
}

// validate applies the per-beat rules: the tape header, the anchor, the framing and the
// expects.
func (b Beat) validate(board *Storyboard, ids map[int]bool, headers []string, report reporter) {
	at := func(format string, args ...any) {
		report("beat %d: "+format, append([]any{b.ID}, args...)...)
	}
	if b.Title == "" {
		at("title: missing")
	}
	switch {
	case b.Tape == "":
		at("tape: missing")
	case headers != nil && !slices.Contains(headers, b.Tape):
		at("tape: no `# %s` header in %s", b.Tape, board.Tape)
	}
	b.Anchor.validate(b.ID, ids, func(format string, args ...any) {
		at("anchor: "+format, args...)
	})
	b.Frame.validate(board.Regions, func(format string, args ...any) {
		at("frame: "+format, args...)
	})
	for index, expect := range b.Expect {
		expect.validate(b, ids, func(format string, args ...any) {
			at("expect[%d]: "+format, append([]any{index}, args...)...)
		})
	}
}

// validate checks that exactly one anchor form is set and that its fields fit that form.
func (a Anchor) validate(beatID int, ids map[int]bool, report reporter) {
	forms := 0
	for _, isSet := range []bool{a.IsSession(), a.IsVideo(), a.IsBeat()} {
		if isSet {
			forms++
		}
	}
	if forms != 1 {
		report("want exactly one of kind, video or beat")
		return
	}
	switch {
	case a.IsSession():
		if !slices.Contains(sessionKinds, a.Kind) {
			report("kind: unknown entry kind %q", a.Kind)
		}
	case a.IsVideo():
		if a.Video != VideoFirstPaint && a.Video != VideoEnd {
			report("video: want %s or %s, got %q", VideoFirstPaint, VideoEnd, a.Video)
		}
	case a.IsBeat():
		if a.Beat >= beatID || !ids[a.Beat] {
			report("beat: %d is not an earlier beat", a.Beat)
		}
	}
	if !a.IsSession() && (a.Text != "" || a.Tool != "" || a.Target != "" || a.Nth != 0) {
		report("text, tool, target and nth belong to a session anchor (kind: …)")
	}
}

// sessionKinds are the entry kinds a session anchor may name, as the session JSON spells them.
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

// validate checks the framing values and that the zoom's region is declared.
func (f Framing) validate(regions map[string][]float64, report reporter) {
	if f.Speed != nil && *f.Speed <= 0 {
		report("speed: want >0, got %g", *f.Speed)
	}
	if f.Hold < 0 {
		report("hold: want ≥0, got %s", f.Hold)
	}
	if f.Zoom == nil {
		return
	}
	if _, declared := regions[f.Zoom.Region]; !declared {
		report("zoom.region: %q is not declared under regions", f.Zoom.Region)
	}
	if f.Zoom.Factor < 1 {
		report("zoom.factor: want ≥1, got %g", f.Zoom.Factor)
	}
	for _, ramp := range []struct {
		name string
		d    time.Duration
	}{{"in", f.Zoom.In}, {"hold", f.Zoom.Hold}, {"out", f.Zoom.Out}} {
		if ramp.d < 0 {
			report("zoom.%s: want ≥0, got %s", ramp.name, ramp.d)
		}
	}
}

// validate checks that the expect asserts something, orders against real beats, and names
// its entry when the beat's own anchor is not a session entry.
func (e Expect) validate(beat Beat, ids map[int]bool, report reporter) {
	if e.Contains == "" && e.Before == 0 && e.After == 0 {
		report("want at least one of contains, before or after")
	}
	for _, order := range []struct {
		name string
		id   int
	}{{"before", e.Before}, {"after", e.After}} {
		if order.id != 0 && (!ids[order.id] || order.id == beat.ID) {
			report("%s: %d is not another beat", order.name, order.id)
		}
	}
	switch {
	case e.Entry == nil && !beat.Anchor.IsSession():
		report("entry: required — the beat's anchor is not a session entry")
	case e.Entry != nil && !e.Entry.IsSession():
		report("entry: want a session anchor (kind: …)")
	case e.Entry != nil:
		e.Entry.validate(beat.ID, ids, func(format string, args ...any) {
			report("entry: "+format, args...)
		})
	}
}

// tapeHeaderPattern matches a tape's `# beat N — …` section headers and captures the label
// (`beat N`) a storyboard's `tape:` names.
var tapeHeaderPattern = regexp.MustCompile(`^# (beat \d+)(\s|$)`)

// tapeHeaders reads the `# beat N` section headers out of a tape file.
func tapeHeaders(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	headers := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		if match := tapeHeaderPattern.FindStringSubmatch(scanner.Text()); match != nil {
			headers = append(headers, match[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return headers, nil
}
