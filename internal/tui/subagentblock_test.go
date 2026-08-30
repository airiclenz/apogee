package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// ----------------------------------------------------------------------------
// Sub-agent framing reflow safety (P3.14)
// ----------------------------------------------------------------------------

// A Depth > 0 block renders at a tiny and a zero width without panicking (the acceptance's
// "reflow at small sizes doesn't panic"), the framed text is still produced, and — the part that
// makes this more than a smoke test — every line it draws obeys layout.md's absolute cap. The cap
// has a floor: a Depth-2 block cannot be drawn in fewer columns than its two rail gutters, its
// marker and one column of text, so below that the bound is the floor rather than the width. The
// fixture's hyphen ("sub-agent") is the point — it is exactly the breakpoint run the wrapper used
// to grow past the limit.
func TestSubAgentReflowAtSmallWidths(t *testing.T) {
	th := newTheme(scheme.Default())
	const depth = 2
	floor := depth*railWidth + th.measure.Width(glyphAssistant+" ") + 1

	for _, width := range []int{0, 1, 2, 3, 6, 12, 40} {
		tr := feed(domain.MessageEvent{
			EventBase: domain.EventBase{Depth: depth},
			Text:      "a deeply nested sub-agent message that must wrap hard",
		})
		lines := tr.renderLines(th, width) // must not panic at any width
		if len(lines) == 0 {
			t.Errorf("width %d produced no lines", width)
		}
		bound := max(width, floor)
		for i, ln := range lines {
			if w := th.measure.Width(ln); w > bound {
				t.Errorf("width %d: line %d %q is %d cells wide, over the %d cap", width, i, strip(ln), w, bound)
			}
		}
	}
}

