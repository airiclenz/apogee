package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// eventsSource is the domain file the variant-coverage guard reads. It is parsed off disk
// rather than reflected over, because reflection can only see the variants this test already
// names — the whole point is to find the one it does not.
const eventsSource = "../domain/events.go"

// eventBaseType is the embedded marker every Event variant carries (domain/events.go).
const eventBaseType = "EventBase"

// statsFold is the observable half of foldStats: the three status-line numbers it can move.
// genStarted stands in for the genStart timestamp, which is a wall clock and so cannot be
// compared against a literal.
type statsFold struct {
	ctxUsed    int
	genStarted bool
	tokPerSec  float64
}

// statsOf snapshots the stats a fold may have moved.
func statsOf(m Model) statsFold {
	return statsFold{ctxUsed: m.ctxUsed, genStarted: !m.genStart.IsZero(), tokPerSec: m.tokPerSec}
}

// foldCase is one Event variant and everything foldEvent does with it on a fresh Model. A row
// that expects nothing at all (an audit record, a fired mechanism outside the debug view) is
// stated as such deliberately: "this variant is inert in the view" is the documentation the
// three separate switches never carried in one place.
type foldCase struct {
	name             string
	event            domain.Event
	wantEntries      int       // transcript entries the fold appended (0 = none)
	wantPending      string    // the in-progress assistant buffer after the fold
	wantPendingDepth int       // whose buffer that is — the nesting level the tokens streamed at
	wantPhrase       string    // the activity phrase after the fold ("" = the slot is left idle)
	wantStats        statsFold // the stats after the fold (the zero value = foldStats moved nothing)
}

// foldCases is the variant table: every domain.Event, what it does to the view, and — by way
// of TestFoldEventCoversEveryEventVariant — the assertion that there is no twelfth.
func foldCases() []foldCase {
	return []foldCase{
		{
			name:        "TokenEvent streams into the pending buffer and says responding",
			event:       domain.TokenEvent{Text: "hi"},
			wantPending: "hi",
			wantPhrase:  "responding",
			wantStats:   statsFold{genStarted: true}, // the generation clock starts on the first token
		},
		{
			name: "TokenEvent at Depth 1 buffers at ITS OWN depth and says a sub-agent is responding",
			// The streaming path routes by depth like every other assistant-text case: the buffer
			// records whose tokens it holds, so the live preview paints inside the delegate's run
			// and its residue can never be committed as the parent's answer (transcript.go).
			event:            domain.TokenEvent{EventBase: domain.EventBase{Depth: 1}, Text: "hi"},
			wantPending:      "hi",
			wantPendingDepth: 1,
			wantPhrase:       subAgentActivityName + " · responding",
			// No generation clock: the gauge times the conversation the human is steering, and a
			// delegate's tokens are not it (foldStats).
		},
		{
			name:       "ReasoningEvent is activity only — the transcript never shows reasoning",
			event:      domain.ReasoningEvent{Text: "hmm"},
			wantPhrase: "thinking",
		},
		{
			name:       "StreamResetEvent discards the pending buffer and says retrying",
			event:      domain.StreamResetEvent{},
			wantPhrase: "retrying",
		},
		{
			name:        "MessageEvent commits an assistant entry and keeps thinking",
			event:       domain.MessageEvent{Text: "done"},
			wantEntries: 1,
			wantPhrase:  "thinking",
		},
		{
			name:        "ToolCallEvent records the call and names it in the phrase",
			event:       domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}},
			wantEntries: 1,
			wantPhrase:  "reading",
		},
		{
			name: "ToolResultEvent with no call to pair appends the orphan and returns to thinking",
			// The defensive case on a fresh Model: addToolResult finds no open call, so the
			// outcome lands as a standalone block rather than being lost, and no call is left
			// open — which is exactly what foldEvent hands foldActivity.
			event:       domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}},
			wantEntries: 1,
			wantPhrase:  "thinking",
		},
		{
			name:        "ApprovalEvent is a transcript note and no activity at all",
			event:       domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow},
			wantEntries: 1,
		},
		{
			name: "MechanismFiredEvent is inert outside the debug view",
			// Nothing: no entry (transcript.debug is off by default), no phrase, no stats.
			event: domain.MechanismFiredEvent{Mechanism: "m", Hook: "h", Action: "a"},
		},
		{
			name:        "ErrorEvent appends a recovered-fault notice and leaves the phrase alone",
			event:       domain.ErrorEvent{Source: "loop", Err: "recovered"},
			wantEntries: 1,
		},
		{
			name: "UsageEvent at depth 0 moves the gauge and appends no entry",
			// The top-level reading is the status line's: it lights the gauge and nothing in the
			// scrollback. A sub-agent's reading (Depth > 0) appends no entry either — it lands ON
			// the run block that is already there (transcript.applyUsage), which is a fold this
			// fresh-Model table has no run to show, so the attribution is pinned in
			// transcript_test.go and model_test.go instead.
			event:     domain.UsageEvent{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200},
			wantStats: statsFold{ctxUsed: 1200},
		},
		{
			name: "AuditEvent is inert in the TUI",
			// Nothing at all: the audit trail is observable through the EventSink for other
			// hosts (a log shipper), and the transcript deliberately renders none of it.
			event: domain.AuditEvent{Tool: "terminal", CallID: "1", Decision: "allowed"},
		},
	}
}

