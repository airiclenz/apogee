package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// The font files the rasterizer reads from the storyboard's fonts directory. Source Code Pro is
// the terminal face; the three Noto families are tried, in this order, for a rune it lacks.
const (
	fontFileRegular = "SourceCodePro-Regular.ttf"
	fontFileBold    = "SourceCodePro-Bold.ttf"
	fontFileItalic  = "SourceCodePro-It.ttf"
)

// fallbackFontFiles is the fallback chain after Source Code Pro. Noto Sans Symbols comes last: it
// is there for the few runes (⌃ among them) the other two lack.
var fallbackFontFiles = []string{
	"NotoSansSymbols2-Regular.ttf",
	"NotoSansMath-Regular.ttf",
	"NotoSansSymbols-Regular.ttf",
}

// cellWidthProbe is the rune whose advance sets the cell width; every Source Code Pro glyph
// shares it.
const cellWidthProbe = 'M'

// faintPercent is the share of the foreground a faint cell keeps over its background.
const faintPercent = 60

// Catppuccin Mocha: the terminal's default foreground and background, and the sixteen ANSI
// colours (0–7 normal, 8–15 bright) a program's palette indices 0–15 resolve to.
var (
	mochaForeground = color.RGBA{0xcd, 0xd6, 0xf4, 0xff}
	mochaBackground = color.RGBA{0x1e, 0x1e, 0x2e, 0xff}
	mochaANSI       = [16]color.RGBA{
		{0x45, 0x47, 0x5a, 0xff}, {0xf3, 0x8b, 0xa8, 0xff}, {0xa6, 0xe3, 0xa1, 0xff}, {0xf9, 0xe2, 0xaf, 0xff},
		{0x89, 0xb4, 0xfa, 0xff}, {0xf5, 0xc2, 0xe7, 0xff}, {0x94, 0xe2, 0xd5, 0xff}, {0xba, 0xc2, 0xde, 0xff},
		{0x58, 0x5b, 0x70, 0xff}, {0xf3, 0x8b, 0xa8, 0xff}, {0xa6, 0xe3, 0xa1, 0xff}, {0xf9, 0xe2, 0xaf, 0xff},
		{0x89, 0xb4, 0xfa, 0xff}, {0xf5, 0xc2, 0xe7, 0xff}, {0x94, 0xe2, 0xd5, 0xff}, {0xa6, 0xad, 0xc8, 0xff},
	}
)

// Geometry is what a rasterized frame is laid out from: the terminal's Cols × Rows, the Padding
// in pixels around the grid, the FontSize in pixels per em and the LineHeight as a multiple of
// it. The pixel size follows from these and the font's advance — see [Rasterizer.Size].
type Geometry struct {
	Cols       int
	Rows       int
	Padding    int
	FontSize   float64
	LineHeight float64
}

// Rasterizer paints take snapshots into RGBA frames. It is safe for concurrent use: the glyph
// cache and the font faces behind it are guarded by one mutex.
type Rasterizer struct {
	geometry   Geometry
	cellWidth  int
	lineHeight int
	baseline   int

	// primary is Source Code Pro indexed by style; fallbacks is the Noto chain.
	primary   [styleCount]*sfnt.Font
	fallbacks []*sfnt.Font

	mu     sync.Mutex
	buffer sfnt.Buffer
	faces  map[*sfnt.Font]font.Face
	glyphs map[glyphKey]*image.Alpha
}

// fontStyle picks one of Source Code Pro's three files. Bold italic has no file of its own and
// draws bold.
type fontStyle int

const (
	styleRegular fontStyle = iota
	styleBold
	styleItalic
	styleCount
)

// glyphKey identifies one cached glyph mask: the rune, the style it is drawn in and how many
// pixels wide its cell span is.
type glyphKey struct {
	r     rune
	style fontStyle
	span  int
}

