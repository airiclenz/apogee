package main

import (
	"crypto/sha256"
	"encoding/hex"
	"go/scanner"
	"go/token"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// testFontsDir is the committed fonts directory as seen from this package (tests run in
// cmd/demorig).
const testFontsDir = "../../graphics/demo/fonts"

// newTestRasterizer builds a rasterizer over the committed fonts at a small, fixed geometry.
func newTestRasterizer(t *testing.T, cols, rows int) *Rasterizer {
	t.Helper()
	r, err := NewRasterizer(testFontsDir, Geometry{Cols: cols, Rows: rows, Padding: 8, FontSize: 15, LineHeight: 1.2})
	if err != nil {
		t.Fatalf("NewRasterizer: %v", err)
	}
	return r
}

// gridOfCells builds a snapshot grid of blank default cells, cols × rows.
func gridOfCells(cols, rows int) [][]TakeCell {
	cells := make([][]TakeCell, rows)
	for y := range cells {
		cells[y] = make([]TakeCell, cols)
		for x := range cells[y] {
			cells[y][x] = TakeCell{Rune: " ", Width: 1}
		}
	}
	return cells
}

// cellRect is the pixel rectangle of cell (x, y) spanning columns columns.
func cellRect(r *Rasterizer, x, y, columns int) image.Rectangle {
	origin := image.Pt(r.geometry.Padding+x*r.CellWidth(), r.geometry.Padding+y*r.LineHeight())
	return image.Rectangle{Min: origin, Max: origin.Add(image.Pt(columns*r.CellWidth(), r.LineHeight()))}
}

// assertFilled fails unless every pixel of rect is want.
func assertFilled(t *testing.T, img *image.RGBA, rect image.Rectangle, want color.RGBA) {
	t.Helper()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if got := img.RGBAAt(x, y); got != want {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestRasterSizeFollowsTheFormula(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                    string
		geometry                Geometry
		wantCellWidth, wantLine int
	}{
		// Source Code Pro advances 600 of 1000 units per em.
		{"font 15", Geometry{Cols: 20, Rows: 4, Padding: 8, FontSize: 15, LineHeight: 1.2}, 9, 18},
		{"font 30", Geometry{Cols: 135, Rows: 44, Padding: 32, FontSize: 30, LineHeight: 1.0}, 18, 30},
		{"fractional line", Geometry{Cols: 10, Rows: 3, Padding: 0, FontSize: 20, LineHeight: 1.33}, 12, 27},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, err := NewRasterizer(testFontsDir, tc.geometry)
			if err != nil {
				t.Fatalf("NewRasterizer: %v", err)
			}
			if r.CellWidth() != tc.wantCellWidth || r.LineHeight() != tc.wantLine {
				t.Fatalf("cell %d × %d, want %d × %d", r.CellWidth(), r.LineHeight(), tc.wantCellWidth, tc.wantLine)
			}
			g := tc.geometry
			want := image.Pt(2*g.Padding+g.Cols*tc.wantCellWidth, 2*g.Padding+g.Rows*tc.wantLine)
			if got := r.Size(); got != want {
				t.Fatalf("Size() = %v, want %v", got, want)
			}
			if got := r.Frame(Snapshot{}).Bounds().Size(); got != want {
				t.Fatalf("frame size = %v, want %v", got, want)
			}
		})
	}
}

func TestRasterRejectsMissingFonts(t *testing.T) {
	t.Parallel()
	_, err := NewRasterizer(t.TempDir(), Geometry{Cols: 1, Rows: 1, FontSize: 15, LineHeight: 1})
	if err == nil {
		t.Fatal("NewRasterizer over an empty directory succeeded")
	}
}

