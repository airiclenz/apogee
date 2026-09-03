package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The tool header's label styling
// ----------------------------------------------------------------------------

// A tool header shows the label alone — no brackets and, now that the block shape is uniform, no
// target either — and that label carries the bold-gold style, baked in before the wrap so the
// visible text is unaffected. The style assertion is a loose contains against the theme's own
// render rather than a byte-exact golden, so a lipgloss change cannot false-fail it; the guard
// below it catches the opposite failure, a toolLabel role that paints nothing at all.
func TestToolHeaderLabelStyled(t *testing.T) {
	th := newTheme(scheme.Default())
	block := renderToolBlock(th, toolView{Label: "Read", Target: "main.go"}, 80, blockState{}).lines
	head := block[0]

	if got, want := ansi.Strip(head), "✦ Read"; got != want {
		t.Errorf("header text = %q; want %q (no brackets, and never a target)", got, want)
	}
	if got, want := ansi.Strip(block[1]), "  ┕ main.go "; !strings.HasPrefix(got, want) {
		t.Errorf("branch text = %q; want it to open %q (the target leads the branch)", got, want)
	}
	styled := th.toolLabel.Render("Read")
	if styled == "Read" {
		t.Fatal("the toolLabel role renders no escape sequence; the header would be unstyled")
	}
	if !strings.Contains(head, styled) {
		t.Errorf("header %q does not carry the styled label %q", head, styled)
	}
}

