package apogee_test

// Bench-readiness proof (the ADR 0001 embedding contract, exercised in-repo). This is the
// executable definition of "benchable": it drives the real Agent exactly the way apogee-sim
// will — the public New / Resume / Submit / Step / Snapshot / Close surface over the real
// provider client dialing a scripted OpenAI-compatible httptest model, catalogued Mechanisms
// armed by ID through Config.EnableMechanisms, experimental hooks at all five hook points via
// AddExperimental, isolated temp state roots — and asserts the contract holds. If a future
// change breaks the way the bench drives apogee, this test breaks first.
//
// It arms every Mechanism through the PUBLIC enable surface (Config.EnableMechanisms +
// AddExperimental, ADR 0015): it no longer builds the catalogue by hand and imports neither
// internal/mechanisms nor internal/library, so a separate module (apogee-sim, which cannot
// import internal/*) can now do everything this test does. The two internal imports that
// remain — internal/session and internal/tools — are a separate concern: they inspect the
// on-disk session schema and stock the tool menu, not the enable path, and neither is the
// bare root module path, so ADR-0010's "internal never imports root" invariant is untouched.

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
	"slices"
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

	// complexPrompt is analysis-AND-action intent with six numbered steps: enough structural
	// complexity for decompose to act (score 10 ⇒ "complex"), an action verb so its step-hint
	// branch engages, and an analysis verb so toolfilter keeps the read tools and the library
	// observer records the shallow-exploration note (list-without-read on an analysis request).
	complexPrompt = "please analyze and then refactor the payment service by working through these steps.\n" +
		"1. read the config parser module.\n" +
		"2. update the request validation logic.\n" +
		"3. add retry handling to the http client.\n" +
		"4. refactor the response serializer.\n" +
		"5. fix the error wrapping in the handlers.\n" +
		"6. write tests for the new behaviour.\n"
)

// allHooks is the complete five-point hook set the experimental probe is registered at.
var allHooks = []apogee.HookPoint{
	apogee.HookPreRequest,
	apogee.HookPostResponse,
	apogee.HookPreToolExec,
	apogee.HookPostToolResult,
	apogee.HookHistoryRewrite,
}

// enabledMechanisms is the multi-wave set the mechanisms-on arm enables via Config: two
// request shapers that ACT every pre-request (toolfilter — wave 3, decompose — wave 4),
// one history-rewrite shaper that stays inspect-only on a short conversation (truncate_history
// — wave 2), and the learning Mechanism whose observe half writes into the injected LibraryDir
// (library — item 14). toolfilter declares "Before decompose", so their fired order is the
// registry's deterministic dispatch order.
var enabledMechanisms = []apogee.MechanismID{"toolfilter", "decompose", "truncate_history", "library"}

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
// Fixtures: sink, approver, menu-padding tool, five-point experimental probe
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

// stubTool is an inert read-only tool that pads the menu past toolfilter's 30-tool activation
// threshold. It declares ReadOnly so it survives every mode's menu, and it is never called.
type stubTool struct{ name string }

