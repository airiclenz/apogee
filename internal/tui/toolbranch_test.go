package tui

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// A member whose result has not landed shows its target and a leader running to the row's edge with
// nothing in the outcome slot; when the result folds in, the whole block repaints with that member's
// outcome in the slot.
func TestRenderGroupWithInFlightMember(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "README.md", 1, 154, 0)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "read_file", Arguments: []byte(`{"path":"TODO.md"}`)}})

	want := strings.Join([]string{
		"✦ Read (2)",
		"  ┝ README.md ⋯ 154 lines",
		"  ┕ TODO.md ⋯",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("in-flight member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2",
		Content: "[File: TODO.md, 408 lines total, showing lines 1-408]\n…",
		Summary: domain.ReadSpan{Start: 1, End: 408, Total: 408}}})
	want = strings.Join([]string{
		"✦ Read (2)",
		"  ┝ README.md ⋯ 154 lines",
		"  ┕ TODO.md ⋯ 408 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("re-rendered group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A lone call renders in the shape a group does — a label header, target leading the branch, the
// outcome at the row's right edge behind a leader — and counts nothing: the "(N)" is a group's
// arithmetic and a block of one has none to state. A second call joins by adding a line rather
// than by moving the first one's target: there is no column to re-measure, the leader simply
// absorbs whatever the two targets differ by.
func TestRenderSingleCallSharesTheGroupShape(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "main.go", 1, 154, 0)

	want := strings.Join([]string{
		"✦ Read",
		"  ┕ main.go ⋯ 154 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("single-call block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// …and a second call joins it by adding a line, not by moving the first one's target.
	readCall(tr, "c2", "a-much-longer-name.go", 1, 9, 0)
	want = strings.Join([]string{
		"✦ Read (2)",
		"  ┝ main.go ⋯ 154 lines",
		"  ┕ a-much-longer-name.go ⋯ 9 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("grown block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A call with a BODY keeps the same header and leader row — its typed stat in the outcome slot at
// the row's right edge — and the body lays out beneath it at the branch marker's width: those lines
// are not ┝/┕ branches of their own, because only calls are (docs/layout/tool-layout.md,
// "Single tool expanded"). COLLAPSED,
// none of them lays out at all: the block spends its one row on the leader and that row's own slot
// counts the body whole (collapsedBodyRows, collapsedRemainder), which is the shape the sketch draws.
func TestRenderMultiDetailStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: "ok   apogee/internal/tui   0.412s\nok   apogee/internal/agent   1.203s\nPASS\n",
	}})

	want := strings.Join([]string{
		"✦ Terminal", // a hidden body is something to reveal, and the branch row's ▶ says so
		groupMemberLine("  ┕ go test ./... ⋯ exit 0 · +3 more lines"),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("multi-detail block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A diff call is the summary-and-body shape layout.md sketches: the diffstat fills the outcome
// slot on the path's leader row and the coloured body hangs beneath it. The body keeps its red/green
// colouring, which — together with having a body at all — is why it can never fold into a group.
// Asserted expanded, because a collapsed diff paints no body line at all (collapsedBodyRows) and
// there would be no colour to see.
func TestRenderDiffDetailStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
		Content: "- a removed line\n+ an added line",
		Summary: domain.DiffStat{Added: 1, Removed: 1}}})
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the diff block expanded")
	}

	want := strings.Join([]string{
		"✦ Diff Preview",
		leaderEdgeRow("  ┕ main.go ⋯ +1 −1", glyphExpanded),
		"    - a removed line",
		"    + an added line",
		seeLessFooterLine(t, 80),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("diff block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	th := newTheme(scheme.Default())
	lines := tr.renderLines(th, 80)
	if got, want := lines[2], th.diffRemoved.Render("    - a removed line"); got != want {
		t.Errorf("removed line = %q; want the diffRemoved style %q", got, want)
	}
	if got, want := lines[3], th.diffAdded.Render("    + an added line"); got != want {
		t.Errorf("added line = %q; want the diffAdded style %q", got, want)
	}
}

// The layout.md sketch, rendered: a two-line change shows "+2 −2" in the outcome slot at the right
// edge of the path's leader row with the diff body beneath it, and the diffstat itself wears the
// outcome slot's own marker tone rather than the diff's red and green — only the body carries those,
// so the row reads like every other tool's summary. The sketch is the EXPANDED
// shape: a collapsed diff hides its body whole like every other block (collapsedBodyRows), so its
// hunks are what a click reveals.
func TestRenderDiffMatchesLayoutSketch(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: "- a code line that has been removed\n- a second removed line\n+ a new code line\n+ a second new line",
		Summary: domain.DiffStat{Added: 2, Removed: 2},
	}})
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the diff block expanded")
	}

	want := strings.Join([]string{
		"✦ Diff Preview",
		leaderEdgeRow("  ┕ main.go ⋯ +2 −2", glyphExpanded),
		"    - a code line that has been removed",
		"    - a second removed line",
		"    + a new code line",
		"    + a second new line",
		seeLessFooterLine(t, 80),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("diff sketch mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	th := newTheme(scheme.Default())
	if got, want := tr.renderLines(th, 80)[1], th.toolMarkerBright.Render("+2 −2"); !strings.Contains(got, want) {
		t.Errorf("diffstat branch = %q; want its outcome slot in the marker tone of an OPEN block %q", got, want)
	}
}

// A diff whose body is hidden still names the whole change on its branch: the diffstat counts
// every line, and the count beside it in the same slot says how many the collapsed paint withheld.
func TestRenderDiffStatSurvivesTheBodyCap(t *testing.T) {
	const longDiff = 25 // well past the collapsed budget, so the stat and the paint cannot agree by luck
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: strings.TrimSuffix(strings.Repeat("+ added\n", longDiff), "\n"),
		Summary: domain.DiffStat{Added: longDiff},
	}})

	lines := strings.Split(renderPlain(tr, 80), "\n")
	hidden := strconv.Itoa(longDiff)
	if got, want := lines[1], groupMemberLine("  ┕ main.go ⋯ +"+hidden+" −0 · +"+hidden+" more lines"); got != want {
		t.Errorf("capped diff branch = %q, want %q (the stat spans the whole diff)", got, want)
	}
	if len(lines) != 2 {
		t.Errorf("the collapsed diff paints %d rows, want its header and one branch:\n%s",
			len(lines), strings.Join(lines, "\n"))
	}
}

