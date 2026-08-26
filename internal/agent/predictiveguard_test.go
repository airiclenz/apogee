package agent

// The PREDICTIVE half of overflow protection: when the calibrated estimate already says the
// request cannot fit the model's context window, step() folds the history BEFORE the request
// reaches the wire — saving the round-trip the server would reject, and covering the one case the
// reactive path cannot (a server whose 400 body the provider cannot classify as an overflow).
// These tests pin the threshold (the full HARD room — the advertised window less the reply
// reserve, exactly, never a comfort margin and never the softer `working-window:` ceiling), the one
// fold the predictive and reactive paths share, the conservative ceiling an unknown window falls
// back to, and the way the guard stays out of the way: a refused fold still sends the request.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
)

// calibrate pins the Agent's chars→token ratio to the default 4.0 through a real usage sample, so
// a test can size a conversation against a known threshold. The blend is a no-op at this sample
// (4000 chars / 1000 tokens is exactly the default ratio), which is the point: the estimator is
// genuinely CALIBRATED (Used > 0) without the arithmetic depending on the blending weight.
func calibrate(a *Agent) {
	a.tokens.Calibrate(4000, 1000)
}

// workingRoom is the threshold the predictive guard compares against: everything the prompt may
// occupy inside the ADVERTISED window once the reply's reserve is held back. It reads Window
// rather than the working ceiling ContextLimit because the guard does — the two are the same
// number on every Agent here, none of which bounds its working room.
func workingRoom(a *Agent) int {
	b := a.budget()
	return b.Window - b.ResponseReserve
}

// seedSizedOpenTurn seeds an Exchange in flight whose history ends in a tool result — a tool
// CONTINUATION, where autoCompact stands down (S2) so the predictive guard is the only reducer
// that can act — and pads the result so the request the next Step builds measures exactly chars
// under domain.PromptChars. The tail is 2 messages past the 1-message protected prefix, so the
// fold itself is never Skipped.
func seedSizedOpenTurn(t *testing.T, a *Agent, chars int) {
	t.Helper()
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "implement feature X"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
		{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)},
	}})
	a.turns.inExchange = true
	a.turns.exchangeStart = 0
	pad := chars - domain.PromptChars(a.conv.Messages(), a.toolMenu())
	if pad < 0 {
		t.Fatalf("test setup: the seeded shape already measures more than the %d chars asked for", chars)
	}
	a.conv.Append(domain.Message{Role: domain.RoleTool, ToolCallID: "c1", Content: strings.Repeat("x", pad)})
	if got := domain.PromptChars(a.conv.Messages(), a.toolMenu()); got != chars {
		t.Fatalf("test setup: seeded request measures %d chars, want %d", got, chars)
	}
}

// TestPredictiveGuardFoldsAtTheWindowThresholdOnly pins the CALIBRATED trigger to the exact
// working room — no uncertainty margin once a UsageEvent has measured the ratio: a request whose
// estimate lands ON the limit is sent as built (the estimate says it fits), and one token over it
// folds BEFORE any Upstream call — the fake records only the post-fold request, which is the
// folded prefix | summary | bridge shape.
func TestPredictiveGuardFoldsAtTheWindowThresholdOnly(t *testing.T) {
	tests := []struct {
		name     string
		overBy   int // chars added past an exactly-fitting request
		wantFold bool
	}{
		{name: "an exactly-fitting request is sent as built", overBy: 0, wantFold: false},
		{name: "one char past the working room folds first", overBy: 1, wantFold: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			up := &recoveryResponder{reply: "the reply", summary: "EMERGENCY-SUMMARY"}
			a, err := newAgent(autoCompactConfig(sink), up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			calibrate(a)
			fits := int(float64(workingRoom(a)) * a.budget().CharsPerToken)
			seedSizedOpenTurn(t, a, fits+tc.overBy)

			res, err := a.Step(context.Background()) // no Submit: the Exchange is already open
			if err != nil {
				t.Fatalf("Step: %v", err)
			}

			if res.Status != domain.StatusExchangeComplete {
				t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
			}
			if errs := errorEvents(sink.events); len(errs) != 0 {
				t.Errorf("the guard surfaced %d ErrorEvent(s) %v; a predictive fold is quiet", len(errs), errs)
			}
			if len(up.mains) != 1 {
				t.Fatalf("main requests = %d, want 1 — the guard replaces the oversized request, it does not add one",
					len(up.mains))
			}
			want := 0
			if tc.wantFold {
				want = 1
			}
			if up.summaries != want {
				t.Fatalf("summarizer calls = %d, want %d", up.summaries, want)
			}

			sent := up.mains[0]
			if !tc.wantFold {
				if len(sent.Messages) != 3 {
					t.Errorf("request carried %d messages, want the 3 seeded ones unfolded", len(sent.Messages))
				}
				return
			}
			// The ONE request that reached the wire is the folded one: the guard fired before it.
			if len(sent.Messages) != 3 {
				t.Fatalf("folded request carried %d messages, want 3 (prefix | summary | bridge)", len(sent.Messages))
			}
			if got := sent.Messages[1]; got.Role != string(domain.RoleAssistant) || !strings.Contains(got.Content, "EMERGENCY-SUMMARY") {
				t.Errorf("request message 1 is not the assistant summary: %+v", got)
			}
			if got := sent.Messages[2]; got.Role != string(domain.RoleUser) || got.Content != overflowBridge {
				t.Errorf("request does not end at the user bridge: %+v", got)
			}
			assertRequestTemplateLegal(t, sent)
			if a.conv.Len() != 4 {
				t.Errorf("conv.Len() = %d (roles %s), want 4 (prefix + summary + bridge + reply)", a.conv.Len(), convRoles(a))
			}
		})
	}
}