func (s stubTool) Name() string          { return s.name }
func (stubTool) Description() string     { return "inert menu-padding tool" }
func (stubTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (stubTool) ReadOnly() bool          { return true }
func (stubTool) Execute(context.Context, apogee.ToolCall) (apogee.ToolResult, error) {
	return apogee.ToolResult{}, nil
}

// fivePointProbe implements all five hook interfaces, recording which points fired without
// acting. Registered via AddExperimental at every point, it is the bench's own instrument:
// dispatched at each hook point, always booked under the synthetic "experimental" ID, and never
// Bypass-gated.
type fivePointProbe struct{ seen map[apogee.HookPoint]int }

func (p *fivePointProbe) RewriteHistory(context.Context, *apogee.Conversation) error {
	p.seen[apogee.HookHistoryRewrite]++
	return nil
}

func (p *fivePointProbe) PreRequest(context.Context, *apogee.Request) error {
	p.seen[apogee.HookPreRequest]++
	return nil
}

func (p *fivePointProbe) PostResponse(context.Context, *apogee.Response) (apogee.PostResponseDecision, error) {
	p.seen[apogee.HookPostResponse]++
	return apogee.PostResponseDecision{}, nil
}

func (p *fivePointProbe) PreToolExec(context.Context, *apogee.ToolCall, apogee.LoopView) error {
	p.seen[apogee.HookPreToolExec]++
	return nil
}

func (p *fivePointProbe) PostToolResult(context.Context, apogee.ToolCall, *apogee.ToolResult, apogee.LoopView) error {
	p.seen[apogee.HookPostToolResult]++
	return nil
}

// ----------------------------------------------------------------------------
// Builders
// ----------------------------------------------------------------------------

// armProbe returns a fresh MechanismRegistry carrying only the five-point experimental probe (via the
// public AddExperimental) plus the probe itself. The catalogued Mechanisms are NOT built here — each
// arm enables them by ID through Config.EnableMechanisms, and the engine builds them INTO this same
// registry (its library store rooted at the arm's injected LibraryDir), so a catalogued+experimental
// combined arm co-fires from one registry. Each arm gets a fresh registry so its probe counters and
// library store never bleed into the other's.
func armProbe(t *testing.T) (*apogee.MechanismRegistry, *fivePointProbe) {
	t.Helper()
	reg := apogee.NewMechanismRegistry()
	probe := &fivePointProbe{seen: map[apogee.HookPoint]int{}}
	for _, at := range allHooks {
		if err := reg.AddExperimental(at, probe); err != nil {
			t.Fatalf("add experimental hook at %q: %v", at, err)
		}
	}
	return reg, probe
}

// paddedRegistry returns a real list_dir plus enough inert stubs to trip toolfilter's 30-tool
// activation threshold, so the request shapers have a menu large enough to narrow.
func paddedRegistry(t *testing.T, workspace string) *apogee.ToolRegistry {
	t.Helper()
	reg := apogee.NewToolRegistry()
	if err := reg.Register(tools.NewListDir(workspace)); err != nil {
		t.Fatalf("register list_dir: %v", err)
	}
	for i := 0; i < 30; i++ {
		if err := reg.Register(stubTool{name: fmt.Sprintf("stub_tool_%02d", i)}); err != nil {
			t.Fatalf("register stub tool: %v", err)
		}
	}
	return reg
}

// stateRoots is a triple of injected temp directories for one Agent.
type stateRoots struct{ workspace, library, sessions string }

func newRoots(t *testing.T) stateRoots {
	t.Helper()
	return stateRoots{workspace: t.TempDir(), library: t.TempDir(), sessions: t.TempDir()}
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

func firedEvents(events []apogee.Event) []apogee.MechanismFiredEvent {
	var out []apogee.MechanismFiredEvent
	for _, e := range events {
		if fe, ok := e.(apogee.MechanismFiredEvent); ok {
			out = append(out, fe)
		}
	}
	return out
}

// firedIDsAt returns, in emission order, the Mechanism IDs of the fires at hook point at.
func firedIDsAt(fires []apogee.MechanismFiredEvent, at apogee.HookPoint) []string {
	var ids []string
	for _, fe := range fires {
		if fe.Hook == at {
			ids = append(ids, string(fe.Mechanism))
		}
	}
	return ids
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

func orderedIndex(reg *apogee.MechanismRegistry, at apogee.HookPoint, want apogee.MechanismID) int {
	for i, m := range reg.Ordered(at) {
		if m.Descriptor().ID == want {
			return i
		}
	}
	return -1
}

// ----------------------------------------------------------------------------
// The proof
// ----------------------------------------------------------------------------

// TestBenchReadinessContract is the permanent regression proving apogee is drivable the way
// apogee-sim will drive it: two arms from one scripted responder against isolated roots,
// experimental hooks at all five points, snapshot/resume forks, deterministic mechanism order,
// the Bypass floor, and no state bleeding across arms or forks.
func TestBenchReadinessContract(t *testing.T) {
	srv := benchModel()
	defer srv.Close()

	// --- Arm A: mechanisms on --------------------------------------------------
	mechRoots := newRoots(t)
	mechSink := &recSink{}
	mechReg, mechProbe := armProbe(t)
	mechArm, err := apogee.New(apogee.Config{
		Endpoint:         srv.URL,
		Model:            benchModelName,
		Mode:             apogee.ModeAskBefore,
		Approver:         allowAll{},
		Events:           mechSink,
		Mechanisms:       mechReg,
		EnableMechanisms: enabledMechanisms,
		Tools:            paddedRegistry(t, mechRoots.workspace),
		WorkspaceDir:     mechRoots.workspace,
		LibraryDir:       mechRoots.library,
		SessionsDir:      mechRoots.sessions,
	})
	if err != nil {
		t.Fatalf("New (mechanisms-on arm): %v", err)
	}
	defer mechArm.Close()

	// --- Arm B: Bypass ---------------------------------------------------------
	bypassRoots := newRoots(t)
	bypassSink := &recSink{}
	bypassReg, bypassProbe := armProbe(t)
	bypassArm, err := apogee.New(apogee.Config{
		Endpoint:         srv.URL,
		Model:            benchModelName,
		Mode:             apogee.ModeAskBefore,
		Bypass:           true,
		Approver:         allowAll{},
		Events:           bypassSink,
		Mechanisms:       bypassReg,
		EnableMechanisms: enabledMechanisms,
		Tools:            paddedRegistry(t, bypassRoots.workspace),
		WorkspaceDir:     bypassRoots.workspace,
		LibraryDir:       bypassRoots.library,
		SessionsDir:      bypassRoots.sessions,
	})
	if err != nil {
		t.Fatalf("New (Bypass arm): %v", err)
	}
	defer bypassArm.Close()

	// Drive both arms through the same task to their quiescent boundaries.
	runToQuiescence(t, mechArm, apogee.UserInput{Text: complexPrompt})
	runToQuiescence(t, bypassArm, apogee.UserInput{Text: complexPrompt})

	// === Assertion 1: deterministic mechanism order visible in the fired stream ===
	// The enabled shapers actually ACT (they book fires); an inspect-only Mechanism does not.
	mechFires := firedEvents(mechSink.events)
	preIDs := firedIDsAt(mechFires, apogee.HookPreRequest)
	if len(preIDs) == 0 || len(preIDs)%3 != 0 {
		t.Fatalf("pre-request fired stream = %v, want repeating [toolfilter decompose experimental] triples", preIDs)
	}
	want := []string{"toolfilter", "decompose", "experimental"}
	for i, id := range preIDs {
		if id != want[i%3] {
			t.Errorf("pre-request fired[%d] = %q, want %q (deterministic order: shapers in Ordered() order, then the experimental hook)", i, id, want[i%3])
		}
	}
	// The observed order is the registry's deterministic dispatch order (toolfilter Before decompose).
	if ti, di := orderedIndex(mechReg, apogee.HookPreRequest, "toolfilter"), orderedIndex(mechReg, apogee.HookPreRequest, "decompose"); ti < 0 || di < 0 || ti >= di {
		t.Errorf("Ordered(pre-request) has toolfilter@%d, decompose@%d; want toolfilter strictly before decompose", ti, di)
	}

	// === Assertion 2: R4 — an inspect-only invocation books no fired event ===
	// truncate_history (short history) and library (observe is silent, inject is confidence-gated)
	// are dispatched every relevant pass but never intervene, so they never appear in the stream.
	for _, fe := range mechFires {
		if fe.Mechanism == "truncate_history" || fe.Mechanism == "library" {
			t.Errorf("inspect-only Mechanism %q booked a fire (R4: only acted fires are booked)", fe.Mechanism)
		}
	}

	// === Assertion 3: all five experimental hooks ran (both arms) ===
	for _, probe := range []*fivePointProbe{mechProbe, bypassProbe} {
		if len(probe.seen) != len(allHooks) {
			t.Errorf("experimental probe fired at %d/%d hook points: %v", len(probe.seen), len(allHooks), probe.seen)
		}
	}

	// === Assertion 4: the Bypass floor ===
	// No non-exempt (indeed no catalogued) Mechanism fired under Bypass, yet the experimental
	// hooks — the bench's own instruments — all ran (asserted above).
	for _, fe := range firedEvents(bypassSink.events) {
		if fe.Mechanism != "experimental" {
			t.Errorf("Bypass arm booked a catalogued fire %q at %q; Bypass runs only off-ramps + experimental hooks", fe.Mechanism, fe.Hook)
		}
	}

	// === Assertion 5: agent-driven writes stay inside the injected roots ===
	// The library observe half wrote its store under the mechanisms-on arm's LibraryDir; under
	// Bypass the Library is fully inert, so its LibraryDir stays empty.
	if _, err := os.Stat(filepath.Join(mechRoots.library, "library.json")); err != nil {
		t.Errorf("mechanisms-on arm did not persist its Library into the injected root: %v", err)
	}
	if entries, err := os.ReadDir(bypassRoots.library); err != nil || len(entries) != 0 {
		t.Errorf("Bypass arm's LibraryDir = %d entries (err %v), want 0 (Library inert under Bypass)", len(entries), err)
	}

	// Snapshot both arms, and prove a host-persisted session lands under the arm's own SessionsDir.
	snapMech, err := mechArm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot (mechanisms-on): %v", err)
	}
	snapBypass, err := bypassArm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot (Bypass): %v", err)
	}
	mechSessPath, err := session.NewStore(mechRoots.sessions).Save(snapMech)
	if err != nil {
		t.Fatalf("save mechanisms-on session: %v", err)
	}
	if filepath.Dir(mechSessPath) != mechRoots.sessions {
		t.Errorf("mechanisms-on session written to %q, want inside %q", mechSessPath, mechRoots.sessions)
	}
	if _, err := session.NewStore(bypassRoots.sessions).Save(snapBypass); err != nil {
		t.Fatalf("save Bypass session: %v", err)
	}

	// === Assertion 6: resumed forks diverge independently, in their own roots ===
	// Two forks resume from the SAME mechanisms-on snapshot and continue with different inputs;
	// the scripted model echoes each fork's own input, so their outputs diverge and never bleed.
	forkA := resumeFork(t, srv.URL, snapMech, "follow-up-A")
	forkB := resumeFork(t, srv.URL, snapMech, "follow-up-B")
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

	// The forks ran in their own roots and never touched the arms': the mechanisms-on arm's
	// Library still holds exactly its one store file, unperturbed by any fork.
	if entries, err := os.ReadDir(mechRoots.library); err != nil || len(entries) != 1 {
		t.Errorf("mechanisms-on LibraryDir = %d entries (err %v) after forks, want exactly 1 (no fork bled in)", len(entries), err)
	}
}

// resumeFork resumes a fork from snap into fresh isolated roots (no Mechanisms), continues it
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
		LibraryDir:   roots.library,
		SessionsDir:  roots.sessions,
	}, snap)
	if err != nil {
		t.Fatalf("Resume fork %q: %v", token, err)
	}
	defer fork.Close()

	runToQuiescence(t, fork, apogee.UserInput{Text: "wrap up now. " + closeMarker + " " + token})

	// The fork used only its own roots — its Library never wrote (no library Mechanism wired).
	if entries, err := os.ReadDir(roots.library); err != nil || len(entries) != 0 {
		t.Errorf("fork %q LibraryDir = %d entries (err %v), want 0", token, len(entries), err)
	}
	return messageText(sink.events)
}