// TestLeaderRowSpendsItsRoomInOrder is design call 4 asked of the painter itself
// (docs/layout/tool-layout.md, leaderRow): the outcome slot is reserved FIRST and prints whole, the
// dotted leader flexes down to its floor of one, and only then is the target cut — dropped outright
// if a row that narrow leaves it nothing. Every case measures the row against the room it was given,
// because the shape's whole promise is a row that fills its width exactly and so puts its outcome
// flush against the block's edge whatever it is carrying.
//
// It goes at the painter rather than through the transcript because these are the geometries a
// fixture cannot aim at: the rendered goldens collapse the leader to one dot precisely so they stop
// asserting this arithmetic (renderPlain, transcript_test.go), and it is asserted here instead.
func TestLeaderRowSpendsItsRoomInOrder(t *testing.T) {
	t.Parallel()

	const (
		marker = "┕ "
		short  = "main.go"
		long   = "internal/tui/deeply/nested/package/holding/one/very/long/path/main.go"
		stat   = "12 lines"
		// An outcome longer than any row below, for the tail case design call 4 does not word.
		wide = "replaced 3 occurrences across internal/tui/render.go and internal/tui/mouse.go"
	)

	th := newTheme(scheme.Default())
	for _, tc := range []struct {
		name      string
		target    string
		summary   string
		remainder string // the collapsed paint's "+N more lines", which rides the slot (slotText)
		room      int
		expanded  bool

		wantTarget  string // the target text the row must carry whole, or "" when it is cut or dropped
		wantSlot    string // the outcome text the row must END in
		wantDropped bool   // no cell of the target survived: the leader opens straight after the marker
		wantClip    bool   // the row carries a clip tail somewhere
		wantFloor   bool   // the leader is down to leaderMinDots
		wantFailed  bool   // the outcome is painted in the failure tone
	}{{
		// Room to spare: nothing gives way and the leader stretches to hold the outcome at the edge.
		name:   "a wide row keeps target, leader and outcome whole",
		target: short, summary: stat, room: 60,
		wantTarget: short, wantSlot: stat,
	}, {
		// The first thing spent: the dots, down to the floor, with the target still whole. At room 20
		// the marker (2), the target (7), the two gaps and the outcome (8) leave exactly one dot.
		name:   "the leader gives way first",
		target: short, summary: stat, room: 20,
		wantTarget: short, wantSlot: stat, wantFloor: true,
	}, {
		// The second: the target is cut, and the outcome is untouched.
		name:   "the target gives way next",
		target: long, summary: stat, room: 40,
		wantSlot: stat, wantClip: true, wantFloor: true,
	}, {
		// The third: a row with nothing left to give up shows WHAT HAPPENED and drops the target
		// outright rather than trading away half the outcome for a stub of a path — a budget this
		// narrow being narrower than the clip tail alone.
		name:   "a narrow row drops the target and keeps the outcome",
		target: long, summary: stat, room: 12,
		wantSlot: stat, wantDropped: true, wantFloor: true,
	}, {
		// Past the point design call 4 words: an outcome wider than the whole row is cut too, since
		// one printed whole there would overrun the frame and be folded onto a second row.
		name:   "an outcome wider than the row is itself cut",
		target: short, summary: wide, room: 30,
		wantSlot: clipTail, wantDropped: true, wantClip: true,
	}, {
		// Design call 11: the red outcome is the only mark a failure leaves on the row.
		name:   "a failed outcome is red",
		target: short, summary: "error: exit 1", room: 60,
		wantTarget: short, wantSlot: "error: exit 1", wantFailed: true,
	}, {
		// The same row open: the state reaches the TONES and never the geometry, which is what lets
		// a click that opened a block close it without the row moving out from under the pointer.
		name:   "an open row keeps the shape and changes tone",
		target: short, summary: stat, room: 20, expanded: true,
		wantTarget: short, wantSlot: stat, wantFloor: true,
	}, {
		// The remainder joins the OUTCOME instead of standing on a row beneath it, in the slot's own
		// separator, and the whole slot is reserved and painted as one (slotText).
		name:   "a collapsed row counts its hidden body inside the slot",
		target: short, summary: stat, remainder: "+3 more lines", room: 60,
		wantTarget: short, wantSlot: stat + " · +3 more lines",
	}, {
		// Error dominance, unchanged by the tail: the count says nothing about the verdict, so the
		// whole slot stays red on a call that failed with body behind it.
		name:   "a failed outcome keeps its red with the count appended",
		target: short, summary: "error: exit 1", remainder: "+3 more lines", room: 60,
		wantTarget: short, wantSlot: "error: exit 1 · +3 more lines", wantFailed: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tv := toolView{Target: tc.target, Summary: namedSummary(detailLine{Text: tc.summary})}
			row := leaderRow(th, tv, marker, tc.room, tc.expanded, tc.remainder)
			plain := strip(row)

			if got := th.measure.Width(plain); got != tc.room {
				t.Errorf("row measures %d cells, want the whole room of %d: %q", got, tc.room, plain)
			}
			if !strings.HasSuffix(plain, tc.wantSlot) {
				t.Errorf("row = %q; want it to end in the outcome %q", plain, tc.wantSlot)
			}
			if tc.wantTarget != "" && !strings.Contains(plain, tc.wantTarget) {
				t.Errorf("row = %q; want the target %q whole", plain, tc.wantTarget)
			}
			// Dropped means DROPPED: the leader opens straight after the marker, so not one cell of
			// the target — nor a lone clip tail standing in for it — is left on the row.
			if got := strings.HasPrefix(plain, marker+glyphLeaderDot); got != tc.wantDropped {
				t.Errorf("row = %q opens with its leader = %v, want the target dropped = %v",
					plain, got, tc.wantDropped)
			}
			if got := strings.Contains(plain, clipTail); got != tc.wantClip {
				t.Errorf("row = %q carries a clip tail = %v, want %v", plain, got, tc.wantClip)
			}

			dots := strings.Count(plain, glyphLeaderDot)
			if dots < leaderMinDots {
				t.Errorf("row = %q carries %d leader dots, below the floor of %d", plain, dots, leaderMinDots)
			}
			if tc.wantFloor && dots != leaderMinDots {
				t.Errorf("row = %q carries %d leader dots, want the floor of %d", plain, dots, leaderMinDots)
			}
			// The dots wear the `tool-leader` role of their own, so a scheme can damp them without
			// moving the target's tone with them.
			if leader := th.toolLeader.Render(strings.Repeat(glyphLeaderDot, dots)); !strings.Contains(row, leader) {
				t.Errorf("the leader is not painted in the tool-leader role: %q", row)
			}
			// Design call 11 both ways: red when the wording says the call failed, and the row's own
			// tone when it does not. The slot is read off the row STRUCTURALLY — everything past the
			// last dot and its gap — so the assertion holds for the case whose outcome was itself
			// cut and whose final text no fixture spells out.
			slot := strings.TrimPrefix(plain[strings.LastIndex(plain, glyphLeaderDot)+len(glyphLeaderDot):], " ")
			if got := strings.Contains(row, th.errorText.Render(slot)); got != tc.wantFailed {
				t.Errorf("outcome %q painted in the failure tone = %v, want %v", slot, got, tc.wantFailed)
			}
			// Design call 2: what did NOT fail wears the `tool-marker` role — the slot is apogee's
			// reading of the call, not a line of its output — one step up once the block is open.
			slotTone := th.toolMarker
			if tc.expanded {
				slotTone = th.toolMarkerBright
			}
			if !tc.wantFailed && !strings.Contains(row, slotTone.Render(slot)) {
				t.Errorf("outcome %q does not wear the %s marker tone: %q",
					slot, map[bool]string{true: "open", false: "collapsed"}[tc.expanded], row)
			}
			// And never the detail gray it used to borrow, which under `dark` was the leader's own
			// hex: the whole point of the role change is that the slot stops reading as filler.
			if !tc.wantFailed && strings.Contains(row, detailTone(th, tc.expanded).Render(slot)) {
				t.Errorf("outcome %q still wears the row's detail tone: %q", slot, row)
			}
		})
	}
}

