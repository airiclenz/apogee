package agent

// The GATE stage of the Approver (ADR 0076 D2): a user's `gate:` entry answers allow / deny / ask
// about a pending tool call, and its answer folds into the verdict the mode ladder already
// reached. These tests drive it where a Driver drives it — through resolveAndExecute and
// prepareDelegation, the two sites dispatch calls it from — because what is under test is the
// FOLD: what a deny does to the call, what an ask does to the Approval, what an allow deliberately
// does not do, and what an unreadable answer costs.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// userGate is one user-origin `gate:` entry over argv, as the reactions: file resolves it.
func userGate(id string, argv ...string) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   domain.ClassGate,
		On:      []domain.Moment{domain.MomentPreToolExec},
		Handler: domain.ArgvHandler{Argv: argv},
	}
}

// goGate is the embedder's half of the same cell: a Go handler answering through Outcome.Gate.
func goGate(id string, decision domain.GateDecision) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginUser,
		Class:  domain.ClassGate,
		On:     []domain.Moment{domain.MomentPreToolExec},
		Handler: domain.PreToolExecFunc(
			func(context.Context, domain.LoopView, *domain.ToolCallEdit) (domain.Outcome, error) {
				return domain.Outcome{Gate: decision}, nil
			}),
	}
}

// gateApprover is fakeApprover plus the request it was handed: the ask upgrade is only observable
// on the prompt, so the test has to read what the human would have read.
type gateApprover struct {
	decision domain.ApprovalDecision
	requests []domain.ApprovalRequest
}

func (a *gateApprover) Approve(_ context.Context, req domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	a.requests = append(a.requests, req)
	return a.decision, nil
}

// gateConfig is an Ask-Before configuration with the given gate entries armed, a read-only tool
// that runs free on that rung and a writing one that gates on it, so a test can pick the ladder
// verdict its case needs. approver may be nil — the embedder that configured none.
func gateConfig(
	t *testing.T,
	sink *recordingSink,
	approver domain.Approver,
	ran *int,
	reactions ...domain.Reaction,
) domain.Config {
	t.Helper()

	cfg := configWithTools(sink,
		fakeTool{name: "list_dir", readOnly: true, ran: ran, result: "main.go"},
		fakeTool{name: "shell", ran: ran, result: "done"},
		tools.NewSubAgent())
	cfg.WorkspaceDir = t.TempDir()
	cfg.Mode = domain.ModeAskBefore
	cfg.Reactions = reactions
	if approver != nil {
		cfg.Approver = approver
	}
	return cfg
}

