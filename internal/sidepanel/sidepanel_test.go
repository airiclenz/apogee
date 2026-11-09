package sidepanel_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sidepanel"
	"github.com/airiclenz/apogee/internal/testtime"
)

// fakeClock is the injected clock: it stands still until a test advances it.
type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time { return c.at }

func (c *fakeClock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newState(t *testing.T) (*sidepanel.State, *fakeClock) {
	t.Helper()
	clock := &fakeClock{at: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	return sidepanel.New(clock.now), clock
}

// child is the base every event of the delegation with run id id carries.
func child(id string) domain.EventBase {
	return domain.EventBase{Depth: 1, CallID: "call-" + id, RunID: id}
}

// spawn is the parent's sub_agent call for the delegation id, with the given arguments.
func spawn(t *testing.T, id string, args map[string]any) domain.ToolCallEvent {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return domain.ToolCallEvent{
		Call:       domain.ToolCall{ID: "call-" + id, Tool: "sub_agent", Arguments: raw},
		SpawnRunID: id,
	}
}

func phase(id string, p domain.SubAgentPhase, at time.Time, isError bool) domain.SubAgentPhaseEvent {
	return domain.SubAgentPhaseEvent{
		EventBase: child(id),
		Phase:     p,
		At:        at,
		Result:    domain.ToolResult{IsError: isError},
	}
}

func TestSubAgents_Rows(t *testing.T) {
	cases := []struct {
		name        string
		events      func(t *testing.T, at time.Time) []domain.Event
		wantName    string
		wantPhase   sidepanel.Phase
		wantElapsed time.Duration
	}{
		{
			name: "named call queued",
			events: func(t *testing.T, at time.Time) []domain.Event {
				return []domain.Event{spawn(t, "r.1", map[string]any{"name": "repo-scout", "task": "map it"})}
			},
			wantName: "repo-scout", wantPhase: sidepanel.PhaseQueued,
		},
		{
			name: "self-named call falls back to the task's first line",
			events: func(t *testing.T, at time.Time) []domain.Event {
				return []domain.Event{spawn(t, "r.1", map[string]any{"task": "  map the parser\nthen more"})}
			},
			wantName: "map the parser", wantPhase: sidepanel.PhaseQueued,
		},
		{
			name: "control-character-only name falls back to the task",
			events: func(t *testing.T, at time.Time) []domain.Event {
				return []domain.Event{spawn(t, "r.1", map[string]any{"name": "\x1b[31m\x07\r", "task": "port the tests"})}
			},
			wantName: "port the tests", wantPhase: sidepanel.PhaseQueued,
		},
		{
			name: "generated name replaces the task line",
			events: func(t *testing.T, at time.Time) []domain.Event {
				return []domain.Event{
					spawn(t, "r.1", map[string]any{"task": "port the tests"}),
					domain.SubAgentNamedEvent{EventBase: child("r.1"), Name: "test-porter"},
				}
			},
			wantName: "test-porter", wantPhase: sidepanel.PhaseQueued,
		},
		{
			name: "running row measures to now",
			events: func(t *testing.T, at time.Time) []domain.Event {
				return []domain.Event{
					spawn(t, "r.1", map[string]any{"name": "a"}),
					phase("r.1", domain.SubAgentStarted, at.Add(-72*time.Second), false),
				}
			},
			wantName: "a", wantPhase: sidepanel.PhaseRunning, wantElapsed: 72 * time.Second,
		},
		{
			name: "finished row measures start to end",
			events: func(t *testing.T, at time.Time) []domain.Event {
				return []domain.Event{
					spawn(t, "r.1", map[string]any{"name": "a"}),
					phase("r.1", domain.SubAgentStarted, at.Add(-60*time.Second), false),
					phase("r.1", domain.SubAgentFinished, at.Add(-22*time.Second), false),
				}
			},
			wantName: "a", wantPhase: sidepanel.PhaseDone, wantElapsed: 38 * time.Second,
		},
		{
			name: "error result fails the row",
			events: func(t *testing.T, at time.Time) []domain.Event {
				return []domain.Event{
					spawn(t, "r.1", map[string]any{"name": "a"}),
					phase("r.1", domain.SubAgentStarted, at, false),
					phase("r.1", domain.SubAgentFinished, at, true),
				}
			},
			wantName: "a", wantPhase: sidepanel.PhaseFailed,
		},
		{
			name: "result closes a delegation no phase reached",
			events: func(t *testing.T, at time.Time) []domain.Event {
				return []domain.Event{
					spawn(t, "r.1", map[string]any{"name": "a"}),
					domain.ToolResultEvent{SpawnRunID: "r.1", Result: domain.ToolResult{CallID: "call-r.1"}},
				}
			},
			wantName: "a", wantPhase: sidepanel.PhaseDone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, clock := newState(t)
			for _, e := range tc.events(t, clock.at) {
				state.Fold(e)
			}

			rows := state.SubAgents().Rows

			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			got := rows[0]
			if got.Name != tc.wantName || got.Phase != tc.wantPhase || got.Elapsed != tc.wantElapsed {
				t.Errorf("row = %q/%v/%v, want %q/%v/%v",
					got.Name, got.Phase, got.Elapsed, tc.wantName, tc.wantPhase, tc.wantElapsed)
			}
		})
	}
}

func TestSubAgents_ZeroAtFallsBackToInjectedNow(t *testing.T) {
	state, clock := newState(t)
	state.Fold(spawn(t, "r.1", map[string]any{"name": "a"}))
	state.Fold(phase("r.1", domain.SubAgentStarted, time.Time{}, false))

	clock.advance(testtime.Fixed(45 * time.Second))

	if got := state.SubAgents().Rows[0].Elapsed; got != 45*time.Second {
		t.Errorf("elapsed = %v, want 45s from the fold-time clock", got)
	}
}

func TestSubAgents_LiveRule(t *testing.T) {
	cases := []struct {
		name     string
		finish   bool
		advance  time.Duration
		wantLive bool
	}{
		{name: "running", finish: false, advance: testtime.Fixed(10 * time.Minute), wantLive: true},
		{name: "inside the linger", finish: true, advance: testtime.Fixed(29 * time.Second), wantLive: true},
		{name: "linger elapsed", finish: true, advance: testtime.Fixed(30 * time.Second), wantLive: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, clock := newState(t)
			state.Fold(spawn(t, "r.1", map[string]any{"name": "a"}))
			state.Fold(phase("r.1", domain.SubAgentStarted, clock.at, false))
			if tc.finish {
				state.Fold(phase("r.1", domain.SubAgentFinished, clock.at, false))
			}

			clock.advance(tc.advance)

			if got := state.SubAgents().IsLive; got != tc.wantLive {
				t.Errorf("live = %v, want %v", got, tc.wantLive)
			}
		})
	}
}

