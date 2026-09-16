// blocktarget_test.go is named for its subject, not for a source file: a deliberate exception to
// the coding-standards Go rule that a suite is named `{source}_test.go` (ratified 2026-08-15). Its
// subject is cross-file behaviour — which rendered line is a block's click target, and what the
// mark on it says — decided together by render.go, blockstate.go, transcript.go, mouse.go and
// toolbranch.go, so no single source can lend the suite its name.

package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/tasklist"
	"github.com/charmbracelet/x/ansi"
)

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
	rendered := tr.renderView(newTheme(scheme.Default()), width, false, breadcrumbHint)
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
	t.Parallel()

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
			// A run's calls carry no bodies (that is what made them groupable), so nothing under the
			// umbrella hides a body — but the TYPE ROW hides the run's member rows, and that is what
			// a click there folds open. The umbrella's own header stays inert while nothing is open,
			// so it keeps a click's selection meaning.
			name:  "a body-less run marks its type row and nothing else",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				readCall(tr, "c2", "util.go", 1, 42, 0)
			},
			want: []blockMark{{line: 1, kind: targetType, entry: 0,
				text: groupMemberLine("  ┕ Read (2) ⋯ 196 lines")}},
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
				tr.apply(domain.ApprovalEvent{Phase: domain.ApprovalDecided, Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow})
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
			// A FINISHED run is the same two marked rows, and no more: it has one shape in the
			// conversation and expanding it opens its view instead of a rail (ADR 0063), so the
			// click surface cannot grow prompt rows or a span of its own. The mark is what makes
			// the row reach that view at all.
			name:  "a finished sub-agent run is two marked rows and no rail",
			width: 80,
			build: func(t *testing.T, tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				readCall(tr, "c1", "a.go", 1, 5, 1)
				subAgentReport(tr, "s1", "survey complete", 0)
				if tr.setExpanded(0, true) {
					t.Fatal("setExpanded(0, true) = true; a run opens as a view, never as a rail")
				}
			},
			want: []blockMark{
				{line: 0, kind: targetHeader, entry: 0, text: "✦ Sub-Agent"},
				{line: 1, kind: targetHeader, entry: 0,
					text: groupMemberLine("  ┕ survey the tests ✓ ⋯ 1 tool call · survey complete")},
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
			t.Parallel()

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
// span (blockState.elides) and a Firing wearing the borrowed shape under its own glyph. The run is
// the one that never flips: what its ▶ opens is the run's VIEW and not a body of its own (ADR
// 0063), so the flag is refused and the glyph goes on pointing the one way it can.
//
// The glyph rides the BRANCH ROW, at the right edge past the outcome slot, and the header carries
// the label alone (renderToolBlock) — so each case names the header it keeps and the row the
// indicator lands on is checked beside it.
func TestHeaderIndicatorFollowsTheBlockState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                        string
		build                       func() *transcript
		wantHeader                  string
		wantCollapsed, wantExpanded string
		refusesToggle               bool // the block opens a run view instead of a body (ADR 0063)
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
			wantCollapsed: glyphCollapsed, wantExpanded: glyphCollapsed,
			refusesToggle: true,
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
			t.Parallel()

			tr := tc.build()

			if got := headerStar(t, tr, false); got != tc.wantHeader {
				t.Errorf("header = %q, want the label alone %q", got, tc.wantHeader)
			}
			if got := branchIndicator(t, tr); got != tc.wantCollapsed {
				t.Errorf("collapsed branch wears %q, want %q", got, tc.wantCollapsed)
			}
			if got := tr.toggleExpanded(0); got == tc.refusesToggle {
				t.Fatalf("toggleExpanded(0) = %v; want %v", got, !tc.refusesToggle)
			}
			if got := branchIndicator(t, tr); got != tc.wantExpanded {
				t.Errorf("expanded branch wears %q, want %q", got, tc.wantExpanded)
			}
			if tc.refusesToggle {
				return // there is no way back from a state the block never took
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
	t.Parallel()

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
// the count is apogee's reading of the block and not a line the tool wrote (the issue register,
// 2026-08-11). The negative half is the whole point of the fold: a collapsed lone call paints its
// header and one row, so no marker line is left for a body line opening with "+" to be mistaken
// for.
func TestRemainderCountRidesTheOutcomeSlot(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "ok   a\nok   b\nPASS"}})

	rendered := tr.renderView(th, 80, false, breadcrumbHint)
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
	t.Parallel()

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
			t.Parallel()

			tr := &transcript{}
			tc.build(t, tr)

			rendered := tr.renderView(newTheme(scheme.Default()), width, false, breadcrumbHint)
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
	t.Parallel()

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
		t.Parallel()

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
		t.Parallel()

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
		// The run folds under the umbrella, so its member rows are painted only under an OPEN type
		// row (toolblock.go).
		if !m.transcript.setTypeExpanded(0, true) {
			t.Fatal("setTypeExpanded(0, true) = false; want the Terminal run's type row open")
		}
		m.refreshViewport()
		lockstep(t, m)

		// One row per member under the opened type row, in order, each naming its own entry — and
		// the entry the mouse's own lookup lands on is that same one. The umbrella's header and its
		// type row carry marks of their own kinds and are not member rows.
		var marked []int
		for i, target := range m.lineTargets {
			if target.kind == targetHeader {
				marked = append(marked, i)
			}
		}
		if len(marked) != 3 {
			t.Fatalf("group marked %d member rows, want one per member:\n%s", len(marked), strings.Join(m.lines, "\n"))
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
	lines := tr.renderView(newTheme(scheme.Default()), 80, blink, breadcrumbHint).lines
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
	t.Parallel()

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
			// A same-type run of 2 is an umbrella of one type row, and the umbrella has ONE header for
			// every call under it, so its star answers for all of them: a batch whose first read
			// landed and whose second has not is still working.
			name: "a same-type run blinks while any of its calls is open",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				openRead(tr, "c2", "util.go", 0)
			},
			settled: "✦ Tools (2 calls)", flipped: "  Tools (2 calls)",
		},
		{
			name: "a same-type run whose calls have all landed settles",
			build: func(_ *testing.T, tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				readCall(tr, "c2", "util.go", 1, 42, 0)
			},
			settled: "✦ Tools (2 calls)", flipped: "✦ Tools (2 calls)",
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
			t.Parallel()

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

// TestTaskListBlockCollapsesToHeader pins the header-folding block's half of the target rule (the
// ratified call of plan "2026-09-14 - 00", item 2): a task_list block with rows folds to its header
// alone — one row, `✦ Task List (1/3) ▶`, no task row and no `+N more lines`, the header's own
// count saying what the fold holds — and opens onto every row uncapped under a `▼` header with NO
// see-less footer, the header's glyph and a click anywhere on the block being its fold. It holds at
// EVERY width, which is the part the width arm of blockHidesWhenCollapsed would otherwise muddle:
// at 40 columns every row of the long-task list overflows the width, and the block is a toggle for
// the rows it hides rather than for the width — open, each row wraps whole under its marker, as the
// expanded targetless shape always has (renderBranchList). The block is a toggle target, and both
// gestures flip it: a click on the header and ⏎ at the block cursor.
func TestTaskListBlockCollapsesToHeader(t *testing.T) {
	t.Parallel()

	longTasks := fmt.Sprintf(tasklist.HeaderFormat, 2, 1) + "\n" +
		"[✔] read the whole plan document and every ADR it names before touching a file\n" +
		"[ ] write the code the item asks for, the tests beside it, and the docs it names\n" +
		"[ ] run the item's acceptance command and the full suite before writing the sidecar"

	cases := []struct {
		name     string
		width    int
		rendered string
		rows     int // task rows the open paint must carry, whatever the width
	}{
		{name: "at 80 columns every row fits", width: 80, rendered: taskListRendered, rows: 3},
		{name: "at 40 columns every row is clipped", width: 40, rendered: longTasks, rows: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr := &transcript{}
			tr.apply(domain.ToolCallEvent{Call: taskListCall})
			tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: tc.rendered}})

			collapsed := strings.Split(renderPlain(tr, tc.width), "\n")
			if want := []string{"✦ Task List (1/3) " + glyphCollapsed}; !reflect.DeepEqual(collapsed, want) {
				t.Errorf("the collapsed block paints:\n%s\nwant exactly its header:\n%s",
					strings.Join(collapsed, "\n"), strings.Join(want, "\n"))
			}
			if marks := blockMarks(t, tr, tc.width); len(marks) != 1 || marks[0].kind != targetHeader {
				t.Errorf("marks on the collapsed block = %+v, want its one header row a toggle target", marks)
			}

			if !tr.toggleExpanded(0) {
				t.Fatal("toggleExpanded(0) = false; want the task_list block to open")
			}
			open := strings.Split(renderPlain(tr, tc.width), "\n")
			if want := "✦ Task List (1/3) " + glyphExpanded; open[0] != want {
				t.Errorf("open header = %q, want %q", open[0], want)
			}
			rows := 0
			for i, ln := range open[1:] {
				switch {
				case strings.Contains(ln, taskListOpenMarker), strings.Contains(ln, taskListDoneMarker):
					rows++
				case strings.HasPrefix(ln, "    "):
					// a task row's own wrapped continuation, under its marker
				default:
					t.Errorf("open row %d = %q, want a task row or its continuation — the open card is header plus rows only", i+1, ln)
				}
			}
			if rows != tc.rows {
				t.Errorf("the open block carries %d task rows, want every one of the %d:\n%s", rows, tc.rows, strings.Join(open, "\n"))
			}
			if tc.width == 80 && len(open) != 1+tc.rows {
				t.Errorf("the open block stands %d rows tall at 80 columns, want %d — the header and every task row, no footer:\n%s",
					len(open), 1+tc.rows, strings.Join(open, "\n"))
			}
			if strings.Contains(strings.Join(open, "\n"), promptSeeLess) {
				t.Errorf("the open block:\n%s\nwears a %q footer; the header's ▼ is its fold", strings.Join(open, "\n"), promptSeeLess)
			}
			if marks := blockMarks(t, tr, tc.width); len(marks) != len(open) {
				t.Errorf("marks on the open block = %+v, want every one of its %d rows a toggle target", marks, len(open))
			}
		})
	}

	t.Run("a click on the header and ⏎ at the block cursor both flip it", func(t *testing.T) {
		t.Parallel()

		m := modelWithTaskListBlock(t, testOpts)
		header := markedLine(t, m, targetHeader)
		if !blockExpanded(t, m, header) {
			t.Fatal("setup: the block is folded before any gesture; a card built from the default Options starts open (item 4)")
		}
		m = clickCell(t, m, 2, screenRow(t, m, header))
		if blockExpanded(t, m, header) {
			t.Fatal("a click on the header did not fold the task list")
		}
		m = clickCell(t, m, 2, screenRow(t, m, header))
		if !blockExpanded(t, m, header) {
			t.Fatal("a second click on the header did not open the task list")
		}

		m = step(t, m, keyAltUp())
		if !strings.Contains(strip(m.lines[m.blockCursorRow()]), "✦ Task List") {
			t.Fatalf("the block cursor entered on %q, not the task list's header", strip(m.lines[m.blockCursorRow()]))
		}
		m = step(t, m, keyEnter())
		if blockExpanded(t, m, header) {
			t.Fatal("⏎ at the block cursor did not fold the task list")
		}
		m = step(t, m, keyEnter())
		if !blockExpanded(t, m, header) {
			t.Fatal("a second ⏎ did not open the task list")
		}
	})
}

