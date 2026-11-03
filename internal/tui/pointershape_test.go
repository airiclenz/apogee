package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// unmarkedLine returns the index of the first on-screen rendered line no block marked as a click
// surface that still holds text, failing when the paint carries none — the fixture no longer has a
// plain, drag-selectable line to aim at.
func unmarkedLine(t *testing.T, m Model) int {
	t.Helper()
	for i := m.viewport.YOffset(); i < len(m.lines) && i < len(m.lineTargets); i++ {
		if m.lineTargets[i].kind == targetNone && strings.TrimSpace(ansi.Strip(m.lines[i])) != "" {
			return i
		}
	}
	t.Fatal("no rendered line is unmarked and non-blank")
	return -1
}

// rowBelowTheTranscript returns a viewport row past the last rendered line that is still one of
// the transcript's own rows, failing when the content fills the transcript to its foot.
func rowBelowTheTranscript(t *testing.T, m Model) int {
	t.Helper()
	row := len(m.lines) - m.viewport.YOffset()
	if row < 0 || row >= m.transcriptRows() {
		t.Fatalf("row %d is not a transcript row past the content (%d rows)", row, m.transcriptRows())
	}
	return row
}

// TestPointerShapeAtAnswersEachRegionOutsideThePanes is the classifier's per-region contract: the
// shape for a point is the one a click there implies, decided by the same hit predicate the click
// handler uses, state gate included.
func TestPointerShapeAtAnswersEachRegionOutsideThePanes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		model func(*testing.T) Model
		point func(*testing.T, Model) (x, y int)
		want  pointerShape
	}{
		{
			name:  "the prompt's text area while editable",
			model: newTestModel,
			point: func(_ *testing.T, m Model) (int, int) {
				x0, y0, _, _ := m.inputContentRect()
				return x0, y0
			},
			want: shapeText,
		},
		{
			name: "the prompt's text area while not editable",
			model: func(t *testing.T) Model {
				m := newTestModel(t)
				m.input.Blur()
				return m
			},
			point: func(_ *testing.T, m Model) (int, int) {
				x0, y0, _, _ := m.inputContentRect()
				return x0, y0
			},
			want: shapeDefault,
		},
		{
			name:  "the footer's mode marker",
			model: newTestModel,
			point: footerMarkerMiddle,
			want:  shapePointer,
		},
		{
			name: "the footer's mode marker while a call awaits approval",
			model: func(t *testing.T) Model {
				m := newTestModel(t)
				m.state = stateAwaitingApproval
				return m
			},
			point: footerMarkerMiddle,
			want:  shapeDefault,
		},
		{
			name: "the footer's mode marker under the /settings pane",
			model: func(t *testing.T) Model {
				m := newTestModel(t)
				m.settings.open = true
				m.layout()
				return m
			},
			point: footerMarkerMiddle,
			want:  shapeDefault,
		},
		{
			name: "the footer's mode marker under the /sessions browser",
			model: func(t *testing.T) Model {
				m := newTestModel(t)
				m.sessionBrowser = sessionBrowser{open: true}
				m.layout()
				return m
			},
			point: footerMarkerMiddle,
			want:  shapeDefault,
		},
		{
			name:  "a tool block's header line",
			model: func(t *testing.T) Model { return modelWithToolBlock(t, "one\ntwo\nthree\nfour") },
			point: func(t *testing.T, m Model) (int, int) {
				return 2, screenRow(t, m, markedLine(t, m, targetHeader))
			},
			want: shapePointer,
		},
		{
			name:  "a plain transcript line",
			model: func(t *testing.T) Model { return modelWithTranscript(t, "hello") },
			point: func(t *testing.T, m Model) (int, int) {
				return 2, screenRow(t, m, unmarkedLine(t, m))
			},
			want: shapeText,
		},
		{
			name:  "a blank row below the transcript",
			model: func(t *testing.T) Model { return modelWithTranscript(t, "hello") },
			point: func(t *testing.T, m Model) (int, int) {
				return 2, rowBelowTheTranscript(t, m)
			},
			want: shapeDefault,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.model(t)
			x, y := tc.point(t, m)

			got := m.pointerShapeAt(m.withFrameSpans(), x, y)

			if got != tc.want {
				t.Fatalf("pointerShapeAt(%d, %d) = %s, want %s", x, y, got.name(), tc.want.name())
			}
		})
	}
}