func TestSubAgentLinger(t *testing.T) {
	cases := []struct {
		name    string
		finish  bool
		queue   bool
		advance time.Duration
		want    func(end time.Time) sidepanel.Linger
	}{
		{
			name: "after the last run ends", finish: true, advance: testtime.Fixed(12 * time.Second),
			want: func(end time.Time) sidepanel.Linger {
				return sidepanel.Linger{Until: end.Add(30 * time.Second), Left: 18 * time.Second}
			},
		},
		{
			name: "while one runs", advance: testtime.Fixed(12 * time.Second),
			want: func(time.Time) sidepanel.Linger { return sidepanel.Linger{} },
		},
		{
			name: "while one queues behind an ended one", finish: true, queue: true, advance: testtime.Fixed(12 * time.Second),
			want: func(time.Time) sidepanel.Linger { return sidepanel.Linger{} },
		},
		{
			name: "once the deadline has passed", finish: true, advance: testtime.Fixed(30 * time.Second),
			want: func(time.Time) sidepanel.Linger { return sidepanel.Linger{} },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, clock := newState(t)
			end := clock.at
			state.Fold(spawn(t, "r.1", map[string]any{"name": "a"}))
			state.Fold(phase("r.1", domain.SubAgentStarted, end, false))
			if tc.finish {
				state.Fold(phase("r.1", domain.SubAgentFinished, end, false))
			}
			if tc.queue {
				state.Fold(spawn(t, "r.2", map[string]any{"name": "b"}))
			}

			clock.advance(tc.advance)

			if got, want := state.SubAgentLinger(), tc.want(end); got != want {
				t.Errorf("linger = %+v, want %+v", got, want)
			}
		})
	}
}

