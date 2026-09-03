package agent

// Loop-level proofs for the post-response Floor guards (ADR 0071): tool-call repair and the
// tool-loop breaker are engine behaviour now, so they fire with no `mechanisms:` block at all and
// with Bypass ON, and each is taken away only by its own domain.FloorConfig opt-out.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// hasGuardFire reports whether a FloorGuardEvent for guard with action was emitted.
func hasGuardFire(events []domain.Event, guard, action string) bool {
	for _, e := range events {
		if ge, ok := e.(domain.FloorGuardEvent); ok && ge.Guard == guard && ge.Action == action {
			return true
		}
	}
	return false
}

// guardFireCountFor counts the FloorGuardEvents attributed to guard, whatever the action.
func guardFireCountFor(events []domain.Event, guard string) int {
	n := 0
	for _, e := range events {
		if ge, ok := e.(domain.FloorGuardEvent); ok && ge.Guard == guard {
			n++
		}
	}
	return n
}

// A call to a tool the model was never shown is repaired and the Turn re-streams — with NO
// catalogued Mechanism enabled and Bypass ON, which is exactly the posture the promotion is for:
// the floor is what every model runs with, not a nudge a block switches on. The retried request
// carries the superseded call and the correction, the corrected call is the one that dispatches,
// and the firing is booked as a FloorGuardEvent naming the CONFIG KEY.
func TestFloorGuard_ToolCallRepairRetriesUnderBypass(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	lookup := fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "42"}
	cfg := configWithTools(sink, lookup)
	cfg.Bypass = true
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "frobnicate", `{}`),  // not in the menu — the guard repairs
		toolCallScript("c2", "lookup", `{"q":1}`), // the corrected call — dispatches
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "look it up")

	if len(responder.got) != 3 {
		t.Fatalf("provider was called %d times, want 3 (draft, guard retry, final)", len(responder.got))
	}
	second := responder.got[1].Messages
	ai := wireMessageIndex(second, "assistant", "")
	if ai < 0 {
		t.Fatalf("retried request carries no superseded assistant message: %+v", second)
	}
	if tc := second[ai].ToolCalls; len(tc) != 1 || tc[0].ID != "c1" {
		t.Errorf("superseded assistant tool calls = %+v, want the draft's c1 call", tc)
	}
	ci := wireUserIndexContaining(second, "Your previous tool call had errors")
	if ci != ai+1 {
		t.Errorf("correction at index %d, want %d (immediately after the superseded assistant)", ci, ai+1)
	}
	if wireUserIndexContaining(second, `function "frobnicate" not in the tool set`) < 0 {
		t.Errorf("retried request correction does not name the unknown tool: %+v", second)
	}

	if !hasGuardFire(sink.events, guardToolCallRepair, guardActionRetry) {
		t.Errorf("no FloorGuardEvent{Guard: %q, Action: %q}", guardToolCallRepair, guardActionRetry)
	}
	calls := dispatchedCalls(sink.events)
	if len(calls) != 1 || calls[0].ID != "c2" {
		t.Errorf("dispatched calls = %+v, want only the corrected c2 call", calls)
	}
	if ran != 1 {
		t.Errorf("tool ran %d times, want 1", ran)
	}
	// Request-scoped: the corrective exchange never committed to history.
	// user, assistant (c2 call), tool result, assistant final = 4 messages.
	if got := a.conv.Len(); got != 4 {
		t.Errorf("committed history has %d messages, want 4", got)
	}
}

// DisableToolCallRepair takes the guard away and nothing else: the malformed call reaches the tool
// path exactly as the model wrote it, no retry, no event.
func TestFloorGuard_DisableToolCallRepairLetsTheBadCallThrough(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, ran: &ran, result: "42"})
	cfg.Bypass = true
	cfg.Floor.DisableToolCallRepair = true
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "frobnicate", `{}`), // unknown — but the guard is off
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "look it up")

	if len(responder.got) != 2 {
		t.Fatalf("provider was called %d times, want 2 (no retry with the guard off)", len(responder.got))
	}
	if n := guardFireCountFor(sink.events, guardToolCallRepair); n != 0 {
		t.Errorf("the repair guard fired %d times with DisableToolCallRepair set", n)
	}
	if calls := dispatchedCalls(sink.events); len(calls) != 1 || calls[0].Tool != "frobnicate" {
		t.Errorf("dispatched calls = %+v, want the unrepaired frobnicate call to reach the tool path", calls)
	}
}

