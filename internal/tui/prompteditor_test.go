package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/present"
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
			t.Parallel()

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
	t.Parallel()

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
			t.Parallel()

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
	t.Parallel()

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
// editor is pessimistic and the answer moves the legend both ways. The widget itself carries no
// placeholder: the legend is derived at paint (Model.legend), so there is nothing here to swap in
// place.
func TestPromptEditorIdleLegendFollowsKeyDisambiguation(t *testing.T) {
	t.Parallel()

	e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))

	if got := e.idleLegend(); got != idlePlaceholder {
		t.Errorf("fresh idleLegend() = %q, want the ⌥⏎-only legend %q", got, idlePlaceholder)
	}
	if got := e.input.Placeholder; got != "" {
		t.Errorf("fresh widget placeholder = %q, want none — the legend is derived at paint, never stored", got)
	}
	if strings.Contains(idlePlaceholder, "⇧⏎") {
		t.Errorf("the not-negotiated legend advertises ⇧⏎: %q", idlePlaceholder)
	}

	e.setKeyDisambiguation(true)
	if got := e.idleLegend(); got != idleShiftPlaceholder {
		t.Errorf("negotiated idleLegend() = %q, want %q", got, idleShiftPlaceholder)
	}

	e.setKeyDisambiguation(false)
	if got := e.idleLegend(); got != idlePlaceholder {
		t.Errorf("idleLegend() after a bare answer = %q, want %q back", got, idlePlaceholder)
	}
}

// The running legend is the one place an empty box announces the stop key, and it names the gesture
// the key actually is: one esc arms, and only a second inside escStopWindow stops the run
// (handleKey's `case "esc"`). The LITERAL is pinned here rather than the constant, because a
// placeholder that still promised a one-press stop would be the chrome lying about the keyboard.
func TestRunningPlaceholderAnnouncesTheDoubleEsc(t *testing.T) {
	t.Parallel()

	const want = "queue a message…  ⏎ queue · ↑ recall · esc×2 cancel"

	if runningPlaceholder != want {
		t.Errorf("runningPlaceholder = %q, want the double-tap legend %q", runningPlaceholder, want)
	}

	m := runningModel(t)
	if got := plain(m.View()); !strings.Contains(got, want) {
		t.Errorf("the empty box while running paints no %q:\n%s", want, got)
	}
}

// A paste is an edit that writes the box and returns: the box growing around the pasted rows is
// laid out by Update's tail, not by the paste arm (doc.go, "an arm mutates").
func TestPasteIsSettledByTheTail(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	before := m.input.Height()

	m = step(t, m, tea.PasteMsg{Content: "one\ntwo\nthree\nfour"})

	if m.input.Height() <= before {
		t.Fatalf("precondition: box height %d after a four-row paste, want more than %d", m.input.Height(), before)
	}
	assertSettled(t, m)
}

// visionEngine is a fakeEngine on a server with `vision: true`: the optional visionReporter answer
// the image attach asks for. A bare fakeEngine answers nothing, which reads as no vision.
type visionEngine struct {
	*fakeEngine
}

// Vision reports the bound server as accepting images.
func (visionEngine) Vision() bool { return true }

// VisionRefusal is never asked of a vision server; it answers "" as an engine with no wording would.
func (visionEngine) VisionRefusal() string { return "" }

// noVisionEngine is a fakeEngine on a server without `vision: true` that gives its own refusal
// wording — a sentinel distinct from noVisionNote, so a flash equal to it came from the engine.
type noVisionEngine struct {
	*fakeEngine
}

// noVisionEngineRefusal is noVisionEngine's refusal wording.
const noVisionEngineRefusal = "engine refusal: this server takes no images"

// Vision reports the bound server as refusing images.
func (noVisionEngine) Vision() bool { return false }

// VisionRefusal is the engine's own refusal wording.
func (noVisionEngine) VisionRefusal() string { return noVisionEngineRefusal }

// pngBytes is a PNG signature followed by filler — enough for the sniff, which reads the leading
// bytes only.
var pngBytes = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 2040))

// visionModel builds an idle model on a vision server whose workspace holds shot.png (a PNG) and
// notes.png (text under an image name), returning the model, its engine and the workspace.
func visionModel(t *testing.T) (Model, *fakeEngine, string) {
	t.Helper()
	workspace := t.TempDir()
	writeFixture(t, filepath.Join(workspace, "shot.png"), pngBytes)
	writeFixture(t, filepath.Join(workspace, "notes.png"), []byte("just some text\n"))
	eng := &fakeEngine{stepFn: scriptedSteps()}
	opts := testOpts
	opts.Workspace = workspace
	return newTestModelEng(t, visionEngine{eng}, opts), eng, workspace
}