// NewRasterizer loads the fonts from fontsDir and derives the cell metrics for g.
func NewRasterizer(fontsDir string, g Geometry) (*Rasterizer, error) {
	if g.Cols <= 0 || g.Rows <= 0 || g.Padding < 0 || g.FontSize <= 0 || g.LineHeight <= 0 {
		return nil, fmt.Errorf("rasterizer: invalid geometry %+v", g)
	}
	r := &Rasterizer{
		geometry: g,
		faces:    map[*sfnt.Font]font.Face{},
		glyphs:   map[glyphKey]*image.Alpha{},
	}
	for style, name := range [styleCount]string{fontFileRegular, fontFileBold, fontFileItalic} {
		f, err := loadFont(fontsDir, name)
		if err != nil {
			return nil, err
		}
		r.primary[style] = f
	}
	for _, name := range fallbackFontFiles {
		f, err := loadFont(fontsDir, name)
		if err != nil {
			return nil, err
		}
		r.fallbacks = append(r.fallbacks, f)
	}

	face := r.faceLocked(r.primary[styleRegular])
	advance, ok := face.GlyphAdvance(cellWidthProbe)
	if !ok {
		return nil, errors.New("rasterizer: Source Code Pro has no advance for the cell-width probe")
	}
	r.cellWidth = int(math.Round(fixedToFloat(advance)))
	r.lineHeight = int(math.Round(g.FontSize * g.LineHeight))
	metrics := face.Metrics()
	ascent, descent := fixedToFloat(metrics.Ascent), fixedToFloat(metrics.Descent)
	r.baseline = int(math.Round((float64(r.lineHeight)-(ascent+descent))/2 + ascent))
	return r, nil
}

// loadFont reads and parses one font file.
func loadFont(dir, name string) (*sfnt.Font, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, fmt.Errorf("rasterizer: read font: %w", err)
	}
	f, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("rasterizer: parse %s: %w", name, err)
	}
	return f, nil
}

// CellWidth is the pixel width of one terminal column.
func (r *Rasterizer) CellWidth() int { return r.cellWidth }

// LineHeight is the pixel height of one terminal row.
func (r *Rasterizer) LineHeight() int { return r.lineHeight }

// Size is the frame's pixel size: the grid plus the padding on every side.
func (r *Rasterizer) Size() image.Point {
	return image.Pt(
		2*r.geometry.Padding+r.geometry.Cols*r.cellWidth,
		2*r.geometry.Padding+r.geometry.Rows*r.lineHeight,
	)
}