// modelWithTaskListBlock is a ready idle model built from opts whose transcript holds the three-task
// fixture's card, laid out so the click and cursor gestures above land on painted rows.
func modelWithTaskListBlock(t *testing.T, opts Options) Model {
	t.Helper()
	m := newTestModelEng(t, &fakeEngine{}, opts) // 80x24
	m.transcript.reset()
	m.transcript.addUser("plan the work", nil)
	addTaskListCard(&m, "1")
	m.refreshViewport()
	return m
}

// addTaskListCard folds one answered task_list call — the three-task fixture under the given call
// id — into m's transcript through the same event path the engine drives (foldEvent), so the card
// is seeded exactly as a live one is.
func addTaskListCard(m *Model, id string) {
	call := taskListCall
	call.ID = id
	*m = m.foldEvent(domain.ToolCallEvent{Call: call})
	*m = m.foldEvent(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: taskListRendered}})
}

// taskListEntries is the index of every task-list card in m's transcript, in order.
func taskListEntries(m Model) []int {
	var at []int
	for i := range m.transcript.entries {
		if m.transcript.entries[i].tool.collapsesToHeader {
			at = append(at, i)
		}
	}
	return at
}

// headerLineOf is the painted line the card at entry hangs its header on — the first line the
// painter marked as that entry's toggle target.
func headerLineOf(t *testing.T, m Model, entry int) int {
	t.Helper()
	for i, target := range m.lineTargets {
		if target.kind == targetHeader && target.entry == entry {
			return i
		}
	}
	t.Fatalf("no rendered line is marked as entry %d's header", entry)
	return -1
}

