package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Take is one recording of a program in a terminal: every distinct picture the terminal showed,
// each stamped with when it appeared, and a log of what the rig did to the program along the way.
// It is the whole input the render reads — the rasterizer draws a snapshot's cells, the section
// timing reads the snapshots' times, and the event log says where each beat's action landed — so
// nothing downstream ever goes back to the program or the pty.
//
// A snapshot is a FULL grid rather than a diff: a take is read at arbitrary points (a section
// boundary, a zoom's first frame), and a full grid is readable at any of them without replaying
// what came before. Consecutive identical grids are never both kept, and the recorder samples no
// faster than the storyboard's fps, which keeps a take's size proportional to how much the screen
// changed rather than to how long it ran.
type Take struct {
	Cols    int       `json:"cols"`
	Rows    int       `json:"rows"`
	FPS     int       `json:"fps"`
	Started time.Time `json:"started"`

	Snapshots []Snapshot  `json:"-"`
	Events    []TakeEvent `json:"-"`
}

// Snapshot is what the terminal showed from At (measured from the take's start) until the next
// snapshot: its cell grid, one slice per row, and its cursor.
type Snapshot struct {
	At     time.Duration `json:"at"`
	Cursor Cursor        `json:"cursor"`
	Cells  [][]TakeCell  `json:"cells"`
}

// Cursor is where the terminal's cursor sat and whether the program had it shown.
type Cursor struct {
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Visible bool `json:"visible"`
}

// TakeCell is one cell of a snapshot: the grapheme it shows, how many columns that grapheme
// occupies (0 for the second column of a wide grapheme), and every style attribute the
// rasterizer draws. Unlike tuitest's Cell, which keeps only what apogee's assertions compare, this
// keeps what a picture needs.
type TakeCell struct {
	Rune      string    `json:"r,omitempty"`
	Width     int       `json:"w"`
	FG        TakeColor `json:"fg,omitempty"`
	BG        TakeColor `json:"bg,omitempty"`
	Bold      bool      `json:"b,omitempty"`
	Faint     bool      `json:"f,omitempty"`
	Italic    bool      `json:"i,omitempty"`
	Underline bool      `json:"u,omitempty"`
	Reverse   bool      `json:"rv,omitempty"`
}

// TakeColor is a cell colour as the program asked for it. "" is the terminal's default — a real
// value, which the rasterizer paints with the theme's foreground or background. "@N" is palette
// entry N (0–255): kept as an index rather than resolved, so the theme decides what the sixteen
// ANSI colours look like, exactly as a real terminal's theme does. "#rrggbb" is a truecolor value.
type TakeColor string

// takeColorOf spells an emulator colour as a [TakeColor].
func takeColorOf(c color.Color) TakeColor {
	switch v := c.(type) {
	case nil:
		return ""
	case ansi.BasicColor:
		return TakeColor("@" + strconv.Itoa(int(v)))
	case ansi.IndexedColor:
		return TakeColor("@" + strconv.Itoa(int(v)))
	}
	r, g, b, _ := c.RGBA()
	return TakeColor(fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8))
}

// Palette reports the palette index of an "@N" colour, and ok=false for any other spelling.
func (c TakeColor) Palette() (index int, ok bool) {
	if !strings.HasPrefix(string(c), "@") {
		return 0, false
	}
	n, err := strconv.Atoi(string(c[1:]))
	if err != nil || n < 0 || n > 255 {
		return 0, false
	}
	return n, true
}

// Color resolves the colour to RGB: a palette index through the standard xterm palette, a
// truecolor value as itself. The terminal's default ("") and an unreadable spelling resolve to nil
// — the caller's theme owns what the default looks like.
func (c TakeColor) Color() color.Color {
	if n, ok := c.Palette(); ok {
		return ansi.IndexedColor(n)
	}
	if len(c) != len("#rrggbb") || c[0] != '#' {
		return nil
	}
	v, err := strconv.ParseUint(string(c[1:]), 16, 32)
	if err != nil {
		return nil
	}
	return ansi.RGBColor{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}
}

// TakeEvent is one thing the rig did or saw during the take — a beat began, keys were typed, a
// click was sent, the program exited — stamped on the same clock as the snapshots.
type TakeEvent struct {
	At     time.Duration `json:"at"`
	Kind   string        `json:"kind"`
	Detail string        `json:"detail,omitempty"`
}

// takeCellOf reads one emulator cell. A nil cell is a blank one.
func takeCellOf(c *uv.Cell) TakeCell {
	if c == nil {
		return TakeCell{Rune: " ", Width: 1}
	}
	attrs := c.Style.Attrs
	return TakeCell{
		Rune:      c.Content,
		Width:     c.Width,
		FG:        takeColorOf(c.Style.Fg),
		BG:        takeColorOf(c.Style.Bg),
		Bold:      attrs&uv.AttrBold != 0,
		Faint:     attrs&uv.AttrFaint != 0,
		Italic:    attrs&uv.AttrItalic != 0,
		Underline: c.Style.Underline != 0,
		Reverse:   attrs&uv.AttrReverse != 0,
	}
}

