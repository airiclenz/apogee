package agent

// The advise slot (ADR 0076 D6): an advise Reaction's text reaches the model as a fenced trailer
// on the CLOSING TOOL RESULT — never in the request prefix, which a local server's prefix cache
// depends on — and every injection is recorded on the message it advised. These tests drive the
// two seams the slot is made of, the post-tool-result cascade (firePostToolResult) and the one
// commit point every tool result crosses (appendToolResult), because that pair is what a Driver's
// history, ledger and session record are all built from.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// adviseReaction is an engine-origin Go advise reaction: it hands text to the model at
// post-tool-result and does nothing else, which is the whole of what class advise may do.
func adviseReaction(id, text string) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginEngine,
		Class:  domain.ClassAdvise,
		On:     []domain.Moment{domain.MomentPostToolResult},
		Handler: domain.PostToolResultFunc(
			func(context.Context, domain.LoopView, domain.ToolCall, *domain.ToolResultEdit) (domain.Outcome, error) {
				return domain.Outcome{Inject: text}, nil
			}),
	}
}

// adviseAgent builds an Agent with the given advise reactions armed and Bypass as asked, plus one
// assistant message holding the tool call the results below answer.
func adviseAgent(t *testing.T, sink *recordingSink, bypass bool, reactions ...domain.Reaction) *Agent {
	t.Helper()

	cfg := configWithTools(sink)
	cfg.Reactions = reactions
	cfg.Bypass = bypass
	a, err := newAgent(cfg, echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}})
	return a
}

// adviseOneCall runs one finished tool call through the post-tool-result cascade and commits it,
// exactly as commitCall does, and returns the committed tool message.
func adviseOneCall(t *testing.T, a *Agent, body string) domain.Message {
	t.Helper()

	result := domain.ToolResult{CallID: "c1", Content: body}
	advised := a.firePostToolResult(context.Background(), domain.ToolCall{ID: "c1", Tool: "read_file"}, &result)
	a.appendToolResult(0, domain.ToolCall{}, "", result, "", advised)
	return a.conv.At(a.conv.Len() - 1)
}

// firedAdvice returns the ReactionFiredEvents the sink saw, in order.
func firedAdvice(sink *recordingSink) []domain.ReactionFiredEvent {
	var fired []domain.ReactionFiredEvent
	for _, e := range sink.events {
		if fe, ok := e.(domain.ReactionFiredEvent); ok {
			fired = append(fired, fe)
		}
	}
	return fired
}

// The slot's whole contract in one pass: the model sees the fence appended to the tool result it
// advised, the ledger records who injected it under what provenance, and the firing books the text
// itself so a bench can attribute an effect to the exact string the model read.
func TestAdviseSpanFencesTheClosingToolResult(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, false, adviseReaction("advisor", "run the tests before you answer"))

	const body = "[File: main.go]\npackage main"
	msg := adviseOneCall(t, a, body)

	want := domain.AdviceSpan{
		Reaction: "advisor",
		Origin:   domain.OriginEngine,
		Moment:   domain.MomentPostToolResult,
		Turn:     0,
		Offset:   len(body),
	}
	if len(msg.Advice) != 1 || msg.Advice[0] != want {
		t.Fatalf("ledger = %+v, want exactly %+v", msg.Advice, want)
	}
	if got := msg.Content; got != body+domain.RenderAdvice(want, "run the tests before you answer") {
		t.Errorf("advised content = %q, want the body followed by the rendered fence", got)
	}

	fired := firedAdvice(sink)
	if len(fired) != 1 {
		t.Fatalf("saw %d firings, want 1: %+v", len(fired), fired)
	}
	if fired[0].Action != "advise" || fired[0].Detail != "run the tests before you answer" {
		t.Errorf("firing = {Action:%q Detail:%q}, want {advise, the injected text}", fired[0].Action, fired[0].Detail)
	}
}