// Resolves reports whether ru draws as something: a procedural box or block glyph, or a glyph in
// Source Code Pro or one of its fallbacks.
func (r *Rasterizer) Resolves(ru rune) bool {
	if isProcedural(ru) {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fontForLocked(ru, styleRegular) != nil
}

// Frame paints one snapshot. Rows and columns past the geometry are ignored; a short grid leaves
// the rest of the frame on the theme background.
func (r *Rasterizer) Frame(s Snapshot) *image.RGBA {
	img := image.NewRGBA(image.Rectangle{Max: r.Size()})
	draw.Draw(img, img.Bounds(), &image.Uniform{C: mochaBackground}, image.Point{}, draw.Src)
	for y, row := range s.Cells {
		if y >= r.geometry.Rows {
			break
		}
		for x, cell := range row {
			if x >= r.geometry.Cols {
				break
			}
			// Width 0 is the second column of a wide grapheme, already painted by its first.
			if cell.Width == 0 {
				continue
			}
			r.drawCell(img, x, y, cell)
		}
	}
	return img
}

// drawCell paints one cell: its background across its whole span, then its glyph and underline
// in its foreground.
func (r *Rasterizer) drawCell(img *image.RGBA, x, y int, cell TakeCell) {
	columns := min(max(cell.Width, 1), r.geometry.Cols-x)
	span := columns * r.cellWidth
	origin := image.Pt(r.geometry.Padding+x*r.cellWidth, r.geometry.Padding+y*r.lineHeight)
	rect := image.Rectangle{Min: origin, Max: origin.Add(image.Pt(span, r.lineHeight))}

	fg, bg := cellColors(cell)
	draw.Draw(img, rect, &image.Uniform{C: bg}, image.Point{}, draw.Src)
	ink := &image.Uniform{C: fg}
	if ru, _ := utf8.DecodeRuneInString(cell.Rune); ru != utf8.RuneError && ru != ' ' {
		if mask := r.glyph(ru, styleOf(cell), span); mask != nil {
			draw.DrawMask(img, rect, ink, image.Point{}, mask, image.Point{}, draw.Over)
		}
	}
	if cell.Underline {
		thickness := lightStroke(r.cellWidth)
		top := origin.Y + min(r.baseline+thickness, r.lineHeight-thickness)
		line := image.Rect(rect.Min.X, top, rect.Max.X, top+thickness)
		draw.Draw(img, line, ink, image.Point{}, draw.Src)
	}
}

// styleOf picks the Source Code Pro file a cell draws with.
func styleOf(cell TakeCell) fontStyle {
	switch {
	case cell.Bold:
		return styleBold
	case cell.Italic:
		return styleItalic
	}
	return styleRegular
}

// cellColors resolves a cell's foreground and background through the theme, then applies reverse
// (a swap) and faint (the foreground at faintPercent over the background).
func cellColors(cell TakeCell) (fg, bg color.RGBA) {
	fg = themeColor(cell.FG, mochaForeground)
	bg = themeColor(cell.BG, mochaBackground)
	if cell.Reverse {
		fg, bg = bg, fg
	}
	if cell.Faint {
		fg = blend(bg, fg, faintPercent)
	}
	return fg, bg
}

// themeColor resolves a take colour: the terminal default to fallback, palette 0–15 to the Mocha
// ANSI colours, anything else (palette 16–255, truecolor) to its own value.
func themeColor(c TakeColor, fallback color.RGBA) color.RGBA {
	if n, ok := c.Palette(); ok && n < len(mochaANSI) {
		return mochaANSI[n]
	}
	resolved := c.Color()
	if resolved == nil {
		return fallback
	}
	return color.RGBAModel.Convert(resolved).(color.RGBA)
}

// blend mixes percent of over into under, channel by channel, in integers so every platform
// paints the same bytes.
func blend(under, over color.RGBA, percent int) color.RGBA {
	mix := func(a, b uint8) uint8 {
		return uint8((int(b)*percent + int(a)*(100-percent) + 50) / 100)
	}
	return color.RGBA{mix(under.R, over.R), mix(under.G, over.G), mix(under.B, over.B), 0xff}
}

// glyph returns the coverage mask for ru drawn in style across span pixels, or nil when nothing
// draws it. Masks are cached.
func (r *Rasterizer) glyph(ru rune, style fontStyle, span int) *image.Alpha {
	if isProcedural(ru) {
		style = styleRegular
	}
	key := glyphKey{r: ru, style: style, span: span}
	r.mu.Lock()
	defer r.mu.Unlock()
	if mask, ok := r.glyphs[key]; ok {
		return mask
	}
	var mask *image.Alpha
	if isProcedural(ru) {
		mask = proceduralGlyph(ru, span, r.lineHeight, lightStroke(r.cellWidth))
	} else {
		mask = r.fontGlyphLocked(ru, style, span)
	}
	r.glyphs[key] = mask
	return mask
}

// fontGlyphLocked draws ru from the first font in the chain that has it, centred in the span. A
// glyph wider than the span (a fallback symbol drawn in a proportional face) is scaled down to fit.
func (r *Rasterizer) fontGlyphLocked(ru rune, style fontStyle, span int) *image.Alpha {
	f := r.fontForLocked(ru, style)
	if f == nil {
		return nil
	}
	face := r.faceLocked(f)
	advance, ok := face.GlyphAdvance(ru)
	if !ok {
		return nil
	}
	if width := fixedToFloat(advance); width > float64(span) {
		scaled, err := opentype.NewFace(f, &opentype.FaceOptions{
			Size:    r.geometry.FontSize * float64(span) / width,
			DPI:     pixelsPerPoint,
			Hinting: font.HintingNone,
		})
		if err == nil {
			defer func() { _ = scaled.Close() }()
			face = scaled
			advance, _ = face.GlyphAdvance(ru)
		}
	}
	mask := image.NewAlpha(image.Rect(0, 0, span, r.lineHeight))
	dot := fixed.Point26_6{X: (fixed.I(span) - advance) / 2, Y: fixed.I(r.baseline)}
	bounds, glyphMask, maskPoint, _, ok := face.Glyph(dot, ru)
	if !ok {
		return nil
	}
	draw.DrawMask(mask, bounds, image.Opaque, image.Point{}, glyphMask, maskPoint, draw.Over)
	return mask
}

// pixelsPerPoint makes a face's Size a size in pixels: at 72 DPI one point is one pixel.
const pixelsPerPoint = 72

// fontForLocked walks the chain for ru: the style's Source Code Pro file, regular Source Code
// Pro, then the fallbacks in order. nil means no font has it.
func (r *Rasterizer) fontForLocked(ru rune, style fontStyle) *sfnt.Font {
	chain := make([]*sfnt.Font, 0, 2+len(r.fallbacks))
	chain = append(chain, r.primary[style], r.primary[styleRegular])
	chain = append(chain, r.fallbacks...)
	for _, f := range chain {
		index, err := f.GlyphIndex(&r.buffer, ru)
		if err == nil && index != 0 {
			return f
		}
	}
	return nil
}

// faceLocked returns f's face at the geometry's font size, creating it on first use.
func (r *Rasterizer) faceLocked(f *sfnt.Font) font.Face {
	if face, ok := r.faces[f]; ok {
		return face
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    r.geometry.FontSize,
		DPI:     pixelsPerPoint,
		Hinting: font.HintingNone,
	})
	if err != nil {
		// NewFace fails only on a non-positive size or DPI, which NewRasterizer rules out.
		panic(fmt.Sprintf("rasterizer: face: %v", err))
	}
	r.faces[f] = face
	return face
}

