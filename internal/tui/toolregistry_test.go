package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tasklist"
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
// delegationBoundHead at the START, delegationFailure from the first line. So a fallen-back
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

// The two result shapes item 9 of plan 2026-09-14 - 03 turns into ERROR results — a spawn-named
// `output_path` the child never wrote, and a closing text that is unparsed tool-call markup — are
// worded by the existing failure layer from their first line, exactly as a faulted child's is: the
// engine's line is the summary, the child's text is the body beneath it, and the steering cell
// still rides the line. Its third shape, the no-report marker, is a non-error result the verdict
// recogniser does not know, so the slot reads the ordinary `done`.
func TestDelegationValidationFaultsReadThroughTheErrorSlot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		head string
	}{
		{"a missing output", "sub-agent ended without writing out/report.md; its last text follows:"},
		{"an unparsed markup reply", "sub-agent reply is unparsed tool-call markup, not a report"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			word, output, ok := delegationFailure(tc.head + "\nI read two files so far" + envelopeSteeredOne)

			if !ok {
				t.Fatal("delegationFailure declined a steered validation fault")
			}
			if want := tc.head + " · steered by 1 message"; word != want {
				t.Errorf("failure line = %q, want %q", word, want)
			}
			if output != "I read two files so far" {
				t.Errorf("failure output = %q, want the child's text beneath the summary", output)
			}
		})
	}

	t.Run("the no-report marker reads done", func(t *testing.T) {
		t.Parallel()

		if got := delegationVerdict("[delegate returned no report]"); got != delegationDoneVerdict {
			t.Errorf("verdict = %q, want %q", got, delegationDoneVerdict)
		}
	})
}

// taskListRendered is the block internal/tasklist renders for the three-task fixture every task_list
// test here shares — 2 open, 1 done — spelled from the package's own header format rather than
// respelled, so a reworded header cannot leave these tests pinning a sentence the tool no longer
// writes.
var taskListRendered = fmt.Sprintf(tasklist.HeaderFormat, 2, 1) + "\n" +
	"[✔] read the plan\n" +
	"[ ] write the code\n" +
	"[ ] run the tests"

// taskListCall is the call that fixture answers.
var taskListCall = domain.ToolCall{
	ID:   "1",
	Tool: "task_list",
	Arguments: []byte(`{"tasks":[{"text":"read the plan","done":true},` +
		`{"text":"write the code"},{"text":"run the tests"}]}`),
}

// TestToolRegistryPresentsTheTaskListCall pins the whole card a task_list call draws (ADR 0072):
// the registry's own label and verb rather than the raw-name fallback a dynamic tool falls to, no
// target — its one argument IS the list, the same reason git_status carries none — the list the
// tool echoed back laid out beneath the branch, and the header counting the list's done rows over
// all of them while the outcome slot stays blank (the ratified call of plan "2026-09-14 - 00").
//
// The body is asserted to carry the done row UNNUMBERED, which is the prose half of the numbering
// rule (TestFileContentBodiesAreNumbered, whose walk reaches every registry entry): a task list is
// the model's own text and sits on no file's lines, so a gutter here would claim a position that
// does not exist.
//
// The PAINT is pinned whole as well, at 80 columns, in both fold states (item 2 of that plan): the
// collapsed card is its counted header alone under a ▶, the open card that header under a ▼ with
// every task row painted in order beneath it and no footer, no `N open` row closes the list — the
// header's `(1/3)` is the one count the card wears — and neither the model-facing header sentence
// nor a `+N more lines` marker is anywhere on it.
func TestToolRegistryPresentsTheTaskListCall(t *testing.T) {
	t.Parallel()

	rendered, call := taskListRendered, taskListCall

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
	if tv.count != "" {
		t.Errorf("count = %q before the result landed, want none — nothing has been counted yet", tv.count)
	}

	tv.enrichWithResult(domain.ToolResult{CallID: "1", Content: rendered}, workspaceRoot{})

	if want := "1/3"; tv.count != want {
		t.Errorf("header count = %q, want %q", tv.count, want)
	}
	if tv.Summary.Text != "" {
		t.Errorf("outcome slot = %q, want it blank — the header's count is the card's one reading", tv.Summary.Text)
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

	tr := &transcript{}
	tr.apply(domain.ToolCallEvent{Call: call})
	tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: rendered}})

	assertTaskListPaint(t, tr)
}

