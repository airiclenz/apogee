package tui

import (
	"reflect"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// promptEditor — editor-direct unit tests (review candidate #3)
// ----------------------------------------------------------------------------
//
// These exercise the promptEditor in isolation — no Model, no fake engine, no full Update loop —
// which is the payoff of lifting the input cluster into its own type: the self-contained input
// logic is now testable without standing up the whole widget graph. The same behaviour is also
// covered end-to-end through the Model in minilang_test.go / skill_test.go / mouse_test.go, which
// keep passing unmodified (the refactor's safety net); these add the direct, loop-free path.

// TestCursorShapeNamesAllDraw is the drift pin on the split the caret vocabulary lives across:
// internal/domain holds the NAMES a config value may spell, this package holds what each one is
// DRAWN as, and nothing in the compiler joins the two. Without this, a fourth name added to the
// domain list would parse successfully here and hand the renderer the zero CursorShape — a
// silently wrong caret rather than a build failure. The default is checked through the same door,
// because it is resolved by map lookup rather than by the parse.
func TestCursorShapeNamesAllDraw(t *testing.T) {
	t.Parallel()

	for _, name := range domain.CursorShapeNames() {
		shape, err := ParseCursorShape(name)
		if err != nil {
			t.Errorf("ParseCursorShape(%q) errored on a name the domain vocabulary offers: %v", name, err)
			continue
		}
		if _, ok := cursorShapes[name]; !ok {
			t.Errorf("the domain offers cursor shape %q but this package draws no shape for it "+
				"(it parsed to %v, the zero shape's meaning)", name, shape)
		}
	}

	if _, ok := cursorShapes[domain.DefaultCursorShapeName]; !ok {
		t.Errorf("the default cursor shape %q has no renderer constant, so defaultCursorShape is "+
			"the zero shape rather than a chosen one", domain.DefaultCursorShapeName)
	}
}

// TestParseCursorShapeRefusesAnUnknownName pins the other half of the seam: a name outside the
// domain vocabulary is an error whose text names the shapes this build draws, because that error
// is the only thing the config layer can show a human who mistyped the key.
func TestParseCursorShapeRefusesAnUnknownName(t *testing.T) {
	t.Parallel()

	_, err := ParseCursorShape("beam")
	if err == nil {
		t.Fatal(`ParseCursorShape("beam") accepted a shape this build cannot draw`)
	}
	for _, name := range domain.CursorShapeNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name the known shape %q", err, name)
		}
	}
}

// submitParse classifies a free-text line as a message and extracts its @file references.
func TestPromptEditorSubmitParseMessage(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetValue("look at @main.go and @pkg/x.go please")
	parsed := e.submitParse(nil)
	if parsed.kind != kindMessage {
		t.Fatalf("kind = %v, want kindMessage", parsed.kind)
	}
	if want := "look at @main.go and @pkg/x.go please"; parsed.text != want {
		t.Errorf("text = %q, want %q (the @tokens stay in place)", parsed.text, want)
	}
	if want := []string{"main.go", "pkg/x.go"}; !reflect.DeepEqual(parsed.fileRefs, want) {
		t.Errorf("fileRefs = %v, want %v", parsed.fileRefs, want)
	}
	if len(parsed.skillIDs) != 0 {
		t.Errorf("skillIDs = %v, want none", parsed.skillIDs)
	}
}

// submitParse recognises a leading /command and reports the bare verb.
func TestPromptEditorSubmitParseCommand(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetValue("/clear")
	parsed := e.submitParse(nil)
	if parsed.kind != kindCommand || parsed.command != "clear" {
		t.Fatalf("parsed = %+v, want kindCommand verb=clear", parsed)
	}
}

// submitParse resolves the inline /tokens through the predicate it is handed, so a message that
// names a skill arrives with the id extracted and the token still in its text.
func TestPromptEditorSubmitParseExtractsSkillTokens(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetValue("/go-testing tidy this up")
	parsed := e.submitParse(knownSkills("go-testing", "git"))
	if want := "/go-testing tidy this up"; parsed.text != want {
		t.Errorf("text = %q, want %q (the /token stays in the message)", parsed.text, want)
	}
	if want := []string{"go-testing"}; !reflect.DeepEqual(parsed.skillIDs, want) {
		t.Errorf("skillIDs = %v, want %v", parsed.skillIDs, want)
	}
}

