package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
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

// A tool header shows "Label target" with no brackets, and the label alone carries the
// bold-orange style — baked in before the wrap, so the visible text is unaffected. The style
// assertion is a loose contains against the theme's own render rather than a byte-exact
// golden, so a lipgloss change cannot false-fail it; the guard above it catches the opposite
// failure, a toolLabel role that paints nothing at all.
func TestToolHeaderLabelStyled(t *testing.T) {
	th := newTheme()
	head := renderToolBlock(th, toolView{Label: "Read File", Target: "main.go"}, 80)[0]

	if got, want := ansi.Strip(head), "✦ Read File main.go"; got != want {
		t.Errorf("header text = %q; want %q (no brackets)", got, want)
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

// A run of one renders exactly as it always has — the target on the header line, its detail on a
// lone ┕ branch — so a single call is never dressed up as a group of one.
func TestRenderSingleCallUnchanged(t *testing.T) {
	tr := &transcript{}
	readCall(tr, "c1", "main.go", "1", "154", 0)

	th := newTheme()
	want := renderToolBlock(th, tr.entries[0].tool, 80)
	got := tr.renderLines(th, 80)
	if len(got) != len(want) {
		t.Fatalf("single call rendered %d lines; want %d (the plain single block)", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q; want %q (byte-identical to the single block)", i, got[i], want[i])
		}
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
				"✦ Read File a.go",
				"  ┕ 1 - 5",
				"",
				"✦ Run go test",
				"  ┝ ok",
				"  ┕ … +2 more lines",
				"",
				"✦ Read File b.go",
				"  ┕ 1 - 9",
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
				"✦ Read File a.go",
				"  ┕ 1 - 5",
				"",
				"· approval allow: read_file",
				"",
				"✦ Read File b.go",
				"  ┕ 1 - 9",
			},
		},
		{
			name: "a deeper sub-agent call",
			build: func(tr *transcript) {
				readCall(tr, "c1", "a.go", "1", "5", 0)
				readCall(tr, "c2", "b.go", "1", "9", 1)
			},
			want: []string{
				"✦ Read File a.go",
				"  ┕ 1 - 5",
				"",
				"│ ⤷ sub-agent",
				"",
				"│ ✦ Read File b.go",
				"│   ┕ 1 - 9",
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
				"✦ Read File a.go",
				"  ┕ 1 - 5",
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
