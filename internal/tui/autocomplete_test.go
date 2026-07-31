package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