// TestPromoteGuardHoldsFifteenCellsOfTarget is design call 5 asked of the painter
// (docs/layout/tool-layout.md, "Width and overflow"): a one-line output may have the outcome slot
// only while the row still keeps promoteMinTargetCells of target beside the floor dot. Below that
// the line goes back where it came from — the first line of the body — and the presenter's typed
// stat takes the slot (guardPromotions, toolView.demoted).
//
// The threshold is computed from the constants rather than spelled as a width, so a change to the
// row's own arithmetic moves the boundary the test aims at instead of silently landing both rows on
// the same side of it. Both sides then assert the SAME invariant — fifteen cells of target — which
// is the whole of what the guard buys; what differs between them is only which reading of the
// outcome the slot got.
func TestPromoteGuardHoldsFifteenCellsOfTarget(t *testing.T) {
	t.Parallel()

	const (
		target = "git rev-parse HEAD"
		output = "abc1234"
		stat   = "1 line"
	)
	// The stat is the value its producer builds, spelled by the slot itself (statValue.spell) — the
	// same "1 line" the const names, reached the way a real promotion reaches it.
	statOfOneLine := pluralStat(1, "line")
	th := newTheme(scheme.Default())
	promoted := toolView{Label: "Terminal", Target: target, stat: statOfOneLine,
		Summary: quotedSummary(detailLine{Text: output})}

	// The narrowest width at which the promoted line still leaves the target its floor: the room a
	// row lays out in is the width less the indicator field, and leaderRow spends it on the marker,
	// the slot, both gaps and the one dot before the target sees a cell of it.
	edge := promoteMinTargetCells + th.measure.Width(output) + 2*leaderGap + leaderMinDots +
		th.measure.Width(branchMarker(true)) + groupIndicatorCells(th)

	// targetCells is what the painted row left the target: everything before the leader, less the
	// marker leading it and the gap the dots stand clear of.
	targetCells := func(plain string) int {
		head := plain[:strings.Index(plain, glyphLeaderDot)]
		return th.measure.Width(strings.TrimRight(head, " ")) - th.measure.Width(branchMarker(true))
	}

	for _, tc := range []struct {
		name     string
		view     toolView
		width    int
		expanded bool

		wantSlot     string // the outcome slot's text on the branch row
		wantCount    string // the remainder count the slot must carry, or "" when the row cannot seat one
		wantBodyLine string // the row the body must carry, or "" when the block keeps none
	}{{
		// At the threshold exactly the line is promoted: the target still has its fifteen cells.
		name: "the line takes the slot while the target keeps its floor",
		view: promoted, width: edge,
		wantSlot: output,
	}, {
		// One cell narrower and the guard refuses: the stat says what happened and the line drops
		// into the body, which a collapsed block counts rather than paints. At THIS width the count
		// cannot ride the slot either — seating it would spend the very cells of target the guard
		// just protected — so the row gives it up first and the ▶ carries the news alone
		// (affordableSlot).
		name: "one cell short and the line goes back to the body",
		view: promoted, width: edge - 1,
		wantSlot: stat,
	}, {
		// The same block open: the demoted line is one click away, whole, which is what makes the
		// guard a MOVE rather than a truncation.
		name: "the demoted line is what opening the block reveals",
		view: promoted, width: edge - 1, expanded: true,
		wantSlot: stat, wantBodyLine: output,
	}, {
		// The case the guard exists for: an output far too long to share a row with anything never
		// reaches the slot at any width a terminal has, and the collapsed block counts it as the one
		// body line it is — however many rows it would take to print.
		name: "a monster one-line output stays a body",
		view: toolView{Label: "Terminal", Target: target, stat: statOfOneLine,
			Summary: quotedSummary(detailLine{Text: strings.Repeat("x", 300)})},
		width:    120,
		wantSlot: stat, wantCount: "+1 more line",
	}, {
		// The guard asked of a never-ran DELEGATION, whose refusal reaches the slot as a promoted
		// one-line report: at a width the refusal cannot share, it is demoted like any other output
		// and the row goes on naming what was delegated (the issue register, 2026-08-27 — "a long
		// refusal clips the target off"). The row is a click target here for a reason of its own:
		// what a delegation's block hides is the prompt it carried, whichever reading the slot got
		// (subAgentHidesPrompt).
		name: "a never-ran delegation's refusal is held to the same floor",
		view: toolView{Label: "Sub-Agent", Target: "survey the tests", name: subAgentToolName,
			task:    "survey the tests\nand report back",
			stat:    plainStat("done"),
			Summary: quotedSummary(detailLine{Text: refusedResult + " — no further delegation is possible"})},
		width:    80,
		wantSlot: "done", wantCount: "+1 more line",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lines := renderToolBlock(th, tc.view, tc.width,
				blockState{expanded: tc.expanded}).lines
			branch := strip(lines[1])

			if !strings.Contains(branch, tc.wantSlot) {
				t.Errorf("branch row = %q; want the outcome %q in its slot", branch, tc.wantSlot)
			}
			if got := targetCells(branch); got < promoteMinTargetCells {
				t.Errorf("branch row = %q leaves the target %d cells, below the guard's %d",
					branch, got, promoteMinTargetCells)
			}
			// The demoted line is counted in the slot, not on a row beneath it — and only while the
			// row can seat the count without eating into that same floor.
			if tc.wantCount != "" && !strings.Contains(branch, tc.wantCount) {
				t.Errorf("branch row = %q; want the remainder count %q in its slot", branch, tc.wantCount)
			}
			if tc.wantCount == "" && strings.Contains(branch, "more line") {
				t.Errorf("branch row = %q carries a remainder count it has no room for", branch)
			}
			body := strip(strings.Join(lines[2:], "\n"))
			if tc.wantBodyLine == "" {
				if len(lines) != 2 {
					t.Errorf("the block paints %d rows, want the header and its branch alone:\n%s",
						len(lines), strip(strings.Join(lines, "\n")))
				}
				return
			}
			if !strings.Contains(body, tc.wantBodyLine) {
				t.Errorf("body = %q; want the demoted line %q beneath the branch", body, tc.wantBodyLine)
			}
			// The slot holds one reading of the outcome, never both: a demoted line that also
			// printed on the branch would be the row the guard was called in to prevent.
			if !tc.expanded && strings.Contains(branch, output) {
				t.Errorf("branch row = %q still carries the demoted line", branch)
			}
		})
	}
}