// ----------------------------------------------------------------------------
// Construction acceptance through the public enable surface (no live model)
// ----------------------------------------------------------------------------

// hermeticArm constructs an Agent for a construction-only assertion, arming Mechanisms by ID through
// the public Config.EnableMechanisms into isolated temp roots with a discarding sink. New builds and
// validates the named Mechanisms WITHOUT dialing the Endpoint, so the returned error (or nil) reports
// exactly whether the arm is admissible — the fail-loud gate apogee-sim hits when it mis-plans an arm.
func hermeticArm(t *testing.T, enable []apogee.MechanismID) (*apogee.Agent, error) {
	t.Helper()
	return apogee.New(apogee.Config{
		Endpoint:         "http://localhost:11434",
		Model:            benchModelName,
		Mode:             apogee.ModeAskBefore,
		Approver:         allowAll{},
		Events:           &recSink{},
		EnableMechanisms: enable,
		WorkspaceDir:     t.TempDir(),
		LibraryDir:       t.TempDir(),
		SessionsDir:      t.TempDir(),
	})
}

// TestBenchReadinessConstructionRefusals proves the campaign's fail-loud arms refuse construction
// through the PUBLIC surface with a matchable sentinel: a half-armed Requires stack
// (guided_decomposition without its required tool_result_cap peer, ADR 0014) fails
// apogee.ErrMissingRequirement, and a bogus catalogue ID fails apogee.ErrUnknownMechanism — the same
// startup gate the bench hits when it mis-plans an arm, asserted only through errors.Is on the root
// sentinels.
func TestBenchReadinessConstructionRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		enable  []apogee.MechanismID
		wantErr error
	}{
		{
			name:    "half-armed Requires stack",
			enable:  []apogee.MechanismID{"guided_decomposition"},
			wantErr: apogee.ErrMissingRequirement,
		},
		{
			name:    "unknown catalogue ID",
			enable:  []apogee.MechanismID{"no_such_mechanism"},
			wantErr: apogee.ErrUnknownMechanism,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ag, err := hermeticArm(t, tc.enable)
			if ag != nil {
				ag.Close()
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New(EnableMechanisms=%v) error = %v, want errors.Is(err, %v)", tc.enable, err, tc.wantErr)
			}
		})
	}
}

