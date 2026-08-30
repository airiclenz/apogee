package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// The event fold (phase-2 detail plan §4 P2.3; §3 C6)
// ----------------------------------------------------------------------------

// The transcript is proven by folding recorded event sequences — the shapes coreagent
// produces — and asserting the rendered scrollback. renderPlain renders at a fixed width,
// strips the ANSI styling, and trims each line's trailing padding so the assertions test the
// text, not the styling (ansiPattern lives in model_test.go). plainRender is the width-80
// default the substring assertions use.
//
// It also collapses a tool row's dotted leader to a SINGLE ⋯ (leaderRow, render.go). The run's
// length is pure geometry — it flexes to whatever the width less the target and the outcome leaves
// — so a golden carrying it verbatim would assert the terminal's width in every line and break on
// any change to a fixture's wording. The shape still shows: a golden reads
// `┕ main.go ⋯ 12 lines ▶`, which says target, leader, outcome slot and indicator in the order
// the spec draws them. The geometry itself — the dot floor, the target's truncation, the outcome
// printing whole — is pinned directly on the painter instead (TestLeaderRowSpendsItsRoomInOrder).
func renderPlain(tr *transcript, width int) string {
	lines := tr.renderLines(newTheme(scheme.Default()), width)
	for i, ln := range lines {
		lines[i] = strings.TrimRight(collapseLeader(ansiPattern.ReplaceAllString(ln, "")), " ")
	}
	return strings.Join(lines, "\n")
}

// leaderRun matches a painted leader — two or more of its dots — so collapsing it cannot touch a
// lone ⋯ a tool's own output happens to contain.
var leaderRun = regexp.MustCompile(glyphLeaderDot + "{2,}")

// collapseLeader reduces every dotted leader on a line to one dot. It is renderPlain's, and the
// paint-level tests deliberately do not use it: what they measure is the very geometry this drops.
func collapseLeader(line string) string {
	return leaderRun.ReplaceAllString(line, glyphLeaderDot)
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
	tr.addUser("read main.go", nil)
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Turn: 0}, Text: "Let me "})
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Turn: 0}, Text: "read it."})
	tr.apply(domain.ToolCallEvent{
		EventBase: domain.EventBase{Turn: 0},
		Call:      domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
	})
	tr.apply(domain.ToolResultEvent{
		EventBase: domain.EventBase{Turn: 0},
		Result: domain.ToolResult{CallID: "c1",
			Content: "[File: main.go, 1 lines total, showing lines 1-1]\npackage main",
			Summary: domain.ReadSpan{Start: 1, End: 1, Total: 1}},
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
	if tool.tool.Label != "Read" || tool.tool.Target != "main.go" {
		t.Errorf("tool view = %+v; want a Read / main.go header", tool.tool)
	}
	if tool.tool.Summary.Text != "1 line" || tool.tool.Details.len() != 0 {
		t.Errorf("tool outcome = %+v / %+v; want a \"1 line\" summary and no body", tool.tool.Summary, tool.tool.Details)
	}

	// (b) render snapshot: the grouped block in the new look — ✦-prefixed, one blank line
	// between blocks, the tool detail hanging off a ┕ branch.
	want := strings.Join([]string{
		"❯ read main.go",
		"",
		"✦ Let me read it.",
		"",
		"✦ Read",
		"  ┕ main.go ⋯ 1 line",
		"",
		"✦ It is a Go file.",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("transcript mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
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
	if !strings.Contains(got, "✦ Read") {
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
// Blank-line hygiene (tool-call layout item 2)
// ----------------------------------------------------------------------------

// Committed assistant text is trimmed of its leading and trailing blank lines, so the model's
// habitual trailing "\n\n" no longer stacks blank rows on top of the renderer's own one-line
// block separator. Each case pins the whole scrollback: exactly one empty line between blocks.
func TestTranscriptTrimsCommittedBlankLines(t *testing.T) {
	want := strings.Join([]string{
		"❯ ping",
		"",
		"✦ the answer",
	}, "\n")
	cases := []struct {
		name string
		text string
	}{
		{"no blank lines to trim", "the answer"},
		{"trailing newlines", "the answer\n\n\n"},
		{"leading newlines", "\n\nthe answer"},
		{"whitespace-only lines at both ends", "  \n\t\nthe answer\n   \n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tr.addUser("ping", nil)
			tr.apply(domain.MessageEvent{Text: tc.text})
			if got := plainRender(tr); got != want {
				t.Errorf("transcript mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// The interior of a committed message keeps its paragraph breaks, but a run of two or more
// blank lines collapses to one — a padded message never opens a three-row gap inside its block.
func TestTranscriptCollapsesInteriorBlankRun(t *testing.T) {
	tr := feed(domain.MessageEvent{Text: "first\n\n\n\nsecond"})
	want := strings.Join([]string{"✦ first", "", "  second"}, "\n")
	if got := plainRender(tr); got != want {
		t.Errorf("interior blank run not collapsed:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A table in an answer is framed like the rest of the message: the ✦ marker leads its first line
// and every following line hangs under it, so the columns stay in the body column (mdtable.go).
func TestTranscriptRendersMarkdownTable(t *testing.T) {
	tr := feed(domain.MessageEvent{Text: strings.Join([]string{
		"Counts:",
		"",
		"| Tool | Calls |",
		"| :-- | --: |",
		"| Read File | 12 |",
		"| Run | 3 |",
	}, "\n")})
	want := strings.Join([]string{
		"✦ Counts:",
		"",
		"  Tool      │ Calls",
		"  ──────────┼──────",
		"  Read File │    12",
		"  ──────────┼──────",
		"  Run       │     3",
	}, "\n")
	if got := plainRender(tr); got != want {
		t.Errorf("table block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A table wide enough to be fitted to the body column reaches that column's last cell on EVERY
// line — marker gutter included, and the continuation lines a wrapped cell adds along with it. This
// is the property the transcript's right-hand chrome is laid out against: the block is rendered to
// the same width the rest of the body wraps to, so the free column beside the scroll-bar gutter is
// the same beside a table row as beside the rule above it (model.go's transcriptWidth /
// bodyRightGutter, layout.md). At this width both the header's and one body row's middle cell wrap,
// which is exactly the case a filler line could break — and the rule between the two body rows is
// held to the body column like every other line.
func TestTranscriptTableFillsTheBodyColumn(t *testing.T) {
	const width = 60
	tr := feed(domain.MessageEvent{Text: strings.Join([]string{
		"| File | Description of the change that was made | Status |",
		"| --- | --- | --- |",
		"| internal/tui/mdtable.go | the parser and the renderer | done |",
		"| layout.md | spec the block | fail |",
	}, "\n")})

	lines := tr.renderLines(newTheme(scheme.Default()), width)

	if len(lines) != 7 {
		t.Fatalf("got %d lines, want 7 (a two-line header, the rule, a two-line row, the rule between the rows and a one-line row): %#v",
			len(lines), visible(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("table line %d is %d cells wide, want the full body column of %d: %q",
				i, w, width, strip(ln))
		}
	}
}

// The same trim applies to pre-tool narration finalised by the first ToolCall: exactly one
// empty line between the narration and the tool block it introduces.
func TestTranscriptTrimsNarrationBlankLines(t *testing.T) {
	tr := feed(
		domain.TokenEvent{Text: "\nReading it.\n\n\n"},
		domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}},
	)
	want := strings.Join([]string{
		"✦ Reading it.",
		"",
		"✦ Read",
		"  ┕ main.go ⋯",
	}, "\n")
	if got := plainRender(tr); got != want {
		t.Errorf("narration mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A message that is blank — empty, or nothing but whitespace and newlines — commits no entry
// at all: the bare ✦ marker line it used to leave behind is itself an unneeded line. The
// streamed-token fallback still applies when only the canonical text is blank.
func TestTranscriptBlankMessageCommitsNothing(t *testing.T) {
	t.Run("empty message, empty buffer", func(t *testing.T) {
		tr := feed(domain.MessageEvent{Text: ""})
		if n := len(tr.entries); n != 0 {
			t.Errorf("entries = %d, want 0 (nothing to show)", n)
		}
	})
	t.Run("whitespace-only message, empty buffer", func(t *testing.T) {
		tr := feed(domain.MessageEvent{Text: "\n \t\n\n"})
		if n := len(tr.entries); n != 0 {
			t.Errorf("entries = %d, want 0 (nothing to show)", n)
		}
	})
	t.Run("whitespace-only message keeps the streamed tokens", func(t *testing.T) {
		tr := feed(
			domain.TokenEvent{Text: "streamed only"},
			domain.MessageEvent{Text: "\n\n"},
		)
		if got := plainRender(tr); got != "✦ streamed only" {
			t.Errorf("render = %q; want the streamed tokens kept", got)
		}
	})
	t.Run("whitespace-only narration commits nothing", func(t *testing.T) {
		tr := feed(
			domain.TokenEvent{Text: "  \n\n"},
			domain.ToolCallEvent{Call: domain.ToolCall{Tool: "read_file"}},
		)
		if n := len(tr.entries); n != 1 { // the tool call alone
			t.Errorf("entries = %d, want 1 (the tool call, no blank narration)", n)
		}
	})
}

// The streaming preview drops the buffer's trailing blank lines for display only — the buffer
// keeps them, since a mid-stream "\n\n" may be a paragraph break about to be continued — while a
// just-opened empty buffer still renders its lone marker so the human sees streaming has begun.
func TestTranscriptStreamingPreviewTrimsTrailingBlanks(t *testing.T) {
	tr := &transcript{}
	tr.addUser("ping", nil)
	tr.apply(domain.TokenEvent{Text: "thinking\n\n"})
	want := strings.Join([]string{"❯ ping", "", "✦ thinking"}, "\n")
	if got := plainRender(tr); got != want {
		t.Errorf("preview mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if tr.pending.String() != "thinking\n\n" {
		t.Errorf("pending = %q; want the buffer itself untouched by the display trim", tr.pending.String())
	}

	empty := feed(domain.TokenEvent{Text: ""})
	if got := plainRender(empty); got != "✦" {
		t.Errorf("empty in-progress buffer = %q; want its lone ✦ marker line", got)
	}
}

// ----------------------------------------------------------------------------
// Terminal-escape hardening (item 8 — strip ESC from untrusted text)
// ----------------------------------------------------------------------------

// escOSC52 is an OSC 52 clipboard-write payload — the audit's verified vector.
const escOSC52 = "\x1b]52;c;cGFyaQ==\a"

// escCSI is a clear-screen + cursor-home CSI payload.
const escCSI = "\x1b[2J\x1b[H"

// entryDisplayStrings returns every string of an entry that can reach the terminal — the body, all
// five fields of a tool card, and the presentation entry's own facts. The
// assertions below walk THIS rather than e.text alone: the per-call-site discipline this test
// replaced passed while a tool card's target and summary carried live escapes, precisely because
// the assertion only looked at the text field.
func entryDisplayStrings(e entry) []string {
	out := []string{
		e.text,
		e.tool.Label, e.tool.Verb, e.tool.Target, e.tool.Summary.Text,
		e.presented.Title, e.presented.Path, e.presented.Location, e.presented.Reason,
		e.startup.Host, e.startup.Model,
		e.ctxModel, // the delegate's server-advertised id, painted beside the gauge's fill
	}
	for _, d := range e.tool.Details.all() {
		out = append(out, d.Text)
	}
	return out
}

// assertTranscriptNoESC fails when any entry field, or any rendered line, still carries an escape.
func assertTranscriptNoESC(t *testing.T, tr *transcript) {
	t.Helper()
	for i, e := range tr.entries {
		assertNoESCIn(t, fmt.Sprintf("entry %d", i), entryDisplayStrings(e)...)
	}
	for _, ln := range tr.renderLines(newTheme(scheme.Default()), 80) {
		if strings.Contains(ln, "\x1b]") { // the OSC introducer never survives to a rendered line
			t.Errorf("rendered line leaks an OSC escape introducer: %q", ln)
		}
	}
}

// assertNoESCIn fails when any of the given strings carries an escape — the non-transcript
// producers (the status line's activity label, a popup row's cells) that never become an entry.
func assertNoESCIn(t *testing.T, what string, strs ...string) {
	t.Helper()
	for _, s := range strs {
		if strings.ContainsRune(s, 0x1b) {
			t.Errorf("%s still carries an ESC byte: %q", what, s)
		}
	}
}

// Untrusted text is escape-stripped at the transcript SEAMS — addNote / addEphemeralNote /
// addError / addApproval, and presentToolCall / enrichWithResult for the tool card — so an OSC 52
// clipboard-write, a CSI screen game, or (the real threat) an unterminated OSC 8 opener can never
// reach the terminal, whichever producer worded the string. The producers are enumerated here
// because the enumeration is what failed before: stripping was applied per call site, and a
// tool-call target, a tool result, a recovered fault, the /skills catalogue, a resume note and a
// rebind note were all missed. The benign text around each payload must survive.
func TestTranscriptStripsTerminalEscapes(t *testing.T) {
	const osc52, csi = escOSC52, escCSI

	t.Run("streamed tokens (TokenEvent)", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.TokenEvent{Text: "stream " + osc52 + "tokens"})
		tr.apply(domain.MessageEvent{Text: ""}) // commit the streamed buffer verbatim
		assertTranscriptNoESC(t, tr)
		if got := plainRender(tr); !strings.Contains(got, "stream") || !strings.Contains(got, "tokens") {
			t.Errorf("stripping ate the benign token text:\n%s", got)
		}
	})

	t.Run("canonical message text (MessageEvent)", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.MessageEvent{Text: "final " + csi + "message"})
		assertTranscriptNoESC(t, tr)
		if got := plainRender(tr); !strings.Contains(got, "final") || !strings.Contains(got, "message") {
			t.Errorf("stripping ate the benign message text:\n%s", got)
		}
	})

	// An attached skill's display name was once a producer here too — it rode the send as a chip
	// label. It is not one any more: a sent block records its invocations as spans into the human's
	// own text (item 3 of the inline-accent plan), so no repo-supplied string reaches the transcript
	// on that path at all. Where a display name still IS shown — the /skills catalogue — it is
	// covered by its own case below.

	// The target is pulled verbatim out of the model's own JSON arguments — a hostile model's
	// cheapest reach to the screen, and foldActivity paints it before any gate runs.
	t.Run("tool-call target from the model's JSON arguments", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID:        "c1",
			Tool:      "read_file",
			Arguments: escapedArgs(t, "path", "ma"+osc52+"in.go"),
		}})
		assertTranscriptNoESC(t, tr)
		if got := tr.entries[0].tool.Target; !strings.Contains(got, "ma") || !strings.Contains(got, "in.go") {
			t.Errorf("stripping ate the benign target text: %q", got)
		}
	})

	// An unregistered (dynamic MCP) tool takes the raw-name fallback: the label, the verb and the
	// pretty-printed argument body are all the model's own bytes.
	t.Run("unknown tool label, verb and argument body", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID:        "c1",
			Tool:      "mcp" + csi + "_thing",
			Arguments: escapedArgs(t, "note", "bo"+osc52+"dy"),
		}})
		assertTranscriptNoESC(t, tr)
		if tr.entries[0].tool.Details.len() == 0 {
			t.Fatal("the unknown-tool fallback rendered no argument body to strip")
		}
	})

	// The summary and every detail line are built from result.Content — a file's first line or a
	// command's first output line, both of which a malicious repo owns.
	t.Run("tool-result summary and detail lines", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal",
			Arguments: escapedArgs(t, "command", "ls")}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID:  "c1",
			Content: "out" + osc52 + "put\nsecond" + csi + " line\nthird line",
		}})
		assertTranscriptNoESC(t, tr)
		if tr.entries[0].tool.Details.len() == 0 {
			t.Fatal("the multi-line output rendered no detail lines to strip")
		}
	})

	// An errored result is worded from the same untrusted content, one branch over.
	t.Run("errored tool result", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "read_file",
			Arguments: escapedArgs(t, "path", "main.go")}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "c1", IsError: true, Content: "no such fi" + osc52 + "le",
		}})
		assertTranscriptNoESC(t, tr)
	})

	// The orphan branch — a result matching no open call — appends the content as its own block
	// without passing enrichWithResult, so it strips at its own seam.
	t.Run("orphan tool result", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "nobody", Content: "orph" + osc52 + "aned",
		}})
		assertTranscriptNoESC(t, tr)
		if got := plainRender(tr); !strings.Contains(got, "orph") || !strings.Contains(got, "aned") {
			t.Errorf("stripping ate the benign orphan text:\n%s", got)
		}
	})

	// A recovered fault quotes what failed — a path, a command, an upstream body — and names the
	// model's own tool as the source.
	t.Run("recovered fault (ErrorEvent)", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ErrorEvent{Source: "read" + csi + "_file", Err: "bo" + osc52 + "om"})
		assertTranscriptNoESC(t, tr)
		if got := plainRender(tr); !strings.Contains(got, "bo") || !strings.Contains(got, "om") {
			t.Errorf("stripping ate the benign error text:\n%s", got)
		}
	})

	// An approval record names the tool the model asked for.
	t.Run("approval record", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ApprovalEvent{
			Request:  domain.ApprovalRequest{Tool: "writ" + osc52 + "e_file"},
			Decision: domain.ApprovalAllow,
		})
		assertTranscriptNoESC(t, tr)
	})

	// The /skills catalogue note is worded from repo-supplied SKILL.md front matter and from the
	// YAML error text of the files discovery refused.
	t.Run("the /skills catalogue note", func(t *testing.T) {
		tr := &transcript{}
		tr.addNote(skillCatalogNote(
			[]skills.Skill{{ID: "review", DisplayName: "Rev" + osc52 + "iew", Summary: "su" + csi + "mmary"}},
			[]skills.SkipError{{Path: "/lib/bad/SKILL.md", Err: errors.New("yaml: " + osc52 + "broken")}},
			"/home/me/.apogee", "/ws",
		))
		assertTranscriptNoESC(t, tr)
		if got := plainRender(tr); !strings.Contains(got, "iew") {
			t.Errorf("stripping ate the benign catalogue text:\n%s", got)
		}
	})

	// A resume note quotes a stored session title: untrusted DISK input, since no codec sanitizes a
	// record's Meta on the way back in.
	t.Run("resume notes", func(t *testing.T) {
		tr := &transcript{}
		tr.addEphemeralNote("resumed: " + "my " + osc52 + "session")
		tr.addEphemeralNote("resumed: " + "my " + csi + "session (no scrollback recorded)")
		assertTranscriptNoESC(t, tr)
		if got := plainRender(tr); !strings.Contains(got, "session") {
			t.Errorf("stripping ate the benign resume text:\n%s", got)
		}
	})

	// The rebind note names the model id the SERVER advertised.
	t.Run("the rebind note", func(t *testing.T) {
		tr := &transcript{}
		tr.addNote(rebindNote("", 0, "gpt"+osc52+"-oss-20b", 32000, false))
		tr.addNote(rebindNote("old-model", 8000, "new"+csi+"-model", 32000, false))
		assertTranscriptNoESC(t, tr)
		if got := plainRender(tr); !strings.Contains(got, "oss-20b") {
			t.Errorf("stripping ate the benign rebind text:\n%s", got)
		}
	})
}