func TestSubAgentLinger_NilAndEmpty(t *testing.T) {
	var none *sidepanel.State
	if got := none.SubAgentLinger(); got != (sidepanel.Linger{}) {
		t.Errorf("nil state linger = %+v, want zero", got)
	}
	state, _ := newState(t)
	if got := state.SubAgentLinger(); got != (sidepanel.Linger{}) {
		t.Errorf("empty state linger = %+v, want zero", got)
	}
}

// TestSubAgents_Summary pins the sub-agents header's summary forms: the running count bare,
// another phase named only while non-zero, `N done` once nothing is active — a failed run counted
// there and never named, since its row carries the failure mark — and nothing with no runs. A run
// waiting on an approval counts as the running run it is.
func TestSubAgents_Summary(t *testing.T) {
	started := func(id string, at time.Time) domain.Event { return phase(id, domain.SubAgentStarted, at, false) }
	ended := func(id string, at time.Time, isError bool) domain.Event {
		return phase(id, domain.SubAgentFinished, at, isError)
	}
	cases := []struct {
		name   string
		runs   []string
		events func(at time.Time) []domain.Event
		want   string
	}{
		{
			name: "no runs carry no summary",
			want: "",
		},
		{
			name: "running only is the bare count",
			runs: []string{"r.1", "r.2"},
			events: func(at time.Time) []domain.Event {
				return []domain.Event{started("r.1", at), started("r.2", at)}
			},
			want: "2",
		},
		{
			name: "running and queued names the queued",
			runs: []string{"r.1", "r.2", "r.3"},
			events: func(at time.Time) []domain.Event {
				return []domain.Event{started("r.1", at), started("r.2", at)}
			},
			want: "2 · 1 queued",
		},
		{
			name: "queued only names the queued",
			runs: []string{"r.1"},
			want: "1 queued",
		},
		{
			name: "nothing active counts the done with a failed run unnamed",
			runs: []string{"r.1", "r.2", "r.3"},
			events: func(at time.Time) []domain.Event {
				return []domain.Event{
					started("r.1", at), started("r.2", at), started("r.3", at),
					ended("r.1", at, false), ended("r.2", at, false), ended("r.3", at, true),
				}
			},
			want: "3 done",
		},
		{
			name: "a waiting run counts as running",
			runs: []string{"r.1"},
			events: func(at time.Time) []domain.Event {
				return []domain.Event{started("r.1", at), gate(child("r.1"), domain.ApprovalRequested, "")}
			},
			want: "1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, clock := newState(t)
			for _, id := range tc.runs {
				state.Fold(spawn(t, id, map[string]any{"name": id}))
			}
			if tc.events != nil {
				for _, event := range tc.events(clock.at) {
					state.Fold(event)
				}
			}
			if got := state.SubAgents().Summary; got != tc.want {
				t.Errorf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSubAgents_EmptyStateIsBlank(t *testing.T) {
	state, _ := newState(t)
	if view := state.SubAgents(); view.IsLive || view.Summary != "" || len(view.Rows) != 0 {
		t.Fatalf("empty state answered %+v", view)
	}
}

func TestSubAgents_FillUsesOwnWindowOrFallback(t *testing.T) {
	cases := []struct {
		name        string
		window      int
		maintenance bool
		wantFill    sidepanel.Fill
	}{
		{name: "own window", window: 8000, wantFill: sidepanel.Fill{Used: 4000, Window: 8000}},
		{name: "window 0 falls back", window: 0, wantFill: sidepanel.Fill{Used: 4000, Window: 32000}},
		{name: "maintenance skipped", window: 8000, maintenance: true, wantFill: sidepanel.Fill{Window: 32000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := newState(t)
			state.SetFallbackWindow(32000)
			state.Fold(spawn(t, "r.1", map[string]any{"name": "a"}))

			state.Fold(domain.UsageEvent{
				EventBase: child("r.1"), TotalTokens: 4000, ContextWindow: tc.window,
				Maintenance: tc.maintenance,
			})

			if got := state.SubAgents().Rows[0].Fill; got != tc.wantFill {
				t.Errorf("fill = %+v, want %+v", got, tc.wantFill)
			}
		})
	}
}

func TestTasks_CurrentSummaryAndLive(t *testing.T) {
	cases := []struct {
		name        string
		items       []domain.TaskItem
		wantCurrent int // index of the current row, -1 for none
		wantSummary string
		wantLive    bool
	}{
		{name: "empty", wantCurrent: -1},
		{
			name:        "first unfinished is current",
			items:       []domain.TaskItem{{Text: "a", Done: true}, {Text: "b"}, {Text: "c"}},
			wantCurrent: 1, wantSummary: "1/3", wantLive: true,
		},
		{
			name:        "all done is not live",
			items:       []domain.TaskItem{{Text: "a", Done: true}, {Text: "b", Done: true}},
			wantCurrent: -1, wantSummary: "2/2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := newState(t)
			state.Fold(domain.TaskListEvent{Items: tc.items})

			view := state.Tasks(sidepanel.RunKey{})

			current := -1
			for i, row := range view.Rows {
				if row.IsCurrent {
					current = i
				}
			}
			if current != tc.wantCurrent || view.Summary != tc.wantSummary || view.IsLive != tc.wantLive {
				t.Errorf("current/summary/live = %d/%q/%v, want %d/%q/%v",
					current, view.Summary, view.IsLive, tc.wantCurrent, tc.wantSummary, tc.wantLive)
			}
		})
	}
}

func TestScope_RunIDAndDepthSpawnFallback(t *testing.T) {
	cases := []struct {
		name  string
		base  domain.EventBase
		scope sidepanel.RunKey
	}{
		{
			name:  "run id",
			base:  domain.EventBase{Depth: 1, CallID: "c1", RunID: "r.1"},
			scope: sidepanel.RunOf(domain.EventBase{RunID: "r.1"}),
		},
		{
			name:  "depth and spawn without a run id",
			base:  domain.EventBase{Depth: 2, CallID: "c1"},
			scope: sidepanel.RunOf(domain.EventBase{Depth: 2, CallID: "c1"}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := newState(t)
			state.Fold(domain.TaskListEvent{EventBase: tc.base, Items: []domain.TaskItem{{Text: "child"}}})
			state.Fold(domain.TaskListEvent{Items: []domain.TaskItem{{Text: "top"}}})
			state.Fold(domain.TaskListEvent{
				EventBase: domain.EventBase{Depth: tc.base.Depth, CallID: "other", RunID: "r.other"},
				Items:     []domain.TaskItem{{Text: "sibling"}},
			})

			rows := state.Tasks(tc.scope).Rows

			if len(rows) != 1 || rows[0].Text != "child" {
				t.Errorf("scoped rows = %+v, want the child's one task", rows)
			}
		})
	}
}

func TestChangedFiles(t *testing.T) {
	edit := func(base domain.EventBase, path string, isError bool, summary domain.ToolSummary) domain.ToolResultEvent {
		return domain.ToolResultEvent{
			EventBase:   base,
			WriteTarget: path,
			Result:      domain.ToolResult{IsError: isError, Summary: summary},
		}
	}
	regions := domain.EditRegions{Regions: []domain.EditRegion{
		{Inserted: []string{"a", "b"}, Removed: []string{"c"}},
	}}
	state, _ := newState(t)
	state.Fold(edit(domain.EventBase{}, "/w/parser.go", false, regions))
	state.Fold(edit(domain.EventBase{}, "/w/parser.go", false, regions))
	state.Fold(edit(domain.EventBase{}, "/w/refused.go", true, regions))
	state.Fold(edit(child("r.1"), "/w/lexer_test.go", false, nil))
	state.Fold(domain.ToolResultEvent{Result: domain.ToolResult{}}) // no target: not a write

	cases := []struct {
		name  string
		scope sidepanel.RunKey
		want  []sidepanel.FileRow
	}{
		{
			name:  "top level folds every depth, refused write ignored",
			scope: sidepanel.RunKey{},
			want: []sidepanel.FileRow{
				{Path: "/w/parser.go", Stat: domain.DiffStat{Added: 4, Removed: 2}, HasStat: true},
				{Path: "/w/lexer_test.go", IsDelegated: true, Run: sidepanel.RunOf(child("r.1"))},
			},
		},
		{
			name:  "run scope holds the run's own",
			scope: sidepanel.RunOf(child("r.1")),
			want:  []sidepanel.FileRow{{Path: "/w/lexer_test.go", IsDelegated: true, Run: sidepanel.RunOf(child("r.1"))}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := state.ChangedFiles(tc.scope)

			if !view.IsLive || len(view.Rows) != len(tc.want) {
				t.Fatalf("view = %+v, want live with %d rows", view, len(tc.want))
			}
			for i, row := range view.Rows {
				if row != tc.want[i] {
					t.Errorf("row %d = %+v, want %+v", i, row, tc.want[i])
				}
			}
		})
	}
}

func TestChangedFiles_NotLiveWithoutAWrite(t *testing.T) {
	state, _ := newState(t)
	state.Fold(domain.ToolResultEvent{WriteTarget: "/w/x.go", Result: domain.ToolResult{IsError: true}})

	if view := state.ChangedFiles(sidepanel.RunKey{}); view.IsLive || len(view.Rows) != 0 {
		t.Errorf("refused write answered %+v, want empty and not live", view)
	}
}

func TestUsage_FillAndLiveRule(t *testing.T) {
	cases := []struct {
		name        string
		events      []domain.Event
		wantFill    sidepanel.Fill
		wantSummary string
		wantLive    bool
	}{
		{
			name:        "reading under the threshold",
			events:      []domain.Event{domain.UsageEvent{TotalTokens: 4900, ContextWindow: 10000}},
			wantFill:    sidepanel.Fill{Used: 4900, Window: 10000},
			wantSummary: "49%",
		},
		{
			name:        "reading at the threshold",
			events:      []domain.Event{domain.UsageEvent{PromptTokens: 4000, CompletionTokens: 1000, ContextWindow: 10000}},
			wantFill:    sidepanel.Fill{Used: 5000, Window: 10000},
			wantSummary: "50%", wantLive: true,
		},
		{
			name: "maintenance reading skipped for fill",
			events: []domain.Event{
				domain.UsageEvent{TotalTokens: 2000, ContextWindow: 10000},
				domain.UsageEvent{TotalTokens: 9000, ContextWindow: 10000, Maintenance: true},
			},
			wantFill:    sidepanel.Fill{Used: 2000, Window: 10000},
			wantSummary: "20%",
		},
		{
			name:        "window 0 falls back",
			events:      []domain.Event{domain.UsageEvent{TotalTokens: 6000}},
			wantFill:    sidepanel.Fill{Used: 6000, Window: 20000},
			wantSummary: "30%",
		},
		{
			name: "newer estimate stands over the reading",
			events: []domain.Event{
				domain.UsageEvent{TotalTokens: 2000, ContextWindow: 10000},
				domain.ContextEstimateEvent{Tokens: 7000},
			},
			wantFill:    sidepanel.Fill{Used: 7000, Window: 10000, IsEstimate: true},
			wantSummary: "70%", wantLive: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := newState(t)
			state.SetFallbackWindow(20000)
			for _, e := range tc.events {
				state.Fold(e)
			}

			view := state.Usage(sidepanel.RunKey{})

			if view.Fill != tc.wantFill || view.Summary != tc.wantSummary || view.IsLive != tc.wantLive {
				t.Errorf("fill/summary/live = %+v/%q/%v, want %+v/%q/%v",
					view.Fill, view.Summary, view.IsLive, tc.wantFill, tc.wantSummary, tc.wantLive)
			}
		})
	}
}

func TestUsage_HistoryArithmetic(t *testing.T) {
	state, _ := newState(t)
	state.Fold(domain.UsageEvent{TotalTokens: 9000, ContextWindow: 20000})
	state.Fold(domain.ContextCostEvent{Cost: domain.ContextCost{
		Rows: []domain.ContextCostRow{
			{Name: "prompt", Tokens: 1200},
			{Name: "orientation", Tokens: 300},
			{Name: "tool menu", Tokens: 2500},
		},
		Tokens: 4000,
	}})

	view := state.Usage(sidepanel.RunKey{})

	if !view.HasSplit || view.System != 1500 || view.Tools != 2500 || view.History != 5000 {
		t.Errorf("split = %v system %d tools %d history %d, want true 1500 2500 5000",
			view.HasSplit, view.System, view.Tools, view.History)
	}
}

// TestUsage_WithFillRemeasures pins the Driver's own fill: handed in, it replaces the State's and
// carries the history share, the summary and the live rule with it, leaving the split and totals.
func TestUsage_WithFillRemeasures(t *testing.T) {
	state, _ := newState(t)
	state.Fold(domain.UsageEvent{TotalTokens: 9000, ContextWindow: 20000, Cumulative: domain.Usage{Calls: 1, TotalTokens: 9000}})
	state.Fold(domain.ContextCostEvent{Cost: domain.ContextCost{
		Rows:   []domain.ContextCostRow{{Name: "prompt", Tokens: 1500}, {Name: "tool menu", Tokens: 2500}},
		Tokens: 4000,
	}})

	view := state.Usage(sidepanel.RunKey{}).WithFill(sidepanel.Fill{Used: 6000, Window: 10000, IsEstimate: true})

	if view.Fill != (sidepanel.Fill{Used: 6000, Window: 10000, IsEstimate: true}) {
		t.Errorf("fill = %+v, want the one handed in", view.Fill)
	}
	if view.History != 2000 || view.System != 1500 || view.Tools != 2500 {
		t.Errorf("split = system %d tools %d history %d, want 1500 2500 2000", view.System, view.Tools, view.History)
	}
	if view.Summary != "60%" || !view.IsLive {
		t.Errorf("summary %q live %v, want 60%% and live", view.Summary, view.IsLive)
	}
	if view.Totals.TotalTokens != 9000 {
		t.Errorf("totals = %d tokens, want the State's 9000", view.Totals.TotalTokens)
	}
	if empty := view.WithFill(sidepanel.Fill{}); empty.Summary != "" || empty.IsLive || empty.History != 0 {
		t.Errorf("an empty fill left summary %q live %v history %d", empty.Summary, empty.IsLive, empty.History)
	}
}

// TestClampedPercent pins the one clamp every fill percentage goes through: a whole percentage of
// the limit, never past 100 and 0 when either side is not positive.
func TestClampedPercent(t *testing.T) {
	cases := []struct {
		name        string
		used, limit int
		want        int
	}{
		{name: "under the limit", used: 6000, limit: 10000, want: 60},
		{name: "at the limit", used: 10000, limit: 10000, want: 100},
		{name: "over the limit", used: 45000, limit: 32768, want: 100},
		{name: "nothing used", used: 0, limit: 10000, want: 0},
		{name: "negative used", used: -5, limit: 10000, want: 0},
		{name: "zero limit", used: 6000, limit: 0, want: 0},
		{name: "negative limit", used: 6000, limit: -1, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sidepanel.ClampedPercent(tc.used, tc.limit); got != tc.want {
				t.Errorf("ClampedPercent(%d, %d) = %d, want %d", tc.used, tc.limit, got, tc.want)
			}
		})
	}
}

// TestUsage_WithFillClampsAnOverfullSummary pins the bug the clamp fixes: a fill past its window
// summarises as 100%, the figure the status-line gauge states beside its full bar.
func TestUsage_WithFillClampsAnOverfullSummary(t *testing.T) {
	view := sidepanel.UsageView{}.WithFill(sidepanel.Fill{Used: 45000, Window: 32768})

	if view.Summary != "100%" || !view.IsLive {
		t.Errorf("summary %q live %v, want 100%% and live", view.Summary, view.IsLive)
	}
	if got := view.Fill.Percent(); got != 100 {
		t.Errorf("Fill.Percent() = %d, want 100", got)
	}
}

func TestUsage_Totals(t *testing.T) {
	state, _ := newState(t)
	state.SetUsage(sidepanel.RunKey{}, domain.Usage{Calls: 2, PromptTokens: 100}, 0)
	state.Fold(domain.UsageEvent{Cumulative: domain.Usage{Calls: 1, PromptTokens: 10}})
	state.Fold(domain.UsageEvent{Cumulative: domain.Usage{Calls: 2, PromptTokens: 30}, Maintenance: true})
	state.Fold(domain.UsageEvent{EventBase: child("r.1"), Cumulative: domain.Usage{Calls: 1, PromptTokens: 5}})

	cases := []struct {
		name       string
		scope      sidepanel.RunKey
		wantPrompt int
	}{
		{name: "top level sums every run, maintenance kept", scope: sidepanel.RunKey{}, wantPrompt: 135},
		{name: "run scope is the run's own", scope: sidepanel.RunOf(child("r.1")), wantPrompt: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := state.Usage(tc.scope).Totals.PromptTokens; got != tc.wantPrompt {
				t.Errorf("prompt tokens = %d, want %d", got, tc.wantPrompt)
			}
		})
	}
}