// Two advise reactions are two spans, in LADDER order, each pointing at its own fence: the ledger
// is what attributes an effect to one reaction rather than to whichever spoke last, so the offsets
// must survive a second injection landing after the first.
func TestAdviseSpansStackInLadderOrder(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, false, adviseReaction("first", "one"), adviseReaction("second", "two"))

	const body = "result"
	msg := adviseOneCall(t, a, body)

	if len(msg.Advice) != 2 {
		t.Fatalf("ledger holds %d spans, want 2: %+v", len(msg.Advice), msg.Advice)
	}
	if msg.Advice[0].Reaction != "first" || msg.Advice[1].Reaction != "second" {
		t.Errorf("ledger order = %q, %q, want the ladder's order (first, second)",
			msg.Advice[0].Reaction, msg.Advice[1].Reaction)
	}
	firstFence := domain.RenderAdvice(msg.Advice[0], "one")
	if msg.Advice[0].Offset != len(body) || msg.Advice[1].Offset != len(body)+len(firstFence) {
		t.Errorf("offsets = %d, %d, want %d, %d — each span starts where its own fence does",
			msg.Advice[0].Offset, msg.Advice[1].Offset, len(body), len(body)+len(firstFence))
	}
	if got := msg.Content; got != body+firstFence+domain.RenderAdvice(msg.Advice[1], "two") {
		t.Errorf("advised content = %q, want both fences appended in ladder order", got)
	}
}

// A handler that returns a book cannot spend the Turn's context on it: the cap is applied ONCE, in
// the dispatcher, so the fence and the firing's Detail carry the same truncated string.
func TestAdviseTextIsCappedBeforeItIsFenced(t *testing.T) {
	sink := &recordingSink{}
	huge := strings.Repeat("x", 9<<10)
	a := adviseAgent(t, sink, false, adviseReaction("verbose", huge))

	msg := adviseOneCall(t, a, "body")

	capped := domain.CapAdvice(huge)
	if !strings.Contains(msg.Content, capped) {
		t.Errorf("advised content does not carry the capped text (%d bytes)", len(capped))
	}
	if strings.Contains(msg.Content, huge) {
		t.Errorf("advised content carries the whole %d-byte text, want it capped at %d", len(huge), domain.AdviceCap)
	}
	if !strings.Contains(msg.Content, "[advice truncated at 8 KiB]") {
		t.Error("advised content carries no truncation marker, so the model cannot tell it was cut")
	}
	if fired := firedAdvice(sink); len(fired) != 1 || fired[0].Detail != capped {
		t.Errorf("firing Detail is not the capped text — a bench would hash a string the model never read")
	}
}

// Bypass switches advise OFF (D9), and a switched-off reaction is silent: no trailer for the model,
// no span in the ledger, and no firing to read back.
func TestAdviseIsSilentUnderBypass(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, true, adviseReaction("advisor", "guidance"))

	msg := adviseOneCall(t, a, "body")

	if msg.Content != "body" || len(msg.Advice) != 0 {
		t.Errorf("under Bypass the committed message = %q with %d spans, want the bare body and none",
			msg.Content, len(msg.Advice))
	}
	if fired := firedAdvice(sink); len(fired) != 0 {
		t.Errorf("under Bypass the cascade booked %+v, want no firing at all", fired)
	}
}