// fixedToFloat converts a 26.6 fixed-point value to pixels.
func fixedToFloat(v fixed.Int26_6) float64 { return float64(v) / 64 }

// lightStroke is the thickness of a light box-drawing line for a cell cellWidth pixels wide; a
// heavy line is twice it, a double line two light lines a light line apart.
func lightStroke(cellWidth int) int { return max(1, int(math.Round(float64(cellWidth)/8))) }

// The ranges drawn procedurally rather than from a font, so every line and block meets its
// neighbour's edge exactly: box drawing (U+2500–257F) and block elements (U+2580–259F).
const (
	boxDrawingFirst    = 0x2500
	blockElementsFirst = 0x2580
	blockElementsLast  = 0x259F
)

// isProcedural reports whether ru is drawn by [proceduralGlyph].
func isProcedural(ru rune) bool { return ru >= boxDrawingFirst && ru <= blockElementsLast }

// proceduralGlyph draws a box-drawing or block-element rune into a width × height mask; stroke is
// the light line thickness.
func proceduralGlyph(ru rune, width, height, stroke int) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	if ru >= blockElementsFirst {
		drawBlockElement(mask, ru)
		return mask
	}
	if arms, ok := boxArmTable[ru]; ok {
		drawBoxArms(mask, arms, stroke)
		return mask
	}
	if dash, ok := boxDashTable[ru]; ok {
		drawBoxDashes(mask, dash, stroke)
		return mask
	}
	switch {
	case ru >= boxArcFirst && ru <= boxArcLast:
		arc := boxArcDirections[ru-boxArcFirst]
		drawBoxArc(mask, arc[0], arc[1], stroke)
	case ru == boxDiagonalRising:
		drawDiagonal(mask, true, stroke)
	case ru == boxDiagonalFalling:
		drawDiagonal(mask, false, stroke)
	case ru == boxDiagonalCross:
		drawDiagonal(mask, true, stroke)
		drawDiagonal(mask, false, stroke)
	}
	return mask
}

// Block elements. The eighths a partial block or bar fills are rounded to whole pixels, and the
// halves of a cell are complementary: ▀ and ▄, ▌ and ▐ tile it without a gap or an overlap.
func drawBlockElement(mask *image.Alpha, ru rune) {
	width, height := mask.Rect.Dx(), mask.Rect.Dy()
	columns := func(eighths int) int { return (width*eighths + 4) / 8 }
	rows := func(eighths int) int { return (height*eighths + 4) / 8 }
	middleX, middleY := columns(4), height-rows(4)
	switch {
	case ru == 0x2580: // ▀ upper half
		fillMask(mask, image.Rect(0, 0, width, middleY), 0xff)
	case ru >= 0x2581 && ru <= 0x2588: // ▁…█ lower eighths
		fillMask(mask, image.Rect(0, height-rows(int(ru-0x2580)), width, height), 0xff)
	case ru >= 0x2589 && ru <= 0x258F: // ▉…▏ left eighths
		fillMask(mask, image.Rect(0, 0, columns(8-int(ru-0x2588)), height), 0xff)
	case ru == 0x2590: // ▐ right half
		fillMask(mask, image.Rect(middleX, 0, width, height), 0xff)
	case ru >= 0x2591 && ru <= 0x2593: // ░▒▓ shades at a quarter, half and three quarters
		fillMask(mask, mask.Rect, uint8(0x40*int(ru-0x2590)))
	case ru == 0x2594: // ▔ upper eighth
		fillMask(mask, image.Rect(0, 0, width, rows(1)), 0xff)
	case ru == 0x2595: // ▕ right eighth
		fillMask(mask, image.Rect(width-columns(1), 0, width, height), 0xff)
	default: // ▖…▟ quadrants
		quadrants := blockQuadrants[ru-0x2596]
		for bit, quadrant := range []image.Rectangle{
			image.Rect(0, 0, middleX, middleY), image.Rect(middleX, 0, width, middleY),
			image.Rect(0, middleY, middleX, height), image.Rect(middleX, middleY, width, height),
		} {
			if quadrants&(1<<bit) != 0 {
				fillMask(mask, quadrant, 0xff)
			}
		}
	}
}