// TestRasterResolvesEveryTUIRune walks every string and rune literal in apogee's TUI sources and
// requires each non-ASCII rune to draw — from the font chain or procedurally.
func TestRasterResolvesEveryTUIRune(t *testing.T) {
	t.Parallel()
	r := newTestRasterizer(t, 1, 1)
	files, err := filepath.Glob("../../internal/tui/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob internal/tui: %v (%d files)", err, len(files))
	}
	seen := map[rune]string{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		for _, ru := range literalRunes(t, path) {
			if ru >= 0x80 {
				seen[ru] = filepath.Base(path)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("found no non-ASCII runes in internal/tui — the scan is broken")
	}
	for ru, file := range seen {
		if !r.Resolves(ru) {
			t.Errorf("U+%04X %q (%s) resolves to no glyph", ru, ru, file)
		}
	}
}

// literalRunes returns every rune of every string and rune literal in a Go source file.
func literalRunes(t *testing.T, path string) []rune {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fileSet := token.NewFileSet()
	var s scanner.Scanner
	s.Init(fileSet.AddFile(path, -1, len(source)), source, nil, 0)
	var runes []rune
	for {
		_, tok, literal := s.Scan()
		switch tok {
		case token.EOF:
			return runes
		case token.STRING:
			text, err := strconv.Unquote(literal)
			if err != nil {
				t.Fatalf("%s: unquote %s: %v", path, literal, err)
			}
			runes = append(runes, []rune(text)...)
		case token.CHAR:
			ru, _, _, err := strconv.UnquoteChar(literal[1:len(literal)-1], '\'')
			if err != nil {
				t.Fatalf("%s: unquote %s: %v", path, literal, err)
			}
			runes = append(runes, ru)
		}
	}
}

func TestRasterResolvesControlCaret(t *testing.T) {
	t.Parallel()
	r := newTestRasterizer(t, 1, 1)
	if !r.Resolves('⌃') {
		t.Fatal("⌃ resolves to no glyph")
	}
	cells := gridOfCells(1, 1)
	cells[0][0].Rune = "⌃"
	img := r.Frame(Snapshot{Cells: cells})
	if countInk(img, cellRect(r, 0, 0, 1)) == 0 {
		t.Fatal("⌃ painted nothing")
	}
}

// countInk counts the pixels of rect that differ from the theme background.
func countInk(img *image.RGBA, rect image.Rectangle) int {
	inked := 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if img.RGBAAt(x, y) != mochaBackground {
				inked++
			}
		}
	}
	return inked
}

func TestRasterFullBlockFillsItsCell(t *testing.T) {
	t.Parallel()
	r := newTestRasterizer(t, 3, 1)
	red := color.RGBA{0xff, 0, 0, 0xff}
	cells := gridOfCells(3, 1)
	cells[0][1] = TakeCell{Rune: "█", Width: 1, FG: "#ff0000"}
	img := r.Frame(Snapshot{Cells: cells})
	rect := cellRect(r, 1, 0, 1)
	assertFilled(t, img, rect, red)
	assertFilled(t, img, cellRect(r, 0, 0, 1), mochaBackground)
	assertFilled(t, img, cellRect(r, 2, 0, 1), mochaBackground)
}

func TestRasterWideCellSpansTwoColumns(t *testing.T) {
	t.Parallel()
	r := newTestRasterizer(t, 3, 1)
	cells := gridOfCells(3, 1)
	cells[0][0] = TakeCell{Rune: "█", Width: 2, FG: "#00ff00"}
	cells[0][1] = TakeCell{Width: 0}
	img := r.Frame(Snapshot{Cells: cells})
	assertFilled(t, img, cellRect(r, 0, 0, 2), color.RGBA{0, 0xff, 0, 0xff})
	assertFilled(t, img, cellRect(r, 2, 0, 1), mochaBackground)
}

func TestRasterColors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		cell   TakeCell
		wantFG color.RGBA
		wantBG color.RGBA
	}{
		{"defaults", TakeCell{}, mochaForeground, mochaBackground},
		{"ansi red on bright black", TakeCell{FG: "@1", BG: "@8"}, mochaANSI[1], mochaANSI[8]},
		{"truecolor passes through", TakeCell{FG: "#123456"}, color.RGBA{0x12, 0x34, 0x56, 0xff}, mochaBackground},
		{"palette above 15 is xterm", TakeCell{FG: "@196"}, color.RGBA{0xff, 0, 0, 0xff}, mochaBackground},
		{"reverse swaps", TakeCell{FG: "@2", Reverse: true}, mochaBackground, mochaANSI[2]},
		{
			"faint is 60 percent over the background",
			TakeCell{FG: "#ffffff", BG: "#000000", Faint: true},
			color.RGBA{0x99, 0x99, 0x99, 0xff}, color.RGBA{0, 0, 0, 0xff},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newTestRasterizer(t, 2, 1)
			cells := gridOfCells(2, 1)
			tc.cell.Rune, tc.cell.Width = "█", 1
			cells[0][0] = tc.cell
			cells[0][1] = TakeCell{Rune: " ", Width: 1, FG: tc.cell.FG, BG: tc.cell.BG, Reverse: tc.cell.Reverse}
			img := r.Frame(Snapshot{Cells: cells})
			assertFilled(t, img, cellRect(r, 0, 0, 1), tc.wantFG)
			assertFilled(t, img, cellRect(r, 1, 0, 1), tc.wantBG)
		})
	}
}