// TestPointerShapeAtNamesTheCSSShapes pins each shape to the CSS cursor name OSC 22 carries.
func TestPointerShapeAtNamesTheCSSShapes(t *testing.T) {
	t.Parallel()

	for shape, want := range map[pointerShape]string{
		shapeDefault: "default",
		shapeText:    "text",
		shapePointer: "pointer",
	} {
		if got := shape.name(); got != want {
			t.Errorf("pointerShape(%d).name() = %q, want %q", shape, got, want)
		}
	}
}

// footerMarkerMiddle aims at the middle cell of the footer's mode marker, through footerMarkerCells
// (mouse_test.go) so the point is the painter's own.
func footerMarkerMiddle(t *testing.T, m Model) (int, int) {
	t.Helper()
	y, _, middle, _ := footerMarkerCells(t, m)
	return middle, y
}

// paneBox is the screen rows the named pane's box spans on the composed frame, failing when the
// fixture did not seat it.
func paneBox(t *testing.T, m Model, pane framePane) (y0, h int) {
	t.Helper()
	y0, h, ok := m.frameSpans().pane(pane)
	if !ok {
		t.Fatalf("pane %s is not on the frame", paneSpecs[pane].name)
	}
	return y0, h
}

// firstPaneRow aims at the first screen row the painter seats one of the pane's rows on, read off its
// placement through popupPaneHit — geometry alone, no state gate — so a case can ask what the gate
// makes of a row the pane does draw.
func firstPaneRow(t *testing.T, m Model, pane framePane) (int, int) {
	t.Helper()
	render := map[framePane]func() (string, popupPlacement){
		paneBrowser:   m.renderSessionBrowserPlaced,
		panePicker:    m.renderPickerPlaced,
		paneWorkflows: m.renderWorkflowsPlaced,
		paneDropdown:  m.renderAutocompletePlaced,
		panePrompt: func() (string, popupPlacement) {
			if m.pending != nil {
				return m.approvalPromptPlaced(m.pending.Request)
			}
			return m.askPromptPlaced(m.pendingAsk.Request)
		},
	}[pane]
	y0, h := paneBox(t, m, pane)
	for y := y0; y < y0+h; y++ {
		if _, _, _, on := popupPaneHit(m, pane, render, y); on {
			return m.width / 2, y
		}
	}
	t.Fatalf("pane %s seats no row", paneSpecs[pane].name)
	return 0, 0
}

// paneTopBorder aims at the named pane's top border.
func paneTopBorder(pane framePane) func(*testing.T, Model) (int, int) {
	return func(t *testing.T, m Model) (int, int) {
		t.Helper()
		y0, _ := paneBox(t, m, pane)
		return m.width / 2, y0
	}
}

// paneMiddle aims at the middle row of the named pane's box.
func paneMiddle(pane framePane) func(*testing.T, Model) (int, int) {
	return func(t *testing.T, m Model) (int, int) {
		t.Helper()
		y0, h := paneBox(t, m, pane)
		return m.width / 2, y0 + h/2
	}
}

// onFrame aims at the first cell of the frame painting want (frameCell, mouse_test.go).
func onFrame(want string) func(*testing.T, Model) (int, int) {
	return func(t *testing.T, m Model) (int, int) {
		t.Helper()
		return frameCell(t, m, want)
	}
}

