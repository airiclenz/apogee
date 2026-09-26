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
	"github.com/airiclenz/apogee/internal/tools"
)

// TestToolRegistryCoversEveryBuiltInTool pins the registry to the tool set in both directions. A
// built-in tool with no row falls to the raw-name fallback — its snake_case id for a label and its
// arguments dumped verbatim — which is how git_show shipped without a card; walking
// tools.KnownToolNames (the build's whole catalogue, default-off tools included) fails the day a
// new tool lands without one. The reverse walk catches a row whose tool was renamed or retired: a
// key no tool answers to is a card nothing can ever reach. The host-delegate rows (ask_user,
// load_skill, present_document) need no exemption: the catalogue carries those tools by
// construction even where a Driver leaves them unwired.
func TestToolRegistryCoversEveryBuiltInTool(t *testing.T) {
	t.Parallel()

	known := tools.KnownToolNames()
	knownSet := make(map[string]bool, len(known))
	for _, name := range known {
		knownSet[name] = true
	}

	for _, name := range known {
		if _, ok := toolRegistry[name]; !ok {
			t.Errorf("built-in tool %q has no toolRegistry row; its card would fall to the raw-name fallback", name)
		}
	}
	for name := range toolRegistry {
		if !knownSet[name] {
			t.Errorf("toolRegistry row %q names no built-in tool (tools.KnownToolNames)", name)
		}
	}
}

// TestGrepTarget pins what a grep row LEADS with. The pattern alone answers "what was searched
// for" but never "where", and the two searches a reader has to tell apart in a group — the whole
// workspace and one file — differ in nothing else, so the path the call scoped itself to and the
// include glob that narrowed it ride the target as qualifiers, in that order. A path of "." is the
// search every grep is until it says otherwise: it is dropped rather than spelled, and dropping it
// must not leave the glob orphaned behind a stray separator. A call scoped through the tool's
// `paths` array is scoped all the same: its entries are the qualifier, ", "-joined the way grep's
// own scope header spells them, with a `path` listed first when the call gave both.
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
		{
			name: "a paths-scoped search names every path, joined",
			args: map[string]any{"pattern": "KeyMsg", "paths": []any{"internal/tui", "cmd/apogee"}},
			want: "KeyMsg · internal/tui, cmd/apogee",
		},
		{
			name: "path and paths together list the path first",
			args: map[string]any{"pattern": "KeyMsg", "path": "internal/tui", "paths": []any{"cmd/apogee", "docs"}},
			want: "KeyMsg · internal/tui, cmd/apogee, docs",
		},
		{
			name: "paths and glob chain in that order",
			args: map[string]any{"pattern": "KeyMsg", "paths": []any{"internal/tui", "cmd/apogee"}, "include": "*.go"},
			want: "KeyMsg · internal/tui, cmd/apogee · *.go",
		},
		{
			name: "a paths array naming only the workspace root adds nothing",
			args: map[string]any{"pattern": "KeyMsg", "paths": []any{".", ""}, "include": "*.go"},
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
// than a row opening on a separator. The `paths` case pins the SHARED helper only — find_files
// itself has no paths parameter (internal/tools/find_files.go carries `path` alone), so it is the
// scope reader both search tools share that is under test there, never the tool's schema.
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
		{
			name: "a paths array reads through the shared scope helper (find_files itself has no paths parameter)",
			args: map[string]any{"pattern": "*.go", "path": "internal/tui", "paths": []any{"cmd/apogee"}},
			want: "*.go · internal/tui, cmd/apogee",
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
// path — registry lookup, sanitize and the display seam — onto the row itself; a call scoped
// through `paths` alone reaches the row with its joined qualifier by the same path.
func TestGrepBranchRowShowsTheSearchedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call string
		want string
	}{
		{
			name: "a path-scoped call",
			call: `{"pattern":"KeyMsg","path":"internal/tui/model.go"}`,
			want: "KeyMsg · internal/tui/model.go",
		},
		{
			name: "a paths-scoped call",
			call: `{"pattern":"KeyMsg","paths":["internal/tui","cmd/apogee"]}`,
			want: "KeyMsg · internal/tui, cmd/apogee",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := &transcript{}
			tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Tool: "grep",
				Arguments: []byte(tt.call)}})

			got := renderPlain(tr, 80)

			if !strings.Contains(got, tt.want) {
				t.Errorf("grep row does not name the searched scope %q:\n--- got ---\n%s", tt.want, got)
			}
		})
	}
}

