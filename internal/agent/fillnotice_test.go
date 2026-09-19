package agent

// The context-fill notice (ADR 0077, fillnotice.go): the engine's first advise Reaction. These
// tests drive it two ways — through the whole loop, where the notice has to land as the advise
// trailer on the closing tool result the model reads next, and through the post-tool-result
// cascade directly (adviseOneCall), where one result's size can be chosen to the character. The
// numbers rest on the uncalibrated ratio (4 chars per token, no usage in the scripts) and the 8192
// window's History allocation of 3,933 tokens (internal/context.Allocate: 20% reserve, 15% system,
// 25% file context), so 50% of the line is ~7.9k chars, 75% ~11.8k, 90% ~14.2k and 100% ~15.7k.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fillConfig is an 8192-window config with the notice switched on and no compaction — the
// climb is observed within one Exchange, where no fold can move the line under it.
func fillConfig(sink domain.EventSink) domain.Config {
	cfg := configWithTools(sink)
	cfg.Context.MaxContextTokens = 8192
	cfg.ContextFillNotice = true
	return cfg
}

// fillAgent builds an Agent on cfg holding one assistant tool call, so adviseOneCall can commit
// results against it exactly as adviseAgent does for user advise entries.
func fillAgent(t *testing.T, cfg domain.Config) *Agent {
	t.Helper()

	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}})
	return a
}

// noticeFirings returns the ReactionFiredEvents the notice booked, in order.
func noticeFirings(sink *recordingSink) []domain.ReactionFiredEvent {
	var out []domain.ReactionFiredEvent
	for _, fe := range firedAdvice(sink) {
		if fe.Reaction == contextFillNoticeID {
			out = append(out, fe)
		}
	}
	return out
}

// noticeSpan asserts msg carries exactly one advice span and that it is the notice's — engine
// origin, at post-tool-result — and returns it for the caller's own look at its Turn.
func noticeSpan(t *testing.T, msg domain.Message) domain.AdviceSpan {
	t.Helper()
	if len(msg.Advice) != 1 {
		t.Fatalf("tool message carries %d advice spans, want one from the notice: %+v", len(msg.Advice), msg.Advice)
	}
	span := msg.Advice[0]
	if span.Reaction != contextFillNoticeID || span.Origin != domain.OriginEngine || span.Moment != domain.MomentPostToolResult {
		t.Errorf("span = %+v, want reaction %q of engine origin at post-tool-result", span, contextFillNoticeID)
	}
	return span
}

// toolMessages returns the tool-role messages of a's conversation, in order.
func toolMessages(a *Agent) []domain.Message {
	var out []domain.Message
	for i := 0; i < a.conv.Len(); i++ {
		if m := a.conv.At(i); m.Role == domain.RoleTool {
			out = append(out, m)
		}
	}
	return out
}

// sizedTool is a read-only fake tool answering each call with the next scripted body, so a
// driven Exchange climbs the line one result at a time.
func sizedTool(sizes ...int) fakeTool {
	i := 0
	return fakeTool{name: "probe", readOnly: true, execute: func(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
		body := strings.Repeat("x", sizes[i])
		i++
		return domain.ToolResult{CallID: call.ID, Content: body}, nil
	}}
}

