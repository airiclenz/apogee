package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
)

// TestGrepTarget pins what a grep row LEADS with. The pattern alone answers "what was searched
// for" but never "where", and the two searches a reader has to tell apart in a group — the whole
// workspace and one file — differ in nothing else, so the path the call scoped itself to and the
// include glob that narrowed it ride the target as qualifiers, in that order. A path of "." is the
// search every grep is until it says otherwise: it is dropped rather than spelled, and dropping it
// must not leave the glob orphaned behind a stray separator.
func TestGrepTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "an unscoped search is the pattern alone",
			args: map[string]any{"pattern": "KeyMsg"},
			want: "KeyMsg",
		},
		{
			name: "a path-scoped search names the path",
			args: map[string]any{"pattern": "KeyMsg", "path": "internal/tui/model.go"},
			want: "KeyMsg · internal/tui/model.go",
		},
		{
			name: "an include glob qualifies on its own",
			args: map[string]any{"pattern": "KeyMsg", "include": "*.go"},
			want: "KeyMsg · *.go",
		},
		{
			name: "path and glob chain in that order",
			args: map[string]any{"pattern": "KeyMsg", "path": "internal/tui", "include": "*.go"},
			want: "KeyMsg · internal/tui · *.go",
		},
		{
			name: "the workspace root itself adds nothing",
			args: map[string]any{"pattern": "KeyMsg", "path": "."},
			want: "KeyMsg",
		},
		{
			name: "dropping the workspace root leaves no stray separator",
			args: map[string]any{"pattern": "KeyMsg", "path": ".", "include": "*.go"},
			want: "KeyMsg · *.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := grepTarget(tt.args); got != tt.want {
				t.Errorf("grepTarget(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// TestFindFilesTarget pins the same shape for the other search tool: the name pattern leads, the
// path the walk was scoped to qualifies it, and "." — the walk the tool does by default — is left
// unsaid. find_files has no include glob; a call that gives only a path is the path alone rather
// than a row opening on a separator.
func TestFindFilesTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "an unscoped walk is the pattern alone",
			args: map[string]any{"pattern": "*.go"},
			want: "*.go",
		},
		{
			name: "a path-scoped walk names the path",
			args: map[string]any{"pattern": "*.go", "path": "internal/tui"},
			want: "*.go · internal/tui",
		},
		{
			name: "the workspace root itself adds nothing",
			args: map[string]any{"pattern": "*.go", "path": "."},
			want: "*.go",
		},
		{
			name: "a path with no pattern stands alone",
			args: map[string]any{"path": "internal/tui"},
			want: "internal/tui",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := findFilesTarget(tt.args); got != tt.want {
				t.Errorf("findFilesTarget(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// The target extractor is only half the claim: what the human reads is the painted branch. This
// folds one scoped grep call into a transcript and asserts the scope survives the whole presenting
// path — registry lookup, sanitize and the display seam — onto the row itself.
func TestGrepBranchRowShowsTheSearchedPath(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "grep",
		Arguments: []byte(`{"pattern":"KeyMsg","path":"internal/tui/model.go"}`)}})

	got := renderPlain(tr, 80)

	if !strings.Contains(got, "KeyMsg · internal/tui/model.go") {
		t.Errorf("grep row does not name the searched path:\n--- got ---\n%s", got)
	}
}

// The ADR 0069 routing note — the line a delegation's result gains when its call asked for the
// Sub-agent server and ran on the session server instead — is APPENDED to the result BODY. Both
// recognisers that word a delegation's slot read the envelope from a fixed end of that body:
// delegationStepCapHead at the START, delegationFailure from the first line. So a fallen-back
// result must classify exactly as the plain one does, and only the text beneath the head may change.
func TestDelegationRecognisersReadThroughTheRoutingNote(t *testing.T) {
	t.Parallel()

	const note = "\n" + agent.SeatFallbackNote

	t.Run("a capped run is still capped", func(t *testing.T) {
		t.Parallel()

		plain := envelopeCapMarker + "\nI had read two files so far"

		if got, want := delegationVerdict(plain+note), delegationVerdict(plain); got != want {
			t.Errorf("fallen-back verdict = %q, want the plain result's %q", got, want)
		}
	})

	t.Run("a capped run that was steered still says both", func(t *testing.T) {
		t.Parallel()

		body := envelopeCapMarker + "\nI had read two files so far"

		got := delegationVerdict(body + note + envelopeSteeredOne)

		if want := delegationVerdict(body + envelopeSteeredOne); got != want {
			t.Errorf("fallen-back steered verdict = %q, want the plain result's %q", got, want)
		}
	})

	t.Run("a whole run still reads done", func(t *testing.T) {
		t.Parallel()

		if got := delegationVerdict("Found 4 gaps\nin the suite" + note); got != delegationDoneVerdict {
			t.Errorf("fallen-back verdict = %q, want %q", got, delegationDoneVerdict)
		}
	})

	t.Run("a faulted run keeps its cause", func(t *testing.T) {
		t.Parallel()

		word, output, ok := delegationFailure(envelopeFaultLine + note + envelopeSteeredOne)

		wantWord, _, _ := delegationFailure(envelopeFaultLine + envelopeSteeredOne)
		if !ok {
			t.Fatal("delegationFailure declined a steered fallen-back result")
		}
		if word != wantWord {
			t.Errorf("fallen-back failure line = %q, want the plain result's %q", word, wantWord)
		}
		// The note is not swallowed: it lands in the body beneath the summary, which is where the
		// reader who wants to know where the work ran finds it.
		if !strings.Contains(output, "the sub-agents server was unavailable") {
			t.Errorf("fallen-back failure output = %q, want it to carry the routing note", output)
		}
	})
}

// TestToolRegistryPresentsTheTaskListCall pins the whole card a task_list call draws (ADR 0072):
// the registry's own label and verb rather than the raw-name fallback a dynamic tool falls to, no
// target — its one argument IS the list, the same reason git_status carries none — and the list the
// tool echoed back laid out beneath the branch with the open count in the outcome slot.
//
// The body is asserted to carry the done row UNNUMBERED, which is the prose half of the numbering
// rule (TestFileContentBodiesAreNumbered, whose walk reaches every registry entry): a task list is
// the model's own text and sits on no file's lines, so a gutter here would claim a position that
// does not exist.
func TestToolRegistryPresentsTheTaskListCall(t *testing.T) {
	t.Parallel()

	const rendered = "Task list — yours to maintain; call task_list with the COMPLETE list to update it (2 open, 1 done):\n" +
		"[✔] read the plan\n" +
		"[ ] write the code\n" +
		"[ ] run the tests"

	call := domain.ToolCall{
		ID:   "1",
		Tool: "task_list",
		Arguments: []byte(`{"tasks":[{"text":"read the plan","done":true},` +
			`{"text":"write the code"},{"text":"run the tests"}]}`),
	}

	tv := presentToolCall(call, "", workspaceRoot{})
	if tv.Label != "Task List" {
		t.Errorf("label = %q, want %q — the raw name is the fallback a dynamic tool takes", tv.Label, "Task List")
	}
	if want := "updating the task list"; tv.Verb != want {
		t.Errorf("verb = %q, want %q", tv.Verb, want)
	}
	if tv.Target != "" {
		t.Errorf("target = %q, want none — the list itself is the target", tv.Target)
	}

	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: rendered}, workspaceRoot{})

	if want := "2 open"; tv.Summary.Text != want {
		t.Errorf("outcome slot = %q, want %q", tv.Summary.Text, want)
	}
	body := tv.Details.all()
	var carriesDoneRow bool
	for i, line := range body {
		if line.Gutter != "" {
			t.Errorf("body row %d (%q) carries the gutter %q, want none — a task list sits on no file's lines",
				i, line.Text, line.Gutter)
		}
		if strings.Contains(line.Text, "[✔] read the plan") {
			carriesDoneRow = true
		}
	}
	if !carriesDoneRow {
		t.Errorf("body = %v, want it to carry the ticked row the tool rendered", body)
	}
}