// compatibleBaseStack computes a maximal enable set from the PUBLIC catalogue query the way the bench
// plans a full-stack arm: walk apogee.CataloguedMechanisms() (already sorted) and greedily include
// each ID unless it is IncompatibleWith one already chosen, then drop any whose Requires peers did not
// survive — so the returned set carries no incompatible pair and no half-armed Requires stack, and New
// accepts it. It is the base the leave-one-out arms subtract from, computed with nothing but the
// public descriptor metadata.
func compatibleBaseStack() []apogee.MechanismID {
	catalogue := apogee.CataloguedMechanisms()
	descByID := make(map[apogee.MechanismID]apogee.MechanismDescriptor, len(catalogue))
	for _, d := range catalogue {
		descByID[d.ID] = d
	}

	var base []apogee.MechanismID
	for _, d := range catalogue {
		conflict := false
		for _, sel := range base {
			if slices.Contains(d.IncompatibleWith, sel) || slices.Contains(descByID[sel].IncompatibleWith, d.ID) {
				conflict = true
				break
			}
		}
		if !conflict {
			base = append(base, d.ID)
		}
	}

	// Drop any Mechanism whose required peers were excluded above, to a fixpoint (one drop can orphan
	// another). Nothing here re-adds, so the loop terminates.
	for {
		kept := make([]apogee.MechanismID, 0, len(base))
		for _, id := range base {
			ok := true
			for _, req := range descByID[id].Requires {
				if !slices.Contains(base, req) {
					ok = false
					break
				}
			}
			if ok {
				kept = append(kept, id)
			}
		}
		if len(kept) == len(base) {
			return kept
		}
		base = kept
	}
}