// reset empties every editable part of the editor: the textarea, the overlay and the skillRegion
// edge-trigger that says a "/" menu region is open. Emptying the text is what drops the skills too
// — they live in it as /tokens, not beside it.
func TestPromptEditorResetClearsEverything(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetValue("half-typed /go")
	e.autocomplete = autocompleteState{active: true, kind: acCommand}
	e.skillRegion = true
	e.reset()
	if v := e.input.Value(); v != "" {
		t.Errorf("input = %q, want empty after reset", v)
	}
	if e.autocomplete.active {
		t.Error("autocomplete still active after reset")
	}
	if e.skillRegion {
		t.Error("skillRegion still set after reset; an emptied box sits in no menu region")
	}
	if got := e.submitParse(knownSkills("go")); len(got.skillIDs) != 0 {
		t.Errorf("skillIDs = %v, want none once the text is gone", got.skillIDs)
	}
}

// wrappedRowsOf reports how many wrapped sub-rows the WIDGET gives one logical line at width,
// read back through its own LineInfo with the caret parked on that line. It is deliberately not
// inputContentRows: that one is apogee's MIRROR of this answer (pinned to it by
// TestInputContentRowsMirrorsTheWidget), and a caret test that leaned on the mirror would go green
// on a geometry the widget never drew — including the phantom trailing sub-line bubbles appends to
// a line that fills its last row exactly, which is the very geometry the caret seat has to survive.
func wrappedRowsOf(line string, width int) int {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
	e.input.SetWidth(width)
	e.input.SetValue(line)
	e.input.MoveToBegin()
	return e.input.LineInfo().Height
}

// caretToOffset must reach a logical row that sits below a SOFT-WRAPPED one, and its walk must
// terminate whatever the geometry. bubbles' CursorDown steps one VISUAL row, so a wrapped line
// takes several steps to cross — and on a line that fills its last row exactly and ends with a
// space it takes INFINITELY many: the wrap appends a phantom trailing sub-line, and CursorDown's
// column clamp (len(line)-1) can never enter it, so the caret does not move at all. A seat built on
// bare CursorDowns therefore stalls on the first line forever and a completion spliced into the
// second seats its caret in the middle of the first. Every rune position of each draft round-trips.
func TestPromptEditorCaretToOffsetCrossesWrappedRows(t *testing.T) {
	cases := []struct {
		name  string
		width int
		value string
	}{
		// The pathological geometries: a first line that ends with a space exactly at a row
		// boundary, at the app's real text width (76) and the two others the walk failed at.
		{"phantom row at app width", 76, strings.Repeat("aaa ", 19) + "\nplease /rev"},
		{"phantom row at width 8", 8, strings.Repeat("wrapped ", 20) + "\nplease /rev"},
		{"phantom row at width 80", 80, strings.Repeat("wrapped ", 20) + "\nplease /rev"},
		// Three logical lines, the middle one phantom-wrapped: the walk crosses two of them.
		{"phantom row in the middle", 76, "head\n" + strings.Repeat("aaa ", 19) + "\ntail past it"},
		// An ordinarily wrapped line, and one with wide runes, still round-trip.
		{"plain wrap", 20, strings.Repeat("wrapped ", 12) + "\nsecond line, well past the wrap"},
		{"wide runes", 20, "日本語のテキストです ここに " + strings.Repeat("あ", 30) + "\nsecond 🎉 line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := false
			for _, line := range strings.Split(tc.value, "\n") {
				if wrappedRowsOf(line, tc.width) >= 2 {
					wrapped = true
				}
			}
			if !wrapped {
				t.Fatal("no logical line wraps at this width; the walk is never asked to cross one")
			}
			e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
			e.input.SetWidth(tc.width)
			e.input.SetHeight(3) // shorter than the draft, so the scroll re-clamp runs for real
			e.input.SetValue(tc.value)
			e.input.MoveToEnd()

			// Rune starts only: a byte offset inside a multi-byte rune is not a caret position.
			offs := []int{len(tc.value)}
			for i := range tc.value {
				offs = append(offs, i)
			}
			for _, off := range offs {
				e.caretToOffset(off)
				if got := e.caretByteOffset(); got != off {
					t.Fatalf("caretToOffset(%d) seated the caret at %d (row %d, col %d)",
						off, got, e.input.Line(), e.input.Column())
				}
				// The auto-grow re-clamp runs on the same seat and must not move the caret either.
				e.reseatInput()
				if got := e.caretByteOffset(); got != off {
					t.Fatalf("reseatInput moved the caret from %d to %d", off, got)
				}
			}

			// An offset past the end lands at the end rather than running away.
			e.caretToOffset(len(tc.value) + 99)
			if got := e.caretByteOffset(); got != len(tc.value) {
				t.Errorf("caretToOffset(past the end) = %d, want %d", got, len(tc.value))
			}
		})
	}
}

