package tuitest

import (
	"image/color"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Style is what a terminal cell looks like, reduced to the four things apogee's own rendering
// decides: the two colours and the three attributes the colour scheme actually uses. A nil colour
// is the terminal's default, which is a real value and not "unknown" — the difference between "the
// footer paints its own grey" and "the footer leaves the terminal's foreground alone" is exactly
// the kind of regression this type exists to catch.
type Style struct {
	FG, BG  color.Color
	Bold    bool
	Faint   bool
	Reverse bool
}

// Equal reports whether two styles would look the same. Colours compare by their resolved RGBA
// rather than by Go equality, so an indexed colour and the literal it resolves to are one style —
// a renderer is free to spell a colour either way and a test should not care which it picked.
func (s Style) Equal(o Style) bool {
	return s.Bold == o.Bold && s.Faint == o.Faint && s.Reverse == o.Reverse &&
		SameColor(s.FG, o.FG) && SameColor(s.BG, o.BG)
}

// String renders the style the way a failure message wants it: only what is set, and "default" for
// a colour the terminal decides.
func (s Style) String() string {
	parts := []string{"fg=" + colorName(s.FG), "bg=" + colorName(s.BG)}
	for _, attr := range []struct {
		on   bool
		name string
	}{{s.Bold, "bold"}, {s.Faint, "faint"}, {s.Reverse, "reverse"}} {
		if attr.on {
			parts = append(parts, attr.name)
		}
	}
	return strings.Join(parts, " ")
}

// SameColor reports whether two terminal colours resolve to the same thing, treating nil (the
// terminal's default) as a value of its own.
func SameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// colorName spells a colour for a human reading a failure.
func colorName(c color.Color) string {
	if c == nil {
		return "default"
	}
	r, g, b, _ := c.RGBA()
	const hex = "0123456789abcdef"
	out := []byte("#......")
	for i, v := range []uint32{r >> 8, g >> 8, b >> 8} {
		out[1+i*2] = hex[(v>>4)&0xf]
		out[2+i*2] = hex[v&0xf]
	}
	return string(out)
}

// Cell is one screen cell: the grapheme it shows, how many columns that grapheme occupies, and
// how it is painted. Width is the emulator's answer and nobody else's — it is the authority a
// glyph-alignment claim (T-20) is settled against, because counting runes in a Go string is
// exactly the measurement that gets alignment wrong.
//
// The second column of a double-width grapheme is a continuation: Rune "" and Width 0. It is a
// real cell, addressable at its own x, and it contributes no text to the row.
type Cell struct {
	Style
	Rune  string
	Width int
}

// Run is a maximal horizontal span of cells on one row that share a Style — the primitive a colour
// assertion is written against. X and Width are terminal columns, so a run's bounds mean the same
// thing whatever is inside it.
type Run struct {
	Style
	X     int
	Width int
	Text  string
}

// Frame is one immutable snapshot of a terminal: what it showed, cell by cell, at the moment
// [Screen.Snapshot] was called. Nothing in it moves afterwards, so a test can assert against it
// while the program keeps painting.
type Frame struct {
	cells   [][]Cell
	rows    []string
	cols    [][]int // per row: the starting column of the byte at each index of rows[y]
	cursorX int
	cursorY int
	width   int
	height  int
	styled  string
}

// String is the frame as plain text: every row, trailing spaces trimmed, joined by newlines. It is
// what a golden file holds and what a failure prints.
func (f Frame) String() string { return strings.Join(f.rows, "\n") }

// Styled is the frame with its SGR sequences intact — the same picture String gives, but with the
// colours still in it. It is for eyes and for judges, never for a golden: escape sequences make a
// diff unreadable and a renderer is free to re-spell them.
func (f Frame) Styled() string { return f.styled }

// Rows returns the frame's rows as plain text, one entry per terminal row.
func (f Frame) Rows() []string { return append([]string(nil), f.rows...) }

// Row returns row y as plain text, or "" when y is off the frame.
func (f Frame) Row(y int) string {
	if y < 0 || y >= len(f.rows) {
		return ""
	}
	return f.rows[y]
}

// Cell returns the cell at column x of row y, or the zero Cell when the position is off the frame.
func (f Frame) Cell(x, y int) Cell {
	if y < 0 || y >= len(f.cells) || x < 0 || x >= len(f.cells[y]) {
		return Cell{}
	}
	return f.cells[y][x]
}

// Cursor returns the cursor's column and row.
func (f Frame) Cursor() (int, int) { return f.cursorX, f.cursorY }

// Width returns the frame's width in columns.
func (f Frame) Width() int { return f.width }

// Height returns the frame's height in rows.
func (f Frame) Height() int { return f.height }

// Find returns the COLUMN and row of the first occurrence of text, scanning rows top to bottom. It
// answers in columns rather than byte offsets, so the x it reports is the x [Frame.Cell] takes —
// a row holding a wide rune or any multi-byte character makes those two different numbers.
func (f Frame) Find(text string) (int, int, bool) {
	for y := range f.rows {
		i := strings.Index(f.rows[y], text)
		if i < 0 {
			continue
		}
		return f.cols[y][i], y, true
	}
	return 0, 0, false
}

// StyleRuns splits row y into maximal spans of cells sharing one Style. Trailing blank cells are
// dropped with the row's trailing spaces, so a run list ends where the visible row ends.
func (f Frame) StyleRuns(y int) []Run {
	if y < 0 || y >= len(f.cells) {
		return nil
	}
	var runs []Run
	for x := 0; x < len(f.cells[y]); {
		c := f.cells[y][x]
		step := c.Width
		if step < 1 {
			step = 1 // a continuation cell, already covered by the grapheme that owns it
		}
		text := c.Rune
		if text == "" && c.Width > 0 {
			text = " "
		}
		if n := len(runs) - 1; n >= 0 && runs[n].Style.Equal(c.Style) {
			runs[n].Width += step
			runs[n].Text += text
		} else {
			runs = append(runs, Run{Style: c.Style, X: x, Width: step, Text: text})
		}
		x += step
	}
	// A row is only as long as its visible text; the blank tail belongs to no assertion.
	for len(runs) > 0 {
		last := &runs[len(runs)-1]
		trimmed := strings.TrimRight(last.Text, " ")
		if trimmed == last.Text {
			break
		}
		last.Width -= len(last.Text) - len(trimmed)
		last.Text = trimmed
		if last.Text != "" {
			break
		}
		runs = runs[:len(runs)-1]
	}
	return runs
}

// newFrame reads a snapshot out of an emulator. The caller holds the screen's lock: everything
// here reads emulator state and the result must be consistent with itself.
func newFrame(w, h int, cellAt func(x, y int) *uv.Cell, cursorX, cursorY int, styled string) Frame {
	f := Frame{
		cells:   make([][]Cell, h),
		rows:    make([]string, h),
		cols:    make([][]int, h),
		cursorX: cursorX,
		cursorY: cursorY,
		width:   w,
		height:  h,
		styled:  styled,
	}
	for y := 0; y < h; y++ {
		row := make([]Cell, w)
		var text strings.Builder
		cols := make([]int, 0, w)
		for x := 0; x < w; x++ {
			row[x] = convertCell(cellAt(x, y))
		}
		for x := 0; x < w; {
			c := row[x]
			step := c.Width
			if step < 1 {
				step = 1
			}
			s := c.Rune
			if s == "" {
				// An unset cell. A wide grapheme's continuation is never reached: the cell that
				// owns it advanced x past it.
				s = " "
			}
			text.WriteString(s)
			for range len(s) {
				cols = append(cols, x)
			}
			x += step
		}
		f.cells[y] = row
		full := text.String()
		f.rows[y] = strings.TrimRight(full, " ")
		f.cols[y] = cols[:len(f.rows[y])]
	}
	return f
}

// convertCell narrows an emulator cell to the part a test asserts on.
func convertCell(c *uv.Cell) Cell {
	if c == nil {
		return Cell{Rune: " ", Width: 1}
	}
	return Cell{
		Style: Style{
			FG:      c.Style.Fg,
			BG:      c.Style.Bg,
			Bold:    c.Style.Attrs&uv.AttrBold != 0,
			Faint:   c.Style.Attrs&uv.AttrFaint != 0,
			Reverse: c.Style.Attrs&uv.AttrReverse != 0,
		},
		Rune:  c.Content,
		Width: c.Width,
	}
}