// The whole contract through the loop: three results climbing past 50, then 75, then 90 fire
// exactly three notices, one each, and each lands as the fenced advise trailer on the tool result
// it measured — the ledger names the notice, the content ends with the shipped fence around the
// rendered line, and the firing books rung and percent, not the text.
func TestContextFillNoticeFiresOncePerRungOnTheClosingToolResult(t *testing.T) {
	sink := &recordingSink{}
	up := scriptedResponder(t,
		toolCallTurn("c1", "probe", `{"n":1}`), // distinct arguments: a verbatim repeat is the
		toolCallTurn("c2", "probe", `{"n":2}`), // tool-loop breaker's cue, not a climb
		toolCallTurn("c3", "probe", `{"n":3}`),
		contentTurn("done"),
	)
	cfg := fillConfig(sink)
	// "start" (5) + three calls (12 each) + each fence's own text ride beside the bodies: the
	// first lands at 8,277 chars = 2,070 tokens = 52% of 3,933; the second near 78%; the third
	// near 95%.
	cfg.Tools = domain.NewToolRegistry()
	if err := cfg.Tools.Register(sizedTool(8260, 3800, 2500)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runExchange(t, a, "start")

	fired := noticeFirings(sink)
	if len(fired) != 3 {
		t.Fatalf("notice fired %d times, want 3 (one per rung): %+v", len(fired), fired)
	}
	wantDetail := []string{"rung 50 (52%)", "rung 75 (", "rung 90 ("}
	for i, fe := range fired {
		if fe.Action != actionNotice || !strings.HasPrefix(fe.Detail, wantDetail[i]) {
			t.Errorf("firing %d = action %q detail %q, want %q / %q…", i, fe.Action, fe.Detail, actionNotice, wantDetail[i])
		}
	}

	msgs := toolMessages(a)
	if len(msgs) != 3 {
		t.Fatalf("conversation holds %d tool messages, want 3", len(msgs))
	}
	for i, msg := range msgs {
		span := noticeSpan(t, msg)
		if span.Turn != i {
			t.Errorf("tool message %d: span Turn = %d, want %d", i, span.Turn, i)
		}
	}
	const wantLine = "context: 52% of the way to automatic compaction — 2.1k tokens used of a 8.2k window"
	if !strings.HasSuffix(msgs[0].Content, domain.RenderAdvice(msgs[0].Advice[0], wantLine)) {
		t.Errorf("first tool message = %q, want it to end with the shipped fence around %q", msgs[0].Content, wantLine)
	}
	if strings.Contains(msgs[2].Content, fillWrapUp) {
		t.Errorf("main agent's 90 rung = %q, want the fact line without the child wrap-up", msgs[2].Content)
	}
}

// One result that jumps the fill from 40% to 80% fires ONE notice — at the highest rung reached,
// reporting the actual percent — not one per rung crossed.
func TestContextFillNoticeReportsTheHighestRungOneResultCrosses(t *testing.T) {
	sink := &recordingSink{}
	a := fillAgent(t, fillConfig(sink))
	// 6,291 + "read_file" (9) = 6,300 chars = 1,575 tokens = 40%; the result adds the same again.
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: strings.Repeat("h", 6291)})

	msg := adviseOneCall(t, a, strings.Repeat("x", 6300))

	fired := noticeFirings(sink)
	if len(fired) != 1 || fired[0].Detail != "rung 75 (80%)" {
		t.Fatalf("firings = %+v, want exactly one at rung 75 reporting 80%%", fired)
	}
	noticeSpan(t, msg)
	if !strings.Contains(msg.Content, "context: 80% of the way") {
		t.Errorf("tool message = %q, want the 80%% fact line", msg.Content)
	}
}

