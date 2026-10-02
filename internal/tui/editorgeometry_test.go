package tui

import (
	"math/rand"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// ----------------------------------------------------------------------------
// The textarea geometry mirror (editorgeometry.go)
// ----------------------------------------------------------------------------
//
// The oracle tests of the line editor's string geometry: the widget mirrors are pinned against a
// real textarea (or the library it measures with), and the offset arithmetic against its own
// inverse.

// ----------------------------------------------------------------------------
// Offsets: the caret, the byte↔rune bridge and the copied span
// ----------------------------------------------------------------------------

func TestCaretOffset(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		value      string
		row, col   int
		wantOffset int
	}{
		{"start", "hello world", 0, 0, 0},
		{"midline", "hello world", 0, 6, 6},
		{"end", "hello world", 0, 11, 11},
		{"second line counts the newline", "ab\ncd", 1, 1, 4},
		{"second line start", "ab\ncd", 1, 0, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := caretOffset(c.value, c.row, c.col); got != c.wantOffset {
				t.Fatalf("caretOffset(%q,%d,%d) = %d, want %d", c.value, c.row, c.col, got, c.wantOffset)
			}
			// The offset must index []rune(value) directly — newlines preserved, soft-wraps absent.
			if r := []rune(c.value); c.wantOffset <= len(r) {
				_ = string(r[:c.wantOffset]) // must not panic
			}
		})
	}
}

// offsetToLineCol must invert caretOffset at EVERY position of a value, line ends and multi-byte
// runes included — a completion that splices mid-draft computes its new caret as an offset and can
// only drive the widget by row and column, so a single off-by-one there would drop the caret inside
// a rune. The byte↔rune bridge the mini-language crosses to reach those offsets is pinned with it.
func TestCaretOffsetRoundTrips(t *testing.T) {
	t.Parallel()
	values := []string{
		"",
		"hello world",
		"ab\ncd",
		"first line\n\nthird line\n",
		"日本語のテキスト\n絵文字 🚀 も",
		"/grill-me 見て @internal/tui/model.go",
	}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			for off := 0; off <= len([]rune(v)); off++ {
				row, col := offsetToLineCol(v, off)
				if got := caretOffset(v, row, col); got != off {
					t.Fatalf("offsetToLineCol(%q, %d) = (%d,%d), which caretOffset reads back as %d", v, off, row, col, got)
				}
				if got := runeOffsetOf(v, byteOffsetOf(v, off)); got != off {
					t.Fatalf("runeOffsetOf(byteOffsetOf(%q, %d)) = %d, want %d", v, off, got, off)
				}
			}
			// Out-of-range offsets clamp to the value's two ends rather than panicking.
			if row, col := offsetToLineCol(v, -1); row != 0 || col != 0 {
				t.Errorf("offsetToLineCol(%q, -1) = (%d,%d), want the first position", v, row, col)
			}
			if got, want := byteOffsetOf(v, len([]rune(v))+9), len(v); got != want {
				t.Errorf("byteOffsetOf(%q, past the end) = %d, want %d", v, got, want)
			}
		})
	}
}

func TestSelectionText(t *testing.T) {
	t.Parallel()
	v := "hello\nworld"
	cases := []struct {
		name string
		a, b int
		want string
	}{
		{"forward", 0, 5, "hello"},
		{"reversed gives same span", 5, 0, "hello"},
		{"across the newline", 0, 7, "hello\nw"},
		{"clamped high", 0, 999, "hello\nworld"},
		{"clamped low", -3, 5, "hello"},
		{"empty", 4, 4, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := selectionText(v, c.a, c.b); got != c.want {
				t.Fatalf("selectionText(%q,%d,%d) = %q, want %q", v, c.a, c.b, got, c.want)
			}
		})
	}
}

// TestCellToRuneOffset pins the conversion at the heart of the caret fix: a display-cell column
// maps to a rune offset, a column inside a wide rune resolves to that rune's left edge, and a
// column past the run clamps to the rune count (not the cell count).
func TestCellToRuneOffset(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		value string
		cells int
		want  int
	}{
		{"ascii midline", "hello", 3, 3},
		{"ascii clamps to rune count", "hi", 10, 2},
		{"zero cells", "abc", 0, 0},
		{"empty run", "", 5, 0},
		{"cjk start of 2nd glyph", "日本語", 2, 1}, // each Han rune is 2 cells wide
		{"cjk start of 3rd glyph", "日本語", 4, 2},
		{"cjk end", "日本語", 6, 3},
		{"cjk inside wide rune → left edge", "日本語", 5, 2},
		{"mixed: first ascii after the cjk run", "日本語 text", 7, 4}, // 6 cells cjk + 1 space, then 't'
		{"mixed clamps past end", "日本語 text", 999, 8},
		// The widget measures "⚠️" as one two-cell grapheme (uniseg), so cell 3 is the 'b' after
		// it — a per-rune ruler reads U+26A0 as one cell and U+FE0F as none and lands on 'b' a
		// cell early, taking the caret with it.
		{"vs16 cluster is two cells wide", "a⚠️b", 3, 3},
		{"vs16 end", "a⚠️b", 4, 4},
		{"vs16 clamps past end", "a⚠️b", 99, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := cellToRuneOffset([]rune(c.value), c.cells); got != c.want {
				t.Fatalf("cellToRuneOffset(%q, %d) = %d, want %d", c.value, c.cells, got, c.want)
			}
		})
	}
}