// A Turn that repeats the previous Turn's exact tool call draws the loop-breaking directive — again
// with no catalogued Mechanism and Bypass on — and the directive names the repeated tool. The
// firing is a FloorGuardEvent under the tool-loop-breaker key; its own opt-out takes it away.
func TestFloorGuard_ToolLoopBreakerOnAnIdenticalRepeat(t *testing.T) {
	for _, tc := range []struct {
		name      string
		disabled  bool
		wantCalls int
		wantFires int
	}{
		{name: "on by default", wantCalls: 3, wantFires: 1},
		{name: "DisableToolLoopBreaker", disabled: true, wantCalls: 3, wantFires: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "package a"})
			cfg.Bypass = true
			cfg.Floor.DisableToolLoopBreaker = tc.disabled
			responder := &captureAllResponder{scripts: [][]provider.Delta{
				toolCallScript("c1", "read_file", `{"path":"a.go"}`),
				toolCallScript("c2", "read_file", `{"path":"a.go"}`), // the identical repeat
				contentScript("done"),
			}}

			a, err := newAgent(cfg, responder)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			runExchange(t, a, "read a.go")

			// Three upstream calls either way — the difference is what the THIRD request carries:
			// the loop directive when the guard is on, the repeat's own tool result when it is off.
			if len(responder.got) != tc.wantCalls {
				t.Fatalf("provider was called %d times, want %d", len(responder.got), tc.wantCalls)
			}
			if n := guardFireCountFor(sink.events, guardToolLoopBreaker); n != tc.wantFires {
				t.Fatalf("the loop breaker fired %d times, want %d", n, tc.wantFires)
			}
			if tc.disabled {
				return
			}
			retried := responder.got[2].Messages
			if wireUserIndexContaining(retried, "read_file") < 0 {
				t.Errorf("the loop directive does not name the repeated tool: %+v", retried)
			}
			if wireUserIndexContaining(retried, "a.go") < 0 {
				t.Errorf("the loop directive does not credit the file already read: %+v", retried)
			}
		})
	}
}

// The loop breaker runs FIRST at the post-response seam (ADR 0071's ratified order) and the first
// guard to fire wins: a response that is BOTH an identical repeat and a malformed call draws the
// loop directive, and the repair guard does not run.
func TestFloorGuard_LoopBreakerWinsOverRepair(t *testing.T) {
	sink := &recordingSink{}
	writeFile := schemaTool{
		fakeTool: fakeTool{name: "write_file", result: "ok"},
		schema:   `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`,
	}
	cfg := configWithTools(sink, writeFile)
	cfg.Bypass = true
	// The first call is well formed and commits; the second repeats it verbatim AND is malformed
	// only in the sense the repair guard would also flag — so make the first call the malformed one
	// after it commits by giving both calls identical, complete arguments and repeating them.
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "write_file", `{"path":"a.go","content":"package a"}`),
		toolCallScript("c2", "write_file", `{"path":"a.go","content":"package a"}`),
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "write a.go")

	if !hasGuardFire(sink.events, guardToolLoopBreaker, guardActionRetry) {
		t.Error("the loop breaker did not fire on the identical repeat")
	}
	if n := guardFireCountFor(sink.events, guardToolCallRepair); n != 0 {
		t.Errorf("the repair guard fired %d times; the first guard to fire wins", n)
	}
}

// The guards are the FLOOR: a sub-agent inherits the parent's live opt-outs at spawn, so a child
// never runs with a floor its parent switched off (or without one its parent kept).
func TestFloorGuard_ChildInheritsTheLiveFloor(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	parent, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{contentScript("done")}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if got := parent.floorConfig(); got != (domain.FloorConfig{}) {
		t.Fatalf("a bare Config seeds %+v, want the zero value (every guard on)", got)
	}

	parent.SetFloor(domain.FloorConfig{DisableToolLoopBreaker: true})
	child, err := parent.newChildAgent("spawn-1", "survey the tree", "surveyor")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	want := domain.FloorConfig{DisableToolLoopBreaker: true}
	if got := child.floorConfig(); got != want {
		t.Errorf("child floor = %+v, want the parent's live %+v", got, want)
	}
}

// A guard fires on the model's OWN failure, so a clean Turn books nothing: no FloorGuardEvent at
// all, and the response stands exactly as the model wrote it.
func TestFloorGuard_CleanTurnBooksNoFiring(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "read_file", `{"path":"a.go"}`),
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "read a.go")

	for _, e := range sink.events {
		if ge, ok := e.(domain.FloorGuardEvent); ok {
			t.Errorf("a clean Turn booked a guard firing: %+v", ge)
		}
	}
	if !strings.Contains(mustLastMessageText(t, sink.events), "done") {
		t.Error("the clean Turn did not produce its reply")
	}
}

// mustLastMessageText returns the last MessageEvent's text, failing the test when there is none.
func mustLastMessageText(t *testing.T, events []domain.Event) string {
	t.Helper()
	me, ok := lastMessageEvent(events)
	if !ok {
		t.Fatal("no MessageEvent was emitted")
	}
	return me.Text
}