// gridOf reads a whole cols×rows grid through cellAt.
func gridOf(cols, rows int, cellAt func(x, y int) *uv.Cell) [][]TakeCell {
	grid := make([][]TakeCell, rows)
	for y := range grid {
		row := make([]TakeCell, cols)
		for x := range row {
			row[x] = takeCellOf(cellAt(x, y))
		}
		grid[y] = row
	}
	return grid
}

// sameScreen reports whether two snapshots show the same picture — the same cells and the same
// cursor. Their times do not count: the question is whether the second one adds a frame.
func sameScreen(a, b Snapshot) bool {
	if a.Cursor != b.Cursor || len(a.Cells) != len(b.Cells) {
		return false
	}
	for y := range a.Cells {
		if len(a.Cells[y]) != len(b.Cells[y]) {
			return false
		}
		for x := range a.Cells[y] {
			if a.Cells[y][x] != b.Cells[y][x] {
				return false
			}
		}
	}
	return true
}

// addSnapshot appends s unless it shows what the last snapshot already shows, and reports whether
// it did.
func (t *Take) addSnapshot(s Snapshot) bool {
	if n := len(t.Snapshots); n > 0 && sameScreen(t.Snapshots[n-1], s) {
		return false
	}
	t.Snapshots = append(t.Snapshots, s)
	return true
}

// takeLine is one line of a take file after the header: exactly one of its fields is set.
type takeLine struct {
	Snapshot *Snapshot  `json:"snap,omitempty"`
	Event    *TakeEvent `json:"event,omitempty"`
}

// errTakeFormat is what a take file that is not one gets.
var errTakeFormat = errors.New("not a demorig take")

// WriteTake writes t to w as a gzip-compressed JSON-lines stream: the header (size, fps, start)
// first, then every snapshot and every event in time order, one per line. Lines keep a take
// streamable — a reader holds one snapshot at a time — and gzip keeps the repeated cell spellings
// cheap.
func WriteTake(w io.Writer, t *Take) error {
	zw := gzip.NewWriter(w)
	enc := json.NewEncoder(zw)
	if err := enc.Encode(t); err != nil {
		return fmt.Errorf("write the take header: %w", err)
	}
	si, ei := 0, 0
	for si < len(t.Snapshots) || ei < len(t.Events) {
		var line takeLine
		if ei >= len(t.Events) || (si < len(t.Snapshots) && t.Snapshots[si].At <= t.Events[ei].At) {
			line.Snapshot = &t.Snapshots[si]
			si++
		} else {
			line.Event = &t.Events[ei]
			ei++
		}
		if err := enc.Encode(line); err != nil {
			return fmt.Errorf("write the take: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("finish the take: %w", err)
	}
	return nil
}

// ReadTake reads a take [WriteTake] wrote.
func ReadTake(r io.Reader) (*Take, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errTakeFormat, err)
	}
	defer zr.Close() //nolint:errcheck // a read-side close reports nothing the decode has not
	dec := json.NewDecoder(bufio.NewReader(zr))
	var t Take
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("%w: read the header: %w", errTakeFormat, err)
	}
	if t.Cols <= 0 || t.Rows <= 0 {
		return nil, fmt.Errorf("%w: the header gives a %dx%d terminal", errTakeFormat, t.Cols, t.Rows)
	}
	for n := 2; ; n++ {
		var line takeLine
		err := dec.Decode(&line)
		if errors.Is(err, io.EOF) {
			return &t, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: line %d: %w", errTakeFormat, n, err)
		}
		switch {
		case line.Snapshot != nil && line.Event == nil:
			t.Snapshots = append(t.Snapshots, *line.Snapshot)
		case line.Event != nil && line.Snapshot == nil:
			t.Events = append(t.Events, *line.Event)
		default:
			return nil, fmt.Errorf("%w: line %d is neither one snapshot nor one event", errTakeFormat, n)
		}
	}
}

// SaveTake writes t to the file at path, replacing it.
func SaveTake(path string, t *Take) (err error) {
	f, err := os.Create(path) //nolint:gosec // the path is the operator's own output argument
	if err != nil {
		return fmt.Errorf("create the take: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close the take: %w", cerr)
		}
	}()
	return WriteTake(f, t)
}

// LoadTake reads the take file at path.
func LoadTake(path string) (*Take, error) {
	f, err := os.Open(path) //nolint:gosec // the path is the operator's own input argument
	if err != nil {
		return nil, fmt.Errorf("open the take: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only
	t, err := ReadTake(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}
