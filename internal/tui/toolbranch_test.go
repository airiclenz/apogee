package tui

import (
	"fmt"
	"image/color"
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
// outcome in the slot — and its type row, which could not sum a run with an open member, with the
// run's total.
func TestRenderGroupWithInFlightMember(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "README.md", 1, 154, 0)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "read_file", Arguments: []byte(`{"path":"TODO.md"}`)}})

	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setTypeExpanded(0, true) = false; want the Read run's type row open")
	}
	want := strings.Join([]string{
		"✦ Tools (2 calls)",
		leaderEdgeRow("  ┕ Read (2) ⋯", glyphExpanded),
		"  │ ┝ README.md ⋯ 154 lines",
		"  │ ┕ TODO.md ⋯",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("in-flight member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2",
		Content: "[File: TODO.md, 408 lines total, showing lines 1-408]\n…",
		Summary: domain.ReadSpan{Start: 1, End: 408, Total: 408}}})
	want = strings.Join([]string{
		"✦ Tools (2 calls)",
		leaderEdgeRow("  ┕ Read (2) ⋯ 562 lines", glyphExpanded),
		"  │ ┝ README.md ⋯ 154 lines",
		"  │ ┕ TODO.md ⋯ 408 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("re-rendered group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A lone call renders in the shape a group does — a label header, target leading the branch, the
// outcome at the row's right edge behind a leader — and counts nothing: the "(N)" is a run's
// arithmetic and a block of one has none to state. A SECOND call folds the pair under the umbrella
// (toolSuperGroup): a type row counting the run stands where the standalone header was, and the two
// member rows lie one level down under it, each joining by adding a line rather than by moving the
// first one's target — there is no column to re-measure, the leader simply absorbs whatever the two
// targets differ by.
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
	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setTypeExpanded(0, true) = false; want the Read run's type row open")
	}
	want = strings.Join([]string{
		"✦ Tools (2 calls)",
		leaderEdgeRow("  ┕ Read (2) ⋯ 163 lines", glyphExpanded),
		"  │ ┝ main.go ⋯ 154 lines",
		"  │ ┕ a-much-longer-name.go ⋯ 9 lines",
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
// slot on the path's leader row and the banded body hangs beneath it. The body keeps its red/green
// bands, which — together with having a body at all — is why it can never fold into a group.
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
		"    1 - a removed line",
		"    1 + an added line",
		seeLessFooterLine(t, 80),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("diff block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// The band is FULL WIDTH and the chrome beside it is CHROME: the tint runs on to the block's
	// wrap rail rather than stopping at the last glyph — so a body of unequal lines reads as one
	// field with a straight right edge and a short line's trailing space says added/removed like the
	// rest of it — while the columns left of the marker stay in the plain detail tone (ratified
	// calls 2 and 3 of docs/plans/"2026-08-19 - 05"; renderHangingRow).
	//
	// Those columns are the four the body hangs under AND the row's own number gutter: the number
	// says WHERE the line is, not that it changed, so it sits outside the band exactly as the split
	// panes' does (splitCell.paint, TestStackedDiffKeepsTheNumberGutterChrome).
	const width = 80
	const hang = "    1 " // the leader row's "  ┕ " the body hangs under, then the row's number gutter
	th := newTheme(scheme.Default())
	lines := tr.renderLines(th, width)
	for _, tc := range []struct {
		row  int
		kind detailKind
		text string
	}{
		{2, detailDiffRemoved, "- a removed line"},
		{3, detailDiffAdded, "+ an added line"},
	} {
		band := detailStyle(th, tc.kind, true)
		want := band.Background(lipgloss.NoColor{}).Render(hang) +
			band.Render(squareLine(th.measure, tc.text, width-th.measure.Width(hang)))
		if got := lines[tc.row]; got != want {
			t.Errorf("line %d = %q; want the band under the open tone beside a chrome hang %q",
				tc.row, got, want)
		}
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
		"    1 - a code line that has been removed",
		"    2 - a second removed line",
		"    1 + a new code line",
		"    2 + a second new line",
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
	// The body is the diff's own regions now, so every row carries the after-file line it sits on,
	// right-aligned into one gutter for the whole body (stackedDiffLines).
	paintedDiff := func(n int) []string {
		gutter := len(strconv.Itoa(n))
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, fmt.Sprintf("    %*d + added", gutter, i+1))
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
			wantCount: "+4 more lines",
			// The rows are numbered: a write's body states the whole AFTER file, so its lines carry
			// their 1..N numbers in the gutter, which widens the paint's lead past the bare indent.
			wantExpanded: []string{"    1 + alpha", "    2 + beta", "    3 + gamma", "    4 + delta"},
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
		if !tr.setTypeExpanded(0, true) {
			t.Fatal("setTypeExpanded(0, true) = false; want the Terminal run's type row open")
		}
		if !tr.setExpanded(1, true) {
			t.Fatal("setExpanded(1, true) = false; want the second member opened")
		}
		rows := tr.renderLines(th, 80)

		// Row 0 is the umbrella's header and row 1 its type row, so the members start at row 2. A
		// member row is not one style run — its ▶/▼ and, open, its gutter are chrome painted beside
		// the text — so the tone is asserted on the text the member is carrying.
		if want := th.toolDetail.Render("go build ./..."); !strings.Contains(rows[2], want) {
			t.Errorf("the closed member = %q; want its row in the dim tone %q", rows[2], want)
		}
		if want := th.toolDetailBright.Render("go vet ./..."); !strings.Contains(rows[3], want) {
			t.Errorf("the open member's first row = %q; want its target in the brighter tone %q", rows[3], want)
		}
		if want := th.toolDetailBright.Render("clean"); !strings.Contains(rows[4], want) {
			t.Errorf("the open member's body = %q; want it in the brighter tone %q", rows[4], want)
		}
		if want := th.toolDetail.Render(memberGutter + glyphMemberGutter + " "); !strings.Contains(rows[4], want) {
			t.Errorf("the open member's body = %q; want the gutters beside it still chrome %q", rows[4], want)
		}
	})
}

// …and the tone step reaches EVERY detail line, a diff line included: what a diff line does not
// take from the state is its BAND, which says which way the line went and is the same collapsed and
// open. The two states are asked of the two painters that draw a targetless branch list — the
// collapsed one under the row budget (clipDetails) and the open one (renderDetails) — over the same
// line, so the comparison is of paint rather than of the style table. Both halves are asserted on
// every kind: the paints must differ (the tone step is there) and each must be exactly what
// detailStyle hands out for that state (the band is under it, and under nothing else).
func TestDiffLinesKeepTheirColourInBothBlockStates(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	cases := []struct {
		name string
		kind detailKind
	}{
		{name: "an added line keeps its turquoise band", kind: detailDiffAdded},
		{name: "a removed line keeps its red band", kind: detailDiffRemoved},
		{name: "a plain line stands on no band", kind: detailPlain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := []detailLine{{Kind: tc.kind, Text: "+ added"}}
			branch := branchMarker(true) // one line, so it is the list's last: the ┕ elbow
			closed, _ := clipDetails(th, line, 40)
			open := renderDetails(th, line, 40)
			if len(closed) != 1 || len(open) != 1 {
				t.Fatalf("the painters spent %d and %d rows on one line; want one each", len(closed), len(open))
			}
			if closed[0] == open[0] {
				t.Errorf("closed and open paint identically (%q); want the open tone a step out of the collapsed dim", closed[0])
			}
			for _, state := range []struct {
				name     string
				row      string
				expanded bool
			}{
				{name: "closed", row: closed[0], expanded: false},
				{name: "open", row: open[0], expanded: true},
			} {
				style := detailStyle(th, tc.kind, state.expanded)
				plain := strip(state.row)
				want := style.Render(plain) // an unbanded kind is painted in one run, branch glyph and all
				if tc.kind != detailPlain {
					// A banded kind splits at the branch glyph: the ┕ stays chrome and the text alone
					// carries the band out to the rail left of it (renderHangingRow, ratified call 3).
					want = style.Background(lipgloss.NoColor{}).Render(branch) +
						style.Render(strings.TrimPrefix(plain, branch))
				}
				if state.row != want {
					t.Errorf("%s paint = %q; want the kind's style for that state %q", state.name, state.row, want)
				}
			}
			if closedBand, openBand := detailStyle(th, tc.kind, false).GetBackground(),
				detailStyle(th, tc.kind, true).GetBackground(); closedBand != openBand {
				t.Errorf("the band moves with the state: closed %v, open %v; want one band in both", closedBand, openBand)
			}
		})
	}
}

// detailStyle is the seam the assertion above reaches through, so it is pinned on its own terms:
// a diff kind hands back the state's PLAIN tone as its foreground — the same style a context line
// gets — with the kind's band added underneath, and nothing else moves between the two states. A
// regression that put the direction back on the glyphs would show up here as a foreground that is
// neither `muted` nor `muted-bright`.
func TestDetailStyleBandsTheDiffKindsUnderTheStateTone(t *testing.T) {
	th := newTheme(scheme.Default())
	cases := []struct {
		name string
		kind detailKind
		band color.Color
	}{
		{name: "added", kind: detailDiffAdded, band: th.diffAdded.GetBackground()},
		{name: "removed", kind: detailDiffRemoved, band: th.diffRemoved.GetBackground()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, expanded := range []bool{false, true} {
				got := detailStyle(th, tc.kind, expanded)
				if want := detailTone(th, expanded).GetForeground(); got.GetForeground() != want {
					t.Errorf("expanded=%v: foreground = %v; want the state's plain tone %v",
						expanded, got.GetForeground(), want)
				}
				if got.GetBackground() != tc.band {
					t.Errorf("expanded=%v: background = %v; want the kind's band %v",
						expanded, got.GetBackground(), tc.band)
				}
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
		"    [✔] Ask before",
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
			{line: 4, kind: targetHeader, entry: 0, text: "    [✔] Ask before"},
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
	// presenter — so it rides the wire (session.ToolView.Solo). Without it a resumed session would fold
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

		if !tr.setTypeExpanded(0, true) {
			t.Fatal("setTypeExpanded(0, true) = false; want the Ask User run's type row open")
		}
		want := strings.Join([]string{
			"✦ Tools (2 calls)",
			leaderEdgeRow("  ┕ Ask User (2) ⋯", glyphExpanded),
			"  │ ┝ Ship it? ⋯",
			"  │ ┕ Tag it? ⋯",
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("pending questions mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// ----------------------------------------------------------------------------
// The two readings of a diff body (plan item 7; ADR 0052)
// ----------------------------------------------------------------------------

// paintRegions is the change every paint-integration test below is fed: one replacement with
// context each side, whose two halves are told apart by their TEXT alone. That is what lets an
// assertion name the reading it is looking at without measuring a single column — the split
// reading puts the removed line and its replacement on ONE row, and the stacked reading never
// does, whatever the numbers, the gutter and the wrap are doing.
func paintRegions() []domain.EditRegion {
	return []domain.EditRegion{{
		BeforeStart: 12, AfterStart: 12,
		Leading:  []string{"func paint() {"},
		Removed:  []string{"  return errNarrow"},
		Inserted: []string{"  return errWide"},
		Trailing: []string{"}"},
	}}
}

// paintsSplit reports which reading a painted body is: true when some row carries BOTH halves of
// the replacement, which only two panes can do.
func paintsSplit(rows []string) bool {
	for _, row := range rows {
		if strings.Contains(row, "errNarrow") && strings.Contains(row, "errWide") {
			return true
		}
	}
	return false
}

// carriesBothHalves reports whether the body shows the whole change at all — the removed line and
// its replacement, on one row or on two. Neither reading may lose a line: the width chooses the
// arrangement and never the content.
func carriesBothHalves(rows []string) bool {
	var removed, inserted bool
	for _, row := range rows {
		removed = removed || strings.Contains(row, "errNarrow")
		inserted = inserted || strings.Contains(row, "errWide")
	}
	return removed && inserted
}

// editWithRegions is an edit call whose result recorded regions, the block every paint path below
// is asked to paint. The block is left COLLAPSED — each test opens it, since the reading is only
// ever a question about an expanded body.
func editWithRegions(regions []domain.EditRegion) *transcript {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"main.go","oldText":"  return errNarrow","newText":"  return errWide"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
		Content: "replaced text in main.go", Summary: domain.EditRegions{Regions: regions}}})
	return tr
}

// openBodyRows is what an EXPANDED single block painted BENEATH its branch row: its body, less the
// see-less footer every open block closes with (seeLessFooter). The header and the branch row are
// the two rows above it, whichever reading filled the body.
func openBodyRows(t *testing.T, tr *transcript, width int) []string {
	t.Helper()
	rows := strings.Split(renderPlain(tr, width), "\n")
	if len(rows) < 4 {
		t.Fatalf("block painted %d rows at width %d, want a header, a branch, a body and a footer:\n%s",
			len(rows), width, strings.Join(rows, "\n"))
	}
	if got, want := rows[len(rows)-1], seeLessFooterLine(t, width); got != want {
		t.Fatalf("last row = %q, want the see-less footer %q", got, want)
	}
	return rows[2 : len(rows)-1]
}

// TestSplitDiffPaintsInAnExpandedBlockWhereItFits is the paint integration for the ungrouped block: one
// body, two readings, and the width alone chooses between them (ADR 0052 §3). A wide terminal puts
// the removed line and its replacement side by side; the same block in eighty columns falls back to
// the stacked rows the same regions already built, and neither loses a line.
func TestSplitDiffPaintsInAnExpandedBlockWhereItFits(t *testing.T) {
	t.Parallel()

	tr := editWithRegions(paintRegions())
	if !tr.setExpanded(0, true) {
		t.Fatal("setup: entries[0] is not a toggleable block")
	}

	wide := openBodyRows(t, tr, 140)
	if !paintsSplit(wide) {
		t.Errorf("at 140 columns the body is not two panes:\n%s", strings.Join(wide, "\n"))
	}
	if !carriesBothHalves(wide) {
		t.Errorf("the split body lost a half of the change:\n%s", strings.Join(wide, "\n"))
	}

	narrow := openBodyRows(t, tr, 80)
	if paintsSplit(narrow) {
		t.Errorf("at 80 columns the body painted two panes; the panes would be under %d columns each:\n%s",
			splitPaneMinCols, strings.Join(narrow, "\n"))
	}
	if !carriesBothHalves(narrow) {
		t.Errorf("the stacked body lost a half of the change:\n%s", strings.Join(narrow, "\n"))
	}
}

// The flip is the COMPOSER's own width rule, asked at the width the body actually gets — the
// block's width less the indent it hangs at. Asserting the two agree across the boundary is what
// keeps the painter's arithmetic from drifting from splitDiffFits: an indent the paint spends but
// the question does not would flip the reading one column early and overrun the block.
func TestSplitDiffPaintFlipsWhereTheComposerSaysItDoes(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	regions := paintRegions()
	indent := th.measure.Width(branchMarker(true))
	tr := editWithRegions(regions)
	if !tr.setExpanded(0, true) {
		t.Fatal("setup: entries[0] is not a toggleable block")
	}

	var flips int
	for width := 80; width <= 130; width++ {
		want := splitDiffFits(regions, width-indent)
		if got := paintsSplit(openBodyRows(t, tr, width)); got != want {
			t.Fatalf("at %d columns the body painted split=%v, want %v — splitDiffFits over the %d "+
				"columns left of the %d-cell indent", width, got, want, width-indent, indent)
		}
		if width > 80 && want != splitDiffFits(regions, width-1-indent) {
			flips++
		}
	}
	if flips != 1 {
		t.Errorf("the reading flipped %d times over 80..130 columns, want exactly one boundary in the "+
			"range — the assertion above would otherwise be vacuous", flips)
	}
}

// A block with NO regions paints exactly what it painted before the split reading existed, at any
// width: the argument-derived body of a result that recorded nothing (ratified call 9). The pin is
// at 140 columns, where a body WITH regions would have painted panes.
func TestSplitDiffLeavesARegionlessBodyUntouched(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"main.go","oldText":"a := 1","newText":"a := 2"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "replaced text in main.go"}})
	if !tr.setExpanded(0, true) {
		t.Fatal("setup: entries[0] is not a toggleable block")
	}

	want := []string{"    - a := 1", "    + a := 2"}
	if got := openBodyRows(t, tr, 140); !reflect.DeepEqual(got, want) {
		t.Errorf("summary-less body at 140 columns:\n--- got ---\n%s\n--- want ---\n%s",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestSplitDiffPaintsInATargetlessBlock is the shape with no leader row: a bare
// git_diff_range names neither base nor head, so its target resolves empty (refRangeTarget) and its
// lines ARE the block's branches. Its body takes the same two readings at the same widths — without
// this arm the very same diff would paint panes with its refs named and stacked rows without them
// — and the branch list's own framing survives the swap: the summary still closes it, since it has
// no leader row to ride.
func TestSplitDiffPaintsInATargetlessBlock(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "git_diff_range", Arguments: []byte(`{}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1",
		Content: "-  return errNarrow\n+  return errWide",
		Summary: domain.EditRegions{Regions: paintRegions()}}})
	if !tr.setExpanded(0, true) {
		t.Fatal("setup: entries[0] is not a toggleable block")
	}

	body := func(width int) []string {
		t.Helper()
		rows := strings.Split(renderPlain(tr, width), "\n")
		if got, want := rows[len(rows)-1], seeLessFooterLine(t, width); got != want {
			t.Fatalf("last row at %d columns = %q, want the see-less footer %q", width, got, want)
		}
		return rows[1 : len(rows)-1]
	}

	wide := body(140)
	if !paintsSplit(wide) {
		t.Errorf("at 140 columns the targetless body is not two panes:\n%s", strings.Join(wide, "\n"))
	}
	if last := wide[len(wide)-1]; !strings.HasPrefix(last, "  "+glyphBranchLast+" ") {
		t.Errorf("the split branch list ends in %q, want the summary still closing it on its own branch",
			last)
	}
	if narrow := body(80); paintsSplit(narrow) {
		t.Errorf("at 80 columns the targetless body painted two panes:\n%s", strings.Join(narrow, "\n"))
	}
}

// TestSplitDiffPaintsAFileHeaderPerSection is the multi-file shape (ratified call 10): a printed
// git diff spans every file the range touched, so each file's regions are painted under a muted row
// naming it. The block's target is the REF RANGE, so that row is the only place the file is named —
// without it a reader would be looking at numbered panes of an unnamed file.
//
// Both readings carry the sections, in git's order, with each file's change under its own header:
// the width chooses the arrangement of a body and never what it says.
func TestSplitDiffPaintsAFileHeaderPerSection(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "git_diff_range",
		Arguments: []byte(`{"base":"main","head":"HEAD"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: strings.Join([]string{
		"diff --git a/alpha.go b/alpha.go",
		"index 1111111..2222222 100644",
		"--- a/alpha.go",
		"+++ b/alpha.go",
		"@@ -10,3 +10,3 @@",
		" one",
		"-  return errNarrow",
		"+  return errWide",
		"diff --git a/beta.go b/beta.go",
		"index 4444444..5555555 100644",
		"--- a/beta.go",
		"+++ b/beta.go",
		"@@ -100,3 +100,3 @@",
		" alpha",
		"-beta",
		"+BETA",
	}, "\n")}})
	if !tr.setExpanded(0, true) {
		t.Fatal("setup: entries[0] is not a toggleable block")
	}

	rowIndex := func(rows []string, want string) int {
		for i, row := range rows {
			if strings.TrimSpace(row) == want {
				return i
			}
		}
		return -1
	}
	for _, width := range []int{140, 80} {
		rows := openBodyRows(t, tr, width)
		if got, want := paintsSplit(rows), width == 140; got != want {
			t.Fatalf("at %d columns the body painted split=%v, want %v:\n%s",
				width, got, want, strings.Join(rows, "\n"))
		}
		alpha, beta := rowIndex(rows, "alpha.go"), rowIndex(rows, "beta.go")
		if alpha < 0 || beta < 0 || alpha > beta {
			t.Fatalf("at %d columns the file headers stand at rows %d and %d, want both painted in "+
				"git's own order:\n%s", width, alpha, beta, strings.Join(rows, "\n"))
		}
		change := -1
		for i, row := range rows {
			if strings.Contains(row, "errWide") {
				change = i
			}
		}
		if change < alpha || change > beta {
			t.Errorf("at %d columns alpha.go's change is on row %d, want it under its own header (%d) "+
				"and above beta.go's (%d):\n%s", width, change, alpha, beta, strings.Join(rows, "\n"))
		}
	}
}

// ----------------------------------------------------------------------------
// The outcome slot's verdict rides the summary (surfaces-that-lie plan, item 12)
// ----------------------------------------------------------------------------

// The red an outcome slot takes is the block's VERDICT about the call ([branchSummary.failed]) and
// never a reading of the words standing in the slot. A terminal call whose whole output came to one
// line has that line promoted into the slot verbatim — the tool's own words, quoted
// (promotedOutput) — so a log line opening "error: …" fills the slot without colouring it and counts
// nothing in a run's failure tally. The same sentence in the PRESENTER's words (summaryOnly) is
// apogee's verdict about the call and does paint red. Telling those two apart by their spelling is
// exactly what F-29 could not do.
func TestOutcomeSlotPaintsTheSummarysOwnVerdict(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}

	promoted := presentToolCall(domain.ToolCall{ID: "c1", Tool: "terminal",
		Arguments: []byte(`{"command":"check"}`)}, "", workspaceRoot{})
	promoted.enrichWithResult(domain.ToolResult{CallID: "c1", Content: "error: not really"}, workspaceRoot{})

	for _, tc := range []struct {
		name       string
		view       toolView
		slot       string
		wantFailed bool
	}{
		{
			name:       "a line the tool printed carries no verdict",
			view:       promoted,
			slot:       "error: not really",
			wantFailed: false,
		},
		{
			name:       "a sentence the block worded is its verdict",
			view:       toolView{Target: "check", Summary: summaryOnly("error: boom").Summary},
			slot:       "error: boom",
			wantFailed: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.view.Summary.Text; got != tc.slot {
				t.Fatalf("outcome slot = %q, want %q", got, tc.slot)
			}
			if got := tc.view.Summary.failed; got != tc.wantFailed {
				t.Errorf("summary verdict = %v, want %v", got, tc.wantFailed)
			}

			row := leaderRow(th, tc.view, branchMarker(true), 60, false, noRemainder)

			if got := strings.Contains(row, th.errorText.Render(tc.slot)); got != tc.wantFailed {
				t.Errorf("slot %q painted in the failure tone = %v, want %v: %q",
					tc.slot, got, tc.wantFailed, row)
			}
			if !tc.wantFailed && !strings.Contains(row, th.toolMarker.Render(tc.slot)) {
				t.Errorf("slot %q does not wear the ordinary marker tone: %q", tc.slot, row)
			}
			want := 0
			if tc.wantFailed {
				want = 1
			}
			if got := failedCalls([]toolView{tc.view}); got != want {
				t.Errorf("failedCalls over %q = %d, want %d", tc.slot, got, want)
			}
		})
	}
}

// A type row's own aggregate is worded from the count it took of its members' verdicts, and it
// carries one of its own: the row reads red because the summary says so, not because a painter
// recognised the house plural on its way past (runAggregate, namedSummary).
func TestRunAggregateCarriesItsFailureVerdict(t *testing.T) {
	run := []toolView{
		{Summary: summaryOnly(errorSummaryPrefix + "no such file").Summary},
		{Summary: typedSummary(pluralStat(5, "line"))},
		{Summary: summaryOnly(deniedSummary).Summary},
	}

	if got := failedCalls(run); got != 2 {
		t.Errorf("failedCalls = %d, want 2", got)
	}
	got := runAggregate(run)
	if got.Text != "2 errors" {
		t.Errorf("aggregate = %q, want %q", got.Text, "2 errors")
	}
	if !got.failed {
		t.Error("the aggregate carries no failure verdict; the type row would paint in the ordinary tone")
	}
}

// ----------------------------------------------------------------------------
// A finished delegation's `done` reads in the success colour (tui-polish plan, item 4)
// ----------------------------------------------------------------------------

// The outcome slot's ONE non-failure verdict: a delegation the engine drove to its own boundary
// says `done`, and that word takes the scheme's `success` role — the very green the done ✓ beside
// the run's name already wears, so a finished run is marked once in one colour rather than twice in
// two. Everything else on the row is unmoved: a run stopped at its step cap did not finish and keeps
// the marker tone, and a failed one is red, since the success verdict may never talk a failure out
// of its red.
func TestDelegationDoneReadsInTheSuccessTone(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}

	// delegation folds ONE sub-agent call and the result the engine wrapped its answer in, so every
	// case here is worded by the presenter's own seam (delegationStat) rather than by a summary
	// spelled in the test.
	delegation := func(t *testing.T, content string, failed bool) toolView {
		t.Helper()
		tv := presentToolCall(domain.ToolCall{ID: "s1", Tool: "sub_agent",
			Arguments: []byte(`{"task":"survey the tests"}`)}, "", workspaceRoot{})
		tv.enrichWithResult(domain.ToolResult{CallID: "s1", Content: content, IsError: failed},
			workspaceRoot{})
		return tv
	}

	for _, tc := range []struct {
		name    string
		content string
		failed  bool
		slot    string
		tone    lipgloss.Style
	}{
		{
			name:    "a run that finished is green",
			content: "the suite is clean\nnothing else to report",
			slot:    "done",
			tone:    th.successMark,
		},
		{
			name: "and stays green with the steering cell beside it",
			content: "the suite is clean\nnothing else to report\n" +
				"(the user sent 2 messages to this sub-agent while it ran)",
			slot: "done · steered by 2 messages",
			tone: th.successMark,
		},
		{
			name: "a run stopped at its step cap did not finish",
			content: "[delegate stopped at its step cap (3 steps); partial result — its last " +
				"visible text follows]\nhalfway there",
			slot: "stopped at its step cap",
			tone: th.toolMarker,
		},
		{
			name:    "a failed run is red, and no success verdict overrides it",
			content: "it fell over",
			failed:  true,
			slot:    "error: it fell over",
			tone:    th.errorText,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := delegation(t, tc.content, tc.failed)

			if got := view.Summary.Text; got != tc.slot {
				t.Fatalf("outcome slot = %q, want %q", got, tc.slot)
			}
			row := leaderRow(th, view, branchMarker(true), 70, false, noRemainder)

			if !strings.Contains(row, tc.tone.Render(tc.slot)) {
				t.Errorf("slot %q is not painted in the expected tone: %q", tc.slot, row)
			}
			// The three tones are distinct, so "painted in the expected one" is only a claim if the
			// other two are excluded: a green that also matched the marker role would pin nothing.
			for _, other := range []lipgloss.Style{th.successMark, th.toolMarker, th.errorText} {
				if other.Render(tc.slot) == tc.tone.Render(tc.slot) {
					continue
				}
				if strings.Contains(row, other.Render(tc.slot)) {
					t.Errorf("slot %q also wears a tone it must not: %q", tc.slot, row)
				}
			}
		})
	}
}