// onSettingsRow aims at want as onFrame does, failing unless the cell is on one of the /settings pane's
// own rows — the pane answers for it — so an arrow there is the pane's and not the chain's below it.
func onSettingsRow(want string) func(*testing.T, Model) (int, int) {
	return func(t *testing.T, m Model) (int, int) {
		t.Helper()
		x, y := frameCell(t, m, want)
		if _, ok := settingsHover(m.withFrameSpans(), x, y); !ok {
			t.Fatalf("%q is painted at (%d, %d), which is not one of the pane's rows", want, x, y)
		}
		return x, y
	}
}

// aTranscriptLine aims at the first line of the transcript behind the panes, failing when the row
// names no content.
func aTranscriptLine(t *testing.T, m Model) (int, int) {
	t.Helper()
	if _, _, ok := m.pointTranscriptRow(2, 0); !ok {
		t.Fatal("the frame's first row is not a transcript line")
	}
	return 2, 0
}

// theInputRow aims at the prompt's first text row.
func theInputRow(_ *testing.T, m Model) (int, int) {
	x0, y0, _, _ := m.inputContentRect()
	return x0, y0
}

// fourReportsModel is every report up at once, on a window tall enough to seat them all.
func fourReportsModel(t *testing.T) Model {
	t.Helper()
	m := bothPanesModel(t, 4)
	m.thinkingPane = reportPane{open: true}
	m.advicePane = reportPane{open: true}
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 60})
}

// settingsResetModel is the /settings pane over one resettable key, and whether its reset is armed.
func settingsResetModel(armed bool) func(*testing.T) Model {
	return func(t *testing.T) Model {
		t.Helper()
		rows := []SettingRow{{
			Path: "auto-title", Section: "Session", Kind: SettingBool, Value: "false", Default: "true",
			Editable: true, Desc: "Name a new session from its first prompt.",
		}}
		m, _ := settingsEditModel(t, rows, &settingsWriteLog{})
		if armed {
			m = step(t, m, keyBackspace())
			if m.settings.kind != settingsResetArmed {
				t.Fatalf("pane = %+v, want the reset armed", m.settings)
			}
		}
		return m
	}
}

// settingsValueBufferModel is the /settings pane with the edit buffer open on its first of two keys.
func settingsValueBufferModel(t *testing.T) Model {
	t.Helper()
	m, _ := settingsEditModel(t, []SettingRow{settingsStringRow(), settingsIntRow()}, &settingsWriteLog{})
	m = step(t, m, keyEnter())
	if m.settings.kind != settingsValueBuffer {
		t.Fatalf("pane = %+v, want the edit buffer open", m.settings)
	}
	return m
}

// threeWayAsk is an ask_user question offering three answers.
func threeWayAsk(t *testing.T) Model {
	t.Helper()
	return askPaneModel(t, domain.AskRequest{Question: "which way?", Choices: []string{"left", "middle", "right"}})
}

