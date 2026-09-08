package agent

// The Reaction-cascade matrix: every one of the five seam Moments × the four things the
// dispatcher (reactions.go) promises at each of them — an armed Reaction that acts is booked,
// one that only inspects is not, one that REPORTS acting through its Outcome is booked even
// though it moved nothing, and a panicking one is contained. Bypass is the fifth cell: at every
// Moment it drops a shape-class Reaction before it is ever invoked.
//
// Every cell is driven through the real Submit → Step → dispatchTools path against a tool
// whose result carries an UNCOMPARABLE summary (ReadSpan's []int of located lines) — the
// shape every successful read_file produces, and the shape that panicked the whole-struct
// compare the Revision bracket replaced. Driving it here keeps that regression pinned at the
// dispatch level; the Revision counters themselves are covered in package domain.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// cascadeReactionID is the ID every cell arms its probe under.
const cascadeReactionID = "cascade_probe"

// locatedReadSummary is the uncomparable summary read_file returns on every successful read:
// LocatedOn is a slice, so a struct compare of two ToolResults carrying one panics.
func locatedReadSummary() domain.ToolSummary {
	return domain.ReadSpan{Start: 1, End: 3, Total: 3, Locate: "main", LocatedOn: []int{1, 2}}
}

// summaryTool is the "probe" tool the cascade driver calls: it counts its runs and returns a
// result whose Summary holds that uncomparable value.
func summaryTool(ran *int) fakeTool {
	return fakeTool{
		name:     "probe",
		readOnly: true,
		execute: func(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
			*ran++
			return domain.ToolResult{
				CallID:  call.ID,
				Content: "package main\nfunc main() {}\n",
				Summary: locatedReadSummary(),
			}, nil
		},
	}
}

// cellBehavior is what the cascade probe does at the seam Moment under test.
type cellBehavior int

const (
	probeInspects cellBehavior = iota // reads the working value and leaves it alone
	probeActs                         // mutates the working value through that Moment's edit surface
	probeReports                      // touches nothing but returns an Outcome saying it acted
	probePanics                       // panics inside the handler, for the recover boundary
)

// reportedOutcome is what a probe returns once it has done (or not done) its work: the zero
// Outcome for every behaviour but probeReports, which says it acted without moving anything —
// the shape a bench instrument uses when it wants every invocation booked.
func reportedOutcome(b cellBehavior) domain.Outcome {
	if b == probeReports {
		return domain.Outcome{Edited: true}
	}
	return domain.Outcome{}
}

// cascadeReaction builds the one Reaction a cell arms: it stands at Moment at, records its
// arrival through seen, and applies behavior there. A Reaction's handler is sealed to one seam,
// so the probe stands at the Moment under test and nowhere else.
func cascadeReaction(at domain.Moment, behavior cellBehavior, class domain.Class, seen func(domain.Moment)) domain.Reaction {
	arrive := func() bool {
		seen(at)
		if behavior == probePanics {
			panic("cascade probe: deliberate panic at " + string(at))
		}
		return behavior == probeActs
	}

	r := domain.Reaction{
		ID:     cascadeReactionID,
		Origin: domain.OriginEngine,
		Class:  class,
		On:     []domain.Moment{at},
	}
	switch at {
	case domain.MomentPreRequest:
		r.Handler = domain.PreRequestFunc(func(_ context.Context, req *domain.Request) (domain.Outcome, error) {
			if arrive() {
				req.AppendToSystem("[cascade]", "[cascade] nudge")
			}
			return reportedOutcome(behavior), nil
		})
	case domain.MomentPostResponse:
		r.Handler = domain.PostResponseFunc(func(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
			if arrive() {
				resp.SetText(resp.Text() + "[cascade]")
			}
			return reportedOutcome(behavior), nil
		})
	case domain.MomentPreToolExec:
		r.Handler = domain.PreToolExecFunc(func(_ context.Context, _ domain.LoopView, call *domain.ToolCallEdit) (domain.Outcome, error) {
			if arrive() {
				call.SetArguments(json.RawMessage(`{"cascade":true}`))
			}
			return reportedOutcome(behavior), nil
		})
	case domain.MomentPostToolResult:
		r.Handler = domain.PostToolResultFunc(func(_ context.Context, _ domain.LoopView, _ domain.ToolCall, result *domain.ToolResultEdit) (domain.Outcome, error) {
			if arrive() {
				result.SetContent(result.Content() + " [cascade]")
			}
			return reportedOutcome(behavior), nil
		})
	case domain.MomentHistoryRewrite:
		r.Handler = domain.HistoryRewriteFunc(func(_ context.Context, conv *domain.Conversation) (domain.Outcome, error) {
			if arrive() && conv.Len() > 0 {
				conv.SetMessageContent(0, conv.At(0).Content+" [cascade]")
			}
			return reportedOutcome(behavior), nil
		})
	}
	return r
}

