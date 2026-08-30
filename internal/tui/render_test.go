package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// Grouped same-label tool calls (tool-call layout item 4)
// ----------------------------------------------------------------------------

// readCall folds a read_file call and its result into tr, so a grouping test reads as the
// batch of reads it is meant to render. The result carries BOTH halves the real tool reports:
// the "showing lines from-to" prose the model reads, and the domain.ReadSpan the view renders
// its branch line from.
func readCall(tr *transcript, id, path string, from, to, depth int) {
	base := domain.EventBase{Depth: depth}
	tr.apply(domain.ToolCallEvent{
		EventBase: base,
		Call:      domain.ToolCall{ID: id, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)},
	})
	tr.apply(domain.ToolResultEvent{
		EventBase: base,
		Result: domain.ToolResult{
			CallID: id,
			Content: "[File: " + path + ", " + strconv.Itoa(to) + " lines total, showing lines " +
				strconv.Itoa(from) + "-" + strconv.Itoa(to) + "]\n…",
			Summary: domain.ReadSpan{Start: from, End: to, Total: to},
		},
	})
}

// groupMemberLine composes a collapsed member row the way the painter lays it out: the row's own
// text, then the ▶ flush against the block's right edge.
func groupMemberLine(text string) string { return leaderEdgeRow(text, glyphCollapsed) }

// leaderEdgeRow is that arithmetic for the indicator a LEADER row carries (leaderRow, render.go) —
// the ▶ of a collapsed one, the ▼ an open one wears on its first row. The field between the outcome
// slot and the mark is the constant groupIndicatorGap rather than a pad measured against the width,
// because a leader row fills its room exactly by construction: the dots take up whatever the target
// and the outcome leave, so nothing but the reserved field can stand at the end of one. A golden
// line then reads as the text it carries and stays true at any width.
func leaderEdgeRow(text, mark string) string {
	return text + strings.Repeat(" ", groupIndicatorGap) + mark
}

// memberEdgeRow is the same arithmetic for a mark that is NOT on a leader row and so must be padded
// out to the edge by hand — today the see-less marker closing an open member. One definition of
// "flush against the block's right edge", so a golden that moves because the edge moved fails
// everywhere at once instead of in one place.
func memberEdgeRow(t *testing.T, text, mark string, width int) string {
	t.Helper()
	th := newTheme(scheme.Default())
	pad := width - th.measure.Width(text) - th.measure.Width(mark)
	if pad < 0 {
		t.Fatalf("member row %q plus %q does not fit width %d", text, mark, width)
	}
	return text + strings.Repeat(" ", pad) + mark
}

// seeLessFooterLine is the row an expanded SINGLE block closes its body with, as a golden reads it:
// nothing but the see-less marker, flush against the block's right edge (seeLessFooter, render.go).
// It goes through memberEdgeRow so the two see-less rows in the transcript — the open member's, under
// its gutter, and this one — are held to one definition of that edge.
func seeLessFooterLine(t *testing.T, width int) string {
	t.Helper()
	return memberEdgeRow(t, "", promptSeeLess, width)
}

// ----------------------------------------------------------------------------
// The firing block: the tool block's shape under a static ⟳ (layout.md, "The firing block")
// ----------------------------------------------------------------------------

// firingBlock folds one Schedule's whole Firing into a fresh transcript — announced by its
// EventFired, closed by the Event that ends the run — so what these tests paint is the block the
// surface's own fold builds (schedule.go) rather than a hand-dressed view.
func firingBlock(answer string) *transcript {
	tr := &transcript{}
	tr.addFiring(schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Prompt: "check the log",
	})
	tr.enrichFiring(schedule.Event{
		Kind: schedule.EventCompleted, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Elapsed: 4 * time.Second,
		Outcome: schedule.Outcome{
			RecordID: "s1", Title: "nightly tidy — 14:05", FinalText: answer, Turns: 2,
		},
	})
	return tr
}

