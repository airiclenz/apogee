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

// panelUsageReading is a top-level reading of used tokens of window, its cumulative totals beside it.
func panelUsageReading(used, window int) domain.UsageEvent {
	return domain.UsageEvent{
		PromptTokens: used - 20, CompletionTokens: 20, TotalTokens: used, ContextWindow: window,
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
				return []domain.Event{panelUsageReading(620, 1000), standingCost(200, 100)}
			},
			want: []string{
				"◆ Usage 62%                      ⊙",
				"██████▎    620/1k",
				"sys 200 · tools 100 · hist 320",
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
			panelUsageReading(620, 1000),
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
