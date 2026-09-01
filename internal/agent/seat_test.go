package agent

// The Delegation SEAT one sub_agent call may name (ADR 0069): `run_on: "session"` builds the child
// on the parent's own Upstream whatever is latched, `run_on: "sub-agents-server"` routes it and
// says so in the result when it cannot, and an absent value leaves the `sub-agents-server` key
// deciding exactly as it did before the parameter existed.
//
// routedspawn_test.go owns the CONSTRUCTION facts of a routed child (dial facts, window, profile,
// posture) and drives newChildAgent, the default-seat wrapper. These tests own the axis that
// wrapper hides: which seat a spawn is built for, what a fallback tells the parent, and where the
// choice stops being offered.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// spawnOn is the one-line child construction these tests repeat, for a named seat.
func spawnOn(t *testing.T, parent *Agent, seat delegationSeat) *Agent {
	t.Helper()
	child, err := parent.newChildAgentOn(seat, "call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgentOn: %v", err)
	}
	return child
}

// postureTarget is routedTarget with a bypass posture of its own, so a child that did NOT take the
// far seat can be seen not taking the far seat's posture either (ADR 0069 decision 8).
func postureTarget() *DelegationTarget {
	target := routedTarget()
	bypass := true
	target.Bypass = &bypass
	return target
}

// seatChoiceRegistry is a tool set whose sub_agent PUBLISHES `run_on` — what the host builds under
// `sub-agents-choice: model`, and the only shape in which a seat a call names is read at all.
func seatChoiceRegistry(t *testing.T) *domain.ToolRegistry {
	t.Helper()
	reg := domain.NewToolRegistry()
	if err := reg.Register(tools.NewSubAgentWith(tools.SubAgentOptions{SeatChoice: true})); err != nil {
		t.Fatalf("register the seat-choice sub_agent: %v", err)
	}
	if err := reg.Register(fakeTool{name: "w"}); err != nil {
		t.Fatalf("register a leaf tool: %v", err)
	}
	return reg
}

// TestSeat_SessionAskRunsOnTheParentUnderALatchedTarget is decision 8: the seat carries everything
// the seat means. A child asked for the session server is built the way a child of an UNROUTED
// session is built — the parent's Upstream, model, window and posture — even though a usable
// target is latched and a silent delegation would have gone there.
func TestSeat_SessionAskRunsOnTheParentUnderALatchedTarget(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)
	target := postureTarget()
	parent.SetDelegationTarget(target)

	child := spawnOn(t, parent, seatSession)

	if child.upstream != parent.upstream {
		t.Error("session-seated child does not share the parent's Upstream responder, want the unrouted path verbatim")
	}
	if child.ownsUpstream {
		t.Error("session-seated child claims to own its Upstream; it borrowed the parent's and must never close it")
	}
	if child.cfg.Endpoint != parent.cfg.Endpoint || child.cfg.Model != parent.cfg.Model {
		t.Errorf("session-seated child dial facts = %q/%q, want the parent's %q/%q",
			child.cfg.Endpoint, child.cfg.Model, parent.cfg.Endpoint, parent.cfg.Model)
	}
	if child.cfg.Context.MaxContextTokens != parent.cfg.Context.MaxContextTokens {
		t.Errorf("session-seated child window = %d, want the parent's %d",
			child.cfg.Context.MaxContextTokens, parent.cfg.Context.MaxContextTokens)
	}
	// The posture keys on the flagged entry say what delegations TO THAT SERVER run as, and this
	// delegation went elsewhere, so the target's bypass must not reach it.
	if child.cfg.Bypass != parent.bypassEnabled() {
		t.Errorf("session-seated child bypass = %v, want the parent's %v — the target's posture followed a delegation that never went there",
			child.cfg.Bypass, parent.bypassEnabled())
	}
	if child.seatFallback {
		t.Error("session-seated child is marked a fallback; it got exactly the seat it asked for")
	}
}

