package tui

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
func TestWrapRowStartsMirrorsTheWidget(t *testing.T) {
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
		{"the acceptance draft", "/grill-me check @internal/tui/model.go and /code-adit", 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ta := textarea.New()
			ta.Prompt = ""
			ta.ShowLineNumbers = false
			ta.CharLimit = 0
			ta.SetWidth(c.width)
			ta.SetHeight(10)
			ta.SetValue(c.line)

			runes := []rune(c.line)
			got := wrapRowStarts(runes, ta.Width())

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