// TestCollapsedPaintTruncatesRetainedBodies pins the relocation itself: the entry KEEPS every
// body line it was given and the collapsed paint is the only thing that withholds them,
// synthesizing the "+N more lines" remainder the outcome builders used to bake in (layout.md,
// "Collapsed and expanded blocks" — truncation is a render-time act on retained facts). One budget
// answers for every body kind: a command's output and a diff alike paint NO body line collapsed
// (collapsedBodyRows) and the branch row's own slot counts the body whole, down to a body of one
// line — there is no length at which a collapsed block starts previewing its output.
func TestCollapsedPaintTruncatesRetainedBodies(t *testing.T) {
	diffLines := func(n int) string {
		return strings.TrimSuffix(strings.Repeat("+ added\n", n), "\n")
	}
	cases := []struct {
		name      string
		build     func(tr *transcript)
		wantKept  int    // body lines the entry retains
		wantCount string // what the branch row's slot says about the lines it withheld
	}{
		{
			name: "free-form output paints no line and counts them all",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
			},
			wantKept:  4,
			wantCount: "+4 more lines",
		},
		{
			name: "a diff body spends the same budget and is counted the same way",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: diffLines(4), Summary: domain.DiffStat{Added: 4}}})
			},
			wantKept:  4,
			wantCount: "+4 more lines",
		},
		{
			name: "a body of one line is hidden and counted like any other",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: "+ new line", Summary: domain.DiffStat{Added: 1}}})
			},
			wantKept:  1,
			wantCount: "+1 more line",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)
			if got := tr.entries[0].tool.Details.len(); got != tc.wantKept {
				t.Errorf("retained body = %d lines, want the whole %d", got, tc.wantKept)
			}
			// The collapsed block is a header and a branch line and nothing else: what it made of
			// the retained lines is the count in that branch's outcome slot.
			lines := strings.Split(renderPlain(tr, 80), "\n")
			if len(lines) != 2 {
				t.Fatalf("the collapsed block paints %d rows, want its header and one branch:\n%s",
					len(lines), strings.Join(lines, "\n"))
			}
			if !strings.HasSuffix(lines[1], leaderEdgeRow(tc.wantCount, glyphCollapsed)) {
				t.Errorf("collapsed branch = %q; want its slot to end in the count %q", lines[1], tc.wantCount)
			}
		})
	}
}

