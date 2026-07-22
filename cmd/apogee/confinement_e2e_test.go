package main

// End-to-end acceptance for the auto-mode confinement degradation loop (auto-confinement-
// degradation plan, item 9): the whole user journey the issue described, on a SIMULATED
// incapable host so it runs identically on a machine that can fence and one that cannot.
//
// The journey, in the order a user lives it:
//
//  1. Auto on a backend that cannot fence the filesystem gates every terminal command
//     through Approval — correct per ADR 0012 ("confine if you can, gate if you can't") —
//     and the startup notice that finally SAYS so is produced.
//  2. `/confine off` — the user's own sanctioned decision — makes the next terminal call
//     run with no Approval prompt, on the live Agent, with no restart.
//  3. `/confine off --save` records THIS machine in ~/.apogee/config.yaml.
//  4. A fresh resolution over that config resolves unconfined on the same host id and
//     CONFINED (notice back) on any other. That last assertion is the whole reason the
//     acknowledgement is host-scoped rather than a global flag flip.
//
// The incapable backend is platform.NewDenyConfiner() — FSWrite=false, the exact shape of a
// container where landlock_create_ruleset returns ENOSYS. The `/confine` steps are driven
// through the two seams the TUI command actually calls, tui.Engine.SetConfineToWorkspace and
// tui.Options.SaveHostAcknowledgement, because internal/tui's Model is unexported and this is
// package main; the command's own parsing and routing are pinned by the tests in
// internal/tui (plan items 6 and 7).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/tui"
)

// The scripted conversation's canonical strings, shared by the fake model and the assertions.
// The command's output is what proves the subprocess really ran rather than being refused.
const (
	e2eEcho            = "apogee-confinement-e2e"
	e2eTerminalCommand = "echo " + e2eEcho
	e2eNarration       = "Running one command.\n"
	e2eFinalMessage    = "Done."
)