// TestSeat_SubAgentsServerAskRoutes is the other explicit ask against the same latch: it takes the
// routed path newChildAgent already takes for a silent delegation, so naming the far seat when the
// far seat is where a silent delegation would go changes nothing about the child.
func TestSeat_SubAgentsServerAskRoutes(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)
	target := routedTarget()
	parent.SetDelegationTarget(target)

	child := spawnOn(t, parent, seatSubAgentsServer)

	if child.cfg.Endpoint != target.Endpoint || child.cfg.Model != target.Model {
		t.Errorf("routed child dial facts = %q/%q, want the target's %q/%q",
			child.cfg.Endpoint, child.cfg.Model, target.Endpoint, target.Model)
	}
	if child.upstream == parent.upstream || !child.ownsUpstream {
		t.Error("routed child did not dial a client of its own, want the routed spawn's own Upstream")
	}
	if child.seatFallback {
		t.Error("routed child is marked a fallback; the target it asked for was latched")
	}
}

// TestSeat_SubAgentsServerAskFallsBackToTheSession is decision 9's first half: no beat yet, the
// server down, no model loaded — the ask cannot be honoured, so the spawn degrades to the session
// server exactly as static routing already degrades (ADR 0045 §4) and the run happens. The mark it
// leaves is what the result's note is rendered from; a silent delegation under the same nil latch
// takes the identical path and leaves no mark, because it asked for nothing.
func TestSeat_SubAgentsServerAskFallsBackToTheSession(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)

	child := spawnOn(t, parent, seatSubAgentsServer)

	if child.upstream != parent.upstream || child.ownsUpstream {
		t.Error("fallen-back child did not take the parent's Upstream, want ADR 0045 §4's fallback verbatim")
	}
	if !child.seatFallback {
		t.Error("fallen-back child is not marked a fallback; its result would never tell the parent its routing decision was overruled")
	}

	silent := spawnOn(t, parent, seatConfigured)
	if silent.seatFallback {
		t.Error("a delegation that named no seat is marked a fallback; nothing was overruled, so its result must be byte-identical to today's")
	}
}

// TestSeat_FallbackNoteIsTheBodysLastLine pins the contract line the parent model reads, on every
// outcome that produces a result: the note is APPENDED, so the head each outcome opens with — the
// step-cap marker, the fault prefix, the child's own first line — is untouched and the recognisers
// anchored there keep matching.
func TestSeat_FallbackNoteIsTheBodysLastLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		child    *Agent
		res      domain.StepResult
		err      error
		wantHead string
	}{
		{"run error", &Agent{seatFallback: true}, domain.StepResult{}, errors.New("boom"), "sub-agent failed: boom"},
		{"faulted", &Agent{seatFallback: true, lastFault: "the upstream died"}, domain.StepResult{Faulted: true}, nil, subAgentFaultPrefix + "the upstream died"},
		{"step capped", &Agent{seatFallback: true, stepCap: 3}, domain.StepResult{StepCapped: true}, nil, fmt.Sprintf(stepCapResultFormat, 3)},
		{"success", &Agent{seatFallback: true}, domain.StepResult{}, nil, "(sub-agent completed with no final message)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, outcome := tc.child.delegationResult("c1", tc.res, tc.err)

			if outcome != dispatchDone {
				t.Fatalf("outcome = %v, want dispatchDone", outcome)
			}
			if !strings.HasPrefix(got.Content, tc.wantHead) {
				t.Errorf("result = %q, want it to open with the outcome's own head %q — the note is appended, never prefixed", got.Content, tc.wantHead)
			}
			if last := lastLine(got.Content); last != seatFallbackNote {
				t.Errorf("result's last line = %q, want the routing note %q", last, seatFallbackNote)
			}
		})
	}
}

// TestSeat_FallbackNoteSitsUnderTheSteeredTrailer is the collision ADR 0069 decision 9 settles:
// where both apply, ADR 0063 D3's trailer stays the result's FINAL line and the note is the last
// line of the BODY, immediately above it.
func TestSeat_FallbackNoteSitsUnderTheSteeredTrailer(t *testing.T) {
	t.Parallel()

	child := &Agent{seatFallback: true, steered: 1}

	got, _ := child.delegationResult("c1", domain.StepResult{}, nil)

	trailer := "\n\n" + userSteeredTrailerSingular
	if !strings.HasSuffix(got.Content, trailer) {
		t.Fatalf("result = %q, want the steered trailer as its final line", got.Content)
	}
	body := strings.TrimSuffix(got.Content, trailer)
	if last := lastLine(body); last != seatFallbackNote {
		t.Errorf("body's last line = %q, want the routing note %q immediately above the trailer", last, seatFallbackNote)
	}
}