// taskListHeaders is every `✦ Task List` header on the frame m last painted, glyph included, so a
// test can say which state EVERY card is painted in from the frame alone — the cached paint path
// (paintcache.go), which is what a click has to move.
func taskListHeaders(m Model) []string {
	var headers []string
	for _, ln := range m.lines {
		if plain := strings.TrimSpace(strip(ln)); strings.HasPrefix(plain, "✦ Task List") {
			headers = append(headers, plain)
		}
	}
	return headers
}

// TestTaskListCardsShareOneFold pins the shared fold (item 4 of plan "2026-09-14 - 00"): every
// task-list card in the transcript is one preference, seeded from Options.TaskListFolded and
// flipped on EVERY card by a gesture on any one of them — a click on the second folds both, ⏎ at
// the block cursor on the first opens both — and the flip lands on the very next frame through the
// cached paint path, because the fact lives on each entry's expanded flag and the paint keys on it.
// The flip is written back through the settings seam as `ui.task-list-open`, once per toggle and
// with NO note in the transcript on success; a card added after the flip is seeded from the flipped
// preference; and a nil seam flips with no write at all.
func TestTaskListCardsShareOneFold(t *testing.T) {
	t.Parallel()

	t.Run("a gesture on any card flips them all and writes the key back", func(t *testing.T) {
		t.Parallel()

		log := &settingsWriteLog{}
		opts := testOpts
		opts.Settings = fakeSettingsHost{write: log.write}
		m := modelWithTaskListBlock(t, opts)
		addTaskListCard(&m, "2")
		m.refreshViewport()
		cards := taskListEntries(m)
		if len(cards) != 2 {
			t.Fatalf("setup: %d task-list cards, want 2", len(cards))
		}
		for _, at := range cards {
			if !m.transcript.entries[at].expanded {
				t.Fatalf("setup: the card at entry %d starts folded; the default preference seeds it open", at)
			}
		}
		if got, want := taskListHeaders(m), []string{"✦ Task List (1/3) " + glyphExpanded, "✦ Task List (1/3) " + glyphExpanded}; !reflect.DeepEqual(got, want) {
			t.Fatalf("setup: headers = %q, want both open %q", got, want)
		}
		entries := len(m.transcript.entries)

		second := headerLineOf(t, m, cards[1])
		m = clickCell(t, m, 2, screenRow(t, m, second))

		for _, at := range cards {
			if m.transcript.entries[at].expanded {
				t.Errorf("after the click on the second card, the card at entry %d is still open; the fold is shared", at)
			}
		}
		if got, want := taskListHeaders(m), []string{"✦ Task List (1/3) " + glyphCollapsed, "✦ Task List (1/3) " + glyphCollapsed}; !reflect.DeepEqual(got, want) {
			t.Errorf("the next frame paints headers %q, want both folded %q — the cached paint did not move", got, want)
		}
		if !m.opts.TaskListFolded {
			t.Error("opts.TaskListFolded is still false after the fold")
		}
		if want := []settingEdit{{path: "ui.task-list-open", value: "false"}}; !reflect.DeepEqual(log.writes, want) {
			t.Errorf("writes = %+v, want exactly %+v", log.writes, want)
		}
		if got := len(m.transcript.entries); got != entries {
			t.Errorf("the transcript grew from %d to %d entries on a landed write; a fold says nothing on success", entries, got)
		}

		m = step(t, m, keyAltUp())
		for !strings.Contains(strip(m.lines[m.blockCursorRow()]), "✦ Task List") ||
			m.lineTargets[m.cursor.line].entry != cards[0] {
			if m.cursor.line == 0 {
				t.Fatal("the block cursor never reached the first task-list card")
			}
			m = step(t, m, keyAltUp())
		}
		m = step(t, m, keyEnter())

		for _, at := range cards {
			if !m.transcript.entries[at].expanded {
				t.Errorf("after ⏎ on the first card, the card at entry %d is still folded; the fold is shared", at)
			}
		}
		if got, want := taskListHeaders(m), []string{"✦ Task List (1/3) " + glyphExpanded, "✦ Task List (1/3) " + glyphExpanded}; !reflect.DeepEqual(got, want) {
			t.Errorf("the next frame paints headers %q, want both open %q", got, want)
		}
		if want := []settingEdit{{path: "ui.task-list-open", value: "false"}, {path: "ui.task-list-open", value: "true"}}; !reflect.DeepEqual(log.writes, want) {
			t.Errorf("writes = %+v, want exactly %+v", log.writes, want)
		}
		if got := len(m.transcript.entries); got != entries {
			t.Errorf("the transcript grew from %d to %d entries across two landed writes; want no note", entries, got)
		}
		// The `/settings` row mirrors the gesture through the journal the pane reads: Rows() answers
		// the launch snapshot, so without the entry the row would go on saying `true`.
		row := SettingRow{Path: "ui.task-list-open", Section: "Interface", Kind: SettingBool, Value: "true", Default: "true", Editable: true}
		if got, want := m.settingsValueCell(row), "true"+settingsEditMarker; got != want {
			t.Errorf("the /settings value cell = %q, want %q — the toggle is journaled like a pane edit", got, want)
		}
	})

	t.Run("a card added after the flip is seeded from the preference", func(t *testing.T) {
		t.Parallel()

		m := modelWithTaskListBlock(t, testOpts)
		header := markedLine(t, m, targetHeader)
		m = clickCell(t, m, 2, screenRow(t, m, header)) // folds

		addTaskListCard(&m, "2")
		m.refreshViewport()

		cards := taskListEntries(m)
		if len(cards) != 2 || m.transcript.entries[cards[1]].expanded {
			t.Fatalf("a card added under the folded preference starts open (cards %v); it is seeded from TaskListFolded", cards)
		}
		m = clickCell(t, m, 2, screenRow(t, m, headerLineOf(t, m, cards[1]))) // opens both
		addTaskListCard(&m, "3")
		m.refreshViewport()
		cards = taskListEntries(m)
		if len(cards) != 3 || !m.transcript.entries[cards[2]].expanded {
			t.Fatalf("a card added under the open preference starts folded (cards %v)", cards)
		}
	})

	t.Run("a folded launch seeds every card folded, and a resumed record too", func(t *testing.T) {
		t.Parallel()

		opts := testOpts
		opts.TaskListFolded = true
		m := modelWithTaskListBlock(t, opts)
		cards := taskListEntries(m)
		if len(cards) != 1 || m.transcript.entries[cards[0]].expanded {
			t.Fatalf("under ui.task-list-open: false the live card starts open (cards %v)", cards)
		}
		if got, want := taskListHeaders(m), []string{"✦ Task List (1/3) " + glyphCollapsed}; !reflect.DeepEqual(got, want) {
			t.Errorf("first paint headers = %q, want %q", got, want)
		}

		// The resume path: a record's cards carry no fold, and are seeded from the file's preference
		// as they are replayed.
		blob, err := encodeTranscript(&m.transcript)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		folded := newTestModelEng(t, &fakeEngine{}, opts)
		folded.replayScrollback(blob, "resumed", false)
		open := newTestModelEng(t, &fakeEngine{}, testOpts)
		open.replayScrollback(blob, "resumed", false)
		if at := taskListEntries(folded); len(at) != 1 || folded.transcript.entries[at[0]].expanded {
			t.Errorf("a record replayed under the folded preference paints its card open (cards %v)", at)
		}
		if at := taskListEntries(open); len(at) != 1 || !open.transcript.entries[at[0]].expanded {
			t.Errorf("a record replayed under the open preference paints its card folded (cards %v)", at)
		}
	})

	t.Run("a nil settings host flips and writes nothing", func(t *testing.T) {
		t.Parallel()

		m := modelWithTaskListBlock(t, testOpts) // testOpts wires no SettingsHost
		entries := len(m.transcript.entries)
		header := markedLine(t, m, targetHeader)

		m = clickCell(t, m, 2, screenRow(t, m, header))

		if blockExpanded(t, m, header) || !m.opts.TaskListFolded {
			t.Error("the fold did not flip under a nil seam; the Driver degrade is flip-and-forget")
		}
		if got := len(m.transcript.entries); got != entries {
			t.Errorf("the transcript grew from %d to %d entries under a nil seam; want no note", entries, got)
		}
		if len(m.settingEdits) != 0 {
			t.Errorf("edits = %+v, want none journaled: nothing was written", m.settingEdits)
		}
	})

	t.Run("a failed write warns once and the flip stands", func(t *testing.T) {
		t.Parallel()

		log := &settingsWriteLog{err: errors.New("config.yaml: permission denied")}
		opts := testOpts
		opts.Settings = fakeSettingsHost{write: log.write}
		m := modelWithTaskListBlock(t, opts)
		header := markedLine(t, m, targetHeader)

		m = clickCell(t, m, 2, screenRow(t, m, header))

		if blockExpanded(t, m, header) || !m.opts.TaskListFolded {
			t.Error("the refused write unwound the fold; the session keeps the flipped state")
		}
		var warnings []string
		for _, e := range m.transcript.entries {
			if e.kind == entryError {
				warnings = append(warnings, e.text)
			}
		}
		if want := []string{"task-list-open: not saved: config.yaml: permission denied"}; !reflect.DeepEqual(warnings, want) {
			t.Errorf("transcript warnings = %q, want exactly %q", warnings, want)
		}
		if len(m.settingEdits) != 0 {
			t.Errorf("edits = %+v, want none journaled: the file is unchanged", m.settingEdits)
		}
	})
}