// TestPointerShapeAtAnswersThePanes is the panes' half of the classifier's contract: inside a pane the
// shape is the one its click implies — the hand on a row the click acts on, the I-beam on the
// /settings field, the arrow on the rest of the box — and outside it the modal lists answer the arrow
// (their click only dismisses) while every other pane leaves the point to the chain.
func TestPointerShapeAtAnswersThePanes(t *testing.T) {
	t.Parallel()

	browser := func(t *testing.T) Model { return browserPaneModel(t, 3) }
	picker := func(t *testing.T) Model { return pickerPaneModel(t, pickerCycle) }
	workflows := func(t *testing.T) Model { return workflowsPaneModel(t, workflowsAtList) }
	dropdown := func(t *testing.T) Model { return dropdownPaneModel(t, testOpts, "/") }
	settings := func(t *testing.T) Model { return settingsFrameModel(t, 80, 24, 8) }
	paneRow := func(pane framePane) func(*testing.T, Model) (int, int) {
		return func(t *testing.T, m Model) (int, int) { return firstPaneRow(t, m, pane) }
	}

	for _, tc := range []struct {
		name  string
		model func(*testing.T) Model
		point func(*testing.T, Model) (x, y int)
		want  pointerShape
	}{
		{"settings: a key row", settings, onFrame("key-02"), shapePointer},
		{"settings: a section label", settings, onSettingsRow("Interface"), shapeDefault},
		{"settings: the legend", settings, onFrame(settingsHint), shapeDefault},
		{"settings: a key row before the reset arms", settingsResetModel(false), onFrame("auto-title"), shapePointer},
		{"settings: a key row under an armed reset", settingsResetModel(true), onSettingsRow("auto-title"), shapeDefault},
		{"settings: the edit buffer's text", settingsValueBufferModel, onFrame("box"), shapeText},
		{"settings: another key row under the edit buffer", settingsValueBufferModel, onSettingsRow("port"), shapeDefault},
		{"settings: the multi-line field's text", func(t *testing.T) Model {
			return settingsTextEditModel(t, "alpha beta gamma")
		}, onFrame("beta"), shapeText},

		{"usage report: inside", func(t *testing.T) Model { return usageReportModel(t, 2) }, paneMiddle(paneUsage), shapeDefault},
		{"inspector pane: inside", func(t *testing.T) Model { return inspectorPaneModel(t, 2) }, paneMiddle(paneInspector), shapeDefault},
		{"four reports: inside /usage", fourReportsModel, paneMiddle(paneUsage), shapeDefault},
		{"four reports: inside /inspect", fourReportsModel, paneMiddle(paneInspector), shapeDefault},
		{"four reports: inside /thinking", fourReportsModel, paneMiddle(paneThinking), shapeDefault},
		{"four reports: inside /advice", fourReportsModel, paneMiddle(paneAdvice), shapeDefault},

		{"browser: a session row", browser, onFrame("session number 01"), shapePointer},
		{"browser: its top border", browser, paneTopBorder(paneBrowser), shapeDefault},
		{"browser: a transcript line outside it", browser, aTranscriptLine, shapeDefault},

		{"picker: an offered row", picker, paneRow(panePicker), shapePointer},
		{"picker: its top border", picker, paneTopBorder(panePicker), shapeDefault},
		{"picker: a transcript line outside it", picker, aTranscriptLine, shapeDefault},

		{"workflows: a workflow row", workflows, paneRow(paneWorkflows), shapePointer},
		{"workflows: its top border", workflows, paneTopBorder(paneWorkflows), shapeDefault},
		{"workflows: a transcript line outside it", workflows, aTranscriptLine, shapeDefault},

		{"ask: an offered answer", threeWayAsk, onFrame("middle"), shapePointer},
		{"ask: its top border", threeWayAsk, paneTopBorder(panePrompt), shapeDefault},
		{"ask: a transcript line outside it", threeWayAsk, aTranscriptLine, shapeText},
		{"ask: the editable input row", threeWayAsk, theInputRow, shapeText},

		{"approval: a menu row", approvalPaneModel, paneRow(panePrompt), shapePointer},
		{"approval: its top border", approvalPaneModel, paneTopBorder(panePrompt), shapeDefault},
		{"approval: a transcript line outside it", approvalPaneModel, aTranscriptLine, shapeText},

		{"dropdown: a menu row", dropdown, paneRow(paneDropdown), shapePointer},
		{"dropdown: its top border", dropdown, paneTopBorder(paneDropdown), shapeDefault},
		{"dropdown: the editable input row below it", dropdown, theInputRow, shapeText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.model(t)
			x, y := tc.point(t, m)

			got := m.pointerShapeAt(m.withFrameSpans(), x, y)

			if got != tc.want {
				t.Fatalf("pointerShapeAt(%d, %d) = %s, want %s", x, y, got.name(), tc.want.name())
			}
		})
	}
}

