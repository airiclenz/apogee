package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/sidepanel"
	"github.com/airiclenz/apogee/internal/tasklist"
	"github.com/airiclenz/apogee/internal/testtime"
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
// slot is the window less the default width clamped to that room — the gate's 36 columns at the
// threshold itself — and the viewport is that slot less the bar.
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
						// The default width, clamped to the room the show mode's floor leaves: the
						// gate's own width at the threshold itself.
						slot -= min(domain.DefaultSidePanelWidth, width-from+sidePanelGateWidth)
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
// width stand where they were — while a change of the show mode or of the width moves both, once.
func TestSidePanelSectionChangeMovesNothing(t *testing.T) {
	t.Parallel()

	m := sidePanelModel(t, domain.SidePanelShowAuto, true, 140)
	key, wrap, width := m.frameKey(), m.transcriptWidth(), m.panelWidth()

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
	if got, want := off.transcriptWidth(), wrap+width; got != want {
		t.Errorf("switching the panel off wraps the transcript at %d columns, want %d", got, want)
	}

	narrow, _, _, _, err := m.settingsApplyLocal(domain.UIKeySidePanelWidth, strconv.Itoa(domain.MinSidePanelWidth))
	if err != nil {
		t.Fatalf("apply the width: %v", err)
	}
	narrow.settle()
	if narrow.frameKey() == key {
		t.Errorf("narrowing the panel left the frame key where it was")
	}
	if narrow.painted != narrow.frameKey() {
		t.Errorf("narrowing the panel left the frame unsettled: painted %+v, key %+v", narrow.painted, narrow.frameKey())
	}
	if got, want := narrow.transcriptWidth(), wrap+width-domain.MinSidePanelWidth; got != want {
		t.Errorf("narrowing the panel wraps the transcript at %d columns, want %d", got, want)
	}
}

// TestSidePanelPaintedWidth pins the clamp the configured width paints through: an unset width
// paints the default while the window leaves the show mode's gate floor and the scroll-bar column
// beside it, and is held to that room otherwise; a stated one paints as set while the window leaves
// the 60-column transcript minimum (sidePanelTranscriptLeast) and the scroll-bar column, and is held
// to that room — never below the minimum width — otherwise; the show gate counts its own width
// (sidePanelGateWidth) whatever is configured. The slot is the window less the painted width, and
// the setting is never rewritten by the clamp.
func TestSidePanelPaintedWidth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		show   domain.SidePanelShow
		stored int
		window int
		want   int
		// isDefaults seeds the UI block from domain.DefaultUIPrefs() rather than storing a width by
		// hand, so the unset width under test is the one a config with no key resolves to.
		isDefaults bool
	}{
		{name: "unset paints the default", show: domain.SidePanelShowAuto, stored: 0, window: 200, want: domain.DefaultSidePanelWidth},
		{name: "unset docks clamped at the gate", show: domain.SidePanelShowAuto, stored: 0, window: 137, want: sidePanelGateWidth},
		{name: "defaults dock clamped at the gate", show: domain.DefaultSidePanelShow, window: 137, want: sidePanelGateWidth, isDefaults: true},
		{name: "stated fits", show: domain.SidePanelShowAuto, stored: 60, window: 200, want: 60},
		{name: "minimum fits", show: domain.SidePanelShowOn, stored: 24, window: 117, want: 24},
		{name: "auto stated fits beside the transcript minimum", show: domain.SidePanelShowAuto, stored: 60, window: 140, want: 60},
		{name: "on stated fits beside the transcript minimum", show: domain.SidePanelShowOn, stored: 60, window: 130, want: 60},
		{name: "auto transcript minimum caps", show: domain.SidePanelShowAuto, stored: 100, window: 140, want: 79},
		{name: "on transcript minimum caps", show: domain.SidePanelShowOn, stored: 100, window: 130, want: 69},
		{name: "a width wider than the window caps", show: domain.SidePanelShowAuto, stored: 300, window: 200, want: 139},
		{name: "below the minimum floors", show: domain.SidePanelShowAuto, stored: 10, window: 140, want: 24},
		{name: "auto gate ignores a narrow width", show: domain.SidePanelShowAuto, stored: 24, window: 136, want: 0},
		{name: "on gate ignores a narrow width", show: domain.SidePanelShowOn, stored: 24, window: 116, want: 0},
		{name: "auto gate ignores a wide width", show: domain.SidePanelShowAuto, stored: 60, window: 137, want: 60},
		{name: "off never paints", show: domain.SidePanelShowOff, stored: 60, window: 200, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := testOpts
			if tc.isDefaults {
				opts.UI = domain.DefaultUIPrefs()
			} else {
				opts.UI.SidePanelWidth = tc.stored
			}
			opts.UI.SidePanelShow = tc.show
			opts.UI.ShowScrollbar = true
			m := newModel(context.Background(), &fakeEngine{}, opts, nil)
			m = step(t, m, tea.WindowSizeMsg{Width: tc.window, Height: 30})

			if got := m.panelWidth(); got != tc.want {
				t.Errorf("panelWidth() = %d, want %d", got, tc.want)
			}
			if got, want := m.slotWidth(), tc.window-tc.want; got != want {
				t.Errorf("slotWidth() = %d, want %d", got, want)
			}
			if got, want := m.viewport.Width(), tc.window-tc.want-scrollbarWidth; got != want {
				t.Errorf("viewport width = %d, want %d", got, want)
			}
			if m.opts.UI.SidePanelWidth != tc.stored {
				t.Errorf("the clamp rewrote ui.side-panel.width to %d, was %d", m.opts.UI.SidePanelWidth, tc.stored)
			}
		})
	}
}

// TestSidePanelSeparatorAtANonDefaultWidth paints a 60-column panel in a 200-column window: the
// separator stands at the slot's edge on every row above the session-name rule, and each of those
// rows is the window's width.
func TestSidePanelSeparatorAtANonDefaultWidth(t *testing.T) {
	t.Parallel()

	const width = 200
	opts := testOpts
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	opts.UI.SidePanelWidth = 60
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: width, Height: 30})
	slot := m.slotWidth()
	if slot != width-60 {
		t.Fatalf("slotWidth() = %d, want %d", slot, width-60)
	}

	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	rule := slices.IndexFunc(lines, func(ln string) bool { return strings.HasPrefix(ln, "▔") })
	if rule < 1 {
		t.Fatalf("no ▔ rule below the slot's rows:\n%s", strings.Join(lines, "\n"))
	}
	for i, ln := range lines[:rule] {
		if sep := ansi.Cut(ln, slot, slot+1); sep != sidePanelSeparator {
			t.Errorf("row %d carries %q at the slot's edge (column %d), want the separator: %q", i, sep, slot, ln)
		}
		if got := ansi.StringWidth(ln); got != width {
			t.Errorf("row %d paints %d columns, want the window's %d: %q", i, got, width, ln)
		}
	}
}

// TestSidePanelDockedAtTheTranscriptMinimumFitsTheFrame docks the widest set width a 140-column
// `auto` window allows — the transcript at its 60-column minimum (sidePanelTranscriptLeast) — beside
// a tool block and a draft in the prompt box: every frame line fits the window, and the tool row and
// the draft paint whole.
func TestSidePanelDockedAtTheTranscriptMinimumFitsTheFrame(t *testing.T) {
	t.Parallel()

	const (
		window = 140
		draft  = "check the panel beside a narrow transcript"
	)
	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		panelToolPair(domain.EventBase{}, "c1", "read_file", `{"path":"internal/tui/model.go"}`, "")...)
	m.opts.UI.SidePanelWidth = window - sidePanelTranscriptLeast - scrollbarWidth
	m = step(t, m, tea.WindowSizeMsg{Width: window, Height: 30})
	m = typeText(t, m, draft)
	if got := m.slotWidth(); got != sidePanelTranscriptLeast+scrollbarWidth {
		t.Fatalf("setup: slotWidth() = %d, want the transcript minimum and the bar, %d",
			got, sidePanelTranscriptLeast+scrollbarWidth)
	}

	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	var slotText []string
	for i, ln := range lines {
		if got := ansi.StringWidth(ln); got > window {
			t.Errorf("row %d paints %d columns, past the window's %d: %q", i, got, window, ln)
		}
		slotText = append(slotText, ansi.Cut(ln, 0, m.slotWidth()))
	}
	slot := strings.Join(slotText, "\n")
	for _, text := range []string{"Read", "internal/tui/model.go"} {
		if !strings.Contains(slot, text) {
			t.Errorf("the transcript slot paints no whole %q of the tool row:\n%s", text, slot)
		}
	}
	if frame := strings.Join(lines, "\n"); !strings.Contains(frame, draft) {
		t.Errorf("the prompt box paints no whole draft %q:\n%s", draft, frame)
	}
}

// TestSidePanelNarrowestColumnPaintsLongRows paints the column at its minimum width with the rows
// whose right-hand facts outrun its room — a viewed running sub-agent past an hour, its facts
// beside the `▸` mark — and pins that each row is squared to the column, not panicked on.
func TestSidePanelNarrowestColumnPaintsLongRows(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		panelDelegationCall(t, "r1", "a-sub-agent-with-a-long-name"),
		delegationPhase("r1", domain.SubAgentStarted, 2*time.Hour),
		landedEdit(delegationBase("r1"), "internal/a/very/deep/path/to/a/file.go", 1200, 3400),
	)
	m = openViewOf(t, m, delegationBase("r1"))
	m.opts.UI.SidePanelWidth = domain.MinSidePanelWidth
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	if got := m.panelWidth(); got != domain.MinSidePanelWidth {
		t.Fatalf("setup: panelWidth() = %d, want %d", got, domain.MinSidePanelWidth)
	}

	for i, row := range m.sidePanelColumn(30) {
		if got := m.th.measure.Width(row); got != domain.MinSidePanelWidth {
			t.Errorf("row %d paints %d columns, want %d: %q", i, got, domain.MinSidePanelWidth, ansi.Strip(row))
		}
	}
	if got := panelText(t, m); !slices.ContainsFunc(got, func(row string) bool { return strings.Contains(row, "▸ running") }) {
		t.Errorf("the narrow column paints no viewed run:\n%s", strings.Join(got, "\n"))
	}
}

// TestPanelElapsedForms pins a sub-agent row's clock: the status line's form below an hour, then
// hours and minutes, then days and hours, clamped at 99d23h — never wider than the six columns of
// subAgentFactsFormat's elapsed field.
func TestPanelElapsedForms(t *testing.T) {
	t.Parallel()

	const day = 24 * time.Hour
	tests := []struct {
		d    time.Duration
		want string
	}{
		{d: 38 * time.Second, want: "38s"},
		{d: 59*time.Minute + 59*time.Second, want: "59:59s"},
		{d: time.Hour, want: "1h00m"},
		{d: time.Hour + 2*time.Minute + 3*time.Second, want: "1h02m"},
		{d: 23*time.Hour + 59*time.Minute + 59*time.Second, want: "23h59m"},
		{d: day, want: "1d00h"},
		{d: day + 2*time.Hour + 59*time.Minute, want: "1d02h"},
		{d: 99*day + 23*time.Hour + 59*time.Minute, want: "99d23h"},
		{d: 120 * day, want: "99d23h"}, // clamped: the field holds no third day digit
	}
	for _, tc := range tests {
		t.Run(tc.d.String(), func(t *testing.T) {
			t.Parallel()

			got := panelElapsed(tc.d)

			if got != tc.want {
				t.Errorf("panelElapsed(%v) = %q, want %q", tc.d, got, tc.want)
			}
			if width := ansi.StringWidth(got); width > 6 {
				t.Errorf("panelElapsed(%v) = %q is %d columns, want at most 6", tc.d, got, width)
			}
		})
	}
}

// TestSubAgentRowAlignsPastAnHour paints two running sub-agents, one an hour and more old and one
// under a minute, at the default-ish and the narrowest column width: the phase word and the end of
// the elapsed field sit at the same column on both rows, and no row outruns the column.
func TestSubAgentRowAlignsPastAnHour(t *testing.T) {
	t.Parallel()

	for _, width := range []int{36, domain.MinSidePanelWidth} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()

			m := panelModel(t, domain.DefaultSidePanelSections, 30,
				panelDelegationCall(t, "r1", "alpha"),
				delegationPhase("r1", domain.SubAgentStarted, time.Hour+2*time.Minute+3*time.Second),
				panelDelegationCall(t, "r2", "beta"),
				delegationPhase("r2", domain.SubAgentStarted, 38*time.Second),
			)
			m.opts.UI.SidePanelWidth = width
			m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})

			long := factColumns(t, panelText(t, m), "1h02m")
			short := factColumns(t, panelText(t, m), "38s")

			if long != short {
				t.Errorf("width %d: the 1h02m row's facts sit at %v, the 38s row's at %v — want equal", width, long, short)
			}
			for i, row := range m.sidePanelColumn(30) {
				if got := m.th.measure.Width(row); got != width {
					t.Errorf("width %d: row %d paints %d columns: %q", width, i, got, ansi.Strip(row))
				}
			}
		})
	}
}

// TestSubAgentRowFactsPaintWholeAtTheMinimumWidth pins the narrow-width yield chain
// (docs/layout/side-panel-layout.md, "Columns"): at the minimum width, under every spinner style, a
// running sub-agent row's facts paint whole — the right margin and the gutter's extra cell yield
// before any fact is cut, the name going first.
func TestSubAgentRowFactsPaintWholeAtTheMinimumWidth(t *testing.T) {
	t.Parallel()

	for _, style := range domain.SpinnerStyleNames() {
		t.Run(string(style), func(t *testing.T) {
			t.Parallel()
			m := panelModel(t, domain.DefaultSidePanelSections, 30)
			m.opts.UI.Spinner = style
			m.opts.UI.SidePanelWidth = domain.MinSidePanelWidth
			m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
			row := sidepanel.SubAgentRow{Name: "alpha", Phase: sidepanel.PhaseRunning, Elapsed: 38 * time.Second,
				Fill: sidepanel.Fill{Used: 62, Window: 100}}
			facts := fmt.Sprintf(subAgentFactsFormat, "running", "38s", "62%")

			painted := m.panelSubAgentRow(row, false)

			if got, room := m.th.measure.Width(painted), m.panelContentWidth(); got > room {
				t.Errorf("the row paints %d columns into %d: %q", got, room, ansi.Strip(painted))
			}
			if got := ansi.Strip(painted); !strings.HasSuffix(got, facts) {
				t.Errorf("the row reads %q, want its facts %q whole", got, facts)
			}
		})
	}
}

// TestSidePanelRightEdgeMeetsStatusLine pins the column's right margin
// (docs/layout/side-panel-layout.md, "Columns"): on a wide window a header's fold indicator and a
// row's right-aligned facts sit in the status line's last painted cell's column, bodyIndent short of
// the window's edge.
func TestSidePanelRightEdgeMeetsStatusLine(t *testing.T) {
	t.Parallel()
	m := panelModel(t, "[tasks: auto, changed-files: auto, usage: hidden]", 30, taskList(0, "one"),
		// A full window, so the gauge's bar — the status line's last cells — paints solid blocks.
		landedEdit(domain.EventBase{}, "a.go", 3, 1), panelUsageReading(testOpts.ContextWindow))
	m = step(t, m, tea.WindowSizeMsg{Width: 170, Height: 30})
	frame := strings.Split(ansi.Strip(m.View().Content), "\n")
	rule := slices.IndexFunc(frame, func(line string) bool { return strings.HasPrefix(line, "▔") })
	if rule < 0 {
		t.Fatal("no ▔ rule on the frame")
	}
	statusEdge := ansi.StringWidth(strings.TrimRight(frame[rule+1], " ")) - 1
	if statusEdge < 0 {
		t.Fatal("setup: the status line paints nothing on its right")
	}

	header := frame[panelRowY(t, m, "Tasks")]
	file := frame[panelRowY(t, m, "a.go")]

	if got := ansi.StringWidth(header[:strings.Index(header, glyphExpanded)]); got != statusEdge {
		t.Errorf("the Tasks header's %s sits at column %d, the status line's last cell at %d", glyphExpanded, got, statusEdge)
	}
	if got := ansi.StringWidth(strings.TrimRight(file, " ")) - 1; got != statusEdge {
		t.Errorf("the a.go row's facts end at column %d, the status line's last cell at %d", got, statusEdge)
	}
}

// TestSidePanelPinCellIsNoGrip pins the grip to the separator and the one cell right of it
// (adr-0110-amend-1108 (b)) whatever the gutter yields: at every width down to the minimum, under
// every spinner style, a press on a header's lead pin cell toggles the pin and never arms a drag.
func TestSidePanelPinCellIsNoGrip(t *testing.T) {
	t.Parallel()

	for _, style := range domain.SpinnerStyleNames() {
		for _, width := range []int{domain.MinSidePanelWidth, 25, 26, 30, domain.DefaultSidePanelWidth} {
			t.Run(fmt.Sprintf("%s/%d", style, width), func(t *testing.T) {
				t.Parallel()
				m := panelModel(t, "[tasks: auto]", 30, taskList(0, "one"))
				m.opts.UI.Spinner = style
				m.opts.UI.SidePanelWidth = width
				m = step(t, m, tea.WindowSizeMsg{Width: 200, Height: 30}).withFrameSpans()
				pin, y := m.panelPinX(), panelRowY(t, m, "Tasks")

				_, isGrip := m.panelGripAt(pin)
				target, _ := m.panelTargetAt(pin, y)

				if isGrip {
					t.Errorf("the lead pin cell %d is a grip", pin)
				}
				if target.kind != panelTargetPin {
					t.Errorf("a click on the lead pin cell %d follows %+v, want the pin toggle", pin, target)
				}
				for x := m.slotWidth(); x < m.slotWidth()+sidePanelGripWidth; x++ {
					if _, isGrip := m.panelGripAt(x); !isGrip {
						t.Errorf("column %d, the separator's or the cell right of it, is no grip", x)
					}
				}
			})
		}
	}
}

// factColumns finds the painted sub-agent row whose elapsed field reads elapsed and returns the
// column its phase word starts at and the column its elapsed field ends at.
func factColumns(t *testing.T, column []string, elapsed string) [2]int {
	t.Helper()

	for _, row := range column {
		end := strings.Index(row, " "+elapsed)
		phase := strings.Index(row, "running")
		if end < 0 || phase < 0 {
			continue
		}
		return [2]int{ansi.StringWidth(row[:phase]), ansi.StringWidth(row[:end+1]) + ansi.StringWidth(elapsed)}
	}
	t.Fatalf("no running row reads %q:\n%s", elapsed, strings.Join(column, "\n"))
	return [2]int{}
}

// TestSidePanelColumnFillsTheSlotRows paints a shown, empty panel: every row above the
// session-name rule carries the separator at the slot's edge, the column's top row is blank and its
// second reads `nothing live`, and the column is exactly the slot's rows ([frameSpans.slotBottom]),
// while every row from the rule down keeps the whole window — the rule and the closing hairline run
// edge to edge with no separator cut into them.
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
	if top := strings.TrimSpace(ansi.Cut(lines[0], slot+1, width)); top != "" {
		t.Errorf("the empty column's top row reads %q, want it blank", top)
	}
	if second := strings.TrimSpace(ansi.Cut(lines[1], slot+1, width)); second != sidePanelEmpty {
		t.Errorf("the empty column's second row reads %q, want %q", second, sidePanelEmpty)
	}
	if slotRows := m.frameSpans().slotBottom; rule != slotRows {
		t.Errorf("the column paints %d rows, want the slot's %d", rule, slotRows)
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

// panelNow is the instant every panel test's State reads as now.
var panelNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// panelModel builds a ready model whose column shows (auto, 140 columns × height) under the
// section list sections, its panel on a clock fixed at panelNow, with events folded through the
// view's own Event fold.
func panelModel(t *testing.T, sections domain.SidePanelSections, height int, events ...domain.Event) Model {
	t.Helper()

	opts := testOpts
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	opts.UI.SidePanelSections = sections
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: height})
	m.panel = sidepanel.New(func() time.Time { return panelNow })
	for _, e := range events {
		m = m.foldEvent(e)
	}
	return m
}

// panelHeaderTail is a header row's dotted leader and its fold indicator at the right edge, in
// either glyph set: what [stripHeaderTail] cuts off so a row reads as its pin, title and summary.
var panelHeaderTail = regexp.MustCompile(` [⋯·]+ [▼▶►]$`)

// stripHeaderTail is row with a header's leader and fold indicator cut off; any other row is
// returned as it is. The header's own shape is pinned by TestSidePanelHeaderLeadsWithPin and
// TestSidePanelFoldTogglesAndWritesBack, so the tests reading a section's content read the
// header's words alone.
func stripHeaderTail(row string) string {
	return panelHeaderTail.ReplaceAllString(row, "")
}

// panelText is the column's content as the frame paints it: one escape-stripped, right-trimmed
// string per row above the session-name rule, the trailing blank rows dropped, each header's
// leader and fold indicator cut off ([stripHeaderTail]).
func panelText(t *testing.T, m Model) []string {
	t.Helper()

	slot := m.slotWidth()
	var column []string
	for _, ln := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.HasPrefix(ln, "▔") {
			for len(column) > 0 && column[len(column)-1] == "" {
				column = column[:len(column)-1]
			}
			return column
		}
		column = append(column, stripHeaderTail(strings.TrimRight(ansi.Cut(ln, slot+m.panelLeftChrome(), m.width), " ")))
	}
	t.Fatal("no ▔ rule below the slot's rows")
	return nil
}

// belowPanelTopRow is rows as panelText reads them under the column's blank top row ([panelLayout]):
// the content the column paints from its second row down.
func belowPanelTopRow(rows ...string) []string {
	return append([]string{""}, rows...)
}

// columnText is sidePanelColumn's rows rows, escape-stripped and right-trimmed, each header's
// leader and fold indicator cut off ([stripHeaderTail]).
func columnText(m Model, rows int) []string {
	column := m.sidePanelColumn(rows)
	for i, row := range column {
		column[i] = stripHeaderTail(strings.TrimRight(ansi.Cut(ansi.Strip(row), m.panelLeftChrome(), m.panelPaintWidth()), " "))
	}
	return column
}

// panelDelegationCall is the parent's sub_agent call spawning run id under name.
func panelDelegationCall(t *testing.T, id, name string) domain.ToolCallEvent {
	t.Helper()
	args, err := json.Marshal(map[string]any{"name": name, "task": "do " + name})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return domain.ToolCallEvent{
		Call:       domain.ToolCall{ID: "call-" + id, Tool: "sub_agent", Arguments: args},
		SpawnRunID: id,
	}
}

// delegationBase is the base every event of run id carries.
func delegationBase(id string) domain.EventBase {
	return domain.EventBase{Depth: 1, CallID: "call-" + id, RunID: id}
}