// The file-changed narrowing is the advise CLASS's rule, not the user command's: subscribes widens
// every advise reaction that listed file-changed onto the post-tool-result seam, so the narrowing
// that pays for it has to hold on the seam's own invoke path, whatever the handler's kind. A Go
// advise handler on file-changed hears a successful workspace write and nothing else — a read-only
// call never calls it, never spans and never books. The reaction is installed through the live
// lane's install half (installGeneration), the one arming route that takes a list as given: the
// arming step itself refuses this shape on both routes — domain.Reaction.Validate pins a Go handler
// to its own seam on Config.Reactions, and SetReactions refuses an engine-origin sync entry — so
// the dispatcher's guarantee is pinned here independently of either refusal.
func TestAdviseGoHandlerAtFileChangedIsNarrowed(t *testing.T) {
	sink := &recordingSink{}
	var reported []string
	ws := t.TempDir()
	a := adviseArgvAgent(t, sink, ws, false, &reported)

	calls := 0
	watcher := domain.Reaction{
		ID:     "watcher",
		Origin: domain.OriginEngine,
		Class:  domain.ClassAdvise,
		On:     []domain.Moment{domain.MomentFileChanged},
		Handler: domain.PostToolResultFunc(
			func(context.Context, domain.LoopView, domain.ToolCall, *domain.ToolResultEdit) (domain.Outcome, error) {
				calls++
				return domain.Outcome{Inject: "a file changed"}, nil
			}),
	}
	gen := a.Generation()
	gen.Sync = []domain.Reaction{watcher}
	a.installGeneration(gen)

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)
	if calls != 0 {
		t.Fatalf("a read-only call invoked the file-changed advise handler %d times, want never", calls)
	}
	if len(msg.Advice) != 0 || msg.Content != result.Content {
		t.Errorf("message = %q with %d spans, want the bare result", msg.Content, len(msg.Advice))
	}
	if fired := firedAdvice(sink); len(fired) != 0 {
		t.Errorf("firings = %+v, want none — a file-changed handler hears no read", fired)
	}

	write := domain.ToolCall{ID: "c2", Tool: "write_file", Arguments: []byte(`{"path":"notes.md","content":"hi"}`)}
	msg = adviseArgvCall(t, a, write, domain.ToolResult{CallID: "c2", Content: "wrote notes.md"})
	if calls != 1 {
		t.Fatalf("a successful write invoked the handler %d times, want once", calls)
	}
	if len(msg.Advice) != 1 || msg.Advice[0].Reaction != "watcher" {
		t.Errorf("ledger = %+v, want one span from watcher on the write", msg.Advice)
	}
}

// A delegation's result closes the same way a leaf call's does — commitCall is the one commit
// point every slot crosses, at every width — so a child's result carries the trailer too.
func TestAdviseFencesADelegationResult(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, false, adviseReaction("advisor", "check the child's claim"))

	slot := &dispatchSlot{
		call:   domain.ToolCall{ID: "c1", Tool: "delegate"},
		result: domain.ToolResult{CallID: "c1", Content: "the child reported back"},
	}
	a.commitCall(context.Background(), 0, slot)

	msg := a.conv.At(a.conv.Len() - 1)
	if len(msg.Advice) != 1 || msg.Advice[0].Reaction != "advisor" {
		t.Fatalf("delegation result carries ledger %+v, want one span from advisor", msg.Advice)
	}
	if !strings.HasSuffix(msg.Content, domain.RenderAdvice(msg.Advice[0], "check the child's claim")) {
		t.Errorf("delegation result = %q, want it to end with the rendered fence", msg.Content)
	}
}

// Advice is EPHEMERAL: the session record is written from the content before the first fence, so a
// resumed session carries neither the trailer nor its ledger. Without this a replay would re-read a
// stale snapshot as though the advising reaction had spoken again.
func TestAdviseNeverSurvivesASnapshotResume(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, false, adviseReaction("advisor", "guidance for this Turn only"))

	const body = "the tool's own output"
	adviseOneCall(t, a, body)

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	restored, err := newAgent(configWithTools(&recordingSink{}), echoResponder(t, "unused"))
	if err != nil {
		t.Fatalf("newAgent (restore target): %v", err)
	}
	if err := restored.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	msg := restored.conv.At(restored.conv.Len() - 1)
	if msg.Content != body || len(msg.Advice) != 0 {
		t.Errorf("resumed tool message = %q with %d spans, want the bare body and no ledger",
			msg.Content, len(msg.Advice))
	}
}

