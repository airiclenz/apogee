package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The event fold (phase-2 detail plan §4 P2.3; §3 C6)
// ----------------------------------------------------------------------------

// The transcript is proven by folding recorded event sequences — the shapes coreagent
// produces — and asserting the rendered scrollback. renderPlain renders at a fixed width,
// strips the ANSI styling, and trims each line's trailing padding so the assertions test the
// text, not the styling (ansiPattern lives in model_test.go). plainRender is the width-80
// default the substring assertions use.
func renderPlain(tr *transcript, width int) string {
	lines := tr.renderLines(newTheme(), width)
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ansiPattern.ReplaceAllString(ln, ""), " ")
	}
	return strings.Join(lines, "\n")
}

func plainRender(tr *transcript) string { return renderPlain(tr, 80) }

// feed folds a sequence of events into a fresh transcript and returns it.
func feed(events ...domain.Event) *transcript {
	tr := &transcript{}
	for _, e := range events {
		tr.apply(e)
	}
	return tr
}

// ----------------------------------------------------------------------------
// The recorded tool-Turn sequence (the C6 golden)
// ----------------------------------------------------------------------------

// A tool Turn streams pre-tool narration, calls a tool, returns a result, then a final
// no-tool Turn streams the answer and commits a MessageEvent — the canonical coreagent
// shape. The whole scrollback is asserted exactly.
func TestTranscriptToolTurnGolden(t *testing.T) {
	tr := &transcript{}
	tr.addUser("read main.go")
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Turn: 0}, Text: "Let me "})
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Turn: 0}, Text: "read it."})
	tr.apply(domain.ToolCallEvent{
		EventBase: domain.EventBase{Turn: 0},
		Call:      domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
	})
	tr.apply(domain.ToolResultEvent{
		EventBase: domain.EventBase{Turn: 0},
		Result:    domain.ToolResult{CallID: "c1", Content: "[File: main.go, 1 lines total, showing lines 1-1]\npackage main"},
	})
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Turn: 1}, Text: "It is "})
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Turn: 1}, Text: "a Go file."})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Turn: 1}, Text: "It is a Go file."})

	// (a) structured: the call and its result fold into one block, keyed by CallID, and the
	// result is summarised to a one-line detail (the read range) rather than the file body.
	var tool *entry
	for i := range tr.entries {
		if tr.entries[i].kind == entryToolCall {
			tool = &tr.entries[i]
		}
	}
	if tool == nil {
		t.Fatal("no tool-call entry recorded")
	}
	if !tool.done {
		t.Error("tool call not marked done after its result folded in")
	}
	if tool.tool.Label != "Read File" || tool.tool.Target != "main.go" || !tool.tool.bracket {
		t.Errorf("tool view = %+v; want a bracketed Read File / main.go header", tool.tool)
	}
	if len(tool.tool.Details) != 1 || tool.tool.Details[0].Text != "1 - 1" {
		t.Errorf("tool details = %+v; want a single \"1 - 1\" summary", tool.tool.Details)
	}

	// (b) render snapshot: the grouped block in the new look — ✦-prefixed, one blank line
	// between blocks, the tool detail hanging off a ┕ branch.
	want := strings.Join([]string{
		"❯ read main.go",
		"",
		"✦ Let me read it.",
		"",
		"✦ [Read File] main.go",
		"  ┕ 1 - 1",
		"",
		"✦ It is a Go file.",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("transcript mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if tr.turn != 1 {
		t.Errorf("turn = %d, want 1 (the latest Turn seen)", tr.turn)
	}
}

// ----------------------------------------------------------------------------
// Narration is finalised on the first ToolCall even with no MessageEvent
// ----------------------------------------------------------------------------

// A tool Turn emits no MessageEvent, so the first ToolCall must finalise the streamed
// pre-tool narration — otherwise the narration would be lost when the next Turn's tokens
// reuse the buffer.
func TestTranscriptToolCallFinalisesNarration(t *testing.T) {
	tr := feed(
		domain.TokenEvent{EventBase: domain.EventBase{Turn: 0}, Text: "Checking the file."},
		domain.ToolCallEvent{EventBase: domain.EventBase{Turn: 0}, Call: domain.ToolCall{Tool: "read_file"}},
	)
	if tr.streaming {
		t.Error("still streaming after the first ToolCall finalised the narration")
	}
	got := plainRender(tr)
	if !strings.Contains(got, "✦ Checking the file.") {
		t.Errorf("pre-tool narration not committed:\n%s", got)
	}
	if !strings.Contains(got, "✦ [Read File]") {
		t.Errorf("tool call not rendered:\n%s", got)
	}
	if n := len(tr.entries); n != 2 { // assistant narration + tool call
		t.Errorf("entries = %d, want 2 (narration + call)", n)
	}
}

// A Turn that streams no narration before its tool call commits no empty assistant entry,
// and a second ToolCall in the same Turn does not re-finalise.
func TestTranscriptToolCallNarrationEdges(t *testing.T) {
	t.Run("no narration commits nothing", func(t *testing.T) {
		tr := feed(domain.ToolCallEvent{Call: domain.ToolCall{Tool: "list_dir"}})
		if n := len(tr.entries); n != 1 { // just the tool call
			t.Errorf("entries = %d, want 1 (no empty narration entry)", n)
		}
	})

	t.Run("two calls in a Turn finalise once", func(t *testing.T) {
		tr := feed(
			domain.TokenEvent{Text: "narrate"},
			domain.ToolCallEvent{Call: domain.ToolCall{Tool: "a"}},
			domain.ToolCallEvent{Call: domain.ToolCall{Tool: "b"}},
		)
		if n := len(tr.entries); n != 3 { // narration + two calls, no second empty entry
			t.Errorf("entries = %d, want 3 (one narration + two calls)", n)
		}
		assistant := 0
		for _, e := range tr.entries {
			if e.kind == entryAssistant {
				assistant++
			}
		}
		if assistant != 1 {
			t.Errorf("assistant entries = %d, want 1 (narration finalised once)", assistant)
		}
	})
}

// ----------------------------------------------------------------------------
// StreamReset discards the superseded tokens
// ----------------------------------------------------------------------------

// A StreamResetEvent (an ActionRetry re-stream) discards the in-progress buffer; only the
// re-stream's accepted text is committed.
func TestTranscriptStreamResetDiscards(t *testing.T) {
	tr := feed(
		domain.TokenEvent{Text: "wrong answer"},
		domain.StreamResetEvent{},
		domain.TokenEvent{Text: "right "},
		domain.TokenEvent{Text: "answer"},
		domain.MessageEvent{Text: "right answer"},
	)
	got := plainRender(tr)
	if strings.Contains(got, "wrong answer") {
		t.Errorf("superseded tokens were not discarded:\n%s", got)
	}
	if !strings.Contains(got, "right answer") {
		t.Errorf("re-streamed answer missing:\n%s", got)
	}
	if n := len(tr.entries); n != 1 {
		t.Errorf("entries = %d, want 1 (only the accepted message)", n)
	}
}

// A reset with no in-progress buffer is a harmless no-op.
func TestTranscriptStreamResetWhenIdle(t *testing.T) {
	tr := feed(domain.StreamResetEvent{})
	if tr.streaming || len(tr.entries) != 0 {
		t.Errorf("idle reset mutated the transcript: streaming=%v entries=%d", tr.streaming, len(tr.entries))
	}
}

// ----------------------------------------------------------------------------
// MessageEvent text is canonical
// ----------------------------------------------------------------------------

// The MessageEvent text supersedes the streamed preview (they should reconcile to the same
// text; the canonical one wins).
func TestTranscriptMessageEventIsCanonical(t *testing.T) {
	tr := feed(
		domain.TokenEvent{Text: "draft"},
		domain.MessageEvent{Text: "final answer"},
	)
	got := plainRender(tr)
	if strings.Contains(got, "draft") {
		t.Errorf("superseded preview still shown:\n%s", got)
	}
	if !strings.Contains(got, "final answer") {
		t.Errorf("canonical text missing:\n%s", got)
	}
}

// An empty canonical MessageEvent falls back to the accumulated tokens so nothing streamed
// is lost.
func TestTranscriptMessageEventEmptyFallsBackToTokens(t *testing.T) {
	tr := feed(
		domain.TokenEvent{Text: "streamed only"},
		domain.MessageEvent{Text: ""},
	)
	if got := plainRender(tr); !strings.Contains(got, "streamed only") {
		t.Errorf("empty MessageEvent lost the streamed tokens:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// ErrorEvent renders inline and the transcript keeps going
// ----------------------------------------------------------------------------

// A recovered fault (ADR 0007) renders as an inline notice without stopping the stream; the
// following Turn still commits its message.
func TestTranscriptErrorEventInline(t *testing.T) {
	tr := feed(
		domain.TokenEvent{EventBase: domain.EventBase{Turn: 0}, Text: "I'll read it."},
		domain.ToolCallEvent{EventBase: domain.EventBase{Turn: 0}, Call: domain.ToolCall{Tool: "read_file"}},
		domain.ErrorEvent{EventBase: domain.EventBase{Turn: 0}, Source: "read_file", Err: "boom"},
		domain.TokenEvent{EventBase: domain.EventBase{Turn: 1}, Text: "Recovered."},
		domain.MessageEvent{EventBase: domain.EventBase{Turn: 1}, Text: "Recovered."},
	)
	got := plainRender(tr)
	for _, want := range []string{"I'll read it.", "read_file: boom", "Recovered."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// ----------------------------------------------------------------------------
// ToolResult: an error result is marked but still rendered as a result
// ----------------------------------------------------------------------------

func TestTranscriptToolResultError(t *testing.T) {
	tr := feed(domain.ToolResultEvent{Result: domain.ToolResult{Content: "no such file", IsError: true}})
	if got := plainRender(tr); !strings.Contains(got, "error: no such file") {
		t.Errorf("error tool result not surfaced:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// Approval is recorded observationally
// ----------------------------------------------------------------------------

func TestTranscriptApprovalRecorded(t *testing.T) {
	tr := feed(domain.ApprovalEvent{
		Request:  domain.ApprovalRequest{Tool: "write_file"},
		Decision: domain.ApprovalAllow,
	})
	if got := plainRender(tr); !strings.Contains(got, "approval allow: write_file") {
		t.Errorf("approval not recorded observationally:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// MechanismFired is gated behind the debug view
// ----------------------------------------------------------------------------

func TestTranscriptMechanismGatedByDebug(t *testing.T) {
	fired := domain.MechanismFiredEvent{Mechanism: "truncate_history", Hook: domain.HookHistoryRewrite, Action: "fired"}

	t.Run("off by default", func(t *testing.T) {
		tr := feed(fired)
		if n := len(tr.entries); n != 0 {
			t.Errorf("mechanism rendered without debug: entries = %d, want 0", n)
		}
	})

	t.Run("recorded under debug", func(t *testing.T) {
		tr := &transcript{debug: true}
		tr.apply(fired)
		if got := plainRender(tr); !strings.Contains(got, "mechanism truncate_history") {
			t.Errorf("mechanism not recorded under debug:\n%s", got)
		}
	})
}

// ----------------------------------------------------------------------------
// Tolerating sub-agent depth (Phase 3)
// ----------------------------------------------------------------------------

// A Depth > 0 event indents its block — including continuation lines of a multi-line body —
// without crashing or corrupting the top-level layout.
func TestTranscriptDepthIndents(t *testing.T) {
	tr := feed(domain.ToolResultEvent{
		EventBase: domain.EventBase{Depth: 1},
		Result:    domain.ToolResult{Content: "line1\nline2"},
	})
	got := plainRender(tr)
	if !strings.HasPrefix(got, "  ✦ [result]") {
		t.Errorf("depth-1 entry not indented:\n%q", got)
	}
	if !strings.Contains(got, "    ┕ line2") {
		t.Errorf("continuation line of a depth-1 entry not indented:\n%q", got)
	}
}

// ----------------------------------------------------------------------------
// Tool call + result group by CallID
// ----------------------------------------------------------------------------

// A result folds into its call's block by CallID — even when results arrive out of order and
// the same tool is called twice in a Turn — so each call shows its own summary and no orphan
// result entry is appended.
func TestToolResultGroupsByCallID(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "a", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "b", Tool: "read_file", Arguments: []byte(`{"path":"b.go"}`)}})

	// The second call's result arrives first; it must fold into call b, not call a.
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "b", Content: "[File: b.go, 10 lines total, showing lines 1-10]\n…"}})

	if n := len(tr.entries); n != 2 {
		t.Fatalf("entries = %d, want 2 (the result folded in, no orphan entry)", n)
	}
	a, b := callEntry(tr, "a"), callEntry(tr, "b")
	if a == nil || b == nil {
		t.Fatal("a tool-call entry went missing")
	}
	if a.done {
		t.Error("call a folded a result it never received")
	}
	if !b.done {
		t.Fatal("call b's result did not fold into it")
	}
	if len(b.tool.Details) != 1 || b.tool.Details[0].Text != "1 - 10" {
		t.Errorf("call b details = %+v; want a single \"1 - 10\" summary", b.tool.Details)
	}

	// Call a's result arrives later and folds into a — still two entries, no orphan.
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "a", Content: "[File: a.go, 5 lines total, showing lines 1-5]\n…"}})
	if !callEntry(tr, "a").done {
		t.Error("call a's later result did not fold into it")
	}
	if n := len(tr.entries); n != 2 {
		t.Errorf("entries = %d after both results; want 2", n)
	}
}

// callEntry returns the tool-call entry with the given CallID, or nil.
func callEntry(tr *transcript, id string) *entry {
	for i := range tr.entries {
		if tr.entries[i].kind == entryToolCall && tr.entries[i].callID == id {
			return &tr.entries[i]
		}
	}
	return nil
}
