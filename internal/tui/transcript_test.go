package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
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
	if tool.tool.Label != "Read File" || tool.tool.Target != "main.go" {
		t.Errorf("tool view = %+v; want a Read File / main.go header", tool.tool)
	}
	if tool.tool.Summary.Text != "1 - 1" || len(tool.tool.Details) != 0 {
		t.Errorf("tool outcome = %+v / %+v; want a \"1 - 1\" summary and no body", tool.tool.Summary, tool.tool.Details)
	}

	// (b) render snapshot: the grouped block in the new look — ✦-prefixed, one blank line
	// between blocks, the tool detail hanging off a ┕ branch.
	want := strings.Join([]string{
		"❯ read main.go",
		"",
		"✦ Let me read it.",
		"",
		"✦ Read File",
		"  ┕ main.go 1 - 1",
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
	if !strings.Contains(got, "✦ Read File") {
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
		"  Tool       Calls",
		"  ────────────────",
		"  Read File     12",
		"  Run            3",
	}, "\n")
	if got := plainRender(tr); got != want {
		t.Errorf("table block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A table wide enough to be fitted to the body column reaches that column's last cell on EVERY
// line — marker gutter included. This is the property the transcript's right-hand chrome is laid
// out against: the block is rendered to the same width the rest of the body wraps to, so the free
// column beside the scroll-bar gutter is the same beside a table row as beside the rule above it
// (model.go's transcriptWidth / bodyRightGutter, layout.md).
func TestTranscriptTableFillsTheBodyColumn(t *testing.T) {
	const width = 60
	tr := feed(domain.MessageEvent{Text: strings.Join([]string{
		"| File | Description of the change that was made | Status |",
		"| --- | --- | --- |",
		"| internal/tui/mdtable.go | the parser and the renderer | done |",
		"| layout.md | spec the block | fail |",
	}, "\n")})

	lines := tr.renderLines(newTheme(), width)

	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (header, rule, two rows): %#v", len(lines), visible(lines))
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
		"✦ Read File",
		"  ┕ main.go",
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
	if tr.pending != "thinking\n\n" {
		t.Errorf("pending = %q; want the buffer itself untouched by the display trim", tr.pending)
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

// entryDisplayStrings returns every string of an entry that can reach the terminal — the body, the
// skill chips, all five fields of a tool card, and the presentation entry's own facts. The
// assertions below walk THIS rather than e.text alone: the per-call-site discipline this test
// replaced passed while a tool card's target and summary carried live escapes, precisely because
// the assertion only looked at the text field.
func entryDisplayStrings(e entry) []string {
	out := []string{
		e.text,
		e.tool.Label, e.tool.Verb, e.tool.Target, e.tool.Summary.Text,
		e.presented.Title, e.presented.Path, e.presented.Location, e.presented.Reason,
		e.startup.Host, e.startup.Model,
	}
	out = append(out, e.skills...)
	for _, d := range e.tool.Details {
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
	for _, ln := range tr.renderLines(newTheme(), 80) {
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

	t.Run("attached skill display name", func(t *testing.T) {
		tr := &transcript{}
		tr.addUser("please review", []string{"Review" + osc52 + "Skill"})
		assertTranscriptNoESC(t, tr)
		if got := plainRender(tr); !strings.Contains(got, "Review") || !strings.Contains(got, "Skill") {
			t.Errorf("stripping ate the benign skill name:\n%s", got)
		}
	})

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
		if len(tr.entries[0].tool.Details) == 0 {
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
		if len(tr.entries[0].tool.Details) == 0 {
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

// The status line's tool phrase is built only from presentToolCall's view, so it inherits the tool
// card's seam. It is pinned here because foldActivity paints it the moment a call is ANNOUNCED —
// before any approval gate runs — which makes it the earliest point a hostile model's argument
// reaches the screen.
func TestToolActivityLabelCarriesNoEscape(t *testing.T) {
	label := toolActivityLabel(domain.ToolCall{
		Tool:      "terminal",
		Arguments: escapedArgs(t, "command", "npm "+escOSC52+"test"),
	})
	assertNoESCIn(t, "the activity label", label)
	if !strings.Contains(label, "running") || !strings.Contains(label, "npm ") {
		t.Errorf("stripping ate the benign label text: %q", label)
	}

	unknown := toolActivityLabel(domain.ToolCall{Tool: "mcp" + escCSI + "_thing"})
	assertNoESCIn(t, "the unregistered-tool activity label", unknown)
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
		assertNoESCIn(t, "a /skill picker row", item.cells...)
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
// The payload here is the CSI one, whose bytes are all printable once the ESCs are gone, so the
// composer can be compared verbatim (escOSC52 also carries a BEL, which the widget's sanitizer eats
// but stripEscapes deliberately leaves — stripEscapes removes the ESC introducer, nothing else).
func TestAcceptedFileRowMatchesItsValue(t *testing.T) {
	m := newTestModel(t)
	m.opts.Workspace = "/ws"
	m.files = &fileCache{
		root:    "/ws",
		files:   []string{"docs/no" + escCSI + "tes.md"},
		expires: time.Now().Add(time.Hour),
	}

	const draft = "read @docs/no"
	m.input.SetValue(draft)
	m.autocomplete = m.computeAutocomplete(len(draft))
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

// A Depth > 0 event renders as a framed sub-agent block: a ⤷ sub-agent label opens the run,
// and every line — header and the continuation lines of a multi-line body — is prefixed by
// the │ rail gutter, without crashing or corrupting the top-level layout.
func TestTranscriptDepthRendersFramedBlock(t *testing.T) {
	tr := feed(domain.ToolResultEvent{
		EventBase: domain.EventBase{Depth: 1},
		Result:    domain.ToolResult{Content: "line1\nline2"},
	})
	got := plainRender(tr)
	if !strings.HasPrefix(got, "│ ⤷ sub-agent") {
		t.Errorf("depth-1 run not opened by a ⤷ sub-agent label:\n%q", got)
	}
	if !strings.Contains(got, "│ ✦ result") {
		t.Errorf("depth-1 entry not framed by the rail:\n%q", got)
	}
	if !strings.Contains(got, "│   ┕ line2") {
		t.Errorf("continuation line of a depth-1 entry not framed by the rail:\n%q", got)
	}
}

// A nested event sequence (Depth 0 → 1 → 0) renders the sub-agent block framed and labelled
// while the parent stream stays intact and unframed (the P3.14 acceptance golden).
func TestTranscriptDepthNestedSequenceGolden(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 0}, Text: "delegating"})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "child work"})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 0}, Text: "back to parent"})

	want := strings.Join([]string{
		"✦ delegating",
		"",
		"│ ⤷ sub-agent",
		"│", // inside the run: the separator carries the rail
		"│ ✦ child work",
		"", // the climb-out joins at depth 0: the rail ends with the run's last line
		"✦ back to parent",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("nested-depth transcript mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The ⤷ sub-agent label opens once per descent and at each level: a 0→1→2 climb labels both
// the first and the second nesting level, framed by one and two rail gutters respectively.
func TestTranscriptDepthLabelsEachLevel(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1}, Text: "child"})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "grandchild"})

	want := strings.Join([]string{
		"│ ⤷ sub-agent",
		"│",
		"│ ✦ child",
		"│", // the 1→2 descent joins at depth 1: the outer rail alone
		"│ │ ⤷ sub-agent",
		"│ │",
		"│ │ ✦ grandchild",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("multi-level transcript mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
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
	if b.tool.Summary.Text != "1 - 10" || len(b.tool.Details) != 0 {
		t.Errorf("call b outcome = %+v / %+v; want a \"1 - 10\" summary and no body", b.tool.Summary, b.tool.Details)
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

// reset returns the transcript to its empty state — no committed entries, no in-progress buffer —
// but preserves the debug flag (a hidden view toggle, not conversation). It is the /clear + /new
// "start a new session" primitive; the caller re-seeds the start-up box afterwards.
func TestTranscriptReset(t *testing.T) {
	tr := &transcript{}
	tr.addStartup(startupView{Logo: "logo", Host: "host", Model: "model"})
	tr.addUser("hello", nil)
	tr.appendToken("streamed ") // sets streaming=true and grows pending
	tr.debug = true

	tr.reset()

	if n := len(tr.entries); n != 0 {
		t.Errorf("entries = %d after reset, want 0 (all committed entries dropped)", n)
	}
	if tr.pending != "" {
		t.Errorf("pending = %q after reset, want empty", tr.pending)
	}
	if tr.streaming {
		t.Error("streaming = true after reset, want false")
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
		{name: "error notice", build: func(tr *transcript) { tr.addError("loop", "upstream refused", 0) }},
		{name: "interjection with no opening prompt", build: func(tr *transcript) {
			tr.addInterjected("wrong file", nil)
		}},
		{name: "user message", build: func(tr *transcript) { tr.addUser("hello", nil) }, want: true},
		{name: "user message after a pile of chrome", build: func(tr *transcript) {
			tr.addStartup(startupView{Logo: "logo", Host: "host", Model: "model"})
			tr.addEphemeralNote("context: AGENTS.md")
			tr.addNote("confinement: workspace")
			tr.addError("loop", "upstream refused", 0)
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

// callEntry returns the tool-call entry with the given CallID, or nil.
func callEntry(tr *transcript, id string) *entry {
	for i := range tr.entries {
		if tr.entries[i].kind == entryToolCall && tr.entries[i].callID == id {
			return &tr.entries[i]
		}
	}
	return nil
}