// TestDemotedLineKeepsTheSpellingItWasWrittenWith is the promote-guard read against the shortening
// seam (branchSummary): a promoted line is QUOTED content, and demoting it moves where it sits
// without touching what it says. So an in-workspace absolute path a command printed reaches the body
// exactly as the file holds it — the same protection the line had on the branch — while the slot
// beside it falls back to the presenter's own count.
func TestDemotedLineKeepsTheSpellingItWasWrittenWith(t *testing.T) {
	t.Parallel()

	const width = 40
	ws := newWorkspaceRoot("/home/me/proj")
	printed := "/home/me/proj/deep/file.txt"

	tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "terminal",
		Arguments: []byte(`{"command":"cat /home/me/proj/notes.md"}`)}, "", ws)
	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: printed + "\n"}, ws)

	th := newTheme(scheme.Default())
	lines := renderToolBlock(th, tv, width, blockState{expanded: true}).lines
	branch, body := strip(lines[1]), strip(strings.Join(lines[2:], "\n"))

	if !strings.Contains(branch, "exit 0") {
		t.Errorf("branch row = %q; want the typed stat in the slot at width %d", branch, width)
	}
	if !strings.Contains(body, printed) {
		t.Errorf("body = %q; want the printed path spelled as the file holds it", body)
	}
	// The TARGET is the block's own words and shortens as it always did — the split the guard
	// inherits rather than one it introduces.
	if !strings.Contains(branch, "cat notes.md") {
		t.Errorf("branch row = %q; want the command shortened against the workspace", branch)
	}
}