// assertTaskListPaint is the header-folding shape the three-task fixture paints, shared by the
// live card and its replayed record so the two are held to ONE picture: collapsed, the header
// counting done rows over all rows and nothing beneath it; open, that header over every row and no
// footer; and no `N open` row closing the list in either state. It toggles tr to reach the second
// picture and leaves it open.
func assertTaskListPaint(t *testing.T, tr *transcript) {
	t.Helper()
	collapsed := renderPlain(tr, 80)
	if want := "✦ Task List (1/3) " + glyphCollapsed; collapsed != want {
		t.Errorf("task_list paints collapsed:\n%s\nwant its counted header alone:\n%s", collapsed, want)
	}
	if !tr.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false; want the task_list card to open")
	}
	painted := renderPlain(tr, 80)
	want := []string{
		"✦ Task List (1/3) " + glyphExpanded,
		"  ┝ [✔] read the plan",
		"  ┝ [ ] write the code",
		"  ┕ [ ] run the tests",
	}
	if got := strings.Split(painted, "\n"); !reflect.DeepEqual(got, want) {
		t.Errorf("task_list paints open:\n%s\nwant the header and every row, no footer:\n%s", painted, strings.Join(want, "\n"))
	}
	for _, state := range []string{collapsed, painted} {
		if strings.Contains(state, "2 open") {
			t.Errorf("paint = %q, want no `N open` row — the header's count is the one count the card wears", state)
		}
		if strings.Contains(state, tasklist.Fence) {
			t.Errorf("paint = %q, want the model-facing header sentence stripped (display-side; the result text is the model's)", state)
		}
		if strings.Contains(state, "more line") {
			t.Errorf("paint = %q, want no +N more lines marker — the header's count says what the fold holds", state)
		}
	}
}

// TestToolRegistryTaskListReplaysCollapsesToHeader holds the replayed record to the live card's
// paint: the header-fold mark is not on the wire and is re-derived off the retained name at decode
// (fromWireToolView, the same registry field presentToolCall reads), so a session resumed with a
// task list on screen folds and opens it exactly as the run that wrote it did.
func TestToolRegistryTaskListReplaysCollapsesToHeader(t *testing.T) {
	t.Parallel()

	live := &transcript{}
	live.apply(domain.ToolCallEvent{Call: taskListCall})
	live.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: taskListRendered}})

	blob, err := encodeTranscript(live)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	entries, err := decodeTranscript(blob)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	replayed := &transcript{entries: entries}

	if !replayed.entries[0].tool.collapsesToHeader {
		t.Errorf("the decoded record lost the header-fold mark; want it re-derived from the registry off %q", replayed.entries[0].tool.name)
	}
	if live, back := renderPlain(live, 80), renderPlain(replayed, 80); live != back {
		t.Errorf("the replayed record paints collapsed:\n%s\nwant the live paint:\n%s", back, live)
	}
	assertTaskListPaint(t, replayed)
	if !live.toggleExpanded(0) {
		t.Fatal("toggleExpanded(0) = false on the live card")
	}
	if live, back := renderPlain(live, 80), renderPlain(replayed, 80); live != back {
		t.Errorf("the replayed record paints open:\n%s\nwant the live paint:\n%s", back, live)
	}
}

// TestToolRegistryTaskListReplaysAnOldOpenCountRecord pins the one-way read of a record a session
// saved BEFORE the header count existed: its retained Summary is the `2 open` stat that used to
// close the list as a ┕ row. The decoder discards that wording where the registry entry counts
// (fromWireToolView), and words the header's `(1/3)` off the decoded rows instead — so a resumed
// card counts itself once, in the header, and never twice.
func TestToolRegistryTaskListReplaysAnOldOpenCountRecord(t *testing.T) {
	t.Parallel()

	blob, err := session.EncodeTranscript([]session.Entry{{
		Kind:   session.EntryKindToolCall,
		CallID: "1",
		Done:   true,
		Tool: &session.ToolView{
			Label: "Task List",
			Verb:  "updating the task list",
			Name:  "task_list",
			Summary: session.BranchSummary{
				DetailLine: session.DetailLine{Text: "2 open"},
				Stat:       &session.StatValue{Counted: true, N: 2, NounForOne: "open", NounForMany: "open"},
			},
			Details: []session.DetailLine{
				{Text: "[✔] read the plan"},
				{Text: "[ ] write the code"},
				{Text: "[ ] run the tests"},
			},
		},
	}})
	if err != nil {
		t.Fatalf("EncodeTranscript: %v", err)
	}
	entries, err := decodeTranscript(blob)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	replayed := &transcript{entries: entries}

	if got := replayed.entries[0].tool.Summary.Text; got != "" {
		t.Errorf("replayed outcome slot = %q, want the old record's `2 open` discarded — the header count supersedes it", got)
	}
	assertTaskListPaint(t, replayed)
}

