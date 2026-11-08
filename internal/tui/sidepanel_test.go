package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
)

// sidePanelModel builds a ready, idle model under show and the scroll-bar setting, sized width × 30.
func sidePanelModel(t *testing.T, show domain.SidePanelShow, scrollbar bool, width int) Model {
	t.Helper()

	opts := testOpts
	opts.UI.SidePanelShow = show
	opts.UI.ShowScrollbar = scrollbar
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	return step(t, m, tea.WindowSizeMsg{Width: width, Height: 30})
}

// TestSidePanelGate pins when the column shows: `auto` from a 137-column window, `on` from a
// 117-column one, `off` and the zero UIPrefs' empty mode never — and the scroll bar's column counts
// either way, so switching the bar off moves no threshold. While the column shows the transcript
// slot is the window less its 36 columns, and the viewport is that slot less the bar.
func TestSidePanelGate(t *testing.T) {
	t.Parallel()

	shownFrom := map[domain.SidePanelShow]int{
		domain.SidePanelShowAuto: 137,
		domain.SidePanelShowOn:   117,
		domain.SidePanelShowOff:  0,
		"":                       0,
	}
	for show, from := range shownFrom {
		for _, scrollbar := range []bool{true, false} {
			for _, width := range []int{116, 117, 136, 137} {
				name := fmt.Sprintf("%q/bar=%v/%d", show, scrollbar, width)
				t.Run(name, func(t *testing.T) {
					t.Parallel()

					m := sidePanelModel(t, show, scrollbar, width)

					want := from != 0 && width >= from
					if got := m.panelShown(); got != want {
						t.Fatalf("width %d: panelShown() = %v, want %v", width, got, want)
					}
					slot := width
					if want {
						slot -= sidePanelWidth
					}
					if got := m.slotWidth(); got != slot {
						t.Errorf("width %d: slotWidth() = %d, want %d", width, got, slot)
					}
					viewport := slot
					if scrollbar {
						viewport -= scrollbarWidth
					}
					if got := m.viewport.Width(); got != viewport {
						t.Errorf("width %d: viewport width = %d, want %d", width, got, viewport)
					}
				})
			}
		}
	}
}

// TestSidePanelSectionChangeMovesNothing pins the stability rule's other half: a change to the
// section list is no input of the transcript's paint, so the frame key and the transcript's wrap
// width stand where they were — while a change of the show mode moves both, once.
func TestSidePanelSectionChangeMovesNothing(t *testing.T) {
	t.Parallel()

	m := sidePanelModel(t, domain.SidePanelShowAuto, true, 140)
	key, wrap := m.frameKey(), m.transcriptWidth()

	sections, _, _, _, err := m.settingsApplyLocal(domain.UIKeySidePanelSections, "[usage: pinned, tasks: hidden]")
	if err != nil {
		t.Fatalf("apply the section list: %v", err)
	}
	sections.settle()
	if sections.frameKey() != key {
		t.Errorf("a section change moved the frame key: %+v, was %+v", sections.frameKey(), key)
	}
	if got := sections.transcriptWidth(); got != wrap {
		t.Errorf("a section change re-wrapped the transcript at %d columns, was %d", got, wrap)
	}

	off, _, _, _, err := m.settingsApplyLocal(domain.UIKeySidePanelShow, string(domain.SidePanelShowOff))
	if err != nil {
		t.Fatalf("apply the show mode: %v", err)
	}
	off.settle()
	if off.frameKey() == key {
		t.Errorf("switching the panel off left the frame key where it was")
	}
	if got, want := off.transcriptWidth(), wrap+sidePanelWidth; got != want {
		t.Errorf("switching the panel off wraps the transcript at %d columns, want %d", got, want)
	}
}

// TestSidePanelColumnFillsTheSlotRows paints a shown, empty panel: every row above the
// session-name rule carries the separator at the slot's edge and the column's top row reads
// `nothing live`, while every row from the rule down keeps the whole window — the rule and the
// closing hairline run edge to edge with no separator cut into them.
func TestSidePanelColumnFillsTheSlotRows(t *testing.T) {
	t.Parallel()

	const width = 140
	m := sidePanelModel(t, domain.SidePanelShowAuto, true, width)
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	slot := m.slotWidth()

	rule := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "▔") {
			rule = i
			break
		}
	}
	if rule < 1 {
		t.Fatalf("no ▔ rule below the slot's rows:\n%s", strings.Join(lines, "\n"))
	}
	for i, ln := range lines[:rule] {
		if sep := ansi.Cut(ln, slot, slot+1); sep != sidePanelSeparator {
			t.Errorf("row %d carries %q at the slot's edge (column %d), want the separator: %q", i, sep, slot, ln)
		}
	}
	if top := strings.TrimSpace(ansi.Cut(lines[0], slot+1, width)); top != sidePanelEmpty {
		t.Errorf("the empty column's top row reads %q, want %q", top, sidePanelEmpty)
	}
	for i, ln := range lines[rule:] {
		if got := ansi.StringWidth(ln); got != width {
			t.Errorf("row %d below the slot paints %d columns, want the window's %d: %q", rule+i, got, width, ln)
		}
		if sep := ansi.Cut(ln, slot, slot+1); sep == sidePanelSeparator {
			t.Errorf("row %d below the slot carries the panel's separator: %q", rule+i, ln)
		}
	}
	if got, want := lines[rule], strings.Repeat("▔", width); got != want {
		t.Errorf("the session-name rule is %q, want %d ▔ runes edge to edge", got, len([]rune(want)))
	}
}
