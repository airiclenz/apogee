package apogee_test

// Bench-readiness proof (the ADR 0001 embedding contract, exercised in-repo). This is the
// executable definition of "benchable": it drives the real Agent exactly the way apogee-sim
// will — the public New / Resume / Submit / Step / Snapshot / Close surface over the real
// provider client dialing a stubllm-scripted OpenAI-compatible upstream, engine-origin Reactions
// armed at all five seam Moments through Config.Reactions, isolated temp state roots — and
// asserts the contract holds. If a future change breaks the way the bench drives apogee, this
// test breaks first.
//
// It is the ADR 0031 invariant-4 proof ("benchable all the way up") over the Reaction core (ADR
// 0076): a Driver that cannot import internal/* arms its instruments through Config.Reactions
// alone and reads what they did off the ReactionFiredEvent stream. The three internal imports
// that remain — internal/session, internal/tools and internal/stubllm — are a separate concern:
// they inspect the on-disk session schema, stock the tool menu and script the upstream, not the
// arming path, and none is the bare root module path, so ADR-0010's "internal never imports
// root" invariant is untouched.
//
// The Workflow proof (ADR 0087) holds to the same rule: TestBenchReadinessRunsAWorkflow runs a
// model's `fan_out` call and a Recipe through the facade alone — the Recipe is built from the
// root's WorkflowPlan / WorkflowStage / ReceiptSpec aliases, served through the root's
// RecipeSource port and started by Agent.StartRecipe — and internal/tools only stocks the menu
// with the fan_out tool, which an injected Config.Tools is taken exactly as given without.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
)

const (
	benchModelName = "test-model"

	// closeMarker in a user message tells the scripted model to close the Exchange with a
	// plain reply instead of asking for a tool — how a fork continuation ends in one Turn.
	closeMarker = "PLEASE_CLOSE"

	// complexPrompt is analysis-AND-action intent with six numbered steps: an analysis verb and an
	// action verb, so the ask reads as real work to every seam that classifies intent.
	complexPrompt = "please analyze and then refactor the payment service by working through these steps.\n" +
		"1. read the config parser module.\n" +
		"2. update the request validation logic.\n" +
		"3. add retry handling to the http client.\n" +
		"4. refactor the response serializer.\n" +
		"5. fix the error wrapping in the handlers.\n" +
		"6. write tests for the new behaviour.\n"
)

// benchSeams is the complete five-seam Moment set the probe Reactions are armed at, read off
// the public vocabulary query rather than restated here — a seam added to the engine joins the
// arms without editing this file.
var benchSeams = apogee.Seams()

// probeID is the id of the seam probe armed at Moment m. Ids are unique across the engine's
// builtins and everything armed beside them, so the prefix keeps the probes clear of the seven
// Floor guards, which fire at the same Moments.
func probeID(m apogee.Moment) string { return "bench-probe-" + string(m) }

// adviceProbeID is the second armed Reaction: an advise-class probe at pre-request whose only
// job is to show what Bypass switches off (ADR 0076 D9).
const adviceProbeID = "bench-probe-advice"

// ----------------------------------------------------------------------------
// The scripted OpenAI-compatible streaming model (one Script, both arms)
// ----------------------------------------------------------------------------

// benchScript is the Script the stubllm upstream plays, speaking the SSE wire the provider
// dials. Every Turn repeats and each is selected by the request's own shape, so one Script
// drives every Agent (both arms and every fork) without cross-talk: a request whose history ends
// in the list_dir result closes the Exchange, a user turn carrying the close marker closes
// immediately echoing the token beside the marker, and whatever else is asked — a fresh task —
// gets a directory listing request. The `when:` Turns beat the ordered one for the requests they
// recognise, so the ordered list_dir call is the fallback, not the first reply.
func benchScript() stubllm.Script {
	return stubllm.Script{Model: benchModelName, Turns: []stubllm.Turn{
		// The previous Turn ran list_dir; commit the final assistant message.
		{When: &stubllm.Match{ToolResult: "list_dir"}, Repeat: true, Text: "completed: the task"},
		// A fork continuation: close at once, echoing the token that follows the marker.
		{
			When:     &stubllm.Match{LastMessage: closeMarker},
			Repeat:   true,
			Captures: []stubllm.Capture{{Name: "token", From: "last_message", Pattern: closeMarker + `\s+(\S+)`}},
			Text:     "completed: {{token}}",
		},
		// A fresh task: ask for a directory listing.
		{Repeat: true, ToolCalls: []stubllm.ToolCall{{ID: "call_1", Name: "list_dir", Arguments: `{"path":"."}`}}},
	}}
}

// benchModel starts the scripted upstream on a loopback port for the duration of the test.
func benchModel(t *testing.T) *stubllm.Server {
	t.Helper()
	return stubllm.New(t, benchScript())
}

// ----------------------------------------------------------------------------
// Fixtures: sink, approver, menu-padding tool, five-seam Reaction probe
// ----------------------------------------------------------------------------

