package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/sidepanel"
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
		column[i] = strings.TrimRight(ansi.Cut(ansi.Strip(row), 2, sidePanelWidth), " ")
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