// TestPointerShapeAtShowsNoPointerWhereAPaneGateRefusesTheClick is the state gates: a pane up in a
// state where its click takes no row — a rename, a confirm or a name row owning the pane, a list with
// nothing to choose, an answer already typed, the rule field open — shows the hand on none of its
// rows, though the rows are drawn.
func TestPointerShapeAtShowsNoPointerWhereAPaneGateRefusesTheClick(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		pane  framePane
		model func(*testing.T) Model
	}{
		{"browser: a rename edit", paneBrowser, func(t *testing.T) Model {
			m := browserPaneModel(t, 3)
			m.sessionBrowser.renaming = true
			return m
		}},
		{"browser: a delete confirm", paneBrowser, func(t *testing.T) Model {
			m := browserPaneModel(t, 3)
			m.sessionBrowser.confirming = true
			return m
		}},
		{"browser: no session matches the filter", paneBrowser, func(t *testing.T) Model {
			m := browserPaneModel(t, 3)
			for _, r := range "zzz" {
				m = step(t, m, keyRune(r))
			}
			return m
		}},
		{"picker: no offering matches the filter", panePicker, func(t *testing.T) Model {
			m := pickerPaneModel(t, pickerCycle)
			for _, r := range "zzz" {
				m = step(t, m, keyRune(r))
			}
			return m
		}},
		{"workflows: the name row open", paneWorkflows, func(t *testing.T) Model {
			m := workflowsPaneModel(t, workflowsAtDetail)
			m.workflowsPane.naming = true
			m.layout()
			return m
		}},
		{"workflows: the shown workflow gone", paneWorkflows, func(t *testing.T) Model {
			m := workflowsPaneModel(t, workflowsAtDetail)
			m.workflowsPane.infos = nil
			m.layout()
			return m
		}},
		{"ask: an answer typed into the box", panePrompt, func(t *testing.T) Model {
			m := threeWayAsk(t)
			m.input.SetValue("my own way")
			return m
		}},
		{"ask: a question offering no choices", panePrompt, func(t *testing.T) Model {
			return askPaneModel(t, domain.AskRequest{Question: "which way?"})
		}},
		{"approval: the rule field open", panePrompt, func(t *testing.T) Model {
			m, _, _ := projectRulePane(t, terminalGate("go test ./..."), nil)
			m = step(t, m, keyRune('p'))
			if !m.editingProjectRule() {
				t.Fatal("setup: p did not open the rule field")
			}
			return m
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := tc.model(t)
			pre := m.withFrameSpans()
			y0, h := paneBox(t, m, tc.pane)

			for y := y0; y < y0+h; y++ {
				for _, x := range []int{2, m.width / 2} {
					if got := m.pointerShapeAt(pre, x, y); got == shapePointer {
						t.Errorf("pointerShapeAt(%d, %d) = pointer inside the %s, whose click takes no row there", x, y, paneSpecs[tc.pane].name)
					}
				}
			}
		})
	}
}

// hoverThePrompt hovers m's prompt text row once and returns the model that wrote the I-beam for it,
// failing unless exactly that one write went out — the fixture a re-assert or a reset test needs: a
// session with a shape in force on the terminal. The gate must already be open on m.
func hoverThePrompt(t *testing.T, m Model) Model {
	t.Helper()
	x0, y0, _, _ := m.inputContentRect()
	m, writes := moveWrites(t, m, noneMotion(x0, y0))
	if want := ansi.SetPointerShape("text"); len(writes) != 1 || writes[0] != want {
		t.Fatalf("hovering the prompt wrote %q, want exactly %q", writes, want)
	}
	return m
}

// trackingWrites returns every raw write cmd delivers that opens with a mouse-tracking sequence —
// the re-assert's writes, whatever follows the tracking escapes inside them.
func trackingWrites(t *testing.T, cmd tea.Cmd) []string {
	t.Helper()
	var writes []string
	for _, msg := range expandBatch(cmd) {
		raw, isRaw := msg.(tea.RawMsg)
		if !isRaw {
			continue
		}
		if seq, isText := raw.Msg.(string); isText &&
			(strings.HasPrefix(seq, mouseTrackingSeq) || strings.HasPrefix(seq, allMotionTrackingSeq)) {
			writes = append(writes, seq)
		}
	}
	return writes
}