// TestCellToRuneOffsetInvertsWidth is the invariant the caret relies on: at every rune boundary,
// the offset that renders at that boundary's cumulative cell width maps back to that same
// boundary — for any script.
//
// The oracle is the widget's own cursor math, so the cumulative width is uniseg.StringWidth of the
// prefix, exactly as textarea.LineInfo computes CharOffset and textarea.Cursor its x. Reaching for
// the library directly rather than for runesWidth keeps this a check against the widget instead of
// a check of the mirror against itself. The "⚠️" fixture is the one a per-rune ruler fails: it
// reads the cluster as one cell where the widget reads two, so every boundary after it maps back
// short. The invariant holds only for prefixes whose width strictly grows, so the fixtures avoid
// combining marks.
func TestCellToRuneOffsetInvertsWidth(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"hello", "日本語 text", "aあb🙂c", "a⚠️b ⚠️", ""} {
		runes := []rune(s)
		for k := 0; k <= len(runes); k++ {
			acc := uniseg.StringWidth(string(runes[:k]))
			if got := cellToRuneOffset(runes, acc); got != k {
				t.Errorf("%q: cellToRuneOffset(., %d cells) = %d, want boundary %d", s, acc, got, k)
			}
		}
	}
}

// TestVisualSubline checks the sub-line slice caretTo feeds the cell→rune conversion: it returns
// exactly the [start, start+width) runes of the row-th logical line, bounds a wrapped row so a
// click near the wrap point cannot read into the next visual row, and clamps out-of-range inputs
// to an empty slice.
func TestVisualSubline(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		value             string
		row, start, width int
		want              string
	}{
		{"whole unwrapped line", "hello", 0, 0, 5, "hello"},
		{"second logical line", "ab\ncd", 1, 0, 2, "cd"},
		{"wrapped row starts mid-line, bounded", "abcdef", 0, 3, 3, "def"},
		{"width clamps to line end", "abc", 0, 1, 99, "bc"},
		{"row out of range → empty", "abc", 5, 0, 3, ""},
		{"start past end → empty", "abc", 0, 10, 2, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := string(visualSubline(c.value, c.row, c.start, c.width)); got != c.want {
				t.Fatalf("visualSubline(%q, %d, %d, %d) = %q, want %q", c.value, c.row, c.start, c.width, got, c.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The soft-wrap mirror, pinned against a real textarea
// ----------------------------------------------------------------------------

// wrapRowStarts is a MIRROR of the bubbles textarea's own soft-wrap, so the oracle is that widget
// itself: at every column of a value, LineInfo names the wrapped row the caret stands on (RowOffset
// — the row's INDEX — and StartColumn, that row's own rune offset), Height names the line's row
// count, and CharOffset names the display cell the caret stands at. A change to the widget's wrap
// has to fail HERE, where the accents are still only mis-measured, rather than in a screenshot
// nobody diffs.
//
// The expectation is keyed by RowOffset rather than read off in order, because a row the widget
// draws EMPTY is addressed by no column at all and would otherwise vanish from the oracle while
// still occupying a row on screen — which is precisely the row an accent would then be painted one
// line too high on. The widget leaves one when a first group overflows its row without ever
// tripping the hard-word-break, which VS16 makes reachable (that break weighs the last rune with
// go-runewidth, and U+FE0F weighs nothing there).
//
// The sanitizer cases are the reason the oracle's runes are read back OFF the widget rather than
// taken from the case: what a textarea holds is what its sanitizer let in, and that rewrites each
// TAB as four spaces and drops utf8.RuneError and every other control rune. So the widget is asked
// about the line it actually wrapped, while the mirror is handed the raw line — which is exactly the
// divergence being pinned, since a mirror that measured those runes as written would wrap such a
// draft where the widget does not: a tab weighed as one column instead of four, a dropped rune
// weighed as a column the widget never drew.
func TestWrapRowStartsMirrorsTheWidget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		line  string
		width int
	}{
		{"empty", "", 10},
		{"short", "/clean-code", 40},
		{"exactly one row", "abcde", 5},
		{"word-wrapped prose", "hello world", 5},
		{"a word longer than the row", "averyveryverylongwordindeed", 6},
		{"trailing space at a row boundary", "aaa aaa aaa aaax ", 8},
		{"a line of nothing but spaces", "     ", 3},
		{"wide runes", "日本語のテキスト 絵文字", 7},
		// The VS16 cases are the ones a per-rune mirror gets wrong: go-runewidth reads U+26A0 as
		// one cell and U+FE0F as none, while the widget measures the cluster whole (uniseg) and
		// wraps as if it were two. The widths are chosen so that difference decides a break.
		{"an emoji carrying VS16", "warn ⚠️ here", 7},
		{"a VS16 run filling the row", "⚠️⚠️⚠️ end", 6},
		{"VS16 inside a word too wide for the row", "aa⚠️bb⚠️cc", 4},
		// Tabs: the widget's sanitizer turns each one into four spaces before it wraps, so a
		// mirror that measured the tab itself would break these lines in the wrong places.
		{"a leading tab", "\tabc def", 6},
		{"a tab inside a word", "ab\tcd efgh", 6},
		{"a tab at the wrap column", "abcd\tefg", 6},
		{"a line of nothing but tabs", "\t\t", 5},
		{"a tab in a draft", "/grill-me\tcheck @internal/tui/model.go", 12},
		// The runes the sanitizer drops outright: a mirror that kept them would carry a phantom
		// column per rune and break these lines one glyph early.
		{"a replacement character ahead of a wrap boundary", "abc\uFFFDdefgh ij", 5},
		{"replacement characters inside a word too wide for the row", "aa\uFFFDbb\uFFFDcc dd", 4},
		{"a control rune inside a word", "ab\x07cd efg", 5},
		{"a control rune at the wrap column", "abcd\x07efg", 5},
		{"a line of nothing but dropped runes", "\uFFFD\x07", 1},
		{"dropped runes around a tab", "ab\x07\tcd\uFFFD efgh", 6},
		{"the acceptance draft", "/grill-me check @internal/tui/model.go and /code-adit", 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			ta := textarea.New()
			ta.Prompt = ""
			ta.ShowLineNumbers = false
			ta.CharLimit = 0
			ta.SetWidth(c.width)
			ta.SetHeight(10)
			ta.SetValue(c.line)

			got := wrapRowStarts([]rune(c.line), ta.Width())

			// The widget's geometry is addressed in the runes it KEPT, which is the raw line
			// sanitised — tabs expanded, dropped runes gone — the same space wrapRowStarts
			// answers in.
			runes := []rune(ta.Value())

			want := make([]int, len(got))
			addressed := make([]bool, len(got))
			for col := 0; col <= len(runes); col++ {
				ta.SetCursorColumn(col)
				li := ta.LineInfo()
				if li.Height != len(got) {
					t.Fatalf("col %d: widget draws %d rows, wrapRowStarts says %d (%v)", col, li.Height, len(got), got)
				}
				if li.RowOffset < 0 || li.RowOffset >= len(got) {
					t.Fatalf("col %d: widget puts the caret on row %d of %d", col, li.RowOffset, len(got))
				}
				want[li.RowOffset] = li.StartColumn
				addressed[li.RowOffset] = true
				if cw := runesWidth(runes[li.StartColumn:col]); cw != li.CharOffset {
					t.Fatalf("col %d: runesWidth from the row start = %d, widget's CharOffset = %d", col, cw, li.CharOffset)
				}
			}
			for i := len(want) - 1; i >= 0; i-- {
				if addressed[i] {
					continue
				}
				want[i] = len(runes) // a trailing empty row begins past the last rune…
				if i+1 < len(want) {
					want[i] = want[i+1] // …and any other one begins where the row below it does
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("wrapRowStarts(%q, %d) = %v, the widget wraps at %v", c.line, ta.Width(), got, want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Row counting cost (wrapRowStarts)
// ----------------------------------------------------------------------------

// baseWrapRowStarts is wrapRowStarts as it stood before the row and word widths were carried
// forward: the row and the pending word re-measured whole at every step. It is the reference the
// incremental measure must agree with rune for rune.
func baseWrapRowStarts(line []rune, width int) []int {
	if width < 1 {
		width = 1
	}
	line = sanitizeInputLine(line)
	starts := []int{0}
	consumed := 0 // runes of line already placed on a row
	wordLen := 0  // the pending word: a run of non-space runes
	spaces := 0   // the whitespace run trailing that word
	for _, r := range line {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			wordLen++
		}
		word := line[consumed : consumed+wordLen] // the widget's `word`, r included
		switch {
		case spaces > 0: // the group is finished: place it, on a new row if it does not fit
			row := line[starts[len(starts)-1]:consumed] // the widget's `lines[row]`
			if runesWidth(row)+runesWidth(word)+spaces > width {
				starts = append(starts, consumed)
			}
			consumed += wordLen + spaces
			wordLen, spaces = 0, 0
		case runesWidth(word)+runewidth.RuneWidth(r) > width: // a word wider than a row: break it here
			if consumed > starts[len(starts)-1] { // the current row already holds something
				starts = append(starts, consumed)
			}
			consumed += wordLen
			wordLen = 0
		}
	}
	row, word := line[starts[len(starts)-1]:consumed], line[consumed:consumed+wordLen]
	if runesWidth(row)+runesWidth(word)+spaces >= width {
		starts = append(starts, consumed) // the trailing row a width-filling line keeps for the caret
	}
	return starts
}

// wrapRowCorpus is the line shapes where carrying a width forward could drift from measuring the
// run whole: every grapheme cluster that spans what the mirror appends in separate steps.
var wrapRowCorpus = []string{
	"",
	"hello world, this is plain ASCII prose that wraps",
	"日本語のテキスト 絵文字 と かな カナ",
	"emoji 😀😃 in 🎉 prose 🚀",
	"warn ⚠️ here ⚠️⚠️⚠️ end aa⚠️bb⚠️cc",
	"family 👨‍👩‍👧‍👦 and 👩‍💻 zwj ❤️‍🔥 joins",
	"flags 🇩🇪🇫🇷 and a lone 🇺 then 🇺🇸🇬🇧🇯🇵",
	"cafe\u0301 re\u0301sume\u0301 and n\u0303o combining marks",
	"a \u0301b  \u0301\u0302c space then combining",
	"\tabc\tdef ghi\t\tjkl",
	"aaa aaa aaa aaax aaaaaaaa bbbbbbbbbbbbbbbbbbbbbbbbbbbbb c",
	"     spaces    only     ",
	"averyveryverylongwordindeed with some short ones after",
}

// The incremental measure answers exactly what the whole-run measure did, over every corpus line at
// every width that can break it — the soft-wrap boundaries of each line included — and over
// generated lines built from the same awkward clusters.
func TestWrapRowStartsMatchesTheWholeRunMeasure(t *testing.T) {
	t.Parallel()
	check := func(line string, width int) {
		t.Helper()
		runes := []rune(line)
		if got, want := wrapRowStarts(runes, width), baseWrapRowStarts(runes, width); !reflect.DeepEqual(got, want) {
			t.Errorf("wrapRowStarts(%q, %d) = %v, want %v", line, width, got, want)
		}
	}
	for _, line := range wrapRowCorpus {
		for width := 1; width <= uniseg.StringWidth(line)+2; width++ {
			check(line, width)
		}
	}
	glyphs := []string{"a", "b", " ", " ", "\t", "あ", "⚠️", "\u0301", "\u200d", "👩", "💻", "🇺", "🇸", "😀", "\ufe0f"}
	rng := rand.New(rand.NewSource(20260923))
	for i := 0; i < 2000; i++ {
		var sb strings.Builder
		for n := rng.Intn(32); n > 0; n-- {
			sb.WriteString(glyphs[rng.Intn(len(glyphs))])
		}
		check(sb.String(), 1+rng.Intn(16))
	}
}

// longProseLine is one 64 KB logical line of short words — a pasted paragraph with no newline.
func longProseLine() []rune {
	const sentence = "the quick brown fox jumps over the lazy dog and keeps on running "
	return []rune(strings.Repeat(sentence, 64<<10/len(sentence)))
}

// wrapRowStartsTotalAlloc is the bytes one wrapRowStarts call over line allocates at width.
func wrapRowStartsTotalAlloc(line []rune, width int) uint64 {
	var stats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&stats)
	before := stats.TotalAlloc
	runtime.KeepAlive(wrapRowStarts(line, width))
	runtime.ReadMemStats(&stats)
	return stats.TotalAlloc - before
}

// A line's wrap costs its length, not its length times the width: re-measuring the whole row at
// every placement made width 400 cost ≈ 10× width 40 on the same line. Not parallel: TotalAlloc is
// process-wide, and a neighbour's allocations would be read as this one's.
func TestWrapRowStartsAllocationIsIndependentOfWidth(t *testing.T) {
	line := longProseLine()
	narrow, wide := wrapRowStartsTotalAlloc(line, 40), wrapRowStartsTotalAlloc(line, 400)
	if wide > 2*narrow {
		t.Fatalf("wrapRowStarts over a %d-rune line allocated %d KB at width 400 against %d KB at width 40; want ≤ 2×",
			len(line), wide>>10, narrow>>10)
	}
}