// Quadrant bits: upper-left, upper-right, lower-left, lower-right.
const (
	quadrantUpperLeft = 1 << iota
	quadrantUpperRight
	quadrantLowerLeft
	quadrantLowerRight
)

// blockQuadrants maps U+2596–259F to the quadrants each fills.
var blockQuadrants = [...]uint8{
	quadrantLowerLeft,  // ▖
	quadrantLowerRight, // ▗
	quadrantUpperLeft,  // ▘
	quadrantUpperLeft | quadrantLowerLeft | quadrantLowerRight,  // ▙
	quadrantUpperLeft | quadrantLowerRight,                      // ▚
	quadrantUpperLeft | quadrantUpperRight | quadrantLowerLeft,  // ▛
	quadrantUpperLeft | quadrantUpperRight | quadrantLowerRight, // ▜
	quadrantUpperRight,                                          // ▝
	quadrantUpperRight | quadrantLowerLeft,                      // ▞
	quadrantUpperRight | quadrantLowerLeft | quadrantLowerRight, // ▟
}

// fillMask sets every pixel of r (clipped to the mask) to alpha.
func fillMask(mask *image.Alpha, r image.Rectangle, alpha uint8) {
	draw.Draw(mask, r.Intersect(mask.Rect), &image.Uniform{C: color.Alpha{A: alpha}}, image.Point{}, draw.Src)
}

// Box-drawing line weights, as boxArmTable spells them.
const (
	weightNone   = '.'
	weightLight  = 'l'
	weightHeavy  = 'h'
	weightDouble = 'd'
)

// The four arms of a box-drawing glyph, in boxArms order.
const (
	armUp = iota
	armRight
	armDown
	armLeft
	armCount
)

// boxArms is the weight of each arm running from the cell's centre to an edge: up, right, down,
// left.
type boxArms [armCount]byte

// arms spells a boxArms from its four weight letters, up-right-down-left.
func arms(spelling string) boxArms {
	var a boxArms
	copy(a[:], spelling)
	return a
}