// TestGitCommitSlotIsTheShortHashAtEveryWidth pins the one tool whose one-line output never reaches
// the outcome slot (commitDetail). A commit's line is "6fd6ff7 feat: x" and the target leading the
// row is already "feat: x", so a promotion would print the subject twice and hide the hash the
// ratified table gives this slot (docs/layout/tool-layout.md, "git_commit … short hash"). The two
// widths straddle the promote-guard's own boundary, so the test would fail the moment the hash held
// the slot only on the narrow row — which is exactly what a demotion-dependent reading looked like.
func TestGitCommitSlotIsTheShortHashAtEveryWidth(t *testing.T) {
	t.Parallel()

	const (
		subject = "add the thing"
		hash    = "6fd6ff7"
		output  = hash + " " + subject
	)
	th := newTheme(scheme.Default())

	// The narrowest width at which the guard would still have promoted a line this long — the
	// arithmetic is leaderRow's, borrowed so the cases sit either side of a boundary this tool no
	// longer has (guardRefuses, TestPromoteGuardHoldsFifteenCellsOfTarget).
	edge := promoteMinTargetCells + th.measure.Width(output) + 2*leaderGap + leaderMinDots +
		th.measure.Width(branchMarker(true)) + groupIndicatorCells(th)

	for _, tc := range []struct {
		name  string
		width int
	}{
		{name: "wide enough for the guard to have promoted", width: edge + 40},
		{name: "one cell under the guard's boundary", width: edge - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tv := presentToolCall(domain.ToolCall{ID: "1", Tool: "git_commit",
				Arguments: []byte(`{"message":"` + subject + `"}`)}, "", workspaceRoot{})
			tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: output + "\n"}, workspaceRoot{})

			lines := renderToolBlock(th, tv, tc.width, blockState{expanded: true}).lines
			branch, body := strip(lines[1]), strip(strings.Join(lines[2:], "\n"))

			if !strings.Contains(branch, hash) {
				t.Errorf("branch row = %q; want the short hash in the slot at width %d", branch, tc.width)
			}
			if strings.Contains(branch, output) {
				t.Errorf("branch row = %q carries the whole one-line output beside its target", branch)
			}
			if n := strings.Count(branch, subject); n > 1 {
				t.Errorf("branch row = %q spells the subject %d times", branch, n)
			}
			// Nothing is lost by withholding the promotion: the line the slot refused is the body's
			// first row, whole, exactly where the guard would have put it.
			if !strings.Contains(body, output) {
				t.Errorf("body = %q; want the commit's own line %q beneath the branch", body, output)
			}
		})
	}
}