// A single result that crosses 90 and 100 together fires exactly one notice — reporting a
// percent past 100 — and the automatic Compaction trigger agrees the line is crossed at the next
// Turn boundary; that fold re-arms the ladder, so a result under 50 is silent and the next climb
// past 50 fires the 50 rung again.
func TestContextFillNoticeReArmsAfterAFold(t *testing.T) {
	sink := &recordingSink{}
	up := scriptedCompactResponder(t, "FOLDED",
		toolCallTurn("c1", "probe", `{"n":1}`), // Exchange 1: one result past the line
		contentTurn("first done"),
		toolCallTurn("c2", "probe", `{"n":2}`), // Exchange 2 (after the fold): under 50, then past it
		toolCallTurn("c3", "probe", `{"n":3}`),
		contentTurn("second done"),
	)
	cfg := fillConfig(sink)
	cfg.Context.CompactionEnabled = true
	cfg.Tools = domain.NewToolRegistry()
	if err := cfg.Tools.Register(sizedTool(16000, 1000, 8300)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runExchange(t, a, "start")

	fired := noticeFirings(sink)
	if len(fired) != 1 || !strings.HasPrefix(fired[0].Detail, "rung 90 (") {
		t.Fatalf("firings after the first Exchange = %+v, want one at rung 90", fired)
	}
	var pct int
	if _, err := fmt.Sscanf(fired[0].Detail, "rung 90 (%d%%)", &pct); err != nil || pct < 100 {
		t.Errorf("detail = %q, want a percent at or past 100 (%v)", fired[0].Detail, err)
	}
	if !a.historyExceedsAllocation() {
		t.Fatal("the trigger does not see the history over its allocation after a result the notice reported past 100%")
	}

	runExchange(t, a, "again")

	if up.summaryCalls() != 1 {
		t.Fatalf("summary calls = %d, want the one fold at the second Exchange's opening", up.summaryCalls())
	}
	fired = noticeFirings(sink)
	if len(fired) != 2 || !strings.HasPrefix(fired[1].Detail, "rung 50 (") {
		t.Fatalf("firings after the fold = %+v, want the 90 rung and then the 50 rung fired again", fired)
	}
}

// The fold re-arms the WHOLE ladder, not only the rungs the fill fell under: the first post-fold
// result fires whichever rung its own fill reaches — 50 when it lands at 50–74, 75 (and never 50)
// when it lands at 75–89 — exactly as a fresh session's first result would. The old re-arm kept
// the 50 rung "fired" from the climb the fold erased and said nothing until 75.
func TestContextFillNoticeFirstPostFoldResultFiresItsOwnRung(t *testing.T) {
	cases := []struct {
		name     string
		size     int    // the first post-fold result's body; the fold leaves ~180 chars beside it
		wantRung string // the one firing's Detail prefix
	}{
		{"lands at 50–74 and fires 50", 8300, "rung 50 ("},
		{"lands at 75–89 and fires 75, never 50", 12500, "rung 75 ("},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			up := scriptedCompactResponder(t, "FOLDED",
				toolCallTurn("c1", "probe", `{"n":1}`), // Exchange 1: one result past the line
				contentTurn("first done"),
				toolCallTurn("c2", "probe", `{"n":2}`), // Exchange 2 (after the fold): straight onto a rung
				contentTurn("second done"),
			)
			cfg := fillConfig(sink)
			cfg.Context.CompactionEnabled = true
			cfg.Tools = domain.NewToolRegistry()
			if err := cfg.Tools.Register(sizedTool(16000, tc.size)); err != nil {
				t.Fatalf("Register: %v", err)
			}
			a, err := newAgent(cfg, up)
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			runExchange(t, a, "start")
			runExchange(t, a, "again")

			if up.summaryCalls() != 1 {
				t.Fatalf("summary calls = %d, want the one fold at the second Exchange's opening", up.summaryCalls())
			}
			fired := noticeFirings(sink)
			if len(fired) != 2 || !strings.HasPrefix(fired[0].Detail, "rung 90 (") || !strings.HasPrefix(fired[1].Detail, tc.wantRung) {
				t.Fatalf("firings = %+v, want the 90 rung before the fold and then exactly one at %q…", fired, tc.wantRung)
			}
			msgs := toolMessages(a)
			if len(msgs) != 1 {
				t.Fatalf("conversation holds %d tool messages after the fold, want the one post-fold result", len(msgs))
			}
			span := noticeSpan(t, msgs[0])
			if !strings.Contains(msgs[0].Content, "context: ") || span.Reaction != contextFillNoticeID {
				t.Errorf("post-fold tool message = %q, want the notice's fact line as its trailer", msgs[0].Content)
			}
		})
	}
}

// An aborted Exchange (Esc in the TUI) drops the tool result a notice rode on, so it ends the
// climb the way a fold does: a fired 50, then AbortExchange, then a result back at 50–74% fires
// the 50 rung again, once — never left silent until 75 by a rung marked fired from a conversation
// the model no longer sees.
func TestContextFillNoticeReArmsAfterAnAbortedExchange(t *testing.T) {
	sink := &recordingSink{}
	up := scriptedResponder(t,
		toolCallTurn("c1", "probe", `{"n":1}`), // Exchange 1: one Turn onto the 50 rung, then Esc
		toolCallTurn("c2", "probe", `{"n":2}`), // Exchange 2: straight back onto the 50 rung
		contentTurn("second done"),
	)
	cfg := fillConfig(sink)
	cfg.Tools = domain.NewToolRegistry()
	if err := cfg.Tools.Register(sizedTool(8300, 8300)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "start"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("Step status = %q, want %q (a tool Turn keeps the Exchange open for the abort)", res.Status, domain.StatusTurnComplete)
	}
	if fired := noticeFirings(sink); len(fired) != 1 || !strings.HasPrefix(fired[0].Detail, "rung 50 (") {
		t.Fatalf("firings before the abort = %+v, want exactly one at rung 50", fired)
	}

	a.AbortExchange()
	if got := a.conv.Len(); got != 0 {
		t.Fatalf("after AbortExchange the conversation has %d messages, want 0", got)
	}

	runExchange(t, a, "again")

	fired := noticeFirings(sink)
	if len(fired) != 2 || !strings.HasPrefix(fired[1].Detail, "rung 50 (") {
		t.Fatalf("firings = %+v, want the 50 rung before the abort and exactly once again after it", fired)
	}
	msgs := toolMessages(a)
	if len(msgs) != 1 {
		t.Fatalf("conversation holds %d tool messages after the abort, want the one post-abort result", len(msgs))
	}
	span := noticeSpan(t, msgs[0])
	if !strings.Contains(msgs[0].Content, "context: 5") || span.Reaction != contextFillNoticeID {
		t.Errorf("post-abort tool message = %q, want the 50 rung's fact line as its trailer", msgs[0].Content)
	}
}