// The two states a Firing's reader cares about, in the shape layout.md gives them: collapsed, the
// block is its header and its branch, that branch's slot counting everything beneath it —
// what rode the BRANCH still shows, which is the whole point of following the outcome's two-halves
// grammar — and expanded, the block shows the answer whole with the prompt, the stats and the record
// pointer beneath it. It is one transcript toggled rather than two fixtures, because that is the
// claim: nothing about the entry changes but the flag the painter reads.
func TestFiringBlockCollapsesToItsRemainderCount(t *testing.T) {
	cases := []struct {
		name                        string
		answer                      string
		wantCollapsed, wantExpanded []string
	}{
		{
			name:   "a multi-line answer leads the body",
			answer: "found 3 stale entries\nremoved them",
			wantCollapsed: []string{
				"⟳ Schedule",
				groupMemberLine("  ┕ nightly tidy ⋯ +5 more lines"),
			},
			wantExpanded: []string{
				"⟳ Schedule",
				leaderEdgeRow("  ┕ nightly tidy ⋯", glyphExpanded),
				"    found 3 stale entries",
				"    removed them",
				"    prompt: check the log",
				"    2 turns · 4s",
				`    saved as "nightly tidy — 14:05" — find it in /sessions`,
			},
		},
		{
			name:   "a one-line answer fills the outcome slot on the Schedule's row",
			answer: "the log is clean",
			wantCollapsed: []string{
				"⟳ Schedule",
				groupMemberLine("  ┕ nightly tidy ⋯ the log is clean · +3 more lines"),
			},
			wantExpanded: []string{
				"⟳ Schedule",
				leaderEdgeRow("  ┕ nightly tidy ⋯ the log is clean", glyphExpanded),
				"    prompt: check the log",
				"    2 turns · 4s",
				`    saved as "nightly tidy — 14:05" — find it in /sessions`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := firingBlock(tc.answer)

			if got, want := renderPlain(tr, 80), strings.Join(tc.wantCollapsed, "\n"); got != want {
				t.Errorf("default paint mismatch (collapsed is the default):\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the firing block toggled")
			}
			// A Firing is painted by the tool block's own renderer, so its open body closes with the
			// same see-less footer (seeLessFooter, render.go).
			wantExpanded := append(append([]string(nil), tc.wantExpanded...), seeLessFooterLine(t, 80))
			if got, want := renderPlain(tr, 80), strings.Join(wantExpanded, "\n"); got != want {
				t.Errorf("expanded paint mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// The ⟳ is STATIC (layout.md, "The firing block"): the spinner belongs to the worker driving this
// session's Exchange and the session is idle while a Firing runs, so the header paints the same at
// both blink phases — most of all while the Firing is still going, which is the one frame a star
// would have blinked in.
func TestFiringBlockHeaderNeverBlinks(t *testing.T) {
	open := &transcript{}
	open.addFiring(schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy", Prompt: "check the log",
	})
	for _, tc := range []struct {
		name string
		tr   *transcript
		want string
	}{
		// The state indicator is orthogonal to the star and follows the ordinary toggle-target
		// rule: a collapsed block paints no body line at all, so the running Firing's one-line
		// prompt is as much hidden as the returned one's whole record and both wear the ▶ a click
		// acts on.
		{"a Firing still running", open, "⟳ Schedule"},
		{"a Firing that returned", firingBlock("the log is clean"), "⟳ Schedule"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := headerStar(t, tc.tr, false); got != tc.want {
				t.Errorf("header at the settled phase = %q, want %q", got, tc.want)
			}
			if got := headerStar(t, tc.tr, true); got != tc.want {
				t.Errorf("header at the flipped phase = %q, want %q", got, tc.want)
			}
		})
	}
}

// The block borrows the tool block's SHAPE and none of its meaning (ADR 0033), so neither derivation
// that folds entries into a bigger block may admit it: a Firing between two reads breaks their group
// instead of joining it, and no sub-agent span opens behind one. Both are pinned with a block
// DRESSED as exactly what each rule looks for — the reads' own label in the groupable shape, the
// sub-agent tool name over deeper entries — so a rule that stopped checking the entry kind fails
// here rather than quietly regrouping the transcript.
func TestFiringBlockJoinsNoToolGrouping(t *testing.T) {
	fired := schedule.Event{
		Kind: schedule.EventFired, ScheduleID: "sch-1", ScheduleName: "nightly tidy", Prompt: "check the log",
	}

	t.Run("it breaks a run of same-label calls", func(t *testing.T) {
		tr := &transcript{}
		readCall(tr, "c1", "main.go", 1, 154, 0)
		tr.addFiring(fired)
		readCall(tr, "c2", "util.go", 1, 42, 0)
		tr.entries[1].tool.Label = tr.entries[0].tool.Label
		tr.entries[1].tool.Details = toolBody{}

		if run := toolCallRun(tr.entries, 0); len(run) != 1 {
			t.Errorf("toolCallRun over the first read = %d views, want 1 — a Firing breaks the run", len(run))
		}
		if run := toolCallRun(tr.entries, 1); run != nil {
			t.Errorf("toolCallRun at the firing block = %v, want nil — it heads no group of its own", run)
		}
	})

	t.Run("it opens no sub-agent span", func(t *testing.T) {
		tr := &transcript{}
		tr.addFiring(fired)
		tr.entries[0].tool.name = subAgentToolName
		readCall(tr, "c1", "a.go", 1, 5, 1)

		if got := subAgentSpan(tr.entries, 0); got != 0 {
			t.Errorf("subAgentSpan at the firing block = %d, want 0 — no run hides behind a Firing", got)
		}
	})
}

// ----------------------------------------------------------------------------
// The whole-transcript layout golden (tool-call layout item 5)
// ----------------------------------------------------------------------------

// TestTranscriptLayoutGolden pins the whole rendered scrollback of one realistic mixed session —
// a user prompt, narration the model padded with a trailing "\n\n", a batch of reads, a Terminal
// call whose output hangs beneath its command, a diff whose "+2 −2" fills the outcome slot over a
// coloured body, two edits showing the lines they change beneath their own reports, a write showing
// the lines it writes beneath the "3 lines" its own slot states, an
// unregistered tool whose verbatim arguments are its own branches, an
// approval note, and a sub-agent read — as an exact line sequence, blank lines included. It is the
// backstop across the layout changes rather than a test of any one of them: the blank-line hygiene
// shows as the single separator row between every block — empty at the top level, the │ rail
// gutter inside the sub-agent run — and the bracketless bold-gold label as the header text.
//
// The eight calls in a row are ONE block now, the umbrella of docs/layout/tool-layout.md: five type
// rows in time order under "✦ Tools (8 calls)", each counting its run where the run holds more than
// one call ("Read (3)", "Replace (2)") and aggregating it in the outcome slot — the reads' 570 lines
// summed, the Replace run blank because a diffstat and a change count do not add up. The golden
// carries all three of the canon sketch's states at once: the rows collapsed, the Terminal row open
// to the call behind it under a │ gutter, and that call open to its output under a second one,
// closed by the see-less footer. The targetless mcp_search block is the run's breaker as well as the
// grammar's other shape — it can lead no member row, so it stands alone with its verbatim arguments
// as its own branches.
//
// The uniform shape shows as the fact that every header here — umbrella, standalone, railed — is a
// label and nothing else, with the target always leading its own branch row, the summary standing in
// the outcome slot flush against that row's right edge behind a leader, and the body beneath. The
// ▶/▼ at the right edge of every row that hides something — on the HEADER only in the targetless
// mcp_search block, which paints no leader row for it to sit at the edge of — and its absence
// everywhere else is the affordance rule in the same picture: exactly the rows here that hide
// something say so, the umbrella's own header wearing none because its floor is the type rows. A
// regression in any of them changes this golden, and the golden doubles as the living example of
// what the canon spec sketches.
func TestTranscriptLayoutGolden(t *testing.T) {
	tr := &transcript{}
	tr.addUser("read the docs, then run the tests", nil)
	tr.apply(domain.TokenEvent{Text: "Reading the docs first."})
	tr.apply(domain.TokenEvent{Text: "\n\n"}) // the model's own padding: trimmed at commit
	readCall(tr, "c1", "README.md", 1, 154, 0)
	readCall(tr, "c2", "TODO.md", 1, 408, 0)
	readCall(tr, "c3", "ISSUES.md", 1, 8, 0)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c4", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c4",
		Content: "ok   apogee/internal/tui     0.412s\nok   apogee/internal/agent   1.203s\nPASS\n",
	}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c5", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c5",
		Content: "  func main() {\n-     fmt.Println(\"old\")\n-     return\n+     fmt.Println(\"new\")\n+     os.Exit(0)\n  }",
		Summary: domain.DiffStat{Added: 2, Removed: 2},
	}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c6", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"main.go","oldText":"fmt.Println(\"old\")","newText":"fmt.Println(\"new\")"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c6", Content: "replaced text in main.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c7", Tool: "multi_find_and_replace",
		Arguments: []byte(`{"path":"main.go","replacements":[{"oldText":"return","newText":"os.Exit(0)"}]}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c7", Content: "applied 1 replacement to main.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c8", Tool: "write_file",
		Arguments: []byte(`{"path":"notes.md","content":"# Notes\n\nrewrote main.go\n"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c8",
		Content: "wrote 25 bytes to notes.md", Summary: domain.WroteBytes{Bytes: 25}}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c9", Tool: "mcp_search",
		Arguments: []byte(`{"query":"collapse","limit":20}`)}})
	tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow})
	readCall(tr, "c10", "main.go", 1, 154, 1)
	// The Terminal run is opened to its member and the member to its body, so the golden carries all
	// three of the canon sketch's states at once: the umbrella collapsed to its type rows, one row
	// open to the calls behind it, and one of those open to its output.
	if !tr.setTypeExpanded(5, true) || !tr.setExpanded(5, true) {
		t.Fatal("entries[5] is not the Terminal run's head — the fixture's indexing is wrong")
	}

	want := strings.Join([]string{
		"❯ read the docs, then run the tests",
		"",
		"✦ Reading the docs first.",
		"",
		"✦ Tools (8 calls)",
		groupMemberLine("  ┝ Read (3) ⋯ 570 lines"),
		leaderEdgeRow("  ┝ Terminal ⋯ exit 0", glyphExpanded),
		leaderEdgeRow("  │ ┕ go test ./... ⋯ exit 0", glyphExpanded),
		"  │ │ ok   apogee/internal/tui     0.412s",
		"  │ │ ok   apogee/internal/agent   1.203s",
		"  │ │ PASS",
		memberEdgeRow(t, "  │ │", promptSeeLess, 80),
		groupMemberLine("  ┝ Diff Preview ⋯ +2 −2"),
		groupMemberLine("  ┝ Replace (2) ⋯"),
		groupMemberLine("  ┕ Write ⋯ 3 lines"),
		"",
		"✦ mcp_search ▶",
		"  ┝ query:",
		"  ┕   collapse",
		"",
		"· approval allow: terminal",
		"",
		"│ ✦ Read",
		"│   ┕ main.go ⋯ 154 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("transcript layout mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// ----------------------------------------------------------------------------
// The streaming preview's tail bound (previewTailLines)
// ----------------------------------------------------------------------------

// streamingPreview is a transcript holding text as its in-flight buffer and nothing else — what a
// repaint sees mid-reply, and the one block a render can never serve from the paint cache
// (paintcache.go keys by entry index, and the live buffer is not an entry).
func streamingPreview(text string) *transcript {
	tr := &transcript{}
	tr.apply(domain.TokenEvent{Text: text})
	return tr
}

// numberedLines is n raw lines each naming its own index, so a paint can be asked which of them it
// kept. The index is zero-padded to a fixed width on purpose: every raw line is then exactly as
// wide as every other, so two buffers of the same LINE count wrap to the same ROW count and a row
// count is a statement about the bound rather than about digits.
func numberedLines(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("line ")
		b.WriteString(strconv.Itoa(100000 + i)[1:])
		b.WriteString("\n")
	}
	return b.String()
}

// A buffer far longer than the bound paints its LAST lines and none of its first: the preview is
// the tail of the reply, which is the only part of it the viewport can show.
func TestPreviewPaintsOnlyItsTail(t *testing.T) {
	th := newTheme(scheme.Default())
	const lines = previewTailLines * 4
	tr := streamingPreview(numberedLines(lines))

	painted := strip(strings.Join(tr.renderLines(th, 80), "\n"))
	for _, want := range []string{"line 01023", "line 00768"} { // the last line, and the first kept one
		if !strings.Contains(painted, want) {
			t.Errorf("preview of %d raw lines dropped %q — the tail is what is on screen:\n%s", lines, want, painted)
		}
	}
	for _, absent := range []string{"line 00000", "line 00767"} { // the buffer's first, and the last cut one
		if strings.Contains(painted, absent) {
			t.Errorf("preview of %d raw lines still paints %q — the whole buffer is being rendered:\n%s", lines, absent, painted)
		}
	}
}

// The bound stated as behaviour: a 10,000-line buffer costs the same paint as a buffer one line
// over the bound. What a repaint pays is a function of the screen, not of the reply's length —
// which is what removes the O(N²) term over a streaming turn.
func TestPreviewRowCountIsBounded(t *testing.T) {
	th := newTheme(scheme.Default())
	huge := streamingPreview(numberedLines(10000)).renderLines(th, 80)
	justOver := streamingPreview(numberedLines(previewTailLines+1)).renderLines(th, 80)

	if len(huge) != len(justOver) {
		t.Errorf("preview of 10,000 raw lines paints %d rows and of %d raw lines %d rows — the render is not bounded",
			len(huge), previewTailLines+1, len(justOver))
	}
}

// A buffer under the bound — every reply anyone actually reads — paints byte-identically to what
// it painted before the bound existed: the whole buffer, trailing blank lines held back.
func TestPreviewUnderTheBoundIsUnchanged(t *testing.T) {
	th := newTheme(scheme.Default())
	const text = "# Heading\n\nsome prose that is long enough to wrap once at this width, and then some.\n\n- a\n- b\n\n\n"

	if got, want := previewTail(text), trimTrailingBlankLines(text); got != want {
		t.Errorf("previewTail cut a sub-bound buffer:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
	want := renderEntryLines(th, paintInput{kind: entryAssistant, text: trimTrailingBlankLines(text)}, 80, false).lines
	if got := streamingPreview(text).renderLines(th, 80); !slices.Equal(got, want) {
		t.Errorf("preview frame changed for a sub-bound buffer:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// An empty buffer still renders its lone marker line, so the human sees that streaming has begun
// (the contract paintPreview has always carried).
func TestPreviewOfAnEmptyBufferKeepsItsMarker(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := &transcript{streaming: true}

	got := tr.renderLines(th, 80)
	want := renderEntryLines(th, paintInput{kind: entryAssistant}, 80, false).lines
	if !slices.Equal(got, want) || len(got) != 1 {
		t.Errorf("empty preview paints %d line(s) %q, want the lone marker %q", len(got), got, want)
	}
}

// previewTail's own edges, which the frame tests cover only indirectly: a buffer that is nothing
// but blank lines, one with no newline at all, and the trailing-blank trim landing exactly on the
// bound. None may panic, and none may return more than the bound.
func TestPreviewTailEdges(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"all blank", "\n\n  \n\n", ""},
		{"no newline at all", "one long unbroken line", "one long unbroken line"},
		{"trailing blanks only", "a\nb\n\n\n", "a\nb"},
		{"leading blank kept", "\na", "\na"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := previewTail(c.in); got != c.want {
				t.Errorf("previewTail(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}

	// Over the bound, the count is the bound exactly — trailing blank lines are held back BEFORE
	// the tail is taken, so they never spend lines the reader would otherwise see.
	over := numberedLines(previewTailLines+50) + "\n\n\n"
	if got := strings.Count(previewTail(over), "\n") + 1; got != previewTailLines {
		t.Errorf("previewTail kept %d raw lines, want the bound %d", got, previewTailLines)
	}
}

// ----------------------------------------------------------------------------
// A transcript rooted at ONE run — the run view's paint (ADR 0063)
// ----------------------------------------------------------------------------

// delegationCall folds the sub_agent call that opens a NAMED run, stamped with the run it is being
// made from: callID is the id the child's own events will carry, parent the run the call itself
// belongs to ("" for the human's own conversation). Both halves matter to a rooted paint — the id
// is what the root is named by, and the parent is the link the breadcrumb climbs.
func delegationCall(tr *transcript, parent, callID, name, task string, depth int) {
	args := `{"task":"` + task + `","name":"` + name + `"}`
	tr.apply(domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: depth, CallID: parent},
		Call:      domain.ToolCall{ID: callID, Tool: "sub_agent", Arguments: []byte(args)},
	})
}

// delegatedRead folds one read_file call and its result as the work OF a run: the EventBase carries
// both halves of the run identity (ADR 0039), which is what the rooted paint reads to tell one
// child's entries from its sibling's.
func delegatedRead(tr *transcript, spawn, callID, path string, depth int) {
	base := domain.EventBase{Depth: depth, CallID: spawn}
	tr.apply(domain.ToolCallEvent{
		EventBase: base,
		Call:      domain.ToolCall{ID: callID, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)},
	})
	tr.apply(domain.ToolResultEvent{
		EventBase: base,
		Result: domain.ToolResult{
			CallID:  callID,
			Content: "[File: " + path + ", 5 lines total, showing lines 1-5]\n…",
			Summary: domain.ReadSpan{Start: 1, End: 5, Total: 5},
		},
	})
}

// rootedFixture is one conversation with two live delegations in it: the human's prompt, a running
// "repo-scout" that has read a file and delegated onwards to "deep-dive", and a sibling "sweeper"
// beside it. It is the shape every question about a rooted paint needs — a parent above, a sibling
// beside, a nested run within — and the run it returns the ref of is the one a view opens on.
func rootedFixture() (*transcript, runRef) {
	tr := &transcript{}
	tr.addUser("what changed?", nil)
	delegationCall(tr, "", "s1", "repo-scout", "scout the repo", 0)
	delegatedRead(tr, "s1", "r1", "a.go", 1)
	delegationCall(tr, "s1", "s2", "deep-dive", "read the tests", 1)
	delegatedRead(tr, "s2", "r2", "b.go", 2)
	delegationCall(tr, "", "s3", "sweeper", "sweep the docs", 0)
	delegatedRead(tr, "s3", "r3", "c.go", 1)
	return tr, runRef{depth: 1, spawn: "s1"}
}

// A rooted paint IS the run view: the child's own entries, painted as if that child were the whole
// conversation. It opens with the prompt the child was handed, its work stands at the top level with
// no rail to say otherwise, its live tail follows, and everything else in the transcript — the
// human's prompt above it, the sibling delegation beside it, the nested run's own span — is not
// there at all.
func TestRootedPaintShowsOneRunAndNothingElse(t *testing.T) {
	tr, root := rootedFixture()
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "still looking"})
	tr.setRoot(root)
	got := renderPlain(tr, 80)

	for _, want := range []string{
		"← main › repo-scout", // the head became the header
		"scout the repo",      // the prompt the run was handed, as its first user row
		"a.go",                // its own work
		"deep-dive",           // the run it opened, as a row
		"still looking",       // its live tail
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the rooted paint does not show %q:\n%s", want, got)
		}
	}
	for _, absent := range []string{
		"what changed?", // the parent's prompt
		"sweeper",       // a sibling delegation
		"c.go",          // that sibling's work
		"b.go",          // the nested run's span, which its collapsed row elides
		glyphSubRail,    // every row of the view stands at the top level
	} {
		if strings.Contains(got, absent) {
			t.Errorf("the rooted paint shows %q, which belongs to another run:\n%s", absent, got)
		}
	}
}

// The breadcrumb is the ONLY sticky header inside a view. The child's task row is a user block by
// paint and not by accounting: it never freezes at the top of the viewport, because the thing a
// reader needs frozen in a run view is the way back out.
func TestRootedPaintRegistersNoUserBlock(t *testing.T) {
	tr, root := rootedFixture()
	tr.setRoot(root)
	view := tr.renderView(newTheme(scheme.Default()), 80, false)

	if len(view.userBlocks) != 0 {
		t.Errorf("the rooted paint registered %d user blocks, want none: %+v", len(view.userBlocks), view.userBlocks)
	}
	if want := (userBlock{start: 0, count: 1}); view.header != want {
		t.Errorf("the rooted paint's header span = %+v, want %+v", view.header, want)
	}
	if view.targets[0].kind != targetBreadcrumb {
		t.Errorf("line 0 is a %v, want the breadcrumb's own kind", view.targets[0].kind)
	}
}

// A view is opened and left, and leaving it must cost the reader nothing: the transcript rooted back
// at the zero value paints byte for byte what it painted before the view was ever opened — the same
// lines, the same click surface, the same user blocks, and no header of its own.
func TestUnrootedPaintIsUnchangedByAVisit(t *testing.T) {
	tr, root := rootedFixture()
	tr.paints = newPaintCache()
	th := newTheme(scheme.Default())

	before := tr.renderView(th, 80, false)
	tr.setRoot(root)
	tr.renderView(th, 80, false)
	tr.setRoot(runRef{})
	after := tr.renderView(th, 80, false)

	sameRender(t, "back at the top level", after, before)
	if !slices.Equal(after.userBlocks, before.userBlocks) {
		t.Errorf("user blocks = %+v after the visit, want %+v", after.userBlocks, before.userBlocks)
	}
	if after.header != (userBlock{}) {
		t.Errorf("the unrooted paint published a header span %+v, want none", after.header)
	}
}

// The breadcrumb reaches the screen the way every sticky header does — as a Model overlay frozen at
// row 0 over the lines the paint laid down (applyStickyHeader) — and not as a row the renderer
// happens to draw first. It reads as the spec words it: the trail in the body column, the key that
// leaves the view held two columns off the right edge, the very margin the status line's right slot
// keeps below it (layout.md, "The status line's right slot").
func TestRunViewHeaderIsDrawnByTheStickyOverlay(t *testing.T) {
	tr, root := rootedFixture()
	m := newTestModel(t)
	m.transcript = *tr
	m.transcript.setRoot(root)
	m.refreshViewport()

	start, count := m.stickyHeaderSpan()
	if start != 0 || count != 1 {
		t.Fatalf("sticky header span = (%d, %d), want the breadcrumb at (0, 1)", start, count)
	}
	row := strip(m.lines[0])
	if want := bodyIndent + "← main › repo-scout"; !strings.HasPrefix(row, want) {
		t.Errorf("the header row is %q; want it to lead with %q", row, want)
	}
	if want := breadcrumbHint + bodyIndent; !strings.HasSuffix(row, want) {
		t.Errorf("the header row is %q; want it to end with %q", row, want)
	}
	if got, want := m.th.measure.Width(row), m.transcriptWidth(); got != want {
		t.Errorf("the header row is %d columns wide, want the transcript's own %d", got, want)
	}
	if drawn := strings.Split(m.applyStickyHeader("a\nb\nc"), "\n")[0]; drawn != m.lines[0] {
		t.Errorf("the overlay drew %q at row 0, want the breadcrumb %q", strip(drawn), row)
	}
}
