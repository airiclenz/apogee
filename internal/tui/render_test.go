package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Sticky-to-top offset drift guard (P2.7)
// ----------------------------------------------------------------------------

// TestWrappedOffsetMatchesViewport pins wrappedOffset to the viewport's own soft-wrap
// accounting: summing every physical line's wrapped height must equal the viewport's
// TotalLineCount for the same lines at the same width. If the viewport's calculateLine ever
// changes (or a border/gutter is added, breaking the maxWidth == Width assumption), this
// fails — and the sticky-to-top SetYOffset would silently mis-pin without it.
func TestWrappedOffsetMatchesViewport(t *testing.T) {
	const width = 10
	lines := []string{
		"short",                 // 1 row
		strings.Repeat("a", 25), // 3 rows (ceil 25/10)
		"",                      // 1 row (a blank line still occupies a row)
		"thirteen char",         // 2 rows (13 cols)
		strings.Repeat("b", 10), // 1 row (exactly the width)
		strings.Repeat("c", 11), // 2 rows
	}

	vp := viewport.New()
	vp.SoftWrap = true
	vp.SetWidth(width)
	vp.SetHeight(5)
	vp.SetContentLines(lines)

	if got, want := wrappedOffset(lines, width), vp.TotalLineCount(); got != want {
		t.Errorf("wrappedOffset = %d; want the viewport's TotalLineCount %d (soft-wrap drift)", got, want)
	}
}

// A blank line and a zero-width input are each a single row, not zero, matching the
// viewport's max(1, …) floor.
func TestWrappedOffsetFloors(t *testing.T) {
	if got := wrappedOffset([]string{"", "", ""}, 10); got != 3 {
		t.Errorf("three blank lines = %d rows; want 3 (each floors to one)", got)
	}
	if got := wrappedOffset([]string{""}, 0); got != 1 {
		t.Errorf("zero width = %d rows; want 1 (width floored to one, no divide-by-zero)", got)
	}
}

// ----------------------------------------------------------------------------
// Sub-agent framing reflow safety (P3.14)
// ----------------------------------------------------------------------------

// railedWidth floors a deeply-nested block's usable width at one column so the wrapper never
// divides by zero, even when the rail gutters consume more than the whole terminal width.
func TestRailedWidthFloors(t *testing.T) {
	if got := railedWidth(80, 0); got != 80 {
		t.Errorf("railedWidth(80, 0) = %d; want 80 (depth 0 takes no gutter)", got)
	}
	if got := railedWidth(3, 5); got != 1 {
		t.Errorf("railedWidth(3, 5) = %d; want 1 (floored, not negative)", got)
	}
}

// A Depth > 0 block renders at a tiny and a zero width without panicking (the acceptance's
// "reflow at small sizes doesn't panic"); the framed text is still produced.
func TestSubAgentReflowAtSmallWidths(t *testing.T) {
	for _, width := range []int{0, 1, 2, 3, 6} {
		tr := feed(domain.MessageEvent{
			EventBase: domain.EventBase{Depth: 2},
			Text:      "a deeply nested sub-agent message that must wrap hard",
		})
		lines := tr.renderLines(newTheme(), width) // must not panic at any width
		if len(lines) == 0 {
			t.Errorf("width %d produced no lines", width)
		}
	}
}

// ----------------------------------------------------------------------------
// The tool header's label styling
// ----------------------------------------------------------------------------

// A tool header shows the label alone — no brackets and, now that the block shape is uniform, no
// target either — and that label carries the bold-orange style, baked in before the wrap so the
// visible text is unaffected. The style assertion is a loose contains against the theme's own
// render rather than a byte-exact golden, so a lipgloss change cannot false-fail it; the guard
// below it catches the opposite failure, a toolLabel role that paints nothing at all.
func TestToolHeaderLabelStyled(t *testing.T) {
	th := newTheme()
	block := renderToolBlock(th, []toolView{{Label: "Read File", Target: "main.go"}}, 80)
	head := block[0]

	if got, want := ansi.Strip(head), "✦ Read File"; got != want {
		t.Errorf("header text = %q; want %q (no brackets, and never a target)", got, want)
	}
	if got, want := ansi.Strip(block[1]), "  ┕ main.go"; got != want {
		t.Errorf("branch text = %q; want %q (the target leads the branch)", got, want)
	}
	styled := th.toolLabel.Render("Read File")
	if styled == "Read File" {
		t.Fatal("the toolLabel role renders no escape sequence; the header would be unstyled")
	}
	if !strings.Contains(head, styled) {
		t.Errorf("header %q does not carry the styled label %q", head, styled)
	}
}