// TestE2EAutoDegradationJourneyOnAnIncapableHost is the item-9 acceptance: one pass through
// the whole loop, wired the way the binary wires it. Every phase depends on the previous one,
// so it is a single linear test rather than subtests — a broken phase invalidates the rest.
func TestE2EAutoDegradationJourneyOnAnIncapableHost(t *testing.T) {
	// Deliberately NOT parallel: phase 3 drives the real composition root, whose startup
	// notices go to the process-global os.Stderr, and captureStderr swaps it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := scriptedTerminalModel()
	defer srv.Close()

	workspace := t.TempDir()
	configHome := t.TempDir()
	configPath := filepath.Join(configHome, "config.yaml")

	// The simulated incapable host: a backend that reports no filesystem confinement, exactly
	// as landlock does in a container (ENOSYS). Using it — rather than the host's real backend —
	// is what makes the degraded path reproducible on any test machine.
	confiner := platform.NewDenyConfiner()
	if confiner.Capabilities().FSWrite {
		t.Fatal("the deny backend reports FSWrite=true; the incapable host cannot be simulated")
	}

	// ------------------------------------------------------------------
	// 1. Auto on an incapable backend: the terminal call gates, and the notice is produced.
	// ------------------------------------------------------------------

	notice := probe.DegradedNotice(probe.BackendName(confiner), confiner.Capabilities(), modeAuto, true)
	if notice == "" {
		t.Fatal("no degradation notice for auto + confine-to-workspace on a backend that cannot fence; " +
			"the silence this whole plan exists to fix is back")
	}
	for _, want := range []string{"deny", "approval", "/confine off", "/confine off --save"} {
		if !strings.Contains(notice, want) {
			t.Errorf("degradation notice does not mention %q:\n%s", want, notice)
		}
	}

	approver := &e2eApprover{}
	sink := &e2eSink{}
	engine := newIncapableHostAgent(t, srv.URL, workspace, confiner, approver, sink)

	runE2EExchange(t, ctx, engine, "run a command")

	if got := approver.requests(); len(got) != 1 || got[0].Tool != "terminal" {
		t.Fatalf("approval requests while confined = %+v; want exactly one for the terminal call "+
			"(a host that cannot fence must gate the subprocess surface)", got)
	}
	assertTerminalRan(t, sink, "the gated-then-approved call")

	// ------------------------------------------------------------------
	// 2. `/confine off`: the next terminal call runs with no Approval prompt.
	// ------------------------------------------------------------------

	// The exact seam the /confine command drives — the narrow Engine view, not the concrete
	// Agent — so a reverted interface method or a dispatch that re-read the construction
	// Config would fail here rather than in a unit test.
	engine.SetConfineToWorkspace(false)
	if engine.ConfineToWorkspace() {
		t.Fatal("Engine.ConfineToWorkspace() still reports confined after /confine off")
	}

	approver.reset()
	sink.reset()
	runE2EExchange(t, ctx, engine, "run it again")

	if got := approver.requests(); len(got) != 0 {
		t.Fatalf("approval requests after /confine off = %+v; want none — the user took the "+
			"blast radius on deliberately, so the next call must run unfenced and ungated", got)
	}
	assertTerminalRan(t, sink, "the ungated call after /confine off")

	// ------------------------------------------------------------------
	// 3. `/confine off --save`: this host lands in the config file.
	// ------------------------------------------------------------------

	// The composition root, driven for real: the recorded tui.Options carry the confinement
	// facts and the writer seam the /confine command uses, so this covers wire.go's wiring as
	// well as the writer itself.
	rec := &recordingLauncher{}
	launched := options{
		endpoint:           srv.URL,
		model:              "fake",
		mode:               "auto",
		workspace:          workspace,
		configDir:          configHome,
		confineToWorkspace: true,
	}
	// Its startup notices are captured rather than asserted: which of them fires depends on
	// whether the REAL host can fence, and TestRunRootConfinementStartupNotices already owns
	// that assertion. The one host-independent fact is checked below — the two Auto branches
	// are mirrors, so a launch that asked for confinement must not print the unconfined warning.
	var runErr error
	stderr := captureStderr(t, func() { runErr = runRoot(ctx, launched, rec.launch) })
	if runErr != nil {
		t.Fatalf("runRoot: %v", runErr)
	}
	if strings.Contains(stderr, "running UNCONFINED") {
		t.Errorf("the unconfined-Auto warning fired for a launch with confine-to-workspace true:\n%s", stderr)
	}
	hostID := rec.opts.Confinement.HostID
	if hostID == "" {
		t.Fatal("tui.Options.Confinement.HostID is empty; /confine status could never name the host " +
			"an acknowledgement is recorded against")
	}
	if rec.opts.SaveHostAcknowledgement == nil {
		t.Fatal("tui.Options.SaveHostAcknowledgement is nil; `/confine off --save` would write nothing")
	}

	written, err := rec.opts.SaveHostAcknowledgement()
	if err != nil {
		t.Fatalf("SaveHostAcknowledgement: %v", err)
	}
	if written != configPath {
		t.Errorf("SaveHostAcknowledgement wrote %q; want the resolved config %q", written, configPath)
	}
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read the saved config: %v", err)
	}
	if !strings.Contains(string(saved), hostID) {
		t.Fatalf("the saved config does not name this host (%q):\n%s", hostID, saved)
	}

	// ------------------------------------------------------------------
	// 4. A fresh resolution over that config: unconfined here, confined anywhere else.
	// ------------------------------------------------------------------

	file, err := loadFileConfig(configPath, os.ReadFile)
	if err != nil {
		t.Fatalf("re-read the saved config: %v", err)
	}

	here, notices := resolveSettings(file, layer{}, layer{}, hostID)
	if here.confineToWorkspace {
		t.Errorf("a fresh resolution on the acknowledged host %q resolves confined; want unconfined — "+
			"the saved acknowledgement did not survive the round trip", hostID)
	}
	if len(notices) != 0 {
		t.Errorf("resolution notices = %v; want none (the writer produced a well-formed entry)", notices)
	}
	if got := probe.DegradedNotice(probe.BackendName(confiner), confiner.Capabilities(), modeAuto, here.confineToWorkspace); got != "" {
		t.Errorf("the degradation notice still fires on the acknowledged host:\n%s", got)
	}

	// The load-bearing assertion for the host-scoped design: the very same config file, read on
	// a DIFFERENT machine, confines again and says why. A global flag flip could not do this.
	elsewhere, _ := resolveSettings(file, layer{}, layer{}, hostID+"-some-other-machine")
	if !elsewhere.confineToWorkspace {
		t.Error("the acknowledgement written on this host also unconfines a different host id; " +
			"it must not travel with the config file")
	}
	if got := probe.DegradedNotice(probe.BackendName(confiner), confiner.Capabilities(), modeAuto, elsewhere.confineToWorkspace); got == "" {
		t.Error("no degradation notice on the unacknowledged host; the other machine would be silently gated again")
	}

	// The production resolution path (which reads platform.HostID() itself, not an injected id)
	// agrees: on this machine, the saved acknowledgement is what startup resolves.
	resolved := options{configDir: configHome}
	changed := func(name string) bool { return name == "config" }
	getenv := func(string) string { return "" }
	if err := applyConfig(&resolved, changed, getenv, os.ReadFile, func(string) {}); err != nil {
		t.Fatalf("applyConfig over the saved config: %v", err)
	}
	if resolved.confineToWorkspace {
		t.Error("applyConfig resolves confined on the acknowledged host; startup would keep gating " +
			"every terminal command despite the saved acknowledgement")
	}
}

