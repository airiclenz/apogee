package tui

import (
	"strconv"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// A batch of reads folds under the umbrella like any other run of 2+ groupable calls: the header
// counts the CALLS, one type row counts the run and aggregates it, and the member rows — target
// left, dotted leader, outcome flush against the row's right edge — lie one level down under the
// opened row, the shape docs/layout/tool-layout.md sketches.
func TestRenderGroupsConsecutiveSameLabelCalls(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	readCall(tr, "c1", "README.md", 1, 154, 0)
	readCall(tr, "c2", "TODO.md", 1, 408, 0)
	readCall(tr, "c3", "ISSUES.md", 1, 8, 0)

	want := strings.Join([]string{
		umbrellaHeaderRow("Read 3 files", glyphExpanded),
		groupMemberLine("  ┕ Read (3) ⋯ 570 lines"),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("collapsed same-type run mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setTypeExpanded(0, true) = false; want the Read run's type row open")
	}
	want = strings.Join([]string{
		umbrellaHeaderRow("Read 3 files", glyphExpanded),
		leaderEdgeRow("  ┕ Read (3) ⋯ 570 lines", glyphExpanded),
		"  │ ┝ README.md ⋯ 154 lines",
		"  │ ┝ TODO.md ⋯ 408 lines",
		"  │ ┕ ISSUES.md ⋯ 8 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("opened same-type run mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A grouped run inside a sub-agent is framed like any other block: every line of the group — header,
// branches, and the separators between its blocks alike — carries the │ rail gutter, so the run
// reads as one continuous frame.
func TestRenderGroupsInsideSubAgent(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	readCall(tr, "c1", "a.go", 1, 5, 1)
	readCall(tr, "c2", "bb.go", 1, 9, 1)

	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setTypeExpanded(0, true) = false; want the Read run's type row open")
	}
	want := strings.Join([]string{
		"│ " + umbrellaHeaderRow("Read 2 files", glyphExpanded),
		"│ " + leaderEdgeRow("  ┕ Read (2) ⋯ 14 lines", glyphExpanded),
		"│   │ ┝ a.go ⋯ 5 lines",
		"│   │ ┕ bb.go ⋯ 9 lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("railed group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// Two different tools that share a friendly label group together — the reader groups by what the
// header says, not by tool id.
func TestRenderGroupsDifferentToolsSharingALabel(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "single_find_and_replace", Arguments: []byte(`{"path":"a.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "replaced text in a.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "multi_find_and_replace", Arguments: []byte(`{"path":"bb.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "applied 2 replacements to bb.go"}})

	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setTypeExpanded(0, true) = false; want the Replace run's type row open")
	}
	want := strings.Join([]string{
		umbrellaHeaderRow("Edited 2 files", glyphExpanded),
		leaderEdgeRow("  ┕ Replace (2) ⋯", glyphExpanded),
		"  │ ┝ a.go ⋯ replaced text in a.go",
		"  │ ┕ bb.go ⋯ applied 2 replacements to bb.go",
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
	t.Parallel()

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "edit_existing_file",
		Arguments: []byte(`{"path":"a.go","content":"x"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "updated a.go"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "single_find_and_replace",
		Arguments: []byte(`{"path":"a.go","oldText":"x","newText":"y"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "replaced text in a.go"}})

	if run := sameLabelRun(tr.entries, 0); run != 1 {
		t.Fatalf("sameLabelRun over Edit then Replace = %d calls, want 1 — the labels differ", run)
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
// The shape each step pins is the spec's: a header naming the umbrella, counting its CALLS and
// wearing the fold's ▶/▼ — every umbrella folds to its header line, a SMALL one on its own head flag
// and a LARGE one (more type rows than `ui.tools-fold-over`) under the shared preference, the fold
// cases that close this test; one row per consecutive run in time
// order, counting the run only where it holds more than one call; the run's aggregate in the outcome
// slot ("14 lines" for the two reads, summed); member rows one level deeper under the │ gutter that
// continues the row they opened out of; and an open member's body under a second gutter, closed by
// the see-less footer.
func TestRenderSuperGroupSketchStates(t *testing.T) {
	t.Parallel()

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
		umbrellaHeaderRow("Read 2 files, ran 1 command", glyphExpanded),
	}
	readRowShut := groupMemberLine("  ┝ Read (2) ⋯ 14 lines")
	readRowOpen := []string{
		leaderEdgeRow("  ┝ Read (2) ⋯ 14 lines", glyphExpanded),
		"  │ ┝ a.go ⋯ 5 lines",
		"  │ ┕ b.go ⋯ 9 lines",
	}

	t.Run("collapsed to its type rows", func(t *testing.T) {
		t.Parallel()

		want := strings.Join(append(header,
			readRowShut,
			groupMemberLine("  ┕ Terminal ⋯ exit 0"),
		), "\n")
		if got := renderPlain(build(t), 80); got != want {
			t.Errorf("collapsed umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("1st step: a type row opened to its members", func(t *testing.T) {
		t.Parallel()

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
		t.Parallel()

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

	// A SMALL umbrella's fold is its head entry's own flag (transcript.setBlockFolded): open, the
	// header wears ▼ over every type row; folded, it is the header line alone under a ▶, and the type
	// row a reader opened is untouched beneath it.
	t.Run("small, open: ▼ over every type row", func(t *testing.T) {
		t.Parallel()

		tr := build(t)
		want := strings.Join(append(header,
			readRowShut,
			groupMemberLine("  ┕ Terminal ⋯ exit 0"),
		), "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("open small umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
		if targets := tr.renderView(newTheme(scheme.Default()), 80, false, breadcrumbHint).targets; len(targets) == 0 || targets[0].kind != targetUmbrella {
			t.Errorf("open small header target = %+v; want targetUmbrella on the header line", targets)
		}
	})

	t.Run("small, folded: the header line alone", func(t *testing.T) {
		t.Parallel()

		tr := build(t)
		if !tr.setTypeExpanded(readHead, true) {
			t.Fatalf("setTypeExpanded(%d, true) = false; want the Read run's type row open under the fold", readHead)
		}
		if !tr.setBlockFolded(readHead, true) {
			t.Fatalf("setBlockFolded(%d, true) = false; want the small umbrella folded on its own flag", readHead)
		}
		want := umbrellaHeaderRow("Read 2 files, ran 1 command", defaultGlyphs.collapsed)
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("folded small umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
		if targets := tr.renderView(newTheme(scheme.Default()), 80, false, breadcrumbHint).targets; len(targets) != 1 || targets[0].kind != targetUmbrella {
			t.Errorf("folded small header targets = %+v; want one targetUmbrella line", targets)
		}
	})

	// The two states a LARGE umbrella has (transcript.umbrellaIsLarge): the fixture's two type rows
	// stand over a threshold of 1, so the shared preference decides between the header line alone
	// and the rows — whatever its head flag holds and whether or not a call is still running.
	t.Run("large, folded: the header line alone", func(t *testing.T) {
		t.Parallel()

		tr := build(t)
		tr.toolsFoldOver = 1
		if !tr.setTypeExpanded(readHead, true) {
			t.Fatalf("setTypeExpanded(%d, true) = false; want the Read run's type row open under the fold", readHead)
		}
		want := umbrellaHeaderRow("Read 2 files, ran 1 command", defaultGlyphs.collapsed)
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("folded umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
		if targets := tr.renderView(newTheme(scheme.Default()), 80, false, breadcrumbHint).targets; len(targets) != 1 || targets[0].kind != targetUmbrella {
			t.Errorf("folded header targets = %+v; want one targetUmbrella line", targets)
		}
	})

	t.Run("large, open: ▼ over the rows a fold left as they were", func(t *testing.T) {
		t.Parallel()

		tr := build(t)
		tr.toolsFoldOver = 1
		if !tr.setTypeExpanded(readHead, true) {
			t.Fatalf("setTypeExpanded(%d, true) = false; want the Read run's type row open", readHead)
		}
		if !tr.setToolsOpen(true) {
			t.Fatal("setToolsOpen(true) = false; want the preference to move off its folded default")
		}
		want := strings.Join(append(append([]string{umbrellaHeaderRow("Read 2 files, ran 1 command", glyphExpanded)}, readRowOpen...),
			groupMemberLine("  ┕ Terminal ⋯ exit 0"),
		), "\n")
		if got := renderPlain(tr, 80); got != want {
			t.Errorf("open large umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
		if targets := tr.renderView(newTheme(scheme.Default()), 80, false, breadcrumbHint).targets; len(targets) == 0 || targets[0].kind != targetUmbrella {
			t.Errorf("open large header target = %+v; want targetUmbrella on the header line", targets)
		}
	})

	// A folded umbrella with a call still running is the header line alone with a LIVE star: it
	// blinks by the frame's phase exactly as the open live header's does (blockState.live), and
	// nothing else joins the line — the fold does not spring open for the running call.
	t.Run("large, folded, live: the header alone under a blinking star", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		readCall(tr, "c1", "a.go", 1, 5, 0)
		readCall(tr, "c2", "b.go", 1, 9, 0)
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c3", Tool: "terminal",
			Arguments: []byte(`{"command":"go test"}`)}})
		tr.toolsFoldOver = 1
		th := newTheme(scheme.Default())
		for blink, want := range map[bool]string{
			false: umbrellaHeaderRow("Read 2 files, running 1 command", defaultGlyphs.collapsed),
			true:  blinkedUmbrellaHeaderRow("Read 2 files, running 1 command", defaultGlyphs.collapsed),
		} {
			view := tr.renderView(th, 80, blink, breadcrumbHint)
			if got := strings.TrimRight(collapseLeader(strip(strings.Join(view.lines, "\n"))), " "); got != want {
				t.Errorf("folded live umbrella at blink=%t:\n--- got ---\n%s\n--- want ---\n%s", blink, got, want)
			}
			if len(view.targets) != 1 || view.targets[0].kind != targetUmbrella {
				t.Errorf("folded live header targets at blink=%t = %+v; want one targetUmbrella line", blink, view.targets)
			}
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
// (docs/layout/tool-layout.md): the umbrella counting the calls over one type row that aggregates
// the run — three "exit 0"s are one reading between them (runAggregate) — and, opened, one member
// row each with every ▶ flush against the block's right edge, whatever the commands beneath it are
// doing.
func TestRenderGroupsBodyCarryingCalls(t *testing.T) {
	t.Parallel()

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
		umbrellaHeaderRow("Ran 3 commands", glyphExpanded),
		groupMemberLine("  ┕ Terminal (3) ⋯ exit 0"),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("collapsed body-carrying run mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setTypeExpanded(0, true) = false; want the Terminal run's type row open")
	}
	want = strings.Join([]string{
		umbrellaHeaderRow("Ran 3 commands", glyphExpanded),
		leaderEdgeRow("  ┕ Terminal (3) ⋯ exit 0", glyphExpanded),
		groupMemberLine("  │ ┝ go build ./... ⋯ exit 0"),
		groupMemberLine("  │ ┝ go vet ./... ⋯ exit 0"),
		groupMemberLine("  │ ┕ go test ./... ⋯ exit 0"),
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("body-carrying group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The count is the run's own arithmetic and is painted as such: it rides the TYPE ROW in the faint
// indicator tone rather than in the label's bold gold, so a reader scanning the gold down the
// left edge does not read "(2)" as part of the tool's name (design call 6). The header's own count
// is of CALLS, and the header wears the fold's ▶/▼ and is the block's fold target whatever the
// umbrella's size: a SMALL one folds on its own head flag, a LARGE one (more type rows than
// `ui.tools-fold-over`) under the shared preference.
func TestGroupHeaderCountIsFaintAndInert(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	tr := &transcript{}
	readCall(tr, "c1", "main.go", 1, 154, 0)
	readCall(tr, "c2", "util.go", 1, 42, 0)

	rows := tr.renderLines(th, 80)
	header, typeRow := rows[0], rows[1]
	// The type row paints the count in one run with the space before it, so the fixture is that run.
	if want := th.toolIndicator.Render(" (2)"); !strings.Contains(typeRow, want) {
		t.Errorf("type row %q does not carry the faint-styled count %q", typeRow, want)
	}
	if styled := th.toolLabel.Render("Read (2)"); strings.Contains(typeRow, styled) {
		t.Errorf("type row %q paints the count in the label's own style", typeRow)
	}
	if !strings.HasSuffix(strings.TrimRight(strip(header), " "), " "+glyphExpanded) {
		t.Errorf("umbrella header %q does not wear the fold's %s as its last word", strip(header), glyphExpanded)
	}
	found := false
	for _, mark := range blockMarks(t, tr, 80) {
		if mark.line == 0 {
			found = true
			if mark.kind != targetUmbrella {
				t.Errorf("umbrella header mark = %+v; want targetUmbrella", mark)
			}
		}
	}
	if !found {
		t.Error("umbrella header is no click target; want the line-0 targetUmbrella mark")
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
	t.Parallel()

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
	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setTypeExpanded(0, true) = false; want the Terminal run's type row open")
	}
	rows := tr.renderLines(th, width)
	if len(rows) != 4 {
		t.Fatalf("group painted %d rows, want 4 — the umbrella header, its type row and one row per member:\n%s",
			len(rows), strings.Join(rows, "\n"))
	}
	for _, tc := range []struct {
		name    string
		row     string
		summary string
		cut     bool
	}{
		{"clipped member", strip(rows[2]), "abc1234", true},
		{"short member", strip(rows[3]), "/repo", false},
	} {
		if got := th.measure.Width(tc.row); got != toolRowCells(th, width) {
			t.Errorf("%s measures %d cells, want the row's whole room of %d: %q",
				tc.name, got, toolRowCells(th, width), tc.row)
		}
		if !strings.HasSuffix(tc.row, tc.summary) {
			t.Errorf("%s = %q; want its outcome %q printed whole and flush at the edge",
				tc.name, tc.row, tc.summary)
		}
		if got := strings.Count(tc.row, defaultGlyphs.leaderDot); got < leaderMinDots {
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
	t.Parallel()

	const width = 80
	tr := runGroup(0,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"},
		[2]string{"go test ./...", "ok\nPASS"})
	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setup: entries[0] does not head the umbrella's Terminal type row")
	}
	if !tr.setExpanded(1, true) {
		t.Fatal("setup: entries[1] is not a toggleable block")
	}

	want := strings.Join([]string{
		umbrellaHeaderRow("Ran 3 commands", glyphExpanded),
		leaderEdgeRow("  ┕ Terminal (3) ⋯ exit 0", glyphExpanded),
		groupMemberLine("  │ ┝ go build ./... ⋯ exit 0"),
		leaderEdgeRow("  │ ┝ go vet ./... ⋯ exit 0", glyphExpanded),
		"  │ │ clean",
		"  │ │ no findings",
		"  │ │ done",
		memberEdgeRow(t, "  │ │ ", promptSeeLess, width),
		groupMemberLine("  │ ┕ go test ./... ⋯ exit 0"),
	}, "\n")
	got := renderPlain(tr, width)
	if got != want {
		t.Errorf("open-member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if strings.Contains(got, "more line") {
		t.Errorf("the open member grew a remainder marker; it hides nothing:\n%s", got)
	}
}

// blankRunTranscript is a Terminal run whose last two calls printed nothing: one that failed, one
// that exited cleanly. The first call prints two lines so the group has an ordinary member to
// read the blank ones against.
func blankRunTranscript() *transcript {
	tr := &transcript{}
	for _, c := range []struct {
		id, command, content string
		failed               bool
	}{
		{"c1", "go build ./...", "cwd: /ws\nok\nbuilt", false},
		{"c2", "false", "cwd: /ws\n[exit code 1]", true},
		{"c3", "true", "cwd: /ws\n", false},
	} {
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: c.id, Tool: "terminal",
			Arguments: []byte(`{"command":"` + c.command + `"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: c.id, Content: c.content, IsError: c.failed}})
	}
	return tr
}

// TestGroupMemberWithABlankRunStillOpens is the member row of a run that printed nothing
// (docs/layout/tool-layout.md, "Failed and empty runs"): a failed one keeps its exit code in the
// slot and still carries a ▶, opening onto the failure text the model received — the marker, the
// `cwd:` line off — and a clean one keeps `exit 0` and opens onto "(no output)". Neither row may
// be a dead end whose caret reveals nothing.
func TestGroupMemberWithABlankRunStillOpens(t *testing.T) {
	t.Parallel()

	const width = 80
	tr := blankRunTranscript()
	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setup: entries[0] does not head the umbrella's Terminal type row")
	}
	wantCollapsed := strings.Join([]string{
		umbrellaHeaderRow("Ran 3 commands · 1 failed", glyphExpanded),
		leaderEdgeRow("  ┕ Terminal (3) ⋯ 1 error", glyphExpanded),
		groupMemberLine("  │ ┝ go build ./... ⋯ exit 0"),
		groupMemberLine("  │ ┝ false ⋯ error: exit 1"),
		groupMemberLine("  │ ┕ true ⋯ exit 0"),
	}, "\n")
	if got := renderPlain(tr, width); got != wantCollapsed {
		t.Errorf("collapsed members mismatch — each blank run keeps its slot and a ▶:\n--- got ---\n%s\n--- want ---\n%s",
			got, wantCollapsed)
	}

	if !tr.setExpanded(1, true) || !tr.setExpanded(2, true) {
		t.Fatal("a blank run's member is not a toggleable block — its ▶ reveals nothing")
	}
	wantExpanded := strings.Join([]string{
		umbrellaHeaderRow("Ran 3 commands · 1 failed", glyphExpanded),
		leaderEdgeRow("  ┕ Terminal (3) ⋯ 1 error", glyphExpanded),
		groupMemberLine("  │ ┝ go build ./... ⋯ exit 0"),
		leaderEdgeRow("  │ ┝ false ⋯ error: exit 1", glyphExpanded),
		"  │ │ [exit code 1]",
		memberEdgeRow(t, "  │ │ ", promptSeeLess, width),
		leaderEdgeRow("  │ ┕ true ⋯ exit 0", glyphExpanded),
		"  │ │ (no output)",
		memberEdgeRow(t, "  │ │ ", promptSeeLess, width),
	}, "\n")
	if got := renderPlain(tr, width); got != wantExpanded {
		t.Errorf("expanded members mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, wantExpanded)
	}
}

// TestLoneBlankRunCountsItsOneLine is the same two runs standing alone: a lone block hides its body
// and counts it in the leader row's slot (collapsedRemainder), so the one line a blank run now
// opens onto is counted beside its exit code — `exit 0 · +1 more line` clean, `error: exit 1 ·
// +1 more line` failed — in the remainder wording every other collapsed body uses.
func TestLoneBlankRunCountsItsOneLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, command, content string
		failed                 bool
		wantRow                string
	}{
		{"a clean run that printed nothing", "true", "cwd: /ws\n", false,
			groupMemberLine("  ┕ true ⋯ exit 0 · +1 more line")},
		{"a failed run that printed nothing", "false", "cwd: /ws\n[exit code 1]", true,
			groupMemberLine("  ┕ false ⋯ error: exit 1 · +1 more line")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr := &transcript{}
			tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal",
				Arguments: []byte(`{"command":"` + tc.command + `"}`)}})
			tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: tc.content, IsError: tc.failed}})

			lines := strings.Split(renderPlain(tr, 80), "\n")
			if len(lines) != 2 {
				t.Fatalf("lone block painted %d rows, want its header and one leader row:\n%s",
					len(lines), strings.Join(lines, "\n"))
			}
			if lines[1] != tc.wantRow {
				t.Errorf("leader row = %q, want %q", lines[1], tc.wantRow)
			}
		})
	}
}

// TestSeeLessFooterClosesAnOpenBody is the single block's footer at the seam that decides it: an
// expanded block that painted a body closes it with a right-aligned see less…, and the two blocks a
// footer would be lying to get none. One hides nothing, so it is no click target at all and a
// see-less there would offer a click that does nothing; one painted no body row, so there is nothing
// above the footer for it to be closing — the sub-agent run whose whole reveal is its railed span.
func TestSeeLessFooterClosesAnOpenBody(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

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
// entries 0 and 2, the type row above them names the run's head as the row a click folds, and the
// umbrella's own header — marked on every umbrella, open or shut, large or small — is the row that
// folds the whole umbrella. This is the click surface the mouse then resolves against (mouse.go,
// toggleBlockAt).
func TestGroupMemberMarksNameTheirOwnCalls(t *testing.T) {
	t.Parallel()

	tr := runGroup(0,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"},
		[2]string{"go test ./...", "ok\nPASS"})
	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setup: entries[0] does not head the umbrella's Terminal type row")
	}
	if !tr.setExpanded(1, true) {
		t.Fatal("setup: entries[1] is not a toggleable block")
	}

	// Line 0 is the umbrella's header — the fold's own target, marked on every umbrella — then the
	// type row, then one row for the first member, six for the open one, one for the last.
	marks := blockMarks(t, tr, 80)
	want := []struct {
		kind  targetKind
		entry int
	}{
		{targetUmbrella, 0},
		{targetType, 0},
		{targetHeader, 0},
		{targetHeader, 1}, {targetHeader, 1}, {targetHeader, 1}, {targetHeader, 1}, {targetHeader, 1},
		{targetHeader, 2},
	}
	if len(marks) != len(want) {
		t.Fatalf("group painted %d marked rows, want %d:\n%+v", len(marks), len(want), marks)
	}
	for i, mark := range marks {
		if mark.line != i || mark.kind != want[i].kind || mark.entry != want[i].entry {
			t.Errorf("mark %d = %+v; want line %d, a %v naming entry %d",
				i, mark, i, want[i].kind, want[i].entry)
		}
	}
}

// An open member nested inside a sub-agent run wears TWO vertical bars on the same row, and they
// must not read as one: the run's rail is the label gold and the member's gutter is the detail
// tone (design call 8). Painted in the same style, a member's body would look like a section of the
// delegate's frame rather than the output of one call inside it.
func TestExpandedMemberGutterIsNotTheSubAgentRail(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	if !colorActive(th) {
		t.Skip("no colour profile in this environment; the SGR assertion would be vacuous")
	}
	tr := runGroup(1,
		[2]string{"go build ./...", "ok\nbuilt"},
		[2]string{"go vet ./...", "clean\nno findings\ndone"})
	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setup: entries[0] does not head the umbrella's Terminal type row")
	}
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
	// Under the umbrella the member's gutter follows its type row's, and the two are painted as one
	// run, so that run is what the tone is asserted on.
	gutters := memberGutter + glyphMemberGutter + " "
	if !strings.Contains(row, th.toolDetail.Render(gutters)) {
		t.Errorf("row %q does not carry the gutter in the detail tone", row)
	}
	if strings.Contains(row, th.subRail.Render(gutters)) {
		t.Errorf("row %q paints the member gutter in the sub-agent rail's style", row)
	}
	if !strings.HasPrefix(row, th.subRail.Render(glyphSubRail+" ")) {
		t.Errorf("row %q lost the run's own rail; the fixture no longer nests the group", row)
	}
}

// TestSplitDiffPaintsUnderAnOpenMembersGutter is plan item 7's third paint path: the body an open
// GROUP member buys reads the same two ways as an ungrouped block's, chosen against the member's
// own room. The panes hang under the member's │ gutter like every other line of that body, so the
// open member still reads as one thing rather than as a row followed by loose panes — and the frame
// around them (the ▼ leader row, the see-less row, the sibling still closed) is untouched by the
// swap. This path carries the unspanned sub-agent group member too (renderGroupMember).
func TestSplitDiffPaintsUnderAnOpenMembersGutter(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	for _, id := range []string{"c1", "c2"} {
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: "single_find_and_replace",
			Arguments: []byte(`{"path":"main.go","oldText":"  return errNarrow","newText":"  return errWide"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id,
			Content: "replaced text in main.go", Summary: domain.EditRegions{Regions: paintRegions()}}})
	}
	if !tr.setTypeExpanded(0, true) {
		t.Fatal("setup: entries[0] does not head the umbrella's Replace type row")
	}
	if !tr.setExpanded(1, true) {
		t.Fatal("setup: entries[1] is not a toggleable block")
	}

	// Under the umbrella the member's own gutter follows its type row's, so the body hangs under
	// both.
	gutters := memberGutter + glyphMemberGutter + " "
	wide := strings.Split(renderPlain(tr, 140), "\n")
	if !paintsSplit(wide) {
		t.Errorf("the open member's body at 140 columns is not two panes:\n%s", strings.Join(wide, "\n"))
	}
	for _, row := range wide {
		if strings.Contains(row, "errNarrow") && !strings.HasPrefix(row, gutters) {
			t.Errorf("a pane row hangs outside the member's gutter %q: %q", gutters, row)
		}
	}
	for _, frame := range []string{
		groupMemberLine(memberGutter + glyphBranch + " main.go ⋯ +1 −1"), // the sibling is still closed
		memberEdgeRow(t, gutters, promptSeeLess, 140),                    // and the open one still closes
	} {
		if !strings.Contains(strings.Join(wide, "\n"), frame) {
			t.Errorf("the split body cost the member its frame row %q:\n%s", frame, strings.Join(wide, "\n"))
		}
	}

	if narrow := strings.Split(renderPlain(tr, 80), "\n"); paintsSplit(narrow) {
		t.Errorf("the open member's body at 80 columns painted two panes; its room leaves each under "+
			"%d columns:\n%s", splitPaneMinCols, strings.Join(narrow, "\n"))
	}
}

// TestTargetlessBlockStillCapsAndToggles is the regression half of the header-fold call (plan
// "2026-09-14 - 00", item 2): the mark is ONE tool's, so a targetless block without it still spends
// the collapsed budget: its branch list is cut at collapsedBodyCap, its header wears the ▶ that says
// there is more, every row of it is a click target, and a click opens it onto the whole output.
// The example is a git_diff_range call that arrived without its refs — a registered call with no
// target, whose output is free-form lines like any command's.
func TestTargetlessBlockStillCapsAndToggles(t *testing.T) {
	t.Parallel()

	const output = "warning: no refs given\n\nfirst line\nsecond line\nthird line\nfourth line"
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "git_diff_range", Arguments: []byte(`{}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: output}})

	if tr.entries[0].tool.Target != "" {
		t.Fatalf("target = %q; the example must be a call with no target", tr.entries[0].tool.Target)
	}
	if tr.entries[0].tool.collapsesToHeader {
		t.Fatal("git_diff_range carries the header-fold mark; it is task_list's alone")
	}
	collapsed := strings.Split(renderPlain(tr, 80), "\n")
	if want := 1 + collapsedBodyCap; len(collapsed) != want {
		t.Errorf("the collapsed block stands %d rows tall, want the budget's %d:\n%s",
			len(collapsed), want, strings.Join(collapsed, "\n"))
	}
	if want := "✦ Git Diff " + defaultGlyphs.collapsed; collapsed[0] != want {
		t.Errorf("header = %q, want %q — a capped targetless block says there is more on its header", collapsed[0], want)
	}
	marks := blockMarks(t, tr, 80)
	if len(marks) != len(collapsed) {
		t.Errorf("marks = %+v, want every one of the %d rows a toggle target", marks, len(collapsed))
	}

	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the targetless block to open")
	}
	expanded := renderPlain(tr, 80)
	for _, ln := range []string{"warning: no refs given", "third line", "fourth line"} {
		if !strings.Contains(expanded, ln) {
			t.Errorf("the open block:\n%s\nwant it to paint %q — the whole output", expanded, ln)
		}
	}
}

// TestGitStatusGroupsAndCounts pins git_status inside a grouped Tools block (issue
// git-status-never-groups): its target is the fixed words "working tree", so a run of read_file,
// grep, git_status and git_log calls is ONE umbrella counting all four, git_status a member row
// like the rest, and that member opened still shows the branch and the staged / unstaged /
// untracked report beneath it.
func TestGitStatusGroupsAndCounts(t *testing.T) {
	t.Parallel()

	const (
		width        = 80
		statusHead   = 2
		statusReport = "On branch main\n\nStaged (1):\n  a.go\n\nUnstaged (1):\n  b.go\n\nUntracked (1):\n  c.go"
	)
	build := func(t *testing.T) *transcript {
		t.Helper()
		tr := &transcript{}
		readCall(tr, "c1", "a.go", 1, 5, 0)
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "a.go:3: // TODO"}})
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c3", Tool: "git_status", Arguments: []byte(`{}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c3", Content: statusReport,
			Summary: domain.ChangedFiles{Staged: 1, Unstaged: 1, Untracked: 1}}})
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c4", Tool: "git_log", Arguments: []byte(`{}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c4", Content: "6fd6ff7 feat: x"}})
		return tr
	}

	header := umbrellaHeaderRow("Read 1 file, searched 1 pattern, checked git", glyphExpanded)
	readRow := groupMemberLine("  ┝ Read ⋯ 5 lines")
	grepRow := groupMemberLine("  ┝ Grep ⋯ a.go:3: // TODO")
	logRow := groupMemberLine("  ┕ Git Log ⋯ 6fd6ff7 feat: x")

	t.Run("collapsed: one umbrella counting all four", func(t *testing.T) {
		t.Parallel()

		want := strings.Join([]string{
			header,
			readRow,
			grepRow,
			groupMemberLine("  ┝ Git Status ⋯ 3 changed"),
			logRow,
		}, "\n")
		if got := renderPlain(build(t), width); got != want {
			t.Errorf("collapsed umbrella mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("type row open: git_status a member led by its target", func(t *testing.T) {
		t.Parallel()

		tr := build(t)
		if !tr.setTypeExpanded(statusHead, true) {
			t.Fatalf("setTypeExpanded(%d, true) = false; want git_status' type row inside the umbrella", statusHead)
		}
		want := strings.Join([]string{
			header,
			readRow,
			grepRow,
			leaderEdgeRow("  ┝ Git Status ⋯ 3 changed", glyphExpanded),
			groupMemberLine("  │ ┕ working tree ⋯ 3 changed"),
			logRow,
		}, "\n")
		if got := renderPlain(tr, width); got != want {
			t.Errorf("open type row mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})

	t.Run("member open: the branch and the report beneath it", func(t *testing.T) {
		t.Parallel()

		tr := build(t)
		if !tr.setTypeExpanded(statusHead, true) || !tr.setExpanded(statusHead, true) {
			t.Fatalf("setTypeExpanded/setExpanded(%d, true) = false; want the git_status member open", statusHead)
		}
		want := strings.Join([]string{
			header,
			readRow,
			grepRow,
			leaderEdgeRow("  ┝ Git Status ⋯ 3 changed", glyphExpanded),
			leaderEdgeRow("  │ ┕ working tree ⋯ 3 changed", glyphExpanded),
			"  │ │ On branch main",
			"  │ │",
			"  │ │ Staged (1):",
			"  │ │   a.go",
			"  │ │",
			"  │ │ Unstaged (1):",
			"  │ │   b.go",
			"  │ │",
			"  │ │ Untracked (1):",
			"  │ │   c.go",
			memberEdgeRow(t, "  │ │ ", promptSeeLess, width),
			logRow,
		}, "\n")
		if got := renderPlain(tr, width); got != want {
			t.Errorf("open member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
		}
	})
}

// The umbrella's header is its Group summary on a leader row of its own (the issue register's
// settled-tool-group-clutter): "✦ Read 3 files, ran 4 commands", the dots run to the fold's ▶/▼ at
// the block's edge, one verb phrase per kind of work summed across non-adjacent runs, in the order
// each first ran, its first word capitalised. Where the summary cannot stand beside one dot it
// spends detail by its fallback order — the phrases after the first two, then the diffstat, then
// the bare `Tools (N calls)` total, a failure count outliving each step — and below that the row
// clips like any leader row: always one line.
func TestUmbrellaHeader_ShowsTheGroupSummary(t *testing.T) {
	t.Parallel()

	// Read, Terminal ×2, Read, Terminal ×2, Read: five runs, two kinds of work.
	interleaved := func(t *testing.T) *transcript {
		t.Helper()
		tr := &transcript{}
		readCall(tr, "c1", "a.go", 1, 5, 0)
		runCall(tr, "c2", "go build", "built", 0)
		runCall(tr, "c3", "go vet", "clean", 0)
		readCall(tr, "c4", "b.go", 1, 9, 0)
		runCall(tr, "c5", "go test", "PASS", 0)
		runCall(tr, "c6", "go run .", "ran", 0)
		readCall(tr, "c7", "c.go", 1, 3, 0)
		return tr
	}
	headerAt := func(tr *transcript, width int, blink bool) string {
		return tr.renderView(newTheme(scheme.Default()), width, blink, breadcrumbHint).lines[0]
	}
	th := newTheme(scheme.Default())
	// The width at which "✦ " + text + " " + one dot + the indicator field fills the row exactly.
	fitWidth := func(text string) int {
		return th.measure.Width(defaultGlyphs.assistant+" ") + th.measure.Width(text) +
			leaderGap + leaderMinDots + groupIndicatorCells(th)
	}
	const summary = "Read 3 files, ran 4 commands"

	t.Run("words each kind of work across its runs, open and folded differing only in the glyph", func(t *testing.T) {
		t.Parallel()

		tr := interleaved(t)
		open := headerAt(tr, 80, false)
		if got, want := collapseLeader(strip(open)), umbrellaHeaderRow(summary, glyphExpanded); got != want {
			t.Errorf("open header = %q, want %q", got, want)
		}
		if !tr.setBlockFolded(0, true) {
			t.Fatal("setBlockFolded(0, true) = false; want the small umbrella folded on its own flag")
		}
		folded := headerAt(tr, 80, false)
		if want := strings.Replace(strip(open), glyphExpanded, defaultGlyphs.collapsed, 1); strip(folded) != want {
			t.Errorf("folded header = %q, want the open one with only its glyph flipped: %q", strip(folded), want)
		}
		if got := th.measure.Width(folded); got != 80 {
			t.Errorf("folded header is %d cells wide, want the block's 80 — the glyph at its edge", got)
		}
	})

	t.Run("orders phrases by first appearance and capitalises the first", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		runCall(tr, "c1", "go build", "built", 0)
		readCall(tr, "c2", "a.go", 1, 5, 0)
		runCall(tr, "c3", "go test", "PASS", 0)
		if got, want := collapseLeader(strip(headerAt(tr, 80, false))), umbrellaHeaderRow("Ran 2 commands, read 1 file", glyphExpanded); got != want {
			t.Errorf("header = %q, want %q", got, want)
		}
	})

	t.Run("keeps the whole summary down to one dot", func(t *testing.T) {
		t.Parallel()

		width := fitWidth(summary)
		want := defaultGlyphs.assistant + " " + summary + " " + defaultGlyphs.leaderDot + " " + glyphExpanded
		if got := strip(headerAt(interleaved(t), width, false)); got != want {
			t.Errorf("header at %d cells = %q, want %q", width, got, want)
		}
	})

	t.Run("falls back to the total one cell narrower", func(t *testing.T) {
		t.Parallel()

		width := fitWidth(summary) - 1
		got := strip(headerAt(interleaved(t), width, false))
		if want := defaultGlyphs.assistant + " Tools (7 calls) "; !strings.HasPrefix(got, want) {
			t.Errorf("header at %d cells = %q, want it to open %q", width, got, want)
		}
		if !strings.HasSuffix(got, defaultGlyphs.leaderDot+" "+glyphExpanded) || th.measure.Width(got) != width {
			t.Errorf("header at %d cells = %q, want its dots to run to the ▼ at the edge", width, got)
		}
	})

	t.Run("spends the later phrases first and keeps the failure count to the last", func(t *testing.T) {
		t.Parallel()

		// Read, Terminal (failed), Grep, List: four kinds of work, one of them gone wrong.
		tr := &transcript{}
		readCall(tr, "c1", "a.go", 1, 5, 0)
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal",
			Arguments: []byte(`{"command":"go test"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "FAIL", IsError: true}})
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c3", Tool: "grep",
			Arguments: []byte(`{"pattern":"TODO"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c3", Content: "a.go:1: TODO"}})
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c4", Tool: "list_dir",
			Arguments: []byte(`{"path":"."}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c4", Content: "a.go"}})

		const whole = "Read 1 file, ran 1 command, searched 1 pattern, listed 1 dir · 1 failed"
		if got, want := collapseLeader(strip(headerAt(tr, 100, false))), umbrellaHeaderRow(whole, glyphExpanded); got != want {
			t.Errorf("header at 100 cells = %q, want %q", got, want)
		}
		const shortened = "Read 1 file, ran 1 command and 2 more · 1 failed"
		if got, want := collapseLeader(strip(headerAt(tr, fitWidth(whole)-1, false))), umbrellaHeaderRow(shortened, glyphExpanded); got != want {
			t.Errorf("header one cell short of the whole = %q, want %q", got, want)
		}
		const counted = "Tools (4 calls) · 1 failed"
		if got, want := collapseLeader(strip(headerAt(tr, fitWidth(shortened)-1, false))), umbrellaHeaderRow(counted, glyphExpanded); got != want {
			t.Errorf("header one cell short of the shortened list = %q, want %q", got, want)
		}
		clipped := strip(headerAt(tr, fitWidth(counted)-1, false))
		if !strings.Contains(clipped, clipTail+" · 1 failed ") {
			t.Errorf("header one cell short of the count = %q, want the head clipped before the failure count", clipped)
		}
	})

	t.Run("clips the total below that, still one line", func(t *testing.T) {
		t.Parallel()

		width := fitWidth("Tools (7 calls)") - 1
		lines := interleaved(t).renderView(th, width, false, breadcrumbHint).lines
		got := strip(lines[0])
		if !strings.HasPrefix(got, defaultGlyphs.assistant+" Tools") || !strings.Contains(got, clipTail) {
			t.Errorf("header at %d cells = %q, want the total clipped with %q", width, got, clipTail)
		}
		if !strings.HasSuffix(got, defaultGlyphs.leaderDot+" "+glyphExpanded) || th.measure.Width(got) != width {
			t.Errorf("header at %d cells = %q, want one row ending in a dot and the ▼ at the edge", width, got)
		}
		if strings.Contains(strip(lines[1]), "Tools") {
			t.Errorf("the header spilled onto a second line: %q", strip(lines[1]))
		}
	})

	t.Run("paints the phrases in the tools header colour, the dots as a leader and the star with them", func(t *testing.T) {
		t.Parallel()

		if !th.toolLabel.GetBold() || th.toolLabel.GetForeground() != th.toolStar.GetForeground() {
			t.Fatalf("toolLabel is not the bold tools header colour: bold=%v fg=%v, want bold and %v",
				th.toolLabel.GetBold(), th.toolLabel.GetForeground(), th.toolStar.GetForeground())
		}
		got := headerAt(interleaved(t), 80, false)
		dots := strings.Count(strip(got), defaultGlyphs.leaderDot)
		want := th.toolStar.Render(defaultGlyphs.assistant) + th.toolHeader.Render(" ") + th.toolLabel.Render(summary) + " " +
			th.toolLeader.Render(strings.Repeat(defaultGlyphs.leaderDot, dots)) +
			strings.Repeat(" ", groupIndicatorGap) + th.toolIndicator.Render(glyphExpanded)
		if got != want {
			t.Errorf("styled header:\n got  %q\n want %q", got, want)
		}
		if colorActive(th) && strings.HasPrefix(got, detailTone(th, true).Render(defaultGlyphs.assistant)) {
			t.Errorf("the star took a row marker's detail tone: %q", got)
		}
	})

	t.Run("paints a failure count red and the count fallback as the header it always was", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		readCall(tr, "c1", "a.go", 1, 5, 0)
		runCall(tr, "c2", "go build", "built", 0)
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c3", Tool: "terminal",
			Arguments: []byte(`{"command":"go test"}`)}})
		tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c3", Content: "FAIL", IsError: true}})
		got := headerAt(tr, 80, false)
		if want := th.toolLabel.Render("Read 1 file, ran 2 commands") + th.errorText.Render(" · 1 failed") + " "; !strings.Contains(got, want) {
			t.Errorf("styled header %q does not carry %q", got, want)
		}
		counted := headerAt(tr, fitWidth("Read 1 file, ran 2 commands · 1 failed")-1, false)
		if want := th.toolLabel.Render(superGroupLabel) + " " + th.toolIndicator.Render("(3 calls)") +
			th.errorText.Render(" · 1 failed"); !strings.Contains(counted, want) {
			t.Errorf("styled count fallback %q does not carry %q", counted, want)
		}
	})

	t.Run("paints the diffstat's halves on the diff bands over the tools header colour, the gap before each unbanded", func(t *testing.T) {
		t.Parallel()

		summary := groupSummary{
			{text: "Edited 2 files", tone: groupTonePhrase},
			{text: " +18", tone: groupToneAdded},
			{text: " −5", tone: groupToneRemoved},
		}
		got := umbrellaHeaderPaint(th, summary)(summary.text())
		want := th.toolLabel.Render("Edited 2 files") +
			th.toolHeader.Render(" ") + th.diffAdded.Inherit(th.toolLabel).Render("+18") +
			th.toolHeader.Render(" ") + th.diffRemoved.Inherit(th.toolLabel).Render("−5")
		if got != want {
			t.Errorf("styled diffstat:\n got  %q\n want %q", got, want)
		}
		for _, band := range []lipgloss.Style{th.diffAdded.Inherit(th.toolLabel), th.diffRemoved.Inherit(th.toolLabel)} {
			if !band.GetBold() || band.GetForeground() != th.toolLabel.GetForeground() {
				t.Errorf("a diffstat half's base is not the bold tools header colour: bold=%v fg=%v",
					band.GetBold(), band.GetForeground())
			}
		}
		if clipped := umbrellaHeaderPaint(th, summary)("Edited 2 f…"); strip(clipped) != "Edited 2 f…" {
			t.Errorf("a clipped head painted as %q, want the survivor alone", strip(clipped))
		}
	})

	t.Run("paints a clipped phrase and any remainder past the summary in the tools header colour", func(t *testing.T) {
		t.Parallel()

		summary := groupSummary{
			{text: "Read 3 files", tone: groupTonePhrase},
			{text: " · 1 failed", tone: groupToneOutcome},
		}
		if got, want := umbrellaHeaderPaint(th, summary)("Read 3 f…"), th.toolLabel.Render("Read 3 f…"); got != want {
			t.Errorf("clipped phrase:\n got  %q\n want %q", got, want)
		}
		phrase := summary[:1]
		got := umbrellaHeaderPaint(th, phrase)("Read 3 files…")
		if want := th.toolLabel.Render("Read 3 files") + th.toolLabel.Render("…"); got != want {
			t.Errorf("remainder past the summary:\n got  %q\n want %q", got, want)
		}
	})

	t.Run("a live umbrella's blink blanks the star's cell alone", func(t *testing.T) {
		t.Parallel()

		tr := interleaved(t)
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c8", Tool: "terminal",
			Arguments: []byte(`{"command":"go test ./..."}`)}})
		settled, blinked := strip(headerAt(tr, 80, false)), strip(headerAt(tr, 80, true))
		if want := strings.Replace(settled, defaultGlyphs.assistant, " ", 1); blinked != want || settled == blinked {
			t.Errorf("blinked header = %q, want the settled %q with only its star blanked", blinked, settled)
		}
	})
}

// TestToolBlockStarPaintsInTheToolsHeaderColour pins every tool block's leading ✦ — an umbrella, a
// lone call, a Sub-agents group and a Skill group — to the scheme's tool-header colour (th.toolStar),
// with the header text after it painted exactly as before: the star alone takes the colour. The
// live blink's bare cell and a Firing's borrowed ⟳ are the two marks it must leave alone.
func TestToolBlockStarPaintsInTheToolsHeaderColour(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	star := th.toolStar.Render(defaultGlyphs.assistant)
	firstLine := func(t *testing.T, tr *transcript, blink bool) string {
		t.Helper()
		lines := tr.renderView(th, 80, blink, breadcrumbHint).lines
		if len(lines) == 0 {
			t.Fatal("the transcript rendered nothing at all")
		}
		return lines[0]
	}
	groupLabel := func(label string) string {
		return th.toolLabel.Render(label) + " " + th.toolIndicator.Render("(2)")
	}
	cases := []struct {
		name  string
		build func(tr *transcript)
		// after is the styled header text the star leads, exactly as it painted before the star
		// took its colour — save the umbrella's Group summary, which now paints in the tools header
		// colour (th.toolLabel) beside it.
		after string
	}{
		{
			name: "umbrella",
			build: func(tr *transcript) {
				readCall(tr, "c1", "main.go", 1, 154, 0)
				readCall(tr, "c2", "util.go", 1, 42, 0)
			},
			after: th.toolHeader.Render(" ") + th.toolLabel.Render("Read 2 files"),
		},
		{
			name:  "lone call",
			build: func(tr *transcript) { runCall(tr, "c1", "go test ./...", "PASS", 0) },
			after: " " + th.toolLabel.Render("Terminal"),
		},
		{
			name: "Sub-agents group",
			build: func(tr *transcript) {
				subAgentCall(tr, "s1", "survey the tests", 0)
				subAgentReport(tr, "s1", "done", 0)
				subAgentCall(tr, "s2", "survey the docs", 0)
				subAgentReport(tr, "s2", "done", 0)
			},
			after: " " + groupLabel("Sub-agents"),
		},
		{
			name: "Skill group",
			build: func(tr *transcript) {
				skillFetch(tr, "c1", "format Go", "<skill: Coding Standards>\nUse gofmt.\n</skill>\n")
				skillFetch(tr, "c2", "cut a release", "<skill: Brew Release>\nTag it.\n</skill>\n")
			},
			after: " " + groupLabel("Skill"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr := &transcript{}
			tc.build(tr)
			if got, want := firstLine(t, tr, false), star+tc.after; !strings.HasPrefix(got, want) {
				t.Errorf("header:\n got  %q\n want prefix %q", got, want)
			}
		})
	}

	t.Run("the star's colour is the scheme's tool-header colour in every scheme", func(t *testing.T) {
		t.Parallel()

		for _, name := range scheme.Discover("") {
			s, _ := scheme.Resolve(name, "")
			sth := newTheme(s)
			if got, want := sth.toolStar.GetForeground(), sth.toolLabel.GetForeground(); got != want {
				t.Errorf("scheme %q: toolStar fg = %v, want the tool-header colour %v", name, got, want)
			}
			if sth.toolStar.GetBold() {
				t.Errorf("scheme %q: toolStar is bold; the star takes the colour alone", name)
			}
		}
		if colorActive(th) && !strings.Contains(star, "\x1b[") {
			t.Errorf("the painted star %q carries no SGR", star)
		}
	})

	t.Run("the live blink still paints a bare cell", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{
			ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
		if got, want := firstLine(t, tr, true), " "+" "+th.toolLabel.Render("Terminal"); !strings.HasPrefix(got, want) {
			t.Errorf("blinked-out header:\n got  %q\n want prefix %q", got, want)
		}
		if got := firstLine(t, tr, false); !strings.HasPrefix(got, star+" ") {
			t.Errorf("live header at the settled phase:\n got  %q\n want the coloured star %q first", got, star)
		}
	})

	t.Run("a Firing's ⟳ paints as it always has", func(t *testing.T) {
		t.Parallel()

		if got, want := firstLine(t, firingBlock("the log is clean"), false), "⟳ "+th.toolLabel.Render("Schedule"); !strings.HasPrefix(got, want) {
			t.Errorf("Firing header:\n got  %q\n want prefix %q", got, want)
		}
	})
}