// ----------------------------------------------------------------------------
// Grouped same-label tool calls (tool-call layout item 4)
// ----------------------------------------------------------------------------

// readCall folds a read_file call and its "showing lines from-to" result into tr, so a grouping
// test reads as the batch of reads it is meant to render.
func readCall(tr *transcript, id, path, from, to string, depth int) {
	base := domain.EventBase{Depth: depth}
	tr.apply(domain.ToolCallEvent{
		EventBase: base,
		Call:      domain.ToolCall{ID: id, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)},
	})
	tr.apply(domain.ToolResultEvent{
		EventBase: base,
		Result: domain.ToolResult{
			CallID:  id,
			Content: "[File: " + path + ", " + to + " lines total, showing lines " + from + "-" + to + "]\n…",
		},
	})
}

// A batch of reads folds into one block: a single ✦ Read File header, ┝ ┝ ┕ rails, and every
// target padded to the widest one so the detail column lines up — the shape layout.md sketches.
func TestRenderGroupsConsecutiveSameLabelCalls(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "README.md", "1", "154", 0)
	readCall(tr, "c2", "TODO.md", "1", "408", 0)
	readCall(tr, "c3", "ISSUES.md", "1", "8", 0)

	want := strings.Join([]string{
		"✦ Read File",
		"  ┝ README.md 1 - 154",
		"  ┝ TODO.md   1 - 408",
		"  ┕ ISSUES.md 1 - 8",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("grouped block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A grouped run inside a sub-agent is framed like any other block: the ⤷ label opens the run once
// and every line of the group — header and branches alike — carries the │ rail gutter.
func TestRenderGroupsInsideSubAgent(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "a.go", "1", "5", 1)
	readCall(tr, "c2", "bb.go", "1", "9", 1)

	want := strings.Join([]string{
		"│ ⤷ sub-agent",
		"",
		"│ ✦ Read File",
		"│   ┝ a.go  1 - 5",
		"│   ┕ bb.go 1 - 9",
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
		"✦ Edit File",
		"  ┝ a.go  replaced text in a.go",
		"  ┕ bb.go applied 2 replacements to bb.go",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("shared-label group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A member whose result has not landed shows its bare padded target and nothing after it; when the
// result folds in, the whole block repaints with that member's detail in the aligned column.
func TestRenderGroupWithInFlightMember(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "README.md", "1", "154", 0)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "read_file", Arguments: []byte(`{"path":"TODO.md"}`)}})

	want := strings.Join([]string{
		"✦ Read File",
		"  ┝ README.md 1 - 154",
		"  ┕ TODO.md",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("in-flight member mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "[File: TODO.md, 408 lines total, showing lines 1-408]\n…"}})
	want = strings.Join([]string{
		"✦ Read File",
		"  ┝ README.md 1 - 154",
		"  ┕ TODO.md   1 - 408",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("re-rendered group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A lone call renders in the very shape a group does — label-only header, target leading the
// branch — so the block does not reshape when a second call joins it. That is the whole point of
// the uniform layout, and the ┕-with-no-padding is what "a group of one pads to itself" means.
func TestRenderSingleCallSharesTheGroupShape(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "main.go", "1", "154", 0)

	want := strings.Join([]string{
		"✦ Read File",
		"  ┕ main.go 1 - 154",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("single-call block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// …and a second call joins it by adding a line, not by moving the first one's target.
	readCall(tr, "c2", "a-much-longer-name.go", "1", "9", 0)
	want = strings.Join([]string{
		"✦ Read File",
		"  ┝ main.go               1 - 154",
		"  ┕ a-much-longer-name.go 1 - 9",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("grown block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A body-only call keeps the same header and branch: nothing rides beside the target, and the
// body lays out beneath it at the branch marker's width — those lines are not ┝/┕ branches of
// their own, because only calls are (layout.md's Run sketch).
func TestRenderMultiDetailStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: "ok   apogee/internal/tui   0.412s\nok   apogee/internal/agent   1.203s\nPASS\n",
	}})

	want := strings.Join([]string{
		"✦ Run",
		"  ┕ go test ./...",
		"    ok   apogee/internal/tui   0.412s",
		"    … +2 more lines",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("multi-detail block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A diff call is the summary-and-body shape layout.md sketches: the diffstat rides the branch
// beside the path and the coloured body hangs beneath it. The body keeps its red/green
// colouring, which — together with having a body at all — is why it can never fold into a group.
func TestRenderDiffDetailStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "- a removed line\n+ an added line"}})

	want := strings.Join([]string{
		"✦ View Diff",
		"  ┕ main.go +1 -1",
		"    - a removed line",
		"    + an added line",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("diff block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	th := newTheme()
	lines := tr.renderLines(th, 80)
	if got, want := lines[2], th.diffRemoved.Render("    - a removed line"); got != want {
		t.Errorf("removed line = %q; want the diffRemoved style %q", got, want)
	}
	if got, want := lines[3], th.diffAdded.Render("    + an added line"); got != want {
		t.Errorf("added line = %q; want the diffAdded style %q", got, want)
	}
}

// The layout.md sketch, rendered: a two-line change shows "+2 -2" on the branch beside the path
// with the diff body beneath it, and the diffstat line itself stays plain — only the body is
// coloured, so the branch reads like every other tool's summary.
func TestRenderDiffMatchesLayoutSketch(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: "- a code line that has been removed\n- a second removed line\n+ a new code line\n+ a second new line",
	}})

	want := strings.Join([]string{
		"✦ View Diff",
		"  ┕ main.go +2 -2",
		"    - a code line that has been removed",
		"    - a second removed line",
		"    + a new code line",
		"    + a second new line",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("diff sketch mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	th := newTheme()
	if got, want := tr.renderLines(th, 80)[1], th.toolDetail.Render("  ┕ main.go +2 -2"); got != want {
		t.Errorf("diffstat branch = %q; want the plain toolDetail style %q", got, want)
	}
}

// A diff whose body is capped still names the whole change on its branch: the diffstat counts
// every line, the body stops at diffDetailCap with its remainder count.
func TestRenderDiffStatSurvivesTheBodyCap(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c1",
		Content: strings.TrimSuffix(strings.Repeat("+ added\n", diffDetailCap+5), "\n"),
	}})

	lines := strings.Split(renderPlain(tr, 80), "\n")
	if got, want := lines[1], "  ┕ main.go +25 -0"; got != want {
		t.Errorf("capped diff branch = %q, want %q (the stat spans the whole diff)", got, want)
	}
	if got, want := lines[len(lines)-1], "    … +5 more lines"; got != want {
		t.Errorf("capped diff body ends %q, want %q", got, want)
	}
}

// A command whose output is a single line puts that line where every other one-line outcome goes:
// on the branch, beside the command. Nothing hangs beneath — a one-line result is a summary, not a
// body, and only a command with more to say than one line reshapes into the Run block above.
func TestRenderOneLineOutputRidesTheBranch(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "abc1234\n"}})

	want := strings.Join([]string{
		"✦ Run",
		"  ┕ git rev-parse HEAD abc1234",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("one-line Run mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// …and because a one-line result leaves the branch line free of a body, consecutive one-line
// commands still fold into one block with their outputs aligned past the widest command — the
// grouping a body would (correctly) break.
func TestRenderGroupsOneLineOutputCalls(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"git rev-parse HEAD"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "abc1234"}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"pwd"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "/workspace/repos/apogee"}})

	want := strings.Join([]string{
		"✦ Run",
		"  ┝ git rev-parse HEAD abc1234",
		"  ┕ pwd                /workspace/repos/apogee",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("one-line Run group mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A call whose result has not landed shows the bare target on its branch and nothing after it —
// the same line it will keep once the detail arrives beside it.
func TestRenderInFlightStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}})

	want := strings.Join([]string{
		"✦ Read File",
		"  ┕ main.go",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("in-flight block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// The one shape with no target line: an unregistered tool has nothing to lead a branch with, so
// the header stands alone and its verbatim pretty-printed arguments are themselves the ┝/┕
// branches — nothing about what the model asked for is hidden.
func TestRenderNoTargetStandalone(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "mcp_thing", Arguments: []byte(`{"a":1}`)}})

	want := strings.Join([]string{
		"✦ mcp_thing",
		"  ┝ {",
		`  ┝   "a": 1`,
		"  ┕ }",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("targetless block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// A targetless call has no branch line for its summary to ride, so the outcome closes the branch
// list instead of vanishing: an unregistered tool's arguments, then the "error: …" it earned.
func TestRenderNoTargetKeepsItsSummary(t *testing.T) {
	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "mcp_thing", Arguments: []byte(`{"a":1}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c1", Content: "no such server", IsError: true}})

	want := strings.Join([]string{
		"✦ mcp_thing",
		"  ┝ {",
		`  ┝   "a": 1`,
		"  ┝ }",
		"  ┕ error: no such server",
	}, "\n")
	if got := renderPlain(tr, 80); got != want {
		t.Errorf("targetless error block mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// Anything between two same-label calls ends the run, and so does a call that cannot fit one
// aligned branch line. Each case pins the whole scrollback, so a break shows as the separate
// blocks it must produce.
func TestRenderGroupBreakers(t *testing.T) {
	cases := []struct {
		name  string
		build func(tr *transcript)
		want  []string
	}{
		{
			name: "a multi-detail call between two reads",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", "1", "5", 0)
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "ok\nPASS\ndone"}})
				readCall(tr, "c3", "b.go", "1", "9", 0)
			},
			want: []string{
				"✦ Read File",
				"  ┕ a.go 1 - 5",
				"",
				"✦ Run",
				"  ┕ go test",
				"    ok",
				"    … +2 more lines",
				"",
				"✦ Read File",
				"  ┕ b.go 1 - 9",
			},
		},
		{
			name: "an approval note between two reads",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", "1", "5", 0)
				tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "read_file"}, Decision: domain.ApprovalAllow})
				readCall(tr, "c2", "b.go", "1", "9", 0)
			},
			want: []string{
				"✦ Read File",
				"  ┕ a.go 1 - 5",
				"",
				"· approval allow: read_file",
				"",
				"✦ Read File",
				"  ┕ b.go 1 - 9",
			},
		},
		{
			name: "a deeper sub-agent call",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", "1", "5", 0)
				readCall(tr, "c2", "b.go", "1", "9", 1)
			},
			want: []string{
				"✦ Read File",
				"  ┕ a.go 1 - 5",
				"",
				"│ ⤷ sub-agent",
				"",
				"│ ✦ Read File",
				"│   ┕ b.go 1 - 9",
			},
		},
		{
			name: "a call with no target",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", "1", "5", 0)
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c2", Tool: "read_file"}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "c2", Content: "[File: ?, 1 lines total, showing lines 1-1]"}})
			},
			want: []string{
				"✦ Read File",
				"  ┕ a.go 1 - 5",
				"",
				"✦ Read File",
				"  ┕ 1 - 1",
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
}

// ----------------------------------------------------------------------------
// The whole-transcript layout golden (tool-call layout item 5)
// ----------------------------------------------------------------------------

// TestTranscriptLayoutGolden pins the whole rendered scrollback of one realistic mixed session —
// a user prompt, narration the model padded with a trailing "\n\n", a batch of reads, a Run whose
// output hangs beneath its command, a diff whose "+2 -2" rides its branch over a coloured body, an
// approval note, and a sub-agent read — as an exact line sequence, blank lines included. It is the
// backstop across the layout changes rather than a test of any one of them: the blank-line hygiene
// shows as the single empty row between every block, the bracketless bold-orange label as the
// header text, the grouping as the one aligned Read File block, and the uniform shape as the fact
// that every header here — grouped, standalone, railed — is a label and nothing else, with the
// target always leading a branch and the outcome split into the summary beside it and the body
// beneath. A regression in any of them changes this golden, and the golden doubles as the living
// example of what layout.md sketches.
func TestTranscriptLayoutGolden(t *testing.T) {
	tr := &transcript{}
	tr.addUser("read the docs, then run the tests", nil)
	tr.apply(domain.TokenEvent{Text: "Reading the docs first."})
	tr.apply(domain.TokenEvent{Text: "\n\n"}) // the model's own padding: trimmed at commit
	readCall(tr, "c1", "README.md", "1", "154", 0)
	readCall(tr, "c2", "TODO.md", "1", "408", 0)
	readCall(tr, "c3", "ISSUES.md", "1", "8", 0)
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c4", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c4",
		Content: "ok   apogee/internal/tui     0.412s\nok   apogee/internal/agent   1.203s\nPASS\n",
	}})
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c5", Tool: "view_diff", Arguments: []byte(`{"path":"main.go"}`)}})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID:  "c5",
		Content: "  func main() {\n-     fmt.Println(\"old\")\n-     return\n+     fmt.Println(\"new\")\n+     os.Exit(0)\n  }",
	}})
	tr.apply(domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow})
	readCall(tr, "c6", "main.go", "1", "154", 1)

	want := strings.Join([]string{
		"❯ read the docs, then run the tests",
		"",
		"✦ Reading the docs first.",
		"",
		"✦ Read File",
		"  ┝ README.md 1 - 154",
		"  ┝ TODO.md   1 - 408",
		"  ┕ ISSUES.md 1 - 8",
		"",
		"✦ Run",
		"  ┕ go test ./...",
		"    ok   apogee/internal/tui     0.412s",
		"    … +2 more lines",
		"",
		"✦ View Diff",
		"  ┕ main.go +2 -2",
		"      func main() {",
		"    -     fmt.Println(\"old\")",
		"    -     return",
		"    +     fmt.Println(\"new\")",
		"    +     os.Exit(0)",
		"      }",
		"",
		"· approval allow: terminal",
		"",
		"│ ⤷ sub-agent",
		"",
		"│ ✦ Read File",
		"│   ┕ main.go 1 - 154",
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

// ----------------------------------------------------------------------------
// The one-time start-up box (version-command-and-startup-box plan, item 3)
// ----------------------------------------------------------------------------

// The start-up box renders the logo and the host / model / version rows inside a rounded card
// that reuses the prompt box's border glyphs but drops its black fill. The four assertions are
// the item's acceptance made mechanical: (a) the logo art is present, (b) the three session facts
// are present, (c) the rounded corner runes match the prompt box, and (d) the card carries none of
// the black-background SGR the input box emits — the "same characters, no black background".
func TestRenderStartupBox(t *testing.T) {
	th := newTheme()
	v := startupView{
		Logo:    strings.TrimRight(apogeeLogo, "\n"),
		Host:    "test-host:1111",
		Model:   "gpt-oss-20b",
		Version: "v9.9.9-test",
	}
	raw := strings.Join(renderStartupBox(th, v, 80), "\n")
	plain := ansi.Strip(raw)

	// (a) a distinctive fragment of the block-art wordmark survives into the card.
	if !strings.Contains(plain, "████▄ ▄███▄") {
		t.Errorf("startup box does not carry the logo art:\n%s", plain)
	}
	// (b) the three session facts, each with its dim label, are present.
	for _, want := range []string{"host", v.Host, "model", v.Model, "version", v.Version} {
		if !strings.Contains(plain, want) {
			t.Errorf("startup box missing %q:\n%s", want, plain)
		}
	}
	// (c) the rounded corners match the prompt box's RoundedBorder glyphs.
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(plain, corner) {
			t.Errorf("startup box missing rounded corner %q:\n%s", corner, plain)
		}
	}
	// (d) none of the black-background SGR the input box paints. Extract it from a real inputBorder
	// render (its leading SGR sets the black background), so the check tracks whatever colour
	// profile lipgloss uses rather than a hard-coded escape.
	probe := lipgloss.NewStyle().Background(colBlack).Render("x")
	if !strings.Contains(probe, "\x1b") {
		t.Fatal("the black-background probe rendered no escape; the colour profile hides the SGR this test relies on")
	}
	blackBG := probe[:strings.IndexByte(probe, 'm')+1] // the leading SGR, up to and including its 'm'
	if strings.Contains(raw, blackBG) {
		t.Errorf("startup box carries the input box's black-background SGR %q — it must be transparent", blackBG)
	}
}