// TestSeat_ResultWithoutAFallbackIsByteIdentical is the floor the note must not move: a delegation
// that was not overruled reports exactly what it reported before seats existed.
func TestSeat_ResultWithoutAFallbackIsByteIdentical(t *testing.T) {
	t.Parallel()

	plain := &Agent{}
	steered := &Agent{steered: 2}

	got, _ := plain.delegationResult("c1", domain.StepResult{}, nil)
	if want := "(sub-agent completed with no final message)"; got.Content != want {
		t.Errorf("unfallen result = %q, want %q", got.Content, want)
	}

	got, _ = steered.delegationResult("c1", domain.StepResult{}, nil)
	if want := "(sub-agent completed with no final message)\n\n" + userSteeredTrailer(2); got.Content != want {
		t.Errorf("unfallen steered result = %q, want %q", got.Content, want)
	}
}

// TestSeat_SessionSeatedChildDelegatesOnTheSessionServer is the identity rule read for the seat the
// model chose (ADR 0069 decision 3 + ADR 0045 decision 1): the child's own tool carries no `run_on`,
// so it can neither confirm nor undo the placement, and a shared latch would send its grandchildren
// to the very server this branch was steered away from. A child on the CONFIGURED seat still shares
// the parent's holder, which is what makes routing reach every depth from one push.
func TestSeat_SessionSeatedChildDelegatesOnTheSessionServer(t *testing.T) {
	t.Parallel()

	parent := routingParent(t)
	parent.SetDelegationTarget(routedTarget())

	child := spawnOn(t, parent, seatSession)

	if child.delegationTarget() != nil {
		t.Error("session-seated child reads a latched target, want an empty latch of its own")
	}
	grandchild, err := child.newChildAgent("call_sub_sub", "the nested task", "")
	if err != nil {
		t.Fatalf("nested newChildAgent: %v", err)
	}
	if grandchild.upstream != parent.upstream || grandchild.ownsUpstream {
		t.Error("a session-seated child's nested delegation routed away, want it on the session server the branch was put on")
	}

	routed := spawnOn(t, parent, seatConfigured)
	if routed.delegationTarget() == nil {
		t.Error("a default-seat child holds no target, want the parent's shared latch so a later push reaches its own spawns")
	}
}

