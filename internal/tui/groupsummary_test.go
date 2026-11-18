package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// groupSummaryWideBudget is a budget no case's whole sentence reaches, so the fallback never runs.
const groupSummaryWideBudget = 500

// settledCall is a finished member built the way the registry would label it, with the path the
// call named (none when path is empty).
func settledCall(t *testing.T, tool, path string) groupCall {
	t.Helper()
	return groupCall{view: memberView(t, tool, path), done: true}
}

// memberView is a hand-built view of a call to tool: the registry's label (the MCP split for a tool
// with no row) and the record's copy of a `path` argument.
func memberView(t *testing.T, tool, path string) toolView {
	t.Helper()
	tv := toolView{name: tool, Label: unregisteredLabel(tool)}
	if p, known := toolRegistry[tool]; known {
		tv.Label = p.label
	}
	if path != "" {
		raw, err := json.Marshal(map[string]string{groupPathArg: path})
		if err != nil {
			t.Fatalf("encode the path argument: %v", err)
		}
		tv.argsWire = raw
	}
	return tv
}

// diffedEdit is a finished edit whose slot carries the diffstat its apply recorded.
func diffedEdit(t *testing.T, path string, added, removed int) groupCall {
	t.Helper()
	call := settledCall(t, "edit_existing_file", path)
	call.view.Summary = branchSummary{stat: diffedStat(added, removed)}
	return call
}

// failedCall is a finished member whose slot carries the ordinary failure verdict.
func failedCall(t *testing.T, tool, path string) groupCall {
	t.Helper()
	call := settledCall(t, tool, path)
	call.view.Summary = namedSummary(detailLine{Text: erroredSummary})
	return call
}

func TestGroupSummary_Phrases(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	inFlight := settledCall(t, "read_file", "b.go")
	inFlight.done = false
	pathless := settledCall(t, "read_file", "")
	elided := settledCall(t, "read_file", "")
	elided.view.argsWire = json.RawMessage(`{"elided":"9000 bytes"}`)

	for _, tc := range []struct {
		name  string
		calls []groupCall
		want  string
	}{
		{
			name: "mixed kinds in the order each phrase first ran",
			calls: []groupCall{
				settledCall(t, "read_file", "a.go"), settledCall(t, "terminal", ""),
				settledCall(t, "read_file", "b.go"), settledCall(t, "grep", ""),
				settledCall(t, "list_dir", "internal"),
			},
			want: "Read 2 files, ran 1 command, searched 1 pattern, listed 1 dir",
		},
		{
			name: "three reads of one file read one file",
			calls: []groupCall{
				settledCall(t, "read_file", "a.go"), settledCall(t, "read_file", "a.go"),
				settledCall(t, "read_file", "./a.go"),
			},
			want: "Read 1 file",
		},
		{
			name:  "a plural count",
			calls: []groupCall{settledCall(t, "terminal", ""), settledCall(t, "python_exec", "")},
			want:  "Ran 2 commands",
		},
		{
			name:  "grep and find merge into one phrase",
			calls: []groupCall{settledCall(t, "grep", ""), settledCall(t, "find_files", "")},
			want:  "Searched 2 patterns",
		},
		{
			name: "a write and an edit of one file edited one file",
			calls: []groupCall{
				settledCall(t, "write_file", "a.go"), settledCall(t, "single_find_and_replace", "a.go"),
			},
			want: "Edited 1 file",
		},
		{
			name:  "a phrase with a call in flight takes its live verb",
			calls: []groupCall{settledCall(t, "read_file", "a.go"), inFlight, settledCall(t, "terminal", "")},
			want:  "Reading 2 files, ran 1 command",
		},
		{
			name:  "a distinct call with no readable path is its own item",
			calls: []groupCall{settledCall(t, "read_file", "a.go"), pathless, elided},
			want:  "Read 3 files",
		},
		{
			name:  "the git tools check git without a count",
			calls: []groupCall{settledCall(t, "git_status", ""), settledCall(t, "git_log", "")},
			want:  "Checked git",
		},
		{
			name:  "commits are counted",
			calls: []groupCall{settledCall(t, "git_commit", ""), settledCall(t, "git_commit", "")},
			want:  "Made 2 commits",
		},
		{
			name:  "an MCP tool is counted under its label",
			calls: []groupCall{settledCall(t, "github__search", ""), settledCall(t, "github__search", "")},
			want:  "github · search ×2",
		},
		{
			name: "an unlisted built-in is counted under its label",
			calls: []groupCall{
				settledCall(t, "read_file", "a.go"), settledCall(t, "diagnostics", "a.go"),
				settledCall(t, "diagnostics", "b.go"),
			},
			want: "Read 1 file, Diagnostics ×2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := buildGroupSummary(th, tc.calls, groupSummaryWideBudget)

			if got.text() != tc.want {
				t.Errorf("summary = %q, want %q", got.text(), tc.want)
			}
		})
	}
}