// gateAgent is gateConfig built into a quiescent Agent — what every case but the Bypass one needs.
func gateAgent(
	t *testing.T,
	sink *recordingSink,
	approver domain.Approver,
	ran *int,
	reactions ...domain.Reaction,
) *Agent {
	t.Helper()

	a, err := newAgent(gateConfig(t, sink, approver, ran, reactions...), echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// readCallOnly is the harmless read every rung of the ladder runs free, so what a gate does to it
// is the gate's doing and nothing else's.
func readCallOnly() domain.ToolCall {
	return domain.ToolCall{ID: "c1", Tool: "list_dir", Arguments: []byte(`{"path":"."}`)}
}

// gateFirings is the firings booked for one reaction id, which is how "booked once" is asserted.
func gateFirings(sink *recordingSink, id string) []domain.ReactionFiredEvent {
	var out []domain.ReactionFiredEvent
	for _, f := range firings(sink.events) {
		if f.Reaction == id {
			out = append(out, f)
		}
	}
	return out
}

// A deny is the whole answer: the tool never runs, the model reads an engine-authored refusal
// naming the reaction, and the firing is booked exactly ONCE — the seam cascade must not fire the
// same gate a second time on its way past pre-tool-exec.
func TestGateDenyRefusesTheCallAndBooksOneFiring(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	ran := 0
	a := gateAgent(t, sink, &fakeApprover{decision: domain.ApprovalAllow}, &ran,
		goGate("no-force-push", domain.GateDecision{Verdict: domain.GateDeny, Reason: "never force-push"}))

	result, outcome := a.resolveAndExecute(context.Background(), 0, readCallOnly())

	if outcome != dispatchDone {
		t.Fatalf("outcome = %v, want dispatchDone", outcome)
	}
	if want := "tool call denied by reaction no-force-push"; result.Content != want {
		t.Errorf("tool result = %q, want %q", result.Content, want)
	}
	if !result.IsError {
		t.Error("a denied call's result is not marked IsError")
	}
	if ran != 0 {
		t.Errorf("the tool ran %d times, want 0", ran)
	}
	booked := gateFirings(sink, "no-force-push")
	if len(booked) != 1 {
		t.Fatalf("booked %d firings, want exactly 1: %+v", len(booked), booked)
	}
	if booked[0].Action != "deny" || booked[0].Moment != domain.MomentPreToolExec {
		t.Errorf("firing = %+v, want action deny at pre-tool-exec", booked[0])
	}
	if booked[0].Detail != "never force-push" {
		t.Errorf("firing Detail = %q, want the gate's reason", booked[0].Detail)
	}
}

// An ask raises the call to the human whatever the ladder said, names the reaction and its reason
// on the prompt, and travels FORCED — the empty CacheKey is the seam's "unrememberable decision"
// signal, so no allow-for-session answer can pre-clear a later call past the gate.
func TestGateArgvAskForcesTheApprover(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	ran := 0
	approver := &gateApprover{decision: domain.ApprovalAllow}
	a := gateAgent(t, sink, approver, &ran,
		userGate("warden", "/bin/sh", "-c", `printf 'ask\nlooks risky\n'`))

	result, _ := a.resolveAndExecute(context.Background(), 0, readCallOnly())

	if len(approver.requests) != 1 {
		t.Fatalf("the Approver was consulted %d times, want 1", len(approver.requests))
	}
	if want := "reaction warden asks: looks risky"; approver.requests[0].Reason != want {
		t.Errorf("Approval reason = %q, want %q", approver.requests[0].Reason, want)
	}
	if approver.requests[0].CacheKey != "" {
		t.Errorf("CacheKey = %q, want empty — a gated ask is never remembered", approver.requests[0].CacheKey)
	}
	if ran != 1 || result.IsError {
		t.Errorf("the allowed call ran %d times, result %+v — want it executed once", ran, result)
	}
	if booked := gateFirings(sink, "warden"); len(booked) != 1 || booked[0].Action != "ask" {
		t.Errorf("firings = %+v, want one ask", booked)
	}
}

// The ask upgrade MUTATES the verdict resolve() computed rather than rebuilding it, because
// resolve() has already stamped facts an approved call still needs: the one out-of-workspace path
// its allow authorises, and — for a Confine — the box its subprocess runs in and the
// runtime-demote fallback. Rebuilding the verdict as a fresh forced gate would drop them silently.
func TestGateAskUpgradeKeepsWhatResolveStamped(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a := gateAgent(t, sink, &gateApprover{decision: domain.ApprovalAllow}, nil,
		goGate("warden", domain.GateDecision{Verdict: domain.GateAsk}))

	box := domain.ConfinementBox{WorkspaceRoot: "/ws"}
	fallback := &resolution{kind: resolveGate, force: true}
	cases := []struct {
		name string
		in   resolution
	}{
		{"a run authorised past the fence", resolution{
			kind:              resolveRun,
			writeEscapeTarget: "/outside/notes.md",
			auditDecision:     security.AuditAllowed,
		}},
		{"a confined subprocess", resolution{
			kind:          resolveConfine,
			box:           box,
			fallback:      fallback,
			auditDecision: security.AuditAllowed,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := a.applyGates(context.Background(), 0, readCallOnly(), tc.in)

			if got.kind != resolveGate || !got.force {
				t.Fatalf("verdict = %v force=%v, want a forced gate", got.kind, got.force)
			}
			if got.reason != "reaction warden asks" {
				t.Errorf("reason = %q, want the bare ask sentence", got.reason)
			}
			if got.writeEscapeTarget != tc.in.writeEscapeTarget {
				t.Errorf("writeEscapeTarget = %q, want %q", got.writeEscapeTarget, tc.in.writeEscapeTarget)
			}
			if got.box.WorkspaceRoot != tc.in.box.WorkspaceRoot || got.fallback != tc.in.fallback {
				t.Errorf("box/fallback = %+v/%v, want them carried through", got.box, got.fallback)
			}
			if tc.in.kind == resolveConfine && !got.confineOnAllow {
				t.Error("a Confine upgraded to an ask must run confined on allow")
			}
			if got.auditDecision != security.AuditAllowed {
				t.Errorf("auditDecision = %q, want it carried through", got.auditDecision)
			}
		})
	}
}

// A gate that cannot answer ASKS. That is the one place the sync lane is not fail-open: a surface
// the user armed to bound the model must not be disarmed by breaking it. The failure is reported
// once through the sync lane's own reporter — an operator line and a "failed" firing — beside the
// ask the human is actually shown.
func TestGateArgvFailureEscalatesToAsk(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	cases := []struct {
		name    string
		argv    []string
		timeout time.Duration
	}{
		{"a non-zero exit", []string{"/bin/sh", "-c", "exit 3"}, 0},
		{"an unreadable answer", []string{"/bin/sh", "-c", "echo maybe"}, 0},
		{"nothing at all", []string{"/bin/sh", "-c", "true"}, 0},
		{"a command that never returns", []string{"/bin/sh", "-c", "sleep 5"}, 150 * time.Millisecond},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			approver := &gateApprover{decision: domain.ApprovalDeny}
			gate := userGate("warden", tc.argv...)
			gate.Timeout = tc.timeout
			a := gateAgent(t, sink, approver, nil, gate)
			var reported []string
			a.cfg.Report = func(msg string) { reported = append(reported, msg) }

			a.resolveAndExecute(context.Background(), 0, readCallOnly())

			if len(approver.requests) != 1 {
				t.Fatalf("the Approver was consulted %d times, want 1", len(approver.requests))
			}
			if want := "reaction warden asks: did not answer ("; !strings.HasPrefix(approver.requests[0].Reason, want) {
				t.Errorf("Approval reason = %q, want it to start %q", approver.requests[0].Reason, want)
			}
			actions := make([]string, 0, 2)
			for _, f := range gateFirings(sink, "warden") {
				actions = append(actions, f.Action)
			}
			if len(actions) != 2 || actions[0] != actionFailed || actions[1] != "ask" {
				t.Errorf("firings = %v, want the failure booked then the ask", actions)
			}
			if len(reported) != 1 || !strings.HasPrefix(reported[0], "reaction warden (pre-tool-exec): ") {
				t.Errorf("reporter lines = %q, want one sync-lane failure line", reported)
			}
		})
	}
}

