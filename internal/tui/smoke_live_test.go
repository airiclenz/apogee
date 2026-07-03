package tui

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Parser-seam follow-through, item 1 — the live smoke test (manual, opt-in)
// ----------------------------------------------------------------------------
//
// docs/plans/parser-seam-follow-through-plan.md §1 asks for ONE end-to-end run of the parser
// seam against a REAL small model before tagging, because every seam-wiring test drives fake
// responders. This harness is that run. It drives the real Agent through the real provider
// client against a live OpenAI-compatible server, but — unlike TestE2ELiveModel, which exercises
// only the native path — it sets a non-zero ModelProfile so the two seam axes are exercised live:
//
//	Check A — the thinking axis (delimited <think>…</think>): a reasoning prompt under a
//	          delimited profile. Confirms (1) no <think> markup in the committed MessageEvent,
//	          (2) the reasoning survives as reasoning_content in the snapshotted session state,
//	          (3) the live TokenEvent stream never leaks the mid-channel markup, and a native
//	          control run (zero profile) streams the same bytes the server sent.
//
//	Check B — the tool-call axis (markdown-fenced): a write_file request under a markdown-fenced
//	          profile. The prompt-seam wiring injects the fenced tool menu + emission
//	          instructions automatically (no manual workaround), so this confirms the fenced
//	          block parses, dispatches through the approval gate, the markup is stripped from the
//	          committed text, and the follow-up turn (tool result in context, the text-parsed
//	          call echoed native-shaped in history) does not derail the model — the recorded D6
//	          watch.
//
// It is opt-in, gated on APOGEE_LIVE_ENDPOINT exactly like TestE2ELiveModel, so `make check`
// never depends on a running model. Run it against the <think>-style reference model with:
//
//	APOGEE_LIVE_ENDPOINT=http://127.0.0.1:1111 go test -race -count=1 \
//	    -run TestSmokeLiveProfileSeam -v ./internal/tui/
//
// -count=1 is load-bearing for the same reason live_test.go documents: the live server's loaded
// model is not a Go-visible input, so caching would replay a stale PASS across a model swap.
func TestSmokeLiveProfileSeam(t *testing.T) {
	endpoint := os.Getenv("APOGEE_LIVE_ENDPOINT")
	if endpoint == "" {
		t.Skip("set APOGEE_LIVE_ENDPOINT (and optionally APOGEE_LIVE_MODEL) to run the live smoke test")
	}

	model := os.Getenv("APOGEE_LIVE_MODEL")
	if model == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := provider.NewClient(endpoint, "").Discover(ctx)
		if err != nil {
			t.Fatalf("discover model at %s: %v", endpoint, err)
		}
		model = info.ActiveModel
	}
	t.Logf("live smoke test: endpoint=%s model=%s", endpoint, model)

	// The delimited profile's markers default to <think>/</think> (the reference the repo docs
	// name), but are env-overridable so the same harness can be pointed at whatever inline
	// delimiters a given model actually emits — e.g. the live smoke test discovered that
	// gemma-4-e4b-it-qat emits "<|channel>…<channel|>", not <think>, so exercising the strip
	// path against it needs those markers (docs/plans/parser-seam-follow-through-plan.md §1).
	start := envOr("APOGEE_SMOKE_THINK_START", "<think>")
	end := envOr("APOGEE_SMOKE_THINK_END", "</think>")
	delimited := domain.ModelProfile{
		Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: start, End: end},
	}
	fenced := domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}

	t.Run("CheckA_thinking_delimited", func(t *testing.T) {
		smokeCheckAThinking(t, endpoint, model, delimited, start, end)
	})
	t.Run("CheckA_native_control", func(t *testing.T) {
		smokeCheckANativeControl(t, endpoint, model)
	})
	t.Run("CheckB_toolcall_markdown_fenced", func(t *testing.T) {
		smokeCheckBToolCall(t, endpoint, model, fenced)
	})
}

