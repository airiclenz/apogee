package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// A batch of reads folds into one block: a single ✦ Read header carrying the member count,
// ┝ ┝ ┕ rails, and every member row a leader row — target left, dotted leader, outcome flush
// against the row's right edge — the shape docs/layout/tool-layout.md sketches.
func TestRenderGroupsConsecutiveSameLabelCalls(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "README.md", 1, 154, 0)
	readCall(tr, "c2", "TODO.md", 1, 408, 0)
	readCall(tr, "c3", "ISSUES.md", 1, 8, 0)

	want := strings.Join([]string{
		"✦ Read (3)",
		"  ┝ README.md ⋯ 154 lines",
		"  ┝ TODO.md ⋯ 408 lines",
		"  ┕ ISSUES.md ⋯ 8 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("grouped block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A grouped run inside a sub-agent is framed like any other block: every line of the group — header,
// branches, and the separators between its blocks alike — carries the │ rail gutter, so the run
// reads as one continuous frame.
func TestRenderGroupsInsideSubAgent(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "a.go", 1, 5, 1)
	readCall(tr, "c2", "bb.go", 1, 9, 1)

	want := strings.Join([]string{
		"│ ✦ Read (2)",
		"│   ┝ a.go ⋯ 5 lines",
		"│   ┕ bb.go ⋯ 9 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("railed group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// Two different tools that share a friendly label group together — the reader groups by what the
// header says, not by tool id.
func TestRenderGroupsDifferentToolsSharingALabel(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "single_find_and_replace", Arguments: []byte(`{"path":"a.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "replaced text in a.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "multi_find_and_replace", Arguments: []byte(`{"path":"bb.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "applied 2 replacements to bb.go"}})

	want := strings.Join([]string{
		"✦ Replace (2)",
		"  ┝ a.go ⋯ replaced text in a.go",
		"  ┕ bb.go ⋯ applied 2 replacements to bb.go",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("shared-label group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The flip side of the rule, and the ratified table's doing: grouping keys on the LABEL, so the
// table's split of "Edit File" into Edit (edit_existing_file) and Replace (the find-and-replace
// pair) splits the run too. Two adjacent calls that used to read as one "Edit File (2)" now head
// two RUNS — two type rows of the umbrella they fold under, since adjacent runs of different labels
// are what a super-group is — which is the point of the rename: a patch and a find-and-replace are
// different acts, and a reader scanning the rows should be told which one ran.
func TestRenderSplitsEditFromReplace(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "edit_existing_file",
		Arguments: []byte(`{"path":"a.go","content":"x"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "updated a.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"a.go","oldText":"x","newText":"y"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "replaced text in a.go"}})

	if run := toolCallRun(tr.entries, 0); len(run) != 1 {
		t.Fatalf("toolCallRun over Edit then Replace = %d views, want 1 — the labels differ", len(run))
	}
	got := renderPlain(tr, 80)
	for _, want := range []string{"┝ Edit ", "┕ Replace "} {
		if !strings.Contains(got, want) {
			t.Errorf("painted transcript is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "(2)") {
		t.Errorf("the two calls were grouped under one type row:\n%s", got)
	}
}

// The umbrella's three states, in the order the canon spec sketches them (docs/layout/tool-layout.md
// — "Grouped tools collapsed / expanded 1st step / expanded 2nd step (different types /
// super-group)"): the type rows alone, one row opened to the calls behind it, and one of those calls
// opened to its own body. One transcript walks all three, so the goldens read as the steps a reader
// actually takes rather than as three unrelated fixtures.
//
// The shape each step pins is the spec's: a header naming the umbrella and counting its CALLS and
// never a state indicator, since its floor is the type rows; one row per consecutive run in time
// order, counting the run only where it holds more than one call; the run's aggregate in the outcome
// slot ("14 lines" for the two reads, summed); member rows one level deeper under the │ gutter that
// continues the row they opened out of; and an open member's body under a second gutter, closed by
// the see-less footer.
func TestRenderSuperGroupSketchStates(t *testing.T) {
	// entries[0] and [1] are the two reads — one run — and entries[2] is the Terminal call that makes
	// the second, which is what an umbrella needs at all (toolSuperGroup).
	const readHead, runHead = 0, 2

	build := func(t *testing.T) *transcript {
		t.Helper()
		tr := &transcript{}
		readCall(tr, "c1", "a.go", 1, 5, 0)
		readCall(tr, "c2", "b.go", 1, 9, 0)
		runCall(tr, "c3", "go test", "ok   a\nPASS", 0)
		return tr
	}
	header := []string{
		"✦ Tools (3 calls)",
	}
	readRowShut := groupMemberLine("  ┝ Read (2) ⋯ 14 lines")
	readRowOpen := []string{
		leaderEdgeRow("  ┝ Read (2) ⋯ 14 lines", glyphExpanded),
		"  │ ┝ a.go ⋯ 5 lines",
		"  │ ┕ b.go ⋯ 9 lines",
	}

	t.Run("collapsed to its type rows", func(t *testing.T) {
		want := strings.Join(append(header,
			readRowShut,
			groupMemberLine("  ┕ Terminal ⋯ exit 0"),
		), "\n")
		if got := renderPlain(build(t), 80); got != want {
			t.Errorf("collapsed umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("1st step: a type row opened to its members", func(t *testing.T) {
		tr := build(t)
		if !tr.setTypeExpanded(readHead, true) {
			t.Fatalf("setTypeExpanded(%d, true) = false; want the Read run's type row open", readHead)
		}
		want := strings.Join(append(append(header, readRowOpen...),
			groupMemberLine("  ┕ Terminal ⋯ exit 0"),
		), "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("1st-step umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("2nd step: a member opened to its body", func(t *testing.T) {
		tr := build(t)
		if !tr.setTypeExpanded(readHead, true) || !tr.setTypeExpanded(runHead, true) {
			t.Fatal("setTypeExpanded = false; want both type rows of the umbrella open")
		}
		if !tr.setExpanded(runHead, true) {
			t.Fatalf("setExpanded(%d, true) = false; want the Terminal member open", runHead)
		}
		want := strings.Join(append(append(header, readRowOpen...),
			leaderEdgeRow("  ┕ Terminal ⋯ exit 0", glyphExpanded),
			leaderEdgeRow("  │ ┕ go test ⋯ exit 0", glyphExpanded),
			"  │ │ ok   a",
			"  │ │ PASS",
			memberEdgeRow(t, "  │ │", promptSeeLess, 80),
		), "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("2nd-step umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// runGroup folds a batch of same-label terminal calls, each with its output, into a fresh
// transcript at depth — the sketch's "✦ Terminal (3)" fixture (docs/layout/tool-layout.md). Each call is
// a {command, output} pair. They carry bodies deliberately: that is what gives every member
// something to reveal and so a state of its own to be opened.
func runGroup(depth int, calls ...[2]string) *transcript {
	tr := &transcript{}
	base := domain.EventBase{Depth: depth}
	for i, c := range calls {
		id := "c" + strconv.Itoa(i+1)
		tr.apply(domain.ToolCallEvent{EventBase: base, Call: domain.ToolCall{ID: id, Tool: "terminal",
			Arguments: []byte(`{"command":"` + c[0] + `"}`)}})
		tr.apply(domain.ToolResultEvent{EventBase: base,
			Result: domain.ToolResult{CallID: id, Content: c[1]}})
	}
	return tr
}

// TestRenderGroupsBodyCarryingCalls is the grouping scope's new half (design call 3): a call that
// carries a body groups exactly as a bodiless one does, and pays for it with a member row held to
// ONE line. Three Terminal calls with output are the sketch's own case
// (docs/layout/tool-layout.md): one header counting them, one row each, and every ▶ flush against
// the block's right edge, whatever the commands beneath it are doing.
func TestRenderGroupsBodyCarryingCalls(t *testing.T) {
	tr := &transcript{}
	for _, c := range []struct{ id, command, output string }{
		{"c1", "go build ./...", "ok\nbuilt"},
		{"c2", "go vet ./...", "clean\nno findings\ndone"},
		{"c3", "go test ./...", "ok\nPASS"},
	} {
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: c.id, Tool: "terminal",
			Arguments: []byte(`{"command":"` + c.command + `"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: c.id, Content: c.output}})
	}

	want := strings.Join([]string{
		"✦ Terminal (3)",
		groupMemberLine("  ┝ go build ./... ⋯ exit 0"),
		groupMemberLine("  ┝ go vet ./... ⋯ exit 0"),
		groupMemberLine("  ┕ go test ./... ⋯ exit 0"),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("body-carrying group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The count is the group's own arithmetic and is painted as such: it rides the header in the faint
// indicator tone rather than in the label's bold gold, so a reader scanning the gold down the
// left edge does not read "(3)" as part of the tool's name (design call 6). The header wears no
// state indicator and takes no click — the members own their state.
func TestGroupHeaderCountIsFaintAndInert(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	tr := &transcript{}
	readCall(tr, "c1", "main.go", 1, 154, 0)
	readCall(tr, "c2", "util.go", 1, 42, 0)

	header := tr.renderLines(th, 80)[0]
	if want := th.toolIndicator.Render("(2)"); !strings.Contains(header, want) {
		t.Errorf("header %q does not carry the faint-styled count %q", header, want)
	}
	if styled := th.toolLabel.Render("Read (2)"); strings.Contains(header, styled) {
		t.Errorf("header %q paints the count in the label's own style", header)
	}
	if strings.ContainsAny(strip(header), glyphCollapsed+glyphExpanded) {
		t.Errorf("group header %q wears a state indicator; the members own their state", strip(header))
	}
	if got := blockMarks(t, tr, 80); got != nil {
		t.Errorf("group marks = %+v, want none — a group header is not a click target", got)
	}
}

// A member's summary is never traded away for more of its target: the outcome's cells are reserved
// first, the leader flexes down to its floor, and only then is the target cut, ending in the clip
// tail (design call 4). What lines the outcomes up down the block's edge is no longer a shared
// column measured across the members — it is that every member row fills its room EXACTLY, so each
// outcome ends flush against the reserved indicator field whatever the target beside it did.
//
// The fixture is deliberately one long member and one short one: the short row proves the leader
// stretches to hold its outcome at the same edge the cut row reaches by giving the target up.
func TestGroupMemberKeepsItsSummaryAndClipsTheTarget(t *testing.T) {
	const width = 60
	long := "cd . && " + strings.Repeat("echo one-more-fragment && ", 6) + "true"

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal",
		Arguments: []byte(`{"command":"` + long + `"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "abc1234"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal",
		Arguments: []byte(`{"command":"pwd"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "/repo"}})

	// The RAW rows, not renderPlain's: the leader's length is the very thing under test here, and
	// renderPlain collapses it (transcript_test.go).
	th := newTheme(scheme.Default())
	rows := tr.renderLines(th, width)
	if len(rows) != 3 {
		t.Fatalf("group painted %d rows, want 3 — one header and one row per member:\n%s",
			len(rows), strings.Join(rows, "\n"))
	}
	for _, tc := range []struct {
		name    string
		row     string
		summary string
		cut     bool
	}{
		{"clipped member", strip(rows[1]), "abc1234", true},
		{"short member", strip(rows[2]), "/repo", false},
	} {
		if got := th.measure.Width(tc.row); got != toolRowCells(th, width) {
			t.Errorf("%s measures %d cells, want the row's whole room of %d: %q",
				tc.name, got, toolRowCells(th, width), tc.row)
		}
		if !strings.HasSuffix(tc.row, tc.summary) {
			t.Errorf("%s = %q; want its outcome %q printed whole and flush at the edge",
				tc.name, tc.row, tc.summary)
		}
		if got := strings.Count(tc.row, glyphLeaderDot); got < leaderMinDots {
			t.Errorf("%s carries %d leader dots, want at least the floor of %d: %q",
				tc.name, got, leaderMinDots, tc.row)
		}
		if got := strings.Contains(tc.row, clipTail); got != tc.cut {
			t.Errorf("%s target cut = %v, want %v: %q", tc.name, got, tc.cut, tc.row)
		}
	}
}

// TestExpandedGroupMemberPaintsTheSketchShape is the open member, whole, against the sketch's
// "middle one expanded" (docs/layout/tool-layout.md): the branch marker and the full target with ▼
// where the ▶ was, the body under a │ gutter, and a right-aligned see-less row closing it — with
// the siblings still one row each, untouched. It is one golden because the shape is the point: a
// gutter that lost its space or a see-less row that drifted off the edge is the failure this catches.
func TestExpandedGroupMemberPaintsTheSketchShape(t *testing.T) {
	const width = 80
	tr := runGroup(0,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"},
		[2]string{"go test ./...", "ok\nPASS"})
	if !tr.setExpanded(1, true) {
		t.Fatal("setup: entries[1] is not a toggleable block")
	}

	want := strings.Join([]string{
		"✦ Terminal (3)",
		groupMemberLine("  ┝ go build ./... ⋯ exit 0"),
		leaderEdgeRow("  ┝ go vet ./... ⋯ exit 0", glyphExpanded),
		"  │ clean",
		"  │ no findings",
		"  │ done",
		memberEdgeRow(t, "  │ ", promptSeeLess, width),
		groupMemberLine("  ┕ go test ./... ⋯ exit 0"),
	}, "\n")
	got := renderPlain(tr, width)
	if got != want {
		t.Errorf("open-member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if strings.Contains(got, "more line") {
		t.Errorf("the open member grew a remainder marker; it hides nothing:\n%s", got)
	}
}

// TestSeeLessFooterClosesAnOpenBody is the single block's footer at the seam that decides it: an
// expanded block that painted a body closes it with a right-aligned see less…, and the two blocks a
// footer would be lying to get none. One hides nothing, so it is no click target at all and a
// see-less there would offer a click that does nothing; one painted no body row, so there is nothing
// above the footer for it to be closing — the sub-agent run whose whole reveal is its railed span.
func TestSeeLessFooterClosesAnOpenBody(t *testing.T) {
	const width = 60
	th := newTheme(scheme.Default())
	body := []string{"    ok   a", "    PASS"}
	for _, tc := range []struct {
		name   string
		body   []string
		toggle targetKind
		want   []string
	}{
		{"an open body closes with the footer", body, targetHeader, []string{seeLessFooterLine(t, width)}},
		{"a block that hides nothing offers no click", body, targetNone, nil},
		{"an empty body has nothing to close", nil, targetHeader, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := seeLessFooter(th, tc.body, width, tc.toggle)
			if len(rows) != len(tc.want) {
				t.Fatalf("footer painted %d rows, want %d: %q", len(rows), len(tc.want), rows)
			}
			for i, row := range rows {
				if got := strip(row); got != tc.want[i] {
					t.Errorf("footer row %d = %q, want %q", i, got, tc.want[i])
				}
				if got := th.measure.Width(strip(row)); got != width {
					t.Errorf("footer row %d measures %d cells, want the block's whole %d", i, got, width)
				}
			}
		})
	}
}

// Every row an open member paints belongs to that member and says so: the marks name entry 1 down
// the whole of it — first row, body and see-less row alike — while the siblings' single rows name
// entries 0 and 2 and the group header names nothing at all. This is the click surface the mouse
// then resolves against (mouse.go, toggleBlockAt).
func TestGroupMemberMarksNameTheirOwnCalls(t *testing.T) {
	tr := runGroup(0,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"},
		[2]string{"go test ./...", "ok\nPASS"})
	if !tr.setExpanded(1, true) {
		t.Fatal("setup: entries[1] is not a toggleable block")
	}

	// Line 0 is the header and carries no mark, so the marks start on line 1 and run to the end:
	// one row for the first member, six for the open one, one for the last.
	marks := blockMarks(t, tr, 80)
	want := []int{0, 1, 1, 1, 1, 1, 2} // the entry each marked line names, in paint order
	if len(marks) != len(want) {
		t.Fatalf("group painted %d marked rows, want %d:\n%+v", len(marks), len(want), marks)
	}
	for i, mark := range marks {
		if mark.line != i+1 || mark.kind != targetHeader || mark.entry != want[i] {
			t.Errorf("mark %d = %+v; want line %d, a toggle naming entry %d", i, mark, i+1, want[i])
		}
	}
}

// An open member nested inside a sub-agent run wears TWO vertical bars on the same row, and they
// must not read as one: the run's rail is the label gold and the member's gutter is the detail
// tone (design call 8). Painted in the same style, a member's body would look like a section of the
// delegate's frame rather than the output of one call inside it.
func TestExpandedMemberGutterIsNotTheSubAgentRail(t *testing.T) {
	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	tr := runGroup(1,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"})
	if !tr.setExpanded(0, true) {
		t.Fatal("setup: entries[0] is not a toggleable block")
	}

	var row string
	for _, ln := range tr.renderLines(th, 80) {
		if strings.Contains(strip(ln), "│ built") {
			row = ln
		}
	}
	if row == "" {
		t.Fatalf("no body row under the member gutter in:\n%s", strings.Join(tr.renderLines(th, 80), "\n"))
	}
	if !strings.Contains(row, th.toolDetail.Render(memberGutter)) {
		t.Errorf("row %q does not carry the gutter in the detail tone", row)
	}
	if strings.Contains(row, th.subRail.Render(memberGutter)) {
		t.Errorf("row %q paints the member gutter in the sub-agent rail's style", row)
	}
	if !strings.HasPrefix(row, th.subRail.Render(glyphSubRail+" ")) {
		t.Errorf("row %q lost the run's own rail; the fixture no longer nests the group", row)
	}
}