// TestSeat_ParseRejectsAnythingButTheTwoSpellings pins the vocabulary the schema publishes, and the
// exact refusal the model reads: naming both accepted values is the whole of what the result can
// teach it.
func TestSeat_ParseRejectsAnythingButTheTwoSpellings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want delegationSeat
	}{
		{"", seatConfigured},
		{tools.RunOnSession, seatSession},
		{tools.RunOnSubAgentsServer, seatSubAgentsServer},
	}
	for _, tc := range cases {
		t.Run("accepts "+tc.raw, func(t *testing.T) {
			got, err := parseDelegationSeat(tc.raw)

			if err != nil {
				t.Fatalf("parseDelegationSeat(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("parseDelegationSeat(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}

	t.Run("refuses anything else", func(t *testing.T) {
		_, err := parseDelegationSeat("banana")

		if err == nil {
			t.Fatal("parseDelegationSeat(\"banana\") returned no error")
		}
		want := `invalid run_on "banana": want "session" or "sub-agents-server"`
		if err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})
}

// TestSeat_RunSubAgentRefusesAnUnknownRunOn proves the refusal reaches the model as a result and
// costs no child: the seat is resolved before anything is constructed.
func TestSeat_RunSubAgentRefusesAnUnknownRunOn(t *testing.T) {
	t.Parallel()

	parent := &Agent{tools: seatChoiceRegistry(t)}

	res, outcome := parent.runSubAgent(context.Background(), domain.ToolCall{
		ID:        "c1",
		Tool:      tools.SubAgentToolName,
		Arguments: json.RawMessage(`{"task":"scout the config keys","run_on":"banana"}`),
	})

	if outcome != dispatchDone {
		t.Fatalf("outcome = %v, want dispatchDone", outcome)
	}
	if !res.IsError {
		t.Fatalf("result = %+v, want an error result", res)
	}
	want := `invalid run_on "banana": want "session" or "sub-agents-server"`
	if res.Content != want {
		t.Errorf("result = %q, want %q", res.Content, want)
	}
}

// TestSeat_ChildRosterDropsTheRunOnArgument is decision 3 where it is enforced: the choice is a
// depth-0 offer, so the tool a child is handed is the PLAIN variant — byte-identical to the schema
// this tool published before seat choice existed — while the parent keeps offering it. Removing the
// parameter rather than accepting and discarding it is the honest form.
func TestSeat_ChildRosterDropsTheRunOnArgument(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Tools = seatChoiceRegistry(t)
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	roster := parent.defaultSubAgentTools()

	inherited, ok := roster.Lookup(tools.SubAgentToolName)
	if !ok {
		t.Fatal("the child's roster has no sub_agent tool")
	}
	spawner, ok := inherited.(*tools.SubAgent)
	if !ok {
		t.Fatalf("the child's sub_agent is a %T, want *tools.SubAgent", inherited)
	}
	if spawner.OffersSeatChoice() {
		t.Error("the child's sub_agent offers seat choice, want the plain variant — the choice is a depth-0 offer")
	}
	if want := string(tools.NewSubAgent().Schema()); string(spawner.Schema()) != want {
		t.Errorf("the child's sub_agent schema = %s, want the plain schema %s", spawner.Schema(), want)
	}
	// The parent is untouched, and so is every other tool it delegates.
	if !publishesSeatChoice(parent.tools) {
		t.Error("narrowing the child's roster took the parameter off the PARENT's tool too")
	}
	if _, hasLeaf := roster.Lookup("w"); !hasLeaf {
		t.Error("the child's roster lost a leaf tool while its sub_agent was swapped")
	}
}

// TestSeat_PlainParentRosterIsInheritedUnchanged is the floor for every session that never enables
// the choice: with a plain sub_agent nothing is rebuilt, so the child holds the parent's own tool
// values and the roster is what it always was.
func TestSeat_PlainParentRosterIsInheritedUnchanged(t *testing.T) {
	t.Parallel()

	cfg := configWithTools(&recordingSink{}, tools.NewSubAgent(), fakeTool{name: "w"})
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	roster := parent.defaultSubAgentTools()

	parentTool, _ := parent.tools.Lookup(tools.SubAgentToolName)
	childTool, ok := roster.Lookup(tools.SubAgentToolName)
	if !ok {
		t.Fatal("the child's roster has no sub_agent tool")
	}
	if childTool != parentTool {
		t.Error("a plain sub_agent was rebuilt on its way to the child, want the parent's own value verbatim")
	}
}

// TestSeat_RunOnIsIgnoredWhereItWasNeverPublished is the identity rule's other half: a child — or
// any agent under `sub-agents-choice: fixed` — whose tool carries no `run_on` sends one anyway, and
// the engine neither honours nor refuses it. Refusing would be an error the model cannot act on for
// an argument it was never told about; honouring would let a delegation move a seat it was not
// offered. The delegation simply runs on the configured seat and reports back.
func TestSeat_RunOnIsIgnoredWhereItWasNeverPublished(t *testing.T) {
	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)

	args, err := json.Marshal(tools.SubAgentArgs{Task: "summarise the repo", RunOn: "banana"})
	if err != nil {
		t.Fatalf("marshal the call arguments: %v", err)
	}
	a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", tools.SubAgentToolName, string(args)),
		contentScript("the repo is a Go TUI agent"),
		contentScript("done — delegated and summarised"),
	}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "please research"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	res, ok := lastSubAgentResult(sink.events)
	if !ok {
		t.Fatal("no sub_agent tool result emitted")
	}
	if res.IsError {
		t.Fatalf("sub_agent result is an error: %q — an unpublished run_on must be ignored, not refused", res.Content)
	}
	if !strings.Contains(res.Content, "Go TUI agent") {
		t.Errorf("sub_agent result = %q, want the child's final message", res.Content)
	}
	if strings.Contains(res.Content, seatFallbackNote) {
		t.Errorf("sub_agent result = %q, want no routing note — the seat was never read, so nothing was overruled", res.Content)
	}
}

// lastLine returns the final line of s — what a reader of a delegation result sees last, and the
// position both the routing note and the steered trailer are contracted to.
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}