// recSink records every emitted Event. It is written only by the goroutine driving Step, so
// it is race-safe under the single-goroutine Agent contract.
type recSink struct{ events []apogee.Event }

func (s *recSink) Emit(e apogee.Event) { s.events = append(s.events, e) }

// allowAll is the human gate for Ask-Before; a read-only list_dir never reaches it, but the
// mode requires a non-nil Approver.
type allowAll struct{}

func (allowAll) Approve(context.Context, apogee.ApprovalRequest) (apogee.ApprovalDecision, error) {
	return apogee.ApprovalAllow, nil
}

// stubTool is an inert read-only tool that pads the menu to a realistic size for the arms. It
// declares ReadOnly so it survives every mode's menu; the arms never call one, while the root
// package's Example arms a Reaction over a stub standing in for list_dir. Its empty result
// still names the call it answers, as every tool's does: that id is what lets the scripted
// upstream recognise the request whose history ends in this tool's result.
type stubTool struct{ name string }

func (s stubTool) Name() string          { return s.name }
func (stubTool) Description() string     { return "inert menu-padding tool" }
func (stubTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (stubTool) ReadOnly() bool          { return true }
func (stubTool) Execute(_ context.Context, call apogee.ToolCall) (apogee.ToolResult, error) {
	return apogee.ToolResult{CallID: call.ID}, nil
}

// fiveSeamProbe is the bench's own instrument: one shared counter behind five engine-origin
// Reactions, one per seam Moment. Each handler records that its seam was passed and returns a
// NON-ZERO Outcome — the intercept a reaction books without moving the working value's revision
// — so every invocation books exactly one ReactionFiredEvent. That is what makes the counters
// and the event stream comparable, which is assertion 2's whole point.
//
// The probes are class observe: ADR 0076 D9 leaves observe (and gate) running under Bypass, so
// the same instrument reads both arms. The advise probe below is what Bypass silences.
type fiveSeamProbe struct{ seen map[apogee.Moment]int }

func (p *fiveSeamProbe) mark(m apogee.Moment) (apogee.Outcome, error) {
	p.seen[m]++
	return apogee.Outcome{Edited: true, Detail: "probe"}, nil
}

// reactions returns the five armed Reactions, one per seam, in loop order.
func (p *fiveSeamProbe) reactions() []apogee.Reaction {
	handlers := map[apogee.Moment]apogee.Handler{
		apogee.MomentPreRequest: apogee.PreRequestFunc(
			func(context.Context, *apogee.Request) (apogee.Outcome, error) {
				return p.mark(apogee.MomentPreRequest)
			}),
		apogee.MomentPostResponse: apogee.PostResponseFunc(
			func(context.Context, *apogee.Response) (apogee.Outcome, error) {
				return p.mark(apogee.MomentPostResponse)
			}),
		apogee.MomentPreToolExec: apogee.PreToolExecFunc(
			func(context.Context, apogee.LoopView, *apogee.ToolCallEdit) (apogee.Outcome, error) {
				return p.mark(apogee.MomentPreToolExec)
			}),
		apogee.MomentPostToolResult: apogee.PostToolResultFunc(
			func(context.Context, apogee.LoopView, apogee.ToolCall, *apogee.ToolResultEdit) (apogee.Outcome, error) {
				return p.mark(apogee.MomentPostToolResult)
			}),
		apogee.MomentHistoryRewrite: apogee.HistoryRewriteFunc(
			func(context.Context, *apogee.Conversation) (apogee.Outcome, error) {
				return p.mark(apogee.MomentHistoryRewrite)
			}),
	}

	armed := make([]apogee.Reaction, 0, len(benchSeams))
	for _, m := range benchSeams {
		armed = append(armed, apogee.Reaction{
			ID:      probeID(m),
			Origin:  apogee.OriginEngine,
			Class:   apogee.ClassObserve,
			On:      []apogee.Moment{m},
			Handler: handlers[m],
		})
	}
	return armed
}

// adviceProbe is one advise-class Reaction at pre-request. Under Bypass an armed advise reaction
// goes quiet (ADR 0076 D9), so its presence in one arm's fired stream and absence from the
// other's is the Bypass floor, asserted through the public event surface alone.
func adviceProbe() apogee.Reaction {
	return apogee.Reaction{
		ID:     adviceProbeID,
		Origin: apogee.OriginEngine,
		Class:  apogee.ClassAdvise,
		On:     []apogee.Moment{apogee.MomentPreRequest},
		Handler: apogee.PreRequestFunc(func(context.Context, *apogee.Request) (apogee.Outcome, error) {
			return apogee.Outcome{Edited: true, Detail: "advice"}, nil
		}),
	}
}

// ----------------------------------------------------------------------------
// Builders
// ----------------------------------------------------------------------------

// armProbe returns the arm one Agent is constructed with — the five seam probes plus the advise
// probe — and the probe instance behind them. Each arm gets a FRESH probe so its counters never
// bleed into the other's, exactly as each arm used to get a fresh registry.
func armProbe() ([]apogee.Reaction, *fiveSeamProbe) {
	probe := &fiveSeamProbe{seen: map[apogee.Moment]int{}}
	return append(probe.reactions(), adviceProbe()), probe
}

// paddedRegistry returns a real list_dir plus enough inert stubs to give the arms a menu of
// realistic size rather than a two-tool toy.
func paddedRegistry(t *testing.T, workspace string) *apogee.ToolRegistry {
	t.Helper()
	reg := apogee.NewToolRegistry()
	if err := reg.Register(tools.NewListDir(workspace, tools.ReadMounts{})); err != nil {
		t.Fatalf("register list_dir: %v", err)
	}
	for i := 0; i < 30; i++ {
		if err := reg.Register(stubTool{name: fmt.Sprintf("stub_tool_%02d", i)}); err != nil {
			t.Fatalf("register stub tool: %v", err)
		}
	}
	return reg
}

// stateRoots is a pair of injected temp directories for one Agent.
type stateRoots struct{ workspace, sessions string }

func newRoots(t *testing.T) stateRoots {
	t.Helper()
	return stateRoots{workspace: t.TempDir(), sessions: t.TempDir()}
}

// runToQuiescence submits input and Steps the Agent to the quiescent boundary that ends the
// Exchange, under a bounded step budget so a misbehaving scenario fails loudly.
func runToQuiescence(t *testing.T, a *apogee.Agent, in apogee.UserInput) {
	t.Helper()
	if err := a.Submit(in); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	stepToQuiescence(t, a)
}

// stepToQuiescence Steps an Agent whose Exchange is already open — by Submit, or by
// Agent.StartRecipe — to the quiescent boundary that ends it, under the same bounded step budget.
func stepToQuiescence(t *testing.T, a *apogee.Agent) {
	t.Helper()
	for i := 0; i < 8; i++ {
		res, err := a.Step(context.Background())
		if err != nil {
			t.Fatalf("Step: %v", err)
		}
		switch res.Status {
		case apogee.StatusExchangeComplete:
			return
		case apogee.StatusCancelled:
			t.Fatalf("Step cancelled unexpectedly")
		}
	}
	t.Fatalf("Exchange did not reach quiescence within the step budget")
}

// ----------------------------------------------------------------------------
// Small event helpers
// ----------------------------------------------------------------------------

// firedEvents is the ONE firing event of the Reaction core, filtered out of an arm's stream:
// builtin Floor guards and armed Reactions alike book one of these when they act (ADR 0076 D1).
func firedEvents(events []apogee.Event) []apogee.ReactionFiredEvent {
	var out []apogee.ReactionFiredEvent
	for _, e := range events {
		if fe, ok := e.(apogee.ReactionFiredEvent); ok {
			out = append(out, fe)
		}
	}
	return out
}

// probeFiresBySeam counts, per seam Moment, the firings booked by THAT seam's probe Reaction —
// so the engine's own builtins, which fire at the same Moments under their own ids, and the
// advise probe are both excluded.
func probeFiresBySeam(fires []apogee.ReactionFiredEvent) map[apogee.Moment]int {
	byMoment := map[apogee.Moment]int{}
	for _, fe := range fires {
		if fe.Reaction == probeID(fe.Moment) {
			byMoment[fe.Moment]++
		}
	}
	return byMoment
}

// firedIDs returns, in emission order, the reaction ids of the fires at Moment m.
func firedIDs(fires []apogee.ReactionFiredEvent, m apogee.Moment) []string {
	var ids []string
	for _, fe := range fires {
		if fe.Moment == m {
			ids = append(ids, fe.Reaction)
		}
	}
	return ids
}

// containsID reports whether ids holds want.
func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func messageText(events []apogee.Event) string {
	var b strings.Builder
	for _, e := range events {
		if me, ok := e.(apogee.MessageEvent); ok {
			b.WriteString(me.Text)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ----------------------------------------------------------------------------
// The proof
// ----------------------------------------------------------------------------

// TestBenchReadinessContract is the permanent regression proving apogee is drivable the way
// apogee-sim will drive it: two arms from one scripted upstream against isolated roots,
// engine-origin Reactions armed at all five seam Moments through Config.Reactions,
// snapshot/resume forks, the Bypass floor, and no state bleeding across arms or forks.
func TestBenchReadinessContract(t *testing.T) {
	srv := benchModel(t)

	// --- Arm A: Reactions armed ------------------------------------------------
	armedRoots := newRoots(t)
	armedSink := &recSink{}
	armedReactions, armedProbe := armProbe()
	reactionArm, err := apogee.New(apogee.Config{
		Endpoint:     srv.URL,
		Model:        benchModelName,
		Mode:         apogee.ModeAskBefore,
		Approver:     allowAll{},
		Events:       armedSink,
		Reactions:    armedReactions,
		Tools:        paddedRegistry(t, armedRoots.workspace),
		WorkspaceDir: armedRoots.workspace,
	})
	if err != nil {
		t.Fatalf("New (Reactions arm): %v", err)
	}
	defer func() { _ = reactionArm.Close() }()

	// --- Arm B: Bypass ---------------------------------------------------------
	bypassRoots := newRoots(t)
	bypassSink := &recSink{}
	bypassReactions, bypassProbe := armProbe()
	bypassArm, err := apogee.New(apogee.Config{
		Endpoint:     srv.URL,
		Model:        benchModelName,
		Mode:         apogee.ModeAskBefore,
		Bypass:       true,
		Approver:     allowAll{},
		Events:       bypassSink,
		Reactions:    bypassReactions,
		Tools:        paddedRegistry(t, bypassRoots.workspace),
		WorkspaceDir: bypassRoots.workspace,
	})
	if err != nil {
		t.Fatalf("New (Bypass arm): %v", err)
	}
	defer func() { _ = bypassArm.Close() }()

	// Drive both arms through the same task to their quiescent boundaries.
	runToQuiescence(t, reactionArm, apogee.UserInput{Text: complexPrompt})
	runToQuiescence(t, bypassArm, apogee.UserInput{Text: complexPrompt})

	armedFires := firedEvents(armedSink.events)

	// === Assertion 1: one ReactionFiredEvent per armed seam ===
	// Each of the five probes is armed on exactly one seam and acts on every invocation, so a
	// seam with no firing booked under its probe's id is a seam the Reaction core never reached.
	firesBySeam := probeFiresBySeam(armedFires)
	for _, m := range benchSeams {
		if firesBySeam[m] == 0 {
			t.Errorf("no ReactionFiredEvent booked at %q; ids seen there: %v", m, firedIDs(armedFires, m))
		}
	}

	// === Assertion 2: R4 — an acted invocation books exactly one firing ===
	// The probe counts its own invocations, so counters and events are directly comparable: an
	// invocation that did not book, or a booking with no invocation behind it, breaks the rule
	// that only ACTED invocations book a fire.
	for _, m := range benchSeams {
		if got, want := firesBySeam[m], armedProbe.seen[m]; got != want {
			t.Errorf("probe at %q booked %d firings for %d invocations; R4: one acted invocation books one", m, got, want)
		}
	}

	// === Assertion 3: every firing is attributed, and engine-origin here ===
	// Both legs of the cascade — the builtin Floor guards and the armed probes — are engine
	// origin in these arms, and every firing names the Moment it fired at.
	for _, fe := range armedFires {
		if fe.Reaction == "" || fe.Moment == "" {
			t.Errorf("unattributed firing: %+v", fe)
		}
		if fe.Origin != apogee.OriginEngine {
			t.Errorf("firing %q at %q has origin %q, want %q", fe.Reaction, fe.Moment, fe.Origin, apogee.OriginEngine)
		}
	}

	// === Assertion 4: the probes read both arms; Bypass silences advise (ADR 0076 D9) ===
	// Observe-class Reactions keep running under Bypass — that is what lets one instrument read
	// both arms — while the advise probe fires in the armed arm and goes quiet under Bypass.
	for _, probe := range []*fiveSeamProbe{armedProbe, bypassProbe} {
		if len(probe.seen) != len(benchSeams) {
			t.Errorf("seam probe fired at %d/%d seams: %v", len(probe.seen), len(benchSeams), probe.seen)
		}
	}
	if !containsID(firedIDs(armedFires, apogee.MomentPreRequest), adviceProbeID) {
		t.Errorf("advise probe booked no firing in the armed arm; pre-request ids: %v",
			firedIDs(armedFires, apogee.MomentPreRequest))
	}
	bypassFires := firedEvents(bypassSink.events)
	if containsID(firedIDs(bypassFires, apogee.MomentPreRequest), adviceProbeID) {
		t.Errorf("advise probe fired under Bypass; ADR 0076 D9 switches armed advise and shape Reactions off")
	}

	// === Assertion 5: agent-driven writes stay inside the injected roots ===
	// Snapshot both arms, and prove a host-persisted session lands under the arm's own sessions root.
	snapArmed, err := reactionArm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot (Reactions arm): %v", err)
	}
	snapBypass, err := bypassArm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot (Bypass): %v", err)
	}
	armedRec := session.Record{Meta: session.Meta{ID: "reactions-arm"}, Session: snapArmed}
	if err := session.NewStore(armedRoots.sessions).Save(armedRec); err != nil {
		t.Fatalf("save Reactions-arm session: %v", err)
	}
	armedSessPath := filepath.Join(armedRoots.sessions, armedRec.Meta.ID+".json")
	if _, err := os.Stat(armedSessPath); err != nil {
		t.Errorf("Reactions-arm session not written under %q: %v", armedRoots.sessions, err)
	}
	bypassRec := session.Record{Meta: session.Meta{ID: "bypass-arm"}, Session: snapBypass}
	if err := session.NewStore(bypassRoots.sessions).Save(bypassRec); err != nil {
		t.Fatalf("save Bypass session: %v", err)
	}

	// === Assertion 6: resumed forks diverge independently, in their own roots ===
	// Two forks resume from the SAME armed snapshot and continue with different inputs; the
	// scripted model echoes each fork's own input, so their outputs diverge and never bleed.
	forkA := resumeFork(t, srv.URL, snapArmed, "follow-up-A")
	forkB := resumeFork(t, srv.URL, snapArmed, "follow-up-B")
	if !strings.Contains(forkA, "follow-up-A") || strings.Contains(forkA, "follow-up-B") {
		t.Errorf("fork A output = %q, want its own input echoed and not fork B's", forkA)
	}
	if !strings.Contains(forkB, "follow-up-B") || strings.Contains(forkB, "follow-up-A") {
		t.Errorf("fork B output = %q, want its own input echoed and not fork A's", forkB)
	}

	// A fork of the OTHER arm (the Bypass snapshot) also resumes and continues independently.
	forkBypass := resumeFork(t, srv.URL, snapBypass, "follow-up-bypass")
	if !strings.Contains(forkBypass, "follow-up-bypass") {
		t.Errorf("fork of the Bypass arm did not continue independently: %q", forkBypass)
	}
}

// resumeFork resumes a fork from snap into fresh isolated roots (nothing armed), continues it
// with a close-marked follow-up carrying token, and returns the fork's rendered message text.
func resumeFork(t *testing.T, endpoint string, snap apogee.Session, token string) string {
	t.Helper()
	roots := newRoots(t)
	sink := &recSink{}
	fork, err := apogee.Resume(apogee.Config{
		Endpoint:     endpoint,
		Model:        benchModelName,
		Mode:         apogee.ModeAskBefore,
		Approver:     allowAll{},
		Events:       sink,
		Tools:        tools.NewDefaultRegistry(roots.workspace),
		WorkspaceDir: roots.workspace,
	}, snap)
	if err != nil {
		t.Fatalf("Resume fork %q: %v", token, err)
	}
	defer func() { _ = fork.Close() }()

	runToQuiescence(t, fork, apogee.UserInput{Text: "wrap up now. " + closeMarker + " " + token})

	return messageText(sink.events)
}

// ----------------------------------------------------------------------------
// Construction acceptance through the public enable surface (no live model)
// ----------------------------------------------------------------------------

// hermeticArm constructs an Agent for a construction-only assertion, arming Reactions through the
// public Config.Reactions into isolated temp roots with a discarding sink. New validates every
// armed Reaction WITHOUT dialing the Endpoint, so the returned error (or nil) reports exactly
// whether the arm is admissible — the fail-loud gate apogee-sim hits when it mis-plans an arm.
func hermeticArm(t *testing.T, reactions []apogee.Reaction) (*apogee.Agent, error) {
	t.Helper()
	return apogee.New(apogee.Config{
		Endpoint:     "http://localhost:11434",
		Model:        benchModelName,
		Mode:         apogee.ModeAskBefore,
		Approver:     allowAll{},
		Events:       &recSink{},
		Reactions:    reactions,
		WorkspaceDir: t.TempDir(),
	})
}

// observeAt is a well-formed engine-origin observe Reaction at Moment m, the shape the refusal
// cases below deform one field at a time.
func observeAt(id string, m apogee.Moment, handler apogee.Handler) apogee.Reaction {
	return apogee.Reaction{
		ID:      id,
		Origin:  apogee.OriginEngine,
		Class:   apogee.ClassObserve,
		On:      []apogee.Moment{m},
		Handler: handler,
	}
}

// inertPreRequest is a handler that inspects and does nothing — enough to make a Reaction well
// formed, so each refusal case below is about the field it deforms and nothing else.
func inertPreRequest() apogee.Handler {
	return apogee.PreRequestFunc(func(context.Context, *apogee.Request) (apogee.Outcome, error) {
		return apogee.Outcome{}, nil
	})
}

// TestBenchReadinessConstructionRefusals proves the campaign's fail-loud arms refuse construction
// through the PUBLIC surface with a matchable sentinel: every mis-armed Reaction fails New with
// apogee.ErrInvalidReaction — the same startup gate the bench hits when it mis-plans an arm,
// asserted only through errors.Is on the root sentinel (ADR 0010: a separate module cannot import
// internal/domain, so the root re-export must BE the sentinel). The four cases are the four ways
// an arm is wrong: an unnamed reaction, a cell outside the Reaction surface matrix, a Moment the
// sealed handler cannot serve, and an id the engine's own builtins already hold.
func TestBenchReadinessConstructionRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		reactions []apogee.Reaction
	}{
		{
			name:      "no id",
			reactions: []apogee.Reaction{observeAt("", apogee.MomentPreRequest, inertPreRequest())},
		},
		{
			name: "a cell outside the Reaction surface matrix",
			reactions: []apogee.Reaction{{
				ID:      "user-shapes-work",
				Origin:  apogee.OriginUser,
				Class:   apogee.ClassShapeWork,
				On:      []apogee.Moment{apogee.MomentPreRequest},
				Handler: inertPreRequest(),
			}},
		},
		{
			name:      "a Moment the handler cannot serve",
			reactions: []apogee.Reaction{observeAt("wrong-seam", apogee.MomentPostResponse, inertPreRequest())},
		},
		{
			name:      "an id an engine builtin already holds",
			reactions: []apogee.Reaction{observeAt("tool-loop-breaker", apogee.MomentPreRequest, inertPreRequest())},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ag, err := hermeticArm(t, tc.reactions)
			if ag != nil {
				_ = ag.Close()
			}
			if !errors.Is(err, apogee.ErrInvalidReaction) {
				t.Fatalf("New(Reactions=%v) error = %v, want errors.Is(err, ErrInvalidReaction)", tc.name, err)
			}
		})
	}
}