// delegationPhase moves run id to p at panelNow less ago.
func delegationPhase(id string, p domain.SubAgentPhase, ago time.Duration) domain.SubAgentPhaseEvent {
	return domain.SubAgentPhaseEvent{EventBase: delegationBase(id), Phase: p, At: panelNow.Add(-ago)}
}

// taskList is a top-level task list whose first done tasks are finished.
func taskList(done int, texts ...string) domain.TaskListEvent {
	items := make([]domain.TaskItem, 0, len(texts))
	for i, text := range texts {
		items = append(items, domain.TaskItem{Text: text, Done: i < done})
	}
	return domain.TaskListEvent{Items: items}
}

// landedEdit is a landed write to path by base's run, with one Edit region of added and removed
// lines where added+removed > 0.
func landedEdit(base domain.EventBase, path string, added, removed int) domain.ToolResultEvent {
	var summary domain.ToolSummary
	if added+removed > 0 {
		summary = domain.EditRegions{Regions: []domain.EditRegion{{
			Inserted: slices.Repeat([]string{"+"}, added),
			Removed:  slices.Repeat([]string{"-"}, removed),
		}}}
	}
	return domain.ToolResultEvent{EventBase: base, WriteTarget: path, Result: domain.ToolResult{Summary: summary}}
}

// panelUsed is a top-level fill of 62% of the session's window: past the usage section's live rule.
const panelUsed = 20480

// panelUsageReading is a top-level reading of used tokens of the session's window, its cumulative
// totals beside it. The window is the session's because the top-level fill is the gauge's, which
// measures against [Model.topLevelWindow].
func panelUsageReading(used int) domain.UsageEvent {
	return domain.UsageEvent{
		PromptTokens: used - 20, CompletionTokens: 20, TotalTokens: used, ContextWindow: testOpts.ContextWindow,
		Cumulative: domain.Usage{
			Calls: 2, PromptTokens: 4000, CachedPromptTokens: 1000, CompletionTokens: 300, TotalTokens: 4300,
		},
	}
}

// standingCost is a Turn's context-cost report: standing system content and the tool menu.
func standingCost(system, tools int) domain.ContextCostEvent {
	return domain.ContextCostEvent{Cost: domain.ContextCost{
		Tokens: system + tools,
		Rows:   []domain.ContextCostRow{{Name: "system prompt", Tokens: system}, {Name: "tool menu", Tokens: tools}},
	}}
}

// TestSidePanelPaintsEachSection paints every section, one at a time, from the Events that make it
// live, as docs/layout/side-panel-layout.md sketches it: the header row led by the auto section's
// ○ with its summary in parentheses, then the section's rows.
func TestSidePanelPaintsEachSection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		events func(t *testing.T) []domain.Event
		want   []string
	}{
		{
			name: "sub-agents",
			events: func(t *testing.T) []domain.Event {
				return []domain.Event{
					panelDelegationCall(t, "r1", "planner"),
					delegationPhase("r1", domain.SubAgentStarted, time.Minute),
					delegationPhase("r1", domain.SubAgentFinished, 22*time.Second),
					panelDelegationCall(t, "r2", "repo-scout"),
				}
			},
			want: []string{
				"○ Sub-agents (1 queued)",
				"  ✓ planner    done       38s",
				"  ○ repo-scout queued",
			},
		},
		{
			name: "tasks",
			events: func(*testing.T) []domain.Event {
				return []domain.Event{taskList(1, "map call sites", "split the tokenizer", "port the tests")}
			},
			want: []string{
				"○ Tasks (1/3)",
				"  ✓ map call sites",
				"  ▸ split the tokenizer",
				"  ○ port the tests",
			},
		},
		{
			name: "changed files",
			events: func(*testing.T) []domain.Event {
				return []domain.Event{
					landedEdit(domain.EventBase{}, "internal/parse/parser.go", 12, 4),
					landedEdit(delegationBase("r1"), "internal/parse/a_rather_long_dir/lexer_test.go", 0, 0),
				}
			},
			want: []string{
				"○ Changed files (2)",
				"    …/parse/parser.go       +12 −4",
				"  ↳ …/lexer_test.go",
			},
		},
		{
			name: "usage",
			events: func(*testing.T) []domain.Event {
				return []domain.Event{panelUsageReading(panelUsed), standingCost(200, 100)}
			},
			want: []string{
				"○ Usage (62%)",
				"    20k/32k 62% ██████▎",
				"    sys 200 · tools 100 · hist 20k",
				"    prompt 4k · completion 300",
				"    cached 25%",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := panelModel(t, domain.DefaultSidePanelSections, 30, tc.events(t)...)

			if got, want := panelText(t, m), belowPanelTopRow(tc.want...); !slices.Equal(got, want) {
				t.Errorf("the column paints\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// TestSidePanelUsageHeaderClampsOverfullFill pins one clamp across every surface that spells a fill
// as a percentage: a top-level fill and a sub-agent's fill past their windows read 100% in the
// panel's Usage header, in its usage fill row, in the sub-agent row, in /usage's fill cell and in
// the status-line gauge — never the unclamped figure past a hundred (bug
// panel-usage-unclamped-percent).
func TestSidePanelUsageHeaderClampsOverfullFill(t *testing.T) {
	t.Parallel()

	over := testOpts.ContextWindow * 137 / 100
	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		panelDelegationCall(t, "r1", "repo-scout"),
		delegationPhase("r1", domain.SubAgentStarted, time.Minute),
		domain.UsageEvent{EventBase: delegationBase("r1"), TotalTokens: 12000, ContextWindow: 8000},
		panelUsageReading(over),
	)

	header := ""
	for _, row := range panelText(t, m) {
		if strings.Contains(row, "Usage") {
			header = row
			break
		}
	}
	if !strings.Contains(header, "Usage (100%)") {
		t.Errorf("usage header = %q, want it to read Usage (100%%)", header)
	}
	over137 := format.Tokens(over) + "/" + format.Tokens(testOpts.ContextWindow)
	if row := panelRowOf(t, m, over137); !strings.Contains(row, " "+over137+" 100% █") {
		t.Errorf("usage fill row = %q, want it to read %s 100%% then a full bar", row, over137)
	}
	if row := panelRowOf(t, m, "repo-scout"); !strings.Contains(row, " 100%") {
		t.Errorf("sub-agent row = %q, want its overfull fill clamped to 100%%", row)
	}
	if got := usageFillCell(over, testOpts.ContextWindow); got != "100%" {
		t.Errorf("usageFillCell = %q, want 100%%", got)
	}
	if gauge := ansi.Strip(m.contextGauge()); !strings.Contains(gauge, " 100% ") {
		t.Errorf("status-line gauge = %q, want its percentage clamped to 100%%", gauge)
	}
}

// TestSidePanelUsageRowsSpeakUsageWords pins the usage rows' words and zero rule to /usage's
// (layout.md, "The /usage popup"): `prompt` and `completion`, never `in` and `out`, and a zero
// count left out of its row rather than printed as `0` — a row whose every count is zero is no
// row at all, and no cached share is no `cached` row.
func TestSidePanelUsageRowsSpeakUsageWords(t *testing.T) {
	t.Parallel()

	reading := panelUsageReading(panelUsed)
	reading.Cumulative = domain.Usage{Calls: 1, PromptTokens: 4000, TotalTokens: 4000}
	m := panelModel(t, domain.DefaultSidePanelSections, 30, reading, standingCost(0, 100))

	want := belowPanelTopRow(
		"○ Usage (62%)",
		"    20k/32k 62% ██████▎",
		"    tools 100 · hist 20k",
		"    prompt 4k",
	)
	if got := panelText(t, m); !slices.Equal(got, want) {
		t.Errorf("the column paints\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestSidePanelUsageBarPaintsNoBackground pins the usage fill row's bar to ink alone: in the dark
// and the light scheme every one of its gaugeWidth cells — the full blocks, the partial cell and
// the blank track — leaves the terminal's own background, and its filled cells wear the gauge
// colour. The status line's gauge keeps its groove (TestBottomChromePaintsOneBackground).
func TestSidePanelUsageBarPaintsNoBackground(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		s    func(*testing.T) scheme.Scheme
	}{
		{"dark", func(*testing.T) scheme.Scheme { return scheme.Default() }},
		{"light", lightScheme},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := testOpts
			opts.ColorScheme = tc.s(t)
			opts.UI.SidePanelShow = domain.SidePanelShowAuto
			opts.UI.SidePanelSections = domain.DefaultSidePanelSections
			m := newModel(context.Background(), &fakeEngine{}, opts, nil)
			m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
			m.panel = sidepanel.New(func() time.Time { return panelNow })
			m = m.foldEvent(panelUsageReading(panelUsed))

			y := panelRowY(t, m, "20k/32k 62% ")
			buf := paintedFrame(t, m)
			start := -1
			for x := m.slotWidth(); x < m.width; x++ {
				if c := buf.CellAt(x, y); c != nil && c.Content == "█" {
					start = x
					break
				}
			}
			if start < 0 {
				t.Fatalf("row %d (%q) paints no bar", y, buf.Lines[y].String())
			}
			ink := 0
			for x := start; x < start+gaugeWidth; x++ {
				c := buf.CellAt(x, y)
				if c == nil {
					t.Fatalf("row %d: no cell at column %d", y, x)
				}
				if c.Style.Bg != nil {
					t.Errorf("bar cell %d (%q): background %v, want the terminal's own", x-start, c.Content, c.Style.Bg)
				}
				if c.Content != " " {
					ink++
					if !sameColor(c.Style.Fg, m.th.gaugeFill.GetForeground()) {
						t.Errorf("bar cell %d (%q): ink %v, want the gauge colour", x-start, c.Content, c.Style.Fg)
					}
				}
			}
			if ink != 7 { // 62% of a ten-cell bar: six full blocks and the partial cell
				t.Errorf("the bar inks %d cells, want 7", ink)
			}
		})
	}
}

// TestSidePanelUsageFillRowKeepsFiguresOnANarrowColumn pins the narrow fill row: a column too
// narrow for the figures and the whole bar keeps the figures — the reading — and paints no bar,
// never a bar cut short.
func TestSidePanelUsageFillRowKeepsFiguresOnANarrowColumn(t *testing.T) {
	t.Parallel()
	m := panelModel(t, domain.DefaultSidePanelSections, 30, panelUsageReading(panelUsed))
	m.opts.UI.SidePanelWidth = domain.MinSidePanelWidth
	if room := m.panelTextRoom(m.panelBodyLead("", ""), ""); room >= len("20k/32k 62% ")+gaugeWidth {
		t.Fatalf("setup: the narrowest column leaves %d cells, room for the whole bar", room)
	}

	row := panelRowOf(t, m, "20k/32k")
	if !strings.HasSuffix(row, " 20k/32k 62%") {
		t.Errorf("usage fill row = %q, want the figures alone", row)
	}
}

// TestSidePanelRunningDelegationSpinsWithTheStatusLine pins a running row: the status line's own
// spinner frame leads it, and its elapsed time counts to now.
func TestSidePanelRunningDelegationSpinsWithTheStatusLine(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		panelDelegationCall(t, "r1", "repo-scout"), delegationPhase("r1", domain.SubAgentStarted, 72*time.Second))

	got := panelText(t, m)
	want := bodyIndent + ansi.Strip(m.spin.drawn(m.th, m.opts.UI.Spinner)) + " repo-scout running 01:12s"
	if len(got) != 3 || got[0] != "" || got[1] != "○ Sub-agents (1)" || got[2] != want {
		t.Errorf("the column paints\n%s\nwant the running header over %q", strings.Join(got, "\n"), want)
	}
}

// spinnerPanelModel is panelModel under the default sections with the spinner style and glyph set
// named explicitly — testUIPrefs leaves the style unset, which resolves to classic.
func spinnerPanelModel(t *testing.T, style SpinnerStyle, glyphs domain.GlyphSet, color bool, events ...domain.Event) Model {
	t.Helper()

	opts := testOpts
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	opts.UI.SidePanelSections = domain.DefaultSidePanelSections
	opts.UI.Spinner = style
	opts.UI.Glyphs = glyphs
	opts.UI.SpinnerColor = color
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	m.panel = sidepanel.New(func() time.Time { return panelNow })
	for _, e := range events {
		m = m.foldEvent(e)
	}
	return m
}

// TestSidePanelSubAgentNamesAlignUnderEverySpinner paints a run of every phase — running, queued,
// done, failed and waiting — under each spinner style, in both glyph sets and across several
// frames: every row's name starts in the same column, so a two-cell spinner frame widens every
// lead instead of shoving the running row's name one column right of the rest.
func TestSidePanelSubAgentNamesAlignUnderEverySpinner(t *testing.T) {
	t.Parallel()

	names := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	for _, glyphs := range []domain.GlyphSet{domain.GlyphsUnicode, domain.GlyphsSafe} {
		for style := range spinnerSpecs {
			t.Run(string(glyphs)+"/"+string(style), func(t *testing.T) {
				t.Parallel()

				m := spinnerPanelModel(t, style, glyphs, true,
					panelDelegationCall(t, "r1", "alpha"),
					delegationPhase("r1", domain.SubAgentStarted, time.Minute),
					panelDelegationCall(t, "r2", "bravo"),
					panelDelegationCall(t, "r3", "charlie"),
					delegationPhase("r3", domain.SubAgentStarted, time.Minute),
					delegationPhase("r3", domain.SubAgentFinished, 22*time.Second),
					panelDelegationCall(t, "r4", "delta"),
					delegationPhase("r4", domain.SubAgentStarted, time.Minute),
					domain.SubAgentPhaseEvent{EventBase: delegationBase("r4"), Phase: domain.SubAgentFinished,
						Result: domain.ToolResult{IsError: true}, At: panelNow.Add(-10 * time.Second)},
					panelDelegationCall(t, "r5", "echo"),
					delegationPhase("r5", domain.SubAgentStarted, 30*time.Second),
					panelGate("r5", domain.ApprovalRequested, ""),
				)
				for _, phase := range []string{" running ", " queued", " done ", " failed ", " waiting "} {
					if !slices.ContainsFunc(panelText(t, m), func(row string) bool { return strings.Contains(row, phase) }) {
						t.Fatalf("setup: no row reads %q:\n%s", phase, strings.Join(panelText(t, m), "\n"))
					}
				}
				for frame := range 4 {
					m.spin.frame = frame
					column := panelText(t, m)
					want := -1
					for _, name := range names {
						row := panelRowOf(t, m, name)
						at := strings.Index(row, " "+name+" ")
						if at < 0 {
							t.Fatalf("frame %d: no row names %q:\n%s", frame, name, strings.Join(column, "\n"))
						}
						got := ansi.StringWidth(row[:at+1])
						if want < 0 {
							want = got
						}
						if got != want {
							t.Errorf("frame %d: %q starts at column %d, %q at %d:\n%s",
								frame, name, got, names[0], want, strings.Join(column, "\n"))
						}
					}
				}
			})
		}
	}
}

// TestSidePanelSpinnerCarriesTheStatusLineColour pins the running row's lead under every style:
// with `ui.spinner-color` on it is the status line's own frame in the colour loop's foreground for
// the frame, on no background; with it off it is the bare glyph, as the status line's is.
func TestSidePanelSpinnerCarriesTheStatusLineColour(t *testing.T) {
	t.Parallel()

	for style := range spinnerSpecs {
		for _, color := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/color=%v", style, color), func(t *testing.T) {
				t.Parallel()

				m := spinnerPanelModel(t, style, domain.GlyphsUnicode, color)
				m.spin.frame = 7
				row := m.panelSubAgentRow(sidepanel.SubAgentRow{Name: "scout", Phase: sidepanel.PhaseRunning}, false)
				lead := strings.TrimPrefix(row[:strings.Index(row, "scout")], bodyIndent)

				glyph := m.spin.drawn(m.th, style)
				want := glyph
				if color {
					want = lipgloss.NewStyle().Foreground(m.th.spinnerColor(m.spin.frame, m.spin.framesPerColorLoop(style))).Render(glyph)
					if want == glyph {
						t.Fatal("setup: the coloured glyph carries no escape")
					}
				}
				if !strings.HasPrefix(lead, want) {
					t.Errorf("running lead = %q, want it to open with %q", lead, want)
				}
				if strings.Contains(lead, "[48;") || strings.Contains(lead, ";48;") {
					t.Errorf("running lead = %q carries a background", lead)
				}
				if !color && lead != ansi.Strip(lead) {
					t.Errorf("uncoloured running lead = %q, want the bare glyph", lead)
				}
			})
		}
	}
}

// panelGate is run id's approval Event at phase: a requested gate, or a human's decision on one.
func panelGate(id string, phase domain.ApprovalPhase, decision domain.ApprovalDecision) domain.ApprovalEvent {
	return domain.ApprovalEvent{
		EventBase: delegationBase(id),
		Phase:     phase,
		Request:   domain.ApprovalRequest{Tool: "write_file", Reason: "it overwrites a tracked file"},
		Decision:  decision,
	}
}

// panelRowOf is the painted panel row that names name, or "" when none does.
func panelRowOf(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, row := range panelText(t, m) {
		if strings.Contains(row, " "+name+" ") {
			return row
		}
	}
	return ""
}

// assertWaiting checks the row naming name reads waiting — the glyph set's waiting mark in place of
// its phase glyph, and the phase word `waiting` — or, when isWaiting is false, neither.
func assertWaiting(t *testing.T, m Model, name string, isWaiting bool) {
	t.Helper()
	row := panelRowOf(t, m, name)
	if row == "" {
		t.Fatalf("no panel row names %q", name)
	}
	marked := strings.HasPrefix(strings.TrimLeft(row, " "), m.th.glyphs.waiting+" "+name) && strings.Contains(row, " waiting ")
	if marked != isWaiting || strings.Contains(row, " waiting ") != isWaiting {
		t.Errorf("row %q: waiting = %v, want %v", row, marked, isWaiting)
	}
}

// TestSidePanelTwoGatedSubAgentsReadWaiting pins ADR 0111 decision 11's paint: with two sub-agents
// gated at once — one approval showing, the other queued behind it — both rows read `waiting` under
// the waiting mark, in the warning tone and on no background of its own; each row clears to
// `running` on its own run's decision alone, the queued one first here.
func TestSidePanelTwoGatedSubAgentsReadWaiting(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		panelDelegationCall(t, "r1", "planner"), delegationPhase("r1", domain.SubAgentStarted, time.Minute),
		panelDelegationCall(t, "r2", "repo-scout"), delegationPhase("r2", domain.SubAgentStarted, 30*time.Second),
		panelGate("r1", domain.ApprovalRequested, ""), panelGate("r2", domain.ApprovalRequested, ""))
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "write_file", Reason: "it overwrites a tracked file", CacheKey: ordinaryGateKey},
		Reply:   make(chan domain.ApprovalDecision, 1),
	})
	if m.state != stateAwaitingApproval {
		t.Fatalf("setup: state = %v, want r1's approval showing", m.state)
	}

	assertWaiting(t, m, "planner", true)
	assertWaiting(t, m, "repo-scout", true)
	mark := m.th.panelWaiting.Render(m.th.glyphs.waiting)
	if column := strings.Join(m.sidePanelColumn(m.viewport.Height()), "\n"); !strings.Contains(column, mark) {
		t.Errorf("the column paints no waiting mark in the panelWaiting style %q", mark)
	}
	if _, isNone := m.th.panelWaiting.GetBackground().(lipgloss.NoColor); !isNone {
		t.Error("panelWaiting carries a background; the mark takes the panel's own field")
	}
	if got, want := m.th.panelWaiting.GetForeground(), m.th.statusWarning.GetForeground(); got != want {
		t.Errorf("panelWaiting foreground = %v, want the warning role's %v", got, want)
	}

	m = m.foldEvent(panelGate("r2", domain.ApprovalDecided, domain.ApprovalAllow))
	assertWaiting(t, m, "planner", true)
	assertWaiting(t, m, "repo-scout", false)
	if row := panelRowOf(t, m, "repo-scout"); !strings.Contains(row, " running ") {
		t.Errorf("the decided row reads %q, want it back to running", row)
	}

	m = m.foldEvent(panelGate("r1", domain.ApprovalDecided, domain.ApprovalDeny))
	assertWaiting(t, m, "planner", false)
	assertWaiting(t, m, "repo-scout", false)
}

// TestSidePanelWaitingClearsOnCancel pins the ways a gate ends without a decision: the Approver's
// failure on it (a cancelled pane), the run finishing — a stopped delegation finishes failed, and
// its row then reads failed, never waiting — and the top-level Turn ending under a cancel. The
// other gated run keeps its mark until its own gate closes.
func TestSidePanelWaitingClearsOnCancel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		end       domain.Event
		wantWord  string
		wantOther bool
	}{
		{
			name: "the approver's failure on the gate",
			end: domain.ErrorEvent{EventBase: delegationBase("r1"), Source: "loop",
				Err: domain.ApproverErrorPrefix + "prompt cancelled"},
			wantWord: " running ", wantOther: true,
		},
		{
			name: "the stopped run finishing",
			end: domain.SubAgentPhaseEvent{EventBase: delegationBase("r1"), Phase: domain.SubAgentFinished,
				Result: domain.ToolResult{IsError: true}, At: panelNow},
			wantWord: " failed ", wantOther: true,
		},
		{
			name: "the cancelled top-level Turn ending", end: domain.TurnEvent{},
			wantWord: " running ", wantOther: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := panelModel(t, domain.DefaultSidePanelSections, 30,
				panelDelegationCall(t, "r1", "planner"), delegationPhase("r1", domain.SubAgentStarted, time.Minute),
				panelDelegationCall(t, "r2", "repo-scout"), delegationPhase("r2", domain.SubAgentStarted, time.Minute),
				panelGate("r1", domain.ApprovalRequested, ""), panelGate("r2", domain.ApprovalRequested, ""))
			assertWaiting(t, m, "planner", true)

			m = m.foldEvent(tc.end)

			assertWaiting(t, m, "planner", false)
			if row := panelRowOf(t, m, "planner"); !strings.Contains(row, tc.wantWord) {
				t.Errorf("the ended gate's row reads %q, want %q", row, strings.TrimSpace(tc.wantWord))
			}
			assertWaiting(t, m, "repo-scout", tc.wantOther)
		})
	}
}