// TestPredictiveGuardUncalibratedNeedsTwiceTheRoom pins the uncertainty margin on an estimator that
// has never seen a UsageEvent (Turn 1, a sub-agent, the first Turn after a resume): there the ratio
// is only the 4.0 seed, which can overstate the true token count by at most 2x (the clamp band's
// 8.0 ceiling), so the guard stays silent anywhere between one and two times the working room —
// where a fold might be spent on a request that would have fit — and still fires past it, keeping
// the pathological cases and the unclassifiable-400 cover the guard exists for.
func TestPredictiveGuardUncalibratedNeedsTwiceTheRoom(t *testing.T) {
	tests := []struct {
		name     string
		chars    func(fits int) int // the request's size, from the chars that exactly fill the working room
		wantFold bool
	}{
		{
			name:     "one char past the working room is not enough while uncalibrated",
			chars:    func(fits int) int { return fits + 1 },
			wantFold: false,
		},
		{
			name:     "exactly twice the working room is still not enough",
			chars:    func(fits int) int { return 2 * fits },
			wantFold: false,
		},
		{
			name:     "one char past twice the working room folds",
			chars:    func(fits int) int { return 2*fits + 1 },
			wantFold: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			up := &recoveryResponder{reply: "the reply", summary: "EMERGENCY-SUMMARY"}
			a, err := newAgent(autoCompactConfig(sink), up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			// Deliberately NOT calibrated: the estimator still carries the seed ratio and a zero Used.
			if b := a.budget(); b.Used != 0 || b.CharsPerToken != apogeectx.DefaultCharsPerToken {
				t.Fatalf("test setup: budget is %+v, want an uncalibrated one (Used 0, the seed ratio)", b)
			}
			fits := int(float64(workingRoom(a)) * a.budget().CharsPerToken)
			seedSizedOpenTurn(t, a, tc.chars(fits))

			res, err := a.Step(context.Background()) // no Submit: the Exchange is already open
			if err != nil {
				t.Fatalf("Step: %v", err)
			}

			if res.Status != domain.StatusExchangeComplete {
				t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
			}
			want := 0
			if tc.wantFold {
				want = 1
			}
			if up.summaries != want {
				t.Fatalf("summarizer calls = %d, want %d", up.summaries, want)
			}
			if len(up.mains) != 1 {
				t.Fatalf("main requests = %d, want 1", len(up.mains))
			}
			sent := up.mains[0]
			if !tc.wantFold {
				if len(sent.Messages) != 3 {
					t.Errorf("request carried %d messages, want the 3 seeded ones unfolded", len(sent.Messages))
				}
				return
			}
			if len(sent.Messages) != 3 {
				t.Fatalf("folded request carried %d messages, want 3 (prefix | summary | bridge)", len(sent.Messages))
			}
			if got := sent.Messages[1]; got.Role != string(domain.RoleAssistant) || !strings.Contains(got.Content, "EMERGENCY-SUMMARY") {
				t.Errorf("request message 1 is not the assistant summary: %+v", got)
			}
			assertRequestTemplateLegal(t, sent)
		})
	}
}