// writeFixture writes data to path.
func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// A bracketed paste that is exactly an image file's path attaches the file on a vision server —
// absolute, relative to the workspace, or quoted as a drag-and-drop writes it — and types nothing;
// the attach line above the box names it and its size.
func TestPasteImagePathAttachesOnAVisionServer(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"ABS", "shot.png", "'ABS'", "  ABS\n"} {
		t.Run(content, func(t *testing.T) {
			t.Parallel()
			m, _, workspace := visionModel(t)
			content = strings.ReplaceAll(content, "ABS", filepath.Join(workspace, "shot.png"))

			m = step(t, m, tea.PasteMsg{Content: content})

			if got := m.input.Value(); got != "" {
				t.Errorf("box = %q, want nothing typed — the path attached", got)
			}
			if len(m.images) != 1 || m.images[0].Name != "shot.png" || m.images[0].MediaType != "image/png" {
				t.Fatalf("images = %+v, want shot.png as image/png", m.images)
			}
			if !reflect.DeepEqual(m.images[0].Data, pngBytes) {
				t.Errorf("attached %d bytes, want the file's %d", len(m.images[0].Data), len(pngBytes))
			}
			if view := plain(m.View()); !strings.Contains(view, "attached: shot.png (2.0 KiB)") {
				t.Errorf("no attach line above the box:\n%s", view)
			}
			assertSettled(t, m)
		})
	}
}

// The same paste on a server without `vision: true` types the path, as it did before images
// existed; so does a path whose file is not an image whatever its name, and a path to nothing.
func TestPasteNonAttachablePathInsertsText(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	writeFixture(t, filepath.Join(workspace, "shot.png"), pngBytes)
	writeFixture(t, filepath.Join(workspace, "notes.png"), []byte("just some text\n"))
	opts := testOpts
	opts.Workspace = workspace

	for _, tc := range []struct {
		name    string
		eng     Engine
		content string
	}{
		{"non-vision server", &fakeEngine{}, "shot.png"},
		{"text named .png", visionEngine{&fakeEngine{}}, "notes.png"},
		{"missing file", visionEngine{&fakeEngine{}}, "gone.png"},
		{"two paths", visionEngine{&fakeEngine{}}, "shot.png shot.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestModelEng(t, tc.eng, opts)

			m = step(t, m, tea.PasteMsg{Content: tc.content})

			if got := m.input.Value(); got != tc.content {
				t.Errorf("box = %q, want the pasted text %q", got, tc.content)
			}
			if len(m.images) != 0 {
				t.Errorf("images = %+v, want none attached", m.images)
			}
		})
	}
}

// A pasted image path whose file breaks the one-image cap is refused in the status line with the
// engine's own wording, and neither attaches nor types anything.
func TestPasteImagePathOverTheCapIsRefused(t *testing.T) {
	t.Parallel()
	m, _, workspace := visionModel(t)
	big := append(append([]byte{}, pngBytes...), make([]byte, domain.MaxImageBytes)...)
	writeFixture(t, filepath.Join(workspace, "big.png"), big)

	m = step(t, m, tea.PasteMsg{Content: "big.png"})

	if len(m.images) != 0 || m.input.Value() != "" {
		t.Errorf("images = %d, box = %q; want neither", len(m.images), m.input.Value())
	}
	if !strings.Contains(m.flash, `image "big.png" is`) || !strings.Contains(m.flash, "cap on one image") {
		t.Errorf("flash = %q, want the one-image cap refusal", m.flash)
	}
}

// ctrl+v asks the clipboard for an image first: an image is attached as clipboard-<n>.png, counted
// per session, and nothing is typed.
func TestPasteCtrlVAttachesAClipboardImage(t *testing.T) {
	m, _, _ := visionModel(t)
	stubClipboardImage(t, pngBytes, nil)

	for _, want := range []string{"clipboard-1.png", "clipboard-2.png"} {
		next, cmd := stepCmd(t, m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatal("ctrl+v returned no Cmd, want the clipboard probe")
		}
		m = step(t, next, cmd())
		if got := m.images[len(m.images)-1].Name; got != want {
			t.Errorf("attached as %q, want %q", got, want)
		}
	}
	if len(m.images) != 2 || m.input.Value() != "" {
		t.Errorf("images = %d, box = %q; want two images and nothing typed", len(m.images), m.input.Value())
	}
}

// With no image on the clipboard — or bytes that are not one — ctrl+v hands back to the
// textarea's own text paste, exactly what the key did before images existed.
func TestPasteCtrlVFallsBackToText(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		err  error
	}{
		{"no image", nil, present.ErrNoClipboardImage},
		{"not an image", []byte("plain text"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _ := visionModel(t)
			stubClipboardImage(t, tc.data, tc.err)

			next, cmd := stepCmd(t, m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
			m, cmd = stepCmd(t, next, cmd())

			if reflect.ValueOf(cmd).Pointer() != reflect.ValueOf(textarea.Paste).Pointer() {
				t.Errorf("fallback Cmd is not textarea.Paste")
			}
			if len(m.images) != 0 {
				t.Errorf("images = %+v, want none", m.images)
			}
		})
	}
}