// TestSidePanelRepaintsWhenTheSubAgentsLingerEnds pins the repaint armed for the end of the
// sub-agents section's linger: each Event that moves the deadline arms one tick for it on the
// panel's clock and retires the one before, a tick from a retired arm changes nothing, and the
// current tick — delivered by hand, never run — repaints an otherwise idle frame without the section.
func TestSidePanelRepaintsWhenTheSubAgentsLingerEnds(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30)
	clock := panelNow
	m.panel = sidepanel.New(func() time.Time { return clock })
	event := func(e domain.Event) tea.Cmd {
		t.Helper()
		var cmd tea.Cmd
		m, cmd = stepCmd(t, m, eventMsg{Event: e})
		return cmd
	}
	at := func(id string, p domain.SubAgentPhase, offset time.Duration) domain.SubAgentPhaseEvent {
		return domain.SubAgentPhaseEvent{EventBase: delegationBase(id), Phase: p, At: panelNow.Add(offset)}
	}
	paintsSubAgents := func() bool {
		return strings.Contains(strings.Join(panelText(t, m), "\n"), "Sub-agents")
	}

	event(panelDelegationCall(t, "r1", "planner"))
	event(at("r1", domain.SubAgentStarted, -testtime.Fixed(time.Minute)))
	if !m.panelLingerUntil.IsZero() {
		t.Fatalf("a running delegation armed a linger for %v", m.panelLingerUntil)
	}
	if cmd := event(at("r1", domain.SubAgentFinished, -testtime.Fixed(10*time.Second))); cmd == nil {
		t.Fatal("the last delegation's end armed no repaint")
	}
	if want := panelNow.Add(testtime.Fixed(20 * time.Second)); !m.panelLingerUntil.Equal(want) {
		t.Fatalf("armed linger = %v, want %v", m.panelLingerUntil, want)
	}
	retired := m.panelLingerGen

	event(panelDelegationCall(t, "r2", "repo-scout"))
	if !m.panelLingerUntil.IsZero() || m.panelLingerGen == retired {
		t.Fatalf("a queued delegation left the linger armed at %v (gen %d)", m.panelLingerUntil, m.panelLingerGen)
	}
	event(at("r2", domain.SubAgentStarted, -testtime.Fixed(5*time.Second)))
	event(at("r2", domain.SubAgentFinished, 0))
	deadline := panelNow.Add(testtime.Fixed(30 * time.Second))
	if !m.panelLingerUntil.Equal(deadline) {
		t.Fatalf("re-armed linger = %v, want %v", m.panelLingerUntil, deadline)
	}
	if left := m.panel.SubAgentLinger().Left; left != testtime.Fixed(30*time.Second) {
		t.Errorf("the panel answers %v left, want 30s on its own clock", left)
	}
	gen := m.panelLingerGen

	clock = panelNow.Add(testtime.Fixed(25 * time.Second))
	m, cmd := stepCmd(t, m, panelLingerMsg{gen: retired})
	if cmd != nil || m.panelLingerGen != gen || !m.panelLingerUntil.Equal(deadline) {
		t.Errorf("a retired tick moved the arm to %v (gen %d), want %v (gen %d)", m.panelLingerUntil, m.panelLingerGen, deadline, gen)
	}
	if !paintsSubAgents() {
		t.Errorf("the column dropped the sub-agents section inside its linger:\n%s", strings.Join(panelText(t, m), "\n"))
	}

	clock = deadline.Add(testtime.Fixed(time.Second))
	m, cmd = stepCmd(t, m, panelLingerMsg{gen: gen})
	if cmd != nil || !m.panelLingerUntil.IsZero() {
		t.Errorf("the spent tick left the linger armed at %v (cmd %v)", m.panelLingerUntil, cmd != nil)
	}
	if paintsSubAgents() {
		t.Errorf("the repaint at the linger's end still paints the sub-agents section:\n%s", strings.Join(panelText(t, m), "\n"))
	}
}

// TestSidePanelEarlyLingerTickArmsTheRemainder pins the guard against a tick that lands before the
// panel's clock reaches the deadline: it arms the time still left instead of leaving the section up.
func TestSidePanelEarlyLingerTickArmsTheRemainder(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30)
	clock := panelNow
	m.panel = sidepanel.New(func() time.Time { return clock })
	for _, e := range []domain.Event{
		panelDelegationCall(t, "r1", "planner"),
		delegationPhase("r1", domain.SubAgentStarted, testtime.Fixed(time.Minute)),
		delegationPhase("r1", domain.SubAgentFinished, 0),
	} {
		m, _ = stepCmd(t, m, eventMsg{Event: e})
	}
	gen := m.panelLingerGen

	clock = panelNow.Add(testtime.Fixed(29 * time.Second))
	m, cmd := stepCmd(t, m, panelLingerMsg{gen: gen})

	if cmd == nil || m.panelLingerGen == gen || !m.panelLingerUntil.Equal(panelNow.Add(testtime.Fixed(30*time.Second))) {
		t.Errorf("an early tick armed %v (gen %d, cmd %v), want the same deadline under a new generation",
			m.panelLingerUntil, m.panelLingerGen, cmd != nil)
	}
	if left := m.panel.SubAgentLinger().Left; left != testtime.Fixed(time.Second) {
		t.Errorf("the panel answers %v left, want the 1s remainder", left)
	}
}

// TestSidePanelSectionModes pins the three modes: a pinned section shows with nothing live and
// wears ●, a hidden one never shows however live it is, and an auto one shows only while live.
func TestSidePanelSectionModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		sections domain.SidePanelSections
		events   []domain.Event
		want     []string
	}{
		{
			name:     "pinned with nothing live",
			sections: "[usage: pinned]",
			want:     []string{"● Usage"},
		},
		{
			name:     "hidden while live",
			sections: "[tasks: hidden]",
			events:   []domain.Event{taskList(0, "one")},
			want:     []string{sidePanelEmpty},
		},
		{
			name:     "auto while not live",
			sections: "[tasks: auto]",
			events:   []domain.Event{taskList(1, "one")},
			want:     []string{sidePanelEmpty},
		},
		{
			name:     "list order, not pin order",
			sections: "[tasks: auto, usage: pinned]",
			events:   []domain.Event{taskList(0, "one")},
			want:     []string{"○ Tasks (0/1)", "  ▸ one", "", "● Usage"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := panelModel(t, tc.sections, 30, tc.events...)

			if got, want := panelText(t, m), belowPanelTopRow(tc.want...); !slices.Equal(got, want) {
				t.Errorf("the column paints\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// TestSidePanelOverflowFoldsToHeaders pins the overflow order: pinned sections take rows first,
// then auto ones in list order; a section whose body does not fit folds to its header row, and
// when not even every header fits the last ones are left out behind `+N more`.
func TestSidePanelOverflowFoldsToHeaders(t *testing.T) {
	t.Parallel()

	events := func(t *testing.T) []domain.Event {
		return []domain.Event{
			taskList(0, "one", "two", "three"),
			panelDelegationCall(t, "r1", "scout"),
			landedEdit(domain.EventBase{}, "a.go", 0, 0),
			panelUsageReading(panelUsed),
		}
	}
	cases := []struct {
		name     string
		sections domain.SidePanelSections
		rows     int
		want     []string
	}{
		{
			name:     "a pinned section's body comes first",
			sections: "[sub-agents: auto, tasks: auto, changed-files: pinned, usage: hidden]",
			rows:     7,
			want: []string{
				"",
				"○ Sub-agents (1 queued)",
				"  ○ scout      queued",
				"○ Tasks (0/3)",
				"● Changed files (1)",
				"    a.go",
				"",
			},
		},
		{
			name:     "a later section that fits still expands",
			sections: "[tasks: auto, sub-agents: auto, changed-files: hidden, usage: hidden]",
			rows:     5,
			want: []string{
				"",
				"○ Tasks (0/3)",
				"○ Sub-agents (1 queued)",
				"  ○ scout      queued",
				"",
			},
		},
		{
			name:     "every header fits exactly",
			sections: domain.DefaultSidePanelSections,
			rows:     5,
			want: []string{
				"",
				"○ Sub-agents (1 queued)",
				"○ Tasks (0/3)",
				"○ Changed files (1)",
				"○ Usage (62%)",
			},
		},
		{
			name:     "headers that do not fit",
			sections: domain.DefaultSidePanelSections,
			rows:     4,
			want: []string{
				"",
				"○ Sub-agents (1 queued)",
				"○ Tasks (0/3)",
				"+2 more",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := panelModel(t, tc.sections, 30, events(t)...)

			if got := columnText(m, tc.rows); !slices.Equal(got, tc.want) {
				t.Errorf("a %d-row column paints\n%s\nwant\n%s", tc.rows, strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// squeezedPanelModel is a height-20 frame (13 column rows) whose sections, pinned changed-files
// first, need one row more than the column has: changed-files (2 files) is pinned, then
// sub-agents (2 runs), usage (3 rows) and tasks (3 tasks) are auto in that order, so the overflow
// squeezes tasks, the last auto section, to its header.
func squeezedPanelModel(t *testing.T) Model {
	t.Helper()
	return panelModel(t, "[changed-files: pinned, sub-agents: auto, usage: auto, tasks: auto, mcp: hidden]", 20,
		squeezedPanelEvents(t)...)
}

// squeezedPanelEvents is the Event stream [squeezedPanelModel] folds.
func squeezedPanelEvents(t *testing.T) []domain.Event {
	t.Helper()
	return panelEvents(panelTaskCard(), []domain.Event{
		panelDelegationCall(t, "r1", "scout"),
		panelDelegationCall(t, "r2", "miner"),
		landedEdit(domain.EventBase{}, "a.go", 1, 0),
		landedEdit(domain.EventBase{}, "b.go", 1, 0),
		panelUsageReading(panelUsed),
	})
}

// assertPanelGlyphs fails the test unless each titled header's fold indicator is the one want maps
// it to.
func assertPanelGlyphs(t *testing.T, m Model, want map[string]string) {
	t.Helper()
	for title, glyph := range want {
		if got := panelHeaderGlyph(t, m, title); got != glyph {
			t.Errorf("the %s header's edge holds %q, want %q:\n%s", title, got, glyph, strings.Join(panelText(t, m), "\n"))
		}
	}
}

// TestSidePanelOverflowMarksAndOpens drives the overflow's fold marks and the opened tier
// (docs/layout/side-panel-layout.md, "Overflow"): every header the overflow folded wears ▶ and
// every open one ▼; a click on a squeezed header opens it into the session's opened tier, which
// takes rows after the pinned sections and before the auto ones, squeezing the last auto section
// instead and writing nothing; a click on it again folds it, writes the fold and drops it from the
// tier; a section the human folded opens into the tier as it unfolds, so one click always shows a
// change; a new model forgets the tier; and a section with no body rows never reads ▶.
func TestSidePanelOverflowMarksAndOpens(t *testing.T) {
	t.Parallel()

	const collapsed = "▶" // the unicode glyph set's folded indicator

	t.Run("every squeezed header wears the folded mark", func(t *testing.T) {
		t.Parallel()
		events := func(t *testing.T) []domain.Event {
			return panelEvents(panelTaskCard(), []domain.Event{
				panelDelegationCall(t, "r1", "scout"),
				panelDelegationCall(t, "r2", "miner"),
				panelDelegationCall(t, "r3", "sifter"),
				landedEdit(domain.EventBase{}, "a.go", 1, 0),
				landedEdit(domain.EventBase{}, "b.go", 1, 0),
				landedEdit(domain.EventBase{}, "c.go", 1, 0),
				landedEdit(domain.EventBase{}, "d.go", 1, 0),
				panelUsageReading(panelUsed),
			})
		}
		m := panelModel(t, "[changed-files: pinned, tasks: auto, usage: auto, sub-agents: auto, mcp: hidden]", 20, events(t)...)

		assertPanelGlyphs(t, m, map[string]string{
			"Changed files": glyphExpanded, "Tasks": glyphExpanded, "Usage": collapsed, "Sub-agents": collapsed,
		})
	})

	t.Run("a squeezed section opens into the tier and folds out of it", func(t *testing.T) {
		t.Parallel()
		m := squeezedPanelModel(t)
		spy := &settingsSpy{}
		m.opts.Settings = spy.host(nil)
		assertPanelGlyphs(t, m, map[string]string{
			"Changed files": glyphExpanded, "Sub-agents": glyphExpanded, "Usage": glyphExpanded, "Tasks": collapsed,
		})

		m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, "Tasks"))

		assertPanelGlyphs(t, m, map[string]string{
			"Changed files": glyphExpanded, "Sub-agents": glyphExpanded, "Usage": collapsed, "Tasks": glyphExpanded,
		})
		panelRowY(t, m, "a.go")
		panelRowY(t, m, "read the plan")
		if len(spy.folds) != 0 || m.opts.UI.SidePanelFolded != "" || len(m.settingEdits) != 0 {
			t.Errorf("opening a squeezed section wrote folds %v, folded %q, journal %+v; want nothing written",
				spy.folds, m.opts.UI.SidePanelFolded, m.settingEdits)
		}

		restarted := newModel(context.Background(), &fakeEngine{}, m.opts, nil)
		restarted = step(t, restarted, tea.WindowSizeMsg{Width: 140, Height: 20})
		restarted.panel = sidepanel.New(func() time.Time { return panelNow })
		for _, e := range squeezedPanelEvents(t) {
			restarted = restarted.foldEvent(e)
		}
		if got := panelHeaderGlyph(t, restarted, "Tasks"); got != collapsed {
			t.Errorf("a new model's Tasks header holds %q, want the tier forgotten and %q:\n%s", got, collapsed, strings.Join(panelText(t, restarted), "\n"))
		}

		m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, "Tasks"))

		assertPanelGlyphs(t, m, map[string]string{"Usage": glyphExpanded, "Tasks": collapsed})
		if m.opts.UI.SidePanelFolded != "[tasks]" || !slices.Equal(spy.folds, []string{"tasks=true"}) {
			t.Errorf("folded %q, writes %v; want the fold written", m.opts.UI.SidePanelFolded, spy.folds)
		}
	})

	t.Run("a folded squeezed section opens with one click", func(t *testing.T) {
		t.Parallel()
		m := squeezedPanelModel(t)
		m.opts.UI.SidePanelFolded = "[tasks]"
		spy := &settingsSpy{}
		m.opts.Settings = spy.host(nil)

		m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, "Tasks"))

		assertPanelGlyphs(t, m, map[string]string{"Usage": collapsed, "Tasks": glyphExpanded})
		panelRowY(t, m, "read the plan")
		if m.opts.UI.SidePanelFolded != "" || !slices.Equal(spy.folds, []string{"tasks=false"}) {
			t.Errorf("folded %q, writes %v; want the unfold written", m.opts.UI.SidePanelFolded, spy.folds)
		}

		m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, "Tasks"))

		assertPanelGlyphs(t, m, map[string]string{"Usage": glyphExpanded, "Tasks": collapsed})
		if m.opts.UI.SidePanelFolded != "[tasks]" || !slices.Equal(spy.folds, []string{"tasks=false", "tasks=true"}) {
			t.Errorf("folded %q, writes %v; want the fold written again", m.opts.UI.SidePanelFolded, spy.folds)
		}
	})

	t.Run("+N more paints in the tool-marker role", func(t *testing.T) {
		t.Parallel()
		m := squeezedPanelModel(t)

		content := m.sidePanelContent(4)

		if want := m.th.toolMarker.Render("+2" + sidePanelMore); content[3] != want {
			t.Errorf("the 4-row column's last row is %q, want %q", content[3], want)
		}
	})

	t.Run("an empty body never reads folded", func(t *testing.T) {
		t.Parallel()
		m := panelModel(t, domain.DefaultSidePanelSections, 20, panelTaskCard()...)
		m, _ = sendPrompt(t, m, "/panel pin changed-files")

		if got := panelHeaderGlyph(t, m, "Changed files"); got != glyphExpanded {
			t.Errorf("the empty pinned section's edge holds %q, want %q", got, glyphExpanded)
		}
		m.opts.UI.SidePanelFolded = "[changed-files]"
		if got := panelHeaderGlyph(t, m, "Changed files"); got == collapsed {
			t.Errorf("the empty folded section's edge holds %q; a section with no body rows never reads it", got)
		}
	})
}

// alignedPanelModel builds a ready model whose column shows under the default sections, in a
// width × height window, with the spinner style and glyph set named explicitly, its panel on a
// clock fixed at panelNow and events folded through the view's own Event fold.
func alignedPanelModel(
	t *testing.T,
	style SpinnerStyle,
	glyphs domain.GlyphSet,
	width, height int,
	events ...domain.Event,
) Model {
	t.Helper()

	opts := testOpts
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	opts.UI.SidePanelSections = domain.DefaultSidePanelSections
	opts.UI.Spinner = style
	opts.UI.Glyphs = glyphs
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	m.panel = sidepanel.New(func() time.Time { return panelNow })
	for _, e := range events {
		m = m.foldEvent(e)
	}
	return m
}

// everySectionEvents makes every section of the default list live with body rows: a running
// sub-agent, a task card, a changed file of the top level and one of the sub-agent's, a usage
// reading with its split, and the MCP servers of [panelMCPEvents].
func everySectionEvents(t *testing.T) []domain.Event {
	t.Helper()
	return panelEvents(panelTaskCard(), []domain.Event{
		panelDelegationCall(t, "r1", "alpha"),
		delegationPhase("r1", domain.SubAgentStarted, time.Minute),
		landedEdit(domain.EventBase{}, "a.go", 1, 0),
		landedEdit(delegationBase("r1"), "b.go", 2, 1),
		panelUsageReading(panelUsed),
		standingCost(200, 100),
	}, panelMCPEvents())
}

// isPanelHeaderText reports whether row, as panelText reads it, is a section header: led by its pin
// glyph on the content's first cell, where a body row leads with its indent.
func isPanelHeaderText(row string) bool {
	return strings.HasPrefix(row, defaultGlyphs.panelOpen+" ") || strings.HasPrefix(row, defaultGlyphs.panelPinned+" ")
}

// TestSidePanelSpacesSectionsApart pins the spacing (docs/layout/side-panel-layout.md, "Rows and
// spacing"): on a column with room, the column's top row is blank, the first header paints on the
// row below it, one blank row precedes every section header after the first, and a click on the top
// row or a spacer is claimed and does nothing.
func TestSidePanelSpacesSectionsApart(t *testing.T) {
	t.Parallel()
	m := alignedPanelModel(t, SpinnerClassic, domain.GlyphsUnicode, 140, 40, everySectionEvents(t)...)

	column := panelText(t, m)
	if len(column) < 2 || column[0] != "" || !isPanelHeaderText(column[1]) {
		t.Fatalf("the column's top row is not blank above its first header:\n%s", strings.Join(column, "\n"))
	}
	if target, isPanel := m.panelTargetAt(panelBodyX(m), 0); !isPanel || target.kind != panelTargetNone {
		t.Errorf("a click on the blank top row follows %+v (column cell %v), want nothing", target, isPanel)
	}
	headers, spacers := 0, 0
	for i := 1; i < len(column); i++ {
		row := column[i]
		switch {
		case isPanelHeaderText(row):
			headers++
			if i > 0 && column[i-1] != "" {
				t.Errorf("header %q at row %d follows %q, want one blank row", row, i, column[i-1])
			}
			if i > 1 && column[i-2] == "" {
				t.Errorf("header %q at row %d follows two blank rows", row, i)
			}
		case row == "":
			spacers++
			if target, isPanel := m.panelTargetAt(panelBodyX(m), i); !isPanel || target.kind != panelTargetNone {
				t.Errorf("a click on the spacer at row %d follows %+v (column cell %v), want nothing", i, target, isPanel)
			}
		}
	}
	if headers != 5 || spacers != headers-1 {
		t.Errorf("the column paints %d headers and %d spacers, want 5 and 4:\n%s", headers, spacers, strings.Join(column, "\n"))
	}
}

// TestPanelAllocationDropsSpacersBeforeFolding pins the overflow's spacer budget: the spacers
// between sections take rows only after every body the column can show has them, so a column a row
// short of its spacers drops them all and folds nothing, and a column that squeezes a section paints
// no spacer though it has rows to spare — the human's own fold squeezes nothing.
func TestPanelAllocationDropsSpacersBeforeFolding(t *testing.T) {
	t.Parallel()

	body := func(n int) []string { return slices.Repeat([]string{"row"}, n) }
	sections := []panelSection{{body: body(2)}, {body: body(1)}, {body: body(3)}} // 3 headers, 6 body rows, 2 spacers
	cases := []struct {
		rows       int
		hasSpacers bool
		isExpanded []bool
	}{
		{rows: 11, hasSpacers: true, isExpanded: []bool{true, true, true}},
		{rows: 10, hasSpacers: false, isExpanded: []bool{true, true, true}},
		{rows: 9, hasSpacers: false, isExpanded: []bool{true, true, true}},
		{rows: 8, hasSpacers: false, isExpanded: []bool{true, true, false}},
	}
	folded := slices.Clone(sections)
	folded[2].isFolded = true
	if _, isExpanded, hasSpacers := panelAllocation(folded, 8); !hasSpacers || !slices.Equal(isExpanded, []bool{true, true, false}) {
		t.Errorf("8 rows, the last section folded by the human: expanded %v, spacers %v; want [true true false], true",
			isExpanded, hasSpacers)
	}
	for _, tc := range cases {
		kept, isExpanded, hasSpacers := panelAllocation(sections, tc.rows)
		if kept != len(sections) || hasSpacers != tc.hasSpacers || !slices.Equal(isExpanded, tc.isExpanded) {
			t.Errorf("%d rows: kept %d, expanded %v, spacers %v; want %d, %v, %v",
				tc.rows, kept, isExpanded, hasSpacers, len(sections), tc.isExpanded, tc.hasSpacers)
		}
		if lines := panelLayout(sections, tc.rows); len(lines) > tc.rows {
			t.Errorf("%d rows: the layout lays out %d lines", tc.rows, len(lines))
		}
	}

	m := squeezedPanelModel(t)
	if got := panelHeaderGlyph(t, m, "Tasks"); got != "▶" {
		t.Fatalf("setup: the squeezed Tasks header holds %q, want ▶", got)
	}
	if column := panelText(t, m); len(column) == 0 || column[0] != "" || slices.Contains(column[1:], "") {
		t.Errorf("a column with a squeezed section paints a spacer, or no blank top row:\n%s", strings.Join(column, "\n"))
	}
}

// TestSidePanelBodyTextColumnsAlign pins the panel-wide lead (docs/layout/side-panel-layout.md,
// "Rows and spacing"): under every spinner style and both glyph sets, the text of a body row of
// every section — a sub-agent's name, a task, a changed file, a usage row, an MCP server — starts in
// one column, the body indent and the lead cell and its blank right of the header's pin.
func TestSidePanelBodyTextColumnsAlign(t *testing.T) {
	t.Parallel()

	texts := []string{"alpha", "read the plan", "write the code", "a.go", "b.go", "sys ", "prompt ", "beta"}
	for _, glyphs := range []domain.GlyphSet{domain.GlyphsUnicode, domain.GlyphsSafe} {
		for style := range spinnerSpecs {
			t.Run(string(glyphs)+"/"+string(style), func(t *testing.T) {
				t.Parallel()
				m := alignedPanelModel(t, style, glyphs, 140, 40, everySectionEvents(t)...)

				want := len(bodyIndent) + m.panelLeadWidth() + 1
				column := panelText(t, m)
				for _, text := range texts {
					row := column[panelRowY(t, m, text)]
					if got := ansi.StringWidth(row[:strings.Index(row, text)]); got != want {
						t.Errorf("%q starts at column %d, want %d:\n%s", text, got, want, strings.Join(column, "\n"))
					}
				}
			})
		}
	}
}

// TestSidePanelRowsNearTheRoomLimitKeepTheirFacts pins the body lead's cost to a row's room: at the
// default width a changed file's path too long for its row is trimmed from the left behind its ↳
// lead and its `+N −M` stays whole at the edge, and an MCP server's name too long for its row is
// clipped so its state stays whole — neither row runs past the content.
func TestSidePanelRowsNearTheRoomLimitKeepTheirFacts(t *testing.T) {
	t.Parallel()
	m := alignedPanelModel(t, SpinnerSnake, domain.GlyphsUnicode, 170, 40,
		landedEdit(delegationBase("r1"), "internal/a/rather/deep/path/to/the/changed/file_name.go", 12, 4),
		domain.MCPServerEvent{Server: "a-server-whose-name-runs-well-past-the-room", Transport: "stdio", Phase: domain.MCPServerFailed},
	)
	if got := m.panelPaintWidth(); got != domain.DefaultSidePanelWidth {
		t.Fatalf("setup: the column paints %d wide, want the default %d", got, domain.DefaultSidePanelWidth)
	}

	files, _ := m.panelSection(domain.SidePanelChangedFiles)
	file := ansi.Strip(files.body[0])
	if !strings.HasPrefix(file, bodyIndent+defaultGlyphs.panelDelegated) || !strings.Contains(file, sidePanelPathElision) ||
		!strings.HasSuffix(file, " +12 −4") || ansi.StringWidth(file) != m.panelContentWidth() {
		t.Errorf("changed-file row %q: want ↳ …/path, `+12 −4` at the edge, %d cells", file, m.panelContentWidth())
	}
	servers, _ := m.panelSection(domain.SidePanelMCP)
	server := ansi.Strip(servers.body[0])
	if !strings.HasSuffix(server, "… failed") || ansi.StringWidth(server) != m.panelContentWidth() {
		t.Errorf("mcp row %q: want the name clipped and `failed` whole at the edge, %d cells", server, m.panelContentWidth())
	}
}

// TestSidePanelFactsPaintTheTranscriptOutcomeTones pins the panel's facts to the roles the
// transcript's outcome slot paints the same facts in ([summaryStyle]): a changed file's `+N −M` in
// `toolMarker`, a finished sub-agent's `done` in the success tone, and every failure — a failed
// sub-agent's `✗` and `failed`, a failed MCP server's `failed` — in the transcript's failure style,
// while an unfinished phase and the facts after a phase word stay faint.
func TestSidePanelFactsPaintTheTranscriptOutcomeTones(t *testing.T) {
	t.Parallel()
	m := panelModel(t, domain.DefaultSidePanelSections, 30)
	failure := summaryStyle(m.th, branchSummary{failed: true}, false)
	success := summaryStyle(m.th, branchSummary{succeeded: true}, false)

	cases := []struct {
		name    string
		painted string
		want    []string
	}{
		{
			name:    "changed file",
			painted: m.panelFileRow(sidepanel.FileRow{Path: "a.go", Stat: domain.DiffStat{Added: 12, Removed: 4}, HasStat: true}),
			want:    []string{m.th.toolMarker.Render("+12 −4")},
		},
		{
			name:    "done sub-agent",
			painted: m.panelSubAgentRow(sidepanel.SubAgentRow{Name: "alpha", Phase: sidepanel.PhaseDone, Elapsed: 38 * time.Second}, false),
			want: []string{
				success.Render("done") + m.th.statusFaint.Render(fmt.Sprintf(subAgentFactsFormat, "done", "38s", "")[len("done"):]),
			},
		},
		{
			name:    "failed sub-agent",
			painted: m.panelSubAgentRow(sidepanel.SubAgentRow{Name: "alpha", Phase: sidepanel.PhaseFailed}, false),
			want:    []string{failure.Render(m.th.glyphs.failure), failure.Render("failed")},
		},
		{
			name:    "running sub-agent",
			painted: m.panelSubAgentRow(sidepanel.SubAgentRow{Name: "alpha", Phase: sidepanel.PhaseRunning}, false),
			want:    []string{m.th.statusFaint.Render("running")},
		},
		{
			name:    "failed MCP server",
			painted: m.panelMCPRow(sidepanel.MCPServerRow{Server: "docs", Phase: domain.MCPServerFailed, Kept: true, Tools: 2}),
			want:    []string{failure.Render("failed") + m.th.statusFaint.Render(" · 2 tools")},
		},
	}
	for _, c := range cases {
		for _, want := range c.want {
			if !strings.Contains(c.painted, want) {
				t.Errorf("%s: row %q paints no %q", c.name, c.painted, want)
			}
		}
	}
}

// TestSidePanelDefaultWidthPaintsWholeRows pins the room the default width leaves past the body
// indent and lead: under every spinner style a 12-character sub-agent name and the usage split row
// `sys 12k · tools 13k · hist 40k` paint whole.
func TestSidePanelDefaultWidthPaintsWholeRows(t *testing.T) {
	t.Parallel()

	for style := range spinnerSpecs {
		t.Run(string(style), func(t *testing.T) {
			t.Parallel()
			m := alignedPanelModel(t, style, domain.GlyphsUnicode, 170, 40,
				panelDelegationCall(t, "r1", "twelve-chars"),
				delegationPhase("r1", domain.SubAgentStarted, time.Minute),
				panelUsageReading((12+13+40)*1024), // the split's coarse form counts in steps of 1024
				standingCost(12*1024, 13*1024),
			)
			if got := m.panelPaintWidth(); got != domain.DefaultSidePanelWidth {
				t.Fatalf("setup: the column paints %d wide, want the default %d", got, domain.DefaultSidePanelWidth)
			}

			column := strings.Join(panelText(t, m), "\n")
			for _, text := range []string{" twelve-chars ", "sys 12k · tools 13k · hist 40k"} {
				if !strings.Contains(column, text) {
					t.Errorf("the column paints no whole %q:\n%s", text, column)
				}
			}
		})
	}
}

// TestSidePanelViewedRunMarkMovesNoColumn pins the viewed run's mark (docs/layout/side-panel-layout.md,
// "Run view"): Run view's `▸` stands in the first cell of its row's body indent, so the viewed
// run's name starts in the column every other row's does, before and after the view opens.
func TestSidePanelViewedRunMarkMovesNoColumn(t *testing.T) {
	t.Parallel()
	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		panelDelegationCall(t, "r1", "alpha"),
		delegationPhase("r1", domain.SubAgentStarted, time.Minute),
		panelDelegationCall(t, "r2", "bravo"),
	)
	nameColumn := func(m Model, name string) int {
		row := panelRowOf(t, m, name)
		return ansi.StringWidth(row[:strings.Index(row, " "+name+" ")+1])
	}
	before := nameColumn(m, "alpha")

	m = openViewOf(t, m, delegationBase("r1"))

	if row := panelRowOf(t, m, "alpha"); !strings.HasPrefix(row, defaultGlyphs.panelCurrent+" ") {
		t.Errorf("the viewed row %q does not lead with %s in its body indent", row, defaultGlyphs.panelCurrent)
	}
	if got, other := nameColumn(m, "alpha"), nameColumn(m, "bravo"); got != before || other != before {
		t.Errorf("viewed name at column %d, other at %d; want both at %d, where it stood unviewed", got, other, before)
	}
}

// TestSidePanelClearEmptiesThePanel pins the /clear boundary: the panel the closed session folded
// falls with it, and the column reads `nothing live` again.
func TestSidePanelClearEmptiesThePanel(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30, taskList(0, "one"))

	m, _ = sendPrompt(t, m, "/clear")

	if m.panel != nil {
		t.Error("/clear kept the closed session's panel state")
	}
	if got := panelText(t, m); !slices.Equal(got, belowPanelTopRow(sidePanelEmpty)) {
		t.Errorf("after /clear the column paints %q, want only %q", got, sidePanelEmpty)
	}
}

// TestSidePanelFoldIsCopyOnWrite pins ADR 0011's rule for the panel's pointer: a fold into a copy of
// the Model that is then discarded leaves the kept Model's panel exactly as it was.
func TestSidePanelFoldIsCopyOnWrite(t *testing.T) {
	t.Parallel()

	kept := panelModel(t, domain.DefaultSidePanelSections, 30, taskList(0, "one"))
	before := panelText(t, kept)

	discarded := kept.foldEvent(taskList(1, "one", "two"))
	discarded = discarded.foldEvent(landedEdit(domain.EventBase{}, "a.go", 0, 0))

	if got := panelText(t, kept); !slices.Equal(got, before) {
		t.Errorf("a discarded copy's fold moved the kept panel to\n%s\nfrom\n%s", strings.Join(got, "\n"), strings.Join(before, "\n"))
	}
	if discarded.panel == kept.panel {
		t.Error("the fold wrote through the pointer the kept Model holds")
	}
}

// TestSidePanelAllocatesOnTheFirstEventItReads pins the nil start: a Model's panel is nil until an
// Event the panel reads arrives, an Event it does not read leaves it nil, and the first one it
// reads allocates it.
func TestSidePanelAllocatesOnTheFirstEventItReads(t *testing.T) {
	t.Parallel()

	m := newTestModel(t)
	m = m.foldEvent(domain.TokenEvent{Text: "hello"})
	if m.panel != nil {
		t.Fatal("an Event the panel does not read allocated its state")
	}

	m = m.foldEvent(taskList(0, "one"))

	if !m.panel.Tasks(sidepanel.RunKey{}).IsLive {
		t.Error("the first task list did not reach a freshly allocated panel")
	}
}

// runTasks is a task list the run base belongs to emitted, none of it done.
func runTasks(base domain.EventBase, texts ...string) domain.TaskListEvent {
	e := taskList(0, texts...)
	e.EventBase = base
	return e
}

// runUsage is a reading by the run base belongs to: used tokens of window (0 names none), a
// Maintenance one when isMaintenance.
func runUsage(base domain.EventBase, used, window int, isMaintenance bool) domain.UsageEvent {
	return domain.UsageEvent{
		EventBase: base, TotalTokens: used, ContextWindow: window, Maintenance: isMaintenance,
		Cumulative: domain.Usage{Calls: 1, PromptTokens: used, TotalTokens: used},
	}
}

// legacyDelegationCall is the parent's sub_agent call spawning a run recorded before run ids: the
// call carries no SpawnRunID, so its run is named by depth and the call id alone.
func legacyDelegationCall(t *testing.T, callID, name string) domain.ToolCallEvent {
	t.Helper()
	call := panelDelegationCall(t, "", name)
	call.Call.ID, call.SpawnRunID = callID, ""
	return call
}

// openViewOf opens Run view on the run base belongs to, as the transcript keys it, and fails the
// test unless the view's head is in the transcript.
func openViewOf(t *testing.T, m Model, base domain.EventBase) Model {
	t.Helper()
	m = m.openRun(runOf(base))
	if _, ok := m.viewedChild(); !ok {
		t.Fatalf("setup: no head answers the run %+v", base)
	}
	return m
}

// gaugeFigures is the status line's gauge as figures: its leading used/window, or "" with none shown.
func gaugeFigures(m Model) string {
	fields := strings.Fields(plainSlot(m.contextGauge()))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// TestSidePanelRunViewFollowsTheViewedRun pins ADR 0110 decision 12: while Run view shows a run,
// tasks, changed files and usage are that run's, and sub-agents still lists every run with the
// viewed one marked `▸`; backing out with esc gives the column back to the top level.
func TestSidePanelRunViewFollowsTheViewedRun(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		taskList(0, "top-level task"),
		landedEdit(domain.EventBase{}, "top.go", 1, 0),
		panelUsageReading(panelUsed),
		panelDelegationCall(t, "r1", "scout"),
		delegationPhase("r1", domain.SubAgentStarted, time.Minute),
		panelDelegationCall(t, "r2", "planner"),
		runTasks(delegationBase("r1"), "scout the parser"),
		landedEdit(delegationBase("r1"), "child.go", 2, 1),
		runUsage(delegationBase("r1"), 6000, 8192, false),
	)
	top := panelText(t, m)

	m = openViewOf(t, m, delegationBase("r1"))

	got := panelText(t, m)
	figures := format.Tokens(6000) + "/" + format.Tokens(8192)
	for _, want := range []string{"▸ scout the parser", "child.go", "Changed files (1)", "Usage (73%)", figures} {
		if !slices.ContainsFunc(got, func(row string) bool { return strings.Contains(row, want) }) {
			t.Errorf("r1's view paints no %q:\n%s", want, strings.Join(got, "\n"))
		}
	}
	for _, unwanted := range []string{"top-level task", "top.go"} {
		if slices.ContainsFunc(got, func(row string) bool { return strings.Contains(row, unwanted) }) {
			t.Errorf("r1's view paints the top level's %q:\n%s", unwanted, strings.Join(got, "\n"))
		}
	}
	if !slices.ContainsFunc(got, func(row string) bool { return strings.Contains(row, "▸ scout") }) ||
		!slices.ContainsFunc(got, func(row string) bool { return strings.Contains(row, "○ planner") }) {
		t.Errorf("sub-agents does not list every run with r1 marked:\n%s", strings.Join(got, "\n"))
	}
	if gauge := gaugeFigures(m); gauge != figures {
		t.Errorf("the gauge states %q in r1's view; want %q, the panel's figure", gauge, figures)
	}

	m = step(t, m, keyEsc())

	if m.inRunView() {
		t.Fatal("setup: esc did not leave the view")
	}
	if back := panelText(t, m); !slices.Equal(back, top) {
		t.Errorf("back at the top level the column paints\n%s\nwant the top level's\n%s",
			strings.Join(back, "\n"), strings.Join(top, "\n"))
	}
	if slices.ContainsFunc(top, func(row string) bool { return strings.Contains(row, "▸ scout") }) {
		t.Errorf("the top level marks a viewed run:\n%s", strings.Join(top, "\n"))
	}
}

// TestSidePanelRunViewUsageMatchesTheGauge pins the usage fill inside a view to the gauge's own
// figure ([Model.viewedFill]), on the two readings where a fold of its own could part from it: a
// Maintenance reading, which moves neither, and a reading naming no window, which both measure
// against the top-level window.
func TestSidePanelRunViewUsageMatchesTheGauge(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		reading domain.UsageEvent
		want    string
	}{
		{
			name:    "a Maintenance reading after a counted one",
			reading: runUsage(delegationBase("r1"), 9000, 16384, true),
			want:    format.Tokens(5000) + "/" + format.Tokens(16384),
		},
		{
			name:    "a reading naming no window",
			reading: runUsage(delegationBase("r1"), 7000, 0, false),
			want:    format.Tokens(7000) + "/" + format.Tokens(testOpts.ContextWindow),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := panelModel(t, "[usage: pinned]", 30,
				panelUsageReading(panelUsed),
				panelDelegationCall(t, "r1", "scout"),
				delegationPhase("r1", domain.SubAgentStarted, time.Minute),
				runUsage(delegationBase("r1"), 5000, 16384, false),
				tc.reading,
			)

			m = openViewOf(t, m, delegationBase("r1"))

			if gauge := gaugeFigures(m); gauge != tc.want {
				t.Fatalf("setup: the gauge states %q; want %q", gauge, tc.want)
			}
			got := panelText(t, m)
			if len(got) < 3 || !strings.HasPrefix(strings.TrimLeft(got[2], " "), tc.want+" ") {
				t.Errorf("the usage fill reads\n%s\nwant the gauge's %q", strings.Join(got, "\n"), tc.want)
			}
		})
	}
}