// cascadeSpec is one cell's arming: where the probe stands, what it does there, which class cell
// it occupies (what Bypass reads), and whether Bypass is on.
type cascadeSpec struct {
	at       domain.Moment
	behavior cellBehavior
	class    domain.Class
	bypass   bool
}

// cascadeRun is what one driven Exchange leaves behind for a cell to assert on.
type cascadeRun struct {
	events     []domain.Event
	seen       map[domain.Moment]int
	toolRuns   int
	toolStatus domain.StepStatus // the status of the tool-carrying first Turn
}

// driveCascade arms the Reaction spec describes and drives one Exchange whose first Turn carries
// a tool call — so all five seam Moments run — closing it with a second Step whenever the first
// Turn survived. It fails the test only on a loop error: what a Moment's cascade leaves behind is
// a contract, not a failure, so the caller asserts the status itself.
func driveCascade(t *testing.T, spec cascadeSpec) cascadeRun {
	t.Helper()

	sink := &recordingSink{}
	toolRuns := 0
	cfg := configWithTools(sink, summaryTool(&toolRuns))
	cfg.Bypass = spec.bypass

	seen := make(map[domain.Moment]int, len(allSeamMoments))
	cfg.Reactions = []domain.Reaction{
		cascadeReaction(spec.at, spec.behavior, spec.class, func(m domain.Moment) { seen[m]++ }),
	}

	a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("call-1", "probe", `{}`),
		contentScript("done"),
	}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "do the thing"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (tool Turn) returned a loop error: %v", err)
	}
	if res.Status == domain.StatusTurnComplete {
		if _, err := a.Step(context.Background()); err != nil {
			t.Fatalf("Step (closing Turn) returned a loop error: %v", err)
		}
	}

	return cascadeRun{events: sink.events, seen: seen, toolRuns: toolRuns, toolStatus: res.Status}
}

// firesAt counts the firings booked for id at seam Moment m.
func firesAt(events []domain.Event, id string, m domain.Moment) int {
	n := 0
	for _, fe := range reactionFires(events) {
		if fe.Reaction == id && fe.Moment == m {
			n++
		}
	}
	return n
}

// hookPanicBooked reports whether the recover boundary turned id's panic into an ErrorEvent
// attributed to id — the containment receipt every seam Moment shares.
func hookPanicBooked(events []domain.Event, id string) bool {
	for _, e := range events {
		ee, ok := e.(domain.ErrorEvent)
		if ok && ee.Source == id && strings.HasPrefix(ee.Err, "panic:") {
			return true
		}
	}
	return false
}

// TestReactionCascade is the five-Moment × five-scenario matrix of the Reaction dispatcher: at every
// seam Moment, booking follows the acted probe and nothing else, a probe that reports acting
// through its Outcome is booked on every invocation, a panic is contained the same way
// everywhere, and Bypass drops a shape-class Reaction before it is ever invoked.
func TestReactionCascade(t *testing.T) {
	cells := []struct {
		name string
		run  func(t *testing.T, m domain.Moment)
	}{
		{name: "acts ⇒ booked", run: assertActedFireBooked},
		{name: "no-op ⇒ not booked", run: assertNoOpNotBooked},
		{name: "reports acting ⇒ booked every time", run: assertReportedFireBooked},
		{name: "panicking reaction ⇒ contained", run: assertPanicContained},
		{name: "bypass ⇒ skipped", run: assertBypassSkips},
	}

	for _, m := range allSeamMoments {
		for _, cell := range cells {
			t.Run(string(m)+"/"+cell.name, func(t *testing.T) { cell.run(t, m) })
		}
	}
}

// assertActedFireBooked: an armed Reaction that edits the Moment's working value is booked under
// its own ID at that Moment — the Revision bracket sees the move whether or not it said so.
func assertActedFireBooked(t *testing.T, m domain.Moment) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: m, behavior: probeActs, class: domain.ClassShapeView})

	if run.seen[m] == 0 {
		t.Fatalf("the armed Reaction was never invoked at %s", m)
	}
	if got := firesAt(run.events, cascadeReactionID, m); got == 0 {
		t.Errorf("an acting Reaction booked no fire at %s; an intervention must be booked", m)
	}
}

// assertNoOpNotBooked: an armed Reaction that inspects and leaves the working value alone is
// invoked but books nothing — fired means ACTED (R4).
func assertNoOpNotBooked(t *testing.T, m domain.Moment) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: m, behavior: probeInspects, class: domain.ClassObserve})

	if run.seen[m] == 0 {
		t.Fatalf("the armed Reaction was never invoked at %s", m)
	}
	if got := firesAt(run.events, cascadeReactionID, m); got != 0 {
		t.Errorf("an inspect-only Reaction booked %d fires at %s, want 0", got, m)
	}
}