// escapedArgs marshals one key/value pair the way a model emits it: the ESC byte travels as the
// JSON escape a model literally writes as backslash-u-001b, decoding back to the raw byte — which
// is exactly the shape that reaches a target extractor.
func escapedArgs(t *testing.T, key, value string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{key: value})
	if err != nil {
		t.Fatalf("marshal tool arguments: %v", err)
	}
	return raw
}

// The seam's sanitizer, pinned character by character. A control character in untrusted text is an
// instruction to the terminal rather than a character in the text — ESC opens an ANSI sequence, BEL
// rings the bell and closes an OSC 52 clipboard payload, CR rewinds the line so what follows
// overwrites what the reader already saw, and NUL or DEL takes string length while occupying no
// display cell — and stripping ESC alone left every one of the others to arrive intact. The two the
// renderer wraps and rails a body BY, the newline and the tab, are the class's only survivors.
func TestStripEscapesDropsControlCharacters(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain text passes through untouched", "just a note", "just a note"},
		{"ESC opens an ANSI sequence", "safe\x1b[31mred", "safe[31mred"},
		{"BEL rings the bell", "safe\x07text", "safetext"},
		{"CR rewinds the line", "shown\rhidden", "shownhidden"},
		{"CRLF leaves the newline behind", "first\r\nsecond", "first\nsecond"},
		{"an OSC 52 clipboard write is left inert", "safe " + escOSC52 + " text", "safe ]52;c;cGFyaQ== text"},
		{"a CSI screen game goes with it", "safe" + escCSI + "text", "safe[2J[Htext"},
		{"NUL, backspace and the rest of C0 go too", "a\x00b\x08c\x1fd", "abcd"},
		{"DEL goes with them", "a\x7fb", "ab"},
		{"the newline and the tab are the body's own", "para\n\nnext\tcolumn", "para\n\nnext\tcolumn"},
		{"non-ASCII text is not control text", "héllo — 世界 ✓", "héllo — 世界 ✓"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := stripEscapes(tc.in)
			if got != tc.want {
				t.Errorf("stripEscapes(%q) = %q; want %q", tc.in, got, tc.want)
			}
			for _, r := range got {
				if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f {
					t.Errorf("stripEscapes(%q) left %#U behind: %q", tc.in, r, got)
				}
			}
			if again := stripEscapes(got); again != got {
				t.Errorf("stripEscapes is not idempotent: %q became %q", got, again) // every seam may strip twice
			}
		})
	}
}

// The bidi half of the same sanitizer, pinned rune by rune. A bidirectional formatting character
// reorders the glyphs around it without touching a byte the executor reads, so on the DECISION
// surface it is the same hazard as the CR above: the row says one thing and the tool runs another,
// and flattening a field does nothing to it. The set is deliberately narrow — the bidi controls, not
// all of unicode.Cf — so the two survivors below are the point of the test as much as the casualties
// are: U+200D ZWJ holds an emoji sequence together and U+00AD is a soft hyphen, and a later
// "consistency" change to blanket-drop Cf must break a test rather than a person's prose.
func TestStripEscapesDropsBidiControls(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"RLO reorders the row it sits in", "run \u202esafe.sh", "run safe.sh"},
		{"LRO goes with it", "run \u202dsafe.sh", "run safe.sh"},
		{"the embeddings and their pop go too", "a\u202ab\u202bc\u202cd", "abcd"},
		{"the isolates go", "a\u2066b\u2067c\u2068d\u2069e", "abcde"},
		{"the marks go", "a\u200eb\u200fc", "abc"},
		{"a whole reversed tail is dropped, not reordered", "echo hello\u202edlrow", "echo hellodlrow"},
		{"ZWJ survives: it holds an emoji sequence together", "\U0001f469\u200d\U0001f4bb ok", "\U0001f469\u200d\U0001f4bb ok"},
		{"a soft hyphen survives: it is the user's own prose", "in\u00adcremental", "in\u00adcremental"},
		{"a zero-width space survives", "a\u200bb", "a\u200bb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := stripEscapes(tc.in)
			if got != tc.want {
				t.Errorf("stripEscapes(%q) = %q; want %q", tc.in, got, tc.want)
			}
			if strings.ContainsFunc(got, bidiControl) {
				t.Errorf("stripEscapes(%q) left a bidi control behind: %q", tc.in, got)
			}
			if again := stripEscapes(got); again != got {
				t.Errorf("stripEscapes is not idempotent: %q became %q", got, again)
			}
		})
	}
}

// The other half of the same seam, pinned character by character: what stripEscapes KEEPS because a
// wrapped body is railed by it, flattenField must FOLD because a one-row field is not a body. The
// newline is the visible half — a field that keeps one paints a row the pane did not author — and
// the tab is the half that hides: lipgloss measures it as a single cell while the terminal expands
// it to the next tab stop, so a field carrying one is laid out at a width it never draws at. The
// carriage return is the third: stripEscapes drops it, but the callers that hand this a model's own
// bytes unstripped do not, and a terminal reading one returns the cursor to column 0 and overwrites
// the row. One rune for one rune every time, so a later clip counts what the row will hold
// (clipRunes) — a "\r\n" is therefore two spaces, never one.
func TestFlattenFieldFoldsNewlinesAndTabs(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"a field with none of the three is returned unchanged", "read_file", "read_file"},
		{"a newline becomes the space it stood for", "read\nfile", "read file"},
		{"a tab becomes one too", "read\tfile", "read file"},
		{"a carriage return no sanitizer dropped becomes one as well", "read\rfile", "read file"},
		{"a CRLF is two runes, so it is two spaces", "read\r\nfile", "read  file"},
		{"both fold in the same pass", "read\tthe\nfile", "read the file"},
		{"all three fold in the same pass", "read\tthe\r\nfile", "read the  file"},
		{"each one is its own space, never collapsed", "a\t\t\nb", "a   b"},
		{"a forged row cannot survive the fold", "ok\n  Reason: run anything", "ok   Reason: run anything"},
		{"an ordinary space is left alone", "read file", "read file"},
		{"non-ASCII text is not layout", "héllo\t世界", "héllo 世界"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenField(tc.in)
			if got != tc.want {
				t.Errorf("flattenField(%q) = %q; want %q", tc.in, got, tc.want)
			}
			if strings.ContainsAny(got, "\n\t\r") {
				t.Errorf("flattenField(%q) left a break behind: %q", tc.in, got)
			}
			if in, out := len([]rune(tc.in)), len([]rune(got)); in != out {
				t.Errorf("flattenField(%q) is %d runes wide; the field was %d", tc.in, out, in)
			}
			if again := flattenField(got); again != got {
				t.Errorf("flattenField is not idempotent: %q became %q", got, again)
			}
		})
	}
}

// The seam in the surface it exists for: an approval pane whose tool ARGUMENT carries a
// right-to-left override. The bytes the executor will run are unchanged by it, so an unstripped
// pane would draw the command in an order the shell never sees and the human would approve a line
// that does not exist. The pane draws the argument's own order or the argument does not reach it.
func TestModelApprovalStripsBidiOverrideFromArguments(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	// The override arrives the way a model really writes one: a \u escape in the call's JSON, decoded
	// into the rune before any TUI code sees it.
	req := domain.ApprovalRequest{
		Tool:      "terminal",
		Reason:    "run a command",
		Arguments: json.RawMessage(`{"command":"echo hello\u202edlrow"}`),
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	got := ansiPattern.ReplaceAllString(m.approvalPrompt(req), "")
	if strings.ContainsFunc(got, bidiControl) {
		t.Errorf("a bidi control reached the approval pane:\n%q", got)
	}
	if !strings.Contains(got, "echo hellodlrow") {
		t.Errorf("the argument did not reach the pane in its own order:\n%s", got)
	}
}

// The status line's verb (toolActivityVerb) is built only from presentToolCall's view, so it inherits
// the tool card's seam rather than re-deriving it — as does the target the same view carries, pinned
// beside it here because it is the other half a live surface could word. They are pinned because
// foldActivity paints the verb the moment a call is ANNOUNCED — before any approval gate runs — which
// makes it the earliest point a hostile model's argument reaches the screen.
func TestToolActivityVerbCarriesNoEscape(t *testing.T) {
	call := domain.ToolCall{
		Tool:      "terminal",
		Arguments: escapedArgs(t, "command", "npm "+escOSC52+"test"),
	}
	verb := toolActivityVerb(call, workspaceRoot{})
	assertNoESCIn(t, "the activity verb", verb)
	if !strings.Contains(verb, "running") {
		t.Errorf("stripping ate the benign verb text: %q", verb)
	}

	tv := presentToolCall(call, "", workspaceRoot{})
	assertNoESCIn(t, "the presented target", tv.Target)
	if !strings.Contains(tv.Target, "npm ") {
		t.Errorf("stripping ate the benign target text: %q", tv.Target)
	}

	unknown := toolActivityVerb(domain.ToolCall{Tool: "mcp" + escCSI + "_thing"}, workspaceRoot{})
	assertNoESCIn(t, "the unregistered-tool activity verb", unknown)
}

// Every cell the autocomplete overlay builds is escape-stripped where the row is built, exactly as
// the session browser and the launcher pickers strip theirs: the popup module strips nothing and
// truncates ANSI-preservingly, and an ESC byte takes string length but no display cell, so an
// unstripped cell both reaches the terminal live and lies to the column math. Skill rows come from
// repo-supplied SKILL.md front matter; file rows come from workspace filenames.
func TestAutocompleteRowsStripEscapes(t *testing.T) {
	m := Model{opts: Options{
		Workspace: "/ws",
		Skills: fakeSkillCatalog{skills: []skills.Skill{{
			ID:          "rev" + escCSI + "iew",
			DisplayName: "Rev" + escOSC52 + "iew",
			Summary:     "review a" + escOSC52 + " diff",
		}}},
	}}
	m.files = &fileCache{
		root:    "/ws",
		files:   []string{"docs/no" + escOSC52 + "tes.md"},
		expires: time.Now().Add(time.Hour),
	}

	for _, item := range m.skillSuggestions("", "") {
		assertNoESCIn(t, "a skill suggestion row", item.cells...)
	}
	for _, item := range m.slashSuggestions("rev", "") {
		assertNoESCIn(t, "a merged \"/\" menu row", item.cells...)
	}
	items := m.fileSuggestions("")
	if len(items) == 0 {
		t.Fatal("the seeded file cache produced no rows to strip")
	}
	for _, item := range items {
		assertNoESCIn(t, "an \"@\" file row", item.cells...)
		assertNoESCIn(t, "an \"@\" file row's spliced value", item.value)
	}
}

// A workspace filename can carry a line break or a tab, and neither may reach the dropdown: a "\n"
// left in the row's cell paints rows the pane never counted (popupRowBlocks composes ONE line per
// row and the frame is split on "\n" from there), and lands in the composer as a real second line
// the "@"-ref scanner cuts at; a "\t" is expanded to spaces by the popup and again by the textarea
// on insert, so the row would show one thing and splice another and autocompleteExactMatch could
// never match a fully-typed token. fileSuggestions flattens the path the way skillRow flattens its
// cells, once, before the cell and the value are both derived from it — so the hostile listing
// renders the frame its flattened, benign twin renders, byte for byte.
func TestFileRowsFlattenLineAndTabBreaks(t *testing.T) {
	const draft = "read @docs/"

	openOn := func(t *testing.T, path string) Model {
		t.Helper()
		m := newTestModel(t)
		m.opts.Workspace = "/ws"
		m.files = &fileCache{
			root:    "/ws",
			files:   []string{path},
			expires: time.Now().Add(time.Hour),
		}
		m.input.SetValue(draft)
		m.autocomplete = m.computeAutocomplete(len(draft))
		if !m.autocomplete.active || m.autocomplete.kind != acFile || len(m.autocomplete.items) != 1 {
			t.Fatalf("the \"@\" overlay did not open on the seeded listing %q: %+v", path, m.autocomplete)
		}
		return m
	}

	for _, tc := range []struct{ name, hostile, flattened string }{
		{"a line break in the name", "docs/no\ntes.md", "docs/no tes.md"},
		{"a tab in the name", "docs/a\tb.md", "docs/a b.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := openOn(t, tc.hostile)
			row := m.autocomplete.items[0]
			for _, cell := range row.cells {
				if strings.ContainsAny(cell, "\n\t") {
					t.Errorf("the file row's cell %q still carries a line or tab break", cell)
				}
			}
			if strings.ContainsAny(row.value, "\n\t") {
				t.Errorf("the file row's spliced value %q still carries a line or tab break", row.value)
			}
			if row.value != tc.flattened {
				t.Errorf("the file row's value = %q, want the flattened path %q", row.value, tc.flattened)
			}

			got := m.renderAutocomplete()
			painted := 0
			for _, ln := range popupLines(got) {
				if strings.Contains(strip(ln), "docs/") {
					painted++
				}
			}
			if painted != 1 {
				t.Errorf("one seeded name painted %d dropdown rows, want exactly one:\n%s", painted, got)
			}
			if want := openOn(t, tc.flattened).renderAutocomplete(); got != want {
				t.Errorf("the dropdown over %q rendered\n%s\nwant the frame its flattened twin renders\n%s", tc.hostile, got, want)
			}

			next, _ := m.acceptAutocomplete()
			composed := next.(Model).input.Value()
			if want := "read " + fileRefToken(row.value) + " "; composed != want {
				t.Errorf("composer after accept = %q, want %q (the row's own value, verbatim)", composed, want)
			}
		})
	}
}