// TestSidePanelRunViewScopesALegacyRunByDepthAndSpawn pins a run recorded before run ids: its
// events carry no RunID, so its view and its folded state both name it by depth and spawning call,
// and a sibling at the same depth stays apart.
func TestSidePanelRunViewScopesALegacyRunByDepthAndSpawn(t *testing.T) {
	t.Parallel()

	first := domain.EventBase{Depth: 1, CallID: "call-a"}
	second := domain.EventBase{Depth: 1, CallID: "call-b"}
	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		legacyDelegationCall(t, "call-a", "first"),
		domain.SubAgentPhaseEvent{EventBase: first, Phase: domain.SubAgentStarted, At: panelNow},
		legacyDelegationCall(t, "call-b", "second"),
		domain.SubAgentPhaseEvent{EventBase: second, Phase: domain.SubAgentStarted, At: panelNow},
		runTasks(first, "first's task"),
		runTasks(second, "second's task"),
	)

	m = openViewOf(t, m, second)

	got := panelText(t, m)
	if !slices.ContainsFunc(got, func(row string) bool { return strings.Contains(row, "▸ second's task") }) {
		t.Errorf("the legacy run's view paints not its own task:\n%s", strings.Join(got, "\n"))
	}
	if slices.ContainsFunc(got, func(row string) bool { return strings.Contains(row, "first's task") }) {
		t.Errorf("the legacy run's view paints its sibling's task:\n%s", strings.Join(got, "\n"))
	}
	// A legacy call names no run, so the rows are unnamed: the second body row is the second run's.
	if len(got) < 4 || strings.Contains(got[2], defaultGlyphs.panelCurrent) || !strings.Contains(got[3], defaultGlyphs.panelCurrent) {
		t.Errorf("sub-agents does not mark the viewed legacy run alone:\n%s", strings.Join(got, "\n"))
	}
}

// panelToolPair is one finished call by base's run — its call, then its result — landing a write
// to target where target is not "".
func panelToolPair(base domain.EventBase, id, tool, args, target string) []domain.Event {
	return []domain.Event{
		domain.ToolCallEvent{EventBase: base, Call: domain.ToolCall{ID: id, Tool: tool, Arguments: []byte(args)}},
		domain.ToolResultEvent{
			EventBase: base, Tool: tool, WriteTarget: target,
			Result: domain.ToolResult{CallID: id, Content: "line one\nline two\nline three\nline four"},
		},
	}
}

// panelWrite is a finished write_file call by base's run, landing a write to path.
func panelWrite(base domain.EventBase, id, path string) []domain.Event {
	return panelToolPair(base, id, "write_file", `{"path":"`+path+`","content":"x\n"}`, path)
}

// panelTaskCard is a finished task_list call at the top level — the card the transcript draws —
// and the TaskListEvent the panel's tasks section reads from it.
func panelTaskCard() []domain.Event {
	return append(panelToolPair(domain.EventBase{}, "tl1", "task_list", string(taskListCall.Arguments), ""),
		taskList(1, "read the plan", "write the code", "run the tests"))
}

// panelEvents flattens groups of events into one stream.
func panelEvents(groups ...[]domain.Event) []domain.Event {
	return slices.Concat(groups...)
}

// panelRowY is the screen row the column paints the first row containing text on.
func panelRowY(t *testing.T, m Model, text string) int {
	t.Helper()
	at := slices.IndexFunc(panelText(t, m), func(row string) bool { return strings.Contains(row, text) })
	if at < 0 {
		t.Fatalf("the column paints no %q:\n%s", text, strings.Join(panelText(t, m), "\n"))
	}
	return at
}

// panelBodyX is a column a click on a panel body row lands on: inside the content, clear of the
// left chrome ([Model.panelLeftChrome]) and of a header's pin cell.
func panelBodyX(m Model) int { return m.slotWidth() + m.panelLeftChrome() + 2 }

// entryAt is the transcript index of the call id by run, failing the test where none is.
func entryAt(t *testing.T, m Model, run runRef, callID string) int {
	t.Helper()
	at := slices.IndexFunc(m.transcript.entries, func(e entry) bool {
		return e.kind == entryToolCall && e.callID == callID && e.run() == run
	})
	if at < 0 {
		t.Fatalf("no tool block %q in run %+v", callID, run)
	}
	return at
}

// entryOnScreen reports whether entries[at]'s first marked line is on a transcript row the frame
// shows.
func entryOnScreen(m Model, at int) bool {
	line := slices.IndexFunc(m.lineTargets, func(target lineTarget) bool {
		return target.kind != targetNone && target.entry == at
	})
	return line >= m.viewport.YOffset() && line < m.viewport.YOffset()+m.transcriptRows()
}

// settingsSpy is a settings host that records every write it is handed: the section and fold
// writes it answers with the list a fresh file would resolve to, any other write, and every reset.
type settingsSpy struct {
	sections []domain.SidePanelEntry
	folds    []string // one `<section>=<folded>` per fold write
	writes   []string
	resets   []string
}

// host is the spy as a SettingsHost, its section and fold writes failing with failure where one is
// given.
func (s *settingsSpy) host(failure error) fakeSettingsHost {
	return fakeSettingsHost{
		write: func(path, value string) error {
			s.writes = append(s.writes, path+"="+value)
			return nil
		},
		reset: func(path string) error {
			s.resets = append(s.resets, path)
			return nil
		},
		writeSection: func(section domain.SidePanelSection, mode domain.SidePanelMode) (domain.SidePanelSections, error) {
			s.sections = append(s.sections, domain.SidePanelEntry{Section: section, Mode: mode})
			if failure != nil {
				return "", failure
			}
			return withSectionMode(domain.DefaultSidePanelSections, section, mode), nil
		},
		writeFolded: func(section domain.SidePanelSection, folded bool) (domain.SidePanelFolded, error) {
			s.folds = append(s.folds, string(section)+"="+strconv.FormatBool(folded))
			if failure != nil {
				return "", failure
			}
			if !folded {
				return "", nil
			}
			return domain.SpellSidePanelFolded([]domain.SidePanelSection{section}), nil
		},
	}
}