// A clipboard image on a server without `vision: true` is not attached: the status line says why,
// since the engine would refuse the message it rode — in the engine's own words when it has them,
// and noVisionNote when it cannot say (a bare fakeEngine, as an unbound session's holder answers).
func TestPasteCtrlVImageOnANonVisionServerIsRefused(t *testing.T) {
	tests := []struct {
		name string
		eng  Engine
		want string
	}{
		{"engine wording", noVisionEngine{&fakeEngine{}}, noVisionEngineRefusal},
		{"no engine wording", &fakeEngine{}, noVisionNote},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModelEng(t, tc.eng, testOpts)

			m = step(t, m, clipboardImageMsg{data: pngBytes})

			if len(m.images) != 0 {
				t.Errorf("images = %+v, want none on a non-vision server", m.images)
			}
			if m.flash != tc.want {
				t.Errorf("flash = %q, want %q", m.flash, tc.want)
			}
		})
	}
}

// stubClipboardImage substitutes the clipboard-image seam for the test's length. Tests that use it
// are not parallel: the seam is package state.
func stubClipboardImage(t *testing.T, data []byte, err error) {
	t.Helper()
	saved := readClipboardImage
	readClipboardImage = func(string) ([]byte, error) { return data, err }
	t.Cleanup(func() { readClipboardImage = saved })
}

// Backspace on an empty box keeps its order: the newest queued command, then the newest staged
// message, and only with nothing queued the newest pending image.
func TestAttachedImageDropsAfterTheQueuedRows(t *testing.T) {
	t.Parallel()
	m, _ := queuedRun(t, "a message", "/clear")
	for _, name := range []string{"one.png", "two.png"} {
		if err := m.attachImage(domain.Image{Name: name, MediaType: "image/png", Data: pngBytes}); err != nil {
			t.Fatalf("attach %s: %v", name, err)
		}
	}
	backspace := tea.KeyPressMsg{Code: tea.KeyBackspace}

	m = step(t, m, backspace)
	if got := m.input.Value(); got != "/clear" || len(m.images) != 2 {
		t.Fatalf("first ⌫: box = %q, images = %d; want the queued command back and both images", got, len(m.images))
	}
	m.input.Reset()
	m = step(t, m, backspace)
	if got := m.input.Value(); got != "a message" || len(m.images) != 2 {
		t.Fatalf("second ⌫: box = %q, images = %d; want the staged message back and both images", got, len(m.images))
	}
	m.input.Reset()
	m = step(t, m, backspace)
	if len(m.images) != 1 || m.images[0].Name != "one.png" {
		t.Fatalf("third ⌫: images = %+v; want the newest dropped", m.images)
	}
}

// Pending images stay pending through an interjection: the staged row is text-only, and the
// attach line still stands for the next idle send.
func TestAttachedImagesStayPendingThroughAnInterjection(t *testing.T) {
	t.Parallel()
	m, _ := queuedRun(t)
	if err := m.attachImage(domain.Image{Name: "one.png", MediaType: "image/png", Data: pngBytes}); err != nil {
		t.Fatal(err)
	}

	m = stageRow(t, m, "look at this")

	if n := len(m.pendingInterjections); n != 1 || len(m.pendingInterjections[0].input.Images) != 0 {
		t.Fatalf("staged rows = %+v; want one text-only row", m.pendingInterjections)
	}
	if len(m.images) != 1 {
		t.Errorf("images = %d after the interjection, want the image still pending", len(m.images))
	}
}

// An image-only submit sends — the empty-send gate counts a pending image — and the send carries
// the images, beside any text, and clears the attach line; the sent block names them on its own
// "attached:" row, under the ❯ marker itself when the send had no text.
func TestAttachedImagesRideTheSubmitAndClearTheLine(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"", "what is this?"} {
		t.Run("text="+text, func(t *testing.T) {
			t.Parallel()
			m, eng, _ := visionModel(t)
			m = step(t, m, tea.PasteMsg{Content: "shot.png"})
			m.input.SetValue(text)

			m, cmd := stepCmd(t, m, keyEnter())

			if m.state != stateRunning {
				t.Fatalf("state = %v, want running — the message was sent", m.state)
			}
			if len(m.images) != 0 || m.pendingImageRow() != "" {
				t.Errorf("images = %d after the send, want the attach line gone", len(m.images))
			}
			wantRow := "attached: shot.png (2.0 KiB)"
			if text == "" {
				wantRow = glyphUser + " " + wantRow
			}
			if view := plain(m.View()); !strings.Contains(view, wantRow) || !strings.Contains(view, text) {
				t.Errorf("view after the send lacks the sent block's %q row beside %q:\n%s", wantRow, text, view)
			}
			drainCmd(t, m, cmd)
			if len(eng.submitted) != 1 {
				t.Fatalf("submitted = %d inputs, want 1", len(eng.submitted))
			}
			in := eng.submitted[0]
			if in.Text != text || len(in.Images) != 1 || in.Images[0].Name != "shot.png" {
				t.Errorf("submitted = %q with %d images, want %q with shot.png", in.Text, len(in.Images), text)
			}
		})
	}
}