// A row's VALUE is the overlay's second door, because an autocomplete row is not only SHOWN, it is
// SPLICED: accepting an "@" row writes it into the composer, which inputView paints. That door is
// currently held shut by the bubbles textarea, whose every insertion path (SetValue → InsertString →
// insertRunesFromUserInput) runs an internal runeutil.Sanitizer that drops control runes — a
// third-party internal with no compatibility promise, not this package's own seam. So the property
// is pinned from both sides: nothing carrying an ESC reaches the box, AND what lands there is
// EXACTLY the row's own value — fileRefToken's contract, "a row shows exactly what accepting it will
// insert". A raw value quietly broke the second half, and with it a user-visible behaviour: the
// sanitized box could never equal the row it came from, so autocompleteExactMatch failed on a
// fully-typed token and ⏎ re-accepted instead of submitting.
//
// The escape payload here is the CSI one, whose bytes are all printable once the ESCs are gone, so
// the composer can be compared verbatim — where escOSC52's trailing BEL would leave the two sides to
// be compared across two sanitizers that agree on it only by coincidence. The TAB seed is the same
// property from the other side: nothing drops a tab, both the popup and the textarea EXPAND one, and
// they expand it to different widths, so a row holding a raw "\t" could never equal what accepting
// it inserts.
func TestAcceptedFileRowMatchesItsValue(t *testing.T) {
	for _, tc := range []struct{ name, seed, draft string }{
		{"an ESC byte in the name", "docs/no" + escCSI + "tes.md", "read @docs/no"},
		{"a tab in the name", "docs/a\tb.md", "read @docs/a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			m.opts.Workspace = "/ws"
			m.files = &fileCache{
				root:    "/ws",
				files:   []string{tc.seed},
				expires: time.Now().Add(time.Hour),
			}

			m.input.SetValue(tc.draft)
			m.autocomplete = m.computeAutocomplete(len(tc.draft))
			if !m.autocomplete.active || m.autocomplete.kind != acFile || len(m.autocomplete.items) != 1 {
				t.Fatalf("the \"@\" overlay did not open on the seeded listing: %+v", m.autocomplete)
			}
			row := m.autocomplete.items[0]

			next, _ := m.acceptAutocomplete()
			got := next.(Model).input.Value()
			assertNoESCIn(t, "the composer after accepting an \"@\" row", got)
			if want := "read " + fileRefToken(row.value) + " "; got != want {
				t.Errorf("composer after accept = %q, want %q (the row's own value, verbatim)", got, want)
			}
		})
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
// Rendering sub-agent depth (Phase 3, P3.14 — "tolerate" → "render")
// ----------------------------------------------------------------------------

// A Depth > 0 event renders as a framed sub-agent block: every line — header and the continuation
// lines of a multi-line body — is prefixed by the │ rail gutter, without crashing or corrupting the
// top-level layout. The rail is the WHOLE frame now: the label that used to announce the descent
// is gone, and what opens a run is its own delegation header (docs/layout/tool-layout.md).
func TestTranscriptDepthRendersFramedBlock(t *testing.T) {
	tr := feed(domain.ToolResultEvent{
		EventBase: domain.EventBase{Depth: 1},
		Result:    domain.ToolResult{Content: "line1\nline2"},
	})
	// The stray result collapses like every other block now, so its second line is behind the cap;
	// open it, because what this test is about is the rail reaching a body's continuation lines.
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the stray result to expand")
	}
	got := plainRender(tr)
	if !strings.Contains(got, "│ ✦ result") {
		t.Errorf("depth-1 entry not framed by the rail:\n%q", got)
	}
	if !strings.Contains(got, "│   ┕ line2") {
		t.Errorf("continuation line of a depth-1 entry not framed by the rail:\n%q", got)
	}
}

// A delegated presentation is committed with its own run's identity, so it lands INSIDE that run's
// stretch — under the head that spawned it, with a sibling fan-out already running — and the run's
// span still reaches past it. Appending it at the tail as a depth-0 entry, which is what it did
// before it carried a run, dropped a top-level block into the middle of the OTHER run and ended
// [subAgentSpan] where it landed: one run then read as two railed stretches with an unframed gap.
func TestPresentedEntryLandsInsideItsOwnRun(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)
	childCall(tr, "s1", "a1", "alpha.go")
	childCall(tr, "s2", "b1", "beta.md")

	tr.addPresented(presentedMsg{
		Path:        "docs/review.html",
		Method:      domain.PresentShown,
		Depth:       1,
		SpawnCallID: "s1",
	})

	head := headIndex(t, tr, "s1")
	span := subAgentSpan(tr.entries, head)
	if span != 2 {
		t.Fatalf("run s1 spans %d entries, want 2 (its child call and its presentation):\n%s",
			span, plainRender(tr))
	}
	shown := tr.entries[head+span]
	if shown.kind != entryPresented {
		t.Fatalf("entry %d is %v, want the presentation at the end of s1's stretch", head+span, shown.kind)
	}
	if shown.depth != 1 || shown.spawnCallID != "s1" {
		t.Errorf("presentation committed at depth %d under run %q, want depth 1 under s1",
			shown.depth, shown.spawnCallID)
	}
	if sibling := headIndex(t, tr, "s2"); subAgentSpan(tr.entries, sibling) != 1 {
		t.Errorf("run s2 spans %d entries, want its own 1 — the presentation belongs to s1:\n%s",
			subAgentSpan(tr.entries, sibling), plainRender(tr))
	}
}

// A host note landing while a delegation is still drawing stays OUTSIDE that run. The note answers
// the HUMAN — it carries no delegate identity and is never railed into one (contrast a presented
// document, TestPresentedEntryLandsInsideItsOwnRun) — so what the child commits afterwards slides in
// FRONT of it: the head's span still covers the whole run, and the note stands after the work it
// interrupted rather than cutting it in two.
func TestHostNoteLandingMidRunStaysOutsideTheRun(t *testing.T) {
	t.Parallel()

	// A delegated entry that carries its run's id is placed by runEnd, which already stops at the
	// note; this pins that it keeps doing so.
	t.Run("a delegated entry carrying its run's id", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		childCall(tr, "s1", "a1", "alpha.go")
		tr.addNote("cancelled")
		childCall(tr, "s1", "a2", "beta.go")

		assertRunSpansPastTheNote(t, tr, 2)
	})

	// A delegated entry carrying NO call id — a serial session's child, a replayed record — has no
	// run to be placed into, so before the trailing-note rule it landed behind the note: outside the
	// span, and so outside a collapsed run's elision.
	t.Run("a delegated entry carrying no call id", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		childCall(tr, "s1", "a1", "alpha.go")
		tr.addNote("cancelled")
		tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "child answer"})

		assertRunSpansPastTheNote(t, tr, 2)
	})

	// An ephemeral note is addNote in every respect the renderer can observe, placement included.
	t.Run("an ephemeral note", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		childCall(tr, "s1", "a1", "alpha.go")
		tr.addEphemeralNote("resumed: yesterday's session")
		childCall(tr, "s1", "a2", "beta.go")

		assertRunSpansPastTheNote(t, tr, 2)
	})

	// A Firing block is the same defect class: a depth-0 host block appended at the tail.
	t.Run("a firing block", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		childCall(tr, "s1", "a1", "alpha.go")
		tr.addFiring(schedule.Event{
			Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
			Prompt: "check the log",
		})
		childCall(tr, "s1", "a2", "beta.go")

		head := headIndex(t, tr, "s1")
		if span := subAgentSpan(tr.entries, head); span != 2 {
			t.Fatalf("run s1 spans %d entries, want 2 — the firing block cut the run short:\n%s",
				span, plainRender(tr))
		}
		if last := tr.entries[len(tr.entries)-1]; last.kind != entrySchedule {
			t.Errorf("the last entry is %v, want the firing block after the run:\n%s",
				last.kind, plainRender(tr))
		}
	})
}

// assertRunSpansPastTheNote checks the one invariant every host-note case shares: run s1's head
// spans want entries, and the note it interrupted stands after them, at depth 0 and last.
func assertRunSpansPastTheNote(t *testing.T, tr *transcript, want int) {
	t.Helper()

	head := headIndex(t, tr, "s1")
	if span := subAgentSpan(tr.entries, head); span != want {
		t.Fatalf("run s1 spans %d entries, want %d — the note cut the run short:\n%s",
			span, want, plainRender(tr))
	}
	last := tr.entries[len(tr.entries)-1]
	if last.kind != entryNote || last.depth != 0 {
		t.Errorf("the last entry is kind %v at depth %d, want the note after the run at depth 0:\n%s",
			last.kind, last.depth, plainRender(tr))
	}
}

