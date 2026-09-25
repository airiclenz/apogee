package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/tuitest"
)

// The engine's pacing. Each gap is a floor the program must see between two writes, not a
// target the render keeps: the render retimes every beat to its own duration.
const (
	// clickHoldGap parts a click's press from its release: apogee toggles blocks on the RELEASE
	// (internal/tui/mouse.go), so the two must reach it as two reports, never one read.
	clickHoldGap = 40 * time.Millisecond
	// clickRepeatGap parts one click's release from the next click's press, so `times: 2` is two
	// clicks rather than a double-click.
	clickRepeatGap = 250 * time.Millisecond
	// keyRepeatGap follows every key press. It sits above Bubble Tea's 50 ms escape timeout, so
	// an esc is never read together with the bytes after it — two esc presses are two keys, never
	// one alt+esc.
	keyRepeatGap = 100 * time.Millisecond
	// waitPollInterval is how often a wait re-reads the screen.
	waitPollInterval = 20 * time.Millisecond
)

// The kinds of take event the engine logs. Every event's Detail is a JSON [EngineEventDetail].
const (
	EventBeatStart   = "beat-start"
	EventActionStart = "action-start"
	EventActionEnd   = "action-end"
	EventTarget      = "target"
	EventClick       = "click"
)

// The screen-chrome glyphs a target's area is located by (layout.md): the `▁` hairline is the
// frame's floor, the `▔` hairline is the top rule the status line sits under, and the prompt box
// opens with `╭`.
const (
	floorHairline   = "▁"
	topRuleHairline = "▔"
	promptBoxCorner = "╭"
)

// keySequences are the key names a storyboard's `key` action accepts, spelled as Bubble Tea's
// msg.String() spells them, mapped to the bytes a terminal sends for each. The bytes are
// tuitest's, which a test pins against the real key parser.
var keySequences = map[string]tuitest.Key{
	"enter": tuitest.Enter, "esc": tuitest.Esc, "tab": tuitest.Tab, "shift+tab": tuitest.ShiftTab,
	"space": tuitest.Space, "backspace": tuitest.Backspace, "ctrl+c": tuitest.CtrlC,
	"up": tuitest.Up, "down": tuitest.Down, "left": tuitest.Left, "right": tuitest.Right,
	"pgup": tuitest.PgUp, "pgdown": tuitest.PgDown, "alt+up": tuitest.AltUp, "alt+down": tuitest.AltDown,
	"f1": tuitest.F1, "f2": tuitest.F2, "f3": tuitest.F3, "f4": tuitest.F4, "f5": tuitest.F5,
	"f6": tuitest.F6, "f7": tuitest.F7, "f8": tuitest.F8, "f9": tuitest.F9, "f10": tuitest.F10,
	"f11": tuitest.F11, "f12": tuitest.F12,
}

// CellBox is a rectangle of terminal cells: its top-left cell (0-based) and its size.
type CellBox struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Center is the box's middle cell — where a click on it lands.
func (b CellBox) Center() (x, y int) { return b.X + (b.W-1)/2, b.Y + (b.H-1)/2 }

// EngineEventDetail is the JSON an engine event carries: the beat it belongs to, and for the
// action events the action's index in the beat's `do` list and its form. A target event adds the
// resolved Box, a click event the Cell clicked ([x, y], 0-based). A beat-start event carries the
// beat's Title instead, and its Action is always 0.
type EngineEventDetail struct {
	Beat   int      `json:"beat"`
	Title  string   `json:"title,omitempty"`
	Action int      `json:"action"`
	Form   string   `json:"form,omitempty"`
	Box    *CellBox `json:"box,omitempty"`
	Cell   *[2]int  `json:"cell,omitempty"`
}

// BeatError is a beat that failed: which beat, which of its actions, and why.
type BeatError struct {
	Beat   int
	Title  string
	Action int
	Err    error
}

