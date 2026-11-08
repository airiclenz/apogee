package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
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
// width stand where they were — while a change of the show mode or of the width moves both, once.
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
	if got, want := narrow.transcriptWidth(), wrap+sidePanelWidth-domain.MinSidePanelWidth; got != want {
		t.Errorf("narrowing the panel wraps the transcript at %d columns, want %d", got, want)
	}
}

// TestSidePanelPaintedWidth pins the clamp the configured width paints through: an unset width
// paints the default, a stated one paints as set while the window leaves the show mode's
// transcript floor and the scroll-bar column beside it, and is held to that room — never below the
// minimum — otherwise; the show gate counts the default width whatever is configured. The slot is
// the window less the painted width, and the setting is never rewritten by the clamp.
func TestSidePanelPaintedWidth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		show   domain.SidePanelShow
		stored int
		window int
		want   int
	}{
		{name: "unset paints the default", show: domain.SidePanelShowAuto, stored: 0, window: 140, want: 36},
		{name: "stated fits", show: domain.SidePanelShowAuto, stored: 60, window: 200, want: 60},
		{name: "minimum fits", show: domain.SidePanelShowOn, stored: 24, window: 117, want: 24},
		{name: "auto floor caps", show: domain.SidePanelShowAuto, stored: 60, window: 140, want: 39},
		{name: "on floor caps", show: domain.SidePanelShowOn, stored: 60, window: 130, want: 49},
		{name: "below the minimum floors", show: domain.SidePanelShowAuto, stored: 10, window: 140, want: 24},
		{name: "auto gate ignores a narrow width", show: domain.SidePanelShowAuto, stored: 24, window: 136, want: 0},
		{name: "on gate ignores a narrow width", show: domain.SidePanelShowOn, stored: 24, window: 116, want: 0},
		{name: "auto gate ignores a wide width", show: domain.SidePanelShowAuto, stored: 60, window: 137, want: 36},
		{name: "off never paints", show: domain.SidePanelShowOff, stored: 60, window: 200, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			opts := testOpts
			opts.UI.SidePanelShow = tc.show
			opts.UI.SidePanelWidth = tc.stored
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

// panelText is the column's content as the frame paints it: one escape-stripped, right-trimmed
// string per row above the session-name rule, the trailing blank rows dropped.
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
		column = append(column, strings.TrimRight(ansi.Cut(ln, slot+2, m.width), " "))
	}
	t.Fatal("no ▔ rule below the slot's rows")
	return nil
}