// The three head lines the engine writes on a bounded delegation's result, in the spelling that
// ships since the body became `[engine summary]` + fold + `[delegate's closing report]` + text
// (internal/agent's stepCapResultFormat, tokenCapResultFormat, timeCapResultFormat): each still
// reads as its bound through delegationBoundHead — the prefix the recogniser anchors on did not
// move — and the body sub-heads beneath it never do, because the head is matched at the START.
func TestDelegationBoundHeadReadsEveryBoundsHead(t *testing.T) {
	t.Parallel()

	const body = "\n[engine summary]\nThe delegate read a.txt and b.txt; c.txt is unread.\n\n[delegate's closing report]\nI had read two files so far"
	cases := []struct {
		name string
		head string
		want string
	}{
		{"step cap", "[delegate stopped at its step cap (3 steps); partial result — engine summary and closing report follow]", "capped at its step cap"},
		{"token budget", "[delegate stopped at its token budget (20000000 tokens); partial result — engine summary and closing report follow]", "capped at its token budget"},
		{"time limit", "[delegate stopped at its time limit (2h0m); partial result — engine summary and closing report follow]", "capped at its time limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if !delegationBoundHead.MatchString(tc.head) {
				t.Errorf("delegationBoundHead does not match %q", tc.head)
			}
			if got := delegationVerdict(tc.head + body); got != tc.want {
				t.Errorf("delegationVerdict = %q, want %q", got, tc.want)
			}
		})
	}
	if got := delegationVerdict("The child said:" + body); got != delegationDoneVerdict {
		t.Errorf("a body sub-head with no bound head above it reads %q, want %q", got, delegationDoneVerdict)
	}
}

// The NON-REPORT variant of each bound's head (internal/agent's stepCapNonReportFormat and
// siblings, plan 2026-09-18 - 00 item 3): the engine writes it when a capped child's closing text
// reads as tool output or narration rather than a report, worded by shape — `tool-call markup`, `a
// file dump`, `a grep dump`, `narration of its next step`. The prefix delegationBoundHead anchors on
// is unchanged, so every variant still reads as its bound and never as `done`; the narration
// sub-head beneath it is body, matched by nothing.
func TestDelegationBoundHeadReadsTheNonReportVariants(t *testing.T) {
	t.Parallel()

	const body = "\n[engine summary]\nThe delegate read a.txt and b.txt; c.txt is unread.\n\n[delegate's closing report — read as narration, not a finding]\nLet me look at c.txt next."
	bounds := []struct {
		bound string
		want  string
	}{
		{"step cap (3 steps)", "capped at its step cap"},
		{"token budget (20000000 tokens)", "capped at its token budget"},
		{"time limit (2h0m)", "capped at its time limit"},
	}
	shapes := []string{"tool-call markup", "a file dump", "a grep dump", "narration of its next step"}
	for _, b := range bounds {
		for _, shape := range shapes {
			head := "[delegate stopped at its " + b.bound + "; no closing report — the delegate's last reply reads as " + shape + ", not a finding; engine summary follows]"
			t.Run(b.want+"/"+shape, func(t *testing.T) {
				t.Parallel()

				if !delegationBoundHead.MatchString(head) {
					t.Errorf("delegationBoundHead does not match %q", head)
				}
				if got := delegationVerdict(head + body); got != b.want {
					t.Errorf("delegationVerdict = %q, want %q", got, b.want)
				}
			})
		}
	}
}