func TestGroupSummary_DiffstatSumsDiffedEditsAlone(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	write := settledCall(t, "write_file", "w.go")
	write.view.Summary = branchSummary{stat: countedStat(12, "lines")}
	calls := []groupCall{
		write,
		failedCall(t, "edit_existing_file", "x.go"),
		diffedEdit(t, "y.go", 3, 1),
		diffedEdit(t, "z.go", 2, 4),
	}

	got := buildGroupSummary(th, calls, groupSummaryWideBudget)

	if want := "Edited 4 files +5 −5 · 1 failed"; got.text() != want {
		t.Fatalf("summary = %q, want %q", got.text(), want)
	}
	tones := map[groupTone]string{}
	for _, seg := range got {
		tones[seg.tone] = seg.text
	}
	if tones[groupToneAdded] != " +5" || tones[groupToneRemoved] != " −5" || tones[groupToneOutcome] != " · 1 failed" {
		t.Errorf("segment tones = %v, want the diffstat halves and the failure apart", tones)
	}
}

func TestGroupSummary_FailedAndDeniedCountApartLiveAndReplayed(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	for _, tc := range []struct {
		name, tool, args, content, want string
	}{
		{
			name: "an approver denial", tool: "read_file", args: `{"path":"a.go"}`,
			content: "tool call denied by approver", want: "Read 1 file · 1 denied",
		},
		{
			name: "an approver denial carrying a hint", tool: "terminal", args: `{"command":"rm -rf x"}`,
			content: "tool call denied by approver — use delete_file", want: "Ran 1 command · 1 denied",
		},
		{
			name: "a reaction denial beneath an edit's own lines", tool: "edit_existing_file",
			args:    `{"path":"a.go","content":"package a\n"}`,
			content: "tool call denied by reaction guard-x", want: "Edited 1 file · 1 denied",
		},
		{
			name: "a tool's own failure", tool: "read_file", args: `{"path":"a.go"}`,
			content: "open a.go: no such file or directory", want: "Read 1 file · 1 failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			live := presentToolCall(domain.ToolCall{ID: "c1", Tool: tc.tool, Arguments: []byte(tc.args)},
				"", workspaceRoot{})
			live.enrichWithResult(domain.ToolResult{CallID: "c1", Content: tc.content, IsError: true},
				workspaceRoot{})
			e := entry{kind: entryToolCall, tool: live, done: true}
			stored := toWireEntry(&e, e.kind.persistedName())
			replayed := fromWireToolView(stored.Tool, stored.Done)

			for _, view := range []struct {
				name string
				tv   toolView
			}{{"live", live}, {"replayed", replayed}} {
				got := buildGroupSummary(th, []groupCall{{view: view.tv, done: true}}, groupSummaryWideBudget)
				if got.text() != tc.want {
					t.Errorf("%s summary = %q, want %q", view.name, got.text(), tc.want)
				}
			}
		})
	}
}

func TestGroupSummary_WidthFallback(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	calls := []groupCall{
		settledCall(t, "read_file", "a.go"),
		settledCall(t, "terminal", ""),
		settledCall(t, "grep", ""),
		diffedEdit(t, "e.go", 18, 5),
		failedCall(t, "terminal", ""),
	}
	rungs := []string{
		"Read 1 file, ran 2 commands, searched 1 pattern, edited 1 file +18 −5 · 1 failed",
		"Read 1 file, ran 2 commands and 2 more +18 −5 · 1 failed",
		"Read 1 file, ran 2 commands and 2 more · 1 failed",
		"Tools (5 calls) · 1 failed",
	}

	for i, want := range rungs {
		budget := th.measure.Width(want)
		if got := buildGroupSummary(th, calls, budget).text(); got != want {
			t.Errorf("rung %d at budget %d = %q, want %q", i, budget, got, want)
		}
		if i == 0 {
			continue
		}
		if got := buildGroupSummary(th, calls, th.measure.Width(rungs[i-1])-1).text(); got != want {
			t.Errorf("one cell short of rung %d = %q, want rung %d %q", i-1, got, i, want)
		}
	}

	t.Run("the clip spends the head before the failure count", func(t *testing.T) {
		t.Parallel()

		budget := th.measure.Width(rungs[len(rungs)-1]) - 1

		got := buildGroupSummary(th, calls, budget).text()

		if th.measure.Width(got) > budget || !strings.HasSuffix(got, " · 1 failed") ||
			!strings.Contains(got, clipTail) {
			t.Errorf("clipped summary = %q at budget %d, want a clipped head before \" · 1 failed\"", got, budget)
		}
	})

	t.Run("the failure count goes last of all", func(t *testing.T) {
		t.Parallel()

		budget := th.measure.Width(" · 1 failed")

		got := buildGroupSummary(th, calls, budget).text()

		if th.measure.Width(got) > budget || strings.Contains(got, groupFailedNoun) || !strings.HasPrefix(got, "T") {
			t.Errorf("narrowest summary = %q at budget %d, want the head alone, clipped", got, budget)
		}
	})
}