// The class default a gate runs under when its entry set no `timeout:`. It is pinned on the
// deadline itself rather than by sleeping for five seconds, which would add that to every run of
// the suite for a fact this states exactly (item 4's rationale).
func TestGateClassDefaultDeadline(t *testing.T) {
	t.Parallel()

	if got := syncTimeout(userGate("warden", "/bin/sh", "-c", "echo allow")); got != domain.DefaultGateTimeout {
		t.Errorf("a gate with no timeout: runs for %v, want the %v class default", got, domain.DefaultGateTimeout)
	}
}

// An allow says NOTHING: a script may say No, never Yes, so the ladder's own verdict stands and the
// call gates exactly as it would have with no gate armed — same reason, same rememberable key.
func TestGateAllowLeavesTheLadderVerdictStanding(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	approver := &gateApprover{decision: domain.ApprovalAllow}
	a := gateAgent(t, sink, approver, nil, userGate("warden", "/bin/sh", "-c", "echo allow"))

	a.resolveAndExecute(context.Background(), 0, domain.ToolCall{ID: "c1", Tool: "shell", Arguments: []byte(`{}`)})

	if len(approver.requests) != 1 {
		t.Fatalf("the Approver was consulted %d times, want 1 — Ask-Before still prompts", len(approver.requests))
	}
	if strings.Contains(approver.requests[0].Reason, "warden") {
		t.Errorf("Approval reason = %q, want the ladder's own reason and no mention of the gate",
			approver.requests[0].Reason)
	}
	if approver.requests[0].CacheKey == "" {
		t.Error("CacheKey is empty — an allowed gate must not force the ladder's own prompt")
	}
	if booked := gateFirings(sink, "warden"); len(booked) != 1 || booked[0].Action != "allow" {
		t.Errorf("firings = %+v, want one allow", booked)
	}
}