// TestRowlessTaskListCardKeepsTheOrdinaryShape is the header fold's regression guard (the ratified
// call of plan "2026-09-14 - 00", item 2): the fold needs task rows to stand for, so a task_list
// card whose lines hold none takes the ORDINARY targetless collapsed shape. An errored call keeps
// its red `error` verdict and the message in view collapsed — the failure is never folded away —
// and a fence-only result, with no line at all, hides nothing, wears no indicator and is not a toggle
// target.
func TestRowlessTaskListCardKeepsTheOrdinaryShape(t *testing.T) {
	t.Parallel()

	t.Run("an errored call shows its verdict collapsed", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: taskListCall})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "1", IsError: true, Content: "the task list holds at most 40 tasks"}})

		if !tr.entries[0].tool.collapsesToHeader {
			t.Fatal("the errored card lost the header-fold mark; it is the registry's word about the tool, not the result")
		}
		collapsed := strings.Split(renderPlain(tr, 80), "\n")
		if len(collapsed) > 1+collapsedBodyCap {
			t.Errorf("the collapsed errored card stands %d rows tall, want at most the budget's %d:\n%s",
				len(collapsed), 1+collapsedBodyCap, strings.Join(collapsed, "\n"))
		}
		if collapsed[0] != "✦ Task List" {
			t.Errorf("header = %q, want the bare label — no count over an error, and nothing folded behind it", collapsed[0])
		}
		painted := strings.Join(collapsed, "\n")
		for _, want := range []string{"error", "the task list holds at most 40 tasks"} {
			if !strings.Contains(painted, want) {
				t.Errorf("the collapsed errored card:\n%s\nwant %q in view — a failure verdict is never folded away", painted, want)
			}
		}
	})

	t.Run("a fence-only result hides nothing", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: taskListCall})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
			CallID: "1", Content: fmt.Sprintf(tasklist.HeaderFormat, 0, 0)}})

		if got := renderPlain(tr, 80); got != "✦ Task List" {
			t.Errorf("the fence-only card paints %q, want the bare header and nothing beneath", got)
		}
		if got := blockMarks(t, tr, 80); got != nil {
			t.Errorf("marks on the fence-only card = %+v, want none — there is nothing to reveal", got)
		}
	})
}