// A nested event sequence (Depth 0 → 1 → 0) renders the sub-agent block framed, and the frame simply
// ENDS where it climbs back out — nothing of a group follows it, so the separator is the flat one
// rather than a ┊ (docs/layout/tool-layout.md, "Grouped Sub-agents") — while the parent stream stays
// intact and unframed (the P3.14 acceptance golden, re-pinned to the frame the spec draws).
func TestTranscriptDepthNestedSequenceGolden(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 0}, Text: "delegating"})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "child work"})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 0}, Text: "back to parent"})

	want := strings.Join([]string{
		"✦ delegating",
		"", // the descent joins at depth 0: nothing announces it, so the spacer is the flat one
		"│ ✦ child work",
		"", // the climb-out ends the frame, and no grouped member follows it to be parted from
		"✦ back to parent",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("nested-depth transcript mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// One rail gutter per level frames a 0→1→2 climb, and nothing else marks the descent: the label
// that used to open each level is gone, so the depth a block stands at is said by the gutters alone.
func TestTranscriptDepthFramesEachLevel(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "child"})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "grandchild"})

	want := strings.Join([]string{
		"│ ✦ child",
		"│", // the 1→2 descent joins at depth 1: the outer rail alone
		"│ │ ✦ grandchild",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("multi-level transcript mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// ----------------------------------------------------------------------------
// A sub-agent run collapses to its call block (layout.md, "Collapsed and expanded blocks")
// ----------------------------------------------------------------------------

// subAgentCall folds the sub_agent call that HEADS a run at depth. Everything folded after it at a
// deeper depth is that run's span, until the stream climbs back out to depth or shallower.
func subAgentCall(tr *transcript, id, task string, depth int) {
	tr.apply(domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: depth},
		Call:      domain.ToolCall{ID: id, Tool: "sub_agent", Arguments: []byte(`{"task":"` + task + `"}`)},
	})
}

// subAgentStarted marks a delegation as RUNNING — the phase a pool worker emits the instant it
// dequeues the job (domain.SubAgentPhaseEvent), stamped with the CHILD's own depth. A delegation
// built without one and with nothing behind it is one still QUEUED behind the Parallel agents cap,
// which is what its row then says (subAgentScheduled).
func subAgentStarted(tr *transcript, id string, depth int) {
	tr.apply(domain.SubAgentPhaseEvent{
		EventBase: domain.EventBase{Depth: depth, CallID: id},
		Phase:     domain.SubAgentStarted,
	})
}

// subAgentReport folds a run's report back into its head, which is what ends the run: the head is
// done from there on, and its collapsed summary switches from the live tempo to the report's gist.
// A run built without one is a run still working.
func subAgentReport(tr *transcript, id, content string, depth int) {
	tr.apply(domain.ToolResultEvent{
		EventBase: domain.EventBase{Depth: depth},
		Result:    domain.ToolResult{CallID: id, Content: content},
	})
}

// runCall folds a terminal call and its multi-line output at depth — an inner block that carries a
// body, so an expanded run demonstrably paints its inner blocks in THEIR own states.
func runCall(tr *transcript, id, command, output string, depth int) {
	base := domain.EventBase{Depth: depth}
	tr.apply(domain.ToolCallEvent{EventBase: base,
		Call: domain.ToolCall{ID: id, Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
	tr.apply(domain.ToolResultEvent{EventBase: base,
		Result: domain.ToolResult{CallID: id, Content: output}})
}

// TestSubAgentRunCollapsesToItsCallBlock is the item's acceptance golden, and under ADR 0063 it has
// one direction only. The whole run is ONE block reading as ONE summarised line: the rail, the inner
// blocks and every spacer among them are gone, the report body is gone with them, and the head's
// summary slot carries the cascading count and gist. The head has reported, so its name carries the
// done ✓ (design call 6).
//
// There is no second shape to toggle into. Expanding a run opens its VIEW (runview.go), so the fold
// is refused here and the row does not move — which is also what closed the double print: the body
// this head laid out open WAS the report, unformatted, above the formatted copy the run's own last
// assistant row already carried (ISSUES.md, 2026-08-30).
func TestSubAgentRunCollapsesToItsCallBlock(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	readCall(tr, "c1", "a.go", 1, 5, 1)
	runCall(tr, "c2", "go test", "ok   a\nok   b\nPASS", 1)
	subAgentUsage(tr, 1, 12000, 32768)
	subAgentReport(tr, "s1", "Found 4 gaps\nin the suite\nhere they are", 0)

	collapsed := strings.Join([]string{
		"✦ Sub-Agent",
		groupMemberLine("  ┕ survey the tests ✓ ⋯ 2 tool calls · 12k/32k · done"),
	}, "\n")
	if got := renderPlain(tr, 80); got != collapsed {
		t.Errorf("collapsed run mismatch (collapsed is the default):\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
	}
	if strings.Contains(collapsed, glyphSubRail) {
		t.Errorf("the collapsed run kept a rail; its span is elided whole:\n%s", collapsed)
	}

	if tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = true; a run opens as a view, never as a rail in place")
	}
	got := renderPlain(tr, 80)
	if got != collapsed {
		t.Errorf("the run moved under a refused expand:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
	}
	// The report's own lines are the ones that used to be printed twice; only its FIRST reaches the
	// row, and here not even that (the report is long enough that the slot says the typed word).
	for _, line := range []string{"in the suite", "here they are"} {
		if strings.Contains(got, line) {
			t.Errorf("the run printed the report line %q above its own span:\n%s", line, got)
		}
	}
}

// TestCollapsedRunSaysItsGistOnce pins the no-repetition rule: a collapsed run's head reads as ONE
// summarised line, so the report's first line — which the summary slot carries — is never painted a
// second time as the block's compact body. The rule holds whichever half of the outcome the report's
// own size filled: a long report that became a body used to say its first line twice in adjacent
// rows, and a one-line report that became a summary has no body to repeat it with.
//
// The fixture carries a context reading, so the two-row count also pins that the fill RIDES that one
// summarised line rather than adding a row of its own.
func TestCollapsedRunSaysItsGistOnce(t *testing.T) {
	const gist = "Found 4 gaps"
	cases := []struct {
		name   string
		report string
		// wantGist is how many painted lines may carry the gist. A one-line report is PROMOTED
		// into the slot and so appears exactly once; a report long enough to be a body is
		// summarised by the ratified table's verdict instead ("done"), and its own words wait
		// behind the fold — which is the same claim from the other side: never twice.
		wantGist int
	}{
		{name: "a long report, which the outcome kept as a body", report: gist + "\nin the suite\nhere they are", wantGist: 0},
		{name: "a one-line report, which the outcome kept as a summary", report: gist, wantGist: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			subAgentCall(tr, "s1", "survey the tests", 0)
			readCall(tr, "c1", "a.go", 1, 5, 1)
			subAgentUsage(tr, 1, 12000, 32768)
			subAgentReport(tr, "s1", tc.report, 0)

			painted := renderPlain(tr, 80)

			lines := strings.Split(painted, "\n")
			hits := 0
			for _, ln := range lines {
				if strings.Contains(ln, gist) {
					hits++
				}
			}
			if hits != tc.wantGist {
				t.Errorf("the gist %q appears on %d lines; want %d:\n%s", gist, hits, tc.wantGist, painted)
			}
			if len(lines) != 2 {
				t.Errorf("collapsed run = %d lines; want 2 (the header and its one summarised line):\n%s", len(lines), painted)
			}
		})
	}
}

// TestSubAgentSummaryTempi pins the collapsed summary's two tempi: while the run works it counts the
// calls and stops — it does NOT name what the span has open, which is the flicker this line was
// freed of (subAgentGist) — and once the report lands it counts the calls and shows the report's own
// gist. A run with nothing to add to the count says the count alone rather than trailing an empty
// separator.
//
// The working tempo keeps exactly one live word, and both halves of its rule are pinned: `delegating`
// while the span's most recent open call is itself a delegation, and nothing again the moment that
// grandchild opens a call of its own — the newest open call is the one the rule reads, so the cell
// names the nearest live fact or none.
//
// Each tempo is pinned twice — with a context reading and without one. The delegate's fill takes the
// middle cell whenever it has reported one, in the gauge's own coarse spelling; where it has
// not, the line degrades to exactly what it said before the reading existed, separator and all, which
// is also what an old session decodes to.
func TestSubAgentSummaryTempi(t *testing.T) {
	cases := []struct {
		name  string
		build func(tr *transcript)
		want  string
	}{
		{
			name: "working: the count alone, the call in flight left to its own block",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				tr.apply(domain.ToolCallEvent{EventBase: domain.EventBase{Depth: 1},
					Call: domain.ToolCall{ID: "c2", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)}})
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 2 tool calls"),
		},
		{
			name: "working, having delegated on: the one live word the line keeps",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentCall(tr, "s2", "read the tests", 1) // open: the child is waiting on its own child
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 2 tool calls · delegating"),
		},
		{
			name: "the grandchild's own call is the newest: the word goes again",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				subAgentCall(tr, "s2", "read the tests", 1)
				tr.apply(domain.ToolCallEvent{EventBase: domain.EventBase{Depth: 2},
					Call: domain.ToolCall{ID: "c1", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)}})
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 2 tool calls"),
		},
		{
			name: "working with every call settled: the count alone",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 1 tool call"),
		},
		{
			name: "finished: the count plus a one-line report, which needs no body",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "Found 4 gaps", 0)
			},
			want: groupMemberLine("  ┕ survey the tests ✓ ⋯ 1 tool call · Found 4 gaps"),
		},
		{
			name: "finished: the count plus the verdict, the report itself being a body",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "Found 4 gaps\nand here they are", 0)
			},
			want: groupMemberLine("  ┕ survey the tests ✓ ⋯ 1 tool call · done"),
		},
		{
			name: "working, having reported: the count and the fill close the line",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsage(tr, 1, 12000, 32768)
				tr.apply(domain.ToolCallEvent{EventBase: domain.EventBase{Depth: 1},
					Call: domain.ToolCall{ID: "c2", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)}})
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 2 tool calls · 12k/32k"),
		},
		{
			name: "working with every call settled: the count and the fill, and no empty separator after it",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsage(tr, 1, 900, 32768)
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 1 tool call · 900/32k"),
		},
		{
			name: "finished: the reading the run ended on stands beside its report",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsage(tr, 1, 12000, 32768)
				subAgentUsage(tr, 1, 18432, 32768)
				subAgentReport(tr, "s1", "Found 4 gaps", 0)
			},
			want: groupMemberLine("  ┕ survey the tests ✓ ⋯ 1 tool call · 18k/32k · Found 4 gaps"),
		},
		{
			name: "a reading with no window behind it is no cell at all",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsage(tr, 1, 12000, 0) // an unbound window: a fill with no scale says nothing
				subAgentReport(tr, "s1", "Found 4 gaps", 0)
			},
			want: groupMemberLine("  ┕ survey the tests ✓ ⋯ 1 tool call · Found 4 gaps"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			// The head block is its header then its branch line; the branch carries the summary.
			branch := strings.Split(renderPlain(tr, 80), "\n")[1]
			if branch != tc.want {
				t.Errorf("summary line = %q; want %q", branch, tc.want)
			}
		})
	}
}

// TestSubAgentCountIsTransitive proves the one number covers the whole run: the span holds every
// entry of every level below the head, so a nested run's calls — and the nested sub_agent call
// itself — count toward the outer run's total without a second rule for depth.
//
// The fill on the very same line is the counter-example, and the reason the two cells cannot share a
// rule: work done deeper down is still work this run commissioned, but context filled deeper down was
// filled in a window of its own, so a grandchild's figure must never surface on the outer line.
func TestSubAgentCountIsTransitive(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the repo", 0)
	readCall(tr, "c1", "a.go", 1, 5, 1)
	subAgentCall(tr, "s2", "read the tests", 1)
	readCall(tr, "c2", "b.go", 1, 9, 2)
	readCall(tr, "c3", "c.go", 1, 3, 2)
	subAgentUsage(tr, 2, 7000, 32768) // the grandchild's own fill
	subAgentReport(tr, "s2", "tests read", 1)
	subAgentUsage(tr, 1, 12000, 32768) // the outer run's own fill
	subAgentReport(tr, "s1", "survey complete", 0)

	// One read at depth 1, the nested sub-agent call, and its two reads at depth 2 — against the
	// outer run's OWN 12k, never the 19k the two windows would add up to.
	painted := renderPlain(tr, 80)
	want := groupMemberLine("  ┕ survey the repo ✓ ⋯ 4 tool calls · 12k/32k · survey complete")
	if branch := strings.Split(painted, "\n")[1]; branch != want {
		t.Errorf("transitive summary = %q; want %q", branch, want)
	}
	if strings.Contains(painted, "7k") {
		t.Errorf("the nested run's fill surfaced on the collapsed outer run — a fill is not transitive:\n%s", painted)
	}
}

// A run VIEW paints the run it is rooted at and nothing deeper: a nested delegation inside it keeps
// its OWN state, which is the collapsed row every run wears in a conversation (ADR 0063). One rule,
// applied at every depth — not a special case for nesting — and the way further in is the same one
// that got the reader here: expanding that row opens ITS view.
//
// Everything the view paints stands at the top level, the rebasing (paintRoot) taking the rail with
// it: the levels are told apart by which view a reader is in rather than by how deep a row is
// indented.
func TestNestedSubAgentRunStaysCollapsedInsideItsParentsView(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the repo", 0)
	subAgentCall(tr, "s2", "read the tests", 1)
	readCall(tr, "c1", "b.go", 1, 9, 2)
	subAgentReport(tr, "s2", "tests read", 1)
	subAgentReport(tr, "s1", "survey complete", 0)

	tr.setRoot(runRef{depth: 1, spawn: "s1"})

	got := renderPlain(tr, 80)
	if !strings.Contains(got, groupMemberLine("  ┕ read the tests ✓ ⋯ 1 tool call · tests read")) {
		t.Errorf("the nested run is not the collapsed row it wears everywhere else:\n%s", got)
	}
	for _, absent := range []string{
		"b.go",       // the nested run's own span, which its collapsed row elides
		glyphSubRail, // every row of a view stands at the top level
	} {
		if strings.Contains(got, absent) {
			t.Errorf("the view shows %q, which belongs to the run one level further in:\n%s", absent, got)
		}
	}
}

// ----------------------------------------------------------------------------
// The streaming buffer (streamBuf)
// ----------------------------------------------------------------------------

// bufOf is a streamBuf holding s — the seed every test that hands the transcript a half-streamed
// buffer uses, so no test has to know how the chunks are cut.
func bufOf(s string) streamBuf {
	var b streamBuf
	b.append(s)
	return b
}

// TestStreamBufAppendsInBoundedChunks is the reason the buffer stopped being a string: an append
// may copy a chunk, never the whole reply. The chunk count is the proof — one growing string would
// be a single "chunk" re-copied 1,000 times, and one chunk per append would be 1,000 of them.
func TestStreamBufAppendsInBoundedChunks(t *testing.T) {
	t.Parallel()

	const appends, size = 1000, 100
	chunk := strings.Repeat("x", size)
	var b streamBuf
	for range appends {
		b.append(chunk)
	}

	if got, want := b.Len(), appends*size; got != want {
		t.Errorf("Len() = %d, want %d", got, want)
	}
	if got, want := b.String(), strings.Repeat(chunk, appends); got != want {
		t.Errorf("String() length = %d, want %d — the buffer must join to the concatenation",
			len(got), len(want))
	}
	if got, want := len(b.chunks), appends*size/streamChunkBytes+1; got > want {
		t.Errorf("%d chunks for %d bytes, want at most %d — an append is not bounded by the chunk size",
			got, b.Len(), want)
	}
	if b.empty() {
		t.Error("empty() = true for a filled buffer")
	}
}

// TestStreamBufTailReturnsOnlyTheLastLines is what makes a mid-stream repaint cost a viewport: the
// preview asks for the lines it can paint, and the buffer walks back from its end to find them
// instead of joining everything the model has said. The extra line is previewTail's margin — it
// trims trailing blank lines before taking its own last previewTailLines.
func TestStreamBufTailReturnsOnlyTheLastLines(t *testing.T) {
	t.Parallel()

	var b streamBuf
	for i := range 5000 {
		b.append(fmt.Sprintf("%04d %s\n", i, strings.Repeat("x", 59)))
	}
	if len(b.chunks) < 2 {
		t.Fatalf("setup: %d chunk(s) — the tail must have several to walk back through", len(b.chunks))
	}

	got := b.tail(previewTailLines)

	lines := strings.Split(b.String(), "\n")
	want := strings.Join(lines[len(lines)-(previewTailLines+1):], "\n")
	if got != want {
		t.Errorf("tail(%d) returned %d bytes, want the last %d lines (%d bytes)",
			previewTailLines, len(got), previewTailLines+1, len(want))
	}
	if strings.Contains(got, "0000 ") {
		t.Error("the tail reached the first chunk — it must join only the chunks the cut reaches")
	}
	if len(got) >= b.Len() {
		t.Errorf("tail = %d bytes of a %d-byte buffer, want a viewport rather than the reply",
			len(got), b.Len())
	}
}

// TestStreamBufSurvivesAValueCopy is ADR 0011 read at the buffer: the Model is copied by value on
// every Update, so a copy that is discarded rather than returned must leave the live transcript
// exactly as it found it. A chunk list appended in place would fail this — the copy shares the
// backing array — which is why append is copy-on-write over the chunk headers.
func TestStreamBufSurvivesAValueCopy(t *testing.T) {
	t.Parallel()

	var original transcript
	original.appendToken("first", runRef{})

	adopted := original
	adopted.appendToken(" and more", runRef{})

	if got, want := original.pending.String(), "first"; got != want {
		t.Errorf("the original's buffer = %q, want %q — the copy's append leaked into it", got, want)
	}
	if got, want := adopted.pending.String(), "first and more"; got != want {
		t.Errorf("the copy's buffer = %q, want %q", got, want)
	}
	if got, want := original.pending.Len(), len("first"); got != want {
		t.Errorf("the original's Len() = %d, want %d", got, want)
	}
}

// ----------------------------------------------------------------------------
// The streamed buffer belongs to the depth that streamed it
// ----------------------------------------------------------------------------

// streamAt folds one streamed chunk at depth — the TokenEvent an agent at that nesting level
// emits, which is the one assistant-text event whose depth used to be thrown away.
func streamAt(tr *transcript, depth int, text string) {
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: depth}, Text: text})
}

