package tui

import (
	"context"
	"math/rand"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// ----------------------------------------------------------------------------
// Inline token accents (inputaccent.go)
// ----------------------------------------------------------------------------

// accentOpener is the escape sequence style opens a span with — what marks a rendered row as
// carrying that accent. It fails the test outright when the style renders no sequence at all,
// because every assertion below would otherwise pass vacuously against uncoloured output.
func accentOpener(t *testing.T, style lipgloss.Style) string {
	t.Helper()
	rendered := style.Render("x")
	i := strings.Index(rendered, "x")
	if i <= 0 {
		t.Fatalf("style renders no escape sequence around its text (%q) — the accent assertions would be vacuous", rendered)
	}
	return rendered[:i]
}

// rowsWithAccent lists the indexes of the block's rows carrying opener, paired with their plain
// text, so a test can assert WHICH row lit up and not merely that one did.
func rowsWithAccent(block, opener string) map[int]string {
	out := map[int]string{}
	for i, row := range strings.Split(block, "\n") {
		if strings.Contains(row, opener) {
			out[i] = ansi.Strip(row)
		}
	}
	return out
}

// accentTestModel is an idle model at the given window width with a two-skill catalog and a
// workspace root, its prompt already holding value and the box laid out around it.
func accentTestModel(t *testing.T, width int, workspace, value string) Model {
	t.Helper()
	opts := skillOpts()
	opts.Workspace = workspace
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: width, Height: 24})
	m.input.SetValue(value)
	m.input.MoveToEnd()
	m.layout()
	return m
}

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

// The byte range → visual geometry mapping: within one row, across a soft-wrap, and offset by the
// rows the logical lines above it occupy. A range crossing a newline is not a token this pass ever
// drew, and yields nothing rather than a guess.
//
// The two coordinates answer to two oracles, and the VS16 cases are where that shows: the ROW comes
// from the widget's wrap, which counts "⚠️" as two cells whatever the terminal does, while the
// COLUMNS come from the width authority, which counts it as the painter will — one cell on a
// terminal that never answered mode 2027, two on one that did.
func TestInputCellSpans(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		measure    widthAuthority // the zero value is the painter's default, ansi.WcWidth
		value      string
		width      int
		from, to   int // rune offsets
		wantSpans  []inputCellSpan
		wantNothin bool
	}{
		{
			name:      "a token inside one row",
			value:     "run /clean-code now",
			width:     40,
			from:      4,
			to:        15,
			wantSpans: []inputCellSpan{{row: 0, c0: 4, c1: 15}},
		},
		{
			name:      "a token straddling a soft-wrap",
			value:     "@internal/tui/model.go",
			width:     16,
			from:      0,
			to:        22,
			wantSpans: []inputCellSpan{{row: 0, c0: 0, c1: 16}, {row: 1, c0: 0, c1: 6}},
		},
		{
			name:      "a token on the second logical line counts the first line's rows",
			value:     "hello world\n/review",
			width:     5,
			from:      12,
			to:        19,
			wantSpans: []inputCellSpan{{row: 4, c0: 0, c1: 5}, {row: 5, c0: 0, c1: 2}},
		},
		{
			name:      "wide runes measure in cells, not runes",
			value:     "日本 /review",
			width:     40,
			from:      3,
			to:        10,
			wantSpans: []inputCellSpan{{row: 0, c0: 5, c1: 12}},
		},
		{
			name:      "a VS16 glyph ahead of the token takes the painter's one cell",
			value:     "a⚠️ /review", // runes: a, U+26A0, U+FE0F, ' ', then the 7-rune token
			width:     40,
			from:      4,
			to:        11,
			wantSpans: []inputCellSpan{{row: 0, c0: 3, c1: 10}},
		},
		{
			name:      "the same glyph takes two cells once the painter answers mode 2027",
			measure:   widthAuthority{method: ansi.GraphemeWidth},
			value:     "a⚠️ /review",
			width:     40,
			from:      4,
			to:        11,
			wantSpans: []inputCellSpan{{row: 0, c0: 4, c1: 11}},
		},
		{name: "a range across a newline is no token", value: "a\nb", width: 40, from: 0, to: 3, wantNothin: true},
		{name: "an empty range", value: "/review", width: 40, from: 3, to: 3, wantNothin: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := inputCellSpans(c.measure, c.value, c.width, c.from, c.to)
			if c.wantNothin {
				if got != nil {
					t.Fatalf("inputCellSpans = %v, want nothing", got)
				}
				return
			}
			if !reflect.DeepEqual(got, c.wantSpans) {
				t.Fatalf("inputCellSpans(%q, %d, %d, %d) = %v, want %v", c.value, c.width, c.from, c.to, got, c.wantSpans)
			}
		})
	}
}

