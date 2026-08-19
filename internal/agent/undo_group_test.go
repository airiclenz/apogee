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
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
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