// TestSubAgentStreamStaysInsideItsCollapsedRun is the elision rule applied to the LIVE buffer: a
// collapsed run stands alone and everything beneath it is elided (layout.md), and a delegate's
// answer is beneath it from its very first token — not only once its MessageEvent lands. Nothing is
// lost by painting nothing: the head blinks live, carries the run's gist once there is work behind
// it, and the status line already reads "sub-agent · responding".
func TestSubAgentStreamStaysInsideItsCollapsedRun(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)

	streamAt(tr, 1, "child words")

	// The row still wears its ▶: opening this run shows the prompt it carried and the stream
	// beneath it (subAgentHidesPrompt), and the elision rule is about the delegate's WORDS, not
	// about whether the head can be opened at all.
	want := strings.Join([]string{
		"✦ Sub-Agent",
		leaderEdgeRow("  ┕ survey the tests ⋯", glyphCollapsed),
	}, "\n")
	got := renderPlain(tr, 80)
	if got != want {
		t.Errorf("collapsed run leaked its delegate's live stream:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A delegate's live words stand inside the RUN VIEW of the child that streamed them, at the top
// level of that view and with no rail to say otherwise (ADR 0063, render.go's rooted paint). This is
// design call 4 of docs/plans/"2026-08-11 - 01" as the view states it: what the LIVE paint draws is
// exactly what the COMMITTED paint draws, so nothing a reader is watching jumps when a token stops
// streaming and starts being scrollback. The same run is painted twice — once with the words still
// in the buffer, once after its MessageEvent has folded those same words into an entry of the run —
// and the two paints must be identical to the byte.
func TestSubAgentStreamSettlesWithoutMovingTheView(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "child words"})
	tr.setRoot(runRef{depth: 1, spawn: "s1"})

	live := renderPlain(tr, 80)
	if !strings.Contains(live, "✦ child words") {
		t.Fatalf("the view does not show the delegate's live tail:\n%s", live)
	}
	if strings.Contains(live, glyphSubRail) {
		t.Errorf("the view railed the run's own words:\n%s", live)
	}

	// The delegate's Turn ends: the streamed words commit as an entry of its run, and the run itself
	// is still open — it has not reported.
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "child words"})

	if settled := renderPlain(tr, 80); settled != live {
		t.Errorf("the view moved when the delegate's stream committed:\n--- live ---\n%s\n--- settled ---\n%s",
			live, settled)
	}
}

// While siblings run at once (ADR 0039) a delegate's stream belongs to the child that is TALKING and
// to no other: in the conversation every member of the fan-out is one collapsed row and the words
// are elided with the rest of that child's run, and in a view they stand in the run whose spawning
// call stamped them — never behind whichever sibling was announced last (transcript.runEnd).
func TestSubAgentStreamBelongsToTheChildThatIsTalking(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)
	// Both children are RUNNING — each has a slot of its own, as the engine says by starting them —
	// so both rows are working ones rather than the queued row a delegation waiting for a slot wears
	// (subAgentScheduled, pinned by TestSubAgentScheduledUntilItStarts).
	subAgentStarted(tr, "s1", 1)
	subAgentStarted(tr, "s2", 1)
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "child words"})

	want := strings.Join([]string{
		"✦ Sub-Agent (2)",
		"  ┝ survey the tests ⋯",
		"  ┕ survey the docs ⋯",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("the fan-out leaked a child's live words:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	tr.setRoot(runRef{depth: 1, spawn: "s1"})
	if got := renderPlain(tr, 80); !strings.Contains(got, "✦ child words") {
		t.Errorf("the talking child's view does not show its own words:\n%s", got)
	}
	tr.setRoot(runRef{depth: 1, spawn: "s2"})
	if got := renderPlain(tr, 80); strings.Contains(got, "child words") {
		t.Errorf("the silent sibling's view shows the other child's words:\n%s", got)
	}
}

// TestSubAgentStreamResidueIsNotAttributedToTheParent pins the committed side: a delegate that
// streamed and then never sent its MessageEvent (faulted, abandoned, cancelled) leaves text in the
// buffer, and the parent's next event must not adopt it. The buffer closes at ITS OWN depth, so the
// residue lands inside the run rather than as a permanent top-level answer in the main transcript.
func TestSubAgentStreamResidueIsNotAttributedToTheParent(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	streamAt(tr, 1, "child words")

	readCall(tr, "c1", "a.go", 1, 5, 0)

	committed := 0
	for i := range tr.entries {
		e := tr.entries[i]
		if e.kind == entryAssistant {
			committed++
			if e.depth != 1 {
				t.Errorf("entry %d: the delegate's stream committed at depth %d, want 1", i, e.depth)
			}
		}
		if e.depth == 0 && strings.Contains(e.text, "child words") {
			t.Errorf("entry %d: a depth-0 entry carries the delegate's text: %q", i, e.text)
		}
	}
	if committed != 1 {
		t.Errorf("committed %d assistant entries, want the delegate's residue kept as exactly 1", committed)
	}
}

// TestParentMessageKeepsTheDelegatesStreamInsideItsRun is the residue rule on the MESSAGE path,
// where the loss is silent rather than misattributed. commitAssistant has a rescue for a blank
// canonical text — it falls back to the buffer — but a parent that answers with words of its own
// never reaches it, so without the foreign-depth close the abandoned delegate's streamed words would
// be overwritten by the parent's answer and vanish from the transcript entirely. Closing at the
// buffer's OWN depth first keeps them, inside the run they were streamed in, and the parent's
// answer still commits as the top-level answer it is.
func TestParentMessageKeepsTheDelegatesStreamInsideItsRun(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	streamAt(tr, 1, "child words")

	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 0}, Text: "parent answer"})

	var child, parent *entry
	for i := range tr.entries {
		e := &tr.entries[i]
		if e.kind != entryAssistant {
			continue
		}
		switch {
		case strings.Contains(e.text, "child words"):
			child = e
		case strings.Contains(e.text, "parent answer"):
			parent = e
		}
	}
	if child == nil {
		t.Fatalf("the parent's message discarded the delegate's streamed words:\n%s", renderPlain(tr, 80))
	}
	if child.depth != 1 {
		t.Errorf("the delegate's words committed at depth %d, want 1 — inside its run", child.depth)
	}
	if parent == nil {
		t.Fatalf("the parent's own answer committed no entry:\n%s", renderPlain(tr, 80))
	}
	if parent.depth != 0 {
		t.Errorf("the parent's answer committed at depth %d, want 0", parent.depth)
	}

	// Where the two land is a paint, not only a field: in the conversation the run elides the
	// delegate's words and the parent's answer stands alone, while the words themselves are one
	// view away — inside the run that streamed them (ADR 0063).
	if got := renderPlain(tr, 80); strings.Contains(got, "child words") || !strings.Contains(got, "parent answer") {
		t.Errorf("the run did not elide the delegate's words beside the parent's answer:\n%s", got)
	}
	tr.setRoot(runRef{depth: 1, spawn: "s1"})
	if got := renderPlain(tr, 80); !strings.Contains(got, "✦ child words") || strings.Contains(got, "parent answer") {
		t.Errorf("the run's view does not hold its own rescued words:\n%s", got)
	}
}

// TestStreamResetOnlyDiscardsItsOwnDepth is the mirror of the residue rule on the discard path: a
// re-stream is one agent's Turn starting over (events.go), so it may only drop the buffer it owns.
func TestStreamResetOnlyDiscardsItsOwnDepth(t *testing.T) {
	tr := &transcript{}
	streamAt(tr, 0, "parent words")

	tr.apply(domain.StreamResetEvent{EventBase: domain.EventBase{Depth: 1}})

	if !tr.streaming || tr.pending.String() != "parent words" {
		t.Errorf("a depth-1 re-stream wiped the parent's buffer: streaming=%v pending=%q",
			tr.streaming, tr.pending.String())
	}
}

// TestStreamResetDropsOnlyTheParkedSiblingsOwnText is the same rule read sideways, now that siblings
// share the one live buffer (ADR 0039): the run that re-streams may hold no slot at all. Its parked
// text — the stream it was displaced from mid-alternation — is superseded exactly like a live buffer
// would be and goes with the reset, while the sibling actually holding the slot streams on untouched.
// Both halves are the point: keeping the parked text would let a superseded stream commit later at
// its own run's report (TestAbandonedChildStreamCommitsWhenItsRunEnds is that exit), and clearing the
// slot instead would shred the answer of a run that never re-streamed anything.
func TestStreamResetDropsOnlyTheParkedSiblingsOwnText(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)
	token := func(spawn, text string) {
		tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: spawn}, Text: text})
	}
	token("s2", "superseded words") // s2 opens the buffer…
	token("s1", "the tests ")       // …and s1 takes the slot, parking s2's words

	tr.apply(domain.StreamResetEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s2"}})
	token("s1", "all pass")

	if want := (runRef{depth: 1, spawn: "s1"}); !tr.streaming || tr.pendingRun != want ||
		tr.pending.String() != "the tests all pass" {
		t.Errorf("the sibling's re-stream disturbed the live streamer: streaming=%v run=%+v pending=%q",
			tr.streaming, tr.pendingRun, tr.pending.String())
	}
	if len(tr.parked) != 0 {
		t.Errorf("parked = %+v, want the resetting sibling's superseded text dropped", tr.parked)
	}

	// Neither exit may surface the dropped tokens: s1 commits its own answer (a blank MessageEvent
	// falls back to the buffer), and s2's run ends — the report that would commit a displaced
	// delegate's residue now has nothing left to commit.
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}})
	subAgentReport(tr, "s2", "cancelled", 0)

	var committed []string
	for _, e := range tr.entries {
		if e.kind == entryAssistant {
			committed = append(committed, e.text)
		}
	}
	if len(committed) != 1 || committed[0] != "the tests all pass" {
		t.Errorf("committed assistant text = %q, want only the live streamer's whole answer", committed)
	}
}

// ----------------------------------------------------------------------------
// A sub-agent's context fill folds onto its own run (transcript.applyUsage)
// ----------------------------------------------------------------------------

// subAgentUsage folds one reading a delegate at depth reported, against the window in force when
// it landed — the UsageEvent a child's Turn emits, which reaches the parent's transcript at the
// child's own nesting level.
func subAgentUsage(tr *transcript, depth, total, window int) {
	tr.applyUsage(domain.UsageEvent{EventBase: domain.EventBase{Depth: depth}, TotalTokens: total}, window, "")
}

// subAgentUsageOn is subAgentUsage for a routed delegation: the same reading, stamped with the model
// the CHILD ran on and folded against the model the SESSION is bound to — the two the fold compares
// to decide whether the run's model is worth saying (ADR 0045).
func subAgentUsageOn(tr *transcript, depth, total, window int, childModel, sessionModel string) {
	t := domain.UsageEvent{EventBase: domain.EventBase{Depth: depth}, TotalTokens: total, Model: childModel}
	tr.applyUsage(t, window, sessionModel)
}

// subAgentUsageIn is subAgentUsage for a routed delegation's FILL: the same reading, stamped with
// the window the CHILD actually worked against (the Delegation target's) and folded while the
// session's own is another number entirely.
func subAgentUsageIn(tr *transcript, depth, total, sessionWindow, childWindow int) {
	reading := domain.UsageEvent{
		EventBase:     domain.EventBase{Depth: depth},
		TotalTokens:   total,
		ContextWindow: childWindow,
	}
	tr.applyUsage(reading, sessionWindow, "")
}

// TestSubAgentFillFoldsTheChildsOwnWindow pins the limit half of a delegation's reading: a routed
// child works against the Delegation target's window (ADR 0045), so its fill is frozen against THAT
// number and painted against it — 7k on an 8k grunt server is `7k/8k`, never `7k/128k` against the
// session's window, which would be a wrong number on screen rather than a missing one. A reading
// naming no window (an unrouted child, a record from before the stamp existed) keeps the session's.
func TestSubAgentFillFoldsTheChildsOwnWindow(t *testing.T) {
	const sessionWindow = 131072

	t.Run("a routed child freezes the target's window", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsageIn(tr, 1, 7000, sessionWindow, 8192)

		used, limit := fillOf(tr, 0)
		if used != 7000 || limit != 8192 {
			t.Errorf("head fill = %d/%d, want 7000/8192 — the window the child actually filled", used, limit)
		}
	})

	t.Run("a reading naming no window falls back to the session's", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsageIn(tr, 1, 7000, sessionWindow, 0)

		if _, limit := fillOf(tr, 0); limit != sessionWindow {
			t.Errorf("head limit = %d, want the session's %d — an unrouted child inherits it verbatim",
				limit, sessionWindow)
		}
	})

	t.Run("a later reading moves the limit with the fill", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsageIn(tr, 1, 7000, sessionWindow, 8192)
		subAgentUsageIn(tr, 1, 9000, sessionWindow, 16384) // the target rebound mid-run

		used, limit := fillOf(tr, 0)
		if used != 9000 || limit != 16384 {
			t.Errorf("head fill = %d/%d, want 9000/16384 — the pair is frozen together", used, limit)
		}
	})

	cases := []struct {
		name  string
		build func(tr *transcript)
		want  string
	}{
		{
			name: "routed: the fill reads against the grunt server's window",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsageIn(tr, 1, 7000, sessionWindow, 8192)
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 1 tool call · 7k/8k"),
		},
		{
			name: "unrouted: the fill reads against the session's window",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsageIn(tr, 1, 7000, sessionWindow, 0)
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 1 tool call · 7k/128k"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			branch := strings.Split(renderPlain(tr, 80), "\n")[1]
			if branch != tc.want {
				t.Errorf("summary line = %q; want %q", branch, tc.want)
			}
		})
	}
}

// TestSubAgentModelFoldsOnlyWhenItDiffers pins what the head keeps of a routed delegation's model:
// the child's own when the session is bound to another, nothing at all when the two match, and
// nothing from an agent that names no model. The comparison is made at FOLD time, so what a finished
// run says about itself survives the session rebinding to the very model the child ran on.
func TestSubAgentModelFoldsOnlyWhenItDiffers(t *testing.T) {
	const window = 32768

	t.Run("a routed child keeps the model it ran on", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsageOn(tr, 1, 12000, window, "qwen3-4b", "gpt-oss-20b")

		if got := tr.entries[0].ctxModel; got != "qwen3-4b" {
			t.Errorf("head model = %q, want %q — the delegation ran somewhere else", got, "qwen3-4b")
		}
	})

	t.Run("a child on the session's own model keeps nothing", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsageOn(tr, 1, 12000, window, "gpt-oss-20b", "gpt-oss-20b")

		if got := tr.entries[0].ctxModel; got != "" {
			t.Errorf("head model = %q, want none — a run where everything else runs is not news", got)
		}
	})

	t.Run("a reading naming no model leaves the answer standing", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsageOn(tr, 1, 12000, window, "qwen3-4b", "gpt-oss-20b")
		subAgentUsageOn(tr, 1, 18000, window, "", "gpt-oss-20b") // an agent bound before its heartbeat

		if got := tr.entries[0].ctxModel; got != "qwen3-4b" {
			t.Errorf("head model = %q, want it left standing at %q", got, "qwen3-4b")
		}
	})

	t.Run("a maintenance reading names the model it left the fill alone for", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		fold := domain.UsageEvent{
			EventBase:              domain.EventBase{Depth: 1},
			TotalTokens:            8400,
			CumulativeCalls:        2,
			CumulativeTotalTokens:  20400,
			CumulativePromptTokens: 19000,
			Model:                  "qwen3-4b",
			Maintenance:            true,
		}
		tr.applyUsage(fold, window, "gpt-oss-20b")

		if used, _ := fillOf(tr, 0); used != 0 {
			t.Errorf("a maintenance reading moved the fill to %d, want it untouched", used)
		}
		if got := tr.entries[0].ctxModel; got != "qwen3-4b" {
			t.Errorf("head model = %q, want %q — the fold still ran on the child's own model", got, "qwen3-4b")
		}
	})

	t.Run("the frozen answer outlives a rebind to the child's model", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsageOn(tr, 1, 12000, window, "qwen3-4b", "gpt-oss-20b")
		subAgentReport(tr, "s1", "tests read", 0)
		// The session rebinds onto the very model the finished run used; its history does not move.
		if got := tr.entries[0].ctxModel; got != "qwen3-4b" {
			t.Errorf("head model = %q, want %q kept as history", got, "qwen3-4b")
		}
	})
}