// The success verdict is anchored on the DELEGATION vocabulary and on nothing else. Every other
// plain phrase an outcome slot carries is a tool's reading of its own work — diagnostics' `clean`, a
// command's `PASS`, a process's `exit 0` — and apogee does not paint those green: the green says
// the ENGINE drove a run to its boundary. The match is on the whole phrase too, so a sentence that
// merely contains the word is not the verdict.
func TestSummaryStyleGreensOnlyTheDelegationVerdict(t *testing.T) {
	th := newTheme(scheme.Default())

	for _, tc := range []struct {
		text string
		want bool
	}{
		{delegationDoneVerdict, true},
		{"done · steered by 1 message", true},
		{"done · steered by 3 messages", true},
		{delegationCappedVerdict, false},
		{"stopped at its step cap · steered by 1 message", false},
		{"clean", false},
		{"PASS", false},
		{"exit 0", false},
		{"done deal", false},
		{"1 tool call · done", false},
		{"", false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			got := succeededSummary(tc.text)
			if got != tc.want {
				t.Fatalf("succeededSummary(%q) = %v, want %v", tc.text, got, tc.want)
			}

			style := summaryStyle(th, branchSummary{succeeded: got}, false)
			if green := reflect.DeepEqual(style, th.successMark); green != tc.want {
				t.Errorf("slot %q painted in the success role = %v, want %v", tc.text, green, tc.want)
			}
		})
	}

	// Where both verdicts stand the failure wins: a run the engine faulted is red however its words
	// read, which is what keeps the success verdict from ever talking a failure out of its red.
	both := branchSummary{failed: true, succeeded: true}
	if got := summaryStyle(th, both, false); !reflect.DeepEqual(got, th.errorText) {
		t.Error("a summary carrying both verdicts is not red; failure must win")
	}
}