// userAdviseMCP is one user-origin `advise:` entry over an MCP tool, as the reactions: file resolves
// an `{mcp: {server:, tool:, args:, payload-arg:}}` mapping (adr-0076-amend-1029).
func userAdviseMCP(id string, on []domain.Moment, handler domain.MCPHandler) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   domain.ClassAdvise,
		On:      on,
		Handler: handler,
	}
}

// llamaStatus is the MCP tool the advise tests call: the llama-launcher status case, with a static
// argument and the seam document under "moment".
var llamaStatus = domain.MCPHandler{
	Server:     "llama",
	Tool:       "status",
	Args:       map[string]any{"verbose": true},
	PayloadArg: "moment",
}

// adviseMCPAgent is adviseArgvAgent with one MCP advise entry on post-tool-result and the given
// resolver injected as the Driver's MCP client — nil for a Driver that dials no MCP server.
func adviseMCPAgent(
	t *testing.T,
	sink *recordingSink,
	bypass bool,
	reported *[]string,
	resolve domain.MCPResolver,
) *Agent {
	t.Helper()

	a := adviseArgvAgent(t, sink, t.TempDir(), bypass, reported,
		userAdviseMCP("llama", []domain.Moment{domain.MomentPostToolResult}, llamaStatus))
	a.cfg.MCP = resolve
	return a
}

// An MCP advise entry's text answer is the trailer, exactly as a command's stdout is: fenced on the
// closing tool result, booked under the answer, and the tool is called with its static args plus
// the stamped post-tool-result document under its payload argument.
func TestAdviseMCPLandsAsATrailer(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	var reported []string
	stub := &stubMCP{answer: "qwen3 loaded\n"}
	a := adviseMCPAgent(t, sink, false, &reported, stub.resolver())

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	if !strings.HasPrefix(msg.Content, "main.go") || !strings.Contains(msg.Content, "qwen3 loaded") ||
		len(msg.Advice) != 1 || msg.Advice[0].Reaction != "llama" {
		t.Errorf("committed message = %q with spans %+v, want the body and one llama span carrying the answer",
			msg.Content, msg.Advice)
	}
	fired := firedAdvice(sink)
	if len(fired) != 1 || fired[0].Action != actionAdvise || fired[0].Detail != "qwen3 loaded\n" {
		t.Errorf("firings = %+v, want one advise firing booking the answer", fired)
	}
	if got := stub.resolutions(); len(got) != 1 || got[0] != (mcpResolution{server: "llama", tool: "status"}) {
		t.Errorf("resolutions = %+v, want one global resolution of llama/status", got)
	}
	calls := stub.calls()
	if len(calls) != 1 {
		t.Fatalf("the tool was called %d times, want 1", len(calls))
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal(calls[0], &args); err != nil {
		t.Fatalf("args are not a JSON object: %v (%s)", err, calls[0])
	}
	var doc domain.SeamPayload
	if err := json.Unmarshal(args["moment"], &doc); err != nil {
		t.Fatalf("payload under moment is not a SeamPayload object: %v (%s)", err, args["moment"])
	}
	if string(args["verbose"]) != "true" || doc.Event != domain.MomentPostToolResult || doc.Tool != "list_dir" ||
		doc.Reaction != "llama" {
		t.Errorf("args = %s, want verbose:true beside the stamped post-tool-result document of reaction llama", calls[0])
	}
	if len(reported) != 0 {
		t.Errorf("reported %q, want nothing", reported)
	}
}

// An empty answer is silence, as an empty stdout is: no span, no firing, no report.
func TestAdviseMCPAnswersNothingOnAnEmptyAnswer(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	var reported []string
	a := adviseMCPAgent(t, sink, false, &reported, (&stubMCP{}).resolver())

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	if msg.Content != "main.go" || len(msg.Advice) != 0 {
		t.Errorf("committed message = %q with %d spans, want the bare body and none", msg.Content, len(msg.Advice))
	}
	if fired := firedAdvice(sink); len(fired) != 0 || len(reported) != 0 {
		t.Errorf("firings = %+v, reported = %q, want neither", fired, reported)
	}
}

// The answer is redacted before it is fenced or booked: an MCP server is one more place a configured
// secret's value can come back from.
func TestAdviseMCPRedactsTheAnswer(t *testing.T) {
	// No t.Parallel: t.Setenv.
	t.Setenv("APOGEE_TEST_ADVISE_MCP_SECRET", "hunter2-the-real-one")

	sink := &recordingSink{}
	var reported []string
	a := adviseMCPAgent(t, sink, false, &reported, (&stubMCP{answer: "token=hunter2-the-real-one\n"}).resolver())
	a.cfg.SecretEnvVars = []string{"APOGEE_TEST_ADVISE_MCP_SECRET"}

	call, result := readCall()
	msg := adviseArgvCall(t, a, call, result)

	if strings.Contains(msg.Content, "hunter2-the-real-one") || !strings.Contains(msg.Content, "token=[redacted]") {
		t.Errorf("advised content = %q, want the secret's value replaced by [redacted]", msg.Content)
	}
	if fired := firedAdvice(sink); len(fired) != 1 || fired[0].Detail != "token=[redacted]\n" {
		t.Errorf("firings = %+v, want one advise firing whose Detail is already redacted", fired)
	}
}

// Bypass switches advise off (D9) before the handler is reached: the tool is never called.
func TestAdviseMCPIsSkippedUnderBypass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		bypass    bool
		wantCalls int
	}{
		{name: "Bypass off calls it", bypass: false, wantCalls: 1},
		{name: "Bypass on never calls it", bypass: true, wantCalls: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var reported []string
			stub := &stubMCP{answer: "advice\n"}
			a := adviseMCPAgent(t, &recordingSink{}, tc.bypass, &reported, stub.resolver())

			call, result := readCall()
			adviseArgvCall(t, a, call, result)

			if got := len(stub.calls()); got != tc.wantCalls {
				t.Errorf("the tool was called %d times, want %d", got, tc.wantCalls)
			}
		})
	}
}