// TestPredictiveGuardWithoutAKnownWindowUsesTheConservativeCeiling pins what an unknown window
// does to the guard. It used to make it INERT — no working room, so no comparison — which left a
// session with no window with no predictive protection at all, including against the one failure
// the reactive path cannot see (a 400 the provider cannot classify as an overflow). It now measures
// the TRANSCRIPT against compactUnknownWindowTranscriptTokens, the same ceiling the emergency fold
// renders against, so the guard still fires only on "cannot fit", just against an assumed window
// rather than a discovered one (audit 2026-08-01, follow-up B). The seeded shape carries no tool
// menu, so what it sizes is the transcript either way; that the menu is EXCLUDED is pinned by
// TestUnknownWindowGuardMeasuresTheTranscriptNotTheWholeRequest (unknownwindow_test.go).
func TestPredictiveGuardWithoutAKnownWindowUsesTheConservativeCeiling(t *testing.T) {
	ceilingChars := func(a *Agent) int {
		return int(float64(compactUnknownWindowTranscriptTokens) * a.budget().CharsPerToken)
	}
	tests := []struct {
		name     string
		chars    func(*Agent) int
		wantFold bool
	}{
		{
			name:     "a request under the assumed ceiling is sent as built",
			chars:    func(a *Agent) int { return ceilingChars(a) / 2 },
			wantFold: false,
		},
		{
			name:     "a request past the assumed ceiling folds before the wire",
			chars:    func(a *Agent) int { return 4 * ceilingChars(a) },
			wantFold: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			up := &recoveryResponder{reply: "the reply", summary: "EMERGENCY-SUMMARY"}
			cfg := baseConfig(sink) // no MaxContextTokens: neither discovery nor `context-window:` reported one
			cfg.Context.CompactionEnabled = true
			a, err := newAgent(cfg, up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			calibrate(a) // CALIBRATED, so the ratio margin is out of the way and the ceiling is the bare one
			if room := workingRoom(a); room > 0 {
				t.Fatalf("test setup: working room = %d, want none — this test is about an unknown window", room)
			}
			seedSizedOpenTurn(t, a, tc.chars(a))

			res, err := a.Step(context.Background())
			if err != nil {
				t.Fatalf("Step: %v", err)
			}

			if res.Status != domain.StatusExchangeComplete {
				t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
			}
			wantSummaries := 0
			if tc.wantFold {
				wantSummaries = 1
			}
			if up.summaries != wantSummaries {
				t.Errorf("summarizer calls = %d, want %d", up.summaries, wantSummaries)
			}
			if len(up.mains) != 1 {
				t.Fatalf("main requests = %d, want 1", len(up.mains))
			}
			sent := up.mains[0]
			if !tc.wantFold {
				if len(sent.Messages) != 3 {
					t.Errorf("request carried %d messages, want the 3 seeded ones untouched", len(sent.Messages))
				}
				return
			}
			if len(sent.Messages) != 3 {
				t.Fatalf("folded request carried %d messages, want 3 (prefix | summary | bridge)", len(sent.Messages))
			}
			if got := sent.Messages[1]; got.Role != string(domain.RoleAssistant) || !strings.Contains(got.Content, "EMERGENCY-SUMMARY") {
				t.Errorf("request message 1 is not the assistant summary: %+v", got)
			}
		})
	}
}

