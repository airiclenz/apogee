package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// The delegation refusals, pinned byte for byte as they stood before a sub_agent call's arguments
// became one parsed request (plan 2026-11-21 - 00). A refusal is the one place the model is told
// what it got wrong, so a reshaped parse that changes its wording — or which check speaks first —
// changes what the model reads: these literals are the reference that reshaping is checked
// against. Each is captured output, deliberately written out rather than composed from the
// constants that produce it, since a pin built from the code it pins would move with it. Edit one
// only where a plan item names the change.

// pinnedRefusalTask is a task long enough to be a real delegation, so an input refuses on the
// argument it is about and never on its task.
const pinnedRefusalTask = "survey the repo for every config key"

// TestDelegationRefusals_AreByteIdentical proves each malformed or unrunnable sub_agent call is
// refused, on the blocking path (runSubAgent) and on the background path (backgroundSubAgentResult)
// alike, with exactly the pinned Content — and that a call two of whose arguments are wrong is
// refused for the one each path checks first.
func TestDelegationRefusals_AreByteIdentical(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		args           string
		wantBlocking   string
		wantBackground string
	}{
		{
			name:           "arguments that are not JSON",
			args:           `{not json`,
			wantBlocking:   "invalid sub_agent arguments: invalid character 'n' looking for beginning of object key string",
			wantBackground: "invalid sub_agent arguments: invalid character 'n' looking for beginning of object key string",
		},
		{
			name:           "a task that is not a string",
			args:           `{"task":5}`,
			wantBlocking:   "invalid sub_agent arguments: json: cannot unmarshal number into Go struct field SubAgentArgs.task of type string",
			wantBackground: "invalid sub_agent arguments: json: cannot unmarshal number into Go struct field SubAgentArgs.task of type string",
		},
		{
			name:           "no task",
			args:           `{}`,
			wantBlocking:   "sub_agent requires a non-empty task",
			wantBackground: "sub_agent requires a non-empty task",
		},
		{
			name:           "an unknown run_on",
			args:           `{"task":"` + pinnedRefusalTask + `","run_on":"gpu"}`,
			wantBlocking:   `invalid run_on "gpu": want "session" or "sub-agents-server"`,
			wantBackground: `invalid run_on "gpu": want "session" or "sub-agents-server"`,
		},
		{
			name:           "continue with background",
			args:           `{"task":"` + pinnedRefusalTask + `","continue":"surveyor","background":true}`,
			wantBlocking:   `[no delegate named "surveyor" to continue — retained: none]`,
			wantBackground: "sub_agent was not run: continue runs the delegation it names blocking, so it cannot be combined with background; call it again without background",
		},
		{
			name:           "an unknown tool",
			args:           `{"task":"` + pinnedRefusalTask + `","tools":["no_such_tool"]}`,
			wantBlocking:   `sub_agent tools: unknown tool no_such_tool — name tools from your own menu, or pass "read-only" as the whole value`,
			wantBackground: `sub_agent tools: unknown tool no_such_tool — name tools from your own menu, or pass "read-only" as the whole value`,
		},
		{
			name:           "a context that is not an array",
			args:           `{"task":"` + pinnedRefusalTask + `","context":"notes.md"}`,
			wantBlocking:   "invalid sub_agent context: want an array of file paths",
			wantBackground: "invalid sub_agent context: want an array of file paths",
		},
		{
			name:           "a context file that cannot be read",
			args:           `{"task":"` + pinnedRefusalTask + `","context":["missing.md"]}`,
			wantBlocking:   "sub_agent was not run: context file missing.md: file not found: missing.md",
			wantBackground: "sub_agent was not run: context file missing.md: file not found: missing.md",
		},
		{
			// Blocking: the unknown `continue` is refused before the seat is read.
			name:           "an unknown continue ahead of an unknown run_on",
			args:           `{"task":"x","continue":"nope","run_on":"gpu"}`,
			wantBlocking:   `[no delegate named "nope" to continue — retained: none]`,
			wantBackground: "sub_agent was not run: continue runs the delegation it names blocking, so it cannot be combined with background; call it again without background",
		},
		{
			// Background: the continuation is refused before the empty task is.
			name:           "a continue ahead of an empty task",
			args:           `{"continue":"x","background":true}`,
			wantBlocking:   "sub_agent requires a non-empty task",
			wantBackground: "sub_agent was not run: continue runs the delegation it names blocking, so it cannot be combined with background; call it again without background",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := newRefusalPinParent(t)
			call := domain.ToolCall{ID: "sa1", Tool: tools.SubAgentToolName, Arguments: json.RawMessage(tc.args)}

			blocking, outcome := a.runSubAgent(context.Background(), call, "")
			background := a.backgroundSubAgentResult(context.Background(), 1, call)

			if outcome != dispatchDone || !blocking.IsError || blocking.Content != tc.wantBlocking {
				t.Errorf("blocking refusal = %q (error %v, outcome %v), want %q", blocking.Content, blocking.IsError, outcome, tc.wantBlocking)
			}
			if !background.IsError || background.Content != tc.wantBackground {
				t.Errorf("background refusal = %q (error %v), want %q", background.Content, background.IsError, tc.wantBackground)
			}
			if infos, err := a.Workflows(); err != nil || len(infos) != 0 {
				t.Errorf("Workflows = %+v, %v; want none — a refused call starts no workflow", infos, err)
			}
		})
	}
}

