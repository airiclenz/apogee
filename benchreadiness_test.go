package apogee_test

// Bench-readiness proof (the ADR 0001 embedding contract, exercised in-repo). This is the
// executable definition of "benchable": it drives the real Agent exactly the way apogee-sim
// will — the public New / Resume / Submit / Step / Snapshot / Close surface over the real
// provider client dialing a scripted OpenAI-compatible httptest model, engine-origin Reactions
// armed at all five seam Moments through Config.Reactions, isolated temp state roots — and
// asserts the contract holds. If a future change breaks the way the bench drives apogee, this
// test breaks first.
//
// It is the ADR 0031 invariant-4 proof ("benchable all the way up") over the Reaction core (ADR
// 0076): a Driver that cannot import internal/* arms its instruments through Config.Reactions
// alone and reads what they did off the ReactionFiredEvent stream. The two internal imports
// that remain — internal/session and internal/tools — are a separate concern: they inspect the
// on-disk session schema and stock the tool menu, not the arming path, and neither is the bare
// root module path, so ADR-0010's "internal never imports root" invariant is untouched.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/session"
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
// The scripted OpenAI-compatible streaming model (one responder, both arms)
// ----------------------------------------------------------------------------

// benchModel returns an httptest server speaking the SSE wire the provider dials. It is
// stateless across requests and decides each reply from the request's own messages, so one
// server drives every Agent (both arms and every fork) without cross-talk: a fresh task asks
// for a directory listing, a request whose history ends in a tool result closes the Exchange
// echoing the task, and a user turn carrying the close marker closes immediately.
func benchModel() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastRole, lastUser := requestTail(r)
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case lastRole == string(apogee.RoleTool):
			writeFinal(w, "completed: "+lastUser)
		case lastRole == string(apogee.RoleUser) && strings.Contains(lastUser, closeMarker):
			writeFinal(w, "completed: "+lastUser)
		default:
			writeToolCall(w, "call_1", "list_dir", `{"path":"."}`)
		}
	}))
}

// requestTail decodes the role of the final message and the text of the last user message —
// the only facts the scripted model branches on.
func requestTail(r *http.Request) (lastRole, lastUser string) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	for _, m := range req.Messages {
		if m.Role == string(apogee.RoleUser) {
			lastUser = m.Content
		}
	}
	if n := len(req.Messages); n > 0 {
		lastRole = req.Messages[n-1].Role
	}
	return lastRole, lastUser
}

// writeToolCall streams one native tool call then a tool_calls finish and the terminator.
func writeToolCall(w http.ResponseWriter, id, name, args string) {
	sseData(w, sseChunk{Choices: []sseChoice{{Delta: sseDelta{ToolCalls: []sseTC{{
		ID: id, Type: "function", Function: sseFunc{Name: name, Arguments: args},
	}}}}}})
	sseData(w, sseChunk{Choices: []sseChoice{{FinishReason: "tool_calls"}}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// writeFinal streams one content chunk then a stop finish and the terminator.
func writeFinal(w http.ResponseWriter, text string) {
	sseData(w, sseChunk{Choices: []sseChoice{{Delta: sseDelta{Content: text}}}})
	sseData(w, sseChunk{Choices: []sseChoice{{FinishReason: "stop"}}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func sseData(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

// The on-the-wire SSE chunk shape the provider parses (the subset this model sets).
type sseChunk struct {
	Choices []sseChoice `json:"choices"`
}

type sseChoice struct {
	Delta        sseDelta `json:"delta"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

type sseDelta struct {
	Content   string  `json:"content,omitempty"`
	ToolCalls []sseTC `json:"tool_calls,omitempty"`
}

type sseTC struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Function sseFunc `json:"function"`
}

type sseFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
// package's Example arms a Reaction over a stub standing in for list_dir.
type stubTool struct{ name string }

func (s stubTool) Name() string          { return s.name }
func (stubTool) Description() string     { return "inert menu-padding tool" }
func (stubTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (stubTool) ReadOnly() bool          { return true }
func (stubTool) Execute(context.Context, apogee.ToolCall) (apogee.ToolResult, error) {
	return apogee.ToolResult{}, nil
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
// apogee-sim will drive it: two arms from one scripted responder against isolated roots,
// engine-origin Reactions armed at all five seam Moments through Config.Reactions,
// snapshot/resume forks, the Bypass floor, and no state bleeding across arms or forks.
func TestBenchReadinessContract(t *testing.T) {
	srv := benchModel()
	defer srv.Close()

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