// TestSidePanelClickOpensTheRowsDetail drives one click per body target (docs/layout/
// side-panel-layout.md, "Pointer"): a sub-agent row opens its Run view, a task row unfolds the
// task-list card and scrolls to it, a changed-file row unfolds that write's tool block and scrolls
// to it — opening the run that landed it, matched by run as well as call id — and a usage row
// opens /usage.
func TestSidePanelClickOpensTheRowsDetail(t *testing.T) {
	t.Parallel()

	events := panelEvents(
		panelTaskCard(),
		panelWrite(domain.EventBase{}, "w1", "/w/top.go"),
		[]domain.Event{
			panelUsageReading(panelUsed),
			panelDelegationCall(t, "r1", "scout"),
			delegationPhase("r1", domain.SubAgentStarted, time.Minute),
		},
		panelWrite(delegationBase("r1"), "w1", "/w/child.go"),
	)
	child := runOf(delegationBase("r1"))
	cases := []struct {
		name  string
		row   string
		check func(t *testing.T, m Model)
	}{
		{
			name: "sub-agent row opens its Run view",
			row:  "scout",
			check: func(t *testing.T, m Model) {
				if !m.inRunView() || m.viewedRun() != child {
					t.Errorf("viewed run = %+v (in view %v), want r1's view", m.viewedRun(), m.inRunView())
				}
			},
		},
		{
			name: "task row unfolds the card and scrolls to it",
			row:  "write the code",
			check: func(t *testing.T, m Model) {
				at := entryAt(t, m, runRef{}, "tl1")
				if !m.transcript.entries[at].expanded || !entryOnScreen(m, at) {
					t.Errorf("card expanded %v, on screen %v; want both", m.transcript.entries[at].expanded, entryOnScreen(m, at))
				}
			},
		},
		{
			name: "top-level file row unfolds its write's block",
			row:  "top.go",
			check: func(t *testing.T, m Model) {
				at := entryAt(t, m, runRef{}, "w1")
				if m.inRunView() || !m.transcript.entries[at].expanded || !entryOnScreen(m, at) {
					t.Errorf("in view %v, expanded %v, on screen %v; want the top level's block open and shown",
						m.inRunView(), m.transcript.entries[at].expanded, entryOnScreen(m, at))
				}
			},
		},
		{
			name: "delegated file row opens the run and unfolds its write's block",
			row:  "child.go",
			check: func(t *testing.T, m Model) {
				at, top := entryAt(t, m, child, "w1"), entryAt(t, m, runRef{}, "w1")
				if m.viewedRun() != child || !m.transcript.entries[at].expanded || !entryOnScreen(m, at) {
					t.Errorf("viewed %+v, expanded %v, on screen %v; want r1's block open in r1's view",
						m.viewedRun(), m.transcript.entries[at].expanded, entryOnScreen(m, at))
				}
				if m.transcript.entries[top].expanded {
					t.Error("the top level's w1 opened: the row's call id was matched without its run")
				}
			},
		},
		{
			name: "usage row opens /usage",
			row:  "cached ",
			check: func(t *testing.T, m Model) {
				if !m.usagePane.open {
					t.Error("the /usage report is not open")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := streamOneScreen(t, panelModel(t, domain.DefaultSidePanelSections, 30, events...))

			m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, tc.row))

			tc.check(t, m)
		})
	}
}

// panelStopEvents makes every section live with a target on each kind of body row: a task card, a
// top-level and a delegated write, a usage reading with several rows, and a started sub-agent.
func panelStopEvents(t *testing.T) []domain.Event {
	t.Helper()
	return panelEvents(
		panelTaskCard(),
		panelWrite(domain.EventBase{}, "w1", "/w/top.go"),
		[]domain.Event{
			panelUsageReading(panelUsed),
			panelDelegationCall(t, "r1", "scout"),
			delegationPhase("r1", domain.SubAgentStarted, time.Minute),
		},
		panelWrite(delegationBase("r1"), "w1", "/w/child.go"),
	)
}

// TestPanelStopsMatchClickTargets pins the stops to the pointer (ADR 0111 decisions 4–5): every
// stop's target is the click at every row of its span — a header's pin stop's on the pin glyph's
// cell, its title stop's and a body stop's inside the content — a row no stop spans clicks nothing,
// and the stops come in paint order with one identity per kind of section row, each header two (its
// pin, then its title: adr-0111-amend-1109b), a task card and the usage report each one stop over
// all their rows. The column's blank top row is no stop: the first header's stops sit on row 1.
func TestPanelStopsMatchClickTargets(t *testing.T) {
	t.Parallel()
	m := streamOneScreen(t, panelModel(t, domain.DefaultSidePanelSections, 30, panelStopEvents(t)...))
	pre := m.withFrameSpans()
	rows := pre.frameSpans().slotBottom

	stops := pre.panelStops(rows)

	isSpanned := make([]bool, rows)
	for _, stop := range stops {
		x := panelBodyX(pre)
		if stop.id.isPin {
			x = pre.panelPinX()
		}
		for y := stop.y; y < stop.y+stop.height; y++ {
			isSpanned[y] = true
			if got, isPanel := pre.panelTargetAt(x, y); !isPanel || got != stop.target {
				t.Errorf("stop %+v: the click at (%d, %d) is %+v (column %v), want %+v", stop.id, x, y, got, isPanel, stop.target)
			}
		}
	}
	for y, spanned := range isSpanned {
		if got, _ := pre.panelTargetAt(panelBodyX(pre), y); !spanned && got.kind != panelTargetNone {
			t.Errorf("row %d is no stop, yet a click there follows %+v", y, got)
		}
	}
	want := []panelStopID{
		{section: domain.SidePanelSubAgents, isHeader: true, isPin: true},
		{section: domain.SidePanelSubAgents, isHeader: true},
		{section: domain.SidePanelSubAgents, run: sidepanel.RunOf(delegationBase("r1"))},
		{section: domain.SidePanelTasks, isHeader: true, isPin: true},
		{section: domain.SidePanelTasks, isHeader: true},
		{section: domain.SidePanelTasks},
		{section: domain.SidePanelChangedFiles, isHeader: true, isPin: true},
		{section: domain.SidePanelChangedFiles, isHeader: true},
		{section: domain.SidePanelChangedFiles, path: pre.panelWriteKey("/w/top.go")},
		{section: domain.SidePanelChangedFiles, path: pre.panelWriteKey("/w/child.go")},
		{section: domain.SidePanelUsage, isHeader: true, isPin: true},
		{section: domain.SidePanelUsage, isHeader: true},
		{section: domain.SidePanelUsage},
	}
	got := make([]panelStopID, 0, len(stops))
	for _, stop := range stops {
		got = append(got, stop.id)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("stop identities\n%+v\nwant\n%+v", got, want)
	}
	if isSpanned[0] {
		t.Errorf("a stop spans the column's blank top row: %+v", stops[0])
	}
	if stops[0].y != 1 || stops[1].y != 1 {
		t.Errorf("the first header's stops sit on rows %d and %d, want row 1, below the blank top row", stops[0].y, stops[1].y)
	}
	firstRows := map[int]string{0: "Sub-agents", 3: "Tasks", 6: "Changed files", 10: "Usage"}
	for at, text := range firstRows {
		y := panelRowY(t, pre, text)
		if stops[at].y != y || stops[at+1].y != y {
			t.Errorf("the %s header's stops sit on rows %d and %d, painted on %d", text, stops[at].y, stops[at+1].y, y)
		}
	}
	if tasks := stops[5]; tasks.y != panelRowY(t, pre, "read the plan") || tasks.height != 3 {
		t.Errorf("the task card's stop spans rows %d+%d, want the three task rows from %d",
			tasks.y, tasks.height, panelRowY(t, pre, "read the plan"))
	}
	if usage := stops[12]; usage.height < 2 || !usage.spans(panelRowY(t, pre, "cached ")) {
		t.Errorf("the usage stop spans rows %d+%d: want several, the cache-hit row among them", usage.y, usage.height)
	}
}

// TestPanelStopsSkipTargetlessRows pins the rows that are no stop: the column's blank top row and the
// `+N more` row of an overflowing column, and a changed-file row whose write names no tool call to
// open.
func TestPanelStopsSkipTargetlessRows(t *testing.T) {
	t.Parallel()
	events := func(t *testing.T) []domain.Event {
		return []domain.Event{
			taskList(0, "one", "two", "three"),
			panelDelegationCall(t, "r1", "scout"),
			landedEdit(domain.EventBase{}, "a.go", 0, 0),
			panelUsageReading(panelUsed),
		}
	}

	t.Run("the +N more row", func(t *testing.T) {
		t.Parallel()
		m := panelModel(t, domain.DefaultSidePanelSections, 30, events(t)...)

		stops := m.panelStops(4)

		if column := columnText(m, 4); column[3] != "+2 more" {
			t.Fatalf("the 4-row column's last row reads %q, want the +2 more row", column[3])
		}
		want := []panelStop{
			{y: 1, height: 1, target: panelTarget{kind: panelTargetPin, section: domain.SidePanelSubAgents},
				id: panelStopID{section: domain.SidePanelSubAgents, isHeader: true, isPin: true}},
			{y: 1, height: 1, target: panelTarget{kind: panelTargetFold, section: domain.SidePanelSubAgents},
				id: panelStopID{section: domain.SidePanelSubAgents, isHeader: true}},
			{y: 2, height: 1, target: panelTarget{kind: panelTargetPin, section: domain.SidePanelTasks},
				id: panelStopID{section: domain.SidePanelTasks, isHeader: true, isPin: true}},
			{y: 2, height: 1, target: panelTarget{kind: panelTargetFold, section: domain.SidePanelTasks},
				id: panelStopID{section: domain.SidePanelTasks, isHeader: true}},
		}
		if !slices.Equal(stops, want) {
			t.Errorf("stops %+v, want the two headers' stops alone", stops)
		}
	})
	t.Run("a file row with no call", func(t *testing.T) {
		t.Parallel()
		m := panelModel(t, "[changed-files: pinned, sub-agents: hidden, tasks: hidden, usage: hidden]", 30, events(t)...)
		pre := m.withFrameSpans()

		stops := pre.panelStops(pre.frameSpans().slotBottom)

		if len(stops) != 2 || !stops[0].id.isHeader || !stops[1].id.isHeader {
			t.Errorf("stops %+v, want the changed-files header's two stops alone", stops)
		}
		if got, _ := pre.panelTargetAt(panelBodyX(pre), panelRowY(t, pre, "a.go")); got.kind != panelTargetNone {
			t.Errorf("a click on the call-less file row follows %+v, want nothing", got)
		}
	})
}

// TestSidePanelClickTaskRowWritesNoSetting pins the regression guard of the task row: it unfolds
// the one card through its own block state, never through the shared task-list fold, whose toggle
// writes `ui.task-list-open`.
func TestSidePanelClickTaskRowWritesNoSetting(t *testing.T) {
	t.Parallel()
	m := panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...)
	spy := &settingsSpy{}
	m.opts.Settings = spy.host(nil)
	m.refreshViewport()
	wasOpen := m.opts.UI.TaskListOpen

	m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, "run the tests"))

	if len(spy.writes) != 0 || len(spy.sections) != 0 || len(m.settingEdits) != 0 {
		t.Errorf("a task-row click wrote %v / %v (journal %v); want nothing", spy.writes, spy.sections, m.settingEdits)
	}
	if m.opts.UI.TaskListOpen != wasOpen {
		t.Errorf("ui.task-list-open moved to %v", m.opts.UI.TaskListOpen)
	}
}

// TestSidePanelClickHeaderTitleBlankAndWheelDoNothing pins the cells that target nothing: a blank
// cell below the sections and a wheel notch over the column — a header row's included — leave the
// model as it was — no view opened, no section toggled, the transcript not scrolled. A click on the
// header title folds the section (TestSidePanelFoldTogglesAndWritesBack).
func TestSidePanelClickHeaderTitleBlankAndWheelDoNothing(t *testing.T) {
	t.Parallel()
	m := streamOneScreen(t, panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...))
	m = step(t, m, tea.MouseWheelMsg{X: 4, Y: 2, Button: tea.MouseWheelUp}) // off the tail, so a notch could move it
	offset, sections := m.viewport.YOffset(), m.opts.UI.SidePanelSections
	header := panelRowY(t, m, "Tasks")

	m = clickCell(t, m, panelBodyX(m), m.transcriptRows()-1)
	m = step(t, m, tea.MouseWheelMsg{X: panelBodyX(m), Y: header, Button: tea.MouseWheelUp})

	if m.viewport.YOffset() != offset || m.opts.UI.SidePanelSections != sections || m.inRunView() ||
		m.opts.UI.SidePanelFolded != "" {
		t.Errorf("offset %d→%d, sections %q→%q, folded %q, in view %v; want all unchanged",
			offset, m.viewport.YOffset(), sections, m.opts.UI.SidePanelSections, m.opts.UI.SidePanelFolded, m.inRunView())
	}
	if m.transcriptSel.active || m.sel.active {
		t.Error("a click on the column armed a selection")
	}
}

// TestSidePanelClickHoverShapeMatches pins the pointer shape over the column to what a click there
// does: the hand over a body row, the pin glyph and a header title (which folds), the arrow over a
// blank cell.
func TestSidePanelClickHoverShapeMatches(t *testing.T) {
	t.Parallel()
	m := panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...)
	m.refreshViewport()
	pre := m.withFrameSpans()
	header := panelRowY(t, m, "Tasks")
	cases := []struct {
		name string
		x, y int
		want pointerShape
	}{
		{"body row", panelBodyX(m), panelRowY(t, m, "read the plan"), shapePointer},
		{"pin glyph", m.panelPinX(), header, shapePointer},
		{"header title", panelBodyX(m), header, shapePointer},
		{"blank cell", panelBodyX(m), m.transcriptRows() - 1, shapeDefault},
	}
	for _, tc := range cases {
		if got := m.pointerShapeAt(pre, tc.x, tc.y); got != tc.want {
			t.Errorf("%s: shape %q, want %q", tc.name, got.name(), tc.want.name())
		}
	}
}

// TestSidePanelClickTakesNoApprovalDecision pins the panel hit before the panes: the approval
// pane's hits are y-only, so two clicks on the column beside its Deny row must take no decision.
func TestSidePanelClickTakesNoApprovalDecision(t *testing.T) {
	t.Parallel()
	reply := make(chan domain.ApprovalDecision, 1)
	m := streamOneScreen(t, panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...))
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "write_file", Reason: "it overwrites a tracked file", CacheKey: ordinaryGateKey},
		Reply:   reply,
	})
	if m.state != stateAwaitingApproval {
		t.Fatalf("setup: state = %v, want the call waiting on the human", m.state)
	}
	m = armApproval(t, m)
	_, y := frameCell(t, m, "Deny")

	m = clickCell(t, clickCell(t, m, m.width-3, y), m.width-3, y)

	if decision, sent := sentDecision(t, reply); sent {
		t.Errorf("two clicks on the column beside Deny ruled %q", decision)
	}
	if m.state != stateAwaitingApproval {
		t.Errorf("state = %v, want the call still waiting", m.state)
	}
}

// TestSidePanelClickRefusedUnderModalPanes pins the panel's targets inert under a modal pane (ADR
// 0111): with an approval, an ask or the /settings pane up, a click on a sub-agent row, a usage row
// and a pin glyph is claimed and changes nothing — no Run view, no report, no section write, the
// pane still up — the hover over each reads the default arrow, and a press on the grip still starts
// a resize drag.
func TestSidePanelClickRefusedUnderModalPanes(t *testing.T) {
	t.Parallel()

	events := panelEvents(panelTaskCard(), []domain.Event{
		panelUsageReading(panelUsed),
		panelDelegationCall(t, "r1", "scout"),
		delegationPhase("r1", domain.SubAgentStarted, time.Minute),
	})
	cases := []struct {
		name string
		open func(t *testing.T, m Model) Model
	}{
		{
			name: "approval",
			open: func(t *testing.T, m Model) Model {
				m = step(t, streamOneScreen(t, m), approvalReqMsg{
					Request: domain.ApprovalRequest{Tool: "write_file", Reason: "it overwrites a tracked file", CacheKey: ordinaryGateKey},
					Reply:   make(chan domain.ApprovalDecision, 1),
				})
				return armApproval(t, m)
			},
		},
		{
			name: "ask",
			open: func(t *testing.T, m Model) Model {
				m = step(t, streamOneScreen(t, m), askReqMsg{
					Request: domain.AskRequest{Question: "which way?", Choices: []string{"left", "right"}},
					Reply:   make(chan domain.AskAnswer, 1),
				})
				return armApproval(t, m)
			},
		},
		{
			name: "settings",
			open: func(t *testing.T, m Model) Model {
				return openSettingsPane(t, m)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := panelModel(t, domain.DefaultSidePanelSections, 30, events...)
			spy := &settingsSpy{}
			host := spy.host(nil)
			rows := settingsTestRows(6)
			host.rows = func() []SettingRow { return rows }
			m.opts.Settings = host
			m = tc.open(t, m)
			if !m.modalPaneOpen() {
				t.Fatal("setup: no modal pane is open")
			}
			pre := m.withFrameSpans()
			targets := []struct {
				name string
				x, y int
			}{
				{"sub-agent row", panelBodyX(m), panelRowY(t, m, "scout")},
				{"usage row", panelBodyX(m), panelRowY(t, m, "cached ")},
				{"pin glyph", m.panelPinX(), panelRowY(t, m, "Tasks")},
				{"header title", panelBodyX(m), panelRowY(t, m, "Tasks")},
			}
			for _, cell := range targets {
				if got := m.pointerShapeAt(pre, cell.x, cell.y); got != shapeDefault {
					t.Errorf("hover over the %s: shape %q, want the default arrow", cell.name, got.name())
				}
			}
			sections := m.opts.UI.SidePanelSections

			for _, cell := range targets {
				m = clickCell(t, m, cell.x, cell.y)
			}

			if m.inRunView() || m.usagePane.open || m.opts.UI.SidePanelSections != sections || m.opts.UI.SidePanelFolded != "" {
				t.Errorf("in view %v, /usage open %v, sections %q→%q, folded %q; want all unchanged",
					m.inRunView(), m.usagePane.open, sections, m.opts.UI.SidePanelSections, m.opts.UI.SidePanelFolded)
			}
			if len(spy.writes) != 0 || len(spy.sections) != 0 || len(spy.folds) != 0 || len(m.settingEdits) != 0 {
				t.Errorf("clicks wrote %v / %v / %v (journal %v); want nothing", spy.writes, spy.sections, spy.folds, m.settingEdits)
			}
			if !m.modalPaneOpen() {
				t.Error("a click on the column closed the modal pane")
			}

			m = step(t, m, leftClick(m.slotWidth(), separatorRow))

			if !m.panelDrag.active {
				t.Error("a press on the grip started no resize drag under the modal pane")
			}
		})
	}
}

// TestSidePanelPinClickRefusedInWatchMode pins the pin glyph read-only while watching (ADR 0111):
// a click there flashes the watch refusal and toggles and writes nothing, and the hover over it
// reads the default arrow.
func TestSidePanelPinClickRefusedInWatchMode(t *testing.T) {
	t.Parallel()
	m := enterWatchOn(t, watchEngine(), newWatchFixture(t, "watched", nil, 0))
	m.opts.UI.SidePanelShow = domain.SidePanelShowAuto
	m.opts.UI.SidePanelSections = "[tasks: pinned]"
	spy := &settingsSpy{}
	m.opts.Settings = spy.host(nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	x, y := m.panelPinX(), panelRowY(t, m, "Tasks")
	if got := m.pointerShapeAt(m.withFrameSpans(), x, y); got != shapeDefault {
		t.Errorf("hover over the watched pin: shape %q, want the default arrow", got.name())
	}
	before := m.opts.UI

	m = clickCell(t, m, x, y)

	if m.flash != watchRefusedFlash {
		t.Errorf("flash %q, want the watch refusal", m.flash)
	}
	if m.opts.UI != before || len(spy.sections) != 0 || len(spy.writes) != 0 || len(m.settingEdits) != 0 {
		t.Errorf("settings %+v, writes %v / %v (journal %v); want %+v kept and nothing written",
			m.opts.UI, spy.sections, spy.writes, m.settingEdits, before)
	}
}

// TestSidePanelClickBesideABlockHeaderLeavesIt pins the column's claim on every cell: a click on a
// blank panel cell on the row of a tool block's header does not reach the block.
func TestSidePanelClickBesideABlockHeaderLeavesIt(t *testing.T) {
	t.Parallel()
	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		panelToolPair(domain.EventBase{}, "r1", "terminal", `{"command":"go test ./..."}`, "")...)
	m.refreshViewport()
	at := entryAt(t, m, runRef{}, "r1")
	line := slices.IndexFunc(m.lineTargets, func(target lineTarget) bool { return target.entry == at && target.kind != targetNone })
	if line < 0 {
		t.Fatal("setup: the read_file block marks no click surface")
	}
	y := screenRow(t, m, line)
	if rows := panelText(t, m); y < len(rows) && strings.TrimSpace(rows[y]) != "" {
		t.Fatalf("setup: the column paints %q beside the header; want a blank cell", rows[y])
	}
	before := expandedFlags(m)

	m = clickCell(t, m, m.width-3, y)

	if got := expandedFlags(m); !slices.Equal(got, before) {
		t.Errorf("expanded %v → %v: the click beside the header reached the block", before, got)
	}
}

// TestSafePanelFrameDrawsOnlySafeGlyphs pins the panel to the glyph set (panel-glyphs-skip-safe):
// under `ui.glyphs: safe` a populated column — a pinned and three auto sections, a running and a
// queued sub-agent, a done, a current and an open task, a delegated write, and a usage reading with
// its bar — paints `•` pinned, `○` auto and queued, `>` for the current task and `»` for the
// delegated write, and every other rune it paints is ASCII, a safe-set glyph, or one of the shapes
// the manual's `ui.glyphs` list names as keeping their form: the open fold's ▼, the │ separator, the
// navigation cursor's ▌ and the gauge's block cells. The `−` of a diff count and the `…` of a cut
// are punctuation the transcript shares, not marks.
func TestSafePanelFrameDrawsOnlySafeGlyphs(t *testing.T) {
	t.Parallel()
	opts := testOpts
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	opts.UI.SidePanelSections = "[sub-agents: auto, tasks: auto, changed-files: auto, usage: pinned]"
	opts.UI.Glyphs = domain.GlyphsSafe
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m.panel = sidepanel.New(func() time.Time { return panelNow })
	for _, e := range panelEvents(
		[]domain.Event{
			panelDelegationCall(t, "r1", "alpha"),
			delegationPhase("r1", domain.SubAgentStarted, time.Minute),
			panelDelegationCall(t, "r2", "bravo"),
			taskList(1, "draft", "test", "ship"),
			landedEdit(domain.EventBase{}, "top.go", 3, 1),
			panelUsageReading(panelUsed),
		},
		panelWrite(delegationBase("r1"), "w1", "child.go"),
	) {
		m = m.foldEvent(e)
	}

	slot := m.slotWidth()
	var column []string
	for _, ln := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if strings.HasPrefix(ln, "▔") {
			break
		}
		column = append(column, ansi.Cut(ln, slot, m.width))
	}
	painted := strings.Join(column, "\n")

	allowed := map[rune]bool{'−': true, '…': true, '█': true}
	safe := reflect.ValueOf(safeGlyphs)
	for i := range safe.NumField() {
		if safe.Field(i).Kind() == reflect.String {
			for _, r := range safe.Field(i).String() {
				allowed[r] = true
			}
		}
	}
	for _, exception := range []string{glyphExpanded, sidePanelSeparator, glyphCursorMark, string(gaugeEighths)} {
		for _, r := range exception {
			allowed[r] = true
		}
	}
	for _, r := range painted {
		if r > unicode.MaxASCII && !allowed[r] {
			t.Errorf("the safe panel paints %q (U+%04X), neither a safe glyph nor a named exception:\n%s", r, r, painted)
		}
	}
	text := panelText(t, m)
	for _, want := range []string{"• Usage", "○ Sub-agents (1 · 1 queued)", "○ bravo", "> test", "○ ship", "» "} {
		if !slices.ContainsFunc(text, func(row string) bool { return strings.Contains(row, want) }) {
			t.Errorf("the safe panel carries no %q:\n%s", want, strings.Join(text, "\n"))
		}
	}
}