// boxArmTable covers every box-drawing rune made only of straight arms from the centre.
var boxArmTable = map[rune]boxArms{
	0x2500: arms(".l.l"), 0x2501: arms(".h.h"), 0x2502: arms("l.l."), 0x2503: arms("h.h."),
	0x250C: arms(".ll."), 0x250D: arms(".hl."), 0x250E: arms(".lh."), 0x250F: arms(".hh."),
	0x2510: arms("..ll"), 0x2511: arms("..lh"), 0x2512: arms("..hl"), 0x2513: arms("..hh"),
	0x2514: arms("ll.."), 0x2515: arms("lh.."), 0x2516: arms("hl.."), 0x2517: arms("hh.."),
	0x2518: arms("l..l"), 0x2519: arms("l..h"), 0x251A: arms("h..l"), 0x251B: arms("h..h"),
	0x251C: arms("lll."), 0x251D: arms("lhl."), 0x251E: arms("hll."), 0x251F: arms("llh."),
	0x2520: arms("hlh."), 0x2521: arms("hhl."), 0x2522: arms("lhh."), 0x2523: arms("hhh."),
	0x2524: arms("l.ll"), 0x2525: arms("l.lh"), 0x2526: arms("h.ll"), 0x2527: arms("l.hl"),
	0x2528: arms("h.hl"), 0x2529: arms("h.lh"), 0x252A: arms("l.hh"), 0x252B: arms("h.hh"),
	0x252C: arms(".lll"), 0x252D: arms(".llh"), 0x252E: arms(".hll"), 0x252F: arms(".hlh"),
	0x2530: arms(".lhl"), 0x2531: arms(".lhh"), 0x2532: arms(".hhl"), 0x2533: arms(".hhh"),
	0x2534: arms("ll.l"), 0x2535: arms("ll.h"), 0x2536: arms("lh.l"), 0x2537: arms("lh.h"),
	0x2538: arms("hl.l"), 0x2539: arms("hl.h"), 0x253A: arms("hh.l"), 0x253B: arms("hh.h"),
	0x253C: arms("llll"), 0x253D: arms("lllh"), 0x253E: arms("lhll"), 0x253F: arms("lhlh"),
	0x2540: arms("hlll"), 0x2541: arms("llhl"), 0x2542: arms("hlhl"), 0x2543: arms("hllh"),
	0x2544: arms("hhll"), 0x2545: arms("llhh"), 0x2546: arms("lhhl"), 0x2547: arms("hhlh"),
	0x2548: arms("lhhh"), 0x2549: arms("hlhh"), 0x254A: arms("hhhl"), 0x254B: arms("hhhh"),
	0x2550: arms(".d.d"), 0x2551: arms("d.d."), 0x2552: arms(".dl."), 0x2553: arms(".ld."),
	0x2554: arms(".dd."), 0x2555: arms("..ld"), 0x2556: arms("..dl"), 0x2557: arms("..dd"),
	0x2558: arms("ld.."), 0x2559: arms("dl.."), 0x255A: arms("dd.."), 0x255B: arms("l..d"),
	0x255C: arms("d..l"), 0x255D: arms("d..d"), 0x255E: arms("ldl."), 0x255F: arms("dld."),
	0x2560: arms("ddd."), 0x2561: arms("l.ld"), 0x2562: arms("d.dl"), 0x2563: arms("d.dd"),
	0x2564: arms(".dld"), 0x2565: arms(".ldl"), 0x2566: arms(".ddd"), 0x2567: arms("ld.d"),
	0x2568: arms("dl.l"), 0x2569: arms("dd.d"), 0x256A: arms("ldld"), 0x256B: arms("dldl"),
	0x256C: arms("dddd"),
	0x2574: arms("...l"), 0x2575: arms("l..."), 0x2576: arms(".l.."), 0x2577: arms("..l."),
	0x2578: arms("...h"), 0x2579: arms("h..."), 0x257A: arms(".h.."), 0x257B: arms("..h."),
	0x257C: arms(".h.l"), 0x257D: arms("l.h."), 0x257E: arms(".l.h"), 0x257F: arms("h.l."),
}

// span is a half-open pixel interval [from, to) along one axis.
type span struct{ from, to int }

// weightBands is where a line of weight crosses an axis whose light line starts at start: one
// band for light and heavy, two for double.
func weightBands(weight byte, start, stroke int) []span {
	switch weight {
	case weightLight:
		return []span{{start, start + stroke}}
	case weightHeavy:
		heavyStart := start - stroke/2
		return []span{{heavyStart, heavyStart + 2*stroke}}
	case weightDouble:
		return []span{{start - stroke, start}, {start + stroke, start + 2*stroke}}
	}
	return nil
}