// A third narration on an action request the model never acted on is corrected into a tool call —
// with NO catalogued Mechanism enabled and Bypass ON. The retried request carries the superseded
// narration followed by the "use a tool" correction (the sim's retryForToolUse shape), the corrected
// call is the one that dispatches, and the firing is booked as a FloorGuardEvent naming the key.
func TestFloorGuard_ToolUseEnforcerRetriesUnderBypass(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink,
		fakeTool{name: "read_file", readOnly: true, ran: &ran, result: "contents"},
		fakeTool{name: "write_file", result: "ok"},
	)
	cfg.Bypass = true
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript("I'll implement feature X."),
		contentScript("Here is my plan."),
		contentScript("I would edit main.go to add the parser."), // narration #3 — the guard retries
		toolCallScript("c1", "read_file", `{"path":"main.go"}`),  // the corrected, acting response
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement feature X")
	runExchange(t, a, "continue")
	runExchange(t, a, "please implement feature X now")

	if len(responder.got) != 5 {
		t.Fatalf("provider was called %d times, want 5", len(responder.got))
	}
	retried := responder.got[3].Messages
	ai := wireMessageIndex(retried, "assistant", "I would edit main.go to add the parser.")
	if ai < 0 {
		t.Fatalf("retried request carries no superseded narration: %+v", retried)
	}
	ci := wireUserIndexContaining(retried, "You MUST use one of the available tools")
	if ci != ai+1 {
		t.Errorf("correction at index %d, want %d (immediately after the superseded narration)", ci, ai+1)
	}
	if wireUserIndexContaining(retried, "Respond with a tool call, not a text description.") < 0 {
		t.Errorf("retried request lacks the sim's tool-use directive: %+v", retried)
	}
	if !hasGuardFire(sink.events, guardToolUseEnforcer, guardActionRetry) {
		t.Error("no FloorGuardEvent for the tool-use enforcer with the retry action")
	}
	if ran != 1 {
		t.Errorf("read_file ran %d times, want 1 (the corrected response acted)", ran)
	}
}

// `tool-use-enforcer: false` gives the prose back: the same three narrations run to their end with
// no retry, no firing, and the third narration standing as the Turn's answer.
func TestFloorGuard_DisableToolUseEnforcerLeavesProseAlone(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink,
		fakeTool{name: "read_file", readOnly: true, result: "contents"},
		fakeTool{name: "write_file", result: "ok"},
	)
	cfg.Floor.DisableToolUseEnforcer = true
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript("I'll implement feature X."),
		contentScript("Here is my plan."),
		contentScript("I would edit main.go to add the parser."),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement feature X")
	runExchange(t, a, "continue")
	runExchange(t, a, "please implement feature X now")

	if len(responder.got) != 3 {
		t.Fatalf("provider was called %d times, want 3 (no retry)", len(responder.got))
	}
	if guardFireCountFor(sink.events, guardToolUseEnforcer) != 0 {
		t.Error("the tool-use enforcer fired with Floor.DisableToolUseEnforcer set")
	}
	if got := mustLastMessageText(t, sink.events); got != "I would edit main.go to add the parser." {
		t.Errorf("final message = %q, want the narration to stand", got)
	}
}