// Bypass switches off what a reaction says to the MODEL; a gate speaks to the human about what the
// model is about to do, so it stays armed under it (D9).
func TestGateDenyStillDeniesUnderBypass(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	ran := 0
	cfg := gateConfig(t, sink, &fakeApprover{decision: domain.ApprovalAllow}, &ran,
		goGate("warden", domain.GateDecision{Verdict: domain.GateDeny}))
	cfg.Bypass = true
	a, err := newAgent(cfg, echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	result, _ := a.resolveAndExecute(context.Background(), 0, readCallOnly())

	if want := "tool call denied by reaction warden"; result.Content != want {
		t.Errorf("tool result = %q, want %q", result.Content, want)
	}
	if ran != 0 {
		t.Errorf("the tool ran %d times under Bypass, want 0", ran)
	}
}

// A gate reaction never travels the seam cascade: its command answers with a decision the cascade
// has no field to carry, and asserting a PreToolExecFunc against it would fail EVERY tool call
// with "pre-tool-exec reaction failed".
func TestGateNeverFiresInTheSeamCascade(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	a := gateAgent(t, sink, &fakeApprover{decision: domain.ApprovalAllow}, nil,
		userGate("warden", "/bin/sh", "-c", "echo deny"))

	call := readCallOnly()
	if _, err := a.fire(context.Background(), domain.MomentPreToolExec, domain.NewToolCallEdit(&call)); err != nil {
		t.Fatalf("the pre-tool-exec cascade failed with a gate armed: %v", err)
	}
	if booked := gateFirings(sink, "warden"); len(booked) != 0 {
		t.Errorf("the cascade booked %+v, want the gate untouched by it", booked)
	}
}

// A delegation is the one verdict an ask does not change (owner call 2026-09-09): nothing executes
// at the recursion point, the child inherits the gate and asks on the calls that do, so the
// delegation proceeds and the firing says where the question actually lands. A deny still refuses
// it — that answer needs no seam to land on.
func TestGateOnADelegation(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	delegation := domain.ToolCall{
		ID:        "d1",
		Tool:      tools.SubAgentToolName,
		Arguments: []byte(`{"task":"scout"}`),
	}

	t.Run("an ask leaves the delegation standing", func(t *testing.T) {
		t.Parallel()

		sink := &recordingSink{}
		a := gateAgent(t, sink, &gateApprover{decision: domain.ApprovalAllow}, nil,
			userGate("warden", "/bin/sh", "-c", `printf 'ask\nlooks risky\n'`))

		slot := a.prepareDelegation(context.Background(), 0, delegation)

		if !slot.run || slot.verdict.kind != resolveDelegate {
			t.Fatalf("slot = run:%v kind:%v, want the delegation to proceed", slot.run, slot.verdict.kind)
		}
		booked := gateFirings(sink, "warden")
		if len(booked) != 1 || booked[0].Action != "ask" {
			t.Fatalf("firings = %+v, want one ask", booked)
		}
		if want := "deferred to the child's calls"; booked[0].Detail != want {
			t.Errorf("firing Detail = %q, want %q", booked[0].Detail, want)
		}
	})

	t.Run("a deny refuses the delegation", func(t *testing.T) {
		t.Parallel()

		sink := &recordingSink{}
		a := gateAgent(t, sink, &gateApprover{decision: domain.ApprovalAllow}, nil,
			userGate("warden", "/bin/sh", "-c", "echo deny"))

		slot := a.prepareDelegation(context.Background(), 0, delegation)

		if slot.run {
			t.Fatal("the delegation ran despite a denying gate")
		}
		if want := "tool call denied by reaction warden"; slot.result.Content != want {
			t.Errorf("tool result = %q, want %q", slot.result.Content, want)
		}
	})
}

// The child inherits the gate, which is what makes the ask on a delegation safe: the question the
// recursion point could not put to anyone is put on the child's own tool calls.
func TestGateIsInheritedByAChild(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	a := gateAgent(t, sink, &fakeApprover{decision: domain.ApprovalAllow}, nil,
		goGate("warden", domain.GateDecision{Verdict: domain.GateDeny}))

	child, err := a.newChildAgent("d1", "scout", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	gates := child.gateReactions()
	if len(gates) != 1 || gates[0].ID != "warden" {
		t.Fatalf("the child's gates = %+v, want the parent's warden", gates)
	}
}

// finishGate's rule reaches the ask upgrade too: a Gate always means the Approver is actually
// consulted (D5), so an embedder that configured none refuses rather than running unapproved. No
// Driver apogee ships reaches this — an unattended one installs a denying Approver.
func TestGateAskWithNoApproverRefuses(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	ran := 0
	a := gateAgent(t, sink, nil, &ran, goGate("warden", domain.GateDecision{Verdict: domain.GateAsk}))

	result, _ := a.resolveAndExecute(context.Background(), 0, readCallOnly())

	if result.Content != noApproverReason {
		t.Errorf("tool result = %q, want %q", result.Content, noApproverReason)
	}
	if ran != 0 {
		t.Errorf("the tool ran %d times, want 0", ran)
	}
}