// drawBoxArms draws each arm from the edge in to the centre. A light or heavy arm reaches across
// the perpendicular arms' lines so the joint is solid; each line of a double arm stops where the
// joint needs it — at the near line of a perpendicular double, on the far line of an outer
// corner — so ╔ ╠ ╬ come out as two unbroken strokes.
func drawBoxArms(mask *image.Alpha, a boxArms, stroke int) {
	width, height := mask.Rect.Dx(), mask.Rect.Dy()
	centreX, centreY := (width-stroke)/2, (height-stroke)/2
	for arm := range armCount {
		weight := a[arm]
		if weight == weightNone {
			continue
		}
		horizontal := arm == armRight || arm == armLeft
		toward := 1
		if arm == armLeft || arm == armUp {
			toward = -1
		}
		alongStart, alongSize, acrossStart := centreY, height, centreX
		negativeSide, positiveSide := a[armLeft], a[armRight]
		if horizontal {
			alongStart, alongSize, acrossStart = centreX, width, centreY
			negativeSide, positiveSide = a[armUp], a[armDown]
		}
		opposite := a[(arm+2)%armCount]

		lines := weightBands(weight, acrossStart, stroke)
		for i, across := range lines {
			var reach span
			if weight == weightDouble {
				near, far := negativeSide, positiveSide
				if i == 1 {
					near, far = positiveSide, negativeSide
				}
				reach = doubleLineReach(near, far, opposite, alongStart, stroke, toward)
			} else {
				reach = solidLineReach(negativeSide, positiveSide, alongStart, stroke)
			}
			along := span{reach.from, alongSize}
			if toward < 0 {
				along = span{0, reach.to}
			}
			rect := image.Rect(along.from, across.from, along.to, across.to)
			if !horizontal {
				rect = image.Rect(across.from, along.from, across.to, along.to)
			}
			fillMask(mask, rect, 0xff)
		}
	}
}

// solidLineReach is the stretch of the along axis the perpendicular arms' lines cover, which a
// light or heavy arm reaches into; with no perpendicular arm it is the centre's light band.
func solidLineReach(negativeSide, positiveSide byte, alongStart, stroke int) span {
	reach := span{alongStart, alongStart + stroke}
	first := true
	for _, weight := range []byte{negativeSide, positiveSide} {
		for _, band := range weightBands(weight, alongStart, stroke) {
			if first {
				reach, first = band, false
				continue
			}
			reach = span{min(reach.from, band.from), max(reach.to, band.to)}
		}
	}
	return reach
}

// doubleLineReach is where one line of a double arm stops. near is the perpendicular arm on the
// line's own side, far the one across from it, opposite the arm continuing straight on; toward
// is +1 for an arm running to the right or bottom edge. Only the side facing the arm's edge of
// the returned span is used.
func doubleLineReach(near, far, opposite byte, alongStart, stroke, toward int) span {
	nearBands := weightBands(near, alongStart, stroke)
	farBands := weightBands(far, alongStart, stroke)
	outer := span{alongStart - stroke, alongStart + 2*stroke}
	switch {
	case near == weightDouble:
		// Stop on the perpendicular's line nearest this arm's edge — the inner corner.
		if toward > 0 {
			return nearBands[1]
		}
		return nearBands[0]
	case near != weightNone:
		return nearBands[0]
	case opposite != weightNone:
		return outer
	case far == weightDouble:
		return outer
	case far != weightNone:
		return farBands[0]
	}
	return span{alongStart, alongStart + stroke}
}

// boxDash is a dashed line: its orientation, weight and how many dashes fill the cell.
type boxDash struct {
	horizontal bool
	weight     byte
	count      int
}

// boxDashTable covers the dashed box-drawing runes.
var boxDashTable = map[rune]boxDash{
	0x2504: {true, weightLight, 3}, 0x2505: {true, weightHeavy, 3},
	0x2506: {false, weightLight, 3}, 0x2507: {false, weightHeavy, 3},
	0x2508: {true, weightLight, 4}, 0x2509: {true, weightHeavy, 4},
	0x250A: {false, weightLight, 4}, 0x250B: {false, weightHeavy, 4},
	0x254C: {true, weightLight, 2}, 0x254D: {true, weightHeavy, 2},
	0x254E: {false, weightLight, 2}, 0x254F: {false, weightHeavy, 2},
}

// drawBoxDashes splits the cell's length into count equal slots and draws a dash centred in each,
// a third of the slot left as the gap.
func drawBoxDashes(mask *image.Alpha, dash boxDash, stroke int) {
	width, height := mask.Rect.Dx(), mask.Rect.Dy()
	length, across := height, weightBands(dash.weight, (width-stroke)/2, stroke)[0]
	if dash.horizontal {
		length, across = width, weightBands(dash.weight, (height-stroke)/2, stroke)[0]
	}
	for i := range dash.count {
		from, to := i*length/dash.count, (i+1)*length/dash.count
		gap := max(1, (to-from)/3)
		from, to = from+gap/2, to-(gap-gap/2)
		rect := image.Rect(across.from, from, across.to, to)
		if dash.horizontal {
			rect = image.Rect(from, across.from, to, across.to)
		}
		fillMask(mask, rect, 0xff)
	}
}

