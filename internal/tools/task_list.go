package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tasklist"
)

// taskListSpec is built with the package's own caps rather than repeating them, so the numbers
// the model is told and the numbers [tasklist.List.Replace] enforces cannot drift apart — a
// schema promising a limit the code refuses is a refusal the model cannot see coming.
var taskListSpec = toolSpec{
	name: "task_list",
	description: "Keep your own checklist for this job. Every call REPLACES the whole list — send" +
		" the COMPLETE set of tasks each time, never just the new or changed ones — so you tick a" +
		" task off by resending it with done set to true, and you clear the list by sending an empty" +
		" array. The list is kept in front of you, so a long job still knows what is left; use it" +
		" when the work has several steps worth tracking, and leave it alone when it does not.",
	schema: json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "required": ["tasks"],
  "properties": {
    "tasks": {
      "type": "array",
      "description": "The COMPLETE list in the order you want to read it back: every task, finished ones included, not only the ones that changed (at most %d; an empty array clears the list)",
      "items": {
        "type": "object",
        "required": ["text"],
        "properties": {
          "text": {"type": "string", "description": "What the task is, as a short label rather than a plan (at most %d characters)"},
          "done": {"type": "boolean", "description": "True when this task is finished (default false)"}
        }
      }
    }
  }
}`, tasklist.MaxItems, tasklist.MaxTextChars)),
}

type taskListArgs struct {
	Tasks []tasklist.Item `json:"tasks"`
}

// TaskList lets the model hold its own checklist as engine state, re-rendered into the standing
// system content so a run that has been compacted still reads what is left (ADR 0072).
//
// It is stateless in the ADR 0008 sense that matters here: the tool instance holds nothing. The
// list lives on the engine and arrives on the call context (tasklist.FromContext), the same way
// the undo journal and the console registry do, because SwapTools rebuilds these instances
// mid-session and a checklist held by a tool that was rebuilt away is a checklist nobody can
// update.
//
// It takes the READ-ONLY floor (domain.ReadOnlyTool): writing a list of intentions touches no
// file, starts no process and reaches no network, so it is ungated in every mode and offered in
// Plan — where a model deciding what it is about to do most wants to write one down. ask_user and
// console_close are the precedent that IsReadOnly measures BLAST RADIUS rather than mutation.
//
// Unlike the Console family it is DEFAULT-ON for every model and carries no DefaultOff marker: it
// costs one slot, it is the same affordance for every model, and `tools.disabled:` turns it off
// for the roster that would rather not have it (ADR 0057).
type TaskList struct {
	toolSpec
}

// NewTaskList returns a task_list tool. It takes no root and no host delegate: the list it writes
// reaches it on the call context, so there is nothing to wire at construction.
func NewTaskList() *TaskList {
	return &TaskList{toolSpec: taskListSpec}
}

// ReadOnly reports that task_list only reads (true) as far as blast radius goes: it writes the
// model's own checklist and nothing the host owns.
func (t *TaskList) ReadOnly() bool { return true }

// Execute replaces the held list with the call's tasks and returns the list as the model will
// read it in its standing block.
//
// A session that carries no list is an error RESULT rather than a Go error — the call was
// well-formed and simply cannot be served here, which is the one thing a model can act on. A
// rejected replace (too many tasks, or one too long) is an error result too, and the held list is
// left exactly as it was. Only ctx cancellation is a Go error.
func (t *TaskList) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[taskListArgs](call)
	if !ok {
		return fail, nil
	}

	list := tasklist.FromContext(ctx)
	if list == nil {
		return errorResult(call.ID, "a task list is not available in this session"), nil
	}

	if err := list.Replace(args.Tasks); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	return okResult(call.ID, list.Render()), nil
}

var (
	_ domain.Tool         = (*TaskList)(nil)
	_ domain.ReadOnlyTool = (*TaskList)(nil)
)