// TestExpandedBlockPaintsItsWholeBody pins what the expanded state is FOR: the block paints every
// body line the entry retained and counts nothing — its leader row gives the count up with the last
// hidden line — and collapsing it again paints exactly the compact shape back. The round trip runs over one transcript rather than two
// fixtures, because that is the claim — nothing about the entry changes but the flag the painter
// reads (layout.md, "Collapsed and expanded blocks").
func TestExpandedBlockPaintsItsWholeBody(t *testing.T) {
	diffContent := func(n int) string { return strings.TrimSuffix(strings.Repeat("+ added\n", n), "\n") }
	paintedDiff := func(n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, "    + added")
		}
		return out
	}
	cases := []struct {
		name         string
		build        func(tr *transcript)
		wantCount    string // the collapsed branch row's count of the body behind it
		wantExpanded []string
	}{
		{
			name: "free-form output expands from nothing to all of it",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
			},
			wantCount:    "+4 more lines",
			wantExpanded: []string{"    ok   a", "    ok   b", "    ok   c", "    PASS"},
		},
		{
			name: "a diff body expands from its counted slot to its hunks",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
					Content: diffContent(4), Summary: domain.DiffStat{Added: 4}}})
			},
			wantCount:    "+4 more lines",
			wantExpanded: paintedDiff(4),
		},
		{
			// The written lines are the body from the moment the call is announced, so this one
			// spends and expands past the budget with no result involved at all.
			name: "a write's own lines are hidden collapsed and expand whole",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "write_file",
					Arguments: []byte(`{"path":"notes.txt","content":"alpha\nbeta\ngamma\ndelta"}`)}})
			},
			wantCount:    "+4 more lines",
			wantExpanded: []string{"    + alpha", "    + beta", "    + gamma", "    + delta"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)
			// The block is a header, a branch line, then its body: everything past the branch is
			// what the block's state made of the retained lines — nothing at all while it is
			// collapsed, the count for it riding the branch row's own slot.
			rows := func() []string { return strings.Split(renderPlain(tr, 80), "\n") }
			body := func() string { return strings.Join(rows()[2:], "\n") }
			collapsed := func(t *testing.T, when string) {
				t.Helper()
				if lines := rows(); len(lines) != 2 {
					t.Errorf("%s paint stands %d rows, want its header and one branch:\n%s",
						when, len(lines), strings.Join(lines, "\n"))
				} else if !strings.HasSuffix(lines[1], leaderEdgeRow(tc.wantCount, glyphCollapsed)) {
					t.Errorf("%s branch = %q; want its slot to end in the count %q", when, lines[1], tc.wantCount)
				}
			}

			collapsed(t, "default (collapsed is the default)")
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the tool-call entry toggled")
			}
			// The expanded body ends in the see-less footer, whatever filled it: the extra collapse
			// target every open block grows (seeLessFooter, render.go).
			wantExpanded := append(append([]string(nil), tc.wantExpanded...), seeLessFooterLine(t, 80))
			if got, want := body(), strings.Join(wantExpanded, "\n"); got != want {
				t.Errorf("expanded paint mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if painted := strings.Join(rows(), "\n"); strings.Contains(painted, "more line") {
				t.Errorf("the expanded block kept a remainder count:\n%s", painted)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false on the way back; want the entry toggled")
			}
			collapsed(t, "re-collapsed")
		})
	}
}