// The rounded corners ╭╮╯╰ and the diagonals ╱╲╳.
const (
	boxArcFirst        = 0x256D
	boxArcLast         = 0x2570
	boxDiagonalRising  = 0x2571
	boxDiagonalFalling = 0x2572
	boxDiagonalCross   = 0x2573
)

// boxArcDirections gives each rounded corner the edges it joins: horizontal (+1 right, −1 left)
// and vertical (+1 down, −1 up).
var boxArcDirections = [...][2]float64{
	{1, 1},   // ╭
	{-1, 1},  // ╮
	{-1, -1}, // ╯
	{1, -1},  // ╰
}

// arcSegments is how many straight pieces approximate a rounded corner's quarter circle.
const arcSegments = 16

// drawBoxArc strokes a rounded corner in a light line: straight in from the vertical edge along
// the light line's centre, a quarter circle, and straight out to the horizontal edge, so it meets
// ─ and │ in the neighbouring cells exactly.
func drawBoxArc(mask *image.Alpha, horizontal, vertical float64, stroke int) {
	width, height := float64(mask.Rect.Dx()), float64(mask.Rect.Dy())
	half := float64(stroke) / 2
	lineX := float64((mask.Rect.Dx()-stroke)/2) + half
	lineY := float64((mask.Rect.Dy()-stroke)/2) + half
	// One stroke short of the tightest edge, so the corner leaves the cell on a straight run that
	// meets the neighbouring line flush.
	radius := max(half, min(lineX, width-lineX, lineY, height-lineY)-float64(stroke))
	centreX, centreY := lineX+horizontal*radius, lineY+vertical*radius
	edgeX, edgeY := width, height
	if horizontal < 0 {
		edgeX = 0
	}
	if vertical < 0 {
		edgeY = 0
	}

	type point struct{ x, y, normalX, normalY float64 }
	path := []point{{lineX, edgeY, -horizontal, 0}}
	for i := range arcSegments + 1 {
		angle := float64(i) / arcSegments * math.Pi / 2
		dx, dy := -horizontal*math.Cos(angle), -vertical*math.Sin(angle)
		path = append(path, point{centreX + radius*dx, centreY + radius*dy, dx, dy})
	}
	path = append(path, point{edgeX, lineY, 0, -vertical})

	outline := make([][2]float64, 0, 2*len(path))
	for _, p := range path {
		outline = append(outline, [2]float64{p.x + p.normalX*half, p.y + p.normalY*half})
	}
	for i := len(path) - 1; i >= 0; i-- {
		p := path[i]
		outline = append(outline, [2]float64{p.x - p.normalX*half, p.y - p.normalY*half})
	}
	fillPolygon(mask, outline)
}

// drawDiagonal strokes a light line corner to corner: bottom-left to top-right when rising,
// top-left to bottom-right otherwise.
func drawDiagonal(mask *image.Alpha, rising bool, stroke int) {
	width, height := float64(mask.Rect.Dx()), float64(mask.Rect.Dy())
	fromX, fromY, toX, toY := 0.0, 0.0, width, height
	if rising {
		fromY, toY = height, 0
	}
	length := math.Hypot(toX-fromX, toY-fromY)
	half := float64(stroke) / 2
	normalX, normalY := -(toY-fromY)/length*half, (toX-fromX)/length*half
	fillPolygon(mask, [][2]float64{
		{fromX + normalX, fromY + normalY}, {toX + normalX, toY + normalY},
		{toX - normalX, toY - normalY}, {fromX - normalX, fromY - normalY},
	})
}

// fillPolygon adds an anti-aliased closed polygon's coverage to the mask.
func fillPolygon(mask *image.Alpha, points [][2]float64) {
	z := vector.NewRasterizer(mask.Rect.Dx(), mask.Rect.Dy())
	z.MoveTo(float32(points[0][0]), float32(points[0][1]))
	for _, p := range points[1:] {
		z.LineTo(float32(p[0]), float32(p[1]))
	}
	z.ClosePath()
	z.Draw(mask, mask.Rect, image.Opaque, image.Point{})
}
