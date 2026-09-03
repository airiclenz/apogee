package agent

// The task list as ENGINE state on the Agent (ADR 0072): that a tool call reaches it through the
// context seam at all, that a delegation is handed its OWN empty one rather than the parent's, and
// that the /clear boundary empties it. Its snapshot half lives in state_test.go.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tasklist"
)

// ---------------------------------------------------------------------------
// Fakes & helpers
// ---------------------------------------------------------------------------

// taskWriter is the fake tool this file drives the engine with: on every call it reaches for the
// list through the CONTEXT — the path the real task_list tool takes — and writes one row into it.
// Reading it out of the call rather than out of a held field is the whole point: a dispatch that
// forgot to install the list would fail here rather than in the standing block.
type taskWriter struct {
	lists   []*tasklist.List // the list each call found on its context, in call order
	failure error            // the first thing that went wrong inside a call, if any
}

// tool returns the writer as a read-only Tool, so the fake runs in every mode without an Approver.
func (w *taskWriter) tool() domain.Tool {
	return fakeTool{name: "write_tasks", readOnly: true, execute: w.execute}
}

// execute records the list the call was handed and replaces its contents. A failure is recorded
// rather than returned: a Go error out of a tool cuts the Turn short, and the test wants the
// Exchange to finish so it can assert on what the engine holds afterwards.
func (w *taskWriter) execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	list := tasklist.FromContext(ctx)
	if list == nil {
		w.failure = errors.New("the tool call's context carries no task list")
		return domain.ToolResult{CallID: call.ID, Content: "no list"}, nil
	}
	w.lists = append(w.lists, list)

	if err := list.Replace([]tasklist.Item{{Text: "written by call " + call.ID}}); err != nil {
		w.failure = err
		return domain.ToolResult{CallID: call.ID, Content: "replace failed"}, nil
	}
	return domain.ToolResult{CallID: call.ID, Content: "list written"}, nil
}

// writeTaskScripts returns the scripted stream pairs for n Exchanges that each ask for one
// write_tasks call and then finish. One scriptedResponder serves a whole tree — a child speaks
// over its parent's Upstream — so the scripts of every Agent in a test come from this one list.
func writeTaskScripts(n int) [][]provider.Delta {
	scripts := make([][]provider.Delta, 0, 2*n)
	for i := range n {
		scripts = append(scripts,
			// The arguments carry the iteration so two consecutive Exchanges are not an identical
			// repeat, which the tool-loop breaker Floor guard would answer instead of dispatching.
			toolCallScript(fmt.Sprintf("t%d", i), "write_tasks", fmt.Sprintf(`{"n":%d}`, i)),
			contentScript("noted"))
	}
	return scripts
}

// newTaskListAgent builds a top-level Agent wired to a fresh writer, scripted for n Exchanges.
func newTaskListAgent(t *testing.T, n int) (*Agent, *taskWriter) {
	t.Helper()

	writer := &taskWriter{}
	a, err := newAgent(configWithTools(&recordingSink{}, writer.tool()),
		&scriptedResponder{scripts: writeTaskScripts(n)})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a, writer
}

// assertTaskTexts fails when the list does not hold exactly these texts, in this order.
func assertTaskTexts(t *testing.T, list *tasklist.List, want ...string) {
	t.Helper()

	items := list.Items()
	if len(items) != len(want) {
		t.Fatalf("task list = %v, want %v", items, want)
	}
	for i, text := range want {
		if items[i].Text != text {
			t.Fatalf("task list = %v, want %v", items, want)
		}
	}
}

// ---------------------------------------------------------------------------
// The seam
// ---------------------------------------------------------------------------

// TestDispatchCarriesTheTaskList is the seam itself: a tool call reaches the engine's list through
// the context, at depth 0 and under a delegation alike — and what it reaches is the list of the
// Agent whose dispatch installed it, so a child's write lands in the child's list.
func TestDispatchCarriesTheTaskList(t *testing.T) {
	t.Parallel()

	parent, writer := newTaskListAgent(t, 2)
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	runOneExchange(t, parent, "write the list at the top level")
	runOneExchange(t, child, "write the list under the delegation")

	if writer.failure != nil {
		t.Fatalf("the fake tool could not reach its list: %v", writer.failure)
	}
	if len(writer.lists) != 2 {
		t.Fatalf("the fake tool ran %d times, want 2", len(writer.lists))
	}
	if writer.lists[0] != parent.tasks {
		t.Errorf("the depth-0 call found list %p, want the engine's %p", writer.lists[0], parent.tasks)
	}
	if writer.lists[1] != child.tasks {
		t.Errorf("the depth-1 call found list %p, want the delegation's own %p", writer.lists[1], child.tasks)
	}
}

// ---------------------------------------------------------------------------
// The delegation's own list, and the /clear boundary
// ---------------------------------------------------------------------------

// TestChildGetsItsOwnEmptyTaskList pins the ratified call (ADR 0072): a delegation inherits
// NOTHING of the parent's checklist. Sharing the handle — the way the journal and the console
// registry are shared — would let a child's whole-list replace overwrite a list the parent is
// still working from, with no ids in the shape to tell the two runs' rows apart.
func TestChildGetsItsOwnEmptyTaskList(t *testing.T) {
	t.Parallel()

	parent, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := parent.tasks.Replace([]tasklist.Item{{Text: "the parent's own work"}}); err != nil {
		t.Fatalf("seed the parent's list: %v", err)
	}

	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	if child.tasks == parent.tasks {
		t.Fatalf("child list = %p, want its own, not the parent's", child.tasks)
	}
	if items := child.tasks.Items(); len(items) != 0 {
		t.Errorf("a fresh delegation's list = %v, want empty", items)
	}

	if err := child.tasks.Replace([]tasklist.Item{{Text: "the delegated decomposition"}}); err != nil {
		t.Fatalf("write the child's list: %v", err)
	}

	assertTaskTexts(t, parent.tasks, "the parent's own work")
	assertTaskTexts(t, child.tasks, "the delegated decomposition")
}

// TestClearContextEmptiesTheTaskList pins the /clear boundary: the checklist described the work
// the forgotten conversation was doing, so a new session starts with a blank one rather than a
// plan for a job the model can no longer see.
func TestClearContextEmptiesTheTaskList(t *testing.T) {
	t.Parallel()

	a, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.tasks.Replace([]tasklist.Item{{Text: "still open"}, {Text: "finished", Done: true}}); err != nil {
		t.Fatalf("seed the list: %v", err)
	}

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	if items := a.tasks.Items(); len(items) != 0 {
		t.Errorf("the task list after /clear = %v, want empty", items)
	}
}