// Error names the beat and the action, then the cause.
func (e *BeatError) Error() string {
	return fmt.Sprintf("beat %d (%s): do[%d]: %v", e.Beat, e.Title, e.Action, e.Err)
}

// Unwrap is the cause.
func (e *BeatError) Unwrap() error { return e.Err }

// Engine performs a storyboard's beats against a live [Terminal]: it types, presses keys, clicks
// on-screen targets with real SGR mouse reports and waits on the screen, logging every beat
// start, action start and end, resolved target and click into the terminal's take. It reads the
// screen only through [Terminal.Current] and [Terminal.Match] and writes only through
// [Terminal.Send], so what it sees is what the take records — a screen a wait matched is
// recorded even when the sampler would have missed it — and what it sends is what the program
// reads.
type Engine struct {
	term *Terminal
}

// NewEngine returns an engine driving term.
func NewEngine(term *Terminal) *Engine { return &Engine{term: term} }

// Run performs beats in order and stops at the first that fails, returning its *BeatError.
func (e *Engine) Run(ctx context.Context, beats []Beat) error {
	for _, beat := range beats {
		if err := e.RunBeat(ctx, beat); err != nil {
			return err
		}
	}
	return nil
}

// RunBeat performs one beat's actions in order. A failing action ends the beat with a
// *BeatError naming the beat and the action; its action-end event is then never logged.
func (e *Engine) RunBeat(ctx context.Context, beat Beat) error {
	e.logEvent(EventBeatStart, EngineEventDetail{Beat: beat.ID, Title: beat.Title})
	for index, action := range beat.Do {
		step := EngineEventDetail{Beat: beat.ID, Action: index, Form: actionForm(action)}
		e.logEvent(EventActionStart, step)
		if err := e.perform(ctx, step, action); err != nil {
			return &BeatError{Beat: beat.ID, Title: beat.Title, Action: index, Err: err}
		}
		e.logEvent(EventActionEnd, step)
	}
	return nil
}

// perform runs one action.
func (e *Engine) perform(ctx context.Context, step EngineEventDetail, action Action) error {
	switch {
	case action.Type != nil:
		return e.typeText(ctx, *action.Type)
	case action.Key != nil:
		return e.pressKey(ctx, *action.Key)
	case action.Click != nil:
		return e.click(ctx, step, *action.Click)
	case action.Wait != nil:
		return e.wait(ctx, *action.Wait)
	case action.Pause != nil:
		return sleepContext(ctx, action.Pause.For)
	}
	return errors.New("no action form set")
}

// actionForm names the form an action takes, as the storyboard spells it.
func actionForm(action Action) string {
	switch {
	case action.Type != nil:
		return "type"
	case action.Key != nil:
		return "key"
	case action.Click != nil:
		return "click"
	case action.Wait != nil:
		return "wait"
	case action.Pause != nil:
		return "pause"
	}
	return ""
}

