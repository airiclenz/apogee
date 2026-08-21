package agent

// The hook-cascade matrix: every one of the five hook points × the five things the shared
// runner (hookrun.go) promises at each of them — a catalogued hook that acts is booked, one
// that only inspects is not, an experimental hook is booked either way, a panicking hook is
// contained per that point's own contract, and Bypass skips the catalogued hook entirely.
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

// cascadeProbeID is the catalogue ID every catalogued cell registers its probe under.
const cascadeProbeID domain.MechanismID = "cascade_probe"

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

// probeBehavior is what a cascadeProbe does at the hook point under test.
type probeBehavior int

const (
	probeInspects probeBehavior = iota // reads the working value and leaves it alone
	probeActs                          // mutates the working value through that point's edit surface
	probePanics                        // panics inside the hook, for the recover boundary
)

// cascadeProbe implements all five hook interfaces, so ONE fixture stands at whichever point a
// cell is testing: at `at` it applies `behavior`, at every other point it inspects and returns.
// seen counts invocations per point, so a Bypass-gated cell can prove the probe never arrived.
type cascadeProbe struct {
	at       domain.HookPoint
	behavior probeBehavior
	seen     func(domain.HookPoint)
}

func (p cascadeProbe) row(capability domain.Capability) domain.RegisteredMechanism {
	return domain.RegisteredMechanism{
		Descriptor: domain.MechanismDescriptor{ID: cascadeProbeID, Capability: capability},
		Hook:       p,
	}
}

// arrive records one invocation at at and reports whether the probe should mutate there — or
// panics instead, when at is the point the cell is probing and the behavior asks for it.
func (p cascadeProbe) arrive(at domain.HookPoint) bool {
	p.seen(at)
	if at != p.at {
		return false
	}
	if p.behavior == probePanics {
		panic("cascade probe: deliberate panic at " + string(at))
	}
	return p.behavior == probeActs
}

func (p cascadeProbe) RewriteHistory(_ context.Context, conv *domain.Conversation) error {
	if p.arrive(domain.HookHistoryRewrite) && conv.Len() > 0 {
		conv.SetMessageContent(0, conv.At(0).Content+" [cascade]")
	}
	return nil
}

func (p cascadeProbe) PreRequest(_ context.Context, req *domain.Request) error {
	if p.arrive(domain.HookPreRequest) {
		req.AppendToSystem("[cascade]", "[cascade] nudge")
	}
	return nil
}

func (p cascadeProbe) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	if p.arrive(domain.HookPostResponse) {
		resp.SetText(resp.Text() + "[cascade]")
	}
	return domain.PostResponseDecision{}, nil
}

func (p cascadeProbe) PreToolExec(_ context.Context, call *domain.ToolCallEdit, _ domain.LoopView) error {
	if p.arrive(domain.HookPreToolExec) {
		call.SetArguments(json.RawMessage(`{"cascade":true}`))
	}
	return nil
}

func (p cascadeProbe) PostToolResult(_ context.Context, _ domain.ToolCall, result *domain.ToolResultEdit, _ domain.LoopView) error {
	if p.arrive(domain.HookPostToolResult) {
		result.SetContent(result.Content() + " [cascade]")
	}
	return nil
}

// cascadeSpec is one cell's registration: where the probe stands, what it does there, whether
// it is catalogued (under capability) or experimental, and whether Bypass is on.
type cascadeSpec struct {
	at           domain.HookPoint
	behavior     probeBehavior
	capability   domain.Capability
	experimental bool
	bypass       bool
}

// cascadeRun is what one driven Exchange leaves behind for a cell to assert on.
type cascadeRun struct {
	events     []domain.Event
	seen       map[domain.HookPoint]int
	toolRuns   int
	toolStatus domain.StepStatus // the status of the tool-carrying first Turn
}