// The accent is resolve-gated, which is what makes it double as live validation: the catalog id
// lights up and the typo beside it stays plain prose.
func TestResolvingSkillTokenIsAccented(t *testing.T) {
	t.Parallel()

	m := accentTestModel(t, 80, "", "/clean-code please check this /code-adit")
	view := m.inputView()
	opener := accentOpener(t, m.th.skillToken)

	if !strings.Contains(view, m.th.skillToken.Render("/clean-code")) {
		t.Errorf("the catalog token is not accented:\n%q", view)
	}
	if strings.Contains(view, m.th.skillToken.Render("/code-adit")) {
		t.Errorf("the mistyped token was accented — the styling is supposed to be the validation")
	}
	if !strings.Contains(ansi.Strip(view), "/code-adit") {
		t.Errorf("the mistyped token vanished from the box; it must render as plain prose")
	}
	if rows := rowsWithAccent(view, opener); len(rows) != 1 {
		t.Errorf("accent painted on %d rows, want exactly the one holding the token: %v", len(rows), rows)
	}
}

// The @ half resolves against the workspace listing the cache holds: a real file lights, a path
// that is not there stays plain.
func TestResolvingFileTokenIsAccented(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "internal", "loop.go"), "package internal")

	m := accentTestModel(t, 80, dir, "look at @internal/loop.go and @internal/gone.go")
	m.files.suggest(dir, "", maxAutocompleteItems, time.Now()) // the "@" overlay's own warm-up

	view := m.inputView()
	if !strings.Contains(view, m.th.fileToken.Render("@internal/loop.go")) {
		t.Errorf("an existing @path is not accented:\n%q", view)
	}
	if strings.Contains(view, m.th.fileToken.Render("@internal/gone.go")) {
		t.Errorf("a path that is not in the workspace was accented")
	}
}

// Nothing in the render path may touch the disk: View runs every frame. A cold cache therefore
// stays cold across a render — the token renders plain and lights up only once the "@" overlay's
// next lookup has warmed the listing.
func TestAccentRenderNeverWalksTheWorkspace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")

	m := accentTestModel(t, 80, dir, "look at @main.go")
	view := m.inputView()

	if m.files.files != nil {
		t.Fatalf("rendering walked the workspace: the cache now holds %v", m.files.files)
	}
	if strings.Contains(view, m.th.fileToken.Render("@main.go")) {
		t.Errorf("an unwarmed cache accented a token it cannot have resolved")
	}
	// …and once something else has warmed it, the very same draft lights up.
	m.files.suggest(dir, "", maxAutocompleteItems, time.Now())
	if !strings.Contains(m.inputView(), m.th.fileToken.Render("@main.go")) {
		t.Errorf("the accent did not self-heal after the listing was warmed")
	}
}

// A token wider than the box is drawn on two rows, so it is accented on two rows.
func TestAccentedTokenWrapsAcrossRows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "internal", "tui", "model.go"), "package tui")

	m := accentTestModel(t, 20, dir, "@internal/tui/model.go") // inner width 16, token 22 cells
	m.files.suggest(dir, "", maxAutocompleteItems, time.Now())

	view := m.inputView()
	if rows := rowsWithAccent(view, accentOpener(t, m.th.fileToken)); len(rows) != 2 {
		t.Fatalf("a wrapped token lit %d rows, want 2: %v", len(rows), rows)
	}
	for _, part := range []string{"@internal/tui/mo", "del.go"} {
		if !strings.Contains(view, m.th.fileToken.Render(part)) {
			t.Errorf("the %q half of the wrapped token is not accented:\n%q", part, view)
		}
	}
}

// The two overlays compose in one order only: the accent paints first and the drag-selection
// paints over it, so a selected token reads as SELECTED rather than keeping its own colour.
func TestSelectionWinsOverTheAccent(t *testing.T) {
	t.Parallel()

	m := accentTestModel(t, 80, "", "/clean-code please check this")
	m.sel = promptSel{
		active:    true,
		anchorOff: 0, headOff: 11,
		anchorVis: cell{0, 0}, headVis: cell{0, 11},
	}
	view := m.inputView()

	if !strings.Contains(view, m.th.selection.Render("/clean-code")) {
		t.Errorf("the selected token does not carry the selection highlight:\n%q", view)
	}
	if strings.Contains(view, m.th.skillToken.Render("/clean-code")) {
		t.Errorf("the accent survived under a selection covering the whole token")
	}
}