// TestApplyUsageStripsTheDelegateModel pins the fold as the SEAM this id enters the view through.
// A delegation's model comes off the wire stamped on a usage reading, and the sub-agent line paints
// what the fold stored (subagentblock.go) — so an OSC 52 payload salted into the id by a hostile
// Sub-agent server reaches the frame unless the fold takes it out here, at the one place the field
// is written.
func TestApplyUsageStripsTheDelegateModel(t *testing.T) {
	t.Parallel()
	const window = 32768

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)

	subAgentUsageOn(tr, 1, 12000, window, "child"+escOSC52, "gpt-oss-20b")

	// The strip drops the ESC introducer and the BEL terminator and leaves the payload behind as
	// inert text, exactly as it does everywhere else (TestStripEscapesDropsControlCharacters): what
	// reaches the frame is no longer a sequence the terminal will act on.
	if got, want := tr.entries[0].ctxModel, "child]52;c;cGFyaQ=="; got != want {
		t.Errorf("head model = %q, want %q — the fold strips the server's own text", got, want)
	}
}

// TestSubAgentSummaryNamesADifferingModel pins the one thing routing to the Sub-agent server shows
// of itself on a delegation's collapsed line (ADR 0045): the model the child ran on, closing the
// line, and only where it is not the session's own. A same-model delegation renders exactly the line
// this block rendered before routing existed — no cell, no separator.
func TestSubAgentSummaryNamesADifferingModel(t *testing.T) {
	const window = 32768

	cases := []struct {
		name  string
		build func(tr *transcript)
		want  string
	}{
		{
			name: "routed: the model closes the line, after the report",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsageOn(tr, 1, 12000, window, "qwen3-4b", "gpt-oss-20b")
				subAgentReport(tr, "s1", "Found 4 gaps", 0)
			},
			want: groupMemberLine("  ┕ survey the tests ✓ ⋯ 1 tool call · 12k/32k · Found 4 gaps · qwen3-4b"),
		},
		{
			name: "routed and still working: the model closes the count and the fill",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsageOn(tr, 1, 900, window, "qwen3-4b", "gpt-oss-20b")
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 1 tool call · 900/32k · qwen3-4b"),
		},
		{
			name: "same model: the line this block always painted",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsageOn(tr, 1, 12000, window, "gpt-oss-20b", "gpt-oss-20b")
				subAgentReport(tr, "s1", "Found 4 gaps", 0)
			},
			want: groupMemberLine("  ┕ survey the tests ✓ ⋯ 1 tool call · 12k/32k · Found 4 gaps"),
		},
		{
			name: "a weights path is spelled the way the footer spells one",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentUsageOn(tr, 1, 900, window, "/models/qwen2.5-coder-7b.gguf", "gpt-oss-20b")
			},
			want: groupMemberLine("  ┕ survey the tests ⋯ 1 tool call · 900/32k · qwen2.5-coder-7b"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			branch := strings.Split(renderPlain(tr, 80), "\n")[1]
			if branch != tc.want {
				t.Errorf("summary line = %q; want %q", branch, tc.want)
			}
		})
	}
}

// fillOf reads back the pair frozen on the entry at i: what the child's context held, out of the
// window that reading filled.
func fillOf(tr *transcript, i int) (used, limit int) {
	return tr.entries[i].ctxUsed, tr.entries[i].ctxLimit
}

// TestSubAgentUsageFillsItsOwnRun pins the attribution rule: a reading belongs to the run that
// produced it — the most recent still-open sub-agent head one level above the reading's depth —
// and to nothing else. Each agent fills its own window, so the fill is neither cumulative across
// the child's Turns nor transitive up the nesting, and a finished run's figure is history.
func TestSubAgentUsageFillsItsOwnRun(t *testing.T) {
	const window = 32768

	t.Run("the reading lands on the open run one level above it", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		readCall(tr, "c1", "a.go", 1, 5, 1)

		subAgentUsage(tr, 1, 12000, window)

		if used, limit := fillOf(tr, 0); used != 12000 || limit != window {
			t.Errorf("run fill = %d/%d, want 12000/%d on the head that delegated", used, limit, window)
		}
		if used, limit := fillOf(tr, 1); used != 0 || limit != 0 {
			t.Errorf("the child's own read call took a fill (%d/%d); only a run head carries one", used, limit)
		}
	})

	t.Run("the latest reading replaces the previous one — a fill, never a sum", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)

		subAgentUsage(tr, 1, 8000, window)
		subAgentUsage(tr, 1, 12000, window)
		subAgentUsage(tr, 1, 0, window) // a Turn that reported nothing leaves the fill standing

		if used, _ := fillOf(tr, 0); used != 12000 {
			t.Errorf("run fill = %d, want the latest reading 12000 (not 20000, and not blanked)", used)
		}
	})

	t.Run("a total the server omitted falls back to prompt+completion", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)

		tr.applyUsage(domain.UsageEvent{
			EventBase:    domain.EventBase{Depth: 1},
			PromptTokens: 900, CompletionTokens: 100,
		}, window, "")

		if used, _ := fillOf(tr, 0); used != 1000 {
			t.Errorf("run fill = %d, want 1000 (the same preference the gauge reads usage by)", used)
		}
	})

	t.Run("a second run reads for itself while the finished one stays frozen", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentUsage(tr, 1, 12000, window)
		subAgentReport(tr, "s1", "done", 0)

		subAgentCall(tr, "s2", "survey the docs", 0)
		subAgentUsage(tr, 1, 5000, window/2) // a rebound window: the second run measures against it

		if used, limit := fillOf(tr, 0); used != 12000 || limit != window {
			t.Errorf("the finished run's fill moved to %d/%d; a reported run is history", used, limit)
		}
		if used, limit := fillOf(tr, 1); used != 5000 || limit != window/2 {
			t.Errorf("second run fill = %d/%d, want 5000/%d", used, limit, window/2)
		}
	})

	t.Run("a nested run's reading stops at the nested head", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the repo", 0)
		subAgentCall(tr, "s2", "read the tests", 1)

		subAgentUsage(tr, 2, 7000, window)

		if used, _ := fillOf(tr, 1); used != 7000 {
			t.Errorf("nested run fill = %d, want the grandchild's 7000", used)
		}
		if used, _ := fillOf(tr, 0); used != 0 {
			t.Errorf("the outer run took the nested reading (%d): a fill is not transitive", used)
		}

		subAgentUsage(tr, 1, 12000, window)
		if used, _ := fillOf(tr, 0); used != 12000 {
			t.Errorf("outer run fill = %d, want its own 12000", used)
		}
		if used, _ := fillOf(tr, 1); used != 7000 {
			t.Errorf("nested run fill = %d, want its own 7000 left alone", used)
		}
	})

	t.Run("a reading with no open run at its depth folds nothing", func(t *testing.T) {
		cases := []struct {
			name  string
			build func(tr *transcript)
			depth int
		}{
			{
				name:  "nothing delegated at all",
				build: func(*transcript) {},
				depth: 1,
			},
			{
				name: "the run already reported",
				build: func(tr *transcript) {
					subAgentCall(tr, "s1", "survey the tests", 0)
					subAgentReport(tr, "s1", "done", 0)
				},
				depth: 1,
			},
			{
				name: "an ordinary open call is not a run head",
				build: func(tr *transcript) {
					tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
						ID: "c1", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)}})
				},
				depth: 1,
			},
			{
				name: "the reading is the human's own conversation",
				build: func(tr *transcript) {
					subAgentCall(tr, "s1", "survey the tests", 0)
				},
				depth: 0, // the gauge's business (foldStats), never a run block's
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tr := &transcript{}
				tc.build(tr)

				subAgentUsage(tr, tc.depth, 12000, window)

				for i := range tr.entries {
					if used, limit := fillOf(tr, i); used != 0 || limit != 0 {
						t.Errorf("entry %d took a fill (%d/%d); the reading matched no open run", i, used, limit)
					}
				}
			})
		}
	})
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
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "b",
		Content: "[File: b.go, 10 lines total, showing lines 1-10]\n…",
		Summary: domain.ReadSpan{Start: 1, End: 10, Total: 10}}})

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
	if b.tool.Summary.Text != "10 lines" || b.tool.Details.len() != 0 {
		t.Errorf("call b outcome = %+v / %+v; want a \"10 lines\" summary and no body", b.tool.Summary, b.tool.Details)
	}

	// Call a's result arrives later and folds into a — still two entries, no orphan.
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "a",
		Content: "[File: a.go, 5 lines total, showing lines 1-5]\n…",
		Summary: domain.ReadSpan{Start: 1, End: 5, Total: 5}}})
	if !callEntry(tr, "a").done {
		t.Error("call a's later result did not fold into it")
	}
	if n := len(tr.entries); n != 2 {
		t.Errorf("entries = %d after both results; want 2", n)
	}
}

// toggleExpanded flips the block state of the kinds that OWN one — a tool call and the human's own
// two voices, the prompt and the interjection — and nothing else: an index naming another kind, or
// naming nothing at all (a click resolved against a paint the transcript has grown past), answers
// false and leaves every entry as it was. It runs on the repaint path, so the out-of-range cases
// are the point — a panic there is the whole session.
//
// The gate is the KIND's, never the block's size: the short prompt below toggles like any other,
// and whether that state changes what is painted is the painter's question, asked at the live width.
func TestToggleExpandedTargetsCollapsibleKinds(t *testing.T) {
	fixture := func() *transcript {
		tr := &transcript{}
		tr.addUser("read a.go", nil)
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}})
		tr.addInterjected("the other a.go", nil)
		tr.entries = append(tr.entries, entry{kind: entryAssistant, text: "reading it now"})
		tr.addNote("cancelled")
		tr.addStartup(startupView{Logo: "logo", Host: "host", Model: "model"})
		return tr
	}
	cases := []struct {
		name  string
		index int
		want  bool
	}{
		{name: "a tool call toggles", index: 1, want: true},
		{name: "a user send toggles", index: 0, want: true},
		{name: "an interjection toggles", index: 2, want: true},
		{name: "an assistant answer has no block state", index: 3, want: false},
		{name: "a note has no block state", index: 4, want: false},
		{name: "the start-up box has no block state", index: 5, want: false},
		{name: "an index past the tail is no entry", index: 6, want: false},
		{name: "a negative index is no entry", index: -1, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := fixture()

			got := tr.toggleExpanded(tc.index)

			if got != tc.want {
				t.Errorf("toggleExpanded(%d) = %v; want %v", tc.index, got, tc.want)
			}
			for i := range tr.entries {
				if expanded := tr.entries[i].expanded; expanded != (tc.want && i == tc.index) {
					t.Errorf("entries[%d].expanded = %v after toggleExpanded(%d)", i, expanded, tc.index)
				}
			}
		})
	}
}

// toolCallCard is one committed tool call as the grouping model reads it: the friendly label the
// rows group by, the target that would lead its member row, and the depth it stands at. Nothing else
// on a call reaches the model, which is why the fixture states nothing else.
func toolCallCard(label, target string, depth int) entry {
	return entry{kind: entryToolCall, depth: depth, tool: toolView{Label: label, Target: target}}
}