// TestFoldEventCoversEveryEventVariant is the compiler-adjacent nudge the three-switch fold
// never had: it reads internal/domain/events.go with go/parser, collects every exported struct
// that embeds EventBase — the shape of an Event variant — and fails if one has no row in
// foldCases. Adding a variant without saying what the view does with it (including "nothing")
// is the failure mode this guard exists for; the reverse check catches a row left behind by a
// renamed or deleted variant.
//
// The file is parsed off disk rather than reflected over: reflection can only enumerate the
// types this test already names, which is precisely the set that cannot contain the omission.
func TestFoldEventCoversEveryEventVariant(t *testing.T) {
	t.Parallel()

	declared := declaredEventVariants(t)
	// A guard that parsed no types would pass over a domain it never looked at.
	if len(declared) == 0 {
		t.Fatalf("no Event variants were parsed out of %s; the coverage guard proved nothing", eventsSource)
	}

	covered := make(map[string]bool, len(foldCases()))
	for _, tc := range foldCases() {
		covered[reflect.TypeOf(tc.event).Name()] = true
	}
	for _, name := range slices.Sorted(maps.Keys(declared)) {
		if !covered[name] {
			t.Errorf("domain.%s is an Event variant with no row in foldCases: say what foldEvent does with it — a transcript entry, a stats change, an activity phrase, or explicitly nothing", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(covered)) {
		if !declared[name] {
			t.Errorf("foldCases has a row for domain.%s, which %s no longer declares as an Event variant", name, eventsSource)
		}
	}
}

// declaredEventVariants parses the domain's Event declarations and returns the name of every
// exported struct type that embeds EventBase.
func declaredEventVariants(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, eventsSource, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", eventsSource, err)
	}
	variants := make(map[string]bool)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !ts.Name.IsExported() {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || !embedsEventBase(st) {
				continue
			}
			variants[ts.Name.Name] = true
		}
	}
	return variants
}

// embedsEventBase reports whether a struct embeds the Event sealing marker — an unnamed field
// whose type is the bare identifier EventBase.
func embedsEventBase(st *ast.StructType) bool {
	if st.Fields == nil {
		return false
	}
	for _, f := range st.Fields.List {
		if len(f.Names) > 0 {
			continue // a named field, not an embedding
		}
		if id, ok := f.Type.(*ast.Ident); ok && id.Name == eventBaseType {
			return true
		}
	}
	return false
}