func TestQueries_NilStateAnswersEmpty(t *testing.T) {
	var state *sidepanel.State

	if state.SubAgents().IsLive || state.Tasks(sidepanel.RunKey{}).IsLive ||
		state.ChangedFiles(sidepanel.RunKey{}).IsLive || state.Usage(sidepanel.RunKey{}).IsLive {
		t.Error("a nil State answered a live section")
	}
}

func TestClone_WritesToTheCloneLeaveTheOriginal(t *testing.T) {
	original, _ := newState(t)
	original.Fold(spawn(t, "r.1", map[string]any{"name": "scout", "task": "map it"}))
	original.Fold(domain.TaskListEvent{Items: []domain.TaskItem{{Text: "one"}}})
	original.Fold(domain.ToolResultEvent{WriteTarget: "/w/a.go"})

	clone := original.Clone()
	clone.Fold(spawn(t, "r.2", map[string]any{"name": "planner", "task": "plan it"}))
	clone.Fold(phase("r.1", domain.SubAgentStarted, time.Time{}, false))
	clone.Fold(domain.TaskListEvent{Items: []domain.TaskItem{{Text: "one", Done: true}}})
	clone.Fold(domain.ToolResultEvent{WriteTarget: "/w/b.go"})

	if got := len(original.SubAgents().Rows); got != 1 {
		t.Errorf("original lists %d sub-agents after the clone's write, want 1", got)
	}
	if got := original.SubAgents().Rows[0].Phase; got != sidepanel.PhaseQueued {
		t.Errorf("original's delegation moved to phase %v with the clone's", got)
	}
	if !original.Tasks(sidepanel.RunKey{}).IsLive {
		t.Error("original's task list finished with the clone's")
	}
	if got := len(original.ChangedFiles(sidepanel.RunKey{}).Rows); got != 1 {
		t.Errorf("original lists %d changed files after the clone's write, want 1", got)
	}
	if got := len(clone.SubAgents().Rows); got != 2 {
		t.Errorf("clone lists %d sub-agents, want 2", got)
	}
}