// TestBenchReadinessLeaveOneOutArms proves the bench's leave-one-out planning idiom works entirely
// over the public surface: the full five-seam arm constructs, and so does every arm that leaves one
// member out. These are the arms the campaign compares to measure a Reaction's marginal
// contribution; that every one of them is admissible is the contract. Nothing here consults a
// catalogue — with the Reaction core a Driver composes its own arms, so the idiom reduces to
// subtracting one entry from the Config.Reactions list it wrote.
func TestBenchReadinessLeaveOneOutArms(t *testing.T) {
	t.Parallel()
	base, _ := armProbe()

	construct := func(t *testing.T, arm []apogee.Reaction) {
		t.Helper()
		ag, err := hermeticArm(t, arm)
		if err != nil {
			t.Fatalf("New with %d armed Reactions: %v", len(arm), err)
		}
		_ = ag.Close()
	}

	t.Run("full arm", func(t *testing.T) {
		t.Parallel()
		construct(t, base)
	})

	for _, leaveOut := range base {
		t.Run("without "+leaveOut.ID, func(t *testing.T) {
			t.Parallel()
			arm := make([]apogee.Reaction, 0, len(base))
			for _, r := range base {
				if r.ID != leaveOut.ID {
					arm = append(arm, r)
				}
			}
			construct(t, arm)
		})
	}
}