// TestBenchReadinessLeaveOneOutArms proves the bench's leave-one-out planning idiom works entirely
// over the public surface: a full-stack arm computed from apogee.CataloguedMechanisms() constructs,
// and so does every arm that leaves one member out (dropping any stack that Requires it — the Requires
// traversal, so no half-armed stack ever reaches New). These are the arms the campaign compares to
// measure each Mechanism's marginal contribution; that every one is admissible is the contract.
func TestBenchReadinessLeaveOneOutArms(t *testing.T) {
	t.Parallel()
	base := compatibleBaseStack()
	if len(base) == 0 {
		t.Fatal("compatibleBaseStack() is empty; the public catalogue query returned nothing")
	}

	construct := func(t *testing.T, enable []apogee.MechanismID) {
		t.Helper()
		ag, err := hermeticArm(t, enable)
		if err != nil {
			t.Fatalf("New(EnableMechanisms=%v): %v", enable, err)
		}
		ag.Close()
	}

	t.Run("full stack", func(t *testing.T) {
		t.Parallel()
		construct(t, base)
	})

	for _, leaveOut := range base {
		leaveOut := leaveOut
		t.Run("without "+string(leaveOut), func(t *testing.T) {
			t.Parallel()
			arm := make([]apogee.MechanismID, 0, len(base))
			for _, id := range base {
				if id == leaveOut || slices.Contains(descriptorFor(id).Requires, leaveOut) {
					continue // the left-out Mechanism, and any stack that Requires it
				}
				arm = append(arm, id)
			}
			construct(t, arm)
		})
	}
}

// descriptorFor returns the public descriptor of a catalogued Mechanism ID, or a zero descriptor if
// the ID is not catalogued (the leave-one-out arms only ever pass IDs drawn from the catalogue query).
func descriptorFor(id apogee.MechanismID) apogee.MechanismDescriptor {
	for _, d := range apogee.CataloguedMechanisms() {
		if d.ID == id {
			return d
		}
	}
	return apogee.MechanismDescriptor{}
}