// TestSidePanelHeaderLeadsWithPin pins the header's shape: its pin glyph leads it — ● pinned, ○
// auto — on the cell a pin click lands on ([Model.panelPinX]), its summary sits in parentheses, and
// a `tool-leader` dotted leader runs from it to the fold's ▼ at the column's right edge, painted in
// the `tool-indicator` role, as a transcript header's does.
func TestSidePanelHeaderLeadsWithPin(t *testing.T) {
	t.Parallel()
	m := panelModel(t, "[tasks: auto, usage: pinned]", 30, taskList(0, "one"))
	frame := strings.Split(ansi.Strip(m.View().Content), "\n")
	painted := strings.Split(m.View().Content, "\n")
	dot := m.th.glyphs.leaderDot

	for _, tc := range []struct{ title, pin, words string }{
		{"Tasks", defaultGlyphs.panelOpen, "○ Tasks (0/1)"},
		{"Usage", defaultGlyphs.panelPinned, "● Usage"},
	} {
		y := panelRowY(t, m, tc.title)
		row := strings.TrimRight(ansi.Cut(frame[y], m.panelPinX(), m.width), " ")
		dots := panelEdgeX(m) + 1 - m.panelPinX() - len([]rune(tc.words)) - len(" ") - len(" ") - len([]rune(glyphExpanded))
		if want := tc.words + " " + strings.Repeat(dot, dots) + " " + glyphExpanded; row != want {
			t.Errorf("the %s header paints %q, want %q", tc.title, row, want)
		}
		if got := ansi.Cut(frame[y], m.panelPinX(), m.panelPinX()+1); got != tc.pin {
			t.Errorf("the %s header's pin cell holds %q, want %q", tc.title, got, tc.pin)
		}
		if got := ansi.Cut(frame[y], panelEdgeX(m), panelEdgeX(m)+1); got != glyphExpanded {
			t.Errorf("the %s header's right edge holds %q, want %q", tc.title, got, glyphExpanded)
		}
		if !strings.Contains(painted[y], m.th.toolIndicator.Render(glyphExpanded)) {
			t.Errorf("the %s header's %s is not painted in the tool-indicator role: %q", tc.title, glyphExpanded, painted[y])
		}
		if !strings.Contains(painted[y], m.th.toolLeader.Render(strings.Repeat(dot, dots))) {
			t.Errorf("the %s header's leader is not painted in the tool-leader role: %q", tc.title, painted[y])
		}
	}
}

// subAgentsHeaderModel is a panel model of width cells in colour scheme s, its Sub-agents section
// holding two running delegations and one still queued — the header `○ Sub-agents (2 · 1 queued)`.
func subAgentsHeaderModel(t *testing.T, s scheme.Scheme, width int) Model {
	t.Helper()

	opts := testOpts
	opts.ColorScheme = s
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	opts.UI.SidePanelSections = domain.DefaultSidePanelSections
	opts.UI.SidePanelWidth = width
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	m.panel = sidepanel.New(func() time.Time { return panelNow })
	for _, e := range []domain.Event{
		panelDelegationCall(t, "r1", "planner"), panelDelegationCall(t, "r2", "scout"),
		panelDelegationCall(t, "r3", "tester"),
		delegationPhase("r1", domain.SubAgentStarted, time.Second),
		delegationPhase("r2", domain.SubAgentStarted, time.Second),
	} {
		m = m.foldEvent(e)
	}
	return m
}

// TestSidePanelHeaderPaintsTheTranscriptHeadingTones pins a header's tones under both built-in
// schemes (docs/layout/side-panel-layout.md, "Header row"): the title bold in the transcript's
// label tone (toolLabel), the parenthesised summary in the muted count tone a type row's count
// wears (toolIndicator) and not bold, and the lead pin glyph not bold.
func TestSidePanelHeaderPaintsTheTranscriptHeadingTones(t *testing.T) {
	t.Parallel()
	const title, summary = "Sub-agents", "(2 · 1 queued)"
	for _, tc := range []struct {
		name string
		s    scheme.Scheme
	}{{"dark", scheme.Default()}, {"light", lightScheme(t)}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := subAgentsHeaderModel(t, tc.s, domain.DefaultSidePanelWidth)
			buf := uv.NewScreenBuffer(m.width, m.height)
			buf.Clear()
			uv.NewStyledString(m.View().Content).Draw(buf, buf.Bounds())
			y := panelRowY(t, m, title)
			labelFg, countFg := m.th.toolLabel.GetForeground(), m.th.toolIndicator.GetForeground()
			if sameColor(labelFg, countFg) {
				t.Fatalf("the label and count tones are one colour %v; the test cannot tell them apart", labelFg)
			}
			cellAt := func(x int) *uv.Cell {
				c := buf.CellAt(x, y)
				if c == nil {
					t.Fatalf("header row %d: no cell at column %d", y, x)
				}
				return c
			}
			if pin := cellAt(m.panelPinX()); pin.Style.Attrs&uv.AttrBold != 0 {
				t.Errorf("the pin glyph %q is bold, want it in its faint tone alone", pin.Content)
			}
			titleX := m.panelPinX() + 2
			for x := titleX; x < titleX+len(title); x++ {
				if c := cellAt(x); c.Style.Attrs&uv.AttrBold == 0 || !sameColor(c.Style.Fg, labelFg) {
					t.Errorf("title cell %d %q: bold %v, fg %v; want bold in toolLabel's %v",
						x, c.Content, c.Style.Attrs&uv.AttrBold != 0, c.Style.Fg, labelFg)
				}
			}
			summaryX := titleX + len(title) + 1
			for x := summaryX; x < summaryX+len([]rune(summary)); x++ {
				if c := cellAt(x); c.Style.Attrs&uv.AttrBold != 0 || !sameColor(c.Style.Fg, countFg) {
					t.Errorf("summary cell %d %q: bold %v, fg %v; want not bold in the muted count tone %v",
						x, c.Content, c.Style.Attrs&uv.AttrBold != 0, c.Style.Fg, countFg)
				}
			}
		})
	}
}

// TestSidePanelSubAgentsHeaderFitsAtWidth36 pins the summary's short form against the narrowest
// customary column: at panel width 36 the default Sub-agents header with two running and one
// queued run paints whole, its leader and fold indicator still after it.
func TestSidePanelSubAgentsHeaderFitsAtWidth36(t *testing.T) {
	t.Parallel()
	m := subAgentsHeaderModel(t, scheme.Default(), 36)
	if got := m.panelWidth(); got != 36 {
		t.Fatalf("the panel paints %d columns wide, want 36", got)
	}
	frame := strings.Split(ansi.Strip(m.View().Content), "\n")
	y := panelRowY(t, m, "Sub-agents")
	row := strings.TrimRight(ansi.Cut(frame[y], m.panelPinX(), m.width), " ")
	if !strings.HasPrefix(row, "○ Sub-agents (2 · 1 queued) ") || !panelHeaderTail.MatchString(row) {
		t.Errorf("the header paints %q, want `○ Sub-agents (2 · 1 queued)` whole before its leader and ▼", row)
	}
}

// foldedPanelModel is panelModel built from Options whose `ui.side-panel.folded` is folded and whose
// glyph set is glyphs, so a model starts folded the way a session started from that config does.
func foldedPanelModel(t *testing.T, folded domain.SidePanelFolded, glyphs domain.GlyphSet, events ...domain.Event) Model {
	t.Helper()

	opts := testOpts
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	opts.UI.SidePanelSections = domain.DefaultSidePanelSections
	opts.UI.SidePanelFolded = folded
	opts.UI.Glyphs = glyphs
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
	m.panel = sidepanel.New(func() time.Time { return panelNow })
	for _, e := range events {
		m = m.foldEvent(e)
	}
	return m
}

// panelHeaderGlyph is the fold indicator at the right edge of the header row titled title.
func panelHeaderGlyph(t *testing.T, m Model, title string) string {
	t.Helper()
	frame := strings.Split(ansi.Strip(m.View().Content), "\n")
	return ansi.Cut(frame[panelRowY(t, m, title)], panelEdgeX(m), panelEdgeX(m)+1)
}

// panelEdgeX is the window column of the panel content's last cell — a header's fold indicator, a
// row's right-aligned facts: the right margin ([Model.panelChrome]) short of the window's edge.
func panelEdgeX(m Model) int { return m.width - 1 - m.panelChrome().margin }

// TestSidePanelFoldTogglesAndWritesBack drives the fold (docs/layout/side-panel-layout.md,
// "Pointer"): a click on a header anywhere but its pin glyph folds the section to its header row,
// ▶ at its edge, and writes `ui.side-panel.folded` silently through the settings host; a second
// click opens it again. A failed write keeps the fold and says `not saved`; a model built from a
// folded list starts folded, and spare rows never open it; the safe glyph set folds to ►; and a
// fold while watching toggles and writes, as a task-list card's does.
func TestSidePanelFoldTogglesAndWritesBack(t *testing.T) {
	t.Parallel()

	t.Run("fold then open", func(t *testing.T) {
		t.Parallel()
		m := panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...)
		spy := &settingsSpy{}
		m.opts.Settings = spy.host(nil)
		notes := len(m.transcript.entries)

		m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, "Tasks"))

		if got := panelHeaderGlyph(t, m, "Tasks"); got != m.th.glyphs.collapsed {
			t.Errorf("the folded header's edge holds %q, want %q", got, m.th.glyphs.collapsed)
		}
		if rows := panelText(t, m); slices.ContainsFunc(rows, func(r string) bool { return strings.Contains(r, "read the plan") }) {
			t.Errorf("the folded section still paints its body:\n%s", strings.Join(rows, "\n"))
		}
		if m.opts.UI.SidePanelFolded != "[tasks]" {
			t.Errorf("folded %q, want [tasks]", m.opts.UI.SidePanelFolded)
		}
		if len(m.settingEdits) != 1 || m.settingEdits[0] != (settingEdit{path: domain.UIKeySidePanelFolded, value: "[tasks]"}) {
			t.Errorf("journal %+v, want one %s=[tasks] entry", m.settingEdits, domain.UIKeySidePanelFolded)
		}
		if len(m.transcript.entries) != notes {
			t.Errorf("the fold added %d transcript entries; a landed write says nothing", len(m.transcript.entries)-notes)
		}

		m = clickCell(t, m, panelEdgeX(m), panelRowY(t, m, "Tasks"))

		if got := panelHeaderGlyph(t, m, "Tasks"); got != glyphExpanded {
			t.Errorf("the reopened header's edge holds %q, want %q", got, glyphExpanded)
		}
		panelRowY(t, m, "read the plan")
		if want := []string{"tasks=true", "tasks=false"}; !slices.Equal(spy.folds, want) {
			t.Errorf("fold writes %v, want %v", spy.folds, want)
		}
		if m.opts.UI.SidePanelFolded != "" || len(spy.sections) != 0 {
			t.Errorf("folded %q, section writes %v; want none folded and no pin moved", m.opts.UI.SidePanelFolded, spy.sections)
		}
	})

	t.Run("a failed write keeps the fold and says so", func(t *testing.T) {
		t.Parallel()
		m := panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...)
		m.opts.Settings = (&settingsSpy{}).host(fmt.Errorf("disk full"))

		m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, "Tasks"))

		if got := panelHeaderGlyph(t, m, "Tasks"); got != m.th.glyphs.collapsed || m.opts.UI.SidePanelFolded != "[tasks]" {
			t.Errorf("edge %q, folded %q after a failed write; want the fold to stand", got, m.opts.UI.SidePanelFolded)
		}
		if !strings.Contains(plain(m.View()), sidePanelFoldedSource+": "+taskListNotSavedNote+"disk full") {
			t.Error("the failed write is not reported in the transcript")
		}
	})

	t.Run("a folded list starts folded and spare rows never open it", func(t *testing.T) {
		t.Parallel()
		m := foldedPanelModel(t, "[usage]", domain.GlyphsUnicode, panelUsageReading(panelUsed), standingCost(200, 100))

		if got := panelText(t, m); !slices.Equal(got, belowPanelTopRow("○ Usage (62%)")) {
			t.Errorf("a column of 30 rows paints\n%s\nwant the usage header alone", strings.Join(got, "\n"))
		}
		if got := panelHeaderGlyph(t, m, "Usage"); got != "▶" {
			t.Errorf("the folded header's edge holds %q, want ▶", got)
		}
	})

	t.Run("the safe glyph set folds to its pointer", func(t *testing.T) {
		t.Parallel()
		m := foldedPanelModel(t, "[usage]", domain.GlyphsSafe, panelUsageReading(panelUsed))

		if got := panelHeaderGlyph(t, m, "Usage"); got != "►" {
			t.Errorf("the folded header's edge holds %q under the safe set, want ►", got)
		}
	})

	t.Run("a fold while watching toggles and writes", func(t *testing.T) {
		t.Parallel()
		m := enterWatchOn(t, watchEngine(), newWatchFixture(t, "watched", nil, 0))
		m.opts.UI.SidePanelShow = domain.SidePanelShowAuto
		m.opts.UI.SidePanelSections = "[tasks: pinned]"
		spy := &settingsSpy{}
		m.opts.Settings = spy.host(nil)
		m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
		x, y := panelBodyX(m), panelRowY(t, m, "Tasks")
		if got := m.pointerShapeAt(m.withFrameSpans(), x, y); got != shapePointer {
			t.Errorf("hover over the watched header title: shape %q, want the pointer", got.name())
		}

		m = clickCell(t, m, x, y)

		if m.flash == watchRefusedFlash || m.opts.UI.SidePanelFolded != "[tasks]" || !slices.Equal(spy.folds, []string{"tasks=true"}) {
			t.Errorf("flash %q, folded %q, writes %v; want the fold toggled and written", m.flash, m.opts.UI.SidePanelFolded, spy.folds)
		}
	})
}

// TestSidePanelPinTogglesAndWritesBack drives the pin glyph: a click on an auto section's ○ pins
// it, through the settings host, and lands the list the host answers; a second click unpins it. A
// failed write leaves the section pinned on screen and says so in the transcript.
func TestSidePanelPinTogglesAndWritesBack(t *testing.T) {
	t.Parallel()

	t.Run("pin then unpin", func(t *testing.T) {
		t.Parallel()
		m := panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...)
		spy := &settingsSpy{}
		m.opts.Settings = spy.host(nil)

		m = clickCell(t, m, m.panelPinX(), panelRowY(t, m, "Tasks"))
		pinned := m.opts.UI.SidePanelSections
		m = clickCell(t, m, m.panelPinX(), panelRowY(t, m, "Tasks"))

		want := []domain.SidePanelEntry{
			{Section: domain.SidePanelTasks, Mode: domain.SidePanelModePinned},
			{Section: domain.SidePanelTasks, Mode: domain.SidePanelModeAuto},
		}
		if !slices.Equal(spy.sections, want) {
			t.Errorf("section writes %v, want %v", spy.sections, want)
		}
		if !strings.Contains(string(pinned), "tasks: pinned") || m.opts.UI.SidePanelSections != domain.DefaultSidePanelSections {
			t.Errorf("lists %q then %q; want tasks pinned, then the default", pinned, m.opts.UI.SidePanelSections)
		}
		if len(m.settingEdits) != 1 || m.settingEdits[0].path != domain.UIKeySidePanelSections {
			t.Errorf("journal %+v, want one %s entry", m.settingEdits, domain.UIKeySidePanelSections)
		}
	})

	t.Run("a failed write keeps the pin and says so", func(t *testing.T) {
		t.Parallel()
		m := panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...)
		m.opts.Settings = (&settingsSpy{}).host(fmt.Errorf("disk full"))

		m = clickCell(t, m, m.panelPinX(), panelRowY(t, m, "Tasks"))

		if !strings.Contains(panelText(t, m)[panelRowY(t, m, "Tasks")], defaultGlyphs.panelPinned) {
			t.Errorf("the header does not wear %s after the pin:\n%s", defaultGlyphs.panelPinned, strings.Join(panelText(t, m), "\n"))
		}
		if !strings.Contains(plain(m.View()), sidePanelSectionsSource+": "+taskListNotSavedNote+"disk full") {
			t.Error("the failed write is not reported in the transcript")
		}
	})
}

// TestSidePanelDragStopsAtTheSeparator pins the drag rule: a drag-select from the transcript stops
// at the separator however far into the column it is carried, and one that starts in the column
// selects nothing.
func TestSidePanelDragStopsAtTheSeparator(t *testing.T) {
	t.Parallel()
	m := streamOneScreen(t, panelModel(t, domain.DefaultSidePanelSections, 30))
	const y = 3

	dragged := step(t, step(t, m, leftClick(2, y)), leftDrag(m.width-2, y))

	if !dragged.transcriptSel.active || dragged.transcriptSel.head.col > dragged.viewport.Width() {
		t.Errorf("selection %+v; want a live drag whose head stops at the slot's edge (%d)",
			dragged.transcriptSel, dragged.viewport.Width())
	}

	fromPanel := step(t, step(t, step(t, m, leftClick(m.width-2, y)), leftDrag(2, y)), leftRelease(2, y))

	if fromPanel.transcriptSel.active || fromPanel.sel.active {
		t.Error("a drag that started in the column armed a selection")
	}
}

// separatorDragModel builds a ready model whose column shows at its default width in a 200-column
// `auto` window — room to drag either way before a limit — with a settings spy wired whose writes
// fail with failure where one is given; it returns the model, the spy and the separator's column.
func separatorDragModel(t *testing.T, failure error) (Model, *settingsSpy, int) {
	t.Helper()
	return separatorDragModelUnder(t, domain.SidePanelShowAuto, failure)
}

// separatorDragModelUnder is separatorDragModel under the show mode show: the `on` twin of the
// `auto` model the drag tests share.
func separatorDragModelUnder(t *testing.T, show domain.SidePanelShow, failure error) (Model, *settingsSpy, int) {
	t.Helper()

	m := sidePanelModel(t, show, true, 200)
	spy := &settingsSpy{}
	host := spy.host(nil)
	host.write = func(path, value string) error {
		spy.writes = append(spy.writes, path+"="+value)
		return failure
	}
	m.opts.Settings = host
	return m, spy, m.slotWidth()
}

// separatorRow is the screen row the separator drags press on: one above the slot's bottom.
const separatorRow = 2

// draggedPanelWidth is the default-width panel after its separator is dragged 14 columns left.
const draggedPanelWidth = domain.DefaultSidePanelWidth + 14