// The HUMAN's stop (ADR 0086 D4) words its own verdict, `stopped by you`, off the two results the
// engine gives a stopped delegation: the non-error partial result under stoppedResultHead, and the
// error-shaped unstarted result of a pooled child stopped before it ran. It outranks every other
// reading — the fold beneath the head is body, so a narrating closing report under it is no
// no-report — keeps the steering cell, and is anchored at the START, so a child that merely printed
// the head's words mid-report is still `done`. An engine bound's head, a session recorded before
// the stop existed included, still reads as the bound, worded `capped`.
func TestDelegationVerdictReadsTheHumansStop(t *testing.T) {
	t.Parallel()

	const fold = "\n[engine summary]\nThe delegate read a.txt; b.txt is unread.\n\n[delegate's closing report]\nLet me now read b.txt."
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"a stopped run", delegationStoppedHead + fold, delegationStoppedVerdict},
		{"a stopped, steered run", delegationStoppedHead + fold + envelopeSteeredOne, delegationStoppedVerdict + " · steered by 1 message"},
		{"a queued stop", delegationStoppedQueuedContent, delegationStoppedVerdict},
		{"the head quoted mid-report", "I found this line:\n" + delegationStoppedHead, delegationDoneVerdict},
		{"an old session's capped head", envelopeCapMarker + fold, delegationCappedVerdict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := delegationVerdict(tc.content); got != tc.want {
				t.Errorf("delegationVerdict(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
	if delegationCappedVerdict != "capped at its step cap" {
		t.Errorf("delegationCappedVerdict = %q, want the human-facing %q", delegationCappedVerdict, "capped at its step cap")
	}
	if delegationStoppedVerdict != "stopped by you" {
		t.Errorf("delegationStoppedVerdict = %q, want %q", delegationStoppedVerdict, "stopped by you")
	}
}

// Neither stopped text is PROMOTED into the slot, however short: the slot is the stopped verdict's,
// as it is the no-report verdict's, and the text lays out as a body beneath it.
func TestDelegationDetailDoesNotPromoteAStoppedText(t *testing.T) {
	t.Parallel()

	for _, content := range []string{delegationStoppedHead, delegationStoppedQueuedContent} {
		out := delegationDetail(content)
		if out.Summary.Text != "" {
			t.Errorf("delegationDetail(%q) promoted %q into the slot; want it laid out as a body", content, out.Summary.Text)
		}
		if len(out.Details) == 0 {
			t.Errorf("delegationDetail(%q) laid out no body", content)
		}
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
// reads as a delegation that ended without a report (delegationEndedWithoutReport).
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

	t.Run("the no-report marker ends without a report", func(t *testing.T) {
		t.Parallel()

		if got := delegationVerdict("[delegate returned no report]"); got != delegationNoReportVerdict {
			t.Errorf("verdict = %q, want %q", got, delegationNoReportVerdict)
		}
	})
}

// A delegation that reached its own boundary with nothing to report — the no-report marker, or a
// closing text the shared classifier (floor.IsNonReport) reads as narration of a next step, a pasted
// file or a grep dump — words its slot `ended without a report` rather than `done` (plan
// "2026-09-24 - 01", item 6). The bound head outranks it, a real report keeps `done`, the steering
// cell still rides it, and the engine's own body notes beneath the text are read through, exactly
// as the bound head reads through them.
func TestDelegationVerdictReadsANonReport(t *testing.T) {
	t.Parallel()

	const (
		note  = "\n" + agent.SeatFallbackNote
		clamp = "\n[max_steps 120 requested; the configured cap is 80 — 80 applied]"
	)
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"one line of narration", "Let me now read X.", delegationNoReportVerdict},
		{"the no-report marker", delegationNoReportMarker, delegationNoReportVerdict},
		{"a multi-line narration", "I read a.go and b.go.\nNext I will read c.go.", delegationNoReportVerdict},
		{"a pasted file", "[File: a.go, 40 lines total, showing lines 1-3]\npackage a\n\nfunc A() {}", delegationNoReportVerdict},
		{"a grep dump", "a.go:3:func A()\nb.go:9:func B()\nc.go:1:package c", delegationNoReportVerdict},
		{"a steered narration", "Let me now read X." + envelopeSteeredOne, delegationNoReportVerdict + " · steered by 1 message"},
		{"a fallen-back, clamped narration", "Let me now read X." + note + clamp, delegationNoReportVerdict},
		{"a fallen-back no-report marker", delegationNoReportMarker + note, delegationNoReportVerdict},
		{"a real report", "Found 4 gaps\nin the suite", delegationDoneVerdict},
		{"a one-line report", "all clear", delegationDoneVerdict},
		{"a fallen-back real report", "all clear" + note + clamp, delegationDoneVerdict},
		{"a capped narration", envelopeCapMarker + "\nLet me now read X.", delegationCappedVerdict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := delegationVerdict(tc.content); got != tc.want {
				t.Errorf("delegationVerdict(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

// The no-report verdict is an OUTCOME ENVELOPE, like the bound head: it takes the slot ahead of a
// one-line narration the child closed on, which is never promoted into it (delegationDetail), and
// the line lays out as the body instead. A one-line REPORT is promoted exactly as before.
func TestDelegationDetailNeverPromotesANonReport(t *testing.T) {
	t.Parallel()

	t.Run("a one-line narration is body, not the slot", func(t *testing.T) {
		t.Parallel()

		out := delegationDetail("Let me now read X.")
		if out.Summary.Text != "" {
			t.Errorf("summary = %q, want none — the verdict takes the slot", out.Summary.Text)
		}
		if lines := out.Details; len(lines) != 1 || lines[0].Text != "Let me now read X." {
			t.Errorf("details = %+v, want the narration as the one body line", lines)
		}
	})

	t.Run("a one-line report is still promoted", func(t *testing.T) {
		t.Parallel()

		out := delegationDetail("all clear")
		if out.Summary.Text != "all clear" || !out.Summary.quoted {
			t.Errorf("summary = %+v, want the report promoted into the slot, quoted", out.Summary)
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
