package tools

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

func consoleCloseCall(callID string, id int) domain.ToolCall {
	args := fmt.Sprintf(`{"id":%d}`, id)
	return domain.ToolCall{ID: callID, Tool: "console_close", Arguments: []byte(args)}
}

func TestConsoleClose_Markers(t *testing.T) {
	t.Parallel()
	tool := NewConsoleClose()

	if tool.Name() != "console_close" {
		t.Errorf("Name() = %q, want console_close", tool.Name())
	}
	if !domain.IsReadOnly(tool) {
		t.Error("console_close must declare itself read-only: ending a process leaves less behind than starting it did")
	}
	if domain.IsSubprocessTool(tool) {
		t.Error("console_close must NOT carry the Subprocess marker: it starts nothing")
	}
	if !domain.IsDefaultOff(tool) {
		t.Error("console_close must ship default-off (ADR 0057)")
	}
}

// TestConsoleClose_ReturnsTheUnreadTailAndTheExitCode is the whole point of closing through the
// tool rather than letting the engine sweep the Console away: the last thing the program said —
// output no read ever collected — reaches the model together with how the program ended.
func TestConsoleClose_ReturnsTheUnreadTailAndTheExitCode(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	id, target := openUndrainedConsole(t, ctx, "echo tail-bytes; exit 3")
	waitForConsoleExit(t, target)

	res, err := NewConsoleClose().Execute(ctx, consoleCloseCall("c1", id))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("close produced an error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "tail-bytes") {
		t.Errorf("result = %q, want the output nobody had read yet", res.Content)
	}
	if !strings.HasSuffix(res.Content, "exited with code 3") {
		t.Errorf("result = %q, want it to end with the program's exit code", res.Content)
	}
}

// TestConsoleClose_KillsALiveProgramAndRetiresTheID pins closing as an END: a program that would
// have run forever is stopped, and the id it answered to is out of the registry — the next call
// naming it reads as unknown rather than reaching a second process.
func TestConsoleClose_KillsALiveProgramAndRetiresTheID(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, registry := consoleTestCtx(t)
	id := openTestConsole(t, ctx, "cat")

	res, err := NewConsoleClose().Execute(ctx, consoleCloseCall("c1", id))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !strings.HasSuffix(res.Content, "killed") {
		t.Errorf("result = %q, want a program stopped by the close to report %q", res.Content, "killed")
	}
	if slices.Contains(registry.OpenIDs(), id) {
		t.Errorf("console %d is still open after close; OpenIDs() = %v", id, registry.OpenIDs())
	}
}

// TestConsoleClose_SecondCloseIsAnErrorResult keeps a double close honest: the id is retired and
// never reissued, so the second call is told there is no such console instead of being waved
// through as a no-op that would leave the model believing it still has one.
func TestConsoleClose_SecondCloseIsAnErrorResult(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	id := openTestConsole(t, ctx, "cat")
	tool := NewConsoleClose()
	if first, err := tool.Execute(ctx, consoleCloseCall("c1", id)); err != nil || first.IsError {
		t.Fatalf("first close = %q (err=%v), want it to succeed", first.Content, err)
	}

	res, err := tool.Execute(ctx, consoleCloseCall("c2", id))

	if err != nil {
		t.Fatalf("Execute err = %v, want an error RESULT rather than a Go error", err)
	}
	if !res.IsError || !strings.Contains(res.Content, fmt.Sprintf("no console %d", id)) {
		t.Errorf("result = %q (IsError=%v), want a refusal naming console %d", res.Content, res.IsError, id)
	}
}

// TestConsoleClose_UnknownIDNamesTheConsolesThatAreOpen: the refusal a model reads has to tell it
// what it DOES have, because "no console 7" alone leaves a model that lost track of its consoles
// with nothing to act on.
func TestConsoleClose_UnknownIDNamesTheConsolesThatAreOpen(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	open := openTestConsole(t, ctx, "cat")

	res, err := NewConsoleClose().Execute(ctx, consoleCloseCall("c1", open+99))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError || !strings.Contains(res.Content, fmt.Sprintf("open consoles: %d", open)) {
		t.Errorf("result = %q (IsError=%v), want it to name the open console %d", res.Content, res.IsError, open)
	}
}

// TestConsoleClose_NoRegistryIsAnErrorResult covers the wiring gap rather than a model mistake: a
// dispatch that carries no registry holds no consoles, so every id in it is unknown — and the
// close reports that instead of dereferencing the registry it never got.
func TestConsoleClose_NoRegistryIsAnErrorResult(t *testing.T) {
	t.Parallel()

	res, err := NewConsoleClose().Execute(t.Context(), consoleCloseCall("c1", 1))

	if err != nil {
		t.Fatalf("Execute err = %v, want an error RESULT rather than a Go error", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "open consoles: none") {
		t.Errorf("result = %q (IsError=%v), want the no-consoles refusal", res.Content, res.IsError)
	}
	if console.FromContext(t.Context()) != nil {
		t.Fatal("this test only means anything on a context with no registry")
	}
}
