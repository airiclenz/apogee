package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The command/file/skill dropdown adopts the shared popup chrome (selector-popup plan §3)
// ----------------------------------------------------------------------------

// newDropdownModel builds a ready, idle model at the 100×30 harness window (so the dropdown pane
// spans the 98-column chat area) over the given options.
func newDropdownModel(t *testing.T, opts Options) Model {
	t.Helper()
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
}

// The skill dropdown is painted with the shared popup chrome: a "skills" title row, the rounded
// box border, and the ❯ marker on the selected row — all above the bottom input chrome.
func TestAutocompleteSkillDropdownChrome(t *testing.T) {
	m := newDropdownModel(t, skillOpts())
	m.input.SetValue("/skill ")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())

	view := plain(m.View())
	if !strings.Contains(view, "skills") {
		t.Errorf("View is missing the \"skills\" title row:\n%s", view)
	}
	for _, glyph := range []string{"╭", "╰"} {
		if !strings.Contains(view, glyph) {
			t.Errorf("View is missing the popup border glyph %q:\n%s", glyph, view)
		}
	}
	if !strings.Contains(view, glyphUser+" Clean Code") {
		t.Errorf("the selected row does not lead with the %q marker:\n%s", glyphUser, view)
	}
	// The dropdown sits above the input box: the "skills" title precedes the "/skill" typed value,
	// which appears only on the input-box line.
	title, input := strings.Index(view, "skills"), strings.Index(view, "/skill")
	if title < 0 || input < 0 || title > input {
		t.Errorf("dropdown not above the input box: skills@%d /skill@%d\n%s", title, input, view)
	}
}

// The "/" command menu carries the "commands" title; an "@" file token carries the "files" title.
func TestAutocompleteCommandAndFileTitles(t *testing.T) {
	cmd := newDropdownModel(t, testOpts)
	cmd.input.SetValue("/")
	cmd.autocomplete = cmd.computeAutocomplete(cmd.caretByteOffset())
	if got := plain(cmd.View()); !strings.Contains(got, "commands") {
		t.Errorf("the \"/\" menu is missing the \"commands\" title:\n%s", got)
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	opts := testOpts
	opts.Workspace = dir
	files := newDropdownModel(t, opts)
	files.input.SetValue("@main")
	files.autocomplete = files.computeAutocomplete(files.caretByteOffset())
	if got := plain(files.View()); !strings.Contains(got, "files") {
		t.Errorf("an \"@\" token is missing the \"files\" title:\n%s", got)
	}
}

// The merged "/" menu mixes rows whose first column is a short verb ("/clear") with rows whose
// first column is a glyph plus a long id ("✦ /clean-code"), and every one of them starts its
// description at the SAME display column — the widest first cell plus the module's gutter. Before
// the column contract each producer concatenated its own two-space gap, so the summaries stepped
// raggedly down the pane behind whatever name preceded them.
func TestSlashMenuSummariesShareOneColumn(t *testing.T) {
	m := newDropdownModel(t, skillOpts())
	m.input.SetValue("/c") // the four c-verbs plus the clean-code skill: first cells of very different widths
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())

	items := m.autocomplete.items
	if len(items) < 2 {
		t.Fatalf("merged menu = %+v, want several rows to align against each other", items)
	}
	rows := make([]popupRow, len(items))
	widest := 0
	for i, it := range items {
		if len(it.cells) < 2 || it.cells[1] == "" {
			t.Fatalf("row %q cells = %q, want a description cell in the shared column", it.value, it.cells)
		}
		rows[i] = it.cells
		widest = max(widest, ansi.StringWidth(it.cells[0]))
	}
	if widest == ansi.StringWidth(items[0].cells[0]) {
		t.Fatalf("every first cell is %d cells wide — the test premise needs uneven names: %+v", widest, items)
	}

	want := widest + len(popupGutter)
	for i, ln := range layoutPopupRows(rows) {
		if got := popupCellOffset(t, ln, items[i].cells[1]); got != want {
			t.Errorf("row %q starts its description at column %d, want %d: %q", items[i].value, got, want, ln)
		}
	}
}

// Every physical line of the dropdown pane spans exactly the full window width (m.width, 100 at
// the 100×30 harness window) — flush with the input box below it, the same width the /sessions
// popup spans.
func TestAutocompleteDropdownSpansFullWidth(t *testing.T) {
	m := newDropdownModel(t, skillOpts())
	wantWidth := m.width // the full window width, matching the input box
	m.input.SetValue("/skill ")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	for i, ln := range popupLines(m.renderAutocomplete()) {
		if w := lipgloss.Width(ln); w != wantWidth {
			t.Errorf("dropdown line %d is %d cells, want %d: %q", i, w, wantWidth, strip(ln))
		}
	}
}