// TestToolRegistryTaskListReplayKeepsAVerdictAndAPromotedLine pins the two slot readings the
// decoder does NOT discard on a counting card, because the live card keeps them too: the `error`
// verdict a failed call wears (absorbFailure), and a one-line refusal the tool printed and the
// presenter promoted onto the branch (outputDetail), which the blank stat never takes back. Neither
// record carries a task row, so neither wears a count.
func TestToolRegistryTaskListReplayKeepsAVerdictAndAPromotedLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		summary session.BranchSummary
		details []session.DetailLine
		want    []string
	}{
		{
			name:    "a failed call keeps its verdict",
			summary: session.BranchSummary{DetailLine: session.DetailLine{Text: "error"}},
			details: []session.DetailLine{{Text: "the task list holds at most 40 tasks"}},
			want:    []string{"✦ Task List", "  ┝ the task list holds at most 40 tasks", "  ┕ error"},
		},
		{
			name:    "a promoted refusal keeps its line",
			summary: session.BranchSummary{DetailLine: session.DetailLine{Text: "task list cleared"}, Quoted: true},
			want:    []string{"✦ Task List", "  ┕ task list cleared"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			blob, err := session.EncodeTranscript([]session.Entry{{
				Kind: session.EntryKindToolCall, CallID: "1", Done: true,
				Tool: &session.ToolView{Label: "Task List", Name: "task_list", Summary: tc.summary, Details: tc.details},
			}})
			if err != nil {
				t.Fatalf("EncodeTranscript: %v", err)
			}
			entries, err := decodeTranscript(blob)
			if err != nil {
				t.Fatalf("decodeTranscript: %v", err)
			}
			replayed := &transcript{entries: entries}

			if got := replayed.entries[0].tool.count; got != "" {
				t.Errorf("count = %q on a row-less record, want none", got)
			}
			if got := strings.Split(renderPlain(replayed, 80), "\n"); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("replayed paint:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// TestToolRegistryTaskListCountsDoneOverTotal pins the readings the header count makes off the
// list the tool echoed back: done rows over every row. It counts only rows wearing a marker, so a
// task whose own text opens with a bracket is never miscounted, and it DECLINES where there is no
// list to count — a cleared list, a fence-only result, a refusal, an error result — which leaves
// the header the bare `✦ Task List` rather than a `0/0` that would read as a finished job.
func TestToolRegistryTaskListCountsDoneOverTotal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result domain.ToolResult
		want   string
		wantOK bool
	}{
		{
			name:   "one of three done",
			result: domain.ToolResult{Content: fmt.Sprintf(tasklist.HeaderFormat, 2, 1) + "\n[✔] one\n[ ] two\n[ ] three"},
			want:   "1/3",
			wantOK: true,
		},
		{
			name:   "a list with everything ticked",
			result: domain.ToolResult{Content: fmt.Sprintf(tasklist.HeaderFormat, 0, 3) + "\n[✔] one\n[✔] two\n[✔] three"},
			want:   "3/3",
			wantOK: true,
		},
		{
			name:   "a list with nothing ticked",
			result: domain.ToolResult{Content: fmt.Sprintf(tasklist.HeaderFormat, 2, 0) + "\n[ ] one\n[ ] two"},
			want:   "0/2",
			wantOK: true,
		},
		{
			name:   "a task's own bracket is text, not a row marker",
			result: domain.ToolResult{Content: fmt.Sprintf(tasklist.HeaderFormat, 1, 0) + "\n[ ] fix [ ] in the parser"},
			want:   "0/1",
			wantOK: true,
		},
		{
			name:   "a cleared list has no rows to count",
			result: domain.ToolResult{Content: ""},
			wantOK: false,
		},
		{
			name:   "a fence-only result has no rows to count",
			result: domain.ToolResult{Content: fmt.Sprintf(tasklist.HeaderFormat, 0, 0)},
			wantOK: false,
		},
		{
			name:   "a refusal keeps the bare label",
			result: domain.ToolResult{Content: "the task list holds at most 40 tasks; that call carried 41", IsError: true},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := toolRegistry["task_list"].count(splitLines(tc.result.Content))
			if ok != tc.wantOK {
				t.Fatalf("count ok = %v, want %v (got %q)", ok, tc.wantOK, got)
			}
			if ok && got != tc.want {
				t.Errorf("count = %q, want %q", got, tc.want)
			}

			tv := presentToolCall(taskListCall, "", workspaceRoot{})
			tv.enrichWithResult(tc.result, workspaceRoot{})
			if tv.count != got {
				t.Errorf("enriched view count = %q, want the hook's %q", tv.count, got)
			}
			if !tc.wantOK {
				tr := &transcript{}
				tr.apply(domain.ToolCallEvent{Call: taskListCall})
				tr.apply(domain.ToolResultEvent{Result: tc.result})
				header := strings.Split(renderPlain(tr, 80), "\n")[0]
				if strings.Contains(header, "(") {
					t.Errorf("header = %q, want the plain `✦ Task List` — nothing to count", header)
				}
			}
		})
	}
}