// subAgentCard is a sub-agent call entry built by the PRESENTER, so the solo mark that keeps it out
// of every folded block is the one the live path sets rather than a fact this test asserts about
// itself (presentToolCall, toolView.solo).
func subAgentCard(name string, depth int) entry {
	return entry{kind: entryToolCall, depth: depth, tool: presentToolCall(domain.ToolCall{
		ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"name":"` + name + `","task":"survey"}`),
	}, "", workspaceRoot{})}
}

// A super-group forms at two adjacent same-depth runs of DIFFERENT labels, a lone call counting as a
// run of 1, and ends at the first thing that is not such a run: a non-tool entry, a sub-agent block,
// a call standing at another depth, a call the presenter left unfoldable. One run alone is the
// same-label group that already had a header of its own and is no umbrella
// (docs/layout/tool-layout.md, "Vocabulary"; design call 1).
func TestTranscriptSuperGroupFormation(t *testing.T) {
	t.Parallel()

	note := entry{kind: entryNote, text: "cancelled"}
	cases := []struct {
		name    string
		entries []entry
		at      int
		want    superGroup
		calls   int
	}{
		{
			name: "two runs of different labels fold under one umbrella",
			entries: []entry{toolCallCard("Read", "a.go", 0), toolCallCard("Read", "b.go", 0),
				toolCallCard("Terminal", "go build", 0)},
			at:    0,
			want:  superGroup{{at: 0, n: 2}, {at: 2, n: 1}},
			calls: 3,
		},
		{
			name: "a lone call is a run of 1, so read/terminal/read is three rows",
			entries: []entry{toolCallCard("Read", "a.go", 0), toolCallCard("Terminal", "go build", 0),
				toolCallCard("Read", "b.go", 0)},
			at:    0,
			want:  superGroup{{at: 0, n: 1}, {at: 1, n: 1}, {at: 2, n: 1}},
			calls: 3,
		},
		{
			name:    "one run alone is the same-label group, not an umbrella",
			entries: []entry{toolCallCard("Read", "a.go", 0), toolCallCard("Read", "b.go", 0)},
			at:      0,
		},
		{
			name:    "a single call heads nothing",
			entries: []entry{toolCallCard("Read", "a.go", 0)},
			at:      0,
		},
		{
			name:    "a note between two runs breaks the umbrella",
			entries: []entry{toolCallCard("Read", "a.go", 0), note, toolCallCard("Terminal", "go build", 0)},
			at:      0,
		},
		{
			name: "the umbrella resumes after the breaker",
			entries: []entry{toolCallCard("Read", "a.go", 0), toolCallCard("Terminal", "go build", 0), note,
				toolCallCard("Read", "b.go", 0), toolCallCard("Terminal", "go vet", 0)},
			at:    3,
			want:  superGroup{{at: 3, n: 1}, {at: 4, n: 1}},
			calls: 2,
		},
		{
			name:    "a sub-agent block breaks the umbrella",
			entries: []entry{toolCallCard("Read", "a.go", 0), subAgentCard("surveyor", 0), toolCallCard("Terminal", "go build", 0)},
			at:      0,
		},
		{
			name:    "a sub-agent call heads no umbrella of its own",
			entries: []entry{subAgentCard("surveyor", 0), toolCallCard("Read", "a.go", 0), toolCallCard("Terminal", "go build", 0)},
			at:      0,
		},
		{
			name:    "a deeper call belongs to another level and does not join",
			entries: []entry{toolCallCard("Read", "a.go", 0), toolCallCard("Terminal", "go build", 1)},
			at:      0,
		},
		{
			name:    "a call with no target cannot be a member row",
			entries: []entry{toolCallCard("Read", "a.go", 0), toolCallCard("HTTP", "", 0), toolCallCard("Terminal", "go build", 0)},
			at:      0,
		},
		{
			name:    "an index naming no entry heads nothing",
			entries: []entry{toolCallCard("Read", "a.go", 0), toolCallCard("Terminal", "go build", 0)},
			at:      7,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := toolSuperGroup(tc.entries, tc.at)

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("toolSuperGroup(…, %d) = %v; want %v", tc.at, got, tc.want)
			}
			if n := got.calls(); n != tc.calls {
				t.Errorf("umbrella counts %d calls; want %d — the header states N", n, tc.calls)
			}
		})
	}
}

// Formation is LIVE (design call 2): the umbrella exists the moment the second different-label run
// starts — while its last call is still open — and grows as calls append, because membership is
// derived from the entries every time it is asked rather than recorded when a call lands.
func TestTranscriptSuperGroupFormsLiveAndGrows(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	call := func(id, tool, args string) {
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: tool, Arguments: []byte(args)}})
	}
	call("c1", "read_file", `{"path":"a.go"}`)
	if got := toolSuperGroup(tr.entries, 0); got != nil {
		t.Fatalf("one call formed %v; a run of 1 is no umbrella on its own", got)
	}

	// The second run's first call is still open — no result has folded into it — and the umbrella is
	// already there, which is what puts the running call on its last row.
	call("c2", "terminal", `{"command":"go build"}`)
	want := superGroup{{at: 0, n: 1}, {at: 1, n: 1}}
	if got := toolSuperGroup(tr.entries, 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("umbrella at the second run = %v; want %v formed live", got, want)
	}
	if tr.entries[1].done {
		t.Fatal("the fixture's last call is settled; the live-formation case needs it open")
	}

	call("c3", "terminal", `{"command":"go vet"}`)
	call("c4", "read_file", `{"path":"b.go"}`)
	got := toolSuperGroup(tr.entries, 0)
	if want := (superGroup{{at: 0, n: 1}, {at: 1, n: 2}, {at: 3, n: 1}}); !reflect.DeepEqual(got, want) {
		t.Errorf("grown umbrella = %v; want %v — a call joins its run, a new label opens one", got, want)
	}
	if n := got.calls(); n != 4 {
		t.Errorf("grown umbrella counts %d calls; want 4", n)
	}
}

// The two levels of state are independent and both survive the list growing beneath them: a type row
// opened inside the umbrella stays open when the next call appends, and opening it never touches the
// member body state that shares the entry (entry.typeExpanded vs entry.expanded).
func TestTranscriptSuperGroupStateSurvivesAppends(t *testing.T) {
	t.Parallel()

	tr := &transcript{entries: []entry{
		toolCallCard("Read", "a.go", 0),
		toolCallCard("Terminal", "go build", 0),
		toolCallCard("Terminal", "go vet", 0),
	}}
	if !tr.setTypeExpanded(1, true) {
		t.Fatal("setTypeExpanded found no run head at the Terminal run's first call")
	}
	if !tr.setExpanded(2, true) {
		t.Fatal("setExpanded found no block at the run's second member")
	}
	if tr.entries[1].expanded {
		t.Error("opening the type row opened the head member's body too; the levels are independent")
	}
	if tr.entries[2].typeExpanded {
		t.Error("opening a member's body opened a type row; the levels are independent")
	}

	tr.entries = append(tr.entries, toolCallCard("Read", "b.go", 0))

	want := superGroup{{at: 0, n: 1}, {at: 1, n: 2}, {at: 3, n: 1}}
	if got := toolSuperGroup(tr.entries, 0); !reflect.DeepEqual(got, want) {
		t.Fatalf("umbrella after the append = %v; want %v", got, want)
	}
	if !tr.entries[1].typeExpanded {
		t.Error("the open type row closed itself when the umbrella grew")
	}
	if !tr.entries[2].expanded {
		t.Error("the open member closed itself when the umbrella grew")
	}
}

// Adjacent delegations fold into ONE list (subAgentGroup, docs/layout/tool-layout.md Rules), and
// adjacency here is adjacency of BLOCKS: a delegation's whole span lies between its call and the
// next call at its own depth, so the walk steps over it. Two are the floor; anything that is not a
// delegation standing at the group's own depth ends it; and a delegation that left no span at all
// joins like any other, which is the case the painter's span rule cannot see.
func TestTranscriptSubAgentGroupFormation(t *testing.T) {
	t.Parallel()

	note := entry{kind: entryNote, text: "cancelled"}
	cases := []struct {
		name    string
		entries []entry
		at      int
		want    []subAgentBlock
	}{
		{
			name:    "two span-less delegations are a group",
			entries: []entry{subAgentCard("scout", 0), subAgentCard("builder", 0)},
			at:      0,
			want:    []subAgentBlock{{at: 0, span: 0}, {at: 1, span: 0}},
		},
		{
			name: "the walk steps over each delegation's span",
			entries: []entry{subAgentCard("scout", 0), toolCallCard("Read", "a.go", 1),
				toolCallCard("Read", "b.go", 1), subAgentCard("builder", 0), toolCallCard("Terminal", "go build", 1)},
			at:   0,
			want: []subAgentBlock{{at: 0, span: 2}, {at: 3, span: 1}},
		},
		{
			name:    "a lone delegation heads no group",
			entries: []entry{subAgentCard("scout", 0), toolCallCard("Read", "a.go", 0)},
			at:      0,
		},
		{
			name:    "a note between two delegations breaks the group",
			entries: []entry{subAgentCard("scout", 0), note, subAgentCard("builder", 0)},
			at:      0,
		},
		{
			name:    "a call to another tool breaks the group",
			entries: []entry{subAgentCard("scout", 0), toolCallCard("Read", "a.go", 0), subAgentCard("builder", 0)},
			at:      0,
		},
		{
			name:    "a nested delegation is its parent's span, not its neighbour",
			entries: []entry{subAgentCard("scout", 0), subAgentCard("deeper", 1)},
			at:      0,
		},
		{
			name:    "an ordinary call heads no group",
			entries: []entry{toolCallCard("Read", "a.go", 0), subAgentCard("scout", 0), subAgentCard("builder", 0)},
			at:      0,
		},
		{
			name:    "an index naming no entry heads nothing",
			entries: []entry{subAgentCard("scout", 0), subAgentCard("builder", 0)},
			at:      7,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := subAgentGroup(tc.entries, tc.at); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("subAgentGroup(…, %d) = %v; want %v", tc.at, got, tc.want)
			}
		})
	}
}

// subAgentGroupAt answers about the group an entry BELONGS to, at every member of it and not only
// at the first — the question the painter asks at each block head, which is what keeps a group from
// growing a second "✦ Sub-Agent (N)" header at every row. Everything in no group at all — a nested
// entry, an ordinary call, a lone delegation — answers false.
func TestTranscriptSubAgentGroupAtEveryMember(t *testing.T) {
	t.Parallel()

	entries := []entry{
		subAgentCard("scout", 0), toolCallCard("Read", "a.go", 1),
		subAgentCard("builder", 0),
		subAgentCard("checker", 0),
	}
	want := []subAgentBlock{{at: 0, span: 1}, {at: 2, span: 0}, {at: 3, span: 0}}
	for pos, member := range want {
		group, at, ok := subAgentGroupAt(entries, member.at)
		if !ok {
			t.Fatalf("entries[%d] belongs to no group; it is member %d of one", member.at, pos)
		}
		if !reflect.DeepEqual(group, want) {
			t.Errorf("the group at entries[%d] = %v; want %v", member.at, group, want)
		}
		if at != pos {
			t.Errorf("entries[%d] reports position %d in its group; want %d", member.at, at, pos)
		}
	}
	for _, outside := range []int{1, 7, -1} {
		if _, _, ok := subAgentGroupAt(entries, outside); ok {
			t.Errorf("entries[%d] reports a group; it heads none", outside)
		}
	}
}

// Delegations group with EACH OTHER and never with anything else (design call 12): the same two
// entries that fold into one "✦ Sub-Agent (2)" head no umbrella and join none, so a batch of mixed
// tool calls standing beside them is split rather than swept in.
func TestTranscriptSubAgentGroupNeverJoinsAnUmbrella(t *testing.T) {
	t.Parallel()

	entries := []entry{
		toolCallCard("Read", "a.go", 0),
		subAgentCard("scout", 0),
		subAgentCard("builder", 0),
		toolCallCard("Terminal", "go build", 0),
	}
	want := []subAgentBlock{{at: 1, span: 0}, {at: 2, span: 0}}
	if got := subAgentGroup(entries, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("the two delegations = %v; want the group %v", got, want)
	}
	for at := 0; at < len(entries); at++ {
		if got := toolSuperGroup(entries, at); got != nil {
			t.Errorf("toolSuperGroup(…, %d) = %v; a delegation neither heads an umbrella nor lets one span it", at, got)
		}
	}
}

// toggleTypeExpanded flips the type row of the one kind that can head a run — a tool call — and
// nothing else: an index naming another kind, or naming nothing at all, answers false and leaves
// every entry as it was. It runs on the repaint path, so the out-of-range cases are the point.
//
// The gate is deliberately the KIND's and not the live derivation's: whether the entry heads a run
// today depends on what the model called next (toolSuperGroup), and a click that succeeded or failed
// by that would lose a reader's open row the moment a call appended behind it.
func TestTranscriptToggleTypeExpandedTargetsToolCalls(t *testing.T) {
	t.Parallel()

	fixture := func() *transcript {
		return &transcript{entries: []entry{
			toolCallCard("Read", "a.go", 0),
			{kind: entryNote, text: "cancelled"},
			{kind: entryUser, text: "read a.go"},
		}}
	}
	cases := []struct {
		name  string
		index int
		want  bool
	}{
		{name: "a tool call heads a type row", index: 0, want: true},
		{name: "a note heads none", index: 1},
		{name: "a user send heads none", index: 2},
		{name: "an index past the tail is no entry", index: 3},
		{name: "a negative index is no entry", index: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := fixture()

			got := tr.toggleTypeExpanded(tc.index)

			if got != tc.want {
				t.Errorf("toggleTypeExpanded(%d) = %v; want %v", tc.index, got, tc.want)
			}
			for i := range tr.entries {
				if open := tr.entries[i].typeExpanded; open != (tc.want && i == tc.index) {
					t.Errorf("entries[%d].typeExpanded = %v after toggleTypeExpanded(%d)", i, open, tc.index)
				}
				if tr.entries[i].expanded {
					t.Errorf("entries[%d].expanded = true; the type row must not carry the body's state", i)
				}
			}
		})
	}
}

// reset returns the transcript to its empty state — no committed entries, no in-progress buffer —
// but preserves the debug flag (a hidden view toggle, not conversation). It is the /clear + /new
// "start a new session" primitive; the caller re-seeds the start-up box afterwards.
func TestTranscriptReset(t *testing.T) {
	tr := &transcript{}
	tr.addStartup(startupView{Logo: "logo", Host: "host", Model: "model"})
	tr.addUser("hello", nil)
	tr.appendToken("streamed ", runRef{depth: 1}) // sets streaming=true, stamps the buffer's depth, grows pending
	tr.debug = true

	tr.reset()

	if n := len(tr.entries); n != 0 {
		t.Errorf("entries = %d after reset, want 0 (all committed entries dropped)", n)
	}
	if !tr.pending.empty() {
		t.Errorf("pending = %q after reset, want empty", tr.pending.String())
	}
	if tr.streaming {
		t.Error("streaming = true after reset, want false")
	}
	if tr.pendingRun != (runRef{}) {
		t.Errorf("pendingRun = %+v after reset, want the zero run", tr.pendingRun)
	}
	if tr.parked != nil {
		t.Errorf("parked = %+v after reset, want nothing set aside", tr.parked)
	}
	if !tr.debug {
		t.Error("debug = false after reset, want it preserved as true")
	}
}

// hasPrompt gates every session save, so only a sent prompt may flip it. Everything the program can
// put on screen by itself — the start-up box, slash-command notes, error notices, the re-derived
// "context: …" / "resumed: …" ephemerals — must leave it false, or a launch spent poking at slash
// commands would file a "Session <date>" record reading 0 messages. An interjection is excluded on
// purpose: it rides an Exchange that an entryUser opened, so it is never the thing that earns a
// record.
func TestTranscriptHasPrompt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		build func(tr *transcript)
		want  bool
	}{
		{name: "empty transcript", build: func(tr *transcript) {}},
		{name: "start-up box only", build: func(tr *transcript) {
			tr.addStartup(startupView{Logo: "logo", Host: "host", Model: "model"})
		}},
		{name: "persisted note", build: func(tr *transcript) { tr.addNote("cancelled") }},
		{name: "ephemeral notes", build: func(tr *transcript) {
			tr.addEphemeralNote("context: AGENTS.md")
			tr.addEphemeralNote("resumed: an earlier session")
		}},
		{name: "error notice", build: func(tr *transcript) { tr.addError("loop", "upstream refused", runRef{}) }},
		{name: "interjection with no opening prompt", build: func(tr *transcript) {
			tr.addInterjected("wrong file", nil)
		}},
		{name: "user message", build: func(tr *transcript) { tr.addUser("hello", nil) }, want: true},
		{name: "user message after a pile of chrome", build: func(tr *transcript) {
			tr.addStartup(startupView{Logo: "logo", Host: "host", Model: "model"})
			tr.addEphemeralNote("context: AGENTS.md")
			tr.addNote("confinement: workspace")
			tr.addError("loop", "upstream refused", runRef{})
			tr.addUser("hello", nil)
		}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tr := &transcript{}
			tc.build(tr)
			if got := tr.hasPrompt(); got != tc.want {
				t.Errorf("hasPrompt() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Skill tokens LOCATED on a sent block (the inline accent's record)
// ----------------------------------------------------------------------------

// TestSentBlocksRecordSkillTokenSpans pins what a sent block keeps about the skills it invoked:
// WHERE they were said and nothing else — one span per occurrence, so a skill named twice lights
// up twice while the engine is still handed one de-duped invocation. The spans are
// the parse layer's own offsets into the very text that becomes the entry, which is what lets the
// block paint the token instead of restating it beside the text.
func TestSentBlocksRecordSkillTokenSpans(t *testing.T) {
	t.Parallel()
	known := knownSkills("refocus", "code-audit")

	t.Run("addUser stores the parsed spans", func(t *testing.T) {
		t.Parallel()
		parsed := parseInput("/refocus then read main.go", known)
		tr := &transcript{}
		tr.addUser(parsed.text, parsed.skillSpans)

		e := tr.entries[0]
		want := []skillSpan{{start: 0, end: 8}}
		if !reflect.DeepEqual(e.skillSpans, want) {
			t.Fatalf("stored spans = %v, want %v", e.skillSpans, want)
		}
		if got := e.text[e.skillSpans[0].start:e.skillSpans[0].end]; got != "/refocus" {
			t.Errorf("the span locates %q in the stored text; want the token itself", got)
		}
	})

	t.Run("addInterjected stores them too", func(t *testing.T) {
		t.Parallel()
		parsed := parseInput("no — /code-audit instead", known)
		tr := &transcript{}
		tr.addInterjected(parsed.text, parsed.skillSpans)

		e := tr.entries[0]
		if len(e.skillSpans) != 1 {
			t.Fatalf("stored %d spans, want 1: %v", len(e.skillSpans), e.skillSpans)
		}
		if got := e.text[e.skillSpans[0].start:e.skillSpans[0].end]; got != "/code-audit" {
			t.Errorf("the span locates %q in the stored text; want the token itself", got)
		}
	})

	t.Run("a skill invoked twice is two spans and one invocation", func(t *testing.T) {
		t.Parallel()
		parsed := parseInput("/refocus and then /refocus again", known)
		if want := []string{"refocus"}; !reflect.DeepEqual(parsed.skillIDs, want) {
			t.Errorf("skillIDs = %v, want the de-duped %v", parsed.skillIDs, want)
		}
		tr := &transcript{}
		tr.addUser(parsed.text, parsed.skillSpans)

		e := tr.entries[0]
		if len(e.skillSpans) != 2 {
			t.Fatalf("stored %d spans, want one per occurrence: %v", len(e.skillSpans), e.skillSpans)
		}
		for _, sp := range e.skillSpans {
			if got := e.text[sp.start:sp.end]; got != "/refocus" {
				t.Errorf("span %v locates %q; want the token itself", sp, got)
			}
		}
	})

	t.Run("a span that does not land in the text is dropped", func(t *testing.T) {
		t.Parallel()
		tr := &transcript{}
		tr.addUser("short", []skillSpan{{start: 0, end: 99}, {start: -1, end: 3}, {start: 4, end: 4}})
		if got := tr.entries[0].skillSpans; got != nil {
			t.Errorf("kept unlandable spans %v; want them dropped so the block paints plain", got)
		}
	})
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

// ----------------------------------------------------------------------------
// A sub-agent's running totals fold onto its own run (transcript.applyUsage)
// ----------------------------------------------------------------------------

// childUsage is one reading a delegate reported: the fill its Turn measured, plus the running
// totals the CHILD stamped it with — its own calls, counted from zero, carried on the event its
// spawning call (callID) identifies.
func childUsage(callID string, depth, total int, cum usageTotals) domain.UsageEvent {
	return domain.UsageEvent{
		EventBase:                    domain.EventBase{Depth: depth, CallID: callID},
		TotalTokens:                  total,
		CumulativePromptTokens:       cum.PromptTokens,
		CumulativeCachedPromptTokens: cum.CachedPromptTokens,
		CumulativeCompletionTokens:   cum.CompletionTokens,
		CumulativeTotalTokens:        cum.TotalTokens,
		CumulativeCalls:              cum.Calls,
	}
}

// TestSubAgentUsageFoldsTheChildsRunningTotals pins the totals half of the delegated fold: a run
// head keeps the newest totals the child stamped, keyed by the call that spawned it — so two
// siblings running at once each keep their own — and the two readings on one event fold
// independently, which is what lets a maintenance call be counted without moving the fill.
func TestSubAgentUsageFoldsTheChildsRunningTotals(t *testing.T) {
	const window = 32768

	t.Run("the totals land on the run the reading's own call opened", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentCall(tr, "s2", "survey the docs", 0)

		want := usageTotals{Calls: 2, PromptTokens: 4000, CompletionTokens: 300, TotalTokens: 4300}
		tr.applyUsage(childUsage("s1", 1, 2200, want), window, "")

		if got := tr.entries[0].usage; got != want {
			t.Errorf("first run totals = %+v, want %+v", got, want)
		}
		if got := tr.entries[1].usage; got != (usageTotals{}) {
			t.Errorf("the sibling took totals it never reported: %+v", got)
		}
	})

	t.Run("the newest reading replaces the previous one", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)

		tr.applyUsage(childUsage("s1", 1, 2200, usageTotals{Calls: 1, PromptTokens: 2000, CompletionTokens: 200, TotalTokens: 2200}), window, "")
		want := usageTotals{Calls: 2, PromptTokens: 5000, CompletionTokens: 400, TotalTokens: 5400}
		tr.applyUsage(childUsage("s1", 1, 3200, want), window, "")

		if got := tr.entries[0].usage; got != want {
			t.Errorf("run totals = %+v, want %+v (the child's own sum, never one added up here)", got, want)
		}
	})

	t.Run("a maintenance reading counts and leaves the fill standing", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		tr.applyUsage(childUsage("s1", 1, 12000, usageTotals{Calls: 1, PromptTokens: 11000, CompletionTokens: 1000, TotalTokens: 12000}), window, "")

		want := usageTotals{Calls: 2, PromptTokens: 19000, CompletionTokens: 1400, TotalTokens: 20400}
		maintenance := childUsage("s1", 1, 8400, want)
		maintenance.Maintenance = true
		tr.applyUsage(maintenance, window/2, "")

		if got := tr.entries[0].usage; got != want {
			t.Errorf("run totals = %+v, want %+v — the compaction's tokens were really spent", got, want)
		}
		if used, limit := fillOf(tr, 0); used != 12000 || limit != window {
			t.Errorf("run fill = %d/%d, want 12000/%d — a maintenance prompt is not the run's fill", used, limit, window)
		}
	})
}

// TestRunHeadPredicates pins the three questions the package asks of a sub-agent run head against
// the conjuncts the twelve call sites used to spell inline, so a site can no longer quietly ask a
// narrower or a wider one than it means: [entry.headsRun] is the kind AND the retained tool name,
// [entry.opensRun] narrows that by the CALL's own pairing, and [entry.headsRunFor] narrows it by
// the spawning call id.
func TestRunHeadPredicates(t *testing.T) {
	t.Parallel()

	// A run head as every site meets one: a committed sub_agent call, still open, carrying the id
	// its run is named by. Each case says only what it changes about that.
	head := func(mutate func(*entry)) entry {
		e := entry{kind: entryToolCall, callID: "s1", tool: toolView{Label: "Sub-Agent", name: subAgentToolName}}
		if mutate != nil {
			mutate(&e)
		}
		return e
	}

	t.Run("headsRun is the kind and the retained name", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name string
			e    entry
			want bool
		}{
			{"a sub_agent call block", head(nil), true},
			{"a call to any other tool", head(func(e *entry) { e.tool.name = "read_file" }), false},
			{"an entry of another kind carrying the name", head(func(e *entry) { e.kind = entryAssistant }), false},
			{"a third-party tool wearing the label", head(func(e *entry) { e.tool.name = "delegate" }), false},
			{"a delegation whose label was changed", head(func(e *entry) { e.tool.Label = "Delegate" }), true},
			{"a delegation already over", head(func(e *entry) { e.done = true }), true},
		} {
			if got := tc.e.headsRun(); got != tc.want {
				t.Errorf("%s: headsRun() = %v, want %v", tc.name, got, tc.want)
			}
		}
	})

	t.Run("opensRun reads the call's pairing and never the child's phase", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name string
			e    entry
			want bool
		}{
			{"an open head", head(nil), true},
			{"a head whose result was paired in", head(func(e *entry) { e.done = true }), false},
			{"a head whose child reported but whose result has not landed", head(func(e *entry) { e.phase = domain.SubAgentFinished }), true},
			{"a head running", head(func(e *entry) { e.phase = domain.SubAgentStarted }), true},
			{"an open call to another tool", head(func(e *entry) { e.tool.name = "read_file" }), false},
		} {
			if got := tc.e.opensRun(); got != tc.want {
				t.Errorf("%s: opensRun() = %v, want %v", tc.name, got, tc.want)
			}
		}
	})

	t.Run("headsRunFor narrows by the spawning call id", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			name  string
			e     entry
			spawn string
			want  bool
		}{
			{"the head the id names", head(nil), "s1", true},
			{"a sibling delegation", head(func(e *entry) { e.callID = "s2" }), "s1", false},
			{"a call to another tool carrying the id", head(func(e *entry) { e.tool.name = "read_file" }), "s1", false},
			{"an id asked of a head that carries none", head(func(e *entry) { e.callID = "" }), "", true},
			{"a named head asked for no id at all", head(nil), "", false},
		} {
			if got := tc.e.headsRunFor(tc.spawn); got != tc.want {
				t.Errorf("%s: headsRunFor(%q) = %v, want %v", tc.name, tc.spawn, got, tc.want)
			}
		}
	})
}

// ----------------------------------------------------------------------------
// A message addressed to a running child (ADR 0063)
// ----------------------------------------------------------------------------

// childMessage folds one delivery report for a message the human addressed to the run spawn
// spawned, at the child's own depth — the shape Agent.drainMailbox emits (domain.ChildInterjectionEvent).
func childMessage(tr *transcript, spawn, text string, depth int, landed bool) {
	tr.apply(domain.ChildInterjectionEvent{
		EventBase: domain.EventBase{Depth: depth, CallID: spawn},
		Input:     domain.UserInput{Text: text},
		Landed:    landed,
	})
}

// TestChildInterjectionLandsInsideItsRun pins the delivery fold: a message the child actually read
// becomes that child's own user block, inside its run and at the boundary it reached, and one that
// never got there becomes a host note instead. Both halves are what a human who was shown a message
// queued is owed — the account is never silence.
func TestChildInterjectionLandsInsideItsRun(t *testing.T) {
	t.Run("the entry carries the child's depth and run", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)

		childMessage(tr, "s1", "check the docs too", 1, true)

		if len(tr.entries) != 2 {
			t.Fatalf("transcript holds %d entries, want the head and the delivered message", len(tr.entries))
		}
		got := tr.entries[1]
		if got.kind != entryUser || got.text != "check the docs too" {
			t.Errorf("delivered entry = %v/%q, want an entryUser carrying the message", got.kind, got.text)
		}
		if got.depth != 1 || got.spawnCallID != "s1" {
			t.Errorf("delivered entry depth/spawn = %d/%q, want 1/\"s1\" — the run it was addressed to",
				got.depth, got.spawnCallID)
		}
	})

	t.Run("a collapsed run elides it with the rest of its span", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)

		childMessage(tr, "s1", "check the docs too", 1, true)

		// The head's own row and nothing else: the message went INSIDE the run, so the collapsed
		// run's elision covers it exactly as it covers the delegate's own work. The row reads as a
		// RUNNING delegation from here — the run has a span now, which is the same reading it takes
		// on its delegate's first committed entry (subAgentScheduled).
		want := strings.Join([]string{
			"✦ Sub-Agent",
			leaderEdgeRow("  ┕ survey the tests ⋯ 0 tool calls", glyphCollapsed),
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("collapsed run leaked the message addressed to it:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("with two siblings live it lands in its OWN run, not behind the last", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		readCall(tr, "c1", "a.go", 1, 5, 1)
		subAgentCall(tr, "s2", "survey the docs", 0)

		childMessage(tr, "s1", "check the docs too", 1, true)

		// Appended, the message would sit past s2's stretch and subAgentSpan would read it as s2's.
		// place puts it at the end of s1's own stretch instead: right behind the child's read call,
		// and in FRONT of the sibling head.
		if len(tr.entries) != 4 {
			t.Fatalf("transcript holds %d entries, want two heads, the child's read block and the message", len(tr.entries))
		}
		if got := tr.entries[2]; got.kind != entryUser || got.spawnCallID != "s1" {
			t.Errorf("entries[2] = %v/%q, want the message placed at the end of s1's stretch",
				got.kind, got.spawnCallID)
		}
		if got := tr.entries[3]; !got.headsRunFor("s2") {
			t.Errorf("entries[3] no longer heads s2's run (%v); the message was appended, not placed", got.kind)
		}
	})

	t.Run("it registers no sticky user block", func(t *testing.T) {
		th := newTheme(scheme.Default())
		tr := &transcript{}
		tr.addUser("delegate it", nil)
		subAgentCall(tr, "s1", "survey the tests", 0)

		childMessage(tr, "s1", "check the docs too", 1, true)

		// One stop, the human's own prompt: a message steering a delegate is drawn like a prompt
		// and walked past like a delegate's entry, so ctrl+↑/↓ offer only turns the reader started.
		blocks := tr.renderView(th, 80, false).userBlocks
		if len(blocks) != 1 {
			t.Errorf("renderView recorded %d user blocks, want 1 — the top-level prompt alone", len(blocks))
		}

		// Inside the run's own view, where the message IS painted, it claims no stop either: the
		// breadcrumb is the only sticky header a view has (ADR 0063).
		tr.setRoot(runRef{depth: 1, spawn: "s1"})
		view := tr.renderView(th, 80, false)
		if !strings.Contains(strings.Join(view.lines, "\n"), "check the docs too") {
			t.Fatalf("the run's view does not paint the message addressed to it:\n%s", strings.Join(view.lines, "\n"))
		}
		if len(view.userBlocks) != 0 {
			t.Errorf("the view recorded %d user blocks, want none: %+v", len(view.userBlocks), view.userBlocks)
		}
	})

	t.Run("a message that never landed is a note naming the delegation", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)

		childMessage(tr, "s1", "check the docs too", 1, false)

		if len(tr.entries) != 2 {
			t.Fatalf("transcript holds %d entries, want the head and the note", len(tr.entries))
		}
		got := tr.entries[1]
		const want = "sub-agent finished before your message landed"
		if got.kind != entryNote || got.text != want {
			t.Errorf("undelivered fold = %v/%q, want an entryNote reading %q", got.kind, got.text, want)
		}
	})

	t.Run("the note names a NAMED delegation", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{
			Call: domain.ToolCall{ID: "s1", Tool: "sub_agent",
				Arguments: []byte(`{"name":"repo-scout","task":"survey the tests"}`)},
		})

		childMessage(tr, "s1", "check the docs too", 1, false)

		const want = "repo-scout finished before your message landed"
		if got := tr.entries[len(tr.entries)-1]; got.text != want {
			t.Errorf("undelivered note = %q, want %q", got.text, want)
		}
	})
}