// A draft taller than the box scrolls inside it, and the accent follows the SCROLL: it lands on
// the visible row the token is actually drawn on, not on the row its absolute position names.
func TestAccentFollowsTheScrolledTextarea(t *testing.T) {
	t.Parallel()

	lines := make([]string, 15)
	for i := range lines {
		lines[i] = "line"
	}
	lines[0] = "/clean-code first"
	lines[14] = "and then /review"
	// Pasted rather than assigned: the scroll offset is the widget's own answer, and it only
	// re-clamps along the paths the Update loop drives.
	m := step(t, accentTestModel(t, 80, "", ""), tea.PasteMsg{Content: strings.Join(lines, "\n")})

	if off := m.input.ScrollYOffset(); off == 0 {
		t.Fatalf("the textarea did not scroll; the test needs content taller than %d rows", maxInputRows)
	}
	view := m.inputView()
	rows := rowsWithAccent(view, accentOpener(t, m.th.skillToken))
	if len(rows) != 1 {
		t.Fatalf("accent painted on %d rows, want only the visible token's: %v", len(rows), rows)
	}
	for _, text := range rows {
		if !strings.Contains(text, "/review") {
			t.Errorf("the accent landed on the wrong row: %q", text)
		}
	}
}

// A skill whose id is not in the catalog cannot light up, and a catalog that is not wired at all
// lights nothing — the predicate is the same one submit resolves by.
func TestAccentSpansFollowTheCatalog(t *testing.T) {
	t.Parallel()

	m := accentTestModel(t, 80, "", "/clean-code and /review")
	if got := len(m.resolvingTokens()); got != 2 {
		t.Fatalf("resolvingTokens = %d spans, want 2", got)
	}

	bare := newModel(context.Background(), &fakeEngine{}, testOpts, nil) // testOpts wires no catalog
	bare = step(t, bare, tea.WindowSizeMsg{Width: 80, Height: 24})
	bare.input.SetValue("/clean-code and /review")
	bare.layout()
	if got := bare.resolvingTokens(); got != nil {
		t.Errorf("an empty catalog resolved %v", got)
	}
}

// ----------------------------------------------------------------------------
// Row counting cost (inputaccent.go wrapRowStarts, prompteditor.go rowCountMemo)
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

// A promptEditor built literally — no newPromptEditor, so no row-count memo — still counts its
// draft's rows, every time, and the Model reading it through hiddenDraftRows agrees.
func TestInputContentRowsWithoutAMemo(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.promptEditor.rowCount = nil
	value := strings.Repeat("word ", 60)
	m.input.SetValue(value)
	w := m.inputInnerWidth()
	for range 2 {
		if got, want := m.promptEditor.contentRows(w), inputContentRows(value, w); got != want {
			t.Fatalf("contentRows = %d, want %d", got, want)
		}
	}
	if got, want := m.hiddenDraftRows(), max(0, inputContentRows(value, w)-m.input.Height()); got != want {
		t.Fatalf("hiddenDraftRows = %d, want %d", got, want)
	}
	e := promptEditor{lineEditor: newLineEditor(defaultCursorShape, lipgloss.Color("#000000"), "")}
	e.input.SetValue(value)
	if got, want := e.rows(w), clampInt(inputContentRows(value, w), minInputRows, maxInputRows); got != want {
		t.Fatalf("a literal promptEditor's rows = %d, want %d", got, want)
	}
}

// One keypress and the frame it paints measure the draft's rows once: the box's height, the
// transcript clamp and the hidden-row count on the border all read the one memoised count.
func TestInputContentRowsMeasuredOncePerKeypressAndView(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.input.SetValue(strings.Repeat("a long draft that wraps ", 40))
	m.input.CursorEnd()
	memo := m.promptEditor.rowCount
	memo.misses = 0
	m = step(t, m, keyRune('x'))
	_ = m.View()
	if memo.misses != 1 {
		t.Fatalf("one keypress + View counted the draft's rows %d times; want 1", memo.misses)
	}
	_ = m.View()
	if memo.misses != 1 {
		t.Fatalf("a repaint of an unchanged draft recounted its rows (%d counts)", memo.misses)
	}
}

func BenchmarkInputContentRows(b *testing.B) {
	value := string(longProseLine())
	b.ReportAllocs()
	for b.Loop() {
		inputContentRows(value, 120)
	}
}