// A cancelled Turn's rollback (end()'s endCancelled row, turn.go) drops the Turn's committed tool
// results — including the one a notice rode on — the way AbortExchange does, and the Step-driven
// host re-attempts the Turn on resume: the ladder therefore ends its climb at the rollback
// (exchangeObserver.turnRolledBack → rearmFillNotice), so the re-attempt's first result back at 50–74%
// fires the 50 rung again, once, rather than staying silent until 75 over a result the model never
// saw.
func TestContextFillNoticeReArmsAfterACancelledTurnRollsBack(t *testing.T) {
	sink := &recordingSink{}
	started := make(chan struct{})
	up := scriptedResponder(t,
		// Turn 1: the probe result fires the 50 rung, then the blocking tool is cancelled and
		// the Turn rolls back over both.
		twoToolCallScript(toolReq{"c1", "probe", `{"n":1}`}, toolReq{"c2", "block", "{}"}),
		toolCallTurn("c3", "probe", `{"n":2}`), // the re-attempt: straight back onto the 50 rung
		contentTurn("done"),
	)
	cfg := fillConfig(sink)
	cfg.Tools = domain.NewToolRegistry()
	if err := cfg.Tools.Register(sizedTool(8300, 8300)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := cfg.Tools.Register(blockingTool{name: "block", started: started}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.Submit(domain.UserInput{Text: "start"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step status = %q, want %q", res.Status, domain.StatusCancelled)
	}
	if fired := noticeFirings(sink); len(fired) != 1 || !strings.HasPrefix(fired[0].Detail, "rung 50 (") {
		t.Fatalf("firings before the rollback = %+v, want exactly one at rung 50", fired)
	}
	if got := a.conv.Len(); got != 1 {
		t.Fatalf("after the rollback the conversation has %d messages, want 1 (the user input alone)", got)
	}
	if a.turns.fillRung != 0 {
		t.Fatalf("fillRung = %d after the rollback, want 0 — the ladder re-arms with the dropped result", a.turns.fillRung)
	}
	// The rollback a Step-driven host may reach again on resume: a second re-arm is a no-op.
	a.rearmFillNotice()
	if a.turns.fillRung != 0 {
		t.Fatalf("fillRung = %d after a second re-arm, want 0", a.turns.fillRung)
	}

	// Resume: the open Exchange re-attempts the Turn against the next script.
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run (resume): %v", err)
	}

	fired := noticeFirings(sink)
	if len(fired) != 2 || !strings.HasPrefix(fired[1].Detail, "rung 50 (") {
		t.Fatalf("firings = %+v, want the 50 rung before the rollback and exactly once again after it", fired)
	}
	msgs := toolMessages(a)
	if len(msgs) != 1 {
		t.Fatalf("conversation holds %d tool messages after the resume, want the one re-attempt result", len(msgs))
	}
	span := noticeSpan(t, msgs[0])
	if !strings.Contains(msgs[0].Content, "context: 5") || span.Reaction != contextFillNoticeID {
		t.Errorf("post-rollback tool message = %q, want the 50 rung's fact line as its trailer", msgs[0].Content)
	}
}

// The window in the line is the advertised one, and a working window with no advertised one is
// the only room anyone named, so it stands in; no window at all is silence — no notice, no
// firing, no ledger — rather than a percent of a guessed line.
func TestContextFillNoticeWindowAndSilence(t *testing.T) {
	cases := []struct {
		name       string
		window     int
		working    int
		body       int
		wantWindow string // empty: no notice at all
	}{
		{name: "advertised window", window: 8192, body: 8300, wantWindow: "8.2k"},
		// 32768 with no advertised window: History = 15,730 tokens, and 34,600 chars = 8,650 = 55%.
		{name: "working window with no advertised one", working: 32768, body: 34600, wantWindow: "32.8k"},
		{name: "no window is silence", body: 40000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := fillConfig(sink)
			cfg.Context.MaxContextTokens = tc.window
			cfg.Context.WorkingWindow = tc.working
			a := fillAgent(t, cfg)

			msg := adviseOneCall(t, a, strings.Repeat("x", tc.body))

			fired := noticeFirings(sink)
			if tc.wantWindow == "" {
				if len(fired) != 0 || len(msg.Advice) != 0 || len(msg.Content) != tc.body {
					t.Errorf("no window: firings %+v, spans %+v, content %d chars — want silence", fired, msg.Advice, len(msg.Content))
				}
				return
			}
			if len(fired) != 1 || !strings.HasPrefix(fired[0].Detail, "rung 50 (") {
				t.Fatalf("firings = %+v, want one at rung 50", fired)
			}
			noticeSpan(t, msg)
			if want := "of a " + tc.wantWindow + " window"; !strings.Contains(msg.Content, want) {
				t.Errorf("tool message = %q, want %q", msg.Content, want)
			}
		})
	}
}

// The 90 rung adds the wrap-up sentence for a child agent alone (ADR 0077 D4): a main agent's
// fold waits for the Exchange boundary and its wrap-up is the human's call.
func TestContextFillNoticeWrapUpIsForAChildAlone(t *testing.T) {
	for _, depth := range []int{0, 1} {
		t.Run(fmt.Sprintf("depth %d", depth), func(t *testing.T) {
			sink := &recordingSink{}
			a := fillAgent(t, fillConfig(sink))
			a.depth = depth

			msg := adviseOneCall(t, a, strings.Repeat("x", 15000)) // 15,009 chars = 3,753 tokens = 95%

			fired := noticeFirings(sink)
			if len(fired) != 1 || !strings.HasPrefix(fired[0].Detail, "rung 90 (") {
				t.Fatalf("firings = %+v, want one at rung 90", fired)
			}
			if got, want := strings.Contains(msg.Content, fillWrapUp), depth > 0; got != want {
				t.Errorf("wrap-up sentence present = %v at depth %d, want %v: %q", got, depth, want, msg.Content)
			}
		})
	}
}

// Off is absent, not silent: with the switch off the notice is no builtin at all, and under Bypass
// the armed notice is skipped with the rest of its class — either way a result past the line lands
// bare, with no firing.
func TestContextFillNoticeIsAbsentWhenOffAndSkippedUnderBypass(t *testing.T) {
	cases := []struct {
		name   string
		notice bool
		bypass bool
	}{
		{name: "switch off", notice: false},
		{name: "Bypass on", notice: true, bypass: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := fillConfig(sink)
			cfg.ContextFillNotice = tc.notice
			cfg.Bypass = tc.bypass
			a := fillAgent(t, cfg)

			if armed := builtinIDs(a); tc.notice != contains(armed, contextFillNoticeID) {
				t.Errorf("builtins = %v, want the notice armed = %v", armed, tc.notice)
			}
			const body = 15000
			msg := adviseOneCall(t, a, strings.Repeat("x", body))

			if fired := noticeFirings(sink); len(fired) != 0 || len(msg.Advice) != 0 || len(msg.Content) != body {
				t.Errorf("firings %+v, spans %+v, content %d chars — want the bare result", fired, msg.Advice, len(msg.Content))
			}
		})
	}
}