// A fan-out reads as ONE list: adjacent delegations fold under "✦ Sub-Agent (3)", one row per agent
// in the shape every folded member takes (docs/layout/tool-layout.md, Rules). What a row SAYS is
// the lone block's own collapsed reading (collapsedSubAgentView) — the work behind it and the gist
// its delegate reported — because folding changes the frame around a delegation, not the record.
//
// Opening one reveals its SPAN rather than a body, and the frame the spec draws around it: the row
// becomes a ┌─┶ header at the very left of the block, the span runs behind a column-0 │ rail, and
// one ┊ closes it before the list resumes — its last row still closing the whole group with ┕. That
// interruption is why the group's remaining rows are painted in a second block, and it is what this
// golden pins. A FINISHED delegation carries a ✓ after its name in both states (design call 6).
func TestRenderSubAgentGroupSketchStates(t *testing.T) {
	// The three delegations stand at entries 0, 2 and 4 — each with one nested read between them.
	const secondHead = 2

	build := func(t *testing.T) *transcript {
		t.Helper()
		tr := &transcript{}
		for _, d := range [][3]string{
			{"s1", "survey", "a.go"},
			{"s2", "build", "b.go"},
			{"s3", "check", "c.go"},
		} {
			subAgentCall(tr, d[0], d[1], 0)
			readCall(tr, "r"+d[0], d[2], 1, 5, 1)
			subAgentReport(tr, d[0], "all clear", 0)
		}
		return tr
	}
	header := "✦ Sub-Agent (3)"
	rows := []string{
		groupMemberLine("  ┝ survey ✓ ⋯ 1 tool call · all clear"),
		groupMemberLine("  ┝ build ✓ ⋯ 1 tool call · all clear"),
		groupMemberLine("  ┕ check ✓ ⋯ 1 tool call · all clear"),
	}

	t.Run("collapsed to one row per agent", func(t *testing.T) {
		want := strings.Join(append([]string{header}, rows...), "\n")
		if got := renderPlain(build(t), 80); got != want {
			t.Errorf("collapsed fan-out mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	// A framed member has no second shape in the list: expanding one opens its RUN VIEW (ADR 0063),
	// so the state that used to open a rail under the row is refused outright and the group paints
	// exactly the rows it painted shut — no ┌─┶, no railed span, no prompt.
	t.Run("a framed member refuses to open a rail in place", func(t *testing.T) {
		tr := build(t)
		if tr.setExpanded(secondHead, true) {
			t.Fatalf("setExpanded(%d, true) = true; a run opens as a view, never as a rail", secondHead)
		}
		want := strings.Join(append([]string{header}, rows...), "\n")
		got := renderPlain(tr, 80)
		if got != want {
			t.Errorf("framed member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
		if strings.Contains(got, subAgentOpenMarker) {
			t.Errorf("the group drew the open marker %q:\n%s", subAgentOpenMarker, got)
		}
	})

	// The ✓ is a report that came off, and nothing else says so: a delegation still working has not
	// reported at all, and one that reported a failure is marked by its red outcome slot alone
	// (design call 6). Both are pinned here against the very rows the goldens above carry the mark on.
	t.Run("no ✓ while running and none on failure", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "working", 0)
		readCall(tr, "r1", "a.go", 1, 5, 1)
		subAgentCall(tr, "s2", "broken", 0)
		readCall(tr, "r2", "b.go", 1, 5, 1)
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "s2", Content: "it fell over", IsError: true}})

		want := strings.Join([]string{
			"✦ Sub-Agent (2)",
			groupMemberLine("  ┝ working ⋯ 1 tool call"),
			groupMemberLine("  ┕ broken ⋯ 1 tool call · error: it fell over"),
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("unfinished fan-out mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// A fan-out's results arrive as ONE trailing burst, in call order, once every child has joined (ADR
// 0039 decision 4) — so the pairing that marks a call done says nothing about when that delegation
// actually stopped working, and a member that finished first would read as working for as long as
// its slowest sibling ran. Its own finished phase is what says otherwise
// (domain.SubAgentPhaseEvent), and this is the difference that makes on screen: the ✓ and the "done"
// appear on THAT row while the sibling beside it is still going, and the report the phase carried is
// what its run view opens on (ADR 0063) — all before either call has been paired.
//
// The burst then adds nothing at all, which is the other half of the claim: the payload already rode
// the phase, and folding it a second time would print the report twice (transcript.addToolResult).
func TestSubAgentMemberDoneOnItsOwnFinishedPhase(t *testing.T) {
	// Two lines: the report lays out as the delegation's BODY, which leaves the slot to say the one
	// word docs/layout/tool-layout.md gives a finished delegation — and gives the open state a body
	// to show.
	const report = "found the bug\nin the parser"
	finish := func(tr *transcript) {
		tr.apply(domain.SubAgentPhaseEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
			Phase:     domain.SubAgentFinished,
			Result:    domain.ToolResult{CallID: "s1", Content: report},
		})
	}
	build := func(t *testing.T, burst bool) *transcript {
		t.Helper()
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey", 0)
		readCall(tr, "rs1", "a.go", 1, 5, 1)
		subAgentCall(tr, "s2", "build", 0)
		readCall(tr, "rs2", "b.go", 1, 5, 1)
		finish(tr)
		if burst {
			subAgentReport(tr, "s1", report, 0)
		}
		return tr
	}
	collapsed := strings.Join([]string{
		"✦ Sub-Agent (2)",
		groupMemberLine("  ┝ survey ✓ ⋯ 1 tool call · done"),
		groupMemberLine("  ┕ build ⋯ 1 tool call"), // still working: no ✓, and no gist of its own
	}, "\n")
	for _, tc := range []struct {
		name  string
		burst bool
	}{
		{name: "before the trailing result burst", burst: false},
		{name: "after the trailing result burst", burst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderPlain(build(t, tc.burst), 80); got != collapsed {
				t.Errorf("collapsed mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
			}
			// The row is the delegation's whole shape here, so the phase's verdict has nowhere else
			// to be read: the report it carried is what the run's VIEW opens on (ADR 0063), and the
			// fold that used to lay it out under the row is refused.
			tr := build(t, tc.burst)
			if tr.setExpanded(0, true) {
				t.Fatal("setExpanded(0, true) = true; a finished run opens as a view, never as a rail")
			}
			if got := renderPlain(tr, 80); got != collapsed {
				t.Errorf("row moved under a refused expand:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
			}
		})
	}
}

// A fan-out wider than the Parallel agents cap emits every delegation's call up front and starts
// only as many children as it has slots (ADR 0039), so the rows past the cap stand for work that has
// not begun. This is what such a row says and what it does: the one word "scheduled" in the outcome
// slot — no count of tool calls, no context fill, no gist, none of which exist yet — no indicator,
// and no click target at all, since there is nothing behind it to open. Its own started phase is
// what ends that (domain.SubAgentPhaseEvent), and the row is an ordinary live delegation from there.
func TestSubAgentScheduledUntilItStarts(t *testing.T) {
	// Two members running with a call apiece, and a third the cap held back.
	build := func(t *testing.T) *transcript {
		t.Helper()
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey", 0)
		subAgentStarted(tr, "s1", 1)
		readCall(tr, "rs1", "a.go", 1, 5, 1)
		subAgentCall(tr, "s2", "build", 0)
		subAgentStarted(tr, "s2", 1)
		readCall(tr, "rs2", "b.go", 1, 5, 1)
		subAgentCall(tr, "s3", "check", 0)
		return tr
	}
	header := "✦ Sub-Agent (3)"
	running := []string{
		groupMemberLine("  ┝ survey ⋯ 1 tool call"),
		groupMemberLine("  ┝ build ⋯ 1 tool call"),
	}

	t.Run("queued member says scheduled and wears no indicator", func(t *testing.T) {
		want := strings.Join(append(append([]string{header}, running...),
			"  ┕ check ⋯ scheduled"), "\n") // no ▶: the row hides nothing
		if got := renderPlain(build(t), 80); got != want {
			t.Errorf("scheduled member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("queued member is not a click target", func(t *testing.T) {
		lines, targets := targetedRender(build(t), 80)
		if got := targets[rowWith(t, lines, "check")].kind; got != targetNone {
			t.Errorf("scheduled row is target kind %v, want targetNone", got)
		}
		// The contrast, off the same paint: a member with a run behind it does open.
		if got := targets[rowWith(t, lines, "survey")].kind; got != targetHeader {
			t.Errorf("running row is target kind %v, want targetHeader", got)
		}
	})

	t.Run("its start ends the scheduled row", func(t *testing.T) {
		tr := build(t)
		subAgentStarted(tr, "s3", 1)
		want := strings.Join(append(append([]string{header}, running...),
			"  ┕ check ⋯"), "\n") // running with nothing done yet: the ordinary live reading
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("started member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("started member with work is an ordinary expandable row", func(t *testing.T) {
		tr := build(t)
		subAgentStarted(tr, "s3", 1)
		readCall(tr, "rs3", "c.go", 1, 5, 1)
		want := strings.Join(append(append([]string{header}, running...),
			groupMemberLine("  ┕ check ⋯ 1 tool call")), "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("started member with work mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
		lines, targets := targetedRender(tr, 80)
		if got := targets[rowWith(t, lines, "check")].kind; got != targetHeader {
			t.Errorf("started row is target kind %v, want targetHeader", got)
		}
	})

	// A delegation REFUSED at the depth bound or failed by a hook never runs, so its started phase
	// never comes — but its result does (internal/agent/dispatch.go). A rule reading the missing
	// phase alone would leave that row queued for the rest of the session; being over is the other
	// thing that ends the state (subAgentScheduled).
	t.Run("a refused delegation is not scheduled", func(t *testing.T) {
		tr := build(t)
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "s3", Content: "sub-agent depth limit reached", IsError: true}})
		want := strings.Join(append(append([]string{header}, running...),
			"  ┕ check ⋯ error: sub-agent depth limit reached"), "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("refused member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// targetedRender renders a transcript the way [renderPlain] does and hands back the click surface
// beside the lines it belongs to — the accounting the paint itself built (renderedTranscript), so a
// test asks what a click would resolve to rather than re-deriving it from the text.
func targetedRender(tr *transcript, width int) ([]string, []lineTarget) {
	view := tr.renderView(newTheme(scheme.Default()), width, false, breadcrumbHint)
	lines := make([]string, len(view.lines))
	for i, ln := range view.lines {
		lines[i] = strings.TrimRight(collapseLeader(ansiPattern.ReplaceAllString(ln, "")), " ")
	}
	return lines, view.targets
}

// rowWith is the index of the ONE painted row carrying text, and a fatal error where there is no
// such row or more than one: a target assertion made against a row the paint never emitted would
// pass by finding nothing at all.
func rowWith(t *testing.T, lines []string, text string) int {
	t.Helper()
	found := -1
	for i, ln := range lines {
		if !strings.Contains(ln, text) {
			continue
		}
		if found >= 0 {
			t.Fatalf("%q appears on rows %d and %d; want exactly one", text, found, i)
		}
		found = i
	}
	if found < 0 {
		t.Fatalf("no painted row carries %q:\n%s", text, strings.Join(lines, "\n"))
	}
	return found
}

// loneDelegation folds one delegation with a nested read behind it, reported unless report is "".
// It is deliberately the same fixture shape the fan-out golden above builds, so the two shapes are
// compared on the same delegation rather than on two that merely look alike.
func loneDelegation(tr *transcript, id, task, path, report string) {
	subAgentCall(tr, id, task, 0)
	readCall(tr, "r"+id, path, 1, 5, 1)
	if report != "" {
		subAgentReport(tr, id, report, 0)
	}
}

// A LONE delegation wears the very ROW a grouped one does (design call 3 of
// docs/plans/"2026-08-11 - 01"): folding changes what stands AROUND a delegation and never the
// delegation itself, so the row a reader learned to read in a fan-out reads the same when the run
// happened to stand by itself. Under ADR 0063 that row is the whole of the shape either way — a run
// opens as a VIEW and never as a rail in place, so neither shape has a frame of its own to differ in.
func TestLoneSubAgentRunWearsTheGroupMembersRow(t *testing.T) {
	// The claim is stronger than "it looks like the sketch": the two rows are compared BYTE FOR BYTE
	// on the same delegation, lone and grouped, rather than restated as a second golden — two
	// goldens is exactly how the two shapes would come to disagree.
	t.Run("row is the grouped member's, to the byte", func(t *testing.T) {
		lone := &transcript{}
		loneDelegation(lone, "s1", "survey", "a.go", "all clear")
		// The survey stands LAST in the group, which is where a lone run stands in its own list of
		// one: the branch marker is a fact about the row's POSITION (┝ mid-list, ┕ last), and this
		// claim is about everything else on the row.
		grouped := &transcript{}
		loneDelegation(grouped, "s2", "build", "b.go", "all clear")
		loneDelegation(grouped, "s1", "survey", "a.go", "all clear")

		// Line 0 is the block header ("✦ Sub-Agent", "✦ Sub-Agent (2)"), then one row per member.
		row := strings.Split(renderPlain(lone, 80), "\n")[1]
		if member := strings.Split(renderPlain(grouped, 80), "\n")[2]; row != member {
			t.Errorf("lone run's row = %q; want the grouped member's %q", row, member)
		}
	})

	// A lone run refuses to open in place exactly as a grouped member does, and its span stays
	// elided: what expanding it opens is its run view (ADR 0063).
	t.Run("it refuses to open a rail of its own", func(t *testing.T) {
		tr := &transcript{}
		loneDelegation(tr, "s1", "survey", "a.go", "all clear")
		tr.apply(domain.MessageEvent{Text: "back to parent"})

		before := renderPlain(tr, 80)
		if tr.setExpanded(0, true) {
			t.Fatal("setExpanded(0, true) = true; a run opens as a view, never as a rail")
		}
		want := strings.Join([]string{
			"✦ Sub-Agent",
			groupMemberLine("  ┕ survey ✓ ⋯ 1 tool call · all clear"),
			"",
			"✦ back to parent",
		}, "\n")
		if got := renderPlain(tr, 80); got != want || got != before {
			t.Errorf("lone run mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	// The ✓ says a report came off, in the lone shape exactly as in the folded one: a delegation
	// still working has not reported at all, and one that reported a failure is marked by its red
	// outcome slot alone (design call 6).
	t.Run("no ✓ while running and none on failure", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			build func(tr *transcript)
			row   string
		}{
			{
				name:  "still working",
				build: func(tr *transcript) { loneDelegation(tr, "s1", "working", "a.go", "") },
				row:   "  ┕ working ⋯ 1 tool call",
			},
			{
				name: "reported a failure",
				build: func(tr *transcript) {
					loneDelegation(tr, "s1", "broken", "a.go", "")
					tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
						CallID: "s1", Content: "it fell over", IsError: true}})
				},
				row: "  ┕ broken ⋯ 1 tool call · error: it fell over",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tr := &transcript{}
				tc.build(tr)

				want := strings.Join([]string{"✦ Sub-Agent", groupMemberLine(tc.row)}, "\n")
				if got := renderPlain(tr, 80); got != want {
					t.Errorf("row mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
				}
			})
		}
	})
}

// A delegation's row carries its whole reading — the run's own count of the work, the delegate's
// context fill and its gist (docs/layout/tool-layout.md, Grouped Sub-agents) — and keeps it however
// the reader reaches for the run behind it. Under ADR 0063 that reach opens the run's VIEW, so the
// row is what stays in the list: expanding may never take a cell away from it, and here it takes
// nothing at all.
//
// The claim is pinned by CONSTRUCTION rather than by two goldens that could drift: the slot text is
// written once and stands in the golden and in the after-the-expand comparison alike, running
// member and finished member both.
func TestFramedSubAgentRowKeepsItsTopLevelDetails(t *testing.T) {
	// The first delegation is still working and has a reading of its own; the second has reported.
	// The usage lands while only the first is open, which is the run it is attributed to.
	build := func(t *testing.T) *transcript {
		t.Helper()
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey", 0)
		readCall(tr, "rs1", "a.go", 1, 5, 1)
		subAgentUsage(tr, 1, 12000, 32768)
		subAgentCall(tr, "s2", "build", 0)
		readCall(tr, "rs2", "b.go", 1, 5, 1)
		subAgentReport(tr, "s2", "all clear", 0)
		return tr
	}
	const (
		header  = "✦ Sub-Agent (2)"
		working = "survey ⋯ 1 tool call · 12k/32k"    // count and fill, and no action phrase
		done    = "build ✓ ⋯ 1 tool call · all clear" // the same line, closed by the report's gist
	)

	collapsed := strings.Join([]string{
		header,
		groupMemberLine("  ┝ " + working),
		groupMemberLine("  ┕ " + done),
	}, "\n")
	if got := renderPlain(build(t), 80); got != collapsed {
		t.Errorf("group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
	}

	for _, tc := range []struct {
		name string
		head int
	}{
		{name: "a running member keeps its count and fill", head: 0},
		{name: "a finished member keeps its count and gist", head: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := build(t)
			if tr.setExpanded(tc.head, true) {
				t.Fatalf("setExpanded(%d, true) = true; a run opens as a view, never as a rail", tc.head)
			}
			if got := renderPlain(tr, 80); got != collapsed {
				t.Errorf("row moved under a refused expand:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
			}
		})
	}
}

func TestSpanlessSubAgentHeadsGroupWithEachOther(t *testing.T) {
	const refusal = "sub-agent depth limit reached (max 2): cannot spawn a deeper sub-agent"
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"first"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "s1", Content: refusal, IsError: true}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "s2", Tool: "sub_agent", Arguments: []byte(`{"task":"second"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "s2", Content: refusal, IsError: true}})

	for i := range tr.entries {
		if span := subAgentSpan(tr.entries, i); span != 0 {
			t.Fatalf("entry %d heads a span of %d; the premise here is a head with none", i, span)
		}
	}
	if run := toolCallRun(tr.entries, 0); run != nil {
		t.Fatalf("toolCallRun over the two refused delegations = %d views, want none — a solo call heads no run", len(run))
	}

	want := strings.Join([]string{
		// The refusal fills the whole outcome slot, so the leader keeps its floor of one dot and
		// the target gives way entirely — design call 4's order, played out to its end.
		"✦ Sub-Agent (2)",
		"  ┝ ⋯ error: sub-agent depth limit reached (max 2): cannot spawn a deeper su" + clipTail,
		"  ┕ ⋯ error: sub-agent depth limit reached (max 2): cannot spawn a deeper su" + clipTail,
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("refused delegations mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// ----------------------------------------------------------------------------
// A delegation the record caught mid-run (delegation progress save §3)
// ----------------------------------------------------------------------------

// A head closed at replay because the record was written while its child was still working
// (closeInterruptedCalls) is REPORTED — its result is paired and there is nothing left to wait for —
// but it is not FINISHED: it never reported anything, so it wears no done ✓ and its outcome slot
// carries the interrupted verdict instead (failedSummary reads it as one). The block is not live
// either: a run whose work died with the engine that was running it must not go on blinking at a
// reader who resumed it hours later.
func TestSubAgentInterruptedHeadIsNotFinished(t *testing.T) {
	tr := &transcript{}
	subAgentCall(tr, "s1", "survey", 0)
	readCall(tr, "r1", "a.go", 1, 5, 1) // the child's work, settled — the head above it is not

	blob, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	entries, err := decodeTranscript(blob)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	if closed := closeInterruptedCalls(entries); closed != 1 {
		t.Fatalf("closed = %d, want the one open head", closed)
	}

	head := entries[0].painted()
	if !subAgentReported(head) {
		t.Error("subAgentReported = false; a closed head has nothing left to wait for")
	}
	if subAgentFinished(head) {
		t.Error("subAgentFinished = true; an interrupted run never reported and must wear no ✓")
	}

	replayed := &transcript{}
	replayed.replay(entries)
	settled := replayed.renderView(newTheme(scheme.Default()), 80, false, breadcrumbHint)
	blinked := replayed.renderView(newTheme(scheme.Default()), 80, true, breadcrumbHint)
	if !equalLines(settled.lines, blinked.lines) {
		t.Error("the blink phase repainted the run; an interrupted delegation must wear no running star")
	}

	painted := renderPlain(replayed, 80)
	if strings.Contains(painted, glyphDone) {
		t.Errorf("the interrupted run wears the done ✓:\n%s", painted)
	}
	if !strings.Contains(painted, interruptedSummary) {
		t.Errorf("the interrupted run does not say what became of it:\n%s", painted)
	}
}

// ----------------------------------------------------------------------------
// A failed delegation's outcome slot (surfaces-that-lie plan, item 12)
// ----------------------------------------------------------------------------

// A delegation that FAILED paints its outcome slot red wherever it is drawn — lone or grouped, shut
// or open — and wears no done ✓, since design call 6 makes that red the whole of the failure
// marking. The row's line is a COMPOSED reading, "1 tool call · error: …", whose own opening words
// say nothing about how the run ended: the verdict is the HEAD's, carried onto that line
// (subAgentSummary), and reading the composed words instead is what left every failed delegation
// painted in the ordinary tone (F-28).
//
// A report that merely MENTIONS an error past its first word is the other half of the claim: the
// vocabulary is anchored at the start of the head's own summary, so such a run keeps the ordinary
// tone and the ✓ it earned.
func TestFailedDelegationPaintsItsSlotRed(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	// Wide enough that no row has to give up its outcome slot: what is under test is the slot's
	// tone, and a clipped slot would assert the geometry instead.
	const width = 100

	// paintedRow is the ONE painted row carrying text, styling and all — a fatal error where there
	// is no such row or more than one, so an assertion cannot pass by finding nothing.
	paintedRow := func(t *testing.T, tr *transcript, text string) string {
		t.Helper()
		found := ""
		for _, ln := range tr.renderLines(th, width) {
			if !strings.Contains(strip(ln), text) {
				continue
			}
			if found != "" {
				t.Fatalf("two painted rows carry %q", text)
			}
			found = ln
		}
		if found == "" {
			t.Fatalf("no painted row carries %q", text)
		}
		return found
	}
	assertSlot := func(t *testing.T, row, slot string, wantRed, wantDone bool) {
		t.Helper()
		if got := strings.Contains(row, th.errorText.Render(slot)); got != wantRed {
			t.Errorf("slot %q painted in the failure tone = %v, want %v: %q", slot, got, wantRed, row)
		}
		if got := strings.Contains(strip(row), glyphDone); got != wantDone {
			t.Errorf("row %q wears the done ✓ = %v, want %v", strip(row), got, wantDone)
		}
	}

	const failedSlot = "1 tool call · error: it fell over"

	t.Run("a lone failed run is red on the one row it has", func(t *testing.T) {
		tr := &transcript{}
		loneDelegation(tr, "s1", "broken", "a.go", "")
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "s1", Content: "it fell over", IsError: true}})

		assertSlot(t, paintedRow(t, tr, failedSlot), failedSlot, true, false)
	})

	t.Run("a failed member of a fan-out is red beside its sibling", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", "working", 0)
		readCall(tr, "r1", "a.go", 1, 5, 1)
		subAgentCall(tr, "s2", "broken", 0)
		readCall(tr, "r2", "b.go", 1, 5, 1)
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "s2", Content: "it fell over", IsError: true}})

		assertSlot(t, paintedRow(t, tr, failedSlot), failedSlot, true, false)
		// The sibling is still working: no verdict, so no red and no ✓ either.
		assertSlot(t, paintedRow(t, tr, "working"), "1 tool call", false, false)
	})

	t.Run("a report that only mentions an error is not a failure", func(t *testing.T) {
		const slot = "1 tool call · recovered from an error: all good"

		tr := &transcript{}
		loneDelegation(tr, "s1", "calm", "a.go", "recovered from an error: all good")

		assertSlot(t, paintedRow(t, tr, slot), slot, false, true)
	})

	// A report that OPENS with the failure vocabulary is the case the wording test above cannot
	// reach, and the one that made the row say two things at once: the child SUCCEEDED — its result
	// carries no error — and quoted its answer into the slot, so a run that reported "error: none
	// found" was painted red by a reading of that quote while subAgentFinished, asking the head's
	// own verdict, gave it the done ✓. The verdict is the result's, so the row is neither red nor
	// un-✓'d and the two marks agree.
	t.Run("a report that opens with the failure vocabulary is not a failure", func(t *testing.T) {
		const slot = "1 tool call · error: none found"

		tr := &transcript{}
		loneDelegation(tr, "s1", "search", "a.go", "error: none found")

		assertSlot(t, paintedRow(t, tr, slot), slot, false, true)
	})

	// The other end of the same rule: a delegation REFUSED before it ran (the depth bound, a hook
	// failure, a construct error — agent.runSubAgent) returns an error result and left no span, so
	// its head wears the refusal in its own words (absorbFailure) and that error status is the whole
	// verdict — red, and no ✓ beside a name whose run never happened.
	t.Run("a delegation refused before it ran is red and wears no done mark", func(t *testing.T) {
		const slot = "error: sub-agent depth limit reached (max 2)"

		tr := &transcript{}
		subAgentCall(tr, "s1", "delegate deeper", 0)
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "s1", Content: "sub-agent depth limit reached (max 2)", IsError: true}})

		assertSlot(t, paintedRow(t, tr, slot), slot, true, false)
	})
}

// ----------------------------------------------------------------------------
// A delegation that never ran shows the prompt it carried (ISSUES.md, 2026-08-11)
// ----------------------------------------------------------------------------

// refusedDelegation folds a delegation that NEVER RAN: the call, then the refusal its result
// carried, with nothing at all between them. It is the shape agent.runSubAgent returns at the depth
// bound, on a hook failure and on a construct error — the delegation is over and left no span, so
// subAgentFramed frames it by neither of its answers and it is drawn as an ordinary tool block.
func refusedDelegation(tr *transcript, id, task string) {
	subAgentCall(tr, id, task, 0)
	subAgentReport(tr, id, refusedResult, 0)
}

const (
	// refusedResult is the depth bound's own wording, short enough that the body row it lands on
	// does not wrap at the goldens' width.
	refusedResult = "error: sub-agent depth limit reached (max 2)"
	refusedTask   = "survey the tests\\nand report back\\nwith detail"
)

// TestUnframedSubAgentShowsThePromptWhenExpanded is the item's acceptance golden, in both rendering
// paths. A delegation that never ran is an ordinary tool block, so the prompt it carried has
// nowhere to go but the BODY: expanded, the block opens with "task: " and the whole prompt, and the
// refusal follows a blank line below it. Collapsed it is still exactly one row — the task's first
// line already rides the header as the name fallback (subAgentTarget), so a second rendering of it
// would say the same thing twice in two adjacent rows.
//
// The two paths are pinned against each OTHER as much as against the goldens: folding changes the
// frame around a delegation and never what the delegation shows of itself, so the lone block and
// the grouped member say the same rows in their own frames.
func TestUnframedSubAgentShowsThePromptWhenExpanded(t *testing.T) {
	const width = 80
	prompt := []string{"task: survey the tests", "and report back", "with detail"}

	t.Run("a lone block opens onto the prompt", func(t *testing.T) {
		tr := &transcript{}
		refusedDelegation(tr, "s1", refusedTask)
		if !tr.setExpanded(0, true) {
			t.Fatalf("setExpanded(0, true) = false; want the delegation open")
		}

		want := strings.Join([]string{
			"✦ Sub-Agent",
			// The promoted refusal leaves the slot for the body so the prompt can stand above it,
			// and the presenter's typed phrase takes its place (toolView.demoted).
			leaderEdgeRow("  ┕ survey the tests ⋯ done", glyphExpanded),
			"    " + prompt[0],
			"    " + prompt[1],
			"    " + prompt[2],
			"",
			"    " + refusedResult,
			seeLessFooterLine(t, width),
		}, "\n")

		if got := renderPlain(tr, width); got != want {
			t.Errorf("expanded lone refusal mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("collapsed it is one row", func(t *testing.T) {
		tr := &transcript{}
		refusedDelegation(tr, "s1", refusedTask)

		// The row wears the ▶ that says the prompt above is behind it: the block hides the
		// delegation's whole task, which no reading of its own lines can see (subAgentHidesPrompt).
		want := "✦ Sub-Agent\n" +
			leaderEdgeRow("  ┕ survey the tests ⋯ "+refusedResult, glyphCollapsed)
		if got := renderPlain(tr, width); got != want {
			t.Errorf("collapsed lone refusal mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("a grouped member opens onto the same rows", func(t *testing.T) {
		tr := &transcript{}
		refusedDelegation(tr, "s1", refusedTask)
		refusedDelegation(tr, "s2", "build it")
		if !tr.setExpanded(0, true) {
			t.Fatalf("setExpanded(0, true) = false; want the first member open")
		}

		want := strings.Join([]string{
			"✦ Sub-Agent (2)",
			leaderEdgeRow("  ┝ survey the tests ✓ ⋯ done", glyphExpanded),
			"  │ " + prompt[0],
			"  │ " + prompt[1],
			"  │ " + prompt[2],
			"  │",
			"  │ " + refusedResult,
			memberEdgeRow(t, "  │", promptSeeLess, width),
			"┊",
			// The sibling is shut and hides nothing of its own — its refusal rides the slot and its
			// one-line prompt already rides the header — so its row wears no indicator.
			"  ┕ build it ✓ ⋯ " + refusedResult,
		}, "\n")

		if got := renderPlain(tr, width); got != want {
			t.Errorf("expanded grouped refusal mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	// The prompt is the UNFRAMED reading's alone. A delegation with a run behind it shows its task
	// in its view (ADR 0063), so the block here neither opens nor grows the lead line.
	t.Run("a delegation with a span shows no prompt body at all", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", refusedTask, 0)
		readCall(tr, "r1", "a.go", 1, 5, 1)
		subAgentReport(tr, "s1", "all clear", 0)
		if tr.setExpanded(0, true) {
			t.Fatalf("setExpanded(0, true) = true; a run opens as a view, never as a rail")
		}

		got := renderPlain(tr, width)
		if strings.Contains(got, unframedSubAgentPromptLead) {
			t.Errorf("a framed delegation grew the unframed lead line:\n%s", got)
		}
		if strings.Contains(got, "│ survey the tests") {
			t.Errorf("a framed delegation opened a railed prompt:\n%s", got)
		}
	})
}

// TestNeverRanDelegationRowIsExpandableAtEveryWidth is the pair of findings ISSUES.md left over the
// unframed reading, pinned together because they are one row's two halves (2026-08-27).
//
// A delegation that never ran opens onto the prompt it carried (unframedSubAgentView), and that body
// exists at every width — so the ▶ that says so cannot be the promote-guard's to grant. It used to
// be: the row's only hidden thing the block could count was the refusal the guard DEMOTED, so the
// affordance appeared on a narrow terminal and vanished on a wide one, taking the prompt with it
// (subAgentHidesPrompt closes that). The other half is what the row keeps while it says it: the
// guard's floor holds the target's promoteMinTargetCells wherever the slot's reading falls, so the
// narrowest terminal still names what was delegated.
//
// The table straddles the guard on purpose. At 80 columns this refusal is too long for the slot and
// the presenter's typed phrase takes it; at 110 and above the refusal itself stays there — the
// binding "no unconditional demote", the wide row going on saying why the delegation never ran.
func TestNeverRanDelegationRowIsExpandableAtEveryWidth(t *testing.T) {
	// Long enough that the guard refuses it at the narrow end of the table and admits it at the
	// wide end — the depth bound's own wording with the sentence it closes with.
	const refusal = refusedResult + " — no further delegation is possible"
	const target = "survey the tests"

	// The floor is composed from the guard's own constant rather than spelled, so a change to the
	// number moves what this test demands instead of leaving it aimed at a stale width.
	floor := string([]rune(target)[:promoteMinTargetCells])

	for _, tc := range []struct {
		name  string
		width int
		slot  string // what the collapsed row's outcome slot must carry at this width
	}{
		{"the guard demotes the refusal at 80 columns", 80, "done"},
		{"the refusal keeps the slot at 110 columns", 110, refusal},
		{"and at 120 columns", 120, refusal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			subAgentCall(tr, "s1", refusedTask, 0)
			subAgentReport(tr, "s1", refusal, 0)

			rows := strings.Split(renderPlain(tr, tc.width), "\n")
			row := rows[len(rows)-1]

			if !strings.HasSuffix(row, glyphCollapsed) {
				t.Errorf("collapsed row = %q; want the ▶ that opens onto the prompt", row)
			}
			if !strings.Contains(row, floor) {
				t.Errorf("collapsed row = %q; want at least %d cells of the target %q",
					row, promoteMinTargetCells, target)
			}
			if !strings.Contains(row, tc.slot) {
				t.Errorf("collapsed row = %q; want the outcome %q in its slot", row, tc.slot)
			}

			if !tr.setExpanded(0, true) {
				t.Fatalf("setExpanded(0, true) = false; want the delegation open")
			}
			// What the ▶ promised: the prompt whole — its lead line and its last — over the refusal
			// the row had been carrying alone. The refusal is asked for by its opening phrase, the
			// body soft-wrapping it at the narrow widths of the table.
			opened := renderPlain(tr, tc.width)
			for _, want := range []string{
				unframedSubAgentPromptLead + target, "with detail", "error: sub-agent depth limit",
			} {
				if !strings.Contains(opened, want) {
					t.Errorf("the opened delegation is missing %q:\n%s", want, opened)
				}
			}
		})
	}
}

// TestSubAgentPromptDetailsLeadsWithTheTask pins the body lines the prompt becomes: its first line
// under the lead, every further line plain beneath it, and nothing at all for a prompt that is blank
// throughout — which is what leaves a record with no retained task (transcriptcodec.go) showing the
// view it always showed.
func TestSubAgentPromptDetailsLeadsWithTheTask(t *testing.T) {
	long := strings.Repeat("x", detailClipRunes+20)

	for _, tc := range []struct {
		name string
		task string
		want []string
	}{
		{"a one-line prompt is one lead line", "survey the tests", []string{"task: survey the tests"}},
		{"further lines follow it plain", "a\nb\nc", []string{"task: a", "b", "c"}},
		{"trailing blank lines come off", "a\n\n", []string{"task: a"}},
		{"an empty prompt has no lines", "", nil},
		{"whitespace alone has no lines", "  \n\t\n", nil},
		{"every line is held to the detail clip", long, []string{clipDetail(unframedSubAgentPromptLead + long)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := subAgentPromptDetails(tc.task)

			text := make([]string, 0, len(got))
			for _, ln := range got {
				text = append(text, ln.Text)
			}
			if !slices.Equal(text, tc.want) {
				t.Errorf("subAgentPromptDetails(%q) = %q; want %q", tc.task, text, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// A delegation never folds into a super-group (design call 12)
// ----------------------------------------------------------------------------

// TestUnframedSubAgentNeverFoldsIntoASuperGroup pins the invariant that makes the two substitution
// sites above enough. Every construction site marks a delegation solo (presentToolCall, and the
// decode path that re-derives it), so it is never groupable: it heads no umbrella and lets none
// span it (toolSuperGroup), and renderSuperGroup's member painter can therefore never be handed
// one — which is why a delegation that never ran only ever reaches the reader through the lone
// block above.
//
// The fixture is the case that WOULD fold if the mark were ever lost: a never-ran delegation
// between two reads is three adjacent runs of different labels at one depth, an umbrella by every
// other rule. It must still paint as three separate blocks, with the delegation opening onto the
// prompt it carried rather than shrinking to an umbrella member row that has nowhere to show it.
func TestUnframedSubAgentNeverFoldsIntoASuperGroup(t *testing.T) {
	const width = 80

	tr := &transcript{}
	readCall(tr, "r1", "a.go", 1, 5, 0)
	refusedDelegation(tr, "s1", refusedTask)
	readCall(tr, "r2", "b.go", 1, 5, 0)
	for i := range tr.entries {
		if !tr.setExpanded(i, true) {
			t.Fatalf("setExpanded(%d, true) = false; want every block open", i)
		}
	}

	for at := range tr.entries {
		if got := toolSuperGroup(tr.entries, at); got != nil {
			t.Errorf("toolSuperGroup(…, %d) = %v; a delegation neither heads an umbrella nor lets one span it", at, got)
		}
	}

	// Its own header, its own leader row, and the prompt beneath them — the lone-block reading.
	want := strings.Join([]string{
		"✦ Sub-Agent",
		leaderEdgeRow("  ┕ survey the tests ⋯ done", glyphExpanded),
		"    " + unframedSubAgentPromptLead + "survey the tests",
		"    and report back",
		"    with detail",
	}, "\n")
	if got := renderPlain(tr, width); !strings.Contains(got, want) {
		t.Errorf("a delegation among reads lost its own block:\n--- got ---\n%s\n--- want it to contain ---\n%s", got, want)
	}
}

// ----------------------------------------------------------------------------
// The run view's breadcrumb (ADR 0063)
// ----------------------------------------------------------------------------

// The trail names every run between the human's own conversation and the one on screen, in reading
// order, and it names each of them the way the rest of the frame does: the call's own short name,
// else the task it was given, else the constant — so a delegation the model named nothing still
// reads as something rather than as a gap between two separators.
func TestBreadcrumbTrailNamesTheWayBackUp(t *testing.T) {
	tr := &transcript{}
	delegationCall(tr, "", "s1", "planner", "plan the work", 0)
	delegationCall(tr, "s1", "s2", "repo-scout", "scout the repo", 1)
	tr.apply(domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: 2, CallID: "s2"},
		Call:      domain.ToolCall{ID: "s3", Tool: "sub_agent", Arguments: []byte(`{"task":"read the tests"}`)},
	})
	tr.apply(domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: 2, CallID: "s2"},
		Call:      domain.ToolCall{ID: "s4", Tool: "sub_agent", Arguments: []byte(`{}`)},
	})

	cases := []struct {
		name  string
		spawn string
		want  string
	}{
		{"the top level itself", "", "← main"},
		{"one level down", "s1", "← main › planner"},
		{"two levels down", "s2", "← main › planner › repo-scout"},
		{"an unnamed run takes its task", "s3", "← main › planner › repo-scout › read the tests"},
		{"a run with neither takes the constant", "s4", "← main › planner › repo-scout › " + usageAgentFallback},
		{"a run the list has no head for", "gone", "← main"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := breadcrumbTrail(tr.entries, c.spawn); got != c.want {
				t.Errorf("breadcrumbTrail(%q) = %q, want %q", c.spawn, got, c.want)
			}
		})
	}
}

// The row spends its width in the order it is READ for: the trail first, the key hint second. Where
// the two cannot both fit, the hint goes whole rather than being truncated to a keystroke nobody can
// make out — and the row is squared to the width either way, so the header's field runs the whole
// way across instead of showing the terminal's background through the gap.
func TestBreadcrumbRowSpendsItsWidthInOrder(t *testing.T) {
	th := newTheme(scheme.Default())
	const trail = "← main › repo-scout"

	wide := strip(breadcrumbRow(th, trail, 60, breadcrumbHint))
	if want := bodyIndent + trail; !strings.HasPrefix(wide, want) {
		t.Errorf("the row is %q, want it to lead with %q", wide, want)
	}
	if want := breadcrumbHint + bodyIndent; !strings.HasSuffix(wide, want) {
		t.Errorf("the row is %q, want it to end with %q", wide, want)
	}
	if got := th.measure.Width(wide); got != 60 {
		t.Errorf("the row is %d columns wide, want 60", got)
	}

	narrow := strip(breadcrumbRow(th, trail, 24, breadcrumbHint))
	if strings.Contains(narrow, breadcrumbHint) {
		t.Errorf("the row is %q at 24 columns; want the hint dropped whole", narrow)
	}
	if got := th.measure.Width(narrow); got != 24 {
		t.Errorf("the narrow row is %d columns wide, want 24", got)
	}
}

// ----------------------------------------------------------------------------
// A run has two shapes and no third (ADR 0063)
// ----------------------------------------------------------------------------

// A delegation with a run behind it — or one that has not reported yet, which is the same
// delegation a beat earlier — owns no block state at all: expanding it opens its VIEW, so the flag
// that used to open a rail in place is refused wherever it is written from ([transcript.setExpanded]
// is the one writer, and [Model.openRunAt] is the redirect that reaches it). What that leaves the
// flag meaning on a sub_agent call is the one reading that is not a run: a delegation that is OVER
// and left nothing behind it still opens onto the prompt it carried (unframedSubAgentView).
func TestRunHeadOwnsNoBlockState(t *testing.T) {
	for _, tc := range []struct {
		name       string
		build      func(tr *transcript)
		wantToggle bool
	}{
		{
			name: "a finished run with entries behind it",
			build: func(tr *transcript) {
				loneDelegation(tr, "s1", "survey", "a.go", "all clear")
			},
		},
		{
			name: "a run still working",
			build: func(tr *transcript) {
				loneDelegation(tr, "s1", "survey", "a.go", "")
			},
		},
		{
			// The one a redirect keyed on the FRAME alone would get wrong: a child announced and not
			// yet talking is unframed while its row is shut, and must still open its view rather
			// than a rail that the view would replace a moment later (subAgentFramed).
			name: "a child announced before its first entry lands",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey", 0)
				subAgentStarted(tr, "s1", 1)
			},
		},
		{
			name: "a delegation that ran and left nothing",
			build: func(tr *transcript) {
				refusedDelegation(tr, "s1", refusedTask)
			},
			wantToggle: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			shut := renderPlain(tr, 80)
			if got := tr.setExpanded(0, true); got != tc.wantToggle {
				t.Fatalf("setExpanded(0, true) = %v, want %v", got, tc.wantToggle)
			}
			open := renderPlain(tr, 80)
			if tc.wantToggle {
				if open == shut {
					t.Errorf("the block took the state and painted no differently:\n%s", open)
				}
				return
			}
			if open != shut {
				t.Errorf("a refused state still moved the row:\n--- got ---\n%s\n--- want ---\n%s", open, shut)
			}
			if strings.Contains(open, subAgentOpenMarker) {
				t.Errorf("the run opened a rail with %q:\n%s", subAgentOpenMarker, open)
			}
		})
	}
}

// A finished delegation says its report ONCE (ISSUES.md, "Finished sub-agents print the sub-agent
// output twice"). The early, badly formatted copy was the head's own tool-result body, laid out
// above the run's span the moment the head was opened; the copy that stays is the child's last
// assistant row, inside the run where it was said.
//
// Both halves are pinned on one fixture: the conversation's row, which carries the report's first
// line as its gist and no more of it anywhere, and the run's own view, where the report stands
// exactly once — as the last row, after the work, under the task the child was handed.
func TestFinishedRunSaysItsReportOnce(t *testing.T) {
	const report = "Found 4 gaps\nin the suite\nhere they are"
	build := func() *transcript {
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentStarted(tr, "s1", 1)
		childCall(tr, "s1", "c1", "a.go")
		tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: report})
		subAgentReport(tr, "s1", report, 0)
		return tr
	}

	t.Run("the conversation's row keeps the gist and nothing else", func(t *testing.T) {
		tr := build()
		if tr.setExpanded(0, true) {
			t.Fatal("setExpanded(0, true) = true; a run opens as a view, never as a rail")
		}

		got := renderPlain(tr, 80)
		if strings.Contains(got, subAgentOpenMarker) {
			t.Errorf("the row opened a rail with %q:\n%s", subAgentOpenMarker, got)
		}
		for _, line := range []string{"in the suite", "here they are"} {
			if strings.Contains(got, line) {
				t.Errorf("the row printed the report line %q a second time:\n%s", line, got)
			}
		}
	})

	t.Run("the run's view holds the one formatted copy", func(t *testing.T) {
		tr := build()
		tr.setRoot(runRef{depth: 1, spawn: "s1"})

		lines := strings.Split(renderPlain(tr, 80), "\n")
		if hits := strings.Count(strings.Join(lines, "\n"), "here they are"); hits != 1 {
			t.Errorf("the report's last line appears %d times in the view, want exactly 1:\n%s",
				hits, strings.Join(lines, "\n"))
		}
		// Row 0 is the breadcrumb the head became; the task the child was handed opens the view
		// directly beneath it, and its own work follows (render.go's rooted paint).
		if !strings.Contains(lines[0], breadcrumbBack) {
			t.Fatalf("line 0 is %q, not the breadcrumb:\n%s", lines[0], strings.Join(lines, "\n"))
		}
		task := -1
		for i, ln := range lines {
			if strings.HasPrefix(ln, glyphUser) {
				task = i
				break
			}
		}
		if task < 0 {
			t.Fatalf("the view shows no task row at all:\n%s", strings.Join(lines, "\n"))
		}
		if !strings.Contains(lines[task], "survey the tests") {
			t.Errorf("the task row reads %q, want the prompt the child was handed", lines[task])
		}
		for i, ln := range lines[1:task] {
			if strings.TrimSpace(ln) != "" {
				t.Errorf("line %d (%q) stands between the breadcrumb and the task row", i+1, ln)
			}
		}
		if last := lines[len(lines)-1]; !strings.Contains(last, "here they are") {
			t.Errorf("the view's last row is %q, want the end of the report the run returned", last)
		}
	})
}

// ----------------------------------------------------------------------------
// The result envelope on the collapsed row (ADR 0063 D3; the F1 follow-up)
// ----------------------------------------------------------------------------

// The three envelope shapes the ENGINE wraps a delegation's result in, restated here for the reason
// cmd/apogee restates its own (internal/agent's stepCapResultFormat, subAgentFaultPrefix and
// userSteeredTrailer are unexported, and this package reads them off the output by shape): they are
// what a human reads, so a rename over there has to fail here.
const (
	envelopeCapMarker  = "[delegate stopped at its step cap (3 steps); partial result — its last visible text follows]"
	envelopeFaultLine  = "sub-agent faulted before finishing the delegated task: the upstream died"
	envelopeSteeredOne = "\n\n(the user sent 1 message to this sub-agent while it ran)"
	envelopeSteeredTwo = "\n\n(the user sent 2 messages to this sub-agent while it ran)"
)

// A run collapses to ONE row in the parent's conversation (collapsedSubAgentView), so that row is
// the only place a reader of that conversation can learn a delegation was stopped at its step cap,
// faulted, or was steered while it ran. The envelope the engine wraps the child's answer in carries
// all three; the row's outcome slot is where it has to land. Before this the slot said the fixed
// word "done" over a capped run and over a steered one alike, which is the regression this pins.
func TestCollapsedRunSlotCarriesTheResultEnvelope(t *testing.T) {
	// One painted row per case: the head's own, found by the count its slot opens with. The width is
	// generous on purpose — what is under test is what the slot SAYS, and a row narrow enough to clip
	// it would assert the geometry instead.
	row := func(t *testing.T, tr *transcript) string {
		t.Helper()
		for _, ln := range strings.Split(renderPlain(tr, 160), "\n") {
			if strings.Contains(ln, "1 tool call · ") {
				return strings.TrimSpace(ln)
			}
		}
		t.Fatalf("no row carries the run's summary:\n%s", renderPlain(tr, 160))
		return ""
	}
	report := func(tr *transcript, content string, failed bool) {
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "s1", Content: content, IsError: failed}})
	}

	cases := []struct {
		name    string
		content string
		failed  bool
		want    string
	}{
		{
			name:    "a whole run still reads done",
			content: "Found 4 gaps\nin the suite",
			want:    "done",
		},
		{
			name:    "a capped run says it was stopped short",
			content: envelopeCapMarker + "\nI had read two files so far",
			want:    "stopped at its step cap",
		},
		{
			name:    "a steered run says how many messages reached it",
			content: "Found 4 gaps\nin the suite" + envelopeSteeredTwo,
			want:    "done · steered by 2 messages",
		},
		{
			name:    "a capped run that was steered says both",
			content: envelopeCapMarker + "\nI had read two files so far" + envelopeSteeredOne,
			want:    "stopped at its step cap · steered by 1 message",
		},
		{
			name:    "a faulted run keeps its cause and gains the steering",
			content: envelopeFaultLine + envelopeSteeredOne,
			failed:  true,
			want:    "error: " + envelopeFaultLine + " · steered by 1 message",
		},
		{
			name:    "a faulted run nobody steered reads exactly as it did",
			content: envelopeFaultLine,
			failed:  true,
			want:    "error: " + envelopeFaultLine,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			loneDelegation(tr, "s1", "survey the tests", "a.go", "")
			report(tr, tc.content, tc.failed)

			if got := row(t, tr); !strings.Contains(got, tc.want) {
				t.Errorf("the run's row reads %q, want its slot to say %q", got, tc.want)
			}
		})
	}
}

// The envelope is read off the engine's OWN lines and nothing else: a child that merely QUOTES one
// of them mid-report has not been capped and has not been steered, and a row that said so would be
// reporting a fact the run never produced.
func TestResultEnvelopeIsReadOffTheEnginesOwnLinesOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"the cap marker quoted mid-report", "The child said:\n" + envelopeCapMarker},
		{"the notice quoted mid-report", "The child said:" + envelopeSteeredTwo + "\nand carried on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := delegationVerdict(tc.content); got != delegationDoneVerdict {
				t.Errorf("delegationVerdict = %q, want %q", got, delegationDoneVerdict)
			}
		})
	}
}