// TestReassertSaysTheWrittenShapeAfterTheTrackingSeq: with pointer shapes live and a shape written,
// both re-assert moments — a resize and a tool result — send ONE write carrying the all-motion
// tracking sequence and then the shape last written, because a child that reset the mouse may have
// reset the pointer with it.
func TestReassertSaysTheWrittenShapeAfterTheTrackingSeq(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		trigger tea.Msg
	}{
		{"resize", tea.WindowSizeMsg{Width: 100, Height: 30}},
		{"tool result", toolResult()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := hoverThePrompt(t, withPointerShapesLive(modelWithInput(t, "hello")))

			_, cmd := m.Update(tc.trigger)

			want := []string{allMotionTrackingSeq + ansi.SetPointerShape("text")}
			if got := trackingWrites(t, cmd); len(got) != 1 || got[0] != want[0] {
				t.Errorf("the %s re-asserted %q, want %q", tc.name, got, want)
			}
		})
	}
}

// TestReassertBeforeAnyShapeIsWrittenSendsTheBareTrackingSeq: with the gate open but no hover yet,
// the terminal is still on its own default pointer, so the re-assert says the all-motion tracking
// sequence alone — no OSC 22 byte for a shape the session never wrote.
func TestReassertBeforeAnyShapeIsWrittenSendsTheBareTrackingSeq(t *testing.T) {
	t.Parallel()
	m := withPointerShapesLive(modelWithInput(t, "hello"))

	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	if got := trackingWrites(t, cmd); len(got) != 1 || got[0] != allMotionTrackingSeq {
		t.Errorf("the resize re-asserted %q, want exactly %q", got, allMotionTrackingSeq)
	}
}

// TestClaimTerminalScreenReleaseResetsThePointerOnBothScreens: the exit release writes the pointer
// reset on the primary screen, enters the alternate one to reset its stack too, and leaves it — but
// only when the run left a shape in force; otherwise the release is the bare exit sequence, exactly
// as before pointer shapes, with no OSC 22 byte.
func TestClaimTerminalScreenReleaseResetsThePointerOnBothScreens(t *testing.T) {
	t.Parallel()
	const (
		claim = "\x1b[?1049h\x1b[3J"
		reset = "\x1b]22;\a"
		enter = "\x1b[?1049h"
		exit  = "\x1b[?1049l"
	)
	for _, tc := range []struct {
		name         string
		shapeWritten bool
		want         string
	}{
		{"no shape written", false, claim + exit},
		{"a shape written", true, claim + reset + enter + reset + exit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "screen")
			f, err := os.Create(path)
			if err != nil {
				t.Fatalf("create %s: %v", path, err)
			}
			defer func() { _ = f.Close() }()

			release, err := claimTerminalScreen(f)
			if err != nil {
				t.Fatalf("claimTerminalScreen: %v", err)
			}
			release(tc.shapeWritten)

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if string(got) != tc.want {
				t.Errorf("the terminal received %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPointerShapeResetReadsTheFinalModel: Run hands the exit release the final Model's word on
// whether a shape is in force — true after a hover wrote one, false for a session that never did,
// and false for anything that is not apogee's Model.
func TestPointerShapeResetReadsTheFinalModel(t *testing.T) {
	t.Parallel()
	quiet := withPointerShapesLive(modelWithInput(t, "hello"))
	hovered := hoverThePrompt(t, quiet)

	if !pointerShapeWrittenBy(hovered) {
		t.Error("a final Model that wrote a shape read as none; the exit would leave it in force")
	}
	if pointerShapeWrittenBy(quiet) || pointerShapeWrittenBy(nil) {
		t.Error("a final Model that never wrote a shape (or none at all) read as one; the exit would write a stray reset")
	}
}
