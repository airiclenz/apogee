package tui

import (
	"encoding/json"
	"reflect"
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

	t.Run("one member opened to its span", func(t *testing.T) {
		tr := build(t)
		if !tr.setExpanded(secondHead, true) {
			t.Fatalf("setExpanded(%d, true) = false; want the second delegation open", secondHead)
		}
		want := strings.Join([]string{
			header,
			rows[0],
			// Open, the row keeps the very <tool-top-level-details> it wore shut — rows[1] word for
			// word — and opens the frame: ┌ at column 0, the arm across to the branch, and the ▼
			// still at the far edge. Only the body below it is the fold's business.
			leaderEdgeRow("┌─┶ build ✓ ⋯ 1 tool call · all clear", glyphExpanded),
			"│",       // the separator is railed too: the frame does not break under its own corner
			"│ build", // the span opens with the prompt the delegate was handed (item 6)
			"│",
			"│ ✦ Read",
			"│   ┕ b.go ⋯ 5 lines", // the nested read hides nothing, so its row wears no indicator
			"┊",                    // one lone closer ends the span, in the separator's place
			rows[2],                // the list resumes, and its last row still closes the whole group
		}, "\n")
		got := renderPlain(tr, 80)
		if got != want {
			t.Errorf("opened fan-out mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
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
// readable inside it — all before either call has been paired.
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
	opened := strings.Join([]string{
		"✦ Sub-Agent (2)",
		// The open row says exactly what the collapsed one above it does — the count, then the
		// finished delegation's one word — and adds the report beneath it, which is the whole of
		// what the fold ever hid.
		leaderEdgeRow("┌─┶ survey ✓ ⋯ 1 tool call · done", glyphExpanded),
		"  │ found the bug", // the report the phase carried, laid out under the member gutter
		"  │ in the parser",
		"│",
		"│ survey", // the span opens with the prompt the delegate was handed
		"│",
		"│ ✦ Read",
		"│   ┕ a.go ⋯ 5 lines",
		"┊",
		groupMemberLine("  ┕ build ⋯ 1 tool call"),
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
			tr := build(t, tc.burst)
			if !tr.setExpanded(0, true) {
				t.Fatal("setExpanded(0, true) = false; want the finished delegation open")
			}
			if got := renderPlain(tr, 80); got != opened {
				t.Errorf("opened mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, opened)
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
	view := tr.renderView(newTheme(scheme.Default()), width, false)
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

// The ┊ has ONE reason to be drawn, and the spec gives it as a rule rather than as a sketch:
// "`┊` is only displayed if another grouped sub-agent follows after the expanded sub-agent. The last
// sub-agent in the group (if expanded) does not show this" (docs/layout/tool-layout.md, "Grouped
// Sub-agents"). Both halves are read off ONE fixture, opened at the first member and then at the
// last, so what is pinned is the difference the member's POSITION makes — two goldens of two
// fixtures could drift apart and still both pass.
func TestSubAgentCloserOnlyWhenAnotherGroupedMemberFollows(t *testing.T) {
	build := func(t *testing.T, head int) *transcript {
		t.Helper()
		tr := &transcript{}
		loneDelegation(tr, "s1", "survey", "a.go", "all clear")
		loneDelegation(tr, "s2", "build", "b.go", "all clear")
		// The parent's own answer gives the LAST member's span something to stand before: a run at the
		// foot of the transcript has no seam at all, and a rule about seams cannot be read off one.
		tr.apply(domain.MessageEvent{Text: "back to parent"})
		if !tr.setExpanded(head, true) {
			t.Fatalf("setExpanded(%d, true) = false; want that delegation open", head)
		}
		return tr
	}
	const header = "✦ Sub-Agent (2)"

	t.Run("first member open: the list resumes, so its span closes with ┊", func(t *testing.T) {
		want := strings.Join([]string{
			header,
			leaderEdgeRow("┌─┶ survey ✓ ⋯ 1 tool call · all clear", glyphExpanded),
			"│",
			"│ survey",
			"│",
			"│ ✦ Read",
			"│   ┕ a.go ⋯ 5 lines",
			"┊", // another grouped sub-agent follows: the closer parts the span from its row
			groupMemberLine("  ┕ build ✓ ⋯ 1 tool call · all clear"),
			"",
			"✦ back to parent",
		}, "\n")
		if got := renderPlain(build(t, 0), 80); got != want {
			t.Errorf("open first member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("last member open: nothing of the group follows, so no ┊ at all", func(t *testing.T) {
		want := strings.Join([]string{
			header,
			groupMemberLine("  ┝ survey ✓ ⋯ 1 tool call · all clear"),
			leaderEdgeRow("┌─┶ build ✓ ⋯ 1 tool call · all clear", glyphExpanded),
			"│",
			"│ build",
			"│",
			"│ ✦ Read",
			"│   ┕ b.go ⋯ 5 lines",
			// The group is out of rows, so the span is parted from the parent's answer by the ordinary
			// separator — the closer would be claiming a member still to come.
			"",
			"✦ back to parent",
		}, "\n")
		got := renderPlain(build(t, 2), 80)
		if got != want {
			t.Errorf("open last member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
		if strings.Contains(got, glyphRailClose) {
			t.Errorf("a group's last expanded member still drew a %s:\n%s", glyphRailClose, got)
		}
	})
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

// A LONE delegation is drawn in the very frame a grouped one is (design call 3 of
// docs/plans/"2026-08-11 - 01"): folding changes what stands AROUND a delegation and never the
// delegation itself, so the row a reader learned to read in a fan-out reads the same when the run
// happened to stand by itself — ┌─┶ at column 0, the span behind the rail, and the ✓ after the name
// of a delegation that has reported. The ┊ is the one thing it does not take: that closer parts an
// expanded member from the next row of its list, and a lone run has no list (railJoin).
func TestLoneSubAgentRunOpensInTheGroupMembersFrame(t *testing.T) {
	openHead := func(t *testing.T, tr *transcript) {
		t.Helper()
		if !tr.setExpanded(0, true) {
			t.Fatal("setExpanded(0, true) = false; want the delegation open")
		}
	}

	t.Run("opened to its span", func(t *testing.T) {
		tr := &transcript{}
		loneDelegation(tr, "s1", "survey", "a.go", "all clear")
		tr.apply(domain.MessageEvent{Text: "back to parent"}) // gives the run something to close before
		openHead(t, tr)

		want := strings.Join([]string{
			"✦ Sub-Agent",
			// Open, the head keeps the collapsed row's <tool-top-level-details> and adds the span.
			leaderEdgeRow("┌─┶ survey ✓ ⋯ 1 tool call · all clear", glyphExpanded),
			"│",        // the separator is railed: the frame does not break under its own corner
			"│ survey", // the span opens with the prompt the delegate was handed (item 6)
			"│",
			"│ ✦ Read",
			"│   ┕ a.go ⋯ 5 lines",
			"", // nothing of a group follows, so the span ends on the ordinary separator
			"✦ back to parent",
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("lone run mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	// The claim is stronger than "it looks like the sketch": the two rows are compared BYTE FOR BYTE
	// on the same delegation, folded and unfolded, rather than restated as a second golden — two
	// goldens is exactly how the two shapes would come to disagree.
	t.Run("row is the grouped member's, to the byte", func(t *testing.T) {
		lone := &transcript{}
		loneDelegation(lone, "s1", "survey", "a.go", "all clear")
		grouped := &transcript{}
		loneDelegation(grouped, "s1", "survey", "a.go", "all clear")
		loneDelegation(grouped, "s2", "build", "b.go", "all clear")
		openHead(t, lone)
		openHead(t, grouped)

		// Line 0 is the block header ("✦ Sub-Agent", "✦ Sub-Agent (2)"), line 1 the delegation's row.
		row := strings.Split(renderPlain(lone, 80), "\n")[1]
		if member := strings.Split(renderPlain(grouped, 80), "\n")[1]; row != member {
			t.Errorf("lone run's open row = %q; want the grouped member's %q", row, member)
		}
	})

	// The ✓ says a report came off, in the lone shape exactly as in the folded one: a delegation
	// still working has not reported at all, and one that reported a failure is marked by its red
	// outcome slot alone (design call 6). Both states are pinned collapsed and open.
	t.Run("no ✓ while running and none on failure", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			build  func(tr *transcript)
			row    string
			opened string // the same <tool-top-level-details> as row: the fold takes only the body
			prompt string // the task the fixture delegated, as the open span's first line
		}{
			{
				name:   "still working",
				build:  func(tr *transcript) { loneDelegation(tr, "s1", "working", "a.go", "") },
				row:    "  ┕ working ⋯ 1 tool call",
				opened: "┌─┶ working ⋯ 1 tool call",
				prompt: "│ working",
			},
			{
				name: "reported a failure",
				build: func(tr *transcript) {
					loneDelegation(tr, "s1", "broken", "a.go", "")
					tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
						CallID: "s1", Content: "it fell over", IsError: true}})
				},
				row:    "  ┕ broken ⋯ 1 tool call · error: it fell over",
				opened: "┌─┶ broken ⋯ 1 tool call · error: it fell over",
				prompt: "│ broken",
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				tr := &transcript{}
				tc.build(tr)

				collapsed := strings.Join([]string{"✦ Sub-Agent", groupMemberLine(tc.row)}, "\n")
				if got := renderPlain(tr, 80); got != collapsed {
					t.Errorf("collapsed mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
				}
				openHead(t, tr)
				want := strings.Join([]string{
					"✦ Sub-Agent",
					leaderEdgeRow(tc.opened, glyphExpanded),
					"│",
					tc.prompt,
					"│",
					"│ ✦ Read",
					"│   ┕ a.go ⋯ 5 lines",
				}, "\n")
				if got := renderPlain(tr, 80); got != want {
					t.Errorf("opened mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
				}
			})
		}
	})
}

// Opening a delegation ADDS — its report, the prompt it was handed, the whole railed span — and
// takes nothing away: the open row keeps the very <tool-top-level-details> the shut row wore, the
// run's own count of the work, the delegate's context fill and its gist
// (docs/layout/tool-layout.md, Grouped Sub-agents). A head reverting to its raw view on the way
// open would tell a reader LESS about the delegation than leaving it shut, which is the one thing
// an expand may never do.
//
// The claim is pinned by CONSTRUCTION rather than by two goldens that could drift: the slot text is
// written once and stands in the collapsed golden and the open one alike, running member and
// finished member both.
func TestExpandedSubAgentKeepsItsTopLevelDetails(t *testing.T) {
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
		t.Errorf("collapsed group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
	}

	for _, tc := range []struct {
		name string
		head int
		want []string
	}{
		{
			name: "a running member keeps its count and fill",
			head: 0,
			want: []string{
				header,
				leaderEdgeRow("┌─┶ "+working, glyphExpanded),
				"│",
				"│ survey",
				"│",
				"│ ✦ Read",
				"│   ┕ a.go ⋯ 5 lines",
				"┊",
				groupMemberLine("  ┕ " + done),
			},
		},
		{
			name: "a finished member keeps its count and gist",
			head: 2,
			want: []string{
				header,
				groupMemberLine("  ┝ " + working),
				leaderEdgeRow("┌─┶ "+done, glyphExpanded),
				"│",
				"│ build",
				"│",
				"│ ✦ Read",
				"│   ┕ b.go ⋯ 5 lines",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := build(t)
			if !tr.setExpanded(tc.head, true) {
				t.Fatalf("setExpanded(%d, true) = false; want that delegation open", tc.head)
			}
			if got, want := renderPlain(tr, 80), strings.Join(tc.want, "\n"); got != want {
				t.Errorf("open member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// delegateWithPrompt folds one delegation carrying an ARBITRARY task — newlines, backticks and all
// — with a nested read behind it and a report closing it. It marshals the arguments rather than
// splicing them into a JSON literal the way subAgentCall's one-word fixture does, which is what lets
// a golden pin a real multi-line markdown prompt.
func delegateWithPrompt(t *testing.T, tr *transcript, id, task string) {
	t.Helper()
	args, err := json.Marshal(map[string]string{"task": task})
	if err != nil {
		t.Fatalf("marshal the delegated task: %v", err)
	}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "sub_agent", Arguments: args}})
	readCall(tr, "r"+id, "a.go", 1, 5, 1)
	subAgentReport(tr, id, "all clear", 0)
}

// An EXPANDED delegation's span opens with what the delegation was actually asked
// (docs/layout/tool-layout.md, "Grouped Sub-agents"): one blank rail line, the task rendered through
// the transcript's own markdown pipeline behind the rail, then the blank line the span's first block
// already brings with it. The header row says what the delegation IS — its name, or the task's
// clipped first line — and this says what it was GIVEN, which is the half a collapsed row has no
// room for.
func TestExpandedSubAgentOpensWithItsPrompt(t *testing.T) {
	// A heading, a paragraph too long for the railed column, and a bullet list: enough markdown that
	// a plain wrap of the raw text could not produce the golden below.
	const task = "# Survey\n\nRead the tests and report the gaps you find.\n\n- read `a.go`\n- be brief"

	openHead := func(t *testing.T, tr *transcript, at int) {
		t.Helper()
		if !tr.setExpanded(at, true) {
			t.Fatalf("setExpanded(%d, true) = false; want the delegation open", at)
		}
	}

	// The width is deliberately narrow: the paragraph must WRAP, and it must wrap to the column left
	// inside the run's own rail rather than to the terminal's, which is the whole of "behind the rail".
	t.Run("the markdown prompt wraps and formats inside the rail", func(t *testing.T) {
		tr := &transcript{}
		delegateWithPrompt(t, tr, "s1", task)
		openHead(t, tr, 0)

		want := strings.Join([]string{
			"✦ Sub-Agent",
			// The open head carries the run's own <tool-top-level-details> like any other, and at
			// 34 columns that line leaves the name too little room to stand in — so the
			// promote-guard demotes it to the head's first body row and the typed `done` takes the
			// slot (guardRefuses). The prompt begins under the body it laid out.
			leaderEdgeRow("┌─┶ # Survey ✓ ⋯ done", glyphExpanded),
			"    1 tool call · all clear",
			seeLessFooterLine(t, 34),
			"│", // the frame's opening row: one blank rail line, never two
			"│ Survey",
			"│",
			"│ Read the tests and report the",
			"│ gaps you find.",
			"│",
			"│ • read a.go",
			"│ • be brief",
			"│", // the closing row is the span's own block separator, railed (railJoin)
			"│ ✦ Read",
			"│   ┕ a.go ⋯ 5 lines",
		}, "\n")
		if got := renderPlain(tr, 34); got != want {
			t.Errorf("prompt body mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	// Folding changes the frame around a delegation and never what the delegation shows of itself, so
	// a member of a group opens on the very same prompt rows. They are compared BYTE FOR BYTE against
	// the lone run's rather than restated as a second golden.
	t.Run("a grouped member opens on the same prompt", func(t *testing.T) {
		lone := &transcript{}
		delegateWithPrompt(t, lone, "s1", task)
		grouped := &transcript{}
		delegateWithPrompt(t, grouped, "s1", task)
		delegateWithPrompt(t, grouped, "s2", "second")
		openHead(t, lone, 0)
		openHead(t, grouped, 0)

		// The prompt runs from the bare rail line that opens the frame to the span's first block. What
		// stands ABOVE it is each painter's own reading of the head's report — the lone block closes
		// its body with a see-less footer and the group member gutters its own — and that difference
		// is not this claim.
		promptOf := func(painted string) []string {
			rows := strings.Split(painted, "\n")
			for i, row := range rows {
				if row != "│" {
					continue
				}
				for j := i; j < len(rows); j++ {
					if strings.Contains(rows[j], "✦ Read") {
						return rows[i:j]
					}
				}
				break
			}
			t.Fatalf("no prompt frame stood before the span:\n%s", painted)
			return nil
		}
		want, got := promptOf(renderPlain(lone, 34)), promptOf(renderPlain(grouped, 34))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("grouped member's prompt = %q; want the lone run's %q", got, want)
		}
	})

	// A delegation with no prompt to show opens no block at all — not a blank rail line over an empty
	// row. The two cases are the same claim: an argument that was never sent (a record written before
	// the text was retained) and one that carries nothing but whitespace. Each pins its OWN head rows,
	// because an empty task also empties the header's target and that is a shape this item did not
	// choose — what it asserts is what stands between those rows and the span.
	for _, tc := range []struct {
		name, task string
		head       []string
	}{
		{
			name: "no task at all",
			task: "",
			// With no target the block wears the targetless shape: the indicator rides the header
			// and the one-line report lays out as a body under it (renderToolBlock).
			head: []string{"✦ Sub-Agent ▼", "  ┕ 1 tool call · all clear", seeLessFooterLine(t, 80)},
		},
		{
			name: "whitespace alone",
			task: "  \n\t\n ",
			head: []string{"✦ Sub-Agent", leaderEdgeRow("┌─┶    ✓ ⋯ 1 tool call · all clear", glyphExpanded)},
		},
	} {
		t.Run("an empty prompt opens no block: "+tc.name, func(t *testing.T) {
			tr := &transcript{}
			delegateWithPrompt(t, tr, "s1", tc.task)
			openHead(t, tr, 0)

			want := strings.Join(append(append([]string{}, tc.head...),
				"│", // the span's own separator, and nothing else: no stray blank rows
				"│ ✦ Read",
				"│   ┕ a.go ⋯ 5 lines"), "\n")
			if got := renderPlain(tr, 80); got != want {
				t.Errorf("empty-prompt mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// delegateAsked folds one delegation the way the engine emits it: the sub_agent call carrying an
// arbitrary prompt, the started phase a worker emits the instant it takes the job, and one nested
// call the child made — stamped with the SPAWNING call's id, which is what keeps that work in this
// delegation's own stretch of the entry list while siblings run at once (transcript.place, ADR
// 0039). Without the stamp a fan-out's child work lands behind whichever delegation was announced
// last, and the member under test would be left with no span to open at all.
//
// report closes the run; "" leaves it working, so ONE fixture serves both halves of every pair
// below rather than a running shape and a finished one being built by two different hands.
func delegateAsked(t *testing.T, tr *transcript, id, task, report string) {
	t.Helper()
	args, err := json.Marshal(map[string]string{"task": task})
	if err != nil {
		t.Fatalf("marshal the delegated task: %v", err)
	}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "sub_agent", Arguments: args}})
	subAgentStarted(tr, id, 1)
	childCall(tr, id, "r"+id, "a.go")
	if report != "" {
		subAgentReport(tr, id, report, 0)
	}
}

// Exactly ONE railed blank line stands between an expanded delegation's own rows and the prompt it
// was handed (docs/layout/tool-layout.md, "Grouped Sub-agents") — never none, and never two. It is
// the spacer [subAgentPromptRows] opens the span with, and what it parts is the two halves of the
// frame: what the delegation SAYS of itself above, what it was ASKED below.
//
// The claim is pinned across the whole matrix of expanded readings because the rows directly above
// that spacer are different in every one of them and the spacer must not be. A lone run and a
// grouped member wear different frames; a run still working has no report at all; a short report is
// promoted into the header's summary slot and leaves no body behind it, while a long one lays out
// as one — flush under a lone head, behind the member gutter when the delegation is folded into a
// list. Six shapes, one blank line, in the same place in each.
func TestExpandedSubAgentPromptOpensOnOneBlankRailLine(t *testing.T) {
	const task = "Survey the tests\n\nReport every gap you find, and be brief about it."
	// A report too long for the summary slot becomes the head's BODY, which is what puts rows
	// between the header row and the prompt's spacer.
	longReport := strings.Repeat("the tests cover the parser and nothing else at all. ", 3)

	// The prompt as it stands inside the frame — the spacer, then the task's two paragraphs rendered
	// as markdown behind the rail — and the span opening under it. Both are stated once, so what a
	// case below carries of its own is only ever the head's rows.
	prompt := []string{
		"│",
		"│ Survey the tests",
		"│",
		"│ Report every gap you find, and be brief about it.",
	}
	span := []string{"│", "│ ✦ Read", "│   ┕ a.go ⋯ 1 - 10"}
	// The list resumes after the open member's span: the ┊ closing it, then the sibling's own row.
	sibling := []string{"┊", groupMemberLine("  ┕ survey the docs ⋯ 1 tool call")}

	for _, tc := range []struct {
		name  string
		build func(t *testing.T) *transcript
		want  []string
	}{
		{
			name: "a lone run still working",
			build: func(t *testing.T) *transcript {
				tr := &transcript{}
				delegateAsked(t, tr, "s1", task, "")
				return tr
			},
			want: slices.Concat([]string{
				"✦ Sub-Agent",
				leaderEdgeRow("┌─┶ Survey the tests ⋯ 1 tool call", glyphExpanded),
			}, prompt, span),
		},
		{
			name: "a lone run whose report was promoted into the slot",
			build: func(t *testing.T) *transcript {
				tr := &transcript{}
				delegateAsked(t, tr, "s1", task, "all clear")
				return tr
			},
			want: slices.Concat([]string{
				"✦ Sub-Agent",
				leaderEdgeRow("┌─┶ Survey the tests ✓ ⋯ 1 tool call · all clear", glyphExpanded),
			}, prompt, span),
		},
		{
			name: "a lone run whose report became a body",
			build: func(t *testing.T) *transcript {
				tr := &transcript{}
				delegateAsked(t, tr, "s1", task, longReport)
				return tr
			},
			want: slices.Concat([]string{
				"✦ Sub-Agent",
				leaderEdgeRow("┌─┶ Survey the tests ✓ ⋯ done", glyphExpanded),
				"    1 tool call · the tests cover the parser and nothing else at all. the tests",
				"    cover the parser and nothing else at all. the tests cover the parser and",
				"    nothing else at all.",
				seeLessFooterLine(t, 80),
			}, prompt, span),
		},
		{
			name: "a grouped member still working",
			build: func(t *testing.T) *transcript {
				tr := &transcript{}
				delegateAsked(t, tr, "s1", task, "")
				delegateAsked(t, tr, "s2", "survey the docs", "")
				return tr
			},
			want: slices.Concat([]string{
				"✦ Sub-Agent (2)",
				leaderEdgeRow("┌─┶ Survey the tests ⋯ 1 tool call", glyphExpanded),
			}, prompt, span, sibling),
		},
		{
			name: "a grouped member whose report was promoted into the slot",
			build: func(t *testing.T) *transcript {
				tr := &transcript{}
				delegateAsked(t, tr, "s1", task, "all clear")
				delegateAsked(t, tr, "s2", "survey the docs", "")
				return tr
			},
			want: slices.Concat([]string{
				"✦ Sub-Agent (2)",
				leaderEdgeRow("┌─┶ Survey the tests ✓ ⋯ 1 tool call · all clear", glyphExpanded),
			}, prompt, span, sibling),
		},
		{
			name: "a grouped member whose report became a body",
			build: func(t *testing.T) *transcript {
				tr := &transcript{}
				delegateAsked(t, tr, "s1", task, longReport)
				delegateAsked(t, tr, "s2", "survey the docs", "")
				return tr
			},
			// The body sits behind the member gutter, and the spacer under it is railed at the run's
			// own depth: two different columns, one blank line between them and the prompt.
			want: slices.Concat([]string{
				"✦ Sub-Agent (2)",
				leaderEdgeRow("┌─┶ Survey the tests ✓ ⋯ done", glyphExpanded),
				"  │ 1 tool call · the tests cover the parser and nothing else at all. the",
				"  │ tests cover the parser and nothing else at all. the tests cover the parser",
				"  │ and nothing else at all.",
			}, prompt, span, sibling),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := tc.build(t)
			if !tr.setExpanded(0, true) {
				t.Fatal("setExpanded(0, true) = false; want the delegation open")
			}
			if got, want := renderPlain(tr, 80), strings.Join(tc.want, "\n"); got != want {
				t.Errorf("expanded prompt frame mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// A delegation with NO span joins its neighbours' list exactly as a full one does (subAgentGroup,
// item 7). The painter's span rule cannot carry the group on its own — it fires only for a head
// with nested entries behind it, and a delegation refused at the depth bound (executeRefuse,
// internal/agent) leaves a head with none — so the group is derived off the TOOL and two refusals
// in a row read as one "✦ Sub-Agent (2)" with a red row each.
//
// What the presenter's solo mark still keeps out is the MIXED umbrella (design call 12): these
// heads are ungroupable in [sameLabelRun]'s sense and so head no super-group, which is the other
// half of the rule and is asserted here beside the paint.
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
	settled := replayed.renderView(newTheme(scheme.Default()), 80, false)
	blinked := replayed.renderView(newTheme(scheme.Default()), 80, true)
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

	t.Run("a lone failed run is red shut and open", func(t *testing.T) {
		tr := &transcript{}
		loneDelegation(tr, "s1", "broken", "a.go", "")
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "s1", Content: "it fell over", IsError: true}})

		assertSlot(t, paintedRow(t, tr, failedSlot), failedSlot, true, false)

		if !tr.setExpanded(0, true) {
			t.Fatal("setExpanded(0, true) = false; want the delegation open")
		}
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

		want := "✦ Sub-Agent\n  ┕ survey the tests ⋯ " + refusedResult
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

	t.Run("a delegation with a span keeps its framed prompt", func(t *testing.T) {
		tr := &transcript{}
		subAgentCall(tr, "s1", refusedTask, 0)
		readCall(tr, "r1", "a.go", 1, 5, 1)
		subAgentReport(tr, "s1", "all clear", 0)
		if !tr.setExpanded(0, true) {
			t.Fatalf("setExpanded(0, true) = false; want the delegation open")
		}

		// The framed reading paints the prompt INSIDE the rail as markdown (subAgentPromptRows), so
		// the unframed body's lead line must not appear anywhere in it.
		got := renderPlain(tr, width)
		if strings.Contains(got, unframedSubAgentPromptLead) {
			t.Errorf("a framed delegation grew the unframed lead line:\n%s", got)
		}
		if !strings.Contains(got, "│ survey the tests") {
			t.Errorf("a framed delegation lost its railed prompt:\n%s", got)
		}
	})
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