// newRefusalPinParent is a top-level parent whose sub_agent publishes every optional argument —
// `run_on`, `background` and `context` — so each argument a pinned input names is one the engine
// reads rather than ignores, over a workspace that holds no files.
func newRefusalPinParent(t *testing.T) *Agent {
	t.Helper()
	cfg := baseConfig(&recordingSink{})
	cfg.Mode = domain.ModeAskBefore
	reg := domain.NewToolRegistry()
	_ = reg.Register(tools.NewSubAgentWith(tools.SubAgentOptions{SeatChoice: true, Background: true, Context: true}))
	_ = reg.Register(tools.NewWorkflow())
	_ = reg.Register(fakeTool{name: "read_thing", readOnly: true, result: "package main"})
	cfg.Tools = reg
	cfg.ScratchDir = t.TempDir()
	cfg.WorkspaceDir = t.TempDir()
	return newBackgroundParent(t, cfg, &workflowResponder{})
}

// TestBackgroundSubAgent_ALenientBackgroundWithBadArgsTakesTheBackgroundRefusal pins the route a
// call whose `background` reads but whose other arguments do not takes through dispatch: the
// background read is lenient (asksBackground decodes only that one key), so `{"task":5,
// "background":true}` is a background call, refused by the background path in its words — and,
// being no delegation of the reply's tool round, it mints no run id and starts no phase.
func TestBackgroundSubAgent_ALenientBackgroundWithBadArgsTakesTheBackgroundRefusal(t *testing.T) {
	t.Parallel()

	sink := newLockedSink()
	cfg := backgroundSubAgentConfig(t, sink)
	up := (&workflowResponder{}).
		route("please delegate", nil, toolCallScript("sa1", tools.SubAgentToolName, `{"task":5,"background":true}`)).
		route("please delegate", nil, contentScript("understood"))
	a := newBackgroundParent(t, cfg, up)

	runSubmitted(t, context.Background(), a, "please delegate")

	events := lockedEvents(sink)
	const want = "invalid sub_agent arguments: json: cannot unmarshal number into Go struct field SubAgentArgs.task of type string"
	if got := callResult(t, events, "sa1"); !got.IsError || got.Content != want {
		t.Errorf("sub_agent answer = %q (error %v), want %q", got.Content, got.IsError, want)
	}
	if id := toolCallEventFor(t, events, "sa1").SpawnRunID; id != "" {
		t.Errorf("ToolCallEvent.SpawnRunID = %q, want none — a background call mints no run id", id)
	}
	for _, e := range events {
		switch ev := e.(type) {
		case domain.ToolResultEvent:
			if ev.Result.CallID == "sa1" && ev.SpawnRunID != "" {
				t.Errorf("ToolResultEvent.SpawnRunID = %q, want none", ev.SpawnRunID)
			}
		case domain.SubAgentPhaseEvent:
			t.Errorf("SubAgentPhaseEvent %q emitted, want none — the refused call started no child", ev.Phase)
		case domain.WorkflowPhaseEvent:
			t.Errorf("WorkflowPhaseEvent %q emitted, want none — the refused call started no workflow", ev.Phase)
		}
	}
	if infos, err := a.Workflows(); err != nil || len(infos) != 0 {
		t.Errorf("Workflows = %+v, %v; want none — a refused call starts no workflow", infos, err)
	}
}