// TestFoldEventFoldsEveryVariant runs the variant table through foldEvent on a fresh Model and
// asserts all three folds at once: what the transcript recorded, what the status-line stats
// moved, and what the activity phrase became.
func TestFoldEventFoldsEveryVariant(t *testing.T) {
	t.Parallel()

	for _, tc := range foldCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := newTestModel(t)
			before := len(m.transcript.entries) // the seeded start-up box

			m = m.foldEvent(tc.event)

			if got := len(m.transcript.entries) - before; got != tc.wantEntries {
				t.Errorf("appended %d transcript entries, want %d", got, tc.wantEntries)
			}
			if got := m.transcript.pending; got != tc.wantPending {
				t.Errorf("pending buffer = %q, want %q", got, tc.wantPending)
			}
			if got := m.transcript.pendingRun.depth; got != tc.wantPendingDepth {
				t.Errorf("pending buffer depth = %d, want %d", got, tc.wantPendingDepth)
			}
			if got := m.act.text(""); got != tc.wantPhrase {
				t.Errorf("activity phrase = %q, want %q", got, tc.wantPhrase)
			}
			if got := statsOf(m); got != tc.wantStats {
				t.Errorf("stats = %+v, want %+v", got, tc.wantStats)
			}
		})
	}
}

// TestFoldEventPairsResultWithCallBeforeActivity pins the ordering foldEvent owns, which used
// to rest on a comment: transcript.apply pairs a result with its call, and only then may the
// activity decide the tool phase is over. Fold the result before the pairing and a call that
// is still open reads as closed, so a parallel batch would drop the tool phrase early.
func TestFoldEventPairsResultWithCallBeforeActivity(t *testing.T) {
	t.Parallel()

	t.Run("the last result of the batch ends the tool phrase", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}})
		if got, want := m.act.text(""), "reading"; got != want {
			t.Fatalf("phrase while the call is open = %q, want %q", got, want)
		}

		m = m.foldEvent(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}})
		if got, want := m.act.text(""), "thinking"; got != want {
			t.Errorf("phrase after the only call was answered = %q, want %q", got, want)
		}
		if m.transcript.hasOpenToolCall() {
			t.Error("the result did not pair with its call: a call is still open")
		}
	})

	t.Run("a result while another call is open keeps the tool phrase", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m = m.foldEvent(domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}})
		m = m.foldEvent(domain.ToolCallEvent{Call: domain.ToolCall{ID: "2", Tool: "read_file", Arguments: []byte(`{"path":"b.go"}`)}})

		m = m.foldEvent(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}})
		if got, want := m.act.text(""), "reading"; got != want {
			t.Errorf("phrase with the second call still open = %q, want %q", got, want)
		}
		if !m.transcript.hasOpenToolCall() {
			t.Error("the second call was not left open by the fold")
		}
	})
}

// ----------------------------------------------------------------------------
// The main agent's cumulative accounting (foldStats)
// ----------------------------------------------------------------------------

// mainUsage is one reading the top-level agent reported: the Turn's own fill, and the running
// totals the agent stamped it with (domain.UsageEvent's Cumulative* fields).
func mainUsage(prompt, completion, total, cumPrompt, cumCompletion, cumTotal, calls int) domain.UsageEvent {
	return domain.UsageEvent{
		PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total,
		CumulativePromptTokens: cumPrompt, CumulativeCompletionTokens: cumCompletion,
		CumulativeTotalTokens: cumTotal, CumulativeCalls: calls,
	}
}