// assertReportedFireBooked: the bench's instrument books every invocation by SAYING it acted —
// a non-zero Outcome is a firing even when the working value never moved. It is what a Reaction
// arms in place of the always-booked experimental hook the registry had.
func assertReportedFireBooked(t *testing.T, m domain.Moment) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: m, behavior: probeReports, class: domain.ClassObserve})

	if run.seen[m] == 0 {
		t.Fatalf("the armed Reaction was never invoked at %s", m)
	}
	if got := firesAt(run.events, cascadeReactionID, m); got != run.seen[m] {
		t.Errorf("a reporting Reaction booked %d fires for %d invocations at %s; each one is booked", got, run.seen[m], m)
	}
}

// assertPanicContained: a panicking Reaction degrades to an ErrorEvent under its own ID and to a
// reaction that did NOTHING — the cascade and the Turn carry on unchanged at every Moment (ADR
// 0076 stage-1 header call). The five per-point dispositions the hook runner had are gone with
// it: a broken extension can no longer degrade a Turn.
func assertPanicContained(t *testing.T, m domain.Moment) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: m, behavior: probePanics, class: domain.ClassShapeView})

	if !hookPanicBooked(run.events, cascadeReactionID) {
		t.Errorf("no ErrorEvent attributed to the panicking Reaction at %s", m)
	}
	if got := firesAt(run.events, cascadeReactionID, m); got != 0 {
		t.Errorf("a panicking Reaction booked %d fires at %s, want 0", got, m)
	}
	if run.toolRuns == 0 {
		t.Errorf("the tool did not run after a panic at %s; the Turn must carry on", m)
	}
	if run.toolStatus != domain.StatusTurnComplete {
		t.Errorf("Turn status = %q after a panic at %s, want %q", run.toolStatus, m, domain.StatusTurnComplete)
	}
}

// assertBypassSkips: under Bypass an armed shape-class Reaction is dropped at dispatch — never
// invoked, so nothing to book (ADR 0076 D9).
func assertBypassSkips(t *testing.T, m domain.Moment) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: m, behavior: probeActs, class: domain.ClassShapeView, bypass: true})

	if run.seen[m] != 0 {
		t.Errorf("a Bypass-gated Reaction was invoked %d times at %s, want 0", run.seen[m], m)
	}
	if got := firesAt(run.events, cascadeReactionID, m); got != 0 {
		t.Errorf("a Bypass-gated Reaction booked %d fires at %s, want 0", got, m)
	}
}

// firePanickingSink is a host Events sink that faults the moment it is told about a fire: it
// records every event and panics on the ReactionFiredEvent a booking emits.
type firePanickingSink struct {
	events []domain.Event
}

func (s *firePanickingSink) Emit(e domain.Event) {
	s.events = append(s.events, e)
	if _, ok := e.(domain.ReactionFiredEvent); ok {
		panic("firePanickingSink: deliberate panic on ReactionFiredEvent")
	}
}

// TestFireBookedOutsideRecoverBoundary pins which side of the recover boundary the booking
// sits on. The booking reaches the HOST's Events sink, so a sink that panics while being told
// about a fire is the host's fault, not the Reaction's: it must unwind to the host rather
// than come back as errHookPanicked attributed to a Reaction whose handler already returned
// cleanly. The handler BODY keeps its coverage — a panic there is still recovered and attributed.
func TestFireBookedOutsideRecoverBoundary(t *testing.T) {
	t.Run("panicking sink ⇒ not attributed to the Reaction", func(t *testing.T) {
		sink := &firePanickingSink{}
		cfg := baseConfig(sink)
		cfg.Reactions = []domain.Reaction{
			cascadeReaction(domain.MomentPreRequest, probeActs, domain.ClassShapeView, func(domain.Moment) {}),
		}

		a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{contentScript("done")}})
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		if err := a.Submit(domain.UserInput{Text: "do the thing"}); err != nil {
			t.Fatalf("Submit: %v", err)
		}

		escaped := stepRecoveringPanic(t, a)

		if escaped == nil {
			t.Fatalf("the sink panic did not reach the host: the fire is still booked inside the recover boundary")
		}
		if hookPanicBooked(sink.events, cascadeReactionID) {
			t.Errorf("a panicking Events sink was reported as a handler panic attributed to %s", cascadeReactionID)
		}
	})

	t.Run("panicking handler body ⇒ still attributed to the Reaction", func(t *testing.T) {
		run := driveCascade(t, cascadeSpec{at: domain.MomentPreRequest, behavior: probePanics, class: domain.ClassShapeView})

		if !hookPanicBooked(run.events, cascadeReactionID) {
			t.Errorf("a panicking handler body was not attributed to %s; the body keeps its coverage", cascadeReactionID)
		}
	})
}

// stepRecoveringPanic drives one Step and returns the panic value that escaped it, or nil when
// none did — the probe for whether a fault was contained inside the loop or handed to the host.
func stepRecoveringPanic(t *testing.T, a *Agent) (escaped any) {
	t.Helper()
	defer func() { escaped = recover() }()
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step returned a loop error: %v", err)
	}
	return nil
}