// An empty reply mid-task retries in place and the retried request carries the sim's
// completion-check nudge verbatim as a role-safe user message — no catalogued Mechanism, Bypass on
// — and no superseded assistant message, the empty draft having carried nothing.
func TestFloorGuard_EmptyReplyDrawsTheCompletionCheckNudge(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	cfg.Bypass = true
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		emptyScript(),
		contentScript("recovered"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement the parser")

	if len(responder.got) != 2 {
		t.Fatalf("provider was called %d times, want 2", len(responder.got))
	}
	second := responder.got[1].Messages
	if n := wireRoleCount(second, "assistant"); n != 0 {
		t.Errorf("retried request carries %d assistant messages, want 0 (empty superseded reply)", n)
	}
	if wireMessageIndex(second, "user", wave1Nudge) < 0 {
		t.Errorf("retried request does not carry the completion-check nudge verbatim: %+v", second)
	}
	if !hasGuardFire(sink.events, guardEmptyResponseRecovery, guardActionRetry) {
		t.Error("no FloorGuardEvent for the empty-response recovery with the retry action")
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "recovered" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q", me, ok, "recovered")
	}
}

// A responder that never produces anything terminates at the loop's maxPostResponseRetries — the
// recovery guard cannot spin the loop. Past the cap the empty reply fails the Turn visibly
// (loop.go reviewedOutcome) rather than committing a blank assistant message: recover first, fail
// honestly when recovery is exhausted. The cap itself is unmoved, and that is what this test is for.
func TestFloorGuard_AlwaysEmptyTerminatesAtCap(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		emptyScript(), emptyScript(), emptyScript(), emptyScript(),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	res := runExchange(t, a, "please implement the parser")

	if res.Status != domain.StatusExchangeComplete || !res.Faulted {
		t.Errorf("StepResult = {Status:%q Faulted:%v}, want {Status:%q Faulted:true} (the exhausted guard faults)",
			res.Status, res.Faulted, domain.StatusExchangeComplete)
	}
	if len(responder.got) != maxPostResponseRetries+1 {
		t.Errorf("provider was called %d times, want %d (the retry cap)",
			len(responder.got), maxPostResponseRetries+1)
	}
	if _, ok := lastMessageEvent(sink.events); ok {
		t.Error("a MessageEvent was emitted for a Turn that never produced a reply")
	}
	if got := a.conv.Len(); got != 1 {
		t.Errorf("committed history has %d messages, want 1 (the user message; no blank assistant)", got)
	}
}

// ADR 0071 decision 1 at the dispatch level: neither recovery guard carries strikes-3 suppression
// nor a Turn-Budget throttle, so both still fire with Bypass ON and the global Turn Budget TRIPPED —
// the posture in which a co-registered catalogued Mechanism (syntax) is withdrawn at dispatch.
func TestFloorGuard_RecoveriesFireUnderBypassAndTrippedBudget(t *testing.T) {
	t.Run("empty-response-recovery", func(t *testing.T) {
		sink := &recordingSink{}
		cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
		cfg.Bypass = true
		cfg.Mechanisms = wave1Registry(t, "syntax")
		responder := &captureAllResponder{scripts: [][]provider.Delta{
			emptyScript(),
			contentScript("recovered"),
		}}

		a, err := newAgent(cfg, responder)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		a.tracker.budgetTripped = true
		a.tracker.harmfulStreak = turnBudgetLimit
		runExchange(t, a, "please implement the parser")

		if len(responder.got) != 2 {
			t.Fatalf("provider was called %d times, want 2 (the guard must retry through the gates)", len(responder.got))
		}
		if wireMessageIndex(responder.got[1].Messages, "user", wave1Nudge) < 0 {
			t.Errorf("retried request does not carry the nudge: %+v", responder.got[1].Messages)
		}
		if !hasGuardFire(sink.events, guardEmptyResponseRecovery, guardActionRetry) {
			t.Error("no FloorGuardEvent for the empty-response recovery with the retry action")
		}
		if n := fireCountFor(sink.events, "syntax"); n != 0 {
			t.Errorf("syntax fired %d times; a catalogued row must be withdrawn under Bypass + a tripped Turn Budget", n)
		}
	})

	t.Run("tool-use-enforcer", func(t *testing.T) {
		sink := &recordingSink{}
		cfg := configWithTools(sink,
			fakeTool{name: "read_file", readOnly: true, result: "contents"},
			fakeTool{name: "write_file", result: "ok"},
		)
		cfg.Bypass = true
		cfg.Mechanisms = wave1Registry(t, "syntax")
		responder := &captureAllResponder{scripts: [][]provider.Delta{
			contentScript("I'll implement feature X."),
			contentScript("Here is my plan."),
			contentScript("I would edit main.go to add the parser."),
			toolCallScript("c1", "read_file", `{"path":"main.go"}`),
			contentScript("done"),
		}}

		a, err := newAgent(cfg, responder)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		a.tracker.budgetTripped = true
		a.tracker.harmfulStreak = turnBudgetLimit
		runExchange(t, a, "please implement feature X")
		runExchange(t, a, "continue")
		runExchange(t, a, "please implement feature X now")

		if len(responder.got) != 5 {
			t.Fatalf("provider was called %d times, want 5 (the guard must retry through the gates)", len(responder.got))
		}
		retried := responder.got[3].Messages
		if wireMessageIndex(retried, "assistant", "I would edit main.go to add the parser.") < 0 {
			t.Errorf("retried request carries no superseded narration: %+v", retried)
		}
		if wireUserIndexContaining(retried, "You MUST use one of the available tools") < 0 {
			t.Errorf("retried request carries no correction: %+v", retried)
		}
		if !hasGuardFire(sink.events, guardToolUseEnforcer, guardActionRetry) {
			t.Error("no FloorGuardEvent for the tool-use enforcer with the retry action")
		}
		if n := fireCountFor(sink.events, "syntax"); n != 0 {
			t.Errorf("syntax fired %d times; a catalogued row must be withdrawn under Bypass + a tripped Turn Budget", n)
		}
	})
}