// driveCascade registers the probe spec describes and drives one Exchange whose first Turn
// carries a tool call — so all five hook points run — closing it with a second Step whenever
// the first Turn survived. It fails the test only on a loop error: a hook point that abandons
// its Turn is a contract, not a failure, so the caller asserts the status itself.
func driveCascade(t *testing.T, spec cascadeSpec) cascadeRun {
	t.Helper()

	sink := &recordingSink{}
	toolRuns := 0
	cfg := configWithTools(sink, summaryTool(&toolRuns))
	cfg.Bypass = spec.bypass
	cfg.Mechanisms = domain.NewMechanismRegistry()

	seen := make(map[domain.HookPoint]int, len(allHookPoints))
	probe := cascadeProbe{at: spec.at, behavior: spec.behavior, seen: func(at domain.HookPoint) { seen[at]++ }}
	if spec.experimental {
		if err := cfg.Mechanisms.AddExperimental(spec.at, probe); err != nil {
			t.Fatalf("AddExperimental(%s): %v", spec.at, err)
		}
	} else {
		mustAddMech(t, cfg.Mechanisms, probe.row(spec.capability))
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

// firesAt counts the fires booked for id at hook point at.
func firesAt(events []domain.Event, id domain.MechanismID, at domain.HookPoint) int {
	n := 0
	for _, fe := range mechanismFires(events) {
		if fe.Mechanism == id && fe.Hook == at {
			n++
		}
	}
	return n
}

// hookPanicBooked reports whether the recover boundary turned id's panic into an ErrorEvent
// attributed to id — the containment receipt every hook point shares.
func hookPanicBooked(events []domain.Event, id domain.MechanismID) bool {
	for _, e := range events {
		ee, ok := e.(domain.ErrorEvent)
		if ok && ee.Source == string(id) && strings.HasPrefix(ee.Err, "panic:") {
			return true
		}
	}
	return false
}

// panicContract is what each hook point's recover boundary leaves behind when a hook panics
// during the tool-carrying Turn: whether the tool still ran, and how that Turn ended. The two
// pre-Upstream points abandon the Turn with nothing executed; post-response degrades into the
// response as reviewed so far, so its tool calls still dispatch; pre-tool-exec skips the call
// it was guarding but lets the Turn finish; post-tool-result panics after execution and is
// swallowed outright.
var panicContract = map[domain.HookPoint]struct {
	toolRan bool
	status  domain.StepStatus
}{
	domain.HookHistoryRewrite: {toolRan: false, status: domain.StatusExchangeComplete},
	domain.HookPreRequest:     {toolRan: false, status: domain.StatusExchangeComplete},
	domain.HookPostResponse:   {toolRan: true, status: domain.StatusTurnComplete},
	domain.HookPreToolExec:    {toolRan: false, status: domain.StatusTurnComplete},
	domain.HookPostToolResult: {toolRan: true, status: domain.StatusTurnComplete},
}

// TestHookCascade is the five-point × five-scenario matrix of the shared hook runner: at every
// hook point, booking follows the acted probe for catalogued Mechanisms and nothing else, an
// experimental hook is booked on every invocation, a panic is contained per that point's
// contract, and Bypass drops the catalogued Mechanism before it is ever invoked.
func TestHookCascade(t *testing.T) {
	cells := []struct {
		name string
		run  func(t *testing.T, at domain.HookPoint)
	}{
		{name: "catalogued acts ⇒ booked", run: assertActedFireBooked},
		{name: "catalogued no-op ⇒ not booked", run: assertNoOpNotBooked},
		{name: "experimental ⇒ always booked", run: assertExperimentalAlwaysBooked},
		{name: "panicking hook ⇒ contained", run: assertPanicContained},
		{name: "bypass ⇒ skipped", run: assertBypassSkips},
	}

	for _, at := range allHookPoints {
		for _, cell := range cells {
			t.Run(string(at)+"/"+cell.name, func(t *testing.T) { cell.run(t, at) })
		}
	}
}

// assertActedFireBooked: a catalogued Mechanism that edits the point's working value is booked
// under its real ID at that point.
func assertActedFireBooked(t *testing.T, at domain.HookPoint) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: at, behavior: probeActs, capability: domain.CapOffRamp})

	if run.seen[at] == 0 {
		t.Fatalf("the catalogued Mechanism was never invoked at %s", at)
	}
	if got := firesAt(run.events, cascadeProbeID, at); got == 0 {
		t.Errorf("an acting hook booked no fire at %s; an intervention must be booked", at)
	}
}

// assertNoOpNotBooked: a catalogued Mechanism that inspects and leaves the working value alone
// is invoked but books nothing — fired means ACTED (R4).
func assertNoOpNotBooked(t *testing.T, at domain.HookPoint) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: at, behavior: probeInspects, capability: domain.CapOffRamp})

	if run.seen[at] == 0 {
		t.Fatalf("the catalogued Mechanism was never invoked at %s", at)
	}
	if got := firesAt(run.events, cascadeProbeID, at); got != 0 {
		t.Errorf("an inspect-only hook booked %d fires at %s, want 0", got, at)
	}
}

// assertExperimentalAlwaysBooked: an experimental hook is the bench's instrument — every
// invocation is booked under the synthetic ID, even one that touches nothing.
func assertExperimentalAlwaysBooked(t *testing.T, at domain.HookPoint) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: at, behavior: probeInspects, experimental: true})

	if run.seen[at] == 0 {
		t.Fatalf("the experimental hook was never invoked at %s", at)
	}
	if got := firesAt(run.events, experimentalMechanismID, at); got != run.seen[at] {
		t.Errorf("experimental hook booked %d fires for %d invocations at %s; every invocation is booked", got, run.seen[at], at)
	}
}

// assertPanicContained: a panicking hook degrades to an ErrorEvent under its own ID and the
// loop carries on exactly as that hook point's contract says.
func assertPanicContained(t *testing.T, at domain.HookPoint) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: at, behavior: probePanics, capability: domain.CapOffRamp})

	want := panicContract[at]
	if !hookPanicBooked(run.events, cascadeProbeID) {
		t.Errorf("no ErrorEvent attributed to the panicking Mechanism at %s", at)
	}
	if got := firesAt(run.events, cascadeProbeID, at); got != 0 {
		t.Errorf("a panicking hook booked %d fires at %s, want 0", got, at)
	}
	if ran := run.toolRuns > 0; ran != want.toolRan {
		t.Errorf("tool ran = %v after a panic at %s, want %v", ran, at, want.toolRan)
	}
	if run.toolStatus != want.status {
		t.Errorf("Turn status = %q after a panic at %s, want %q", run.toolStatus, at, want.status)
	}
}

// assertBypassSkips: under Bypass a catalogued non-off-ramp Mechanism is dropped at dispatch —
// never invoked, so nothing to book (D5).
func assertBypassSkips(t *testing.T, at domain.HookPoint) {
	t.Helper()

	run := driveCascade(t, cascadeSpec{at: at, behavior: probeActs, capability: domain.CapProactiveNudge, bypass: true})

	if run.seen[at] != 0 {
		t.Errorf("a Bypass-gated Mechanism was invoked %d times at %s, want 0", run.seen[at], at)
	}
	if got := firesAt(run.events, cascadeProbeID, at); got != 0 {
		t.Errorf("a Bypass-gated Mechanism booked %d fires at %s, want 0", got, at)
	}
}