// contains reports whether ids holds id.
func contains(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

// The notice is advice, and advice is ephemeral: a snapshot taken after a firing resumes with the
// bare tool result and no ledger, so a replayed session never re-reads a stale percent as current.
func TestContextFillNoticeNeverSurvivesASnapshotResume(t *testing.T) {
	sink := &recordingSink{}
	a := fillAgent(t, fillConfig(sink))
	body := strings.Repeat("x", 8300)
	adviseOneCall(t, a, body)
	if len(noticeFirings(sink)) != 1 {
		t.Fatal("setup: the notice did not fire, so the resume would be untested")
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	restored, err := newAgent(fillConfig(&recordingSink{}), echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent (restore target): %v", err)
	}
	if err := restored.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	msg := restored.conv.At(restored.conv.Len() - 1)
	if msg.Content != body || len(msg.Advice) != 0 {
		t.Errorf("resumed tool message = %d chars with %d spans, want the bare body and no ledger", len(msg.Content), len(msg.Advice))
	}
}

// formatTokens keeps a small window's precision and drops a large one's noise.
func TestFormatTokens(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{999, "999"},
		{1000, "1.0k"},
		{8192, "8.2k"},
		{15400, "15.4k"},
		{32768, "32.8k"},
		{131072, "131k"},
		{999_499, "999k"},
		{1_300_000, "1.3M"},
		{20_000_000, "20.0M"},
	}

	for _, tc := range cases {
		if got := formatTokens(tc.n); got != tc.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
