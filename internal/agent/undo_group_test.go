package agent

// The undo journal's Exchange grouping (ADR 0051). `/undo` reverts one INSTRUCTION's worth of
// writes, and the engine's whole contribution to that promise is where it calls BeginGroup: a
// new user input opens a group, an interjection joins the group already open (ADR 0025 — it
// commits mid-Exchange), and every funnel write in between lands in that one group — a
// delegated child's writes included, since a sub-agent shares its parent's journal and opens no
// group of its own. These tests drive the real loop with the real write_file and sub_agent
// tools, so they also pin the threading that gets the journal from the engine to the funnel.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/snapshot"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/undo"
)

// stepExpecting runs one Step and fails unless it ended with the expected status.
func stepExpecting(t *testing.T, a *Agent, want domain.StepStatus, label string) {
	t.Helper()

	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	if res.Status != want {
		t.Fatalf("%s status = %q, want %q", label, res.Status, want)
	}
}

// TestUndoGroupsFollowTheExchange: two Exchanges, three writes, and an interjection in the
// middle of the first one. The top undo step must hold the SECOND Exchange's write alone, and
// the step under it must hold both writes of the first — the interjection's included.
func TestUndoGroupsFollowTheExchange(t *testing.T) {
	// The workspace root is symlink-resolved so dispatch's in-workspace classification and the
	// journal's recorded paths agree on a box whose temp dir is reached through a symlink.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp root: %v", err)
	}

	cfg := configWithTools(&recordingSink{}, tools.NewWriteFile(root))
	cfg.Mode = domain.ModeAllowEdits // auto-approves Apogee's own workspace-scoped writes
	cfg.WorkspaceDir = root

	responder := &captureAllResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "write_file", `{"path":"a.txt","content":"a"}`),
		toolCallScript("c2", "write_file", `{"path":"b.txt","content":"b"}`),
		contentScript("first instruction done"),
		toolCallScript("c3", "write_file", `{"path":"c.txt","content":"c"}`),
		contentScript("second instruction done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// Exchange 1: the user's instruction, one write, an interjection, a second write.
	if err := a.Submit(domain.UserInput{Text: "write a"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	stepExpecting(t, a, domain.StatusTurnComplete, "exchange 1 turn 0")
	if err := a.Interject(domain.UserInput{Text: "also write b"}); err != nil {
		t.Fatalf("Interject: %v", err)
	}
	stepExpecting(t, a, domain.StatusTurnComplete, "exchange 1 turn 1")
	stepExpecting(t, a, domain.StatusExchangeComplete, "exchange 1 turn 2")

	// Exchange 2: a second instruction, one write.
	if err := a.Submit(domain.UserInput{Text: "write c"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	stepExpecting(t, a, domain.StatusTurnComplete, "exchange 2 turn 0")
	stepExpecting(t, a, domain.StatusExchangeComplete, "exchange 2 turn 1")

	top, ok := a.journal.Preview()
	if !ok {
		t.Fatal("the run left no undo step")
	}
	if top.Ordinal != 2 {
		t.Errorf("top step ordinal = %d, want 2 (one group per Exchange)", top.Ordinal)
	}
	assertChangedPaths(t, top.Changes, []string{filepath.Join(root, "c.txt")}, "the second Exchange")

	if _, err := a.journal.Revert(); err != nil {
		t.Fatalf("Revert: %v", err)
	}

	under, ok := a.journal.Preview()
	if !ok {
		t.Fatal("the first Exchange left no undo step")
	}
	if under.Ordinal != 1 {
		t.Errorf("next step ordinal = %d, want 1", under.Ordinal)
	}
	assertChangedPaths(t, under.Changes,
		[]string{filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")},
		"the first Exchange and its interjection")
}

// assertChangedPaths compares a previewed step's paths, in order, against what the Exchange
// should have grouped.
func assertChangedPaths(t *testing.T, changes []undo.Change, want []string, what string) {
	t.Helper()

	if len(changes) != len(want) {
		t.Fatalf("%s grouped %d changes, want %d: %+v", what, len(changes), len(want), changes)
	}
	for i, change := range changes {
		if change.Path != want[i] {
			t.Errorf("%s change %d path = %q, want %q", what, i, change.Path, want[i])
		}
	}
}

// TestDelegationWritesJoinTheParentGroup: a delegation is work the human asked for in the
// CURRENT Exchange, so a child's writes belong to that Exchange's undo step. This drives a real
// sub_agent call whose child writes a file of its own and asks for ONE group holding both files
// — the parent's write and the child's, in the order they happened. A child journalling into an
// instance of its own would leave its file out of every preview the human can reach, and a child
// opening a GROUP of its own would split one instruction across two `/undo` presses.
func TestDelegationWritesJoinTheParentGroup(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp root: %v", err)
	}

	cfg := configWithTools(&recordingSink{}, tools.NewSubAgent(), tools.NewWriteFile(root))
	cfg.Mode = domain.ModeAllowEdits // auto-approves the workspace-scoped writes at both depths
	cfg.WorkspaceDir = root

	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "write_file", `{"path":"parent.txt","content":"p"}`),
		toolCallScript("c2", tools.SubAgentToolName, subAgentArgs("write the child's file")),
		toolCallScript("c3", "write_file", `{"path":"child.txt","content":"c"}`), // the child's Turn 0
		contentScript("child done"), // the child's Turn 1, its final message
		contentScript("parent done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "write both files"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	step, ok := a.journal.Preview()
	if !ok {
		t.Fatal("the delegating Exchange left no undo step")
	}
	if step.Ordinal != 1 {
		t.Errorf("step ordinal = %d, want 1 (the delegation opens no group of its own)", step.Ordinal)
	}
	assertChangedPaths(t, step.Changes,
		[]string{filepath.Join(root, "parent.txt"), filepath.Join(root, "child.txt")},
		"the delegating Exchange")
}

// ---------------------------------------------------------------------------
// The snapshot capture points (ADR 0074): the pre image taken at the tool choke
// point, and the post image taken at the one owner of Exchange end.
// ---------------------------------------------------------------------------

// snapshotAgent builds an Agent over a fresh workspace with a real snapshot-backed journal —
// the wiring a Driver does at start-up (snapshot.OpenJournal + SetJournal) — and returns both.
// The workspace root is symlink-resolved for the reason the tests above resolve it.
func snapshotAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	requireGit(t)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the temp root: %v", err)
	}
	home, root := filepath.Join(base, "home"), filepath.Join(base, "workspace")
	for _, dir := range []string{home, root} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	a := newWorkspaceAgent(t, root)
	journal, note, err := snapshot.OpenJournal(context.Background(), home, "session-1", root, true)
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	a.SetJournal(journal, note)
	if a.UndoNote() != "" {
		t.Fatalf("UndoNote = %q, want none: this workspace has snapshots", a.UndoNote())
	}
	return a, root
}

// writeThrough returns the fake subprocess tool that writes one file the write funnel never sees —
// the coverage gap the snapshot pair exists to close.
func writeThrough(root, name, content string) mutatingSubprocessTool {
	return mutatingSubprocessTool{
		subprocess: true,
		run:        func() error { return os.WriteFile(filepath.Join(root, name), []byte(content), 0o644) },
		result:     domain.ToolResult{Content: "ok"},
	}
}

// readingFakeTool is a read-only stand-in: dispatch must take no pre image for it, so an Exchange
// that only looks at the workspace costs no git and leaves no step to walk past.
type readingFakeTool struct{ ran *bool }

func (r readingFakeTool) Name() string            { return "fake_read" }
func (r readingFakeTool) Description() string     { return "test read-only stand-in" }
func (r readingFakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (r readingFakeTool) ReadOnly() bool          { return true }

func (r readingFakeTool) Execute(context.Context, domain.ToolCall) (domain.ToolResult, error) {
	*r.ran = true
	return domain.ToolResult{CallID: "call-1", Content: "read"}, nil
}

// TestSnapshotsCoverAWriteTheFunnelNeverSaw: a subprocess writes a file through no seam of
// apogee's, and `/undo` still names it and puts it back. This is the whole promise of ADR 0074 —
// the funnel journal, which is what ADR 0051 shipped, records nothing at all for this call.
func TestSnapshotsCoverAWriteTheFunnelNeverSaw(t *testing.T) {
	a, root := snapshotAgent(t)
	created := filepath.Join(root, "by-the-subprocess.txt")

	a.turns.openExchange()
	a.journal.BeginGroup()
	executeFake(t, a, writeThrough(root, "by-the-subprocess.txt", "written outside the funnel\n"))
	a.turns.closeExchange()

	step, ok := a.UndoPreview()
	if !ok {
		t.Fatal("the Exchange left no undo step; the closing capture did not run")
	}
	assertChangedPaths(t, step.Changes, []string{created}, "the subprocess write")

	if _, err := a.UndoRevert(step.Generation); err != nil {
		t.Fatalf("UndoRevert: %v", err)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("the created file survived the revert (stat err %v)", err)
	}
}

// TestReadOnlyCallTakesNoPreImage: the pre image is owed by the first WRITE-capable call, not by
// the first call. A read-only tool leaves the group unopened, so an Exchange that only looked at
// the workspace never becomes a step.
func TestReadOnlyCallTakesNoPreImage(t *testing.T) {
	a, _ := snapshotAgent(t)

	ran := false
	a.turns.openExchange()
	a.journal.BeginGroup()
	call := domain.ToolCall{ID: "call-1", Tool: "fake_read"}
	if _, outcome := a.executeTool(context.Background(), 0, readingFakeTool{ran: &ran}, call, nil); outcome != dispatchDone {
		t.Fatalf("outcome = %v, want dispatchDone", outcome)
	}
	if !ran {
		t.Fatal("the read-only tool did not run")
	}
	a.turns.closeExchange()

	if step, ok := a.UndoPreview(); ok {
		t.Errorf("a read-only Exchange left an undo step: %+v", step.Changes)
	}
}

// TestExchangeEndClosesTheGroupOnEveryRowThatEndsOne: the closing capture hangs off the ONE owner
// of Exchange end, so the host scrapping an Exchange and the delegate step cap both take the image
// — and a cancelled Turn, which leaves the Exchange open for its re-attempt, does not.
func TestExchangeEndClosesTheGroupOnEveryRowThatEndsOne(t *testing.T) {
	tests := []struct {
		name       string
		end        func(a *Agent)
		wantClosed bool
	}{
		{
			name:       "the host scraps the Exchange",
			end:        func(a *Agent) { a.AbortExchange() },
			wantClosed: true,
		},
		{
			name:       "the delegate step cap ends it",
			end:        func(a *Agent) { a.turns.end(&turnRun{}, endStepCapped) },
			wantClosed: true,
		},
		{
			name:       "a faulted Turn ends it",
			end:        func(a *Agent) { a.turns.end(&turnRun{}, endAbandoned) },
			wantClosed: true,
		},
		{
			name:       "a cancelled Turn leaves it open for the re-attempt",
			end:        func(a *Agent) { a.turns.end(&turnRun{}, endCancelled) },
			wantClosed: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, root := snapshotAgent(t)
			created := filepath.Join(root, "written.txt")

			a.turns.openExchange()
			a.journal.BeginGroup()
			executeFake(t, a, writeThrough(root, "written.txt", "one write\n"))
			tt.end(a)

			step, ok := a.UndoPreview()
			if !ok {
				t.Fatal("the group vanished entirely; MarkPre opened one")
			}
			if tt.wantClosed {
				assertChangedPaths(t, step.Changes, []string{created}, tt.name)
				return
			}
			if len(step.Changes) != 0 {
				t.Errorf("a cancelled Turn closed the group: %+v; the re-attempt writes into it",
					step.Changes)
			}
		})
	}
}

// TestChildExchangeEndLeavesTheParentGroupOpen: a delegated child shares the parent's journal, and
// its Exchange runs INSIDE the parent's, so the child's end must not take the closing image. If it
// did, a parent write made AFTER the delegation would fall outside the step the human is offered —
// one instruction, two `/undo` presses.
func TestChildExchangeEndLeavesTheParentGroupOpen(t *testing.T) {
	a, root := snapshotAgent(t)

	a.turns.openExchange()
	a.journal.BeginGroup()
	executeFake(t, a, writeThrough(root, "parent.txt", "p\n"))

	// The child: an Agent of its own at depth 1 holding the PARENT's journal instance, which is
	// what newChildAgent hands it, and an Exchange of its own that ends.
	child := newWorkspaceAgent(t, root)
	child.depth = 1
	child.SetJournal(a.journal, a.UndoNote())
	child.turns.openExchange()
	executeFake(t, child, writeThrough(root, "child.txt", "c\n"))
	child.turns.closeExchange()

	if step, _ := a.UndoPreview(); len(step.Changes) != 0 {
		t.Fatalf("the child's Exchange end closed the parent's group: %+v", step.Changes)
	}

	// …and the parent's own later write joins the same step when the parent's Exchange ends.
	executeFake(t, a, writeThrough(root, "after.txt", "a\n"))
	a.turns.closeExchange()

	step, ok := a.UndoPreview()
	if !ok {
		t.Fatal("the parent Exchange left no undo step")
	}
	if step.Ordinal != 1 {
		t.Errorf("step ordinal = %d, want 1: one instruction is one step", step.Ordinal)
	}
	assertChangedPaths(t, step.Changes,
		[]string{filepath.Join(root, "after.txt"), filepath.Join(root, "child.txt"), filepath.Join(root, "parent.txt")},
		"the delegating Exchange")
}

// failingSnapshotter is an image source whose captures fail after the first n of them — the
// wedged-git case, which must cost a warning and never an Exchange.
type failingSnapshotter struct {
	ok   int
	took int
}

func (f *failingSnapshotter) Capture(context.Context) (string, error) {
	f.took++
	if f.took > f.ok {
		return "", errors.New("git is wedged")
	}
	return strings.Repeat("a", 40), nil
}

func (f *failingSnapshotter) Diff(context.Context, string, string) ([]string, error) { return nil, nil }

func (f *failingSnapshotter) ListBlobs(context.Context, string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (f *failingSnapshotter) Content(string, string) ([]byte, bool, error) { return nil, false, nil }

// TestACaptureFailureIsReportedAndNeverFailsTheExchange: both capture points report through
// ErrorEvent{Source: "undo"} and neither turns a working call, or a finished Exchange, into a
// failure. The group falls back to what the funnel recorded, which is the coverage ADR 0051
// shipped — a thinner undo, never a broken run.
func TestACaptureFailureIsReportedAndNeverFailsTheExchange(t *testing.T) {
	for _, tt := range []struct {
		name string
		ok   int
	}{
		{name: "the pre image fails", ok: 0},
		{name: "the closing image fails", ok: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("resolve the temp root: %v", err)
			}
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.WorkspaceDir = root
			a, err := newAgent(cfg, echoResponder{reply: "unused"})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}
			a.SetJournal(undo.New(
				undo.WithSnapshotter(&failingSnapshotter{ok: tt.ok}),
				undo.WithWorkspace(root),
			), "")

			a.turns.openExchange()
			a.journal.BeginGroup()
			result := executeFake(t, a, writeThrough(root, "written.txt", "one write\n"))
			if result.IsError {
				t.Errorf("the tool result was faulted by apogee's own bookkeeping: %+v", result)
			}
			a.turns.closeExchange()

			var reported bool
			for _, event := range sink.events {
				if e, ok := event.(domain.ErrorEvent); ok && e.Source == "undo" {
					reported = true
				}
			}
			if !reported {
				t.Errorf("no ErrorEvent{Source: \"undo\"} for a failed capture; events = %+v", sink.events)
			}
		})
	}
}