// TestToolRegistryTaskListStatCountsOpenRows pins the readings the outcome slot makes off the list
// the tool echoed back. It counts only rows wearing a marker, so a task whose own text opens with a
// bracket is never miscounted, and it DECLINES where there is no list to count — a cleared list, a
// refusal, an error result — which leaves the tool's own sentence in the slot rather than a `0 open`
// that would read as a finished job.
func TestToolRegistryTaskListStatCountsOpenRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result domain.ToolResult
		want   string
		wantOK bool
	}{
		{
			name:   "counts the open rows and not the done ones",
			result: domain.ToolResult{Content: "Task list — … (2 open, 1 done):\n[✔] one\n[ ] two\n[ ] three"},
			want:   "2 open",
			wantOK: true,
		},
		{
			name:   "a single open task is not pluralised into another word",
			result: domain.ToolResult{Content: "Task list — … (1 open, 0 done):\n[ ] the only one"},
			want:   "1 open",
			wantOK: true,
		},
		{
			name:   "a list with everything ticked reads zero open",
			result: domain.ToolResult{Content: "Task list — … (0 open, 2 done):\n[✔] one\n[✔] two"},
			want:   "0 open",
			wantOK: true,
		},
		{
			name:   "a task's own bracket is text, not a row marker",
			result: domain.ToolResult{Content: "Task list — … (1 open, 0 done):\n[ ] fix [ ] in the parser"},
			want:   "1 open",
			wantOK: true,
		},
		{
			name:   "a cleared list has no rows to count",
			result: domain.ToolResult{Content: ""},
			wantOK: false,
		},
		{
			name:   "a refusal keeps its prose floor",
			result: domain.ToolResult{Content: "the task list holds at most 40 tasks; that call carried 41", IsError: true},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := toolRegistry["task_list"].stat(tc.result)
			if ok != tc.wantOK {
				t.Fatalf("stat ok = %v, want %v (got %q)", ok, tc.wantOK, got.spell())
			}
			if ok && got.spell() != tc.want {
				t.Errorf("stat = %q, want %q", got.spell(), tc.want)
			}
		})
	}
}