func TestClone_NilClonesToNil(t *testing.T) {
	var state *sidepanel.State

	if state.Clone() != nil {
		t.Error("a nil State cloned to a non-nil one")
	}
}

func TestReads_NamesExactlyTheVariantsFoldReads(t *testing.T) {
	cases := []struct {
		name  string
		event domain.Event
		want  bool
	}{
		{"a delegation's call", domain.ToolCallEvent{SpawnRunID: "r.1"}, true},
		{"a plain tool call", domain.ToolCallEvent{}, false},
		{"a phase", domain.SubAgentPhaseEvent{}, true},
		{"a name", domain.SubAgentNamedEvent{}, true},
		{"a tool result", domain.ToolResultEvent{}, true},
		{"a task list", domain.TaskListEvent{}, true},
		{"a usage reading", domain.UsageEvent{}, true},
		{"an estimate", domain.ContextEstimateEvent{}, true},
		{"a cost report", domain.ContextCostEvent{}, true},
		{"an approval", domain.ApprovalEvent{}, true},
		{"a Turn end", domain.TurnEvent{}, true},
		{"an approver failure", domain.ErrorEvent{Source: "loop", Err: domain.ApproverErrorPrefix + "prompt closed"}, true},
		{"a tool's error", domain.ErrorEvent{Source: "terminal", Err: domain.ApproverErrorPrefix + "x"}, false},
		{"another loop error", domain.ErrorEvent{Source: "loop", Err: "model stalled"}, false},
		{"a streamed token", domain.TokenEvent{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sidepanel.Reads(tc.event); got != tc.want {
				t.Errorf("Reads = %v, want %v", got, tc.want)
			}
		})
	}
}