// TestPredictiveGuardRefusedFoldStillSendsTheRequest pins the guard's advisory nature at its
// sharpest edge: with `auto-compact: false` the fold declines, and the Turn goes out exactly as it
// would have without the guard. An estimate is never on its own a reason to abandon a Turn — the
// server, not the estimator, has the last word on what fits.
func TestPredictiveGuardRefusedFoldStillSendsTheRequest(t *testing.T) {
	sink := &recordingSink{}
	up := &recoveryResponder{reply: "the reply", summary: "UNREACHED"}
	cfg := autoCompactConfig(sink)
	cfg.Context.CompactionEnabled = false
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	calibrate(a)
	seedSizedOpenTurn(t, a, 4*int(float64(workingRoom(a))*a.budget().CharsPerToken))

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
	if errs := errorEvents(sink.events); len(errs) != 0 {
		t.Errorf("a refused predictive fold surfaced %d ErrorEvent(s) %v; it is not a fault", len(errs), errs)
	}
	if up.summaries != 0 {
		t.Errorf("summarizer calls = %d with auto-compact off, want 0", up.summaries)
	}
	if len(up.mains) != 1 || len(up.mains[0].Messages) != 3 {
		t.Errorf("main requests = %d (first carrying %d messages), want 1 carrying the 3 seeded ones unfolded",
			len(up.mains), len(up.mains[0].Messages))
	}
}

// TestPredictiveGuardSpendsTheTurnsOneFold is the interlock with the reactive path: the two share
// ONE fold per Turn, so when a predictively folded request STILL overflows on the wire, the Turn
// gives up on the spot — one summarizer call, one main request, and the give-up ErrorEvent
// byte-identical to a plain fault's — rather than folding a second time.
func TestPredictiveGuardSpendsTheTurnsOneFold(t *testing.T) {
	sink := &recordingSink{}
	up := &recoveryResponder{reply: "UNREACHED", summary: "EMERGENCY-SUMMARY", overflows: []bool{true}}
	a, err := newAgent(autoCompactConfig(sink), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	calibrate(a)
	seedSizedOpenTurn(t, a, 4*int(float64(workingRoom(a))*a.budget().CharsPerToken))

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q — a spent recovery ends the Exchange at a clean boundary",
			res.Status, domain.StatusExchangeComplete)
	}
	if up.summaries != 1 {
		t.Errorf("summarizer calls = %d, want exactly 1 — the predictive fold spent the Turn's only one", up.summaries)
	}
	if len(up.mains) != 1 {
		t.Errorf("main requests = %d, want 1 — the overflow arrives with the fold already spent, so there is no retry",
			len(up.mains))
	}
	errs := errorEvents(sink.events)
	if len(errs) != 1 {
		t.Fatalf("ErrorEvents = %d (%v), want exactly 1 — the give-up is indistinguishable from a plain fault", len(errs), errs)
	}
	if errs[0].Source != "loop" || errs[0].Err != overflowFaultMsg {
		t.Errorf("ErrorEvent = {Source:%q Err:%q}, want {Source:%q Err:%q}",
			errs[0].Source, errs[0].Err, "loop", overflowFaultMsg)
	}
	if hasEvent[domain.MessageEvent](sink.events) {
		t.Error("a MessageEvent was emitted for a Turn that produced no assistant message")
	}
}

// TestPredictiveGuardMeasuresTheAdvertisedWindow pins which of the Budget's two ceilings this
// guard reads. A `working-window:` bound is a SOFT line the reducers keep the session under, so a
// request that runs past it while still fitting the server's window must not be folded — folding
// there would make the bound a fold trigger rather than a budget, on every Turn of a bounded
// session. The hard wall still folds, which is what the guard is for.
func TestPredictiveGuardMeasuresTheAdvertisedWindow(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(&recordingSink{})
	cfg.Context.MaxContextTokens = 1310720
	cfg.Context.WorkingWindow = 200000
	a, err := newAgent(cfg, echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	calibrate(a) // CALIBRATED: the ratio margin is out of the way and the room is the bare one

	b := a.budget()
	const past = 400000 // tokens: far past the 200K working room, far inside the 1.3M window
	if working := b.ContextLimit - b.ResponseReserve; working >= past {
		t.Fatalf("test setup: the working room is %d tokens, not under the %d-token request", working, past)
	}
	exceeds := func(tokens int) bool {
		msgs := []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("x", tokens*4)}}
		return a.requestExceedsWindow(domain.NewRequest(a.cfg.Model, msgs, nil, a.budget(), 0, nil))
	}

	if exceeds(past) {
		t.Errorf("a %d-token request folded under a %d working / %d advertised pair; the guard is reading the soft ceiling",
			past, b.ContextLimit, b.Window)
	}
	if !exceeds(b.Window) {
		t.Errorf("a request the size of the whole %d-token advertised window did not fold; the hard wall is unguarded", b.Window)
	}
}
