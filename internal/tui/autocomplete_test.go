package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
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
