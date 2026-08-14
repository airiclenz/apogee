package tui

import (
	"math/rand"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/charmbracelet/x/ansi"
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

// ----------------------------------------------------------------------------
// The click surface: which rendered lines toggle a block (layout.md, "Collapsed and expanded
// blocks")
// ----------------------------------------------------------------------------

// blockMark is one line the painter marked as a block's click surface: where the line sits, what
// it is, whose block a click there toggles, and the text painted on it. The text is carried so a
// failure names the line that moved rather than reporting a bare index mismatch — and so the
// assertion pins the mark to the shape, not merely to a number.
type blockMark struct {
	line  int
	kind  targetKind
	entry int
	text  string
}

// blockMarks renders tr and returns its marked lines in order. It first asserts the map's standing
// invariant — exactly one target per rendered line — which is what makes an index into the lines
// safe to use as an index into the targets on the mouse path (model.go).
func blockMarks(t *testing.T, tr *transcript, width int) []blockMark {
	t.Helper()
	rendered := tr.renderView(newTheme(scheme.Default()), width, false)
	if len(rendered.targets) != len(rendered.lines) {
		t.Fatalf("targets and lines out of lockstep: %d targets for %d lines",
			len(rendered.targets), len(rendered.lines))
	}
	var marks []blockMark
	for i, target := range rendered.targets {
		if target.kind == targetNone {
			continue
		}
		marks = append(marks, blockMark{
			line:  i,
			kind:  target.kind,
			entry: target.entry,
			text:  strings.TrimRight(collapseLeader(ansiPattern.ReplaceAllString(rendered.lines[i], "")), " "),
		})
	}
	return marks
}

// TestRenderMarksTheWholeBlock pins the whole target rule in one table: a single tool
// block that hides something is a click surface WHOLE — every row it paints, its header, its leader
// row and (open) its body, each carrying the index of the entry a click there toggles, and each
// meaning the one thing now that the remainder count rides the leader row rather than a marker line
// of its own (collapsedRemainder). A block that hides nothing
// marks no row at all. Every case asserts the complete set of marks, so a line that quietly became
// clickable, or quietly stopped being, fails here.
//
// It pins the AFFORDANCE against the same rule, because each mark carries its line's text: a marked
// block wears the ▶/▼ state indicator at its leader row's right edge (on its header where the block
// is the targetless shape and paints no leader row) and an unmarked one wears none, so the visible
// hint and the click target cannot drift apart — a block that grew an indicator without becoming
// clickable, or became clickable without growing one, fails here too.
func TestRenderMarksTheWholeBlock(t *testing.T) {
	// run folds a terminal call and its multi-line output — the block with a body, and therefore
	// the block with something to reveal.
	run := func(tr *transcript, id, command, output string, depth int) {
		base := domain.EventBase{Depth: depth}
		tr.apply(domain.ToolCallEvent{EventBase: base,
			Call: domain.ToolCall{ID: id, Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
		tr.apply(domain.ToolResultEvent{EventBase: base,
			Result: domain.ToolResult{CallID: id, Content: output}})
	}
	cases := []struct {
		name  string
		width int
		build func(t *testing.T, tr *transcript)
		want  []blockMark
	}{
		{
			// ❯ run the tests | (spacer) | ✦ Terminal | ┕ go test ./... ⋯ exit 0 · +4 more lines ▶ —
			// the header and the leader row beneath it are one surface, and the count of the body
			// behind it rides that row's outcome slot rather than a line of its own.
			name:  "a hidden body marks the block's rows",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.addUser("run the tests", nil)
				run(tr, "c1", "go test ./...", "ok   a\nok   b\nok   c\nPASS", 0)
			},
			want: []blockMark{
				{line: 2, kind: targetHeader, entry: 1, text: "✦ Terminal"},
				{line: 3, kind: targetHeader, entry: 1,
					text: groupMemberLine("  ┕ go test ./... ⋯ exit 0 · +4 more lines")},
			},
		},
		{
			// The state does not decide the target: an expanded block keeps every row marked — that
			// is the click that collapses it again, wherever in the output the pointer happens to be
			// — and its leader row loses the count, there being nothing left hidden to count. The
			// see-less footer closing the body is marked with the rest: it is the one row that
			// exists ONLY to be clicked (seeLessFooter).
			name:  "an expanded block marks its body too and loses its count",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.addUser("run the tests", nil)
				run(tr, "c1", "go test ./...", "ok   a\nok   b\nok   c\nPASS", 0)
				if !tr.toggleExpanded(1) {
					t.Fatal("toggleExpanded(1) = false; want the tool-call entry expanded")
				}
			},
			want: []blockMark{
				{line: 2, kind: targetHeader, entry: 1, text: "✦ Terminal"},
				{line: 3, kind: targetHeader, entry: 1, text: leaderEdgeRow("  ┕ go test ./... ⋯ exit 0", glyphExpanded)},
				{line: 4, kind: targetHeader, entry: 1, text: "    ok   a"},
				{line: 5, kind: targetHeader, entry: 1, text: "    ok   b"},
				{line: 6, kind: targetHeader, entry: 1, text: "    ok   c"},
				{line: 7, kind: targetHeader, entry: 1, text: "    PASS"},
				{line: 8, kind: targetHeader, entry: 1, text: seeLessFooterLine(t, 80)},
			},
		},
		{
			// The other half of the rule, on the shape that has a body row to offer: a short call
			// with no body hides nothing at this width, so not one of its rows is a click target and
			// a click anywhere on it keeps its selection meaning.
			name:  "a block that hides nothing marks no row at all",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
			},
			want: nil,
		},
		{
			// A group's calls carry no bodies (that is what made them groupable), so the block
			// hides nothing and its header keeps a click's selection meaning.
			name:  "a body-less group is no target at all",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				readCall(tr, "c2", "util.go", 1, 42, 0)
			},
			want: nil,
		},
		{
			// The targetless shape is capped like every other: an unregistered tool's verbatim
			// arguments ARE its branches, and a blob that overflows the cap makes the block a
			// target, exactly as a body would. It counts what it cut nowhere — the count rides an
			// outcome slot and this shape paints none — so its ▶ is what says there is more.
			name:  "a targetless block over the cap marks its rows",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "weird_tool", Arguments: []byte(`{"a":1,"b":2,"c":3}`)}})
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ weird_tool ▶"},
				{line: 1, kind: targetHeader, entry: 0, text: "  ┝ a:"},
				{line: 2, kind: targetHeader, entry: 0, text: "  ┕   1"},
			},
		},
		{
			// The cap is what decides, not the shape: a targetless call whose whole branch list
			// fits hides nothing and keeps a click's selection meaning.
			name:  "a targetless block inside the cap marks nothing",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "weird_tool", Arguments: []byte(`"go"`)}})
			},
			want: nil,
		},
		{
			// Narrow enough that the header wraps: the click lands on the header, not on its first
			// row, so EVERY physical line of it is marked.
			name:  "a wrapped header marks all its physical lines",
			width: 11,
			build: func(t *testing.T, tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "git_commit", Arguments: []byte(`{"message":"a much longer commit subject"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "1\n2\n3"}})
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Git"},
				{line: 1, kind: targetHeader, entry: 0, text: "  Commit"},
				// One leader row whatever the width: at eleven columns the target is cut to a single
				// column and its clip tail, and the leader runs out to the indicator on the floor of
				// one dot (design call 4). The hidden body is counted nowhere either: a row this
				// narrow cannot seat the count without spending the target's own floor on it
				// (affordableSlot).
				{line: 2, kind: targetHeader, entry: 0, text: leaderEdgeRow("  ┕ a"+clipTail+" ⋯", glyphCollapsed)},
			},
		},
		{
			// Two blocks of the same shape: each block's rows name its OWN head entry, which
			// is the whole of what the index is for. The approval note between them is what keeps
			// them two blocks — two same-label calls are one group now, however much body they
			// carry (groupable) — and it makes the second block's index 2, which a mark that
			// counted blocks rather than entries would get wrong.
			name:  "each block's marks carry its own entry index",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				run(tr, "c1", "go build ./...", "a\nb\nc", 0)
				tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow})
				run(tr, "c2", "go vet ./...", "x\ny", 0)
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Terminal"},
				{line: 1, kind: targetHeader, entry: 0,
					text: groupMemberLine("  ┕ go build ./... ⋯ exit 0 · +3 more lines")},
				{line: 5, kind: targetHeader, entry: 2, text: "✦ Terminal"},
				{line: 6, kind: targetHeader, entry: 2,
					text: groupMemberLine("  ┕ go vet ./... ⋯ exit 0 · +2 more lines")},
			},
		},
		{
			// A sub-agent run's head is a target for its SPAN alone. This one is still working, so
			// it has no body to truncate and nothing among its views hides anything — what a click
			// there reveals is the elided run behind it, and only the span rule knows that.
			name:  "a working sub-agent head is a target for its elided span",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Sub-Agent"},
				{line: 1, kind: targetHeader, entry: 0, text: groupMemberLine("  ┕ survey the tests ⋯ 1 tool call")},
			},
		},
		{
			// Expanded, the run's head keeps its mark — that is the click that closes it again —
			// even though its own one-line report hides nothing. The prompt rows opening its span
			// carry that same mark: they are body the head painted, so clicking the delegation's
			// prompt closes the delegation, exactly as clicking a tool block's output closes it.
			// The span itself carries no marks of its own here, the read inside it having nothing
			// to reveal either.
			name:  "an expanded sub-agent head stays clickable",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "survey complete", 0)
				if !tr.setExpanded(0, true) {
					t.Fatal("setExpanded(0, true) = false; want the run's head expanded")
				}
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Sub-Agent"},
				{line: 1, kind: targetHeader, entry: 0,
					text: leaderEdgeRow("┌─┶ survey the tests ✓ ⋯ 1 tool call · survey complete", glyphExpanded)},
				{line: 2, kind: targetHeader, entry: 0, text: "│"},
				{line: 3, kind: targetHeader, entry: 0, text: "│ survey the tests"},
			},
		},
		{
			// A railed sub-agent block is marked exactly like a flat one — the rail prefixes lines
			// and adds none — and nothing stands ahead of it now that no label opens the descent.
			name:  "a nested block keeps its marks behind the rail",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				run(tr, "c1", "go test", "a\nb\nc", 1)
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "│ ✦ Terminal"},
				{line: 1, kind: targetHeader, entry: 0,
					text: leaderEdgeRow("│   ┕ go test ⋯ exit 0 · +3 more lines", glyphCollapsed)},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(t, tr)

			if got := blockMarks(t, tr, tc.width); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("marked lines mismatch:\n--- got ---\n%+v\n--- want ---\n%+v", got, tc.want)
			}
		})
	}
}

// TestHeaderIndicatorFollowsTheBlockState pins the indicator's OTHER half: unlike the click mark,
// which is state-independent by design, the glyph says which way the click will go — ▶ while the
// block is collapsed, ▼ while it is expanded — and it follows the state back and forth on one
// transcript rather than across two fixtures, because that is the claim: nothing about the entry
// changes but the flag the painter reads. The block kinds that reach the indicator by three
// different routes are each here: a hidden body (blockHidesWhenCollapsed), a sub-agent run's elided
// span (blockState.elides) and a Firing wearing the borrowed shape under its own glyph.
//
// The glyph rides the BRANCH ROW, at the right edge past the outcome slot, and the header carries
// the label alone (renderToolBlock) — so each case names the header it keeps and the row the
// indicator lands on is checked beside it.
func TestHeaderIndicatorFollowsTheBlockState(t *testing.T) {
	cases := []struct {
		name                        string
		build                       func() *transcript
		wantHeader                  string
		wantCollapsed, wantExpanded string
	}{
		{
			name: "a hidden body",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok\na\nb"}})
				return tr
			},
			wantHeader:    "✦ Terminal",
			wantCollapsed: glyphCollapsed, wantExpanded: glyphExpanded,
		},
		{
			name: "a sub-agent run's elided span",
			build: func() *transcript {
				tr := &transcript{}
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "survey complete", 0)
				return tr
			},
			wantHeader:    "✦ Sub-Agent",
			wantCollapsed: glyphCollapsed, wantExpanded: glyphExpanded,
		},
		{
			name:          "a Firing under its own glyph",
			build:         func() *transcript { return firingBlock("found 3 stale entries\nremoved them") },
			wantHeader:    "⟳ Schedule",
			wantCollapsed: glyphCollapsed, wantExpanded: glyphExpanded,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := tc.build()

			if got := headerStar(t, tr, false); got != tc.wantHeader {
				t.Errorf("header = %q, want the label alone %q", got, tc.wantHeader)
			}
			if got := branchIndicator(t, tr); got != tc.wantCollapsed {
				t.Errorf("collapsed branch wears %q, want %q", got, tc.wantCollapsed)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the block expanded")
			}
			if got := branchIndicator(t, tr); got != tc.wantExpanded {
				t.Errorf("expanded branch wears %q, want %q", got, tc.wantExpanded)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false on the way back; want the block collapsed again")
			}
			if got := branchIndicator(t, tr); got != tc.wantCollapsed {
				t.Errorf("re-collapsed branch wears %q, want %q", got, tc.wantCollapsed)
			}
		})
	}
}

// The indicator is painted apart from the text it closes: the detail tone, never toolLabel's bold
// gold, so the affordance reads as chrome at the leader row's right edge rather than as the last
// word of that row. The assertion is against the theme's own roles rather than a lipgloss
// byte-golden, and the second guard catches the opposite failure — an indicator styled into the
// header label's own run, which is where the shape before the leader row put it.
func TestHeaderIndicatorIsStyledApartFromTheLabel(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok\na\nb"}})

	lines := tr.renderLines(th, 80)

	if want := th.toolIndicator.Render(glyphCollapsed); !strings.Contains(lines[1], want) {
		t.Errorf("branch row %q does not carry the detail-toned indicator %q", lines[1], want)
	}
	if styledIntoTheLabel := th.toolLabel.Render("Run " + glyphCollapsed); strings.Contains(lines[0], styledIntoTheLabel) {
		t.Errorf("header %q paints the indicator inside the label's own run", lines[0])
	}
}

// The "+N more lines" count RIDES the outcome slot rather than standing on a row of its own, and it
// is painted with that slot in one style — apogee's own marker role, never the body's tone, because
// the count is apogee's reading of the block and not a line the tool wrote (ISSUES.md, 2026-08-11).
// The negative half is the whole point of the fold: a collapsed lone call paints its header and one
// row, so no marker line is left for a body line opening with "+" to be mistaken for.
func TestRemainderCountRidesTheOutcomeSlot(t *testing.T) {
	th := newTheme(scheme.Default())
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nPASS"}})

	rendered := tr.renderView(th, 80, false)
	if len(rendered.lines) != 2 {
		t.Fatalf("the collapsed block paints %d rows, want its header and one leader row:\n%s",
			len(rendered.lines), ansi.Strip(strings.Join(rendered.lines, "\n")))
	}
	const slot = "exit 0 · +3 more lines"
	row := rendered.lines[1]
	if want := th.toolMarker.Render(slot); !strings.Contains(row, want) {
		t.Errorf("leader row = %q; want its slot painted as the marker role's %q", row, want)
	}
	if asABodyLine := th.toolDetail.Render(slot); strings.Contains(row, asABodyLine) {
		t.Errorf("leader row %q paints its slot exactly as a body line, so the two cannot be told apart", row)
	}
	for _, ln := range rendered.lines {
		if plain := strings.TrimSpace(ansi.Strip(ln)); plain == "+3 more lines" {
			t.Errorf("the block still paints the remainder on a row of its own: %q", plain)
		}
	}
}

// TestPromptBlockIsOneClickSurface pins the prompt's half of the target rule (D8): a block with two
// shapes to move between is a click surface WHOLE — every row it paints, the marker row and the
// see-less row among them — and a block with one shape is no target on any row. Each
// case renders a transcript holding that block alone, so "every row of the block" and "every
// rendered line" are the same set and a row that quietly changed its mind fails here.
func TestPromptBlockIsOneClickSurface(t *testing.T) {
	const width = 40
	const huge = "alpha\nbravo\ncharlie\ndelta" // four wrapped rows: one past promptCollapsedRows
	cases := []struct {
		name  string
		build func(t *testing.T, tr *transcript)
		want  targetKind
	}{
		{
			name:  "an over-threshold prompt marks every row it paints",
			build: func(_ *testing.T, tr *transcript) { tr.addUser(huge, nil) },
			want:  targetHeader,
		},
		{
			// State-independent, for the tool block's reason: this is the click that closes it again.
			name: "an expanded prompt keeps its marks, see-less row included",
			build: func(t *testing.T, tr *transcript) {
				tr.addUser(huge, nil)
				if !tr.setExpanded(0, true) {
					t.Fatal("setExpanded(0, true) = false; want the prompt expanded")
				}
			},
			want: targetHeader,
		},
		{
			name:  "an interjection is a click surface by the same rule",
			build: func(_ *testing.T, tr *transcript) { tr.addInterjected(huge, nil) },
			want:  targetHeader,
		},
		{
			name:  "an under-threshold prompt is no target at all",
			build: func(_ *testing.T, tr *transcript) { tr.addUser("alpha\nbravo\ncharlie", nil) },
			want:  targetNone,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(t, tr)

			rendered := tr.renderView(newTheme(scheme.Default()), width, false)
			if len(rendered.targets) != len(rendered.lines) {
				t.Fatalf("targets and lines out of lockstep: %d targets for %d lines",
					len(rendered.targets), len(rendered.lines))
			}
			if len(rendered.lines) == 0 {
				t.Fatal("the block painted nothing at all")
			}
			for i, target := range rendered.targets {
				if target.kind != tc.want {
					t.Errorf("row %d (%q) is marked %v; want %v", i, strip(rendered.lines[i]), target.kind, tc.want)
				}
				if tc.want != targetNone && target.entry != 0 {
					t.Errorf("row %d names entry %d; want the block's own head entry 0", i, target.entry)
				}
			}
		})
	}
}

// TestBlockMarksAgreeWithTheMouseMapping walks the seam the toggle uses: the row a mark is PAINTED
// on is the row the mouse resolves to that mark's content line, and the entry it names is the one
// whose state a click there flips. One accounting, so a click can never toggle a block other than
// the one under the cursor — the map's whole reason for being built by the painter.
//
// A grouped block is the case where "the one under the cursor" stops being the block: its members
// each own a state, so the marks have to name the MEMBER's entry rather than the run's head, and a
// mapping that quietly fell back to the head would open the wrong call.
func TestBlockMarksAgreeWithTheMouseMapping(t *testing.T) {
	// lockstep is the map's standing invariant, asserted before any index into it is used.
	lockstep := func(t *testing.T, m Model) {
		t.Helper()
		if len(m.lineTargets) != len(m.lines) {
			t.Fatalf("stashed targets and lines out of lockstep: %d targets for %d lines",
				len(m.lineTargets), len(m.lines))
		}
	}
	// resolves asserts that the mouse maps the row line is painted on back to line itself.
	resolves := func(t *testing.T, m Model, line int) {
		t.Helper()
		got, _, ok := m.pointTranscriptRow(2, screenRow(t, m, line))
		if !ok {
			t.Fatalf("the mouse maps nothing to the row line %d is painted on", line)
		}
		if got != line {
			t.Errorf("a click on line %d's row resolved to content line %d", line, got)
		}
	}

	t.Run("a single block's rows", func(t *testing.T) {
		m := newTestModel(t)
		m.transcript.reset() // drop the seeded start-up box: the block under test opens at line 0
		m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
		m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
		m.refreshViewport()
		lockstep(t, m)

		marked := 0
		for i, target := range m.lineTargets {
			if target.kind != targetHeader {
				continue
			}
			marked++
			resolves(t, m, i)
			if entry := target.entry; m.transcript.entries[entry].kind != entryToolCall {
				t.Errorf("line %d is marked %v but names entry %d, a %v", i, target.kind, entry,
					m.transcript.entries[entry].kind)
			}
		}
		if marked != 2 {
			t.Fatalf("%d lines marked in the stashed map, want the block's header and its leader row", marked)
		}
	})

	t.Run("a group's member rows name their own calls", func(t *testing.T) {
		m := newTestModel(t)
		m.transcript.reset()
		for i, c := range [][2]string{
			{"go build ./...", "ok\nbuilt"},
			{"go vet ./...", "clean\nno findings"},
			{"go test ./...", "ok\nPASS"},
		} {
			id := "c" + strconv.Itoa(i+1)
			m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "terminal",
				Arguments: []byte(`{"command":"` + c[0] + `"}`)}})
			m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: c[1]}})
		}
		m.refreshViewport()
		lockstep(t, m)

		// One header and one row per member: the marks are the three member rows, in order, each
		// naming its own entry — and the entry the mouse's own lookup lands on is that same one.
		var marked []int
		for i, target := range m.lineTargets {
			if target.kind != targetNone {
				marked = append(marked, i)
			}
		}
		if len(marked) != 3 {
			t.Fatalf("group marked %d rows, want one per member:\n%s", len(marked), strings.Join(m.lines, "\n"))
		}
		for member, line := range marked {
			resolves(t, m, line)
			if got := m.lineTargets[line].entry; got != member {
				t.Errorf("member %d's row (line %d) names entry %d, not its own call", member, line, got)
			}
		}
	})
}

// ----------------------------------------------------------------------------
// The live star: which blocks blink their header glyph (layout.md, "The live star")
// ----------------------------------------------------------------------------

// headerStar renders tr at one blink phase and returns its first rendered line with the styling
// stripped — the block header the star leads. The phase is the renderer's parameter rather than
// anything the transcript holds, so a test names it outright instead of driving a clock.
// branchIndicator is the mark a targeted block's BRANCH ROW wears at its right edge — the ▶/▼ the
// header used to carry — or "" where the row wears none.
func branchIndicator(t *testing.T, tr *transcript) string {
	t.Helper()
	lines := strings.Split(renderPlain(tr, 80), "\n")
	if len(lines) < 2 {
		t.Fatalf("the block painted %d rows; it has no branch row to check", len(lines))
	}
	for _, glyph := range []string{glyphCollapsed, glyphExpanded} {
		if strings.HasSuffix(lines[1], glyph) {
			return glyph
		}
	}
	return ""
}

func headerStar(t *testing.T, tr *transcript, blink bool) string {
	t.Helper()
	lines := tr.renderView(newTheme(scheme.Default()), 80, blink).lines
	if len(lines) == 0 {
		t.Fatal("the transcript rendered nothing at all")
	}
	return strings.TrimRight(ansiPattern.ReplaceAllString(lines[0], ""), " ")
}

// TestLiveBlockHeaderStarBlinks is the rule in one table: a block still holding an open call paints
// ✦ or a bare cell by the frame's blink phase, and a block with everything it was waiting for paints
// ✦ at BOTH phases — the phase alone never moves a settled star. Each case asserts the header at
// both phases, so a block that blinked when it should not have fails here just as loudly as one that
// did not blink when it should. The blinked-out phase keeps the star's column, so its expectation is
// the header led by two leading spaces rather than one glyph short.
func TestLiveBlockHeaderStarBlinks(t *testing.T) {
	openRead := func(tr *transcript, id, path string, depth int) {
		tr.apply(domain.ToolCallEvent{
			EventBase: domain.EventBase{Depth: depth},
			Call:      domain.ToolCall{ID: id, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)},
		})
	}
	openRun := func(tr *transcript, id, command string) {
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID: id, Tool: "terminal", Arguments: []byte(`{"command":"` + command + `"}`)}})
	}
	cases := []struct {
		name             string
		build            func(t *testing.T, tr *transcript)
		settled, flipped string
	}{
		{
			name:    "a call still awaiting its result blinks",
			build:   func(_ *testing.T, tr *transcript) { openRun(tr, "c1", "go test ./...") },
			settled: "✦ Terminal", flipped: "  Terminal",
		},
		{
			name: "a landed result settles the star",
			build: func(_ *testing.T, tr *transcript) {
				openRun(tr, "c1", "go test ./...")
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "PASS"}})
			},
			settled: "✦ Terminal", flipped: "✦ Terminal",
		},
		{
			// The state is none of the star's business: expanding a block shows more of it, it does
			// not make the work behind it land.
			name: "an expanded live block blinks like any other",
			build: func(t *testing.T, tr *transcript) {
				openRun(tr, "c1", "go test ./...")
				if !tr.setExpanded(0, true) {
					t.Fatal("setExpanded(0, true) = false; want the in-flight call expanded")
				}
			},
			settled: "✦ Terminal", flipped: "  Terminal",
		},
		{
			// A group has ONE header for many calls, so its star answers for all of them: a batch
			// whose first read landed and whose second has not is still working.
			name: "a group blinks while any of its calls is open",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				openRead(tr, "c2", "util.go", 0)
			},
			settled: "✦ Read (2)", flipped: "  Read (2)",
		},
		{
			name: "a group whose calls have all landed settles",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				readCall(tr, "c2", "util.go", 1, 42, 0)
			},
			settled: "✦ Read (2)", flipped: "✦ Read (2)",
		},
		{
			// A run is live until its REPORT lands, whatever the span has already finished.
			name: "a sub-agent run blinks while its report is out",
			build: func(_ *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
			},
			settled: "✦ Sub-Agent", flipped: "  Sub-Agent",
		},
		{
			// The mirror case, and the reason the rule asks the span as well as the head: the report
			// landed over a call that never got its result, so work is still standing behind the
			// star — and behind a COLLAPSED run nothing else on screen says so.
			name: "a reported run whose span still holds an open call keeps blinking",
			build: func(_ *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				openRead(tr, "c1", "a.go", 1)
				subAgentReport(tr, "s1", "survey complete", 0)
			},
			settled: "✦ Sub-Agent", flipped: "  Sub-Agent",
		},
		{
			name: "a finished run settles",
			build: func(_ *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "survey complete", 0)
			},
			settled: "✦ Sub-Agent", flipped: "✦ Sub-Agent",
		},
		{
			// The umbrella's star answers for every call under it, the group's rule one level up: the
			// running call is its LAST row by construction of a time-ordered walk (design call 2), and
			// that row is the only thing on screen saying the batch is not done.
			name: "an umbrella blinks while its last run is open",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				openRun(tr, "c2", "go test ./...")
			},
			settled: "✦ Tools (2 calls)", flipped: "  Tools (2 calls)",
		},
		{
			name: "an umbrella whose calls have all landed settles",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				runCall(tr, "c2", "go test ./...", "PASS", 0)
			},
			settled: "✦ Tools (2 calls)", flipped: "✦ Tools (2 calls)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(t, tr)

			if got := headerStar(t, tr, false); got != tc.settled {
				t.Errorf("header at the settled phase = %q, want %q", got, tc.settled)
			}
			if got := headerStar(t, tr, true); got != tc.flipped {
				t.Errorf("header at the flipped phase = %q, want %q", got, tc.flipped)
			}
		})
	}
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

// A command whose output is a single line puts that line where every other one-line outcome goes:
// the outcome slot at the right edge of the command's leader row. Nothing hangs beneath — a one-line
// result is a summary, not a body, and only a command with more to say than one line reshapes into
// the Terminal block above.
func TestRenderOneLineOutputRidesTheBranch(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "abc1234\n"}})

	want := strings.Join([]string{
		"✦ Terminal",
		"  ┕ git rev-parse HEAD ⋯ abc1234",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("one-line Run mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// …and because a one-line result leaves the branch row free of a body, consecutive one-line
// commands still fold into one block, each output standing in the outcome slot at its own row's
// right edge behind a leader of its own — the grouping a body would (correctly) break.
func TestRenderGroupsOneLineOutputCalls(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "abc1234"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"pwd"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "/workspace/repos/apogee"}})

	want := strings.Join([]string{
		"✦ Terminal (2)",
		"  ┝ git rev-parse HEAD ⋯ abc1234",
		"  ┕ pwd ⋯ /workspace/repos/apogee",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("one-line Run group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A call whose result has not landed shows its target and a leader running to the row's edge, the
// outcome slot empty — the same row it will keep once the outcome arrives to fill that slot.
func TestRenderInFlightStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}})

	want := strings.Join([]string{
		"✦ Read",
		"  ┕ main.go ⋯",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("in-flight block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The one shape with no target line: an unregistered tool has nothing to lead a branch with, so
// the header stands alone and its LABELLED arguments — one `name:` line with the value's own lines
// beneath it, the same rendering the approval prompt shows — are themselves the ┝/┕ branches.
// Collapsed, that branch list is capped like any other block's body and the header's ▶ is what
// says there is more behind it — this shape has no outcome slot for a count to ride
// (collapsedRemainder); expanded, every line the model sent is back — the approval
// popup is where a human approves an action, the transcript block is the record (layout.md,
// "Collapsed and expanded blocks").
func TestRenderNoTargetStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "mcp_thing", Arguments: []byte(`{"a":1,"b":2}`)}})

	want := strings.Join([]string{
		"✦ mcp_thing ▶",
		"  ┝ a:",
		"  ┕   1",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("collapsed targetless block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the targetless block to expand")
	}
	want = strings.Join([]string{
		"✦ mcp_thing ▼",
		"  ┝ a:",
		"  ┝   1",
		"  ┝ b:",
		"  ┕   2",
		seeLessFooterLine(t, 80),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("expanded targetless block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A targetless call has no branch line for its summary to ride, so the outcome closes the branch
// list instead of vanishing: an unregistered tool's arguments, then the "error: …" it earned. The
// summary is part of that list, so the collapsed cap counts it like any other branch line.
func TestRenderNoTargetKeepsItsSummary(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "mcp_thing", Arguments: []byte(`{"a":1}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "no such server", IsError: true}})
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the targetless block to expand")
	}

	want := strings.Join([]string{
		"✦ mcp_thing ▼",
		"  ┝ a:",
		"  ┝   1",
		"  ┕ error: no such server",
		seeLessFooterLine(t, 80),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("targetless error block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestTargetlessBlocksCollapseToTheBudget pins the reversal of layout.md's old never-hide rule
// across all three targetless shapes: the unregistered/MCP argument dump, a registered call whose
// target argument never arrived, and a stray result. Each collapses to the house budget under a ▶
// header — two branch rows, whatever it is hiding, which is the whole of the ask — and each expands
// to every line it retained. This shape counts what it withheld nowhere: the count rides an outcome
// slot and a targetless block paints none (collapsedRemainder), so its ▶ carries the news alone. The
// 60-line blob is the case the old rule made 61 permanent rows.
func TestTargetlessBlocksCollapseToTheBudget(t *testing.T) {
	blob := func(lines int) []byte {
		items := make([]string, lines)
		for i := range items {
			items[i] = strconv.Quote("arg" + strconv.Itoa(i))
		}
		return []byte("[" + strings.Join(items, ",") + "]")
	}
	cases := []struct {
		name          string
		build         func(tr *transcript)
		wantCollapsed []string
		wantExpanded  int // physical lines, header and see-less footer included
	}{
		{
			name: "an unregistered tool's 60-line argument blob",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "mcp_search", Arguments: blob(58)}})
			},
			// The blob's own rows hang at argumentValueIndent, unlabelled argument bytes being
			// value lines wherever they surface (prettyJSONDetails) — the branch glyphs are the
			// block's, the two columns behind them are the blob's.
			wantCollapsed: []string{"✦ mcp_search ▶", "  ┝   [", `  ┕     "arg0",`},
			wantExpanded:  62,
		},
		{
			name: "a registered call whose target argument is missing",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "terminal", Arguments: []byte(`{"cmd":"go test"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
					CallID: "c1", Content: "one\ntwo\nthree\nfour"}})
			},
			// The typed stat has nowhere to ride on a targetless block, so it lands as the last
			// branch of the list — one more row for the collapsed cap to cut.
			wantCollapsed: []string{"✦ Terminal ▶", "  ┝ one", "  ┕ two"},
			wantExpanded:  7,
		},
		{
			name: "a stray result that matched no call",
			build: func(tr *transcript) {
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
					CallID: "gone", Content: "one\ntwo\nthree"}})
			},
			wantCollapsed: []string{"✦ result ▶", "  ┝ one", "  ┕ two"},
			wantExpanded:  5,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)

			want := strings.Join(tc.wantCollapsed, "\n")
			if got := renderPlain(tr, 80); got != want {
				t.Errorf("collapsed block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the targetless block to own a block state")
			}
			got := strings.Split(renderPlain(tr, 80), "\n")
			if len(got) != tc.wantExpanded {
				t.Errorf("expanded block is %d lines, want %d:\n%s", len(got), tc.wantExpanded, strings.Join(got, "\n"))
			}
			if !strings.HasSuffix(got[0], " "+glyphExpanded) {
				t.Errorf("expanded header = %q, want it to wear %q", got[0], glyphExpanded)
			}
			for _, ln := range got {
				if strings.Contains(ln, "more line") {
					t.Errorf("an expanded block kept a remainder count: %q", ln)
				}
			}
		})
	}
}

// TestEveryToolShapeCollapsesInsideTheRowBudget is the UNIFORM cap read across every shape that
// wears the tool block, at a width narrow enough that each one's content soft-wraps if nothing stops
// it: a targeted call with a long target and a long body, a targetless argument blob, a stray
// result, a scheduled Firing and a collapsed sub-agent run. Collapsed, none of them may stand taller
// than its header plus collapsedBodyCap content rows — two, since the remainder count rides the
// leader row's outcome slot and no longer spends a row of its own — or wider than the column it was
// painted in. That is the whole of the budget, and the point of it is that a reader can predict a
// block's height without knowing which tool filled it (docs/layout/tool-layout.md).
//
// It asserts the SHAPE rather than the text, which the per-shape tests above pin line by line: what
// would regress here is a path that still soft-wraps unbounded, and that shows as a row count.
func TestEveryToolShapeCollapsesInsideTheRowBudget(t *testing.T) {
	const width = 60
	long := strings.Repeat("go test ./internal/tui/ -run TestSomethingLong ", 9)
	body := "line one is itself long enough to wrap at sixty columns without help\ntwo\nthree\nfour"
	cases := []struct {
		name  string
		build func() *transcript
	}{
		{
			name: "a targeted call with a long target and a long body",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":` + strconv.Quote(long) + `}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: body}})
				return tr
			},
		},
		{
			name: "a targetless call whose one verbatim argument line overflows the row",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "mcp_search", Arguments: []byte(strconv.Quote(long))}})
				return tr
			},
		},
		{
			name: "a targetless argument list past both the cap and the row",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
					ID: "c1", Tool: "mcp_search", Arguments: []byte(
						`{"query":` + strconv.Quote(long) + `,"server":"docs","limit":20}`)}})
				return tr
			},
		},
		{
			name: "a stray result that matched no call",
			build: func() *transcript {
				tr := &transcript{}
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "gone", Content: long + "\n" + body}})
				return tr
			},
		},
		{
			name:  "a scheduled Firing",
			build: func() *transcript { return firingBlock(long + "\n" + body) },
		},
		{
			name: "a collapsed sub-agent run",
			build: func() *transcript {
				tr := &transcript{}
				subAgentCall(tr, "s1", long, 0)
				runCall(tr, "c1", "go build ./...", body, 1)
				subAgentReport(tr, "s1", long+"\n"+body, 0)
				return tr
			},
		},
	}
	th := newTheme(scheme.Default())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := strings.Split(renderPlain(tc.build(), width), "\n")
			if len(lines) > 1+collapsedBodyCap {
				t.Errorf("collapsed block is %d rows, want at most %d:\n%s",
					len(lines), 1+collapsedBodyCap, strings.Join(lines, "\n"))
			}
			for i, ln := range lines {
				if w := th.measure.Width(ln); w > width {
					t.Errorf("row %d measures %d cells, want at most %d: %q", i, w, width, ln)
				}
			}
			// A block that hides this much says so, whatever its shape: the indicator and the click
			// target are one predicate, so a missing ▶ here is an unreachable second state. WHERE it
			// sits follows the shape — at the right edge of a targeted call's leader row, and on the
			// header of the targetless shape, which paints no such row (renderToolBlock).
			worn := false
			for _, ln := range lines {
				worn = worn || strings.HasSuffix(ln, glyphCollapsed)
			}
			if !worn {
				t.Errorf("collapsed block wears no %q:\n%s", glyphCollapsed, strings.Join(lines, "\n"))
			}
		})
	}
}

// A call the presenter does not recognise paints its arguments the way the approval prompt does:
// one `name:` line per argument with the value's own real lines beneath it — no brace envelope
// around the set, no quoted key names, and a multi-line value showing the lines it will actually
// run rather than one `"…\n…"` blob. The labelling changes what a body SAYS and nothing about how
// a block behaves: it still collapses to the house budget behind a remainder marker and still
// gives every retained line back on toggle.
func TestUnregisteredCallLabelsItsArguments(t *testing.T) {
	cases := []struct {
		name          string
		args          string
		wantCollapsed []string
		wantExpanded  []string
	}{
		{
			name: "a multi-key argument object",
			args: `{"query":"collapse","server":"docs","limit":20}`,
			wantCollapsed: []string{
				"✦ mcp_search ▶",
				"  ┝ query:",
				"  ┕   collapse",
			},
			wantExpanded: []string{
				"✦ mcp_search ▼",
				"  ┝ query:",
				"  ┝   collapse",
				"  ┝ server:",
				"  ┝   docs",
				"  ┝ limit:",
				"  ┕   20",
			},
		},
		{
			name: "a multi-line value keeps its own lines",
			args: `{"script":"cd /ws\ngit status\ngit diff"}`,
			wantCollapsed: []string{
				"✦ mcp_search ▶",
				"  ┝ script:",
				"  ┕   cd /ws",
			},
			wantExpanded: []string{
				"✦ mcp_search ▼",
				"  ┝ script:",
				"  ┝   cd /ws",
				"  ┝   git status",
				"  ┕   git diff",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
				ID: "c1", Tool: "mcp_search", Arguments: []byte(tc.args)}})

			collapsed := renderPlain(tr, 80)
			if want := strings.Join(tc.wantCollapsed, "\n"); collapsed != want {
				t.Errorf("collapsed block mismatch:\n--- got ---\n%s\n--- want ---\n%s", collapsed, want)
			}
			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the unregistered call to own a block state")
			}
			expanded := renderPlain(tr, 80)
			// The open block closes with the see-less footer, as every expanded block does
			// (seeLessFooter, render.go).
			wantExpanded := append(append([]string(nil), tc.wantExpanded...), seeLessFooterLine(t, 80))
			if want := strings.Join(wantExpanded, "\n"); expanded != want {
				t.Errorf("expanded block mismatch:\n--- got ---\n%s\n--- want ---\n%s", expanded, want)
			}
			// The JSON envelope is what the labelling replaces, so neither state may carry a
			// brace of its own or a key still wearing its wire quotes.
			for _, state := range []string{collapsed, expanded} {
				for _, banned := range []string{"{", "}", `"query"`, `"server"`, `"limit"`, `"script"`} {
					if strings.Contains(state, banned) {
						t.Errorf("painted block still carries %q:\n%s", banned, state)
					}
				}
			}
		})
	}
}

// Anything between two same-label calls ends the run, and so does a call with no target to lead a
// member's leader row. A BODY is no longer among the breakers — that is the flip this test carries,
// asserted after the table — so the case that used to stand for it stands for what it actually
// breaks on now: the label. Each case pins the whole scrollback, so a break shows as the separate
// blocks it must produce.
func TestRenderGroupBreakers(t *testing.T) {
	cases := []struct {
		name  string
		build func(tr *transcript)
		want  []string
	}{
		{
			// The break shows as three TYPE ROWS rather than three blocks: adjacent runs of different
			// labels fold under one umbrella (renderSuperGroup), and what the breaker buys is that
			// the two reads are two rows of it instead of one "Read (2)".
			name: "a differently-labelled call between two reads",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 0)
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "ok\nPASS\ndone"}})
				readCall(tr, "c3", "b.go", 1, 9, 0)
			},
			want: []string{
				"✦ Tools (3 calls)",
				groupMemberLine("  ┝ Read ⋯ 5 lines"),
				groupMemberLine("  ┝ Terminal ⋯ exit 0"),
				groupMemberLine("  ┕ Read ⋯ 9 lines"),
			},
		},
		{
			name: "an approval note between two reads",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 0)
				tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "read_file"}, Decision: domain.ApprovalAllow})
				readCall(tr, "c2", "b.go", 1, 9, 0)
			},
			want: []string{
				"✦ Read",
				"  ┕ a.go ⋯ 5 lines",
				"",
				"· approval allow: read_file",
				"",
				"✦ Read",
				"  ┕ b.go ⋯ 9 lines",
			},
		},
		{
			name: "a deeper sub-agent call",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 0)
				readCall(tr, "c2", "b.go", 1, 9, 1)
			},
			want: []string{
				"✦ Read",
				"  ┕ a.go ⋯ 5 lines",
				"", // the descent's own spacer joins at depth 0: the rail starts at the block
				"│ ✦ Read",
				"│   ┕ b.go ⋯ 9 lines",
			},
		},
		{
			name: "a call with no target",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", 1, 5, 0)
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "read_file"}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2",
					Content: "[File: ?, 1 lines total, showing lines 1-1]",
					Summary: domain.ReadSpan{Start: 1, End: 1, Total: 1}}})
			},
			want: []string{
				"✦ Read",
				"  ┕ a.go ⋯ 5 lines",
				"",
				"✦ Read",
				"  ┕ 1 line",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &transcript{}
			tc.build(tr)
			if got, want := renderPlain(tr, 80), strings.Join(tc.want, "\n"); got != want {
				t.Errorf("group not broken:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}

	// The flip: a call carrying output used to end a run of its own label and now joins it, giving
	// up nothing but the rows its body would have taken — which are a click away on the member
	// itself (design call 3).
	t.Run("a call with output joins the run", func(t *testing.T) {
		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go build"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "done"}})
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "ok\nPASS\ndone"}})

		if run := toolCallRun(tr.entries, 0); len(run) != 2 {
			t.Fatalf("toolCallRun over the two Terminal calls = %d views, want 2 — a body no longer breaks a run", len(run))
		}
		want := strings.Join([]string{
			"✦ Terminal (2)",
			"  ┝ go build ⋯ done",
			groupMemberLine("  ┕ go test ⋯ exit 0"),
		}, "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("joined group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
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
// inputContentRows sizes the prompt box to what the textarea actually draws
// ----------------------------------------------------------------------------

// TestInputContentRows pins the box-sizing count against the textarea's own wrap, including the
// edge that used to under-count: a logical line whose final wrapped segment exactly fills the
// width takes one extra visual row (the widget reserves a trailing row for the caret past a full
// line). Under-counting it left the box a row short at the wrap boundary, stranding the scroll the
// layout re-seat then could not clamp (ISSUES #2).
//
// The CR cases carry the numbers the widget itself answers, spelled out rather than compared: the
// sanitizer rewrites each '\r' AND each '\n' as one newline before the split, so a bare CR opens a
// row and a CRLF opens two. Reading them as one boundary — the intuitive line ending — would put
// this table one row under the widget for every CRLF, which is the failure the count already had.
func TestInputContentRows(t *testing.T) {
	const w = 10
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{"empty is one row", "", 1},
		{"short line", "abc", 1},
		{"one under the width", strings.Repeat("a", 9), 1},
		{"exact width gains a trailing row", strings.Repeat("a", 10), 2},
		{"one over the width", strings.Repeat("a", 11), 2},
		{"two full widths", strings.Repeat("a", 20), 3},
		{"two full widths plus one", strings.Repeat("a", 21), 3},
		{"trailing newline adds a row", "abc\n", 2},
		{"two logical lines", "abc\ndef", 2},
		{"a bare CR is a row boundary too", "abc\rdef", 2},
		{"trailing CR adds a row", "abc\r", 2},
		{"CRLF is two boundaries, one per rune", "abc\r\ndef", 3},
		{"each full logical line gets its trailing row", strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10), 4},
		{"wide glyphs count by display cells", strings.Repeat("あ", 5), 2}, // 5×2 = 10 cells = exact width
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inputContentRows(c.value, w); got != c.want {
				t.Errorf("inputContentRows(%q, %d) = %d, want %d", c.value, w, got, c.want)
			}
		})
	}
}

// A zero or negative width floors to one column rather than dividing by zero, and still returns at
// least one row.
func TestInputContentRowsZeroWidth(t *testing.T) {
	if got := inputContentRows("ab", 0); got < 1 {
		t.Errorf("inputContentRows with zero width = %d, want >= 1 (width floored to one)", got)
	}
}

// widgetContentRows is the row count a REAL textarea draws for value at width, and the effective
// text width it settled on. It is the oracle inputContentRows mirrors, read straight off the widget
// rather than re-derived: DynamicHeight makes the textarea publish its own totalVisualLines as its
// height (bubbles/v2@v2.1.0/textarea/textarea.go:1666-1692), and MaxHeight is cleared so nothing
// clamps that answer. It is the whole-value counterpart of the per-line LineInfo.Height oracle
// TestWrapRowStartsMirrorsTheWidget pins wrapRowStarts to — the same widget, asked the same
// question the box-sizing path asks.
func widgetContentRows(t *testing.T, value string, width int) (rows, effWidth int) {
	t.Helper()
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0 // uncapped: the height it reports is then its raw visual-line total
	ta.MinHeight = 1
	ta.DynamicHeight = true
	ta.SetWidth(width)
	ta.SetValue(value)
	return ta.Height(), ta.Width()
}

// TestInputContentRowsMirrorsTheWidget pins the box-sizing count to the widget itself, which the
// old Wordwrap+Hardwrap approximation was not: it under-counted "hello world" at width 5 (four
// widget rows, it said three) and "a b  c" at width 3 (three, it said two), and over-counted
// "a-b-c-d" at width 3 (three, it said four). The count now delegates to wrapRowStarts, so the box's
// height and the rows the accent pass paints on come off one ruler.
//
// Tabs are in the table because the count now sanitises each line the way the widget's own sanitizer
// did (sanitizeInputLine, inputaccent.go): the oracle sets the raw value on a real textarea, which
// keeps four spaces per tab, so a mirror still measuring the tab as written would come up short here.
// The runes that sanitizer DROPS — utf8.RuneError and the other control runes — are in the table for
// the mirror image of that reason: the textarea keeps none of them, so a mirror that measured one
// would come up long.
//
// The CR cases are the third face of the same sanitizer: '\r' is neither kept nor dropped but
// rewritten as a newline, one per rune, so the widget opens a row on a bare CR and two on a CRLF.
// The mirror split on '\n' alone until 2026-08-14 and came up a row short for either; asking the
// real widget is what settles that CRLF is two rows here and not the one a line ending suggests.
func TestInputContentRowsMirrorsTheWidget(t *testing.T) {
	cases := []struct {
		name  string
		value string
		width int
	}{
		// The three the follow-up finding named, each a concrete failure of the old count.
		{"word-wrapped prose", "hello world", 5},
		{"a double space between words", "a b  c", 3},
		{"a hyphen run", "a-b-c-d", 3},

		{"empty", "", 10},
		{"short line", "abc", 10},
		{"one under the width", strings.Repeat("a", 9), 10},
		{"exact width", strings.Repeat("a", 10), 10},
		{"one over the width", strings.Repeat("a", 11), 10},
		{"two full widths", strings.Repeat("a", 20), 10},
		{"trailing newline", "abc\n", 10},
		{"two logical lines", "abc\ndef", 10},
		{"two full logical lines", strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10), 10},
		{"a blank line between two", "abc\n\ndef", 10},
		{"a bare CR", "abc\rdef", 10},
		{"a trailing CR", "abc\r", 10},
		{"a leading CR", "\rabc", 10},
		{"a CRLF pair", "abc\r\ndef", 10},
		{"CR between two width-filling lines", strings.Repeat("a", 10) + "\r" + strings.Repeat("b", 10), 10},
		{"a CR inside a wrapped word", "averyvery\rlongwordindeed", 6},
		{"a line of nothing but spaces", "     ", 3},
		{"trailing space at a row boundary", "aaa aaa aaa aaax ", 8},
		{"a word longer than the row", "averyveryverylongwordindeed", 6},
		{"wide glyphs count by display cells", strings.Repeat("あ", 5), 10},
		{"wide runes wrapping mid-word", "日本語のテキスト 絵文字", 7},
		{"an emoji carrying VS16", "warn ⚠️ here", 7},
		{"a VS16 run filling the row", "⚠️⚠️⚠️ end", 6},
		{"VS16 inside a word too wide for the row", "aa⚠️bb⚠️cc", 4},
		{"a leading tab", "\tabc def", 6},
		{"a tab inside a word", "ab\tcd efgh", 6},
		{"a tab at the wrap column", "abcd\tefg", 6},
		{"a line of nothing but tabs", "\t\t", 5},
		{"a tab on the second logical line", "abc\n\tdef ghi", 6},
		{"a replacement character and a control rune", "ab\uFFFDcd\x07ef gh", 5},
		{"a realistic draft", "/grill-me check @internal/tui/model.go and /code-adit", 20},
		{"a multi-line draft", "fix the wrap bug\n\nsee @internal/tui/render.go — the mirror under-counts", 24},
		{"one column", "ab cd", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want, w := widgetContentRows(t, c.value, c.width)
			if got := inputContentRows(c.value, w); got != want {
				t.Errorf("inputContentRows(%q, %d) = %d, the widget draws %d rows", c.value, w, got, want)
			}
		})
	}
}

// The same oracle over a spread of generated prompt-shaped drafts, which is what turns "the three
// named cases agree" into "the mirror is faithful": the old count differed from the widget on
// roughly 41% of inputs like these, so any regression to an approximation fails here loudly rather
// than on one lucky fixture. Deterministic — a fixed seed, so a failure is reproducible.
func TestInputContentRowsMirrorsTheWidgetOnGeneratedDrafts(t *testing.T) {
	// The alphabet is chosen for the boundaries the two mirrors disagreed at: spaces (the widget's
	// word/space grouping), hyphens (a breakpoint to ansi.Wordwrap but not to the widget), a wide
	// rune and a VS16 cluster (grapheme-vs-rune measurement), newlines (logical lines), and tabs
	// (the widget's sanitizer expands each into four spaces before it wraps).
	glyphs := []string{"a", "b", "c", " ", " ", "-", "@", "/", "あ", "⚠️", "\n", "\t"}
	rng := rand.New(rand.NewSource(20260731))
	for _, width := range []int{1, 2, 3, 5, 8, 13, 40} {
		for i := 0; i < 300; i++ {
			var sb strings.Builder
			for n := rng.Intn(24); n > 0; n-- {
				sb.WriteString(glyphs[rng.Intn(len(glyphs))])
			}
			value := sb.String()
			want, w := widgetContentRows(t, value, width)
			if got := inputContentRows(value, w); got != want {
				t.Fatalf("inputContentRows(%q, %d) = %d, the widget draws %d rows", value, w, got, want)
			}
		}
	}
}

// The clamp above the count still holds now that it can report MORE rows than the old
// approximation: the box never grows past maxInputRows (past it the widget scrolls internally) and
// never shrinks below minInputRows, whatever the mirror returns. inputContentRows itself stays
// unclamped — layout() derives the viewport's height from the clamped box height, not from this.
func TestPromptEditorRowsClampsTheWidgetCount(t *testing.T) {
	const width = 8
	cases := []string{
		"",
		"short",
		strings.Repeat("a", width*maxInputRows*2),   // one very long soft-wrapped line
		strings.Repeat("word ", width*maxInputRows), // many wrapped words
		strings.Repeat("line\n", maxInputRows*3),    // many logical lines
		strings.Repeat("⚠️", width*maxInputRows),    // VS16 clusters, the widest measure gap
		strings.Repeat("a", width) + "\n" + "b",     // an exact width fill plus a line
	}
	for _, value := range cases {
		e := newPromptEditor(defaultCursorShape, lipgloss.Color(scheme.Default().Surface))
		e.input.SetValue(value)
		raw := inputContentRows(e.input.Value(), width) // what the editor holds, not what was handed to it
		got := e.rows(width)
		if got < minInputRows || got > maxInputRows {
			t.Fatalf("rows(%q) = %d, outside [%d, %d] (unclamped count %d)", value, got, minInputRows, maxInputRows, raw)
		}
		if want := clampInt(raw, minInputRows, maxInputRows); got != want {
			t.Errorf("rows(%q) = %d, want %d (clamp of the unclamped count %d)", value, got, want, raw)
		}
	}
}

// ----------------------------------------------------------------------------
// The one-time start-up box (version-command-and-startup-box plan, item 3)
// ----------------------------------------------------------------------------

// lineWithLogoAnd reports whether any rendered (ANSI-stripped) line carries both a distinctive
// logo fragment and the given substring — i.e. whether the logo and that text share a physical row.
// It is the mechanical side-by-side / stacked discriminator the two start-up-box tests pivot on.
func lineWithLogoAnd(lines []string, sub string) bool {
	t := false
	for _, ln := range lines {
		p := ansi.Strip(ln)
		if strings.Contains(p, "▗▄▄▖▗▄▄") && strings.Contains(p, sub) {
			t = true
		}
	}
	return t
}

// When there is horizontal room the start-up box uses the WIDE layout: the logo on the left and a
// right-aligned host / model / context / version block on the right, inside a rounded card that
// reuses the prompt box's border glyphs but drops its black fill. The assertions are the layout's
// acceptance made mechanical: (a) the logo art is present, (b) all FOUR session facts (incl. the new
// context) with their dim labels are present, (c) the logo and the info block share physical rows
// (side by side — the wide layout, not stacked), (d) the widest info row is flush against the right
// padding (the block is right-aligned), (e) the rounded corners match the prompt box, (f) the card
// carries none of the black-background SGR the input box emits, and (g) every line spans the full
// content width, top and bottom closing on the rounded corner at that edge.
func TestRenderStartupBox(t *testing.T) {
	th := newTheme(scheme.Default())
	v := startupView{
		Logo:    strings.TrimRight(apogeeLogo, "\n"),
		Host:    "test-host:1111", // the widest value → its row is the one flushed right
		Model:   "gpt-oss-20b",
		Context: "32k",
		Version: "v9.9.9-test",
	}
	const width = 80 // ample room: inner 76 ≥ logo 36 + gap 4 + info 23, so the wide layout engages
	lines := renderStartupBox(th, v, width)
	raw := strings.Join(lines, "\n")
	plain := ansi.Strip(raw)

	// (a) a distinctive fragment of the block-art wordmark survives into the card.
	if !strings.Contains(plain, "▗▄▄▖▗▄▄") {
		t.Errorf("startup box does not carry the logo art:\n%s", plain)
	}
	// (b) all four session facts, each with its dim label, are present.
	for _, want := range []string{"host", v.Host, "model", v.Model, "context", v.Context, "version", v.Version} {
		if !strings.Contains(plain, want) {
			t.Errorf("startup box missing %q:\n%s", want, plain)
		}
	}
	// (c) the logo and the info block sit on the same rows — the wide (side-by-side) layout, not the
	// stacked fallback where the logo lines stand alone above the facts.
	if !lineWithLogoAnd(lines, "host") {
		t.Errorf("wide startup box does not place the info block beside the logo:\n%s", plain)
	}
	// (d) the info block is right-aligned: the widest row (host) ends flush against the right padding,
	// so its line closes on the value then the one-column padding and the border.
	if want := v.Host + " │"; !strings.Contains(plain, want) {
		t.Errorf("wide startup box is not right-aligned — no line ends %q (flush to the right padding):\n%s", want, plain)
	}
	// (e) the rounded corners match the prompt box's RoundedBorder glyphs.
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(plain, corner) {
			t.Errorf("startup box missing rounded corner %q:\n%s", corner, plain)
		}
	}
	// (f) none of the surface-background SGR the input box paints. Extract it from a real
	// Background(surface) render, so the check tracks whatever colour profile lipgloss uses rather
	// than a hard-coded escape.
	probe := lipgloss.NewStyle().Background(lipgloss.Color(scheme.Default().Surface)).Render("x")
	if !strings.Contains(probe, "\x1b") {
		t.Fatal("the black-background probe rendered no escape; the colour profile hides the SGR this test relies on")
	}
	blackBG := probe[:strings.IndexByte(probe, 'm')+1] // the leading SGR, up to and including its 'm'
	if strings.Contains(raw, blackBG) {
		t.Errorf("startup box carries the input box's black-background SGR %q — it must be transparent", blackBG)
	}
	// (g) every rendered line — border runes included — is exactly the content width it was handed,
	// so the right border aligns to the same column the rest of the transcript's content ends at.
	// The top and bottom rows close on the rounded corner at that edge.
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("startup box line %d is %d cols, want the full content width %d: %q", i, w, width, ansi.Strip(ln))
		}
	}
	if top := ansi.Strip(lines[0]); !strings.HasSuffix(top, "╮") {
		t.Errorf("top row does not close on ╮ at the content edge: %q", top)
	}
	if bot := ansi.Strip(lines[len(lines)-1]); !strings.HasSuffix(bot, "╯") {
		t.Errorf("bottom row does not close on ╯ at the content edge: %q", bot)
	}
}

// When the width cannot fit the logo, a gap, and the info block side by side, the start-up box falls
// back to the STACKED layout — the card's original shape: the logo, a blank line, then host / model /
// version below it, and (by owner decision) NO context row. The assertions: (a) the three fallback
// facts are present, (b) the context fact is absent (it lives only in the wide layout), (c) the logo
// and the facts are on SEPARATE rows (stacked, not side by side), (d) the card still spans the full
// content width with rounded corners.
func TestRenderStartupBoxStackedFallback(t *testing.T) {
	th := newTheme(scheme.Default())
	v := startupView{
		Logo:    strings.TrimRight(apogeeLogo, "\n"),
		Host:    "test-host:1111",
		Model:   "gpt-oss-20b",
		Context: "32k",
		Version: "v9.9.9-test",
	}
	const width = 50 // inner 46 < logo 36 + gap 4 + info 23 → the wide layout does not fit, so stacked
	lines := renderStartupBox(th, v, width)
	plain := ansi.Strip(strings.Join(lines, "\n"))

	// (a) the three stacked facts, each with its dim label, are present.
	for _, want := range []string{"host", v.Host, "model", v.Model, "version", v.Version} {
		if !strings.Contains(plain, want) {
			t.Errorf("stacked startup box missing %q:\n%s", want, plain)
		}
	}
	// (b) the context row does not appear in the fallback (context is wide-layout-only).
	for _, absent := range []string{"context", v.Context} {
		if strings.Contains(plain, absent) {
			t.Errorf("stacked startup box carries %q — context belongs only to the wide layout:\n%s", absent, plain)
		}
	}
	// (c) the logo and the facts are stacked, not side by side: no line carries both the logo and a
	// fact label.
	if lineWithLogoAnd(lines, "host") {
		t.Errorf("stacked startup box put a fact beside the logo — expected the stacked layout:\n%s", plain)
	}
	// (d) full-width card with rounded corners.
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != width {
			t.Errorf("stacked startup box line %d is %d cols, want the full content width %d: %q", i, w, width, ansi.Strip(ln))
		}
	}
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(plain, corner) {
			t.Errorf("stacked startup box missing rounded corner %q:\n%s", corner, plain)
		}
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
	want := renderEntryLines(th, entry{kind: entryAssistant, text: trimTrailingBlankLines(text)}, 80, false).lines
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
	want := renderEntryLines(th, entry{kind: entryAssistant}, 80, false).lines
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