// ----------------------------------------------------------------------------
// The engine under test
// ----------------------------------------------------------------------------

// newIncapableHostAgent builds the Agent the journey drives: Auto, confinement asked for, the
// real default tool set (so `terminal` is the real SubprocessTool the ladder classifies), and a
// Confiner that cannot fence. It is returned as tui.Engine — the narrow seam the /confine
// command drives — so the toggle in phase 2 goes through exactly the surface the TUI has.
func newIncapableHostAgent(t *testing.T, endpoint, workspace string, confiner apogee.Confiner, approver apogee.Approver, sink apogee.EventSink) tui.Engine {
	t.Helper()
	agent, err := apogee.New(apogee.Config{
		Endpoint:           endpoint,
		Model:              "fake",
		Mode:               modeAuto,
		Events:             sink,
		Approver:           approver,
		Tools:              tools.NewDefaultRegistry(workspace),
		WorkspaceDir:       workspace,
		Confiner:           confiner,
		ConfineToWorkspace: true,
	})
	if err != nil {
		t.Fatalf("apogee.New on the incapable host: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent
}

// runE2EExchange drives one Exchange to its quiescent boundary — the canonical Submit-then-Run
// loop a host uses — and fails the test unless the Exchange completed normally.
func runE2EExchange(t *testing.T, ctx context.Context, engine tui.Engine, text string) {
	t.Helper()
	if err := engine.Submit(apogee.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit(%q): %v", text, err)
	}
	for {
		res, err := engine.Step(ctx)
		if err != nil {
			t.Fatalf("Step during %q: %v", text, err)
		}
		if res.Status == apogee.StatusTurnComplete {
			continue
		}
		if res.Status != apogee.StatusExchangeComplete {
			t.Fatalf("exchange %q ended with status %q; want %q", text, res.Status, apogee.StatusExchangeComplete)
		}
		return
	}
}

// assertTerminalRan proves the subprocess actually executed rather than being refused or
// silently dropped: exactly one terminal call, one non-error result, and the command's own
// output in it.
func assertTerminalRan(t *testing.T, sink *e2eSink, phase string) {
	t.Helper()
	calls, results := sink.toolActivity()
	if len(calls) != 1 || calls[0] != "terminal" {
		t.Fatalf("%s: tool calls = %v; want exactly one terminal call", phase, calls)
	}
	if len(results) != 1 {
		t.Fatalf("%s: tool results = %d; want exactly one", phase, len(results))
	}
	if results[0].IsError {
		t.Fatalf("%s: the terminal result is an error: %s", phase, results[0].Content)
	}
	if !strings.Contains(results[0].Content, e2eEcho) {
		t.Fatalf("%s: the terminal result does not carry the command's output %q:\n%s", phase, e2eEcho, results[0].Content)
	}
}

// ----------------------------------------------------------------------------
// The recording host delegates
// ----------------------------------------------------------------------------

// e2eApprover is the human gate, standing in for the TUI's Approval prompt: it records every
// request and always allows, so an approval that should NOT have been asked for is visible as a
// recorded request rather than as a hang. The mutex is defensive — the loop consults it inside
// Step, on the driving goroutine — and keeps the test honest under -race.
type e2eApprover struct {
	mu   sync.Mutex
	seen []apogee.ApprovalRequest
}

func (a *e2eApprover) Approve(_ context.Context, req apogee.ApprovalRequest) (apogee.ApprovalDecision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seen = append(a.seen, req)
	// Allow, never allow-for-session: a cached grant would make the next phase's
	// "no approval was asked for" assertion pass for the wrong reason.
	return apogee.ApprovalAllow, nil
}

// requests returns the approval requests recorded since the last reset.
func (a *e2eApprover) requests() []apogee.ApprovalRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]apogee.ApprovalRequest(nil), a.seen...)
}