// The attach refuses an image past the per-message cap beside what is already pending, and leaves
// the pending set as it was.
func TestAttachImageEnforcesTheMessageCap(t *testing.T) {
	t.Parallel()
	var e promptEditor
	half := make([]byte, domain.MaxMessageImageBytes/2+1)
	if err := e.attachImage(domain.Image{Name: "a.png", Data: half}); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	err := e.attachImage(domain.Image{Name: "b.png", Data: half})
	if err == nil || !strings.Contains(err.Error(), "cap on one message") {
		t.Errorf("second attach = %v, want the one-message cap refusal", err)
	}
	if len(e.images) != 1 {
		t.Errorf("images = %d, want the first kept alone", len(e.images))
	}
}

// withPendingImage attaches one image to m and lays the frame out again at its window, the way the
// Update tail does after a real attach, so the box's rows count the attach line.
func withPendingImage(t *testing.T, m Model) Model {
	t.Helper()
	if err := m.attachImage(domain.Image{Name: "shot.png", MediaType: "image/png", Data: pngBytes}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	return step(t, m, tea.WindowSizeMsg{Width: m.width, Height: m.height})
}

// The attach line is not part of the frame's floor, so a pending image never makes the composed
// frame taller than the terminal: at the eight-row floor the box keeps its one content row and the
// line gives way, and from the first row the floor can spare it is drawn again.
func TestAttachLineKeepsTheFrameInsideTheTerminal(t *testing.T) {
	t.Parallel()
	for _, height := range []int{frameFloorRows, frameFloorRows + 1, 10, smallestOverlayWindow, 24} {
		t.Run(fmt.Sprintf("%d rows", height), func(t *testing.T) {
			t.Parallel()
			m := withPendingImage(t, modelWithOverlayRoomAt(t, 80, height, testOpts))

			plainFrame := plain(m.View())
			if rows := len(strings.Split(m.View().Content, "\n")); rows != height {
				t.Fatalf("composed frame is %d rows on a %d-row terminal, want exactly %d:\n%s",
					rows, height, height, plainFrame)
			}
			if got := m.input.Height(); got < minInputRows {
				t.Errorf("input box is %d rows, want at least %d", got, minInputRows)
			}
			drawn := strings.Contains(plainFrame, "attached: shot.png")
			if want := height > frameFloorRows; drawn != want {
				t.Errorf("attach line drawn = %v at %d rows, want %v:\n%s", drawn, height, want, plainFrame)
			}
			if len(m.images) != 1 {
				t.Errorf("images = %d, want the image still pending whether or not its line is drawn", len(m.images))
			}
		})
	}
}

// A pending image never pushes a decision surface off the frame: at the shortest window a pane is
// drawn in, the attach line gives way to the approval prompt's four rows the way the draft's extra
// rows do (draftRowsCeiling), and comes back on the first row past them.
func TestAttachLineGivesWayToTheApprovalPrompt(t *testing.T) {
	t.Parallel()
	for _, height := range []int{smallestOverlayWindow, smallestOverlayWindow + 1, 24} {
		t.Run(fmt.Sprintf("%d rows", height), func(t *testing.T) {
			t.Parallel()
			m := withPendingImage(t, modelWithOverlayRoomAt(t, 80, height, Options{Workspace: "/ws/a"}))
			startStubWorker(t, &m)
			m = step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{
				Tool:      "write_file",
				Arguments: []byte(`{"path":"/ws/a/main.go","content":"package main"}`),
				CacheKey:  ordinaryGateKey,
			}})

			plainFrame := plain(m.View())
			if rows := len(strings.Split(m.View().Content, "\n")); rows != height {
				t.Fatalf("composed frame is %d rows on a %d-row terminal, want exactly %d:\n%s",
					rows, height, height, plainFrame)
			}
			if m.frameOverlays().block(panePrompt) == "" {
				t.Fatalf("the frame seated no prompt pane while its keys are live:\n%s", plainFrame)
			}
			drawn := strings.Contains(plainFrame, "attached: shot.png")
			if want := height > smallestOverlayWindow; drawn != want {
				t.Errorf("attach line drawn = %v at %d rows, want %v:\n%s", drawn, height, want, plainFrame)
			}
		})
	}
}