// An MCP advise entry that cannot answer FAILS OPEN — the tool result is committed bare — and is
// reported on the sync lane's terms (reportSyncFailure): a Driver that dials no MCP server owns its
// own notice, so nothing is reported; a resolver's refusal is reported once per entry however often
// it fires; a call that fails is reported every time.
func TestAdviseMCPFailsOpen(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		stub        *stubMCP
		wantReports int
	}{
		{name: "no resolver", stub: nil, wantReports: 0},
		{
			name:        "a resolution failure",
			stub:        &stubMCP{resolveErr: errors.New(`mcp server "llama" is not connected`)},
			wantReports: 1,
		},
		{name: "a failed call", stub: &stubMCP{callErr: errors.New("model not loaded")}, wantReports: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sink := &recordingSink{}
			var reported []string
			var resolve domain.MCPResolver
			if tc.stub != nil {
				resolve = tc.stub.resolver()
			}
			a := adviseMCPAgent(t, sink, false, &reported, resolve)

			for range 2 {
				call, result := readCall()
				msg := adviseArgvCall(t, a, call, result)
				if msg.Content != "main.go" || len(msg.Advice) != 0 {
					t.Errorf("committed message = %q with %d spans, want the bare body and none",
						msg.Content, len(msg.Advice))
				}
			}

			if len(reported) != tc.wantReports {
				t.Fatalf("reported %q, want %d lines", reported, tc.wantReports)
			}
			for _, line := range reported {
				if !strings.HasPrefix(line, "reaction llama (post-tool-result): ") {
					t.Errorf("report = %q, want the sync lane's line naming the reaction", line)
				}
			}
			fired := firedAdvice(sink)
			if len(fired) != tc.wantReports {
				t.Errorf("firings = %+v, want %d failed firings", fired, tc.wantReports)
			}
			for _, f := range fired {
				if f.Action != actionFailed {
					t.Errorf("firing = %+v, want Action %q", f, actionFailed)
				}
			}
		})
	}
}