// reset drops the recorded requests so the next phase counts only its own.
func (a *e2eApprover) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seen = nil
}

// e2eSink is the host's EventSink, kept to the two variants this journey reads: which tools the
// loop dispatched and what they returned.
type e2eSink struct {
	mu      sync.Mutex
	calls   []string
	results []apogee.ToolResult
}

func (s *e2eSink) Emit(ev apogee.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e := ev.(type) {
	case apogee.ToolCallEvent:
		s.calls = append(s.calls, e.Call.Tool)
	case apogee.ToolResultEvent:
		s.results = append(s.results, e.Result)
	}
}

// toolActivity returns the tool names dispatched and the results they produced since the last reset.
func (s *e2eSink) toolActivity() ([]string, []apogee.ToolResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...), append([]apogee.ToolResult(nil), s.results...)
}

// reset drops the recorded events so the next phase observes only its own.
func (s *e2eSink) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls, s.results = nil, nil
}

// ----------------------------------------------------------------------------
// A scripted OpenAI-compatible streaming model
// ----------------------------------------------------------------------------

// scriptedTerminalModel returns an httptest server speaking the SSE wire the provider dials. It
// is stateless and decides each reply from the request's own history, as a real model does: a
// request that does not yet end in a tool result asks for one `terminal` call; one that does
// commits the final message that ends the Exchange. Two Exchanges therefore run the same shape
// twice, which is what lets the journey compare a gated call with an ungated one.
func scriptedTerminalModel() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if lastMessageIsToolResult(r) {
			e2eWriteFinal(w, e2eFinalMessage)
			return
		}
		e2eWriteToolCall(w, e2eNarration, "terminal", fmt.Sprintf(`{"command":%q}`, e2eTerminalCommand))
	}))
}

// lastMessageIsToolResult reports whether the request's final message carries the tool role —
// the signal that the previous Turn ran the tool and the model should now close the Exchange.
// Only the roles matter here, so the rest of the OpenAI request shape is ignored.
func lastMessageIsToolResult(r *http.Request) bool {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	if len(req.Messages) == 0 {
		return false
	}
	return req.Messages[len(req.Messages)-1].Role == "tool"
}

// e2eWriteToolCall streams a narration chunk, one native tool call, a tool_calls finish and the
// SSE terminator — the wire shape of a Turn that asks for a tool.
func e2eWriteToolCall(w http.ResponseWriter, narration, name, args string) {
	e2eSSE(w, e2eChunk{Choices: []e2eChoice{{Delta: e2eDelta{Content: narration}}}})
	e2eSSE(w, e2eChunk{Choices: []e2eChoice{{Delta: e2eDelta{ToolCalls: []e2eToolCall{{
		ID: "call_1", Type: "function", Function: e2eFunc{Name: name, Arguments: args},
	}}}}}})
	e2eSSE(w, e2eChunk{Choices: []e2eChoice{{FinishReason: "tool_calls"}}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// e2eWriteFinal streams one content chunk, a stop finish and the terminator — the wire shape of
// a final no-tool Turn that ends the Exchange.
func e2eWriteFinal(w http.ResponseWriter, text string) {
	e2eSSE(w, e2eChunk{Choices: []e2eChoice{{Delta: e2eDelta{Content: text}}}})
	e2eSSE(w, e2eChunk{Choices: []e2eChoice{{FinishReason: "stop"}}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// e2eSSE writes v as one SSE data event. Writes are best-effort: the handler runs on the server
// goroutine, where a test-failure call is not permitted, and the fixed structs never fail to marshal.
func e2eSSE(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

// The on-the-wire SSE chunk shape the provider parses — only the fields this scripted model sets.
type e2eChunk struct {
	Choices []e2eChoice `json:"choices"`
}

type e2eChoice struct {
	Delta        e2eDelta `json:"delta"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

type e2eDelta struct {
	Content   string        `json:"content,omitempty"`
	ToolCalls []e2eToolCall `json:"tool_calls,omitempty"`
}

type e2eToolCall struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Function e2eFunc `json:"function"`
}

type e2eFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