// TestSidePanelSeparatorDrag pins the resize drag (adr-0110-amend-1108 (b), (c)): a press on the
// separator or the cell right of it grabs it, each held-button motion re-wraps the frame live at
// the clamped width, and the release writes `ui.side-panel.width` once — only when the painted width
// moved off the press's, so a drag that comes back where it began writes nothing and leaves a stored
// width the window clamps as it stood.
func TestSidePanelSeparatorDrag(t *testing.T) {
	t.Parallel()

	t.Run("drag moves live and writes once", func(t *testing.T) {
		t.Parallel()
		m, spy, sep := separatorDragModel(t, nil)

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(sep-4, separatorRow))
		live := m.viewport.Width()
		m = step(t, m, leftDrag(sep-14, separatorRow))
		m = step(t, m, leftRelease(sep-14, separatorRow))

		if live != sep-4-scrollbarWidth {
			t.Errorf("mid-drag viewport width %d, want %d — the transcript did not re-wrap live", live, sep-4-scrollbarWidth)
		}
		if m.panelWidth() != draggedPanelWidth || m.slotWidth() != sep-14 {
			t.Errorf("panel %d, slot %d; want %d and %d", m.panelWidth(), m.slotWidth(), draggedPanelWidth, sep-14)
		}
		if want := []string{domain.UIKeySidePanelWidth + "=" + strconv.Itoa(draggedPanelWidth)}; !slices.Equal(spy.writes, want) {
			t.Errorf("writes %v, want %v", spy.writes, want)
		}
		if len(m.settingEdits) != 1 || m.settingEdits[0].path != domain.UIKeySidePanelWidth {
			t.Errorf("journal %+v, want one %s entry", m.settingEdits, domain.UIKeySidePanelWidth)
		}
	})

	t.Run("the cell right of it grabs it at its offset", func(t *testing.T) {
		t.Parallel()
		m, spy, sep := separatorDragModel(t, nil)

		m = step(t, m, leftClick(sep+1, separatorRow))
		m = step(t, m, leftDrag(sep-9, separatorRow))
		m = step(t, m, leftRelease(sep-9, separatorRow))

		if m.slotWidth() != sep-10 {
			t.Errorf("slot %d, want %d: the separator keeps one column left of the pointer", m.slotWidth(), sep-10)
		}
		if want := []string{domain.UIKeySidePanelWidth + "=" + strconv.Itoa(draggedPanelWidth-4)}; !slices.Equal(spy.writes, want) {
			t.Errorf("writes %v, want %v", spy.writes, want)
		}
	})

	for _, show := range []domain.SidePanelShow{domain.SidePanelShowAuto, domain.SidePanelShowOn} {
		t.Run("a drag past either limit clamps/"+string(show), func(t *testing.T) {
			t.Parallel()
			m, spy, sep := separatorDragModelUnder(t, show, nil)

			m = step(t, m, leftClick(sep, separatorRow))
			m = step(t, m, leftDrag(0, separatorRow))
			widest := m.panelWidth()
			m = step(t, m, leftDrag(m.width-1, separatorRow))
			m = step(t, m, leftRelease(m.width-1, separatorRow))

			if widthCap := m.width - sidePanelTranscriptLeast - scrollbarWidth; widest != widthCap {
				t.Errorf("dragged to the left edge the panel is %d, want the cap %d", widest, widthCap)
			}
			if m.panelWidth() != domain.MinSidePanelWidth {
				t.Errorf("dragged to the right edge the panel is %d, want the minimum %d", m.panelWidth(), domain.MinSidePanelWidth)
			}
			if want := []string{domain.UIKeySidePanelWidth + "=24"}; !slices.Equal(spy.writes, want) {
				t.Errorf("writes %v, want %v", spy.writes, want)
			}
		})
	}

	t.Run("press and release writes nothing", func(t *testing.T) {
		t.Parallel()
		m, spy, sep := separatorDragModel(t, nil)
		stored := m.opts.UI.SidePanelWidth

		m = clickCell(t, m, sep, separatorRow)

		if len(spy.writes) != 0 || m.opts.UI.SidePanelWidth != stored || m.panelDrag.active {
			t.Errorf("writes %v, stored %d, drag %+v; want none, %d, ended", spy.writes, m.opts.UI.SidePanelWidth, m.panelDrag, stored)
		}
	})

	t.Run("a drag back to the press column writes nothing", func(t *testing.T) {
		t.Parallel()
		m, spy, sep := separatorDragModel(t, nil)
		stored := m.opts.UI.SidePanelWidth

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(sep-10, separatorRow))
		m = step(t, m, leftDrag(sep, separatorRow))
		m = step(t, m, leftRelease(sep, separatorRow))

		if len(spy.writes) != 0 || m.opts.UI.SidePanelWidth != stored {
			t.Errorf("writes %v, stored %d; want none and %d", spy.writes, m.opts.UI.SidePanelWidth, stored)
		}
	})

	t.Run("a clamped stored width survives a drag that comes back", func(t *testing.T) {
		t.Parallel()
		opts := testOpts
		opts.UI.SidePanelShow = domain.SidePanelShowAuto
		opts.UI.SidePanelWidth = 100
		spy := &settingsSpy{}
		opts.Settings = spy.host(nil)
		m := newModel(context.Background(), &fakeEngine{}, opts, nil)
		m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
		sep := m.slotWidth()
		if clamped := 140 - sidePanelTranscriptLeast - scrollbarWidth; m.panelWidth() != clamped {
			t.Fatalf("setup: the stored 100 paints %d, want it clamped to %d", m.panelWidth(), clamped)
		}

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(sep, separatorRow))
		m = step(t, m, leftDrag(sep+9, separatorRow))
		m = step(t, m, leftDrag(sep, separatorRow))
		m = step(t, m, leftRelease(sep, separatorRow))

		if len(spy.writes) != 0 || m.opts.UI.SidePanelWidth != 100 {
			t.Errorf("writes %v, stored %d; want none and the stored 100 the window clamps to %d",
				spy.writes, m.opts.UI.SidePanelWidth, m.panelWidth())
		}
	})

	t.Run("a failed write says so once and the width stands", func(t *testing.T) {
		t.Parallel()
		m, _, sep := separatorDragModel(t, fmt.Errorf("disk full"))

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(sep-14, separatorRow))
		m = step(t, m, leftRelease(sep-14, separatorRow))

		note := sidePanelWidthSource + ": " + taskListNotSavedNote + "disk full"
		if got := strings.Count(plain(m.View()), note); got != 1 {
			t.Errorf("the failed write is reported %d times, want once: %q", got, note)
		}
		if m.panelWidth() != draggedPanelWidth || len(m.settingEdits) != 0 {
			t.Errorf("panel %d, journal %+v; want the dragged %d standing and nothing journalled", m.panelWidth(), m.settingEdits, draggedPanelWidth)
		}
	})

	t.Run("a resize that hides the column abandons the drag", func(t *testing.T) {
		t.Parallel()
		m, spy, sep := separatorDragModel(t, nil)
		stored := m.opts.UI.SidePanelWidth

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(sep-14, separatorRow))
		m = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
		m = step(t, m, leftDrag(10, separatorRow))
		m = step(t, m, leftRelease(10, separatorRow))
		m = step(t, m, tea.WindowSizeMsg{Width: 200, Height: 30})

		if len(spy.writes) != 0 || m.opts.UI.SidePanelWidth != stored || m.panelWidth() != domain.DefaultSidePanelWidth {
			t.Errorf("writes %v, stored %d, panel %d; want none, the press-time %d, and the default width back",
				spy.writes, m.opts.UI.SidePanelWidth, m.panelWidth(), stored)
		}
	})

	t.Run("a button-less motion mid-drag ends it", func(t *testing.T) {
		t.Parallel()
		m, spy, sep := separatorDragModel(t, nil)

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(sep-14, separatorRow))
		m = step(t, m, tea.MouseMotionMsg{X: sep - 14, Y: separatorRow, Button: tea.MouseNone})
		m = step(t, m, leftDrag(sep-20, separatorRow))

		if m.panelDrag.active || m.mousePressed || m.panelWidth() != draggedPanelWidth {
			t.Errorf("drag %+v, pressed %v, panel %d; want the SGR-35 release to end it at %d", m.panelDrag, m.mousePressed, m.panelWidth(), draggedPanelWidth)
		}
		if want := []string{domain.UIKeySidePanelWidth + "=" + strconv.Itoa(draggedPanelWidth)}; !slices.Equal(spy.writes, want) {
			t.Errorf("writes %v, want %v", spy.writes, want)
		}
	})

	t.Run("the separator is lit while the drag holds it", func(t *testing.T) {
		t.Parallel()
		m, _, sep := separatorDragModel(t, nil)
		lit := m.th.panelDragRule.Render(sidePanelSeparator)
		if lit == m.th.scrollTrack.Render(sidePanelSeparator) {
			t.Skip("no colour profile in this environment — the two separator styles are not observable")
		}

		held := step(t, step(t, m, leftClick(sep, separatorRow)), leftDrag(sep-4, separatorRow))
		released := step(t, held, leftRelease(sep-4, separatorRow))

		if !strings.HasPrefix(held.sidePanelColumn(1)[0], lit) {
			t.Errorf("mid-drag the column opens %q, want the accent-bold separator %q", held.sidePanelColumn(1)[0], lit)
		}
		if strings.HasPrefix(released.sidePanelColumn(1)[0], lit) {
			t.Error("the separator stays lit after the release")
		}
	})

	// panel-separator-double-stroke: at rest the separator is the scroll track's stroke in the
	// track's role, so the bar beside it never reads as a second rule in another tone.
	t.Run("at rest the separator wears the scroll track's role", func(t *testing.T) {
		t.Parallel()
		m, _, _ := separatorDragModel(t, nil)
		track := scrollbarCells(m.th, 0, 1, 2, 2)[1]
		if track != m.th.scrollTrack.Render(glyphScrollTrack) {
			t.Fatalf("the bar's track cell is %q, want the scrollTrack stroke", track)
		}
		if glyphScrollTrack != sidePanelSeparator {
			t.Fatalf("the separator %q and the track %q are different shapes", sidePanelSeparator, glyphScrollTrack)
		}

		if opening := m.sidePanelColumn(1)[0]; !strings.HasPrefix(opening, track) {
			t.Errorf("at rest the column opens %q, want the scroll track's stroke %q", opening, track)
		}
	})
}

// TestSidePanelSeparatorDragIsInertWhileWatching pins adr-0110-amend-1108 (c): watching another
// session, a press on the separator is claimed as any column cell is and starts no drag.
func TestSidePanelSeparatorDragIsInertWhileWatching(t *testing.T) {
	t.Parallel()
	m := enterWatchOn(t, watchEngine(), newWatchFixture(t, "watched", nil, 0))
	m.opts.UI.SidePanelShow = domain.SidePanelShowAuto
	m = step(t, m, tea.WindowSizeMsg{Width: 200, Height: 30})
	sep, stored := m.slotWidth(), m.opts.UI.SidePanelWidth

	m = step(t, m, leftClick(sep, separatorRow))
	m = step(t, m, leftDrag(sep-14, separatorRow))
	m = step(t, m, leftRelease(sep-14, separatorRow))

	if m.slotWidth() != sep || m.opts.UI.SidePanelWidth != stored || m.transcriptSel.active {
		t.Errorf("slot %d, stored %d, selection %+v; want the separator unmoved at %d and nothing selected",
			m.slotWidth(), m.opts.UI.SidePanelWidth, m.transcriptSel, sep)
	}
}

// doubleClickModel is separatorDragModel at a stored width of 50, its settings host also recording
// each reset (failing with failure where one is given) and its clock stubbed: set *clock to place
// the next press in time.
func doubleClickModel(t *testing.T, failure error) (Model, *settingsSpy, *[]string, *time.Time) {
	t.Helper()

	m, spy, _ := separatorDragModel(t, nil)
	var resets []string
	host := m.opts.Settings.(fakeSettingsHost)
	host.reset = func(path string) error {
		resets = append(resets, path)
		return failure
	}
	m.opts.Settings = host
	clock := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clock }
	m.opts.UI.SidePanelWidth = 50
	m = step(t, m, tea.WindowSizeMsg{Width: 200, Height: 30})
	return m, spy, &resets, &clock
}

// pressSeparator presses and releases the grip at column x, without moving.
func pressSeparator(t *testing.T, m Model, x int) Model {
	t.Helper()
	return step(t, step(t, m, leftClick(x, separatorRow)), leftRelease(x, separatorRow))
}

// TestSidePanelSeparatorDoubleClick pins adr-0110-amend-1108 (b)'s reset: two presses on the grip
// within 500 ms return `ui.side-panel.width` to its unset width (0, painted at the default 44)
// through the settings host's Reset and start no drag; presses further apart, or a press right after a drag that moved the separator, are drags.
func TestSidePanelSeparatorDoubleClick(t *testing.T) {
	t.Parallel()

	t.Run("a double-click resets and starts no drag", func(t *testing.T) {
		t.Parallel()
		m, spy, resets, clock := doubleClickModel(t, nil)
		sep := m.slotWidth()

		m = pressSeparator(t, m, sep)
		*clock = clock.Add(400 * time.Millisecond)
		m = step(t, m, leftClick(sep, separatorRow))
		pressed := m.panelDrag
		m = step(t, m, leftRelease(sep, separatorRow))

		if pressed.active {
			t.Errorf("the second press armed a drag %+v, want none", pressed)
		}
		if want := []string{domain.UIKeySidePanelWidth}; !slices.Equal(*resets, want) || len(spy.writes) != 0 {
			t.Errorf("resets %v, writes %v; want %v and no write", *resets, spy.writes, want)
		}
		if m.panelWidth() != domain.DefaultSidePanelWidth || m.slotWidth() != sep+50-domain.DefaultSidePanelWidth {
			t.Errorf("panel %d, slot %d; want %d and %d", m.panelWidth(), m.slotWidth(),
				domain.DefaultSidePanelWidth, sep+50-domain.DefaultSidePanelWidth)
		}
		if m.opts.UI.SidePanelWidth != 0 {
			t.Errorf("the reset stored ui.side-panel.width %d, want 0, the unset width", m.opts.UI.SidePanelWidth)
		}
		if m.viewport.Width() != m.slotWidth()-scrollbarWidth {
			t.Errorf("viewport width %d, want %d — the transcript did not re-wrap at the reset width",
				m.viewport.Width(), m.slotWidth()-scrollbarWidth)
		}
		if len(m.settingEdits) != 1 || !m.settingEdits[0].reset || m.settingEdits[0].path != domain.UIKeySidePanelWidth {
			t.Errorf("journal %+v, want one %s reset", m.settingEdits, domain.UIKeySidePanelWidth)
		}
	})

	t.Run("two slow presses keep the width", func(t *testing.T) {
		t.Parallel()
		m, spy, resets, clock := doubleClickModel(t, nil)
		sep := m.slotWidth()

		m = pressSeparator(t, m, sep)
		*clock = clock.Add(600 * time.Millisecond)
		m = pressSeparator(t, m, sep)

		if len(*resets) != 0 || len(spy.writes) != 0 || m.panelWidth() != 50 {
			t.Errorf("resets %v, writes %v, panel %d; want none, none and 50", *resets, spy.writes, m.panelWidth())
		}
	})

	t.Run("a press after a moving drag starts a drag", func(t *testing.T) {
		t.Parallel()
		m, _, resets, clock := doubleClickModel(t, nil)
		sep := m.slotWidth()

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(sep-4, separatorRow))
		m = step(t, m, leftRelease(sep-4, separatorRow))
		*clock = clock.Add(100 * time.Millisecond)
		m = step(t, m, leftClick(sep-4, separatorRow))

		if !m.panelDrag.active || len(*resets) != 0 || m.panelWidth() != 54 {
			t.Errorf("drag %+v, resets %v, panel %d; want a drag armed, no reset and the dragged 54",
				m.panelDrag, *resets, m.panelWidth())
		}
	})

	t.Run("a failed reset says so once and the default stands", func(t *testing.T) {
		t.Parallel()
		m, _, _, clock := doubleClickModel(t, fmt.Errorf("disk full"))
		sep := m.slotWidth()

		m = pressSeparator(t, m, sep)
		*clock = clock.Add(100 * time.Millisecond)
		m = pressSeparator(t, m, sep)

		note := sidePanelWidthSource + ": " + taskListNotSavedNote + "disk full"
		if got := strings.Count(plain(m.View()), note); got != 1 {
			t.Errorf("the failed reset is reported %d times, want once: %q", got, note)
		}
		if m.panelWidth() != domain.DefaultSidePanelWidth || len(m.settingEdits) != 0 {
			t.Errorf("panel %d, journal %+v; want the default %d standing and nothing journalled",
				m.panelWidth(), m.settingEdits, domain.DefaultSidePanelWidth)
		}
	})
}

// panelHelpWords reads the /panel line /help prints and returns its three word groups — the show
// modes, the section verbs and the width forms — so the tests drive exactly the strings the human
// is shown.
func panelHelpWords(t *testing.T) (shows, verbs, widths []string) {
	t.Helper()
	line := ""
	for _, ln := range strings.Split(helpNote(false), "\n") {
		if strings.HasPrefix(ln, "/panel — ") {
			line = ln
		}
	}
	var groups [][]string
	for _, field := range strings.Fields(line) {
		if strings.Contains(field, "|") {
			groups = append(groups, strings.Split(strings.TrimSuffix(field, ","), "|"))
		}
	}
	if len(groups) != 3 {
		t.Fatalf("the /help line %q names %d word groups, want the show modes, the section verbs and the width forms",
			line, len(groups))
	}
	return groups[0], groups[1], groups[2]
}

// noteHolding reports whether the transcript holds a note containing text.
func noteHolding(m Model, text string) bool {
	return slices.ContainsFunc(m.transcript.entries, func(e entry) bool {
		return e.kind == entryNote && strings.Contains(e.text, text)
	})
}

// TestSidePanelCommandDrivesTheSettings drives every /panel form /help lists: each show mode sets
// and writes `ui.side-panel.show` and moves the column at once; each section verb sets that
// section's mode through the section writer, `unpin` meaning `auto`; `width <n>` writes the width
// and `width reset` resets it through the host's Reset, each moving the column at once; and each
// answers with the settings now in force.
func TestSidePanelCommandDrivesTheSettings(t *testing.T) {
	t.Parallel()
	shows, verbs, widths := panelHelpWords(t)

	for _, word := range shows {
		t.Run("/panel "+word, func(t *testing.T) {
			t.Parallel()
			start := domain.SidePanelShowOff
			if word == string(domain.SidePanelShowOff) {
				start = domain.SidePanelShowOn
			}
			m := sidePanelModel(t, start, true, 140)
			spy := &settingsSpy{}
			m.opts.Settings = spy.host(nil)

			m = sendLine(t, m, "/panel "+word)

			if m.opts.UI.SidePanelShow != domain.SidePanelShow(word) {
				t.Errorf("show mode = %q, want %q", m.opts.UI.SidePanelShow, word)
			}
			if want := []string{domain.UIKeySidePanelShow + "=" + word}; !slices.Equal(spy.writes, want) {
				t.Errorf("writes %v, want %v", spy.writes, want)
			}
			if len(m.settingEdits) != 1 || m.settingEdits[0].path != domain.UIKeySidePanelShow {
				t.Errorf("journal %+v, want one %s entry", m.settingEdits, domain.UIKeySidePanelShow)
			}
			if wantShown := word != string(domain.SidePanelShowOff); m.panelShown() != wantShown {
				t.Errorf("column shown = %v at 140 columns under %q, want %v", m.panelShown(), word, wantShown)
			}
			if !noteHolding(m, "side panel: "+word+" — ") {
				t.Errorf("no report of the new mode:\n%s", plain(m.View()))
			}
		})
	}

	modes := map[string]domain.SidePanelMode{
		"pin": domain.SidePanelModePinned, "unpin": domain.SidePanelModeAuto, "hide": domain.SidePanelModeHidden,
	}
	for _, verb := range verbs {
		t.Run("/panel "+verb+" tasks", func(t *testing.T) {
			t.Parallel()
			want, ok := modes[verb]
			if !ok {
				t.Fatalf("/help lists a section verb %q this test does not know", verb)
			}
			m := sidePanelModel(t, domain.SidePanelShowAuto, true, 140)
			spy := &settingsSpy{}
			m.opts.Settings = spy.host(nil)

			m = sendLine(t, m, "/panel "+verb+" tasks")

			if w := []domain.SidePanelEntry{{Section: domain.SidePanelTasks, Mode: want}}; !slices.Equal(spy.sections, w) {
				t.Errorf("section writes %v, want %v", spy.sections, w)
			}
			if !strings.Contains(string(m.opts.UI.SidePanelSections), "tasks: "+string(want)) {
				t.Errorf("sections = %q, want tasks %s", m.opts.UI.SidePanelSections, want)
			}
			if !noteHolding(m, "tasks "+string(want)) {
				t.Errorf("no report of the new section mode:\n%s", plain(m.View()))
			}
		})
	}

	for _, form := range widths {
		t.Run("/panel width "+form, func(t *testing.T) {
			t.Parallel()
			m := sidePanelModel(t, domain.SidePanelShowOn, true, 140)
			m.opts.UI.SidePanelWidth = panelCommandWidth
			spy := &settingsSpy{}
			m.opts.Settings = spy.host(nil)
			line, want := panelWidthLine(t, form)

			m = sendLine(t, m, line)

			if m.panelWidth() != want.width {
				t.Errorf("panel width = %d, want %d", m.panelWidth(), want.width)
			}
			if !slices.Equal(spy.writes, want.writes) || !slices.Equal(spy.resets, want.resets) {
				t.Errorf("writes %v, resets %v; want %v, %v", spy.writes, spy.resets, want.writes, want.resets)
			}
			wantEdit := settingEdit{path: domain.UIKeySidePanelWidth, value: strconv.Itoa(want.width), reset: want.resets != nil}
			if len(m.settingEdits) != 1 || m.settingEdits[0] != wantEdit {
				t.Errorf("journal %+v, want %+v", m.settingEdits, wantEdit)
			}
			if want.resets != nil && m.opts.UI.SidePanelWidth != 0 {
				t.Errorf("the reset stored ui.side-panel.width %d, want 0, the unset width", m.opts.UI.SidePanelWidth)
			}
			if !noteHolding(m, "side panel: on — ") {
				t.Errorf("no report of the settings in force:\n%s", plain(m.View()))
			}
		})
	}

	t.Run("a failed show write keeps the mode and says so", func(t *testing.T) {
		t.Parallel()
		m := sidePanelModel(t, domain.SidePanelShowOff, true, 140)
		m.opts.Settings = fakeSettingsHost{write: func(string, string) error { return fmt.Errorf("disk full") }}

		m = sendLine(t, m, "/panel on")

		if !m.panelShown() {
			t.Error("the column did not move on the failed write")
		}
		if !strings.Contains(plain(m.View()), sidePanelShowSource+": "+taskListNotSavedNote+"disk full") {
			t.Error("the failed write is not reported in the transcript")
		}
	})
}

// panelCommandWidth is the stored width the /panel width tests start from: neither the width they
// set nor the default, and paintable in their 140-column window under `on`.
const panelCommandWidth = 50

// panelWidthWant is what one /panel width form must leave: the painted width, and the host's
// writes and resets.
type panelWidthWant struct {
	width  int
	writes []string
	resets []string
}

// panelWidthLine turns one width form /help lists into the line that drives it and what that line
// must leave: `<n>` is filled with a width of 40, `reset` returns the default through Reset.
func panelWidthLine(t *testing.T, form string) (string, panelWidthWant) {
	t.Helper()
	switch form {
	case "<n>":
		return "/panel width 40", panelWidthWant{width: 40, writes: []string{domain.UIKeySidePanelWidth + "=40"}}
	case panelWidthReset:
		return "/panel width reset", panelWidthWant{
			width:  domain.DefaultSidePanelWidth,
			resets: []string{domain.UIKeySidePanelWidth},
		}
	}
	t.Fatalf("/help lists a width form %q this test does not know", form)
	return "", panelWidthWant{}
}

// TestSidePanelCommandRefusesABadWidth pins the width refusals: a width below the minimum, 0, a
// non-integer, no width and two widths are each refused — the first three in the sentence a config
// file stating them is refused in — with the usage line, and nothing is written or changed.
func TestSidePanelCommandRefusesABadWidth(t *testing.T) {
	t.Parallel()
	refusals := map[string]string{
		"/panel width 23":    "apogee: invalid ui.side-panel.width 23: want 24 columns or more. " + panelUsage,
		"/panel width 0":     "apogee: invalid ui.side-panel.width 0: want 24 columns or more. " + panelUsage,
		"/panel width wide":  `apogee: invalid ui.side-panel.width "wide": want a number of columns, 24 or more. ` + panelUsage,
		"/panel width":       "/panel width takes a number of columns or reset. " + panelUsage,
		"/panel width 40 50": "/panel width takes a number of columns or reset. " + panelUsage,
	}
	for line, want := range refusals {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			m := sidePanelModel(t, domain.SidePanelShowOn, true, 140)
			spy := &settingsSpy{}
			m.opts.Settings = spy.host(nil)
			before := m.opts.UI

			m = sendLine(t, m, line)

			if !noteHolding(m, want) {
				t.Errorf("no refusal %q:\n%s", want, plain(m.View()))
			}
			if len(spy.writes) != 0 || len(spy.resets) != 0 || m.opts.UI != before {
				t.Errorf("a refused line wrote %v, reset %v and left %+v", spy.writes, spy.resets, m.opts.UI)
			}
		})
	}
}

// TestSidePanelCommandRefusesAnUnknownSection pins the refusal: a section this build does not know
// is named back with the sections there are and the usage line, and nothing is written or changed.
func TestSidePanelCommandRefusesAnUnknownSection(t *testing.T) {
	t.Parallel()
	m := sidePanelModel(t, domain.SidePanelShowAuto, true, 140)
	spy := &settingsSpy{}
	m.opts.Settings = spy.host(nil)
	before := m.opts.UI

	m = sendLine(t, m, "/panel pin lsp")

	if !noteHolding(m, `unknown side-panel section "lsp" (known sections: sub-agents, tasks, changed-files, usage, mcp). `+panelUsage) {
		t.Errorf("no refusal naming the sections:\n%s", plain(m.View()))
	}
	if len(spy.sections) != 0 || len(spy.writes) != 0 || m.opts.UI != before {
		t.Errorf("a refused line wrote %v / %v and left %+v", spy.sections, spy.writes, m.opts.UI)
	}
	for _, line := range []string{"/panel sideways", "/panel on now", "/panel hide"} {
		if _, err := parsePanel(strings.Fields(line)[1:]); err == nil || !strings.Contains(err.Error(), panelUsage) {
			t.Errorf("parsePanel(%q) = %v, want an error carrying the usage line", line, err)
		}
	}
}