// TestRasterRoundedCornerCoverage checks ╭ by where it has ink, not by its bytes: the arc's
// anti-aliasing differs by architecture. It must leave its cell on the light line's column at the
// bottom edge and on the light line's row at the right edge, and keep off the far corner.
func TestRasterRoundedCornerCoverage(t *testing.T) {
	t.Parallel()
	r := newTestRasterizer(t, 1, 1)
	cells := gridOfCells(1, 1)
	cells[0][0].Rune = "╭"
	img := r.Frame(Snapshot{Cells: cells})
	rect := cellRect(r, 0, 0, 1)
	stroke := lightStroke(r.CellWidth())
	lineX := rect.Min.X + (r.CellWidth()-stroke)/2
	lineY := rect.Min.Y + (r.LineHeight()-stroke)/2
	for _, p := range []image.Point{{lineX, rect.Max.Y - 1}, {rect.Max.X - 1, lineY}} {
		if img.RGBAAt(p.X, p.Y) != mochaForeground {
			t.Errorf("╭ has no full ink at %v", p)
		}
	}
	if img.RGBAAt(rect.Min.X, rect.Min.Y) != mochaBackground {
		t.Error("╭ inked its top-left corner")
	}
	if countInk(img, rect) == 0 {
		t.Fatal("╭ painted nothing")
	}
}

// goldenAxisAlignedHash is the SHA-256 of the RGBA pixels of goldenSnapshot. Only axis-aligned
// integer fills are in it — straight box lines, blocks, eighths, spaces and colours — so it is the
// same on every architecture.
const goldenAxisAlignedHash = "ab4d1a6e3960e3bcac72617020408e95d21e62fd0594d2cd6853b416b596caf6"

// goldenSnapshot is a fixed 20 × 4 grid of straight and double box lines, blocks, eighths,
// shades, coloured spaces, a reversed and a faint cell.
func goldenSnapshot() Snapshot {
	rows := []string{
		"┌──┬──┐ ┏━━┓ ╔══╗ ▀▄",
		"│  ┼  │ ┃┝┶┃ ╠╬╦╣ ▌▐",
		"└──┴──┘ ┗━━┛ ╚══╝ ░▒",
		"▁▂▃▄▅▆▇█▉▊▋▌▍▎▏▔▕▙▚┊",
	}
	cells := make([][]TakeCell, len(rows))
	for y, row := range rows {
		for _, ru := range row {
			cells[y] = append(cells[y], TakeCell{Rune: string(ru), Width: 1})
		}
	}
	cells[0][7] = TakeCell{Rune: " ", Width: 1, BG: "@4"}
	cells[1][7] = TakeCell{Rune: " ", Width: 1, BG: "#fab387"}
	cells[2][7] = TakeCell{Rune: "█", Width: 1, FG: "@3", Reverse: true}
	cells[3][0].Faint = true
	cells[3][1].FG = "@2"
	return Snapshot{Cells: cells}
}

func TestRasterGoldenAxisAlignedFills(t *testing.T) {
	t.Parallel()
	r := newTestRasterizer(t, 20, 4)
	img := r.Frame(goldenSnapshot())
	sum := sha256.Sum256(img.Pix)
	if got := hex.EncodeToString(sum[:]); got != goldenAxisAlignedHash {
		t.Fatalf("golden hash = %s, want %s", got, goldenAxisAlignedHash)
	}
}