// gate is one ApprovalEvent of the run base names.
func gate(base domain.EventBase, phase domain.ApprovalPhase, decision domain.ApprovalDecision) domain.ApprovalEvent {
	return domain.ApprovalEvent{EventBase: base, Phase: phase, Request: domain.ApprovalRequest{Tool: "write"}, Decision: decision}
}

// waitingByRun reads each row's Waiting flag, keyed by the run id the test spawned it under.
func waitingByRun(t *testing.T, state *sidepanel.State, ids ...string) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	rows := state.SubAgents().Rows
	for _, id := range ids {
		key := sidepanel.RunOf(child(id))
		for _, row := range rows {
			if row.Run == key {
				got[id] = row.Waiting
			}
		}
	}
	return got
}

func TestSubAgentWaitingOpensAndCloses(t *testing.T) {
	requested := func(id string) domain.Event { return gate(child(id), domain.ApprovalRequested, "") }
	decided := func(id string) domain.Event { return gate(child(id), domain.ApprovalDecided, domain.ApprovalAllow) }
	cases := []struct {
		name   string
		events func(at time.Time) []domain.Event
		want   map[string]bool
	}{
		{
			name:   "a requested gate marks only its own run, queued or running",
			events: func(at time.Time) []domain.Event { return []domain.Event{requested("r.1")} },
			want:   map[string]bool{"r.1": true, "r.2": false},
		},
		{
			name: "two gated runs are each cleared by their own decided",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{requested("r.1"), requested("r.2"), decided("r.2")}
			},
			want: map[string]bool{"r.1": true, "r.2": false},
		},
		{
			name: "both decided clears both",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{requested("r.1"), requested("r.2"), decided("r.2"), decided("r.1")}
			},
			want: map[string]bool{"r.1": false, "r.2": false},
		},
		{
			name: "the approver's failure for the run closes its gate",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{requested("r.1"), requested("r.2"), domain.ErrorEvent{
					EventBase: child("r.1"), Source: "loop", Err: domain.ApproverErrorPrefix + "prompt closed",
				}}
			},
			want: map[string]bool{"r.1": false, "r.2": true},
		},
		{
			name: "a tool's error with the same text closes nothing",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{requested("r.1"), domain.ErrorEvent{
					EventBase: child("r.1"), Source: "terminal", Err: domain.ApproverErrorPrefix + "x",
				}}
			},
			want: map[string]bool{"r.1": true},
		},
		{
			name: "the run finishing closes its gate",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{requested("r.1"), phase("r.1", domain.SubAgentFinished, at, false)}
			},
			want: map[string]bool{"r.1": false},
		},
		{
			name: "the run's own Turn end closes only its gate",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{requested("r.1"), requested("r.2"), domain.TurnEvent{EventBase: child("r.1")}}
			},
			want: map[string]bool{"r.1": false, "r.2": true},
		},
		{
			name: "a depth-0 Turn end clears every gate",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{requested("r.1"), requested("r.2"), domain.TurnEvent{}}
			},
			want: map[string]bool{"r.1": false, "r.2": false},
		},
		{
			name: "an allow-rule decided with no request clears nothing",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{requested("r.1"), gate(child("r.1"), domain.ApprovalDecided, domain.ApprovalAllowedByRule)}
			},
			want: map[string]bool{"r.1": true},
		},
		{
			name: "a top-level gate marks no row",
			events: func(at time.Time) []domain.Event {
				return []domain.Event{gate(domain.EventBase{}, domain.ApprovalRequested, "")}
			},
			want: map[string]bool{"r.1": false, "r.2": false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, clock := newState(t)
			state.Fold(spawn(t, "r.1", map[string]any{"name": "one"}))
			state.Fold(spawn(t, "r.2", map[string]any{"name": "two"}))
			state.Fold(phase("r.1", domain.SubAgentStarted, clock.at, false))
			for _, event := range tc.events(clock.at) {
				state.Fold(event)
			}
			got := waitingByRun(t, state, "r.1", "r.2")
			for id, want := range tc.want {
				if got[id] != want {
					t.Errorf("%s Waiting = %v, want %v", id, got[id], want)
				}
			}
		})
	}
}

func TestSubAgentWaiting_TopLevelGateAddsNoRow(t *testing.T) {
	state, _ := newState(t)
	state.Fold(gate(domain.EventBase{}, domain.ApprovalRequested, ""))
	state.Fold(gate(domain.EventBase{Depth: 1, CallID: "call-x", RunID: "unseen"}, domain.ApprovalRequested, ""))
	if rows := state.SubAgents().Rows; len(rows) != 0 {
		t.Errorf("rows = %+v, want none: a gate is keyed through run, never delegation", rows)
	}
}

func TestSubAgentWaiting_FinishedRowIsNeverWaiting(t *testing.T) {
	state, clock := newState(t)
	state.Fold(spawn(t, "r.1", map[string]any{"name": "one"}))
	state.Fold(phase("r.1", domain.SubAgentStarted, clock.at, false))
	state.Fold(phase("r.1", domain.SubAgentFinished, clock.at, false))
	state.Fold(gate(child("r.1"), domain.ApprovalRequested, ""))
	if got := waitingByRun(t, state, "r.1")["r.1"]; got {
		t.Errorf("a done row's Waiting = true, want false: only active rows wait")
	}
}