// reseatCaret must reach EVERY visual row a draft wraps to, the phantom trailing sub-line
// included. bubbles' CursorDown steps one visual row and its column guess clamps at len(line)-1,
// which is one short of where that sub-line begins, so a walk of bare CursorDowns stalls on a
// logical line that carries one: every row below it then seats a row short, on the wrong logical
// line entirely. The Height-aware walk crosses whole logical lines instead, and reads its counts
// off the widget.
//
// wrapRowStarts is the independent mirror the assertion is written against — it decomposes a
// logical line into visual rows the way the widget does (pinned to it by
// TestInputContentRowsMirrorsTheWidget) — so the walk is checked against a second derivation of
// the geometry rather than against itself. Every row of the draft is asked for in turn, at column
// 0, and must land on that row's logical line at that row's first rune; the phantom row's first
// rune is the line's end, which is exactly where a click on it belongs.
func TestPromptEditorReseatCaretReachesEveryVisualRow(t *testing.T) {
	cases := []struct {
		name  string
		width int
		value string
	}{
		// A first line that ends with a space exactly at a row boundary — the phantom geometry —
		// at the app's real text width and at a narrow one.
		{"phantom row at app width", 76, strings.Repeat("aaa ", 19) + "\nplease /rev"},
		{"phantom row at width 8", 8, strings.Repeat("wrapped ", 20) + "\nplease /rev"},
		// Three logical lines, the middle one phantom-wrapped: the walk crosses two of them.
		{"phantom row in the middle", 76, "head\n" + strings.Repeat("aaa ", 19) + "\ntail past it"},
		// An ordinarily wrapped draft, and one of wide runes, still land row for row.
		{"plain wrap", 20, strings.Repeat("wrapped ", 12) + "\nsecond line, well past the wrap"},
		{"wide runes", 20, "日本語のテキストです ここに " + strings.Repeat("あ", 30) + "\nsecond 🎉 line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
			e.input.SetWidth(tc.width)
			e.input.SetHeight(3) // shorter than the draft, so the scroll re-clamp runs for real
			e.input.SetValue(tc.value)

			type seat struct{ line, col int }
			var want []seat
			lines := strings.Split(tc.value, "\n")
			for i, line := range lines {
				for _, start := range wrapRowStarts([]rune(line), e.input.Width()) {
					want = append(want, seat{i, start})
				}
			}
			if len(want) <= len(lines) {
				t.Fatal("no logical line wraps at this width; the walk is never asked to cross one")
			}

			for visRow, w := range want {
				e.caretTo(visRow, 0)
				if e.input.Line() != w.line || e.input.Column() != w.col {
					t.Fatalf("caretTo(visual row %d) seated the caret at line %d col %d, want line %d col %d",
						visRow, e.input.Line(), e.input.Column(), w.line, w.col)
				}
				// The auto-grow re-clamp runs on the same seat and must not move the caret either.
				e.reseatInput()
				if e.input.Line() != w.line || e.input.Column() != w.col {
					t.Fatalf("reseatInput moved the caret off visual row %d to line %d col %d",
						visRow, e.input.Line(), e.input.Column())
				}
			}

			// A row below the last one clamps into the value rather than running away.
			e.caretTo(len(want)+9, 0)
			if got, last := e.input.Line(), len(lines)-1; got != last {
				t.Errorf("caretTo(past the last row) seated the caret on line %d, want %d", got, last)
			}
		})
	}
}