// TestBenchConfigDialsTheProductsPromptAndShape proves a Driver that cannot import internal/* can
// still dial the PRODUCT's agent rather than an approximation of it: the facade hands out the
// default system-prompt template and the shipped per-model shape, and a bench-shaped Config built
// from them constructs. Before the facade exported the two, a bench either ran promptless and
// zero-profile or copied both out of the repo — and its baselines drifted from what the TUI runs.
func TestBenchConfigDialsTheProductsPromptAndShape(t *testing.T) {
	t.Parallel()
	const model = "gpt-oss-20b"

	prompt := apogee.DefaultSystemPrompt()
	profile, shipped := apogee.ShippedProfile(model)

	if prompt == "" {
		t.Fatal("DefaultSystemPrompt() is empty; the product's own prompt is not reachable through the facade")
	}
	if !shipped {
		t.Fatalf("ShippedProfile(%q) matched nothing; the shipped shape table is not reachable through the facade", model)
	}
	if profile.Thinking.Style != apogee.ThinkingHarmony {
		t.Fatalf("ShippedProfile(%q).Thinking.Style = %q, want %q (the shape the product reads gpt-oss in)", model, profile.Thinking.Style, apogee.ThinkingHarmony)
	}
	if unknown, ok := apogee.ShippedProfile("nothing-shipped-knows"); ok || unknown.Thinking.Style != "" || unknown.ToolCallFormat != "" {
		t.Fatalf("ShippedProfile of an unknown model = (%+v, %v), want the zero profile and false", unknown, ok)
	}

	ag, err := apogee.New(apogee.Config{
		Endpoint:     "http://localhost:11434",
		Model:        model,
		Mode:         apogee.ModeAskBefore,
		Approver:     allowAll{},
		Events:       &recSink{},
		SystemPrompt: prompt,
		Profile:      profile,
		WorkspaceDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New with the product's prompt and shape: %v", err)
	}
	_ = ag.Close()
}

// ----------------------------------------------------------------------------
// Workflows (ADR 0087): a model's fan_out and a Recipe, run in-process to quiescence
// ----------------------------------------------------------------------------

// The briefs the two workflows run and the recipe's skill id. {item} names each child's item,
// which the scripted upstream reads back into the child's receipt; {scope} is the recipe's input.
const (
	fanOutTask = "check {item} carefully and report"
	recipeTask = "check {item} carefully in {scope} and report"
	recipeID   = "bench-review"
)

// workflowScript is the Script both workflows play. Every Turn is selected by the request's own
// shape, so it holds however the item children interleave: an item child hands back an ok
// receipt naming its item through `finish`; the parent that has the fan_out's result, or the
// recipe's result lines, closes the Exchange; and a fresh fan-out request calls fan_out over two
// items.
func workflowScript() stubllm.Script {
	fanOutArgs, _ := json.Marshal(map[string]any{
		"task":    fanOutTask,
		"over":    []string{"alpha", "beta"},
		"returns": map[string]string{"count": "int"},
	})
	return stubllm.Script{Model: benchModelName, Turns: []stubllm.Turn{
		{When: &stubllm.Match{ToolResult: tools.FanOutToolName}, Repeat: true, Text: "fanned out"},
		{When: &stubllm.Match{LastMessage: `^/` + recipeID}, Repeat: true, Text: "reviewed"},
		{
			When:     &stubllm.Match{LastMessage: `check \w+ carefully`},
			Repeat:   true,
			Captures: []stubllm.Capture{{Name: "item", From: "last_message", Pattern: `check (\w+) carefully`}},
			ToolCalls: []stubllm.ToolCall{{
				ID: "call_finish", Name: tools.FinishToolName,
				Arguments: `{"status":"ok","summary":"{{item}} is fine","count":1}`,
			}},
		},
		{
			When:      &stubllm.Match{LastMessage: "please fan out"},
			Repeat:    true,
			ToolCalls: []stubllm.ToolCall{{ID: "call_fan_out", Name: tools.FanOutToolName, Arguments: string(fanOutArgs)}},
		},
	}}
}

// lockedSink records every emitted Event under a lock: a Workflow's item children run beside
// each other, so their events may reach the sink from more than one goroutine.
type lockedSink struct {
	mu     sync.Mutex
	events []apogee.Event
}

func (s *lockedSink) Emit(e apogee.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

// workflowPhases returns the Workflow phase events the sink recorded, in emission order.
func (s *lockedSink) workflowPhases() []apogee.WorkflowPhaseEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []apogee.WorkflowPhaseEvent
	for _, e := range s.events {
		if we, ok := e.(apogee.WorkflowPhaseEvent); ok {
			out = append(out, we)
		}
	}
	return out
}

// benchRecipes is the bench's own recipe catalog, built from the root's aliases alone: one skill,
// a Recipe of one fanout stage over alpha and beta whose brief names its required `scope` input.
// It is both the SkillResolver an attach reads and the RecipeSource a launch reads.
type benchRecipes struct{}

func (benchRecipes) ResolveSkills(ids []string) []apogee.ResolvedSkill {
	var out []apogee.ResolvedSkill
	for _, id := range ids {
		if id == recipeID {
			out = append(out, apogee.ResolvedSkill{ID: id, DisplayName: id, Body: "BENCH REVIEW BODY"})
		}
	}
	return out
}

func (benchRecipes) Recipe(id string) (apogee.Recipe, bool) {
	if id != recipeID {
		return apogee.Recipe{}, false
	}
	return apogee.Recipe{
		ID: recipeID,
		Plan: apogee.WorkflowPlan{Name: recipeID, Stages: []apogee.WorkflowStage{{
			Name:    "items",
			Kind:    apogee.StageFanout,
			Task:    recipeTask,
			Over:    &apogee.ItemSource{List: []string{"alpha", "beta"}},
			Returns: apogee.ReceiptSpec{"count": "int"},
		}}},
		Inputs: []apogee.InputDecl{{Name: "scope", Required: true, Description: "the folder to review"}},
		Dir:    "/bench/skills/" + recipeID,
	}, true
}

func (benchRecipes) RecipeIDs() []string { return []string{recipeID} }

// The bench's catalog must satisfy both ports the engine reads off Config.Skills.
var (
	_ apogee.SkillResolver = benchRecipes{}
	_ apogee.RecipeSource  = benchRecipes{}
)

// workflowArm constructs an Agent that can run a Workflow: fan_out on its injected menu beside a
// real list_dir, the bench's recipe catalog as its skills, and a scratch directory of its own for
// the workflow folders.
func workflowArm(t *testing.T, endpoint string, sink apogee.EventSink) *apogee.Agent {
	t.Helper()
	roots := newRoots(t)
	reg := apogee.NewToolRegistry()
	if err := reg.Register(tools.NewListDir(roots.workspace, tools.ReadMounts{})); err != nil {
		t.Fatalf("register list_dir: %v", err)
	}
	if err := reg.Register(tools.NewFanOut()); err != nil {
		t.Fatalf("register fan_out: %v", err)
	}
	a, err := apogee.New(apogee.Config{
		Endpoint:     endpoint,
		Model:        benchModelName,
		Mode:         apogee.ModeAskBefore,
		Approver:     allowAll{},
		Events:       sink,
		Tools:        reg,
		Skills:       benchRecipes{},
		WorkspaceDir: roots.workspace,
		ScratchDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New (workflow arm): %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// wantItemLines fails unless text carries one ok result line per item and the totals line.
func wantItemLines(t *testing.T, text string) {
	t.Helper()
	for _, want := range []string{
		"#1 alpha — ok — alpha is fine count=1",
		"#2 beta — ok — beta is fine count=1",
		"items 2 · ok 2 · partial 0 · blocked 0",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("result lacks %q:\n%s", want, text)
		}
	}
}

// wantPhases fails unless the sink saw the workflow start, finish each item, and finish.
func wantPhases(t *testing.T, sink *lockedSink) {
	t.Helper()
	seen := map[apogee.WorkflowPhase]int{}
	for _, e := range sink.workflowPhases() {
		seen[e.Phase]++
	}
	if seen[apogee.WorkflowStarted] != 1 || seen[apogee.WorkflowItemFinished] != 2 || seen[apogee.WorkflowFinished] != 1 {
		t.Errorf("workflow phases = %v, want one started, two items finished, one finished", seen)
	}
}

// wantOneFinishedWorkflow fails unless Agent.Workflows lists exactly one Workflow, run to its end
// in the foreground, and returns it.
func wantOneFinishedWorkflow(t *testing.T, a *apogee.Agent) apogee.WorkflowInfo {
	t.Helper()
	listed, err := a.Workflows()
	if err != nil {
		t.Fatalf("Workflows: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("Workflows listed %d, want the one this Exchange ran", len(listed))
	}
	info := listed[0]
	if info.Status.Phase != "done" || info.Background || info.Queued || info.Dir == "" {
		t.Errorf("listed workflow = phase %q, background %v, queued %v, dir %q; want a finished foreground one with a folder",
			info.Status.Phase, info.Background, info.Queued, info.Dir)
	}
	return info
}

// TestBenchReadinessRunsAWorkflow proves a Driver that cannot import internal/* can run both kinds
// of Workflow in-process and read what they did (ADR 0087, ADR 0031 invariant 4): a model's
// blocking fan_out call and a Recipe started by Agent.StartRecipe each run their item children
// against the scripted upstream to quiescence, report their phases on the public event stream, and
// are listed by Agent.Workflows with the folder their record lives in.
func TestBenchReadinessRunsAWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("fan_out", func(t *testing.T) {
		t.Parallel()
		srv := stubllm.New(t, workflowScript())
		sink := &lockedSink{}
		a := workflowArm(t, srv.URL, sink)

		runToQuiescence(t, a, apogee.UserInput{Text: "please fan out over the two modules"})

		var result string
		for _, e := range sink.events {
			if re, ok := e.(apogee.ToolResultEvent); ok && re.Depth == 0 && re.Result.CallID == "call_fan_out" {
				result = re.Result.Content
			}
		}
		if result == "" {
			t.Fatal("no depth-0 result answered the fan_out call")
		}
		wantItemLines(t, result)
		wantPhases(t, sink)
		if info := wantOneFinishedWorkflow(t, a); info.Status.Recipe != "" {
			t.Errorf("a fan_out's workflow names recipe %q, want none", info.Status.Recipe)
		}
		if unmatched := srv.Unmatched(); len(unmatched) != 0 {
			t.Errorf("the upstream saw %d requests no Turn answered", len(unmatched))
		}
	})

	t.Run("recipe", func(t *testing.T) {
		t.Parallel()
		srv := stubllm.New(t, workflowScript())
		sink := &lockedSink{}
		a := workflowArm(t, srv.URL, sink)

		id, err := a.StartRecipe(context.Background(), apogee.RecipeLaunch{SkillID: recipeID, Text: "src"})
		if err != nil || id != "" {
			t.Fatalf("StartRecipe = %q, %v; want a foreground launch", id, err)
		}
		stepToQuiescence(t, a)

		var parent string
		for n := range srv.Requests() {
			if last := srv.LastMessage(n + 1); strings.HasPrefix(last, "/"+recipeID) {
				parent = last
			}
		}
		if !strings.HasPrefix(parent, "/"+recipeID+" src\n\n") {
			t.Fatalf("the model's request does not open with the user's line:\n%s", parent)
		}
		wantItemLines(t, parent)
		bound := false
		for n := range srv.Requests() {
			bound = bound || strings.Contains(srv.LastMessage(n+1), "check alpha carefully in src")
		}
		if !bound {
			t.Error("no item child's brief carried the scope input bound from the launch text")
		}
		if strings.Contains(parent, "BENCH REVIEW BODY") {
			t.Errorf("the launch attached the skill body instead of running the recipe:\n%s", parent)
		}
		wantPhases(t, sink)
		if info := wantOneFinishedWorkflow(t, a); info.Status.Recipe != recipeID {
			t.Errorf("the recipe's workflow names recipe %q, want %q", info.Status.Recipe, recipeID)
		}
		if unmatched := srv.Unmatched(); len(unmatched) != 0 {
			t.Errorf("the upstream saw %d requests no Turn answered", len(unmatched))
		}
	})
}