// typeText sends the text one character per write, paced by the humanized profile, or — with
// humanize off — as a single write.
func (e *Engine) typeText(ctx context.Context, action TypeAction) error {
	if !action.IsHumanized() {
		return e.term.Send([]byte(action.Text))
	}
	gaps := Humanize(action.Text, DefaultTypingSeed)
	for index, character := range []rune(action.Text) {
		if err := e.term.Send([]byte(string(character))); err != nil {
			return err
		}
		if index < len(gaps) {
			if err := sleepContext(ctx, gaps[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

// pressKey sends the named key's sequence, Repeat times, each press one write and each followed
// by keyRepeatGap: an ESC-prefixed sequence split across writes, or an esc glued to whatever is
// written next, would reach the program as a different key — and a stray esc cancels a run.
func (e *Engine) pressKey(ctx context.Context, action KeyAction) error {
	sequence, known := keySequences[action.Name]
	if !known {
		return fmt.Errorf("key: unknown key name %q", action.Name)
	}
	for range max(action.Repeat, 1) {
		if err := e.term.Send([]byte(sequence)); err != nil {
			return err
		}
		if err := sleepContext(ctx, keyRepeatGap); err != nil {
			return err
		}
	}
	return nil
}

// click resolves the target on the screen as it stands, logs the box, then sends Times left
// clicks at its centre cell — each an SGR press and, clickHoldGap later, its release.
func (e *Engine) click(ctx context.Context, step EngineEventDetail, action ClickAction) error {
	box, err := resolveTarget(e.term.Current(), action.Target)
	if err != nil {
		return err
	}
	step.Box = &box
	e.logEvent(EventTarget, step)
	step.Box = nil

	x, y := box.Center()
	for press := range max(action.Times, 1) {
		if press > 0 {
			if err := sleepContext(ctx, clickRepeatGap); err != nil {
				return err
			}
		}
		step.Cell = &[2]int{x, y}
		e.logEvent(EventClick, step)
		if err := e.term.Send([]byte(tuitest.Click(x, y))); err != nil {
			return err
		}
		if err := sleepContext(ctx, clickHoldGap); err != nil {
			return err
		}
		if err := e.term.Send([]byte(tuitest.Release(x, y))); err != nil {
			return err
		}
	}
	return nil
}

// wait polls the screen until the regex matches it (or, with Gone, no longer does). The screen
// that satisfies it is recorded into the take ([Terminal.Match]), so a seen expect on a beat
// that ends on a wait always finds the screen the wait saw. It fails at the timeout, or at once
// when the program exits first.
func (e *Engine) wait(ctx context.Context, action WaitAction) error {
	pattern, err := regexp.Compile(action.Screen)
	if err != nil {
		return fmt.Errorf("wait: %w", err)
	}
	want := "shown"
	if action.Gone {
		want = "gone"
	}
	deadline := time.NewTimer(action.Timeout)
	defer deadline.Stop()
	poll := time.NewTicker(waitPollInterval)
	defer poll.Stop()
	for {
		if e.term.Match(func(screen Snapshot) bool {
			return pattern.MatchString(screenText(screen)) != action.Gone
		}) {
			return nil
		}
		select {
		case <-poll.C:
		case <-deadline.C:
			return fmt.Errorf("wait for /%s/ %s: timed out after %s", action.Screen, want, action.Timeout)
		case <-e.term.Done():
			return fmt.Errorf("wait for /%s/ %s: the program exited", action.Screen, want)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// logEvent appends one engine event to the take.
func (e *Engine) logEvent(kind string, detail EngineEventDetail) {
	encoded, err := json.Marshal(detail)
	if err != nil {
		// Unreachable: the detail holds only ints and strings.
		panic("demorig: encode an engine event: " + err.Error())
	}
	e.term.Event(kind, string(encoded))
}

// sleepContext waits d, or less when ctx ends first.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// screenRow is one snapshot row as plain text, trailing blanks trimmed, with the terminal column
// every byte of the text starts in: cols has one entry per byte plus one for the end of the text,
// so a match's byte span [start, end) covers the columns [cols[start], cols[end]). The second
// column of a wide grapheme contributes no text, so a wide grapheme counts once.
type screenRow struct {
	text string
	cols []int
}

// screenRowsOf reads every row of s.
func screenRowsOf(s Snapshot) []screenRow {
	rows := make([]screenRow, len(s.Cells))
	for y, cells := range s.Cells {
		var text strings.Builder
		cols := make([]int, 0, len(cells)+1)
		end, endCol := 0, 0
		for x, cell := range cells {
			if cell.Width == 0 {
				continue
			}
			grapheme := cell.Rune
			if grapheme == "" {
				grapheme = " "
			}
			for range len(grapheme) {
				cols = append(cols, x)
			}
			text.WriteString(grapheme)
			if grapheme != " " {
				end, endCol = text.Len(), x+cell.Width
			}
		}
		rows[y] = screenRow{text: text.String()[:end], cols: append(cols[:end], endCol)}
	}
	return rows
}

// screenText is the snapshot as plain text, rows joined by newlines — what a wait's regex reads.
func screenText(s Snapshot) string {
	rows := screenRowsOf(s)
	lines := make([]string, len(rows))
	for y, row := range rows {
		lines[y] = row.text
	}
	return strings.Join(lines, "\n")
}

// resolveTarget finds the target on s: every match of its regex within its area's rows, in
// reading order (top to bottom, left to right), from which Nth picks one — first, the Nth, or
// the last, which is the lowest row's rightmost match.
func resolveTarget(s Snapshot, target Target) (CellBox, error) {
	pattern, err := regexp.Compile(target.Text)
	if err != nil {
		return CellBox{}, fmt.Errorf("target: %w", err)
	}
	rows := screenRowsOf(s)
	top, bottom, err := areaRows(rows, target.Area)
	if err != nil {
		return CellBox{}, err
	}
	var matches []CellBox
	for y := top; y < bottom; y++ {
		row := rows[y]
		for _, span := range pattern.FindAllStringIndex(row.text, -1) {
			if span[0] == span[1] {
				continue
			}
			x := row.cols[span[0]]
			matches = append(matches, CellBox{X: x, Y: y, W: row.cols[span[1]] - x, H: 1})
		}
	}
	return pickMatch(matches, target)
}

// pickMatch applies the target's Nth to its matches.
func pickMatch(matches []CellBox, target Target) (CellBox, error) {
	where := target.Area
	if where == "" {
		where = AreaAny
	}
	switch {
	case len(matches) == 0:
		return CellBox{}, fmt.Errorf("target /%s/ (area %s): not on screen", target.Text, where)
	case target.Nth == TargetLast:
		return matches[len(matches)-1], nil
	case target.Nth <= TargetFirst:
		return matches[0], nil
	case int(target.Nth) > len(matches):
		return CellBox{}, fmt.Errorf("target /%s/ (area %s): nth %d, but only %d on screen",
			target.Text, where, target.Nth, len(matches))
	}
	return matches[target.Nth-1], nil
}

// areaRows maps a target area to the half-open row range [top, bottom) it searches, reading
// apogee's chrome (layout.md): footer is the row directly above the `▁` floor hairline; status
// is the row directly below the lowest `▔` top rule above the prompt box — the staged band sits
// between it and the box; transcript is everything above that top rule; any is every row.
func areaRows(rows []screenRow, area string) (top, bottom int, err error) {
	switch area {
	case "", AreaAny:
		return 0, len(rows), nil
	case AreaFooter:
		floor := lowestRowOpeningWith(rows, len(rows), floorHairline)
		if floor < 1 {
			return 0, 0, fmt.Errorf("area footer: no %s floor hairline on screen", floorHairline)
		}
		return floor - 1, floor, nil
	}
	box := lowestRowOpeningWith(rows, len(rows), promptBoxCorner)
	if box < 0 {
		return 0, 0, fmt.Errorf("area %s: no prompt box on screen", area)
	}
	rule := lowestRowOpeningWith(rows, box, topRuleHairline)
	if rule < 0 {
		return 0, 0, fmt.Errorf("area %s: no %s top rule above the prompt box", area, topRuleHairline)
	}
	if area == AreaTranscript {
		return 0, rule, nil
	}
	return rule + 1, rule + 2, nil
}

// lowestRowOpeningWith is the lowest row above limit whose text, leading blanks trimmed, opens
// with glyph, or -1 when none does.
func lowestRowOpeningWith(rows []screenRow, limit int, glyph string) int {
	for y := limit - 1; y >= 0; y-- {
		if strings.HasPrefix(strings.TrimLeft(rows[y].text, " "), glyph) {
			return y
		}
	}
	return -1
}