// TestSidePanelMCPSectionIsNamed pins the `mcp` section's config half (ADR 0111 D10): `/panel
// pin|unpin|hide mcp` parses to that section.
func TestSidePanelMCPSectionIsNamed(t *testing.T) {
	t.Parallel()
	for verb, mode := range map[string]domain.SidePanelMode{
		"pin": domain.SidePanelModePinned, "unpin": domain.SidePanelModeAuto, "hide": domain.SidePanelModeHidden,
	} {
		got, err := parsePanel([]string{verb, "mcp"})
		want := panelArgs{action: panelSetSection, section: domain.SidePanelMCP, mode: mode}
		if err != nil || got != want {
			t.Errorf("parsePanel(%s mcp) = %+v, %v; want %+v", verb, got, err, want)
		}
	}
}

// panelMCPEvents is the mcp section's fixture: a server in each state the rows word — connected with
// one tool, failed with no connection left, a failed reconnect whose previous connection is kept,
// dialling, and disconnected — the first named with an escape sequence to be stripped.
func panelMCPEvents() []domain.Event {
	return []domain.Event{
		domain.MCPServerEvent{Server: "\x1b]0;pwned\x07alpha", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 1},
		domain.MCPServerEvent{Server: "beta", Transport: "sse", Phase: domain.MCPServerFailed, Cause: "connection refused"},
		domain.MCPServerEvent{Server: "gamma", Transport: "http", Phase: domain.MCPServerConnected, Tools: 2},
		domain.MCPServerEvent{Server: "gamma", Transport: "http", Phase: domain.MCPServerFailed, Cause: "timeout", Kept: true},
		domain.MCPServerEvent{Server: "delta", Transport: "stdio", Phase: domain.MCPServerDialling},
		domain.MCPServerEvent{Server: "omega", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 4},
		domain.MCPServerEvent{Server: "omega", Transport: "stdio", Phase: domain.MCPServerDisconnected},
	}
}

// panelFactsRow is a body row as the panel must lay it out behind a blank one-cell lead: the body
// indent and lead, text, and facts right-aligned so they end at the content's right edge.
func panelFactsRow(m Model, text, facts string) string {
	lead := bodyIndent + "  "
	pad := m.panelContentWidth() - len(lead) - ansi.StringWidth(text) - ansi.StringWidth(facts)
	return lead + text + strings.Repeat(" ", max(1, pad)) + facts
}

// TestSidePanelMCPSectionRows pins the mcp section's rows (ADR 0111 D10, as amended by
// adr-0111-amend-1109c): the name, then `state · N tools` right-aligned at the content's edge,
// per server in the order they were first reported, `1 tool` in the singular, the count left off
// while no connection serves and kept while a failed reconnect's previous connection still does,
// the header summarising the servers not connected by phase, and a name's escapes stripped.
func TestSidePanelMCPSectionRows(t *testing.T) {
	t.Parallel()
	m := panelModel(t, "[mcp: pinned]", 30, panelMCPEvents()...)

	section, isLive := m.panelSection(domain.SidePanelMCP)
	if !isLive {
		t.Error("the mcp section is not live with servers not connected")
	}
	want := []string{ // behind the body indent and the classic spinner's blank one-cell lead
		panelFactsRow(m, "alpha", "connected · 1 tool"),
		panelFactsRow(m, "beta", "failed"),
		panelFactsRow(m, "gamma", "failed · 2 tools"),
		panelFactsRow(m, "delta", "dialling"),
		panelFactsRow(m, "omega", "disconnected"),
	}
	got := make([]string, 0, len(section.body))
	for _, row := range section.body {
		if strings.Contains(row, "\x1b]") || strings.Contains(row, "pwned") {
			t.Errorf("row %q carries the server name's escape sequence", row)
		}
		if width := ansi.StringWidth(row); width != m.panelContentWidth() {
			t.Errorf("row %q paints %d cells; want its facts to end at the content's edge, %d", ansi.Strip(row), width, m.panelContentWidth())
		}
		got = append(got, ansi.Strip(row))
	}
	if !slices.Equal(got, want) {
		t.Errorf("mcp rows = %q, want %q", got, want)
	}
	if wantSummary := "1 dialling · 2 failed · 1 disconnected"; section.summary != wantSummary {
		t.Errorf("mcp summary = %q, want %q", section.summary, wantSummary)
	}
	column := strings.Join(panelText(t, m), "\n")
	for _, text := range append([]string{"MCP (1 dialling · 2 failed"}, want...) { // the header clips its tail
		if !strings.Contains(column, text) {
			t.Errorf("the column paints no %q:\n%s", text, column)
		}
	}
}

// TestSidePanelMCPSectionAuto pins the mcp section's live rule (ADR 0111 D10): in auto it shows
// while any server is dialling, failed or disconnected — summarising the count, `1 failed` — and
// is hidden with no server or every server connected; pinned it shows regardless, hidden never.
func TestSidePanelMCPSectionAuto(t *testing.T) {
	t.Parallel()
	connected := []domain.Event{
		domain.MCPServerEvent{Server: "alpha", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 3},
		domain.MCPServerEvent{Server: "beta", Transport: "stdio", Phase: domain.MCPServerConnected, Tools: 1},
	}
	failed := append(slices.Clone(connected),
		domain.MCPServerEvent{Server: "beta", Transport: "stdio", Phase: domain.MCPServerFailed, Cause: "exit 1"})
	cases := []struct {
		name      string
		sections  domain.SidePanelSections
		events    []domain.Event
		wantShown bool
	}{
		{name: "auto, no server", sections: domain.DefaultSidePanelSections, wantShown: false},
		{name: "auto, all connected", sections: domain.DefaultSidePanelSections, events: connected, wantShown: false},
		{name: "auto, one failed", sections: domain.DefaultSidePanelSections, events: failed, wantShown: true},
		{name: "pinned, all connected", sections: "[mcp: pinned]", events: connected, wantShown: true},
		{name: "hidden, one failed", sections: "[mcp: hidden]", events: failed, wantShown: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := panelModel(t, tc.sections, 30, tc.events...)
			at := slices.IndexFunc(m.panelSections(), func(s panelSection) bool { return s.kind == domain.SidePanelMCP })
			if shown := at >= 0; shown != tc.wantShown {
				t.Fatalf("mcp shown = %v, want %v", shown, tc.wantShown)
			}
			if tc.name == "auto, one failed" && m.panelSections()[at].summary != "1 failed" {
				t.Errorf("mcp summary = %q, want %q", m.panelSections()[at].summary, "1 failed")
			}
		})
	}
}

// TestSidePanelMCPClickOpensReport pins the mcp section's click (ADR 0111 D10): a click on any of
// its rows opens /mcp.
func TestSidePanelMCPClickOpensReport(t *testing.T) {
	t.Parallel()
	for _, row := range []string{"connected · 1 tool", "omega"} {
		m := panelModel(t, domain.DefaultSidePanelSections, 30, panelMCPEvents()...)

		m = clickCell(t, m, panelBodyX(m), panelRowY(t, m, row))

		if !m.mcpPane.open {
			t.Errorf("a click on %q left the /mcp report closed", row)
		}
	}
}

// TestSidePanelCommandBareReports pins the bare form: it reports the show mode and every section's
// mode in display order, writes nothing, says when the window is too narrow to dock the column,
// and answers mid-run where the writing forms wait for idle. On such a window it also opens the
// column as the overlay in navigation mode on its first stop (ADR 0111 D7) — but only where the
// column could be drawn and has a stop: under `off`, the zero UIPrefs' empty mode or with nothing
// live it parks no cursor.
func TestSidePanelCommandBareReports(t *testing.T) {
	t.Parallel()
	m := sidePanelModel(t, domain.SidePanelShowAuto, true, 100)
	m.opts.UI.SidePanelSections = "[usage: pinned, tasks: hidden]"
	spy := &settingsSpy{}
	m.opts.Settings = spy.host(nil)

	m = sendLine(t, m, "/panel")

	want := "side panel: auto — usage pinned · tasks hidden · sub-agents auto · changed-files auto · mcp auto" +
		" (not docked: the window is narrower than 137 columns)"
	if !noteHolding(m, want) {
		t.Errorf("no report %q:\n%s", want, plain(m.View()))
	}
	if stops := m.navPanelStops(); len(stops) == 0 || panelCursorStop(t, m) != stops[0] {
		t.Errorf("the cursor is not on the overlay's first stop %+v", stops)
	}
	if !m.panelOverlaid() || frameColumnOf(m, "Usage") < m.width-m.panelPaintWidth() {
		t.Errorf("the overlay is not painted (overlaid %v):\n%s", m.panelOverlaid(), plain(m.View()))
	}
	if len(spy.sections) != 0 || len(spy.writes) != 0 {
		t.Errorf("the report wrote %v / %v", spy.sections, spy.writes)
	}
	for line, wantSafe := range map[string]bool{
		"/panel": true, "/panel on": false, "/panel pin tasks": false,
		"/panel width 40": false, "/panel width reset": false,
	} {
		if got := parseInput(line, nil).safeWhileRunning(); got != wantSafe {
			t.Errorf("%q safe while running = %v, want %v", line, got, wantSafe)
		}
	}

	cases := []struct {
		name     string
		show     domain.SidePanelShow
		sections domain.SidePanelSections
		width    int
		want     string
	}{
		{"off", domain.SidePanelShowOff, "[usage: pinned]", 100,
			"side panel: off — usage pinned · sub-agents auto · tasks auto · changed-files auto · mcp auto"},
		{"the zero UIPrefs' empty mode", "", "[usage: pinned]", 100,
			"side panel: off — usage pinned · sub-agents auto · tasks auto · changed-files auto · mcp auto"},
		{"nothing live", domain.SidePanelShowAuto, domain.DefaultSidePanelSections, 100,
			"side panel: auto — sub-agents auto · tasks auto · changed-files auto · usage auto · mcp auto" +
				" (not docked: the window is narrower than 137 columns)"},
		{"docked", domain.SidePanelShowAuto, "[usage: pinned]", 140,
			"side panel: auto — usage pinned · sub-agents auto · tasks auto · changed-files auto · mcp auto"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := sidePanelModel(t, tc.show, true, tc.width)
			m.opts.UI.SidePanelSections = tc.sections

			m = sendLine(t, m, "/panel")

			if !noteHolding(m, tc.want) {
				t.Errorf("no report %q:\n%s", tc.want, plain(m.View()))
			}
			if m.cursor.active || m.panelOverlaid() {
				t.Errorf("bare /panel parked a cursor (active %v, region %v, overlaid %v)",
					m.cursor.active, m.cursor.region, m.panelOverlaid())
			}
		})
	}
}

// TestSidePanelCommandIsRefusedWhileWatching pins /panel outside the watch verbs: a watch is
// read-only, so each writing form flashes the refusal and the viewer's settings stay as they were.
func TestSidePanelCommandIsRefusedWhileWatching(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"/panel on", "/panel width 40", "/panel width reset"} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			m := enterWatchOn(t, watchEngine(), newWatchFixture(t, "watched", nil, 0))
			m.opts.UI.SidePanelWidth = panelCommandWidth
			before := m.opts.UI

			m = sendLine(t, m, line)

			if m.flash != watchRefusedFlash || m.opts.UI != before {
				t.Errorf("flash %q, settings %+v; want the refusal and %+v kept", m.flash, m.opts.UI, before)
			}
		})
	}
}

// panelResumeWorkspace is the workspace the resume tests' writes land in.
const panelResumeWorkspace = "/w"

// panelResumeUsage is the resumed record's accounting: a fill past the usage section's live rule,
// and the totals its in/out row states.
var panelResumeUsage = session.Usage{Calls: 3, PromptTokens: 9000, CompletionTokens: 700, TotalTokens: 9700}

// panelTaskListPair is a task_list call by base's run that came back with items, the result
// content internal/tasklist renders for them.
func panelTaskListPair(t *testing.T, base domain.EventBase, id string, items ...tasklist.Item) []domain.Event {
	t.Helper()
	list := tasklist.New()
	if err := list.Replace(items); err != nil {
		t.Fatalf("replace the task list: %v", err)
	}
	return []domain.Event{
		domain.ToolCallEvent{EventBase: base, Call: domain.ToolCall{ID: id, Tool: "task_list", Arguments: []byte(`{"tasks":[]}`)}},
		domain.ToolResultEvent{EventBase: base, Result: domain.ToolResult{CallID: id, Content: list.Render()}},
	}
}

// panelResumeRecord is the stored scrollback of a session that kept a task list, had a child keep
// one of its own, failed a later task_list call, and wrote two files — one argument absolute, one
// relative to the workspace.
func panelResumeRecord(t *testing.T) []byte {
	t.Helper()
	opts := testOpts
	opts.Workspace = panelResumeWorkspace
	src := newModel(context.Background(), &fakeEngine{}, opts, nil)
	failed := domain.ToolResultEvent{Result: domain.ToolResult{CallID: "tl2", Content: "too many tasks", IsError: true}}
	events := panelEvents(
		panelTaskListPair(t, domain.EventBase{}, "tl1",
			tasklist.Item{Text: "read the plan", Done: true}, tasklist.Item{Text: "write the code"}),
		[]domain.Event{
			panelDelegationCall(t, "r1", "scout"),
			delegationPhase("r1", domain.SubAgentStarted, time.Minute),
		},
		panelTaskListPair(t, delegationBase("r1"), "c1", tasklist.Item{Text: "child's task"}),
		[]domain.Event{
			domain.ToolCallEvent{Call: domain.ToolCall{ID: "tl2", Tool: "task_list", Arguments: []byte(`{"tasks":[]}`)}},
			failed,
		},
		panelWrite(domain.EventBase{}, "w1", panelResumeWorkspace+"/src/a.go"),
		panelToolPair(domain.EventBase{}, "w2", "write_file", `{"path":"src/b.go","content":"x\n"}`, panelResumeWorkspace+"/src/b.go"),
	)
	for _, e := range events {
		src = src.foldEvent(e)
	}
	blob, err := encodeTranscript(&src.transcript)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	return blob
}

// assertResumedPanel checks the panel a resume rebuilt: the session's own task list (not the
// child's, not the failed call's), both writes keyed workspace-relative, sub-agents empty.
func assertResumedPanel(t *testing.T, m Model) {
	t.Helper()
	tasks := m.panel.Tasks(sidepanel.RunKey{}).Rows
	if len(tasks) != 2 || tasks[0].Text != "read the plan" || !tasks[0].IsDone || tasks[1].Text != "write the code" {
		t.Errorf("the session's tasks = %+v, want its own two-task list", tasks)
	}
	if child := m.panel.Tasks(sidepanel.RunOf(delegationBase("r1"))).Rows; len(child) != 1 || child[0].Text != "child's task" {
		t.Errorf("the child's tasks = %+v, want its own one task", child)
	}
	files := m.panel.ChangedFiles(sidepanel.RunKey{}).Rows
	if len(files) != 2 || files[0].Path != "src/a.go" || files[1].Path != "src/b.go" || files[0].CallID != "w1" {
		t.Errorf("changed files = %+v, want src/a.go (w1) and src/b.go", files)
	}
	if view := m.panel.SubAgents(); len(view.Rows) != 0 {
		t.Errorf("sub-agents = %+v, want it empty after a resume", view.Rows)
	}
	column := strings.Join(panelText(t, m), "\n")
	for _, want := range []string{"Tasks", "write the code", "Changed files", "src/b.go"} {
		if !strings.Contains(column, want) {
			t.Errorf("the column paints no %q:\n%s", want, column)
		}
	}
	if strings.Contains(column, "Sub-agents") {
		t.Errorf("the column paints a sub-agents section after a resume:\n%s", column)
	}
}

// TestSidePanelResumeRebuildsThroughBothDoors resumes one record through each door — a start-up
// --resume and a /sessions restore — and finds the same panel rebuilt from it, its usage section
// reading the record's accounting, and a file rewritten live afterwards kept on its one row.
func TestSidePanelResumeRebuildsThroughBothDoors(t *testing.T) {
	t.Parallel()
	blob := panelResumeRecord(t)
	opts := testOpts
	opts.Workspace = panelResumeWorkspace
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	inOut := "prompt " + format.Tokens(panelResumeUsage.PromptTokens) + sidePanelPartSeparator +
		"completion " + format.Tokens(panelResumeUsage.CompletionTokens)

	t.Run("start-up resume", func(t *testing.T) {
		t.Parallel()
		startup := opts
		startup.Resumed = &ResumedSession{Transcript: blob, Title: "t", CtxUsed: panelUsed, Usage: panelResumeUsage}
		m := newModel(context.Background(), &fakeEngine{}, startup, nil)
		m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

		assertResumedPanel(t, m)
		if totals := m.panel.Usage(sidepanel.RunKey{}).Totals; totals.PromptTokens != panelResumeUsage.PromptTokens {
			t.Errorf("usage totals = %+v, want the record's", totals)
		}

		m = m.foldEvent(panelWrite(domain.EventBase{}, "w9", panelResumeWorkspace+"/src/a.go")[0])
		m = m.foldEvent(panelWrite(domain.EventBase{}, "w9", panelResumeWorkspace+"/src/a.go")[1])
		if files := m.panel.ChangedFiles(sidepanel.RunKey{}).Rows; len(files) != 2 || files[0].CallID != "w9" {
			t.Errorf("after a live rewrite changed files = %+v, want two rows, src/a.go's naming w9", files)
		}
	})

	t.Run("/sessions restore", func(t *testing.T) {
		t.Parallel()
		m := newModel(context.Background(), &fakeEngine{}, opts, nil)
		m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
		m = m.foldEvent(taskList(0, "outgoing task"))

		m = step(t, m, sessionLoadedMsg{rec: session.Record{
			Meta:       session.Meta{ID: "sess-1", Title: "t", CtxUsed: panelUsed, Usage: panelResumeUsage},
			Session:    domain.Session{Version: domain.SessionVersion},
			Transcript: blob,
		}})

		assertResumedPanel(t, m)
		column := strings.Join(panelText(t, m), "\n")
		if !strings.Contains(column, inOut) {
			t.Errorf("the usage section paints no %q:\n%s", inOut, column)
		}
		if strings.Contains(column, "outgoing task") {
			t.Errorf("the outgoing session's task survived the restore:\n%s", column)
		}
	})
}

// TestSidePanelResumeDropsADeletedFile resumes a record that wrote two files and then deleted one:
// the resumed panel lists the same rows the live one did — the surviving file alone. The workspace
// is a real directory, so the deleted file keys through its nearest existing ancestor on both sides.
func TestSidePanelResumeDropsADeletedFile(t *testing.T) {
	t.Parallel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp workspace: %v", err)
	}
	deleted, kept := filepath.Join(root, "src", "a.go"), filepath.Join(root, "src", "b.go")
	opts := testOpts
	opts.Workspace = root
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	src := newModel(context.Background(), &fakeEngine{}, opts, nil)
	events := panelEvents(
		panelToolPair(domain.EventBase{}, "w1", "write_file", `{"path":"src/a.go","content":"x\n"}`, deleted),
		panelToolPair(domain.EventBase{}, "w2", "write_file", `{"path":"src/b.go","content":"x\n"}`, kept),
		panelToolPair(domain.EventBase{}, "d1", deleteFileToolName, `{"path":"src/a.go"}`, deleted),
	)
	for _, e := range events {
		src = src.foldEvent(e)
	}
	live := src.panel.ChangedFiles(sidepanel.RunKey{}).Rows
	if len(live) != 1 || live[0].Path != filepath.Join("src", "b.go") {
		t.Fatalf("live changed files = %+v, want src/b.go alone", live)
	}
	blob, err := encodeTranscript(&src.transcript)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	opts.Resumed = &ResumedSession{Transcript: blob, Title: "t"}

	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	if resumed := m.panel.ChangedFiles(sidepanel.RunKey{}).Rows; !slices.Equal(resumed, live) {
		t.Errorf("resumed changed files = %+v, want the live session's %+v", resumed, live)
	}
}

// symlinkedWorkspace makes a real workspace directory and a symlink to it, returning both spelled
// with every other symlink already resolved, so a temp directory that itself sits under one (macOS's
// /var) cannot stand in for the link under test.
func symlinkedWorkspace(t *testing.T) (link, realRoot string) {
	t.Helper()
	realRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp workspace: %v", err)
	}
	linkDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp link directory: %v", err)
	}
	link = filepath.Join(linkDir, "workspace")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	return link, realRoot
}

// TestPanelWriteKeyUnderSymlinkedRoot keys one file one way whatever spelling reaches the panel —
// relative, absolute through the symlinked root, absolute through the real root the engine's
// WriteTarget names — keeps a file outside the workspace absolute, and with no workspace only
// cleans the path.
func TestPanelWriteKeyUnderSymlinkedRoot(t *testing.T) {
	t.Parallel()
	link, realRoot := symlinkedWorkspace(t)
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the outside directory: %v", err)
	}

	cases := []struct {
		name      string
		workspace string
		path      string
		want      string
	}{
		{"relative", link, "src/a.go", "src/a.go"},
		{"through the symlinked root", link, link + "/src/a.go", "src/a.go"},
		{"through the real root", link, realRoot + "/src/a.go", "src/a.go"},
		{"uncleaned through the symlinked root", link, link + "/src/../src/a.go", "src/a.go"},
		{"outside the workspace", link, outsideDir + "/x.go", outsideDir + "/x.go"},
		{"no workspace, relative", "", "src/./a.go", "src/a.go"},
		{"no workspace, absolute", "", "/elsewhere/../x.go", "/x.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := testOpts
			opts.Workspace = tc.workspace
			m := newModel(context.Background(), &fakeEngine{}, opts, nil)

			got := m.panelWriteKey(tc.path)

			if got != tc.want {
				t.Errorf("panelWriteKey(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestSidePanelResumeThenLiveWriteUnderSymlinkedRootIsOneRow resumes a record whose write_file call
// spelled its path relative, under a workspace reached through a symlink, then lands a live rewrite
// of the same file whose WriteTarget is the engine's real path: one changed-files row, naming the
// live call.
func TestSidePanelResumeThenLiveWriteUnderSymlinkedRootIsOneRow(t *testing.T) {
	t.Parallel()
	link, realRoot := symlinkedWorkspace(t)
	opts := testOpts
	opts.Workspace = link
	opts.UI.SidePanelShow = domain.SidePanelShowAuto
	src := newModel(context.Background(), &fakeEngine{}, opts, nil)
	for _, e := range panelToolPair(domain.EventBase{}, "w1", "write_file", `{"path":"src/a.go","content":"x\n"}`, realRoot+"/src/a.go") {
		src = src.foldEvent(e)
	}
	blob, err := encodeTranscript(&src.transcript)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	opts.Resumed = &ResumedSession{Transcript: blob, Title: "t"}
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	for _, e := range panelToolPair(domain.EventBase{}, "w9", "write_file", `{"path":"src/a.go","content":"y\n"}`, realRoot+"/src/a.go") {
		m = m.foldEvent(e)
	}

	if files := m.panel.ChangedFiles(sidepanel.RunKey{}).Rows; len(files) != 1 || files[0].Path != "src/a.go" || files[0].CallID != "w9" {
		t.Errorf("changed files = %+v, want one src/a.go row naming w9", files)
	}
}
