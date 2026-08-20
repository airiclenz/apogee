// toolshape_test.go is named for its subject, not for a source file: a deliberate exception to the
// coding-standards Go rule that a suite is named `{source}_test.go` (ratified 2026-08-15). Its
// subject is cross-file behaviour — the shape a tool call takes on screen — decided together by
// toolblock.go, toolbranch.go, render.go and toolview.go, each of which already has a suite of its
// own (the tool card's is toolpresent_test.go, which kept its name through the ADR 0043 split).

package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

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