// columnText is sidePanelColumn's rows rows, escape-stripped and right-trimmed.
func columnText(m Model, rows int) []string {
	column := m.sidePanelColumn(rows)
	for i, row := range column {
		column[i] = strings.TrimRight(ansi.Cut(ansi.Strip(row), sidePanelChromeWidth, m.panelWidth()), " ")
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
// live, as docs/layout/side-panel-layout.md sketches it: the header row with its summary and the
// unpinned ⊙ at the right edge, then the section's rows.
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
				"◆ Sub-agents 1 queued            ⊙",
				"✓ planner      done       38s",
				"○ repo-scout   queued",
			},
		},
		{
			name: "tasks",
			events: func(*testing.T) []domain.Event {
				return []domain.Event{taskList(1, "map call sites", "split the tokenizer", "port the tests")}
			},
			want: []string{
				"◆ Tasks 1/3                      ⊙",
				"✓ map call sites",
				"▸ split the tokenizer",
				"○ port the tests",
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
				"◆ Changed files 2                ⊙",
				"internal/parse/parser.go    +12 −4",
				"…/lexer_test.go                  ↳",
			},
		},
		{
			name: "usage",
			events: func(*testing.T) []domain.Event {
				return []domain.Event{panelUsageReading(panelUsed), standingCost(200, 100)}
			},
			want: []string{
				"◆ Usage 62%                      ⊙",
				"██████▎    20k/32k",
				"sys 200 · tools 100 · hist 20k",
				"in 4k · out 300",
				"cache hit 25%",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := panelModel(t, domain.DefaultSidePanelSections, 30, tc.events(t)...)

			if got := panelText(t, m); !slices.Equal(got, tc.want) {
				t.Errorf("the column paints\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// TestSidePanelRunningDelegationSpinsWithTheStatusLine pins a running row: the status line's own
// spinner frame leads it, and its elapsed time counts to now.
func TestSidePanelRunningDelegationSpinsWithTheStatusLine(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30,
		panelDelegationCall(t, "r1", "repo-scout"), delegationPhase("r1", domain.SubAgentStarted, 72*time.Second))

	got := panelText(t, m)
	want := ansi.Strip(m.spin.drawn(m.th, m.opts.UI.Spinner)) + " repo-scout   running 01:12s"
	if len(got) != 2 || got[0] != "◆ Sub-agents 1 running           ⊙" || got[1] != want {
		t.Errorf("the column paints\n%s\nwant the running header over %q", strings.Join(got, "\n"), want)
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
			want:     []string{"◆ Usage                          ●"},
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
			want:     []string{"◆ Tasks 0/1                      ⊙", "▸ one", "◆ Usage                          ●"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := panelModel(t, tc.sections, 30, tc.events...)

			if got := panelText(t, m); !slices.Equal(got, tc.want) {
				t.Errorf("the column paints\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
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
			rows:     6,
			want: []string{
				"◆ Sub-agents 1 queued            ⊙",
				"○ scout        queued",
				"◆ Tasks 0/3                      ⊙",
				"◆ Changed files 1                ●",
				"a.go",
				"",
			},
		},
		{
			name:     "a later section that fits still expands",
			sections: "[tasks: auto, sub-agents: auto, changed-files: hidden, usage: hidden]",
			rows:     4,
			want: []string{
				"◆ Tasks 0/3                      ⊙",
				"◆ Sub-agents 1 queued            ⊙",
				"○ scout        queued",
				"",
			},
		},
		{
			name:     "every header fits exactly",
			sections: domain.DefaultSidePanelSections,
			rows:     4,
			want: []string{
				"◆ Sub-agents 1 queued            ⊙",
				"◆ Tasks 0/3                      ⊙",
				"◆ Changed files 1                ⊙",
				"◆ Usage 62%                      ⊙",
			},
		},
		{
			name:     "headers that do not fit",
			sections: domain.DefaultSidePanelSections,
			rows:     3,
			want: []string{
				"◆ Sub-agents 1 queued            ⊙",
				"◆ Tasks 0/3                      ⊙",
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

// TestSidePanelClearEmptiesThePanel pins the /clear boundary: the panel the closed session folded
// falls with it, and the column reads `nothing live` again.
func TestSidePanelClearEmptiesThePanel(t *testing.T) {
	t.Parallel()

	m := panelModel(t, domain.DefaultSidePanelSections, 30, taskList(0, "one"))

	m, _ = sendPrompt(t, m, "/clear")

	if m.panel != nil {
		t.Error("/clear kept the closed session's panel state")
	}
	if got := panelText(t, m); !slices.Equal(got, []string{sidePanelEmpty}) {
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
	for _, want := range []string{"▸ scout the parser", "child.go", "Changed files 1", "Usage 73%", figures} {
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
			if len(got) < 2 || !strings.HasSuffix(got[1], tc.want) {
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
	// A legacy call names no run, so the rows are unnamed: the second row is the second run's.
	if len(got) < 3 || strings.Contains(got[1], glyphPanelCurrent) || !strings.Contains(got[2], glyphPanelCurrent) {
		t.Errorf("sub-agents does not mark the viewed legacy run alone:\n%s", strings.Join(got, "\n"))
	}
}

// panelToolPair is one finished call by base's run — its call, then its result — landing a write
// to target where target is not "".
func panelToolPair(base domain.EventBase, id, tool, args, target string) []domain.Event {
	return []domain.Event{
		domain.ToolCallEvent{EventBase: base, Call: domain.ToolCall{ID: id, Tool: tool, Arguments: []byte(args)}},
		domain.ToolResultEvent{
			EventBase: base, WriteTarget: target,
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
// separator and the gutter.
func panelBodyX(m Model) int { return m.slotWidth() + 4 }

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

// settingsSpy is a settings host that records every write it is handed: the section writes it
// answers with the list a fresh file would resolve to, and any other write.
type settingsSpy struct {
	sections []domain.SidePanelEntry
	writes   []string
}

// host is the spy as a SettingsHost, its section write failing with failure where one is given.
func (s *settingsSpy) host(failure error) fakeSettingsHost {
	return fakeSettingsHost{
		write: func(path, value string) error {
			s.writes = append(s.writes, path+"="+value)
			return nil
		},
		writeSection: func(section domain.SidePanelSection, mode domain.SidePanelMode) (domain.SidePanelSections, error) {
			s.sections = append(s.sections, domain.SidePanelEntry{Section: section, Mode: mode})
			if failure != nil {
				return "", failure
			}
			return withSectionMode(domain.DefaultSidePanelSections, section, mode), nil
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
			row:  "cache hit",
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

// TestSidePanelClickHeaderTitleBlankAndWheelDoNothing pins the cells that target nothing: the
// header title, a blank cell below the sections and a wheel notch over the column leave the model
// as it was — no view opened, no section toggled, the transcript not scrolled.
func TestSidePanelClickHeaderTitleBlankAndWheelDoNothing(t *testing.T) {
	t.Parallel()
	m := streamOneScreen(t, panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...))
	m = step(t, m, tea.MouseWheelMsg{X: 4, Y: 2, Button: tea.MouseWheelUp}) // off the tail, so a notch could move it
	offset, sections := m.viewport.YOffset(), m.opts.UI.SidePanelSections
	header := panelRowY(t, m, "Tasks")

	for _, cell := range [][2]int{{panelBodyX(m), header}, {panelBodyX(m), m.transcriptRows() - 1}} {
		m = clickCell(t, m, cell[0], cell[1])
	}
	m = step(t, m, tea.MouseWheelMsg{X: panelBodyX(m), Y: header, Button: tea.MouseWheelUp})

	if m.viewport.YOffset() != offset || m.opts.UI.SidePanelSections != sections || m.inRunView() {
		t.Errorf("offset %d→%d, sections %q→%q, in view %v; want all unchanged",
			offset, m.viewport.YOffset(), sections, m.opts.UI.SidePanelSections, m.inRunView())
	}
	if m.transcriptSel.active || m.sel.active {
		t.Error("a click on the column armed a selection")
	}
}

// TestSidePanelClickHoverShapeMatches pins the pointer shape over the column to what a click there
// does: the hand over a body row and the pin glyph, the arrow over a header title and a blank cell.
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
		{"pin glyph", m.width - 1, header, shapePointer},
		{"header title", panelBodyX(m), header, shapeDefault},
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

// TestSidePanelPinTogglesAndWritesBack drives the pin glyph: a click on an auto section's ⊙ pins
// it, through the settings host, and lands the list the host answers; a second click unpins it. A
// failed write leaves the section pinned on screen and says so in the transcript.
func TestSidePanelPinTogglesAndWritesBack(t *testing.T) {
	t.Parallel()

	t.Run("pin then unpin", func(t *testing.T) {
		t.Parallel()
		m := panelModel(t, domain.DefaultSidePanelSections, 30, panelTaskCard()...)
		spy := &settingsSpy{}
		m.opts.Settings = spy.host(nil)

		m = clickCell(t, m, m.width-1, panelRowY(t, m, "Tasks"))
		pinned := m.opts.UI.SidePanelSections
		m = clickCell(t, m, m.width-1, panelRowY(t, m, "Tasks"))

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

		m = clickCell(t, m, m.width-1, panelRowY(t, m, "Tasks"))

		if !strings.Contains(panelText(t, m)[panelRowY(t, m, "Tasks")], glyphPanelPinned) {
			t.Errorf("the header does not wear %s after the pin:\n%s", glyphPanelPinned, strings.Join(panelText(t, m), "\n"))
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

	m := sidePanelModel(t, domain.SidePanelShowAuto, true, 200)
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

// TestSidePanelSeparatorDrag pins the resize drag (adr-0110-amend-1108 (b), (c)): a press on the
// separator or the gutter right of it grabs it, each held-button motion re-wraps the frame live at
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
		if m.panelWidth() != 50 || m.slotWidth() != sep-14 {
			t.Errorf("panel %d, slot %d; want 50 and %d", m.panelWidth(), m.slotWidth(), sep-14)
		}
		if want := []string{domain.UIKeySidePanelWidth + "=50"}; !slices.Equal(spy.writes, want) {
			t.Errorf("writes %v, want %v", spy.writes, want)
		}
		if len(m.settingEdits) != 1 || m.settingEdits[0].path != domain.UIKeySidePanelWidth {
			t.Errorf("journal %+v, want one %s entry", m.settingEdits, domain.UIKeySidePanelWidth)
		}
	})

	t.Run("the gutter grabs it at its offset", func(t *testing.T) {
		t.Parallel()
		m, spy, sep := separatorDragModel(t, nil)

		m = step(t, m, leftClick(sep+1, separatorRow))
		m = step(t, m, leftDrag(sep-9, separatorRow))
		m = step(t, m, leftRelease(sep-9, separatorRow))

		if m.slotWidth() != sep-10 {
			t.Errorf("slot %d, want %d: the separator keeps one column left of the pointer", m.slotWidth(), sep-10)
		}
		if want := []string{domain.UIKeySidePanelWidth + "=46"}; !slices.Equal(spy.writes, want) {
			t.Errorf("writes %v, want %v", spy.writes, want)
		}
	})

	t.Run("a drag past either limit clamps", func(t *testing.T) {
		t.Parallel()
		m, spy, sep := separatorDragModel(t, nil)

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(0, separatorRow))
		widest := m.panelWidth()
		m = step(t, m, leftDrag(m.width-1, separatorRow))
		m = step(t, m, leftRelease(m.width-1, separatorRow))

		if widthCap := m.width - sidePanelAutoFloor - scrollbarWidth; widest != widthCap {
			t.Errorf("dragged to the left edge the panel is %d, want the cap %d", widest, widthCap)
		}
		if m.panelWidth() != domain.MinSidePanelWidth {
			t.Errorf("dragged to the right edge the panel is %d, want the minimum %d", m.panelWidth(), domain.MinSidePanelWidth)
		}
		if want := []string{domain.UIKeySidePanelWidth + "=24"}; !slices.Equal(spy.writes, want) {
			t.Errorf("writes %v, want %v", spy.writes, want)
		}
	})

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
		opts.UI.SidePanelWidth = 60
		spy := &settingsSpy{}
		opts.Settings = spy.host(nil)
		m := newModel(context.Background(), &fakeEngine{}, opts, nil)
		m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 30})
		sep := m.slotWidth()

		m = step(t, m, leftClick(sep, separatorRow))
		m = step(t, m, leftDrag(sep, separatorRow))
		m = step(t, m, leftDrag(sep+9, separatorRow))
		m = step(t, m, leftDrag(sep, separatorRow))
		m = step(t, m, leftRelease(sep, separatorRow))

		if len(spy.writes) != 0 || m.opts.UI.SidePanelWidth != 60 {
			t.Errorf("writes %v, stored %d; want none and the stored 60 the window clamps to %d",
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
		if m.panelWidth() != 50 || len(m.settingEdits) != 0 {
			t.Errorf("panel %d, journal %+v; want the dragged 50 standing and nothing journalled", m.panelWidth(), m.settingEdits)
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

		if len(spy.writes) != 0 || m.opts.UI.SidePanelWidth != stored || m.panelWidth() != sidePanelWidth {
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

		if m.panelDrag.active || m.mousePressed || m.panelWidth() != 50 {
			t.Errorf("drag %+v, pressed %v, panel %d; want the SGR-35 release to end it at 50", m.panelDrag, m.mousePressed, m.panelWidth())
		}
		if want := []string{domain.UIKeySidePanelWidth + "=50"}; !slices.Equal(spy.writes, want) {
			t.Errorf("writes %v, want %v", spy.writes, want)
		}
	})

	t.Run("the separator is lit while the drag holds it", func(t *testing.T) {
		t.Parallel()
		m, _, sep := separatorDragModel(t, nil)
		lit := m.th.panelDragRule.Render(sidePanelSeparator)
		if lit == m.th.statusFaint.Render(sidePanelSeparator) {
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

// panelHelpWords reads the /panel line /help prints and returns its two word groups — the show
// modes and the section verbs — so the tests drive exactly the strings the human is shown.
func panelHelpWords(t *testing.T) (shows, verbs []string) {
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
	if len(groups) != 2 {
		t.Fatalf("the /help line %q names %d word groups, want the show modes and the section verbs", line, len(groups))
	}
	return groups[0], groups[1]
}

// noteHolding reports whether the transcript holds a note containing text.
func noteHolding(m Model, text string) bool {
	return slices.ContainsFunc(m.transcript.entries, func(e entry) bool {
		return e.kind == entryNote && strings.Contains(e.text, text)
	})
}

// TestSidePanelCommandDrivesTheSettings drives every /panel form /help lists: each show mode sets
// and writes `ui.side-panel.show` and moves the column at once; each section verb sets that
// section's mode through the section writer, `unpin` meaning `auto`; and each answers with the
// settings now in force.
func TestSidePanelCommandDrivesTheSettings(t *testing.T) {
	t.Parallel()
	shows, verbs := panelHelpWords(t)

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

// TestSidePanelCommandRefusesAnUnknownSection pins the refusal: a section this build does not know
// is named back with the sections there are and the usage line, and nothing is written or changed.
func TestSidePanelCommandRefusesAnUnknownSection(t *testing.T) {
	t.Parallel()
	m := sidePanelModel(t, domain.SidePanelShowAuto, true, 140)
	spy := &settingsSpy{}
	m.opts.Settings = spy.host(nil)
	before := m.opts.UI

	m = sendLine(t, m, "/panel pin mcp")

	if !noteHolding(m, `unknown side-panel section "mcp" (known sections: sub-agents, tasks, changed-files, usage). `+panelUsage) {
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

// TestSidePanelCommandBareReports pins the bare form: it reports the show mode and every section's
// mode in display order, writes nothing, says when the window is too narrow to draw the column,
// and answers mid-run where the writing forms wait for idle.
func TestSidePanelCommandBareReports(t *testing.T) {
	t.Parallel()
	m := sidePanelModel(t, domain.SidePanelShowAuto, true, 100)
	m.opts.UI.SidePanelSections = "[usage: pinned, tasks: hidden]"
	spy := &settingsSpy{}
	m.opts.Settings = spy.host(nil)

	m = sendLine(t, m, "/panel")

	want := "side panel: auto — usage pinned · tasks hidden · sub-agents auto · changed-files auto" +
		" (not drawn: the window is narrower than 137 columns)"
	if !noteHolding(m, want) {
		t.Errorf("no report %q:\n%s", want, plain(m.View()))
	}
	if len(spy.sections) != 0 || len(spy.writes) != 0 {
		t.Errorf("the report wrote %v / %v", spy.sections, spy.writes)
	}
	for line, wantSafe := range map[string]bool{"/panel": true, "/panel on": false, "/panel pin tasks": false} {
		if got := parseInput(line, nil).safeWhileRunning(); got != wantSafe {
			t.Errorf("%q safe while running = %v, want %v", line, got, wantSafe)
		}
	}
}

// TestSidePanelCommandIsRefusedWhileWatching pins /panel outside the watch verbs: a watch is
// read-only, so the line flashes the refusal and the viewer's settings stay as they were.
func TestSidePanelCommandIsRefusedWhileWatching(t *testing.T) {
	t.Parallel()
	m := enterWatchOn(t, watchEngine(), newWatchFixture(t, "watched", nil, 0))
	before := m.opts.UI.SidePanelShow

	m = sendLine(t, m, "/panel on")

	if m.flash != watchRefusedFlash || m.opts.UI.SidePanelShow != before {
		t.Errorf("flash %q, show %q; want the refusal and the show mode %q kept", m.flash, m.opts.UI.SidePanelShow, before)
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
	inOut := "in " + panelTokens(panelResumeUsage.PromptTokens) + sidePanelPartSeparator +
		"out " + panelTokens(panelResumeUsage.CompletionTokens)

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
	opts := testOpts
	opts.Workspace = link
	linked := newModel(context.Background(), &fakeEngine{}, opts, nil)
	opts.Workspace = ""
	unrooted := newModel(context.Background(), &fakeEngine{}, opts, nil)

	cases := []struct {
		name string
		m    Model
		path string
		want string
	}{
		{"relative", linked, "src/a.go", "src/a.go"},
		{"through the symlinked root", linked, link + "/src/a.go", "src/a.go"},
		{"through the real root", linked, realRoot + "/src/a.go", "src/a.go"},
		{"uncleaned through the symlinked root", linked, link + "/src/../src/a.go", "src/a.go"},
		{"outside the workspace", linked, outsideDir + "/x.go", outsideDir + "/x.go"},
		{"no workspace, relative", unrooted, "src/./a.go", "src/a.go"},
		{"no workspace, absolute", unrooted, "/elsewhere/../x.go", "/x.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.m.panelWriteKey(tc.path)

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
