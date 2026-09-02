package tui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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
// Opening one reveals nothing here any more: expanding a framed delegation opens its RUN VIEW
// (ADR 0063), so transcript.setExpanded refuses the flag and the list paints the very rows it
// painted shut — no ┌─┶ header, no column-0 │ rail over a span, no ┊ closing it before the list
// resumes — and that refusal is what this golden pins. A FINISHED delegation carries a ✓ after its
// name whatever its own fold flag says (design call 6).
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

// Adjacent skill fetches fold under ONE "✦ Skill (N)" umbrella, one row per fetch, in the very
// shape the fan-out above pins for delegations (ISSUES.md, "load_skill renders as a raw tool") —
// because it is the same derivation and the same painter, asked of another tool name (ownGroup,
// renderSubAgentGroup). Each row carries the SKILL that answered, off the retarget item 1 landed,
// and none of the delegation-only markings: no ✓ for having reported, no run to open.
func TestRenderSkillGroupSketchStates(t *testing.T) {
	build := func(fetches ...[3]string) *transcript {
		tr := &transcript{}
		for _, f := range fetches {
			skillFetch(tr, f[0], f[1], "<skill: "+f[2]+">\nUse gofmt.\n</skill>\n")
		}
		return tr
	}
	standards := [3]string{"c1", "format Go", "Coding Standards"}
	release := [3]string{"c2", "cut a release", "Brew Release"}
	grill := [3]string{"c3", "grill me", "Grill Me"}

	t.Run("two fetches are one umbrella", func(t *testing.T) {
		want := strings.Join([]string{
			"✦ Skill (2)",
			groupMemberLine("  ┝ Coding Standards ⋯"),
			groupMemberLine("  ┕ Brew Release ⋯"),
		}, "\n")
		if got := renderPlain(build(standards, release), 80); got != want {
			t.Errorf("two fetches mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("three fetches are one umbrella", func(t *testing.T) {
		want := strings.Join([]string{
			"✦ Skill (3)",
			groupMemberLine("  ┝ Coding Standards ⋯"),
			groupMemberLine("  ┝ Brew Release ⋯"),
			groupMemberLine("  ┕ Grill Me ⋯"),
		}, "\n")
		if got := renderPlain(build(standards, release, grill), 80); got != want {
			t.Errorf("three fetches mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	// A fetch heads no run, so a member row opens the skill's own text IN PLACE — the inline expand
	// every other folded call gets (renderGroupMember) — and never a run view. The list is not
	// re-headed by the interruption either: the rows after the open member resume headerless.
	t.Run("a member opens inline, never a run view", func(t *testing.T) {
		tr := build(standards, release)
		if tr.entries[0].headsRun() || tr.entries[0].opensRun() {
			t.Fatal("a skill fetch heads a run; opensRun/headsRun must stay keyed on sub_agent")
		}
		if !tr.setExpanded(0, true) {
			t.Fatal("setExpanded(0, true) = false; a skill member expands in place")
		}
		got := renderPlain(tr, 80)
		for _, line := range []string{"✦ Skill (2)", "│ Use gofmt.", groupMemberLine("  ┕ Brew Release ⋯")} {
			if !strings.Contains(got, line) {
				t.Errorf("the open group is missing %q:\n%s", line, got)
			}
		}
		if n := strings.Count(got, "✦ Skill"); n != 1 {
			t.Errorf("the open member re-headed the list: %d headers in\n%s", n, got)
		}
		if strings.Contains(got, subAgentOpenMarker) {
			t.Errorf("the skill group drew the run marker %q:\n%s", subAgentOpenMarker, got)
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

	// The one WORD is what the queued state changes, and the indicator is deliberately not part of
	// it: what a delegation's row opens is its child's run view (ADR 0063), and a queued child has
	// one — it names the delegation and says it has not started, which is exactly what a lone queued
	// delegation opens onto today (TestRunViewPlaceholderNamesTheChild). An affordance that arrived
	// the instant a worker freed a slot would be one no reader could learn.
	t.Run("queued member says scheduled and still wears its indicator", func(t *testing.T) {
		want := strings.Join(append(append([]string{header}, running...),
			groupMemberLine("  ┕ check ⋯ scheduled")), "\n")
		if got := renderPlain(build(t), 80); got != want {
			t.Errorf("scheduled member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("queued member is a click target like its running siblings", func(t *testing.T) {
		lines, targets := targetedRender(build(t), 80)
		if got := targets[rowWith(t, lines, "check")].kind; got != targetHeader {
			t.Errorf("scheduled row is target kind %v, want targetHeader", got)
		}
		// The comparison, off the same paint: a member with a run behind it opens the same way, so
		// the fold offers one gesture down the whole list.
		if got := targets[rowWith(t, lines, "survey")].kind; got != targetHeader {
			t.Errorf("running row is target kind %v, want targetHeader", got)
		}
	})

	t.Run("its start ends the scheduled row", func(t *testing.T) {
		tr := build(t)
		subAgentStarted(tr, "s3", 1)
		want := strings.Join(append(append([]string{header}, running...),
			groupMemberLine("  ┕ check ⋯")), "\n") // running with nothing done yet: the live reading
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
	//
	// What changes with it is the row's WORDS and not its ▶: the refusal takes the slot the one
	// queued word held, and the reading behind the row moves from the child's view to the prompt
	// the delegation carried (renderSubAgentMemberRows), which is a swap the reader never has to
	// notice — the row was openable throughout.
	t.Run("a refused delegation is not scheduled", func(t *testing.T) {
		tr := build(t)
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "s3", Content: "sub-agent depth limit reached", IsError: true}})
		want := strings.Join(append(append([]string{header}, running...),
			groupMemberLine("  ┕ check ⋯ error: sub-agent depth limit reached")), "\n")
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
	if run := sameLabelRun(tr.entries, 0); run != 0 {
		t.Fatalf("sameLabelRun over the two refused delegations = %d calls, want none — a solo call heads no run", run)
	}

	want := strings.Join([]string{
		// The refusal fills the whole outcome slot, so the leader keeps its floor of one dot and
		// the target gives way entirely — design call 4's order, played out to its end. Each row
		// still wears its ▶: the task pushed off the row is precisely what the member opens onto
		// (renderSubAgentMemberRows), so the delegation crowded out of its own header is the one
		// with the most behind the indicator.
		"✦ Sub-Agent (2)",
		groupMemberLine("  ┝ ⋯ error: sub-agent depth limit reached (max 2): cannot spawn a deeper su" + clipTail),
		groupMemberLine("  ┕ ⋯ error: sub-agent depth limit reached (max 2): cannot spawn a deeper su" + clipTail),
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

		sibling := leaderEdgeRow("  ┕ build it ✓ ⋯ "+refusedResult, glyphCollapsed)
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
			// The sibling is shut and hides the prompt it carried, exactly as the lone block's row
			// does: the task's first line rides the header as the delegation's NAME (subAgentTarget)
			// and the prompt itself is nowhere on the row, so the member wears the ▶ that says so.
			sibling,
		}, "\n")

		if got := renderPlain(tr, width); got != want {
			t.Errorf("expanded grouped refusal mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}

		// The affordance and the click surface are one answer: the row wearing the ▶ is the row a
		// click on it toggles, carrying the sibling's own entry (renderSubAgentMemberRows).
		marks := blockMarks(t, tr, width)
		last := marks[len(marks)-1]
		if last.kind != targetHeader || last.entry != 1 || last.text != sibling {
			t.Errorf("the shut sibling's mark = %+v; want a targetHeader on entry 1 at %q", last, sibling)
		}

		// And what it opens is the lone block's reading in the member's frame: the prompt under its
		// lead, then the refusal a blank line below it.
		if !tr.setExpanded(1, true) {
			t.Fatalf("setExpanded(1, true) = false; want the shut sibling open")
		}
		opened := strings.Join([]string{
			leaderEdgeRow("  ┕ build it ✓ ⋯ done", glyphExpanded),
			"  │ " + unframedSubAgentPromptLead + "build it",
			"  │",
			"  │ " + refusedResult,
			memberEdgeRow(t, "  │", promptSeeLess, width),
		}, "\n")
		if got := renderPlain(tr, width); !strings.Contains(got, opened) {
			t.Errorf("the opened sibling mismatch:\n--- got ---\n%s\n--- want it to contain ---\n%s",
				got, opened)
		}
	})

	// The rule is the prompt's, not the row's: a delegation that carried no task at all — a record
	// replayed from a session written before the text was retained (transcriptbridge.go) — opens
	// onto nothing, so its member row stays bare however its siblings fold (subAgentHidesPrompt).
	t.Run("a shut member with no prompt wears nothing", func(t *testing.T) {
		tr := &transcript{}
		refusedDelegation(tr, "s1", refusedTask)
		refusedDelegation(tr, "s2", "")

		rows := strings.Split(renderPlain(tr, width), "\n")
		last := rows[len(rows)-1]
		if !strings.Contains(last, refusedResult) {
			t.Fatalf("last row = %q; want the promptless member's own row", last)
		}
		if strings.HasSuffix(last, glyphCollapsed) {
			t.Errorf("the promptless member row = %q; want no indicator over an empty body", last)
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
//
// The subtest after it asks the same two halves of a delegation folded into a GROUP, where the row
// is painted by a different hand (renderSubAgentMemberRows): the member's ▶ has to arrive with the
// refusal still in its slot, because an indicator bought by demoting the outcome would be the fold
// paying for the affordance with what the row says.
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

	// The GROUPED reading of the same row, at the wide end of the table where the guard admits the
	// refusal. A member's indicator is granted by the prompt clause alone and never by a demote
	// (renderSubAgentMemberRows), so both folded members go on saying why the delegation never ran
	// while wearing the ▶ that opens onto the prompt — the binding "no unconditional demote"
	// (render.go's rooted paint) asked of a fold rather than of a lone block.
	t.Run("a folded grouped member keeps its promoted refusal at 110 columns", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", refusedTask, 0)
		subAgentReport(tr, "s1", refusal, 0)
		subAgentCall(tr, "s2", "build it", 0)
		subAgentReport(tr, "s2", refusal, 0)

		want := strings.Join([]string{
			"✦ Sub-Agent (2)",
			leaderEdgeRow("  ┝ "+target+" ✓ ⋯ "+refusal, glyphCollapsed),
			leaderEdgeRow("  ┕ build it ✓ ⋯ "+refusal, glyphCollapsed),
		}, "\n")
		if got := renderPlain(tr, 110); got != want {
			t.Errorf("folded grouped refusals mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// TestRunningGroupedDelegationOpensItsChild pins the FOLD's half of one rule: a delegation's row is
// a toggle target whenever it holds back the prompt it carried, whatever lifecycle the delegation is
// in — because the reading behind the row while its child works is that child's own RUN VIEW
// (ADR 0063, [Model.openRunAt]), and the prompt itself only once the delegation is over.
//
// It is the grouped reading of what a LONE delegation has always done, and the point is that they
// are the same delegation. A member of a fan-out painted a bare, unreachable row while an identical
// delegation standing alone opened fine, so which siblings happened to fold beside a child decided
// whether a reader could watch it at all — in exactly the case parallel delegation exists to make
// (ADR 0039). ISSUES.md, 2026-09-01.
func TestRunningGroupedDelegationOpensItsChild(t *testing.T) {
	const width = 80

	// Two children with a slot apiece and nothing committed behind either: the shape a fan-out
	// wears for the whole beat between its start and its first landing.
	started := func(tr *transcript) {
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentStarted(tr, "s1", 1)
		subAgentCall(tr, "s2", "build it", 0)
		subAgentStarted(tr, "s2", 1)
	}

	t.Run("both folded rows wear the indicator and carry their own entry", func(t *testing.T) {
		tr := &transcript{}
		started(tr)

		want := strings.Join([]string{
			"✦ Sub-Agent (2)",
			groupMemberLine("  ┝ survey the tests ⋯"),
			groupMemberLine("  ┕ build it ⋯"),
		}, "\n")
		if got := renderPlain(tr, width); got != want {
			t.Errorf("running fan-out mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}

		// The affordance and the click are the one predicate's single answer, and a list has no
		// state of its own: the two member rows are marked, each for its OWN member's entry, and
		// the header over them is marked for nothing.
		marks := blockMarks(t, tr, width)
		if len(marks) != 2 {
			t.Fatalf("the fan-out marks %+v; want one target per member row", marks)
		}
		for i, mk := range marks {
			if mk.kind != targetHeader || mk.entry != i {
				t.Errorf("mark %+v; want a targetHeader on entry %d", mk, i)
			}
		}
	})

	t.Run("activating a member opens that member's own child", func(t *testing.T) {
		m := newTestModel(t)
		m.transcript.reset()
		m.transcript.addUser("survey the repo", nil)
		started(&m.transcript)
		m.refreshViewport()

		if got := enterOnLastBlock(t, m).viewedRun().spawn; got != "s2" {
			t.Errorf("⏎ on the last member opened run %q; want the child that row names", got)
		}
		// The sibling above it opens ITS child and not the one the last row named — the rows are
		// marked apart (blockPaint.addFor), so the fold is a way into each of them rather than one
		// way into the group.
		first := step(t, step(t, step(t, m, keyAltUp()), keyUp()), keyEnter())
		if got := first.viewedRun().spawn; got != "s1" {
			t.Errorf("⏎ on the first member opened run %q; want the child that row names", got)
		}
	})
}

// TestSubAgentPromptDetailsLeadsWithTheTask pins the body lines the prompt becomes: its first line
// under the lead, every further line plain beneath it, and nothing at all for a prompt that is blank
// throughout — which is what leaves a record with no retained task (transcriptbridge.go) showing the
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

// The header stands on the `surface` field the input box and the status line stand on — a black
// band at the top of the frame to match the one at its bottom — and not on the prompt block's gray,
// which is the field it used to borrow for want of one of its own. The row is painted edge to edge
// at every width: a bare cell would show the terminal's own background through the band, and a cell
// on `chrome` would be the borrowed field leaking back.
func TestBreadcrumbRowIsPaintedEdgeToEdgeOnTheSurfaceField(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	const trail = "← main › repo-scout"

	// The two fields, read off the styles that DEFINE them rather than off a literal: the status
	// line's black and the prompt block's gray. A scheme retune moves both and this test still
	// asks the question it means to ask.
	surfaceField := backgroundToken(t, th.statusBar.Render("x"))
	chromeField := backgroundToken(t, th.userBlock.Render("x"))
	if surfaceField == chromeField {
		t.Fatalf("the surface and chrome fields both render as %q; the test cannot tell them apart", surfaceField)
	}

	for _, tc := range []struct {
		name  string
		width int
		hint  string
	}{
		{"wide, with the hint", 60, breadcrumbHint},
		{"wide, hint silent", 60, ""},
		{"narrow, the hint dropped", 24, breadcrumbHint},
		{"narrow, hint silent", 24, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			row := breadcrumbRow(th, trail, tc.width, tc.hint)

			if got := ansi.StringWidth(row); got != tc.width {
				t.Errorf("the row is %d columns wide, want %d: %q", got, tc.width, strip(row))
			}
			if col, ok := firstCellWithoutBackground(row); !ok {
				t.Errorf("the row has a bare (no-background) cell at column %d: %q", col, strip(row))
			}
			for _, bg := range backgroundSGR.FindAllString(row, -1) {
				if bg != surfaceField {
					t.Errorf("the row carries the background %q, want the surface field %q: %q",
						bg, surfaceField, strip(row))
				}
			}
		})
	}
}

// backgroundSGR matches ONE background parameter run inside a rendered line — "48;5;n" or
// "48;2;r;g;b". It answers which field a row stands on, where firstCellWithoutBackground
// (popup_test.go) answers only whether it stands on one at all.
var backgroundSGR = regexp.MustCompile(`48;(?:5;\d+|2;\d+;\d+;\d+)`)

// backgroundToken reports the background a probe render carries, for use as the expected field of
// the rows under test. It fails the test outright when the probe carries none, because a bare probe
// would otherwise make every comparison against it pass.
func backgroundToken(t *testing.T, probe string) string {
	t.Helper()

	found := backgroundSGR.FindAllString(probe, -1)
	if len(found) == 0 {
		t.Fatalf("the probe %q carries no background at all", probe)
	}
	return found[0]
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

// ----------------------------------------------------------------------------
// A finished delegation's collapsed row (tui-polish plan, item 4)
// ----------------------------------------------------------------------------

// A run that FINISHED paints its outcome slot in the scheme's `success` green on the one row it
// has, and wears the done ✓ beside its name — the two marks being one style and one fact
// (theme.successMark). The collapsed row is where the claim bites: its line is a COMPOSED reading,
// "1 tool call · done", whose leading words are a count of the work and say nothing about how the
// run ended, so the verdict has to be the HEAD's, carried onto that line exactly as a failure's is
// (subAgentSummary). A painter reading the composed words instead would find no verdict in them.
//
// The other half of the claim is that the green is anchored on the delegation vocabulary and not on
// the row's spelling: a run stopped at its step cap did not finish, keeps the ordinary marker tone,
// and wears no ✓.
func TestSubAgentFinishedRunReadsInTheSuccessTone(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	// Wide enough that no row has to give up its outcome slot: what is under test is the slot's
	// tone, and a clipped slot would assert the geometry instead.
	const width = 100

	// A report of two lines, so the run's answer hangs in the block's body and the slot carries the
	// ENGINE's verdict rather than a one-line report promoted into it (promotedOutput) — which is
	// quoted, carries no verdict of either kind, and is a different claim.
	const report = "all clear\nnothing else to report"

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

	t.Run("a finished run is green on the one row it has", func(t *testing.T) {
		const slot = "1 tool call · done"

		tr := &transcript{}
		loneDelegation(tr, "s1", "survey", "a.go", report)

		row := paintedRow(t, tr, slot)
		if !strings.Contains(row, th.successMark.Render(slot)) {
			t.Errorf("slot %q is not painted in the success role: %q", slot, row)
		}
		if strings.Contains(row, th.toolMarker.Render(slot)) {
			t.Errorf("slot %q still wears the ordinary marker tone: %q", slot, row)
		}
		if !strings.Contains(strip(row), glyphDone) {
			t.Errorf("the finished run wears no done ✓: %q", strip(row))
		}
	})

	t.Run("a run stopped at its step cap keeps the marker tone", func(t *testing.T) {
		const slot = "1 tool call · stopped at its step cap"

		tr := &transcript{}
		loneDelegation(tr, "s1", "survey", "a.go",
			"[delegate stopped at its step cap (3 steps); partial result — its last visible "+
				"text follows]\nhalfway there")

		row := paintedRow(t, tr, slot)
		if !strings.Contains(row, th.toolMarker.Render(slot)) {
			t.Errorf("slot %q does not wear the ordinary marker tone: %q", slot, row)
		}
		if strings.Contains(row, th.successMark.Render(slot)) {
			t.Errorf("a run that did not finish is painted green: %q", row)
		}
	})
}

// ----------------------------------------------------------------------------
// A delegation named out of band (ADR 0068)
// ----------------------------------------------------------------------------

// TestGeneratedDelegationNameReachesEverySurface is the whole point of folding a
// domain.SubAgentNamedEvent onto the head rather than onto any one display: a delegation the model
// left unnamed is named while it runs, and every surface that names a run reads that head and
// nothing else (usageAgentName, transcript.runName). So ONE fold has to move all of them together —
// a surface reading the name from somewhere else would go on showing the delegated task's first
// line while its neighbour showed the generated name, and the human would be reading about two
// different children.
//
// The fixture is a fan-out because the rename's aim is what a lone run cannot test: the event is
// applied by CALL ID, so it must land on the member it names and leave its sibling wearing the task
// it was given (ADR 0039).
func TestGeneratedDelegationNameReachesEverySurface(t *testing.T) {
	const name = "test-surveyor"

	build := func(t *testing.T) *transcript {
		t.Helper()
		tr := &transcript{}
		subAgentCall(tr, "s1", "survey the tests", 0)
		subAgentStarted(tr, "s1", 1)
		readCall(tr, "rs1", "a.go", 1, 5, 1)
		subAgentCall(tr, "s2", "build the docs", 0)
		subAgentStarted(tr, "s2", 1)
		readCall(tr, "rs2", "b.go", 1, 5, 1)
		return tr
	}
	rename := func(tr *transcript, callID, to string) {
		tr.apply(domain.SubAgentNamedEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: callID},
			Name:      to,
		})
	}

	// The collapsed group is where the delegation is READ: its member row leads with the head's
	// Target, so the rename is visible on the scrollback without opening anything.
	t.Run("the collapsed member row wears it and its sibling is untouched", func(t *testing.T) {
		tr := build(t)
		rename(tr, "s1", name)

		want := strings.Join([]string{
			"✦ Sub-Agent (2)",
			groupMemberLine("  ┝ " + name + " ⋯ 1 tool call"),
			groupMemberLine("  ┕ build the docs ⋯ 1 tool call"),
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("renamed group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("the breadcrumb trail names it", func(t *testing.T) {
		tr := build(t)
		rename(tr, "s1", name)

		if got, want := breadcrumbTrail(tr.entries, "s1"), "← main › "+name; got != want {
			t.Errorf("trail = %q, want %q", got, want)
		}
		if got, want := breadcrumbTrail(tr.entries, "s2"), "← main › build the docs"; got != want {
			t.Errorf("sibling trail = %q, want %q — the rename named one member", got, want)
		}
	})

	// The run view's empty box addresses the child on screen BY NAME, so the invitation is a reader
	// of the head like the rest. The fold that renames the head is the same one that re-resolves the
	// box (fold.go), which is why the legend never lags a rename by an event.
	t.Run("the run view's invitation names it", func(t *testing.T) {
		m := modelViewingChild(t, &fakeEngine{}, childRunning)
		m = m.foldEvent(domain.SubAgentNamedEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
			Name:      name,
		})

		if got, want := m.runLabel("s1"), name; got != want {
			t.Errorf("runLabel = %q, want %q", got, want)
		}
		if got := m.input.Placeholder; !strings.Contains(got, name) {
			t.Errorf("placeholder = %q, want it inviting a message to %q", got, name)
		}
	})

	t.Run("the /usage row names it", func(t *testing.T) {
		m := usageModel(t, mainTotals, 8192)
		m = delegate(t, m, "s1", "survey the tests", childTotals, 16384)
		m = delegate(t, m, "s2", "build the docs", childTotals, 0)
		rename(&m.transcript, "s1", name)

		rows := m.usageSubAgentRows(false)
		if len(rows) != 2 {
			t.Fatalf("delegate rows = %q, want the two that spent", rows)
		}
		if got := rows[0][0]; !strings.Contains(got, name) {
			t.Errorf("renamed delegate cell = %q, want it naming %q", got, name)
		}
		if got := rows[1][0]; !strings.Contains(got, "build the docs") {
			t.Errorf("sibling cell = %q, want the task it was given", got)
		}
	})

	// The status line is the surface the rename reaches LAST and the only one composed per frame:
	// the phrase is pure and the name is resolved against the transcript when the row is painted
	// (Model.runningPhrase), so a name that arrived as an event rather than in the call's arguments
	// has to reach the row through the run head the fold rewrote. Asserted off the rendered phrase,
	// never off transcript.runName, because the row is what the human reads.
	t.Run("the status line names it", func(t *testing.T) {
		m := newTestModel(t)
		m = m.foldEvent(domain.ToolCallEvent{
			Call: domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"survey the tests"}`)},
		})
		m = m.foldEvent(domain.TokenEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
			Text:      "working on it",
		})
		if got, want := strip(m.runningPhrase(runRef{}, shownAct(m).since, false)), subAgentActivityName+" · responding · 0s"; got != want {
			t.Fatalf("phrase before the rename = %q, want the unnamed %q", got, want)
		}

		m = m.foldEvent(domain.SubAgentNamedEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
			Name:      name,
		})
		if got, want := strip(m.runningPhrase(runRef{}, shownAct(m).since, false)), name+" · responding · 0s"; got != want {
			t.Errorf("phrase after the rename = %q, want %q", got, want)
		}
	})

	// A rename for a run this view never saw — a record replayed without the head, a child whose
	// event beat its parent's tool call in — renames nothing rather than renaming the last run it
	// finds, and appends nothing either.
	t.Run("an unknown call id renames nothing", func(t *testing.T) {
		tr := build(t)
		before := renderPlain(tr, 80)
		rename(tr, "gone", name)

		if got := renderPlain(tr, 80); got != before {
			t.Errorf("an unknown id moved the paint:\n--- got ---\n%s\n--- want ---\n%s", got, before)
		}
		if len(tr.entries) != 4 {
			t.Errorf("entries = %d, want the 4 the fixture built — a rename appends nothing", len(tr.entries))
		}
	})

	// The control: a delegation the model named itself is one the engine never renames (no event is
	// emitted for it at all), so its row goes on saying what its call said.
	t.Run("a call the model named is unchanged when no event fires", func(t *testing.T) {
		tr := &transcript{}
		delegationCall(tr, "", "s1", "planner", "plan the work", 0)

		if got, want := breadcrumbTrail(tr.entries, "s1"), "← main › planner"; got != want {
			t.Errorf("trail = %q, want %q", got, want)
		}
	})

	// The persistence half. agentName is deliberately off the wire, so the ONLY thing that carries a
	// generated name into a resumed session is the head's Target — which is why the fold sets both.
	// A record saved before the rename and resumed after it would otherwise paint the task's first
	// line the session had already stopped showing (ADR 0068).
	t.Run("a record saved after the rename comes back wearing it", func(t *testing.T) {
		tr := build(t)
		rename(tr, "s1", name)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		entries, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}

		head, ok := runHead(entries, "s1")
		if !ok {
			t.Fatalf("the record came back without the renamed run head: %+v", entries)
		}
		if got := usageAgentName(head); got != name {
			t.Errorf("restored name = %q, want the generated %q off the persisted Target", got, name)
		}
		if got, want := breadcrumbTrail(entries, "s1"), "← main › "+name; got != want {
			t.Errorf("restored trail = %q, want %q", got, want)
		}
	})
}