// TestExpandedBlockLiftsItsDetailTone is design call 9 in the paint: a block's own text is dim while
// it is collapsed and a step brighter once it is open (the scheme's `muted-bright` role), so the block a
// reader opened stands out of the scrollback of closed ones around it. It holds for both shapes that
// have a state — the single block and the group member, which are painted by different functions
// (renderToolBranch, renderExpandedMember) and could drift apart.
//
// The tones are asserted as the theme's own roles rather than as SGR bytes, and the guard above the
// subtests fails the day the two roles resolve to the same colour: a contrast step that quietly went
// away would satisfy every equality beneath it.
func TestExpandedBlockLiftsItsDetailTone(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	if th.toolDetail.Render("x") == th.toolDetailBright.Render("x") {
		t.Fatal("the collapsed and the open detail tone paint identically; there is no contrast step to assert")
	}

	t.Run("a single block", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal",
			Arguments: []byte(`{"command":"go test ./..."}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nPASS"}})

		// Row 0 is the header and row 2 the remainder marker — chrome with roles of their own — so
		// row 1, the branch line, is the whole of what the collapsed paint says about the call. Its
		// own leader and indicator are chrome too, so the tone is asserted on the TARGET it carries.
		collapsed := tr.renderLines(th, 80)
		if want := th.toolDetail.Render("go test ./..."); !strings.Contains(collapsed[1], want) {
			t.Errorf("collapsed branch = %q; want its target in the dim tone %q", collapsed[1], want)
		}

		if !tr.setExpanded(0, true) {
			t.Fatal("setExpanded(0, true) = false; want the block opened")
		}
		open := tr.renderLines(th, 80)
		if want := th.toolDetailBright.Render("go test ./..."); !strings.Contains(open[1], want) {
			t.Errorf("open branch = %q; want its target in the brighter tone %q", open[1], want)
		}
		// Open, every body row below the branch is the call's own text and lifts whole — the
		// see-less footer closing the body apart, which is apogee's own affordance and wears the
		// marker tone (seeLessFooter).
		for i, row := range open[2 : len(open)-1] {
			if want := th.toolDetailBright.Render(strip(row)); row != want {
				t.Errorf("open row %d = %q; want the brighter tone %q", i+2, row, want)
			}
		}
	})

	t.Run("a group member", func(t *testing.T) {
		// Both members carry a MULTI-line body: a one-line output rides the branch as the call's
		// summary instead, which would leave the member with nothing to open.
		tr := runGroup(0, [2]string{"go build ./...", "ok\nbuilt"}, [2]string{"go vet ./...", "clean\ndone"})
		if !tr.setExpanded(1, true) {
			t.Fatal("setExpanded(1, true) = false; want the second member opened")
		}
		rows := tr.renderLines(th, 80)

		// A member row is not one style run — its ▶/▼ and, open, its gutter are chrome painted
		// beside the text — so the tone is asserted on the text the member is carrying.
		if want := th.toolDetail.Render("go build ./..."); !strings.Contains(rows[1], want) {
			t.Errorf("the closed member = %q; want its row in the dim tone %q", rows[1], want)
		}
		if want := th.toolDetailBright.Render("go vet ./..."); !strings.Contains(rows[2], want) {
			t.Errorf("the open member's first row = %q; want its target in the brighter tone %q", rows[2], want)
		}
		if want := th.toolDetailBright.Render("clean"); !strings.Contains(rows[3], want) {
			t.Errorf("the open member's body = %q; want it in the brighter tone %q", rows[3], want)
		}
		if want := th.toolDetail.Render(memberGutter); !strings.Contains(rows[3], want) {
			t.Errorf("the open member's body = %q; want the gutter beside it still chrome %q", rows[3], want)
		}
	})
}

// …and the tone step is the PLAIN detail's alone: a diff line is red or green because of which way
// it went, and layering an emphasis step onto that would give the same colour two meanings. The two
// states are asked of the two painters that draw a targetless branch list — the collapsed one under
// the row budget (clipDetails) and the open one (renderDetails) — over the same line, so the
// comparison is of paint rather than of the style table. The plain case is the control: the same
// pair of painters must NOT agree there, or the diff assertion would hold by the tone step having
// gone missing altogether.
func TestDiffLinesKeepTheirColourInBothBlockStates(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	cases := []struct {
		name     string
		kind     detailKind
		wantSame bool
		style    lipgloss.Style
	}{
		{name: "an added line keeps its green", kind: detailDiffAdded, wantSame: true, style: th.diffAdded},
		{name: "a removed line keeps its red", kind: detailDiffRemoved, wantSame: true, style: th.diffRemoved},
		{name: "a plain line takes the state's tone", kind: detailPlain, wantSame: false, style: th.toolDetail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := []detailLine{{Kind: tc.kind, Text: "+ added"}}
			closed, _ := clipDetails(th, line, 40)
			open := renderDetails(th, line, 40)
			if len(closed) != 1 || len(open) != 1 {
				t.Fatalf("the painters spent %d and %d rows on one line; want one each", len(closed), len(open))
			}
			if same := closed[0] == open[0]; same != tc.wantSame {
				t.Errorf("closed = %q, open = %q; want the two paints same=%v", closed[0], open[0], tc.wantSame)
			}
			if want := tc.style.Render(strip(closed[0])); closed[0] != want {
				t.Errorf("closed paint = %q; want the kind's own style %q", closed[0], want)
			}
		})
	}
}

// TestCollapsedBlockStandsAtMostTwoRows is the cap itself, asked of the case that used to break
// it: a 400-character command soft-wrapped over five rows before the row budget existed, and the
// block it led stood seven rows tall in a scrollback of them. Now the block is its header and ONE
// leader row — the clip's " …" saying the target goes on, that row's own slot counting the body
// behind it — whatever the target's length and whatever the body's (docs/layout/tool-layout.md).
//
// The width bound is asserted on every row rather than assumed from the wrap: the clip re-cuts the
// row it ends, and a tail appended past the column would fold that row in two and spend a row the
// budget does not have (clipWrap).
func TestCollapsedBlockStandsAtMostTwoRows(t *testing.T) {
	const width = 80
	command := strings.Repeat("cd . && head -3 go.mod && ", 16)[:400]

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok\nPASS\ndone"}})

	lines := strings.Split(renderPlain(tr, width), "\n")
	if len(lines) != 2 {
		t.Fatalf("the collapsed block stands %d rows tall, want the budget's 2:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if want := "✦ Terminal"; lines[0] != want {
		t.Errorf("header = %q, want %q — the indicator rides the branch row now", lines[0], want)
	}
	if !strings.HasSuffix(lines[1], glyphCollapsed) || !strings.Contains(lines[1], clipTail) {
		t.Errorf("branch row = %q, want the target cut short with %q and the row wearing %q",
			lines[1], clipTail, glyphCollapsed)
	}
	if want := "+3 more lines"; !strings.Contains(lines[1], want) {
		t.Errorf("branch row = %q, want its slot to count the hidden body with %q", lines[1], want)
	}
	th := newTheme(scheme.Default())
	for i, ln := range lines {
		if w := th.measure.Width(ln); w > width {
			t.Errorf("row %d paints %d columns, past the %d-column block: %q", i, w, width, ln)
		}
	}
}

// …but the target's own clip is NOT enough to make a block a toggle target any more. A leader row
// is one row in both states (leaderRow), so a bodiless call whose path the width cuts shows exactly
// the same row open as closed — and the canon spec's rule for that case is that the row carries no
// indicator at all (docs/layout/tool-layout.md). The block therefore wears nothing, marks nothing,
// and a click on it keeps its selection meaning, at any width.
func TestClippedTargetAloneIsNoToggleTarget(t *testing.T) {
	const width = 60
	path := "internal/" + strings.Repeat("deeply-nested-package/", 6) + "main.go"

	tr := &transcript{}
	readCall(tr, "c1", path, 1, 154, 0)
	if got := tr.entries[0].tool.Details.len(); got != 0 {
		t.Fatalf("the fixture's call carries %d body lines, want the bodiless case", got)
	}

	collapsed := strings.Split(renderPlain(tr, width), "\n")
	if len(collapsed) != 2 || !strings.Contains(collapsed[1], clipTail) {
		t.Errorf("collapsed paint = %d rows, last %q, want 2 rows with the clip tail in the branch:\n%s",
			len(collapsed), collapsed[len(collapsed)-1], strings.Join(collapsed, "\n"))
	}
	for i, ln := range collapsed {
		if strings.HasSuffix(ln, glyphCollapsed) || strings.HasSuffix(ln, glyphExpanded) {
			t.Errorf("row %d = %q wears an indicator; a cut target reveals nothing", i, ln)
		}
	}
	if got := blockMarks(t, tr, width); got != nil {
		t.Errorf("marks on a bodiless block = %+v, want none", got)
	}
	// And the same call at a width its target fits is no target either — one rule, not two.
	if got := blockMarks(t, tr, 200); got != nil {
		t.Errorf("marks at a width the target fits = %+v, want none", got)
	}
}

// ----------------------------------------------------------------------------
// The answered Ask User block: an ordinary body-bearing block (layout.md, "Collapsed and
// expanded blocks")
// ----------------------------------------------------------------------------

// askUserCall folds an ask_user call and, where answer is non-empty, the human's reply — the two
// halves the answered block's record is built from (the question and choices are the CALL's,
// the ticks are the RESULT's). An empty answer leaves the question pending, which is the state the
// popup owns and the block says nothing about.
func askUserCall(tr *transcript, id, args, answer string) {
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "ask_user", Arguments: []byte(args)}})
	if answer != "" {
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: answer}})
	}
}

// TestAnsweredAskUserBlockPaintsTheRecord walks the answered question through both block states:
// the record is a body like any other, so the collapsed block withholds it whole behind the
// remainder count in its slot and the expanded one paints it, with the answer riding the branch throughout. No
// painter rule is new here — that is the claim. Once the presenter hands the block a body, the
// machinery already in place gives the exchange its permanent shape.
func TestAnsweredAskUserBlockPaintsTheRecord(t *testing.T) {
	tr := &transcript{}
	askUserCall(tr, "c1", `{"question":"Which mode?","choices":["Plan","Ask before","Auto"]}`, "Ask before")

	collapsed := strings.Join([]string{
		"✦ Ask User",
		groupMemberLine("  ┕ Which mode? ⋯ Ask before · +4 more lines"),
	}, "\n")
	if got := renderPlain(tr, 80); got != collapsed {
		t.Errorf("collapsed record mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
	}

	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the answered question expanded")
	}
	expanded := strings.Join([]string{
		"✦ Ask User",
		leaderEdgeRow("  ┕ Which mode? ⋯ Ask before", glyphExpanded),
		"    Which mode?",
		"    [ ] Plan",
		"    [x] Ask before",
		"    [ ] Auto",
		seeLessFooterLine(t, 80),
	}, "\n")
	if got := renderPlain(tr, 80); got != expanded {
		t.Errorf("expanded record mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, expanded)
	}

	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false on the way back; want the block collapsed again")
	}
	if got := renderPlain(tr, 80); got != collapsed {
		t.Errorf("re-collapsed record mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, collapsed)
	}
}

// …and because the collapsed paint now hides something, the block becomes a toggle target by the
// one predicate that decides both the affordance and the click (blockHidesWhenCollapsed): every row
// it paints is marked, the leader row wearing the ▶/▼ indicator and, collapsed, the count of the
// record behind it, and expanding takes that count away while the block — the answers it now shows
// included — keeps the click that closes it again. A question still on the screen hides nothing and
// is no target at all.
func TestAnsweredAskUserBlockIsAToggleTarget(t *testing.T) {
	const question = `{"question":"Which mode?","choices":["Plan","Ask before","Auto"]}`

	t.Run("an answered question marks its rows", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", question, "Ask before")

		want := []blockMark{
			{line: 0, kind: targetHeader, entry: 0, text: "✦ Ask User"},
			{line: 1, kind: targetHeader, entry: 0,
				text: groupMemberLine("  ┕ Which mode? ⋯ Ask before · +4 more lines")},
		}
		if got := blockMarks(t, tr, 80); !reflect.DeepEqual(got, want) {
			t.Errorf("collapsed marks mismatch:\n--- got ---\n%+v\n--- want ---\n%+v", got, want)
		}

		if !tr.toggleExpanded(0) {
			t.Fatal("toggleExpanded(0) = false; want the answered question expanded")
		}
		want = []blockMark{
			{line: 0, kind: targetHeader, entry: 0, text: "✦ Ask User"},
			{line: 1, kind: targetHeader, entry: 0, text: leaderEdgeRow("  ┕ Which mode? ⋯ Ask before", glyphExpanded)},
			{line: 2, kind: targetHeader, entry: 0, text: "    Which mode?"},
			{line: 3, kind: targetHeader, entry: 0, text: "    [ ] Plan"},
			{line: 4, kind: targetHeader, entry: 0, text: "    [x] Ask before"},
			{line: 5, kind: targetHeader, entry: 0, text: "    [ ] Auto"},
			{line: 6, kind: targetHeader, entry: 0, text: seeLessFooterLine(t, 80)},
		}
		if got := blockMarks(t, tr, 80); !reflect.DeepEqual(got, want) {
			t.Errorf("expanded marks mismatch:\n--- got ---\n%+v\n--- want ---\n%+v", got, want)
		}
	})

	t.Run("a pending question is no target", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", question, "")

		if got := blockMarks(t, tr, 80); got != nil {
			t.Errorf("pending question marks = %+v, want none — the popup is its live view", got)
		}
	})
}

// The record breaks the grouping a question used to fold into, and now SAYS so: the presenter marks
// an answered record solo (askUserAnswerRecord), so consecutive answered questions each keep a block
// of their own with the room the exchange needs. It used to be kept apart by the body it carries,
// back when grouping admitted bodiless calls only; a Terminal call and its output group now, so the
// exclusion had to become a statement rather than a side effect. Pending questions still group —
// nothing has been answered, so there is no record to stand alone — which is what keeps this a rule
// about records and not a rule about Ask User.
func TestAnsweredAskUserBlocksNeverGroup(t *testing.T) {
	t.Run("answered questions stand alone", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", `{"question":"Ship it?","choices":["Yes","No"]}`, "Yes")
		askUserCall(tr, "c2", `{"question":"Tag it?","choices":["Yes","No"]}`, "No")

		if !tr.entries[0].tool.solo {
			t.Error("the answered record is not marked solo; the split would rest on its body again")
		}

		want := strings.Join([]string{
			"✦ Ask User",
			groupMemberLine("  ┕ Ship it? ⋯ Yes · +3 more lines"),
			"",
			"✦ Ask User",
			groupMemberLine("  ┕ Tag it? ⋯ No · +3 more lines"),
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("answered questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	// The mark is a verdict the presenter reached when the result landed, and decode never re-runs a
	// presenter — so it rides the wire (wireToolView.Solo). Without it a resumed session would fold
	// two records into one group, which is the scrollback changing shape across a restart.
	t.Run("a replayed record still stands alone", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", `{"question":"Ship it?","choices":["Yes","No"]}`, "Yes")
		askUserCall(tr, "c2", `{"question":"Tag it?","choices":["Yes","No"]}`, "No")
		before := renderPlain(tr, 80)

		data, err := encodeTranscript(tr)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		entries, err := decodeTranscript(data)
		if err != nil {
			t.Fatalf("decodeTranscript: %v", err)
		}
		if got := renderPlain(&transcript{entries: entries}, 80); got != before {
			t.Errorf("replayed records mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, before)
		}
	})

	t.Run("pending questions still group", func(t *testing.T) {
		tr := &transcript{}
		askUserCall(tr, "c1", `{"question":"Ship it?","choices":["Yes","No"]}`, "")
		askUserCall(tr, "c2", `{"question":"Tag it?","choices":["Yes","No"]}`, "")

		want := strings.Join([]string{
			"✦ Ask User (2)",
			"  ┝ Ship it? ⋯",
			"  ┕ Tag it? ⋯",
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("pending questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}