// The dropdown's window shrank to the frame's budget (renderAutocomplete → popupBudget), and this
// is the property that shrinking may not cost: the row the human is ON stays on the screen. A menu
// is a live filter over a moving list — every keystroke re-derives the rows and the arrow keys walk
// them — so a pane that seated the FIRST n rows of a fourteen-verb menu would leave the selection
// invisible from the third ↓ onwards on any window short enough to matter, which is worse than the
// overflow the budget fixed: the human would be accepting a row they cannot see. popupRowWindow is
// what holds it — the window scrolls around the selection rather than starting at row zero — and
// this asserts it through the composed pane at every short height, for every position the selection
// can take, so the guarantee is the drawn one and not the arithmetic's.
//
// The banded rows are the same property under the frame's SHARED allocation: a staged queue beside
// the menu takes its rows out of the same viewport, so the granted window is smaller than the same
// terminal gives an unaccompanied dropdown — and the row the human has arrowed onto is still in it.
func TestAutocompleteSelectionStaysOnScreenAtEveryBudget(t *testing.T) {
	cases := []struct{ height, staged int }{
		{18, 0}, {20, 0}, {22, 0}, {24, 0}, {30, 0},
		{22, 2}, {24, 2}, {30, 2},
	}
	for _, c := range cases {
		m := withStagedRows(modelWithOverlayRoomAt(t, 80, c.height, Options{Workspace: "/ws/a"}), c.staged)
		m.input.SetValue("/") // the whole verb table: more rows than these windows can seat
		m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
		if len(m.autocomplete.items) != len(commandSpecs) {
			t.Fatalf("the \"/\" menu offered %d rows, want every verb (%d)", len(m.autocomplete.items), len(commandSpecs))
		}
		if _, shown, _ := m.popupBudget(paneDropdown, len(m.autocomplete.items), maxAutocompleteItems); shown < 1 {
			t.Fatalf("a %d-row window with %d staged granted no dropdown rows — test premise broken", c.height, c.staged)
		}

		for i, it := range m.autocomplete.items {
			t.Run(fmt.Sprintf("%d rows/%d staged/select %s", c.height, c.staged, it.value), func(t *testing.T) {
				sel := m
				sel.autocomplete.selected = i
				flat := ansiPattern.ReplaceAllString(sel.renderAutocomplete(), "")
				// The marker is on the selected row and nowhere else, so finding the verb BESIDE it
				// is the proof the highlighted row is the one drawn — not merely that the text is
				// somewhere in the pane.
				if want := glyphUser + " /" + it.value; !strings.Contains(flat, want) {
					t.Errorf("row %d (%q) selected but not on the screen at %d rows with %d staged; want a row leading %q:\n%s",
						i, it.value, c.height, c.staged, want, flat)
				}
			})
		}
	}
}

// TestModalPromptDismissesTheDropdown closes the CO-OCCURRENCE the frame's row allocation was
// having to arbitrate: a "/" or "@" menu left open while the agent works survived, stale, into the
// modal prompt that interrupts it. Neither the approval fold nor the ask fold cleared it, and the
// menu is only ever re-derived at idle and running (recomputeAutocomplete), so what stayed on the
// screen beside the decision was frozen — no keystroke there filters it, dismisses it or accepts
// from it, because handleKey has given those keys to the decision — while it went on competing for
// the same four rows as the pane the run is blocked on.
func TestModalPromptDismissesTheDropdown(t *testing.T) {
	arrivals := []struct {
		name string
		msg  tea.Msg
	}{
		{"approval", approvalReqMsg{Request: domain.ApprovalRequest{Tool: "write_file", Reason: "it overwrites"}}},
		{"ask", askReqMsg{Request: domain.AskRequest{Question: "which way?", Choices: []string{"left", "right"}}}},
	}
	// Both regions the box opens while a worker runs, since both survive into the prompt the same way.
	drafts := []struct{ name, value string }{
		{"slash menu", "/"},
		{"file menu", "@"},
	}

	for _, a := range arrivals {
		for _, d := range drafts {
			t.Run(a.name+"/"+d.name, func(t *testing.T) {
				m := modelWithOverlayRoomAt(t, 80, 30, Options{Workspace: "."})
				m.state = stateRunning
				m.input.SetValue(d.value)
				m = m.recomputeAutocomplete()
				m.layout()
				if !m.autocomplete.active {
					t.Fatalf("the %q menu did not open — test premise broken", d.value)
				}
				before := plain(m.View())
				if !strings.Contains(before, "╭") {
					t.Fatalf("the menu is not on the frame — test premise broken:\n%s", before)
				}

				m = step(t, m, a.msg)

				if m.autocomplete.active {
					t.Errorf("the %q menu survived the %s prompt: a stale menu may not share the frame with a decision surface",
						d.value, a.name)
				}
				if got := m.frameOverlays().dropdown; got != "" {
					t.Errorf("the frame still draws the dropdown beside the prompt:\n%s", got)
				}
				if got := m.frameRowPlan(m.openPanes()).panes[paneDropdown]; got != 0 {
					t.Errorf("the row allocation still seats %d dropdown rows beside the prompt", got)
				}
			})
		}
	}
}