// smokeCheckAThinking runs the thinking-axis check: a reasoning prompt under the delimited
// profile, then asserts the seam stripped the inline channel everywhere it should and preserved
// it where it should. start/end are the profile's configured delimiters (the assertions key off
// them, so the check is honest whichever markers the model under test actually emits).
func smokeCheckAThinking(t *testing.T, endpoint, model string, profile domain.ModelProfile, start, end string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sink := &smokeSink{}
	eng := newSmokeEngine(t, endpoint, model, t.TempDir(), sink, &smokeApprover{}, profile)

	const prompt = "What is 17 multiplied by 24? Think step by step, then give the final number."
	if err := eng.Submit(domain.UserInput{Text: prompt}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := eng.Run(ctx); err != nil {
		t.Fatalf("live exchange returned a loop error: %v", err)
	}

	finalMsg := sink.lastMessage()
	liveStream := sink.joinedTokens()
	state := smokeSnapshot(t, eng)

	t.Logf("delimiters: start=%q end=%q", start, end)
	t.Logf("final MessageEvent (%d bytes):\n%s", len(finalMsg), finalMsg)
	t.Logf("live TokenEvent stream (%d bytes):\n%s", len(liveStream), liveStream)
	t.Logf("snapshot state (%d bytes): reasoning_content present=%v", len(state), strings.Contains(state, "reasoning_content"))
	if errs := sink.errorStrings(); len(errs) > 0 {
		t.Logf("ErrorEvents observed: %v", errs)
	}

	// (1) No delimiter markup in the committed assistant text (the span was stripped).
	if strings.Contains(finalMsg, start) || strings.Contains(finalMsg, end) {
		t.Errorf("committed MessageEvent still carries the %q/%q markup — the inline span was not stripped:\n%s", start, end, finalMsg)
	}
	// (2) The reasoning survives as reasoning_content in the snapshotted session state.
	if !strings.Contains(state, "reasoning_content") {
		t.Errorf("no reasoning_content in the snapshotted session state — the inline channel was not preserved")
	}
	// (3) The live token stream never leaked the closing marker. A start token split across two
	//     deltas may briefly reveal a partial prefix (accepted parity — emitVisibleDelta's
	//     recorded edge), so the load-bearing leak check is the closing marker, which only
	//     appears once a whole span has streamed unheld.
	if strings.Contains(liveStream, end) {
		t.Errorf("live TokenEvent stream leaked mid-channel markup (%q reached the live stream):\n%s", end, liveStream)
	}
	if liveStream == "" {
		t.Errorf("no TokenEvents were emitted — the visible answer never streamed")
	}
}

// envOr returns the environment variable value for key, or def when it is unset/empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// smokeCheckANativeControl runs the same reasoning prompt with a ZERO profile (native, no inline
// thinking) and records what the server sends untouched — the byte-identical anchor the profile
// run is contrasted against. It asserts only that the native path runs clean; whether <think>
// survives in the visible text depends on the server's own reasoning-format handling, which is
// precisely what the smoke test is here to observe, so it is logged, not asserted.
func smokeCheckANativeControl(t *testing.T, endpoint, model string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sink := &smokeSink{}
	eng := newSmokeEngine(t, endpoint, model, t.TempDir(), sink, &smokeApprover{}, domain.ModelProfile{})

	const prompt = "What is 17 multiplied by 24? Think step by step, then give the final number."
	if err := eng.Submit(domain.UserInput{Text: prompt}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := eng.Run(ctx); err != nil {
		t.Fatalf("native control returned a loop error: %v", err)
	}

	finalMsg := sink.lastMessage()
	liveStream := sink.joinedTokens()
	state := smokeSnapshot(t, eng)

	t.Logf("NATIVE final MessageEvent (%d bytes):\n%s", len(finalMsg), finalMsg)
	t.Logf("NATIVE live TokenEvent stream (%d bytes):\n%s", len(liveStream), liveStream)
	t.Logf("NATIVE snapshot: reasoning_content present=%v, <think> in visible=%v",
		strings.Contains(state, "reasoning_content"),
		strings.Contains(finalMsg, "<think>") || strings.Contains(finalMsg, "</think>"))

	if liveStream == "" && finalMsg == "" {
		t.Errorf("native control produced no visible output at all")
	}
}

// smokeCheckBToolCall runs the tool-call-axis check: a write_file request under the
// markdown-fenced profile. The prompt-seam wiring injects the fenced tool menu + emission
// instructions, so a successful run proves the fenced block parses, dispatches through the
// approval gate, and the follow-up turn does not derail (the D6 watch).
func smokeCheckBToolCall(t *testing.T, endpoint, model string, profile domain.ModelProfile) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	workspace := t.TempDir()
	sink := &smokeSink{}
	approver := &smokeApprover{}
	eng := newSmokeEngine(t, endpoint, model, workspace, sink, approver, profile)

	const prompt = "Use the write_file tool to create a file named greeting.txt containing exactly: Hello, Apogee!"
	if err := eng.Submit(domain.UserInput{Text: prompt}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := eng.Run(ctx); err != nil {
		t.Fatalf("live fenced exchange returned a loop error (the D6 watch — a derailed follow-up turn shows here): %v", err)
	}

	calls := sink.toolCalls()
	finalMsg := sink.lastMessage()

	t.Logf("tool calls parsed+dispatched: %d", len(calls))
	for _, c := range calls {
		t.Logf("  → %s(%s)", c.Tool, string(c.Arguments))
	}
	t.Logf("approvals resolved: %d", approver.count())
	t.Logf("final MessageEvent (%d bytes):\n%s", len(finalMsg), finalMsg)
	if errs := sink.errorStrings(); len(errs) > 0 {
		t.Logf("ErrorEvents observed: %v", errs)
	}
	if entries, err := os.ReadDir(workspace); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Logf("workspace files: %v", names)
	}

	// The fenced block must have parsed into at least one dispatched tool call.
	if len(calls) == 0 {
		t.Errorf("no tool call was parsed from the fenced output — the fenced emission instructions did not land, or the model did not emit a fenced block")
	}
	// The markup must be stripped from the committed assistant text.
	if strings.Contains(finalMsg, "```") {
		t.Errorf("committed MessageEvent still carries the fenced code block markup:\n%s", finalMsg)
	}
}

// ----------------------------------------------------------------------------
// Smoke-test harness helpers
// ----------------------------------------------------------------------------

// newSmokeEngine builds a real ask-before Agent bound to endpoint/model with the given profile
// and an event sink — newE2EEngine's sibling, adding the Profile the seam under test needs. It
// is ask-before so Check B's write gates through the (auto-allowing) smokeApprover, the headless
// stand-in for a human pressing "a".
func newSmokeEngine(t *testing.T, endpoint, model, workspace string, sink domain.EventSink, approver domain.Approver, profile domain.ModelProfile) *agent.Agent {
	t.Helper()
	eng, err := agent.New(domain.Config{
		Endpoint:     endpoint,
		Model:        model,
		Mode:         domain.ModeAskBefore,
		Events:       sink,
		Approver:     approver,
		Tools:        tools.NewDefaultRegistry(workspace),
		WorkspaceDir: workspace,
		Profile:      profile,
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

// smokeSnapshot captures the Agent's serialized session state as a string for content asserts
// (reasoning_content presence). Snapshot is valid at the quiescent boundary Run returns at.
func smokeSnapshot(t *testing.T, eng *agent.Agent) string {
	t.Helper()
	sess, err := eng.Snapshot()
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return string(sess.State)
}

// smokeSink records the loop's event stream for post-run inspection. The Agent emits
// synchronously from the single drive goroutine (Submit+Run in the test goroutine), but the mutex
// keeps -race honest and lets the accessors read safely.
type smokeSink struct {
	mu       sync.Mutex
	tokens   []string
	messages []string
	calls    []domain.ToolCall
	errs     []string
}

func (s *smokeSink) Emit(e domain.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev := e.(type) {
	case domain.TokenEvent:
		s.tokens = append(s.tokens, ev.Text)
	case domain.MessageEvent:
		s.messages = append(s.messages, ev.Text)
	case domain.ToolCallEvent:
		s.calls = append(s.calls, ev.Call)
	case domain.ErrorEvent:
		s.errs = append(s.errs, ev.Source+": "+ev.Err)
	}
}

func (s *smokeSink) joinedTokens() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.tokens, "")
}

func (s *smokeSink) lastMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return ""
	}
	return s.messages[len(s.messages)-1]
}

func (s *smokeSink) toolCalls() []domain.ToolCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ToolCall(nil), s.calls...)
}

func (s *smokeSink) errorStrings() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.errs...)
}

// smokeApprover auto-allows every gated call (the headless stand-in for a human pressing "a")
// and counts how many it saw.
type smokeApprover struct {
	mu sync.Mutex
	n  int
}

func (a *smokeApprover) Approve(context.Context, domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	a.mu.Lock()
	a.n++
	a.mu.Unlock()
	return domain.ApprovalAllow, nil
}

func (a *smokeApprover) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}