// TestFoldStatsTracksTheMainAgentsCumulativeTotals pins the totals half of the usage fold: the
// Model holds the LATEST reading the top-level agent stamped, never a sum of the stream, so a view
// that joined late reports what one that saw every event reports. A delegate's reading is not the
// main agent's — it belongs to its own run block — and a reading from an agent that has accounted
// for nothing leaves the standing totals alone rather than blanking them.
func TestFoldStatsTracksTheMainAgentsCumulativeTotals(t *testing.T) {
	t.Parallel()

	t.Run("the newest reading replaces the previous one", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)

		m = m.foldEvent(mainUsage(1000, 200, 1200, 1000, 200, 1200, 1))
		m = m.foldEvent(mainUsage(2400, 300, 2700, 3400, 500, 3900, 2))

		want := usageTotals{Calls: 2, PromptTokens: 3400, CompletionTokens: 500, TotalTokens: 3900}
		if m.usage != want {
			t.Errorf("totals = %+v, want %+v (the agent's own running sum, not a fold-side sum)", m.usage, want)
		}
	})

	t.Run("a delegate's reading is not the main agent's", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)

		m = m.foldEvent(mainUsage(1000, 200, 1200, 1000, 200, 1200, 1))
		child := mainUsage(500, 100, 600, 500, 100, 600, 1)
		child.Depth = 1
		m = m.foldEvent(child)

		want := usageTotals{Calls: 1, PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200}
		if m.usage != want {
			t.Errorf("totals = %+v, want %+v — a child counts on its own run head", m.usage, want)
		}
	})

	t.Run("a reading stamped by no accounting leaves the totals standing", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)

		m = m.foldEvent(mainUsage(1000, 200, 1200, 1000, 200, 1200, 1))
		m = m.foldEvent(domain.UsageEvent{PromptTokens: 900, CompletionTokens: 100, TotalTokens: 1000})

		want := usageTotals{Calls: 1, PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200}
		if m.usage != want {
			t.Errorf("totals = %+v, want %+v (an uncounted event blanks nothing)", m.usage, want)
		}
		if m.ctxUsed != 1000 {
			t.Errorf("ctxUsed = %d, want 1000 — the fill still follows every reading", m.ctxUsed)
		}
	})
}

// TestFoldStatsSkipsAMaintenanceReadingForTheGaugeAndClock pins where the two readings part
// company: a maintenance event (the compaction call) is real spend, so the totals take it, but its
// prompt describes the summarizer's own request — so the gauge must not move to it and the
// generation clock must survive it, ready to time the Turn that is still streaming.
func TestFoldStatsSkipsAMaintenanceReadingForTheGaugeAndClock(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)

	m = m.foldEvent(mainUsage(1000, 200, 1200, 1000, 200, 1200, 1))
	m = m.foldEvent(domain.TokenEvent{Text: "hi"}) // the next Turn starts streaming

	maintenance := mainUsage(8000, 400, 8400, 9000, 600, 9600, 2)
	maintenance.Maintenance = true
	m = m.foldEvent(maintenance)

	want := usageTotals{Calls: 2, PromptTokens: 9000, CompletionTokens: 600, TotalTokens: 9600}
	if m.usage != want {
		t.Errorf("totals = %+v, want %+v — a maintenance call's tokens were really spent", m.usage, want)
	}
	if m.ctxUsed != 1200 {
		t.Errorf("ctxUsed = %d, want the last Turn's 1200 — the gauge skips a maintenance reading", m.ctxUsed)
	}
	if m.tokPerSec != 0 {
		t.Errorf("tokPerSec = %v, want 0 — a maintenance reading times nothing", m.tokPerSec)
	}
	if m.genStart.IsZero() {
		t.Error("the generation clock was cleared by a maintenance reading; the Turn is still streaming")
	}

	m = m.foldEvent(mainUsage(2000, 300, 2300, 11000, 900, 11900, 3))
	if m.ctxUsed != 2300 {
		t.Errorf("ctxUsed = %d, want 2300 — the next Turn moves the gauge normally", m.ctxUsed)
	}
	if m.tokPerSec <= 0 {
		t.Errorf("tokPerSec = %v, want > 0 — the surviving clock timed the completion", m.tokPerSec)
	}
}