// rows grows one row per logical line and clamps at maxInputRows.
func TestPromptEditorRowsGrowsAndClamps(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))

	e.input.SetValue("hello")
	if got := e.rows(40); got != minInputRows {
		t.Errorf("rows(one short line) = %d, want %d", got, minInputRows)
	}

	e.input.SetValue("a\nb\nc")
	if got := e.rows(40); got != 3 {
		t.Errorf("rows(three lines) = %d, want 3", got)
	}

	e.input.SetValue(strings.Repeat("line\n", maxInputRows*3))
	if got := e.rows(40); got != maxInputRows {
		t.Errorf("rows(overflow) = %d, want the %d cap", got, maxInputRows)
	}
}

// The idle legend names ⇧⏎ only once the terminal has negotiated key disambiguation. A fresh
// editor is pessimistic, the answer swaps the legend in place both ways, and a box showing the
// running invitation — which names no newline chord — is left as it is.
func TestPromptEditorIdleLegendFollowsKeyDisambiguation(t *testing.T) {
	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))

	if got := e.idleLegend(); got != idlePlaceholder {
		t.Errorf("fresh idleLegend() = %q, want the ⌥⏎-only legend %q", got, idlePlaceholder)
	}
	if got := e.input.Placeholder; got != idlePlaceholder {
		t.Errorf("fresh placeholder = %q, want %q", got, idlePlaceholder)
	}
	if strings.Contains(idlePlaceholder, "⇧⏎") {
		t.Errorf("the not-negotiated legend advertises ⇧⏎: %q", idlePlaceholder)
	}

	e.setKeyDisambiguation(true)
	if got := e.idleLegend(); got != idleShiftPlaceholder {
		t.Errorf("negotiated idleLegend() = %q, want %q", got, idleShiftPlaceholder)
	}
	if got := e.input.Placeholder; got != idleShiftPlaceholder {
		t.Errorf("negotiated placeholder = %q, want the legend swapped in place to %q", got, idleShiftPlaceholder)
	}

	e.setKeyDisambiguation(false)
	if got := e.input.Placeholder; got != idlePlaceholder {
		t.Errorf("placeholder after a bare answer = %q, want %q back", got, idlePlaceholder)
	}

	e.setPlaceholder(runningPlaceholder)
	e.setKeyDisambiguation(true)
	if got := e.input.Placeholder; got != runningPlaceholder {
		t.Errorf("running placeholder = %q, want it untouched by the keyboard answer", got)
	}
	if got := e.idleLegend(); got != idleShiftPlaceholder {
		t.Errorf("idleLegend() after the answer = %q, want %q for the next idle transition", got, idleShiftPlaceholder)
	}
}

// The running legend is the one place an empty box announces the stop key, and it names the gesture
// the key actually is: one esc arms, and only a second inside escStopWindow stops the run
// (handleKey's `case "esc"`). The LITERAL is pinned here rather than the constant, because a
// placeholder that still promised a one-press stop would be the chrome lying about the keyboard.
func TestRunningPlaceholderAnnouncesTheDoubleEsc(t *testing.T) {
	const want = "queue a message…  ⏎ queue · ↑ recall · esc×2 stop"

	if runningPlaceholder != want {
		t.Errorf("runningPlaceholder = %q, want the double-tap legend %q", runningPlaceholder, want)
	}

	m := runningModel(t)
	if got := plain(m.View()); !strings.Contains(got, want) {
		t.Errorf("the empty box while running paints no %q:\n%s", want, got)
	}
}
