package tools

import (
	"context"
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

// TestConsoleClose_AnswersOnlyToTheRunThatOpenedIt is F-37 at the close end, where addressing
// somebody else's Console would not just read it but END it: a run that did not open one is
// refused as though the id had never been issued (ADR 0059 §6).
func TestConsoleClose_AnswersOnlyToTheRunThatOpenedIt(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	for _, testCase := range consoleOwnerCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			checkConsoleOwnership(t, testCase, func(ctx context.Context, id int) (domain.ToolResult, error) {
				return NewConsoleClose().Execute(ctx, consoleCloseCall("c1", id))
			})
		})
	}
}

// TestConsoleTail_ZeroWaitReadsTheTailUnderACancelledContext pins what console_close's tail read
// leans on: a wait of 0 drains what is buffered before it looks at ctx, so a cancel landing after
// the teardown cannot cost the model the last thing the program said.
func TestConsoleTail_ZeroWaitReadsTheTailUnderACancelledContext(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, registry := consoleTestCtx(t)
	id, target := openUndrainedConsole(t, ctx, "echo tail-bytes; exit 3")
	waitForConsoleExit(t, target)
	if err := registry.Close(id); err != nil {
		t.Fatalf("closing console %d: %v", id, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	tail, _, gathered := consoleTail(cancelled, target, 0)

	if !gathered || !strings.Contains(tail, "tail-bytes") {
		t.Errorf("tail = %q (gathered=%t), want the output nobody had read yet", tail, gathered)
	}
	if !strings.HasSuffix(tail, "exited with code 3") {
		t.Errorf("tail = %q, want it to end with the program's exit code", tail)
	}
}

// TestConsoleResultsCarryTheirEnding pins the structured half of every Console result that
// renders a tail: the domain.ProcessEnding a host reads is the state the closing line states —
// running after an open, exited with its code once the program is over, killed after a close
// that ended it — and a refusal that reached no Console carries none.
func TestConsoleResultsCarryTheirEnding(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	running := domain.ProcessEnding{State: domain.ProcessRunning}
	cases := []struct {
		name string
		call func(t *testing.T, ctx context.Context) domain.ToolResult
		want domain.ToolSummary
	}{
		{
			name: "open of a live program",
			call: func(t *testing.T, ctx context.Context) domain.ToolResult {
				res, err := NewConsoleOpen(t.TempDir(), nil).Execute(ctx, consoleOpenCall("c1", "cat", 200))
				if err != nil {
					t.Fatalf("console_open err = %v, want nil", err)
				}
				return res
			},
			want: running,
		},
		{
			name: "send to a live program",
			call: func(t *testing.T, ctx context.Context) domain.ToolResult {
				id := openTestConsole(t, ctx, "cat")
				res, err := NewConsoleSend().Execute(ctx, consoleSendCall("c1", id, "hello", false, 200))
				if err != nil {
					t.Fatalf("console_send err = %v, want nil", err)
				}
				return res
			},
			want: running,
		},
		{
			name: "read after the program exited",
			call: func(t *testing.T, ctx context.Context) domain.ToolResult {
				id, target := openUndrainedConsole(t, ctx, "exit 4")
				waitForConsoleExit(t, target)
				res, err := NewConsoleRead().Execute(ctx, consoleReadCall("c1", id, 0))
				if err != nil {
					t.Fatalf("console_read err = %v, want nil", err)
				}
				return res
			},
			want: domain.ProcessEnding{State: domain.ProcessExited, ExitCode: 4},
		},
		{
			name: "close of a live program",
			call: func(t *testing.T, ctx context.Context) domain.ToolResult {
				id := openTestConsole(t, ctx, "cat")
				res, err := NewConsoleClose().Execute(ctx, consoleCloseCall("c1", id))
				if err != nil {
					t.Fatalf("console_close err = %v, want nil", err)
				}
				return res
			},
			want: domain.ProcessEnding{State: domain.ProcessKilled, ExitCode: -1},
		},
		{
			name: "refusal naming no console",
			call: func(t *testing.T, ctx context.Context) domain.ToolResult {
				res, err := NewConsoleRead().Execute(ctx, consoleReadCall("c1", 99, 0))
				if err != nil {
					t.Fatalf("console_read err = %v, want nil", err)
				}
				return res
			},
			want: nil,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, _ := consoleTestCtx(t)

			res := testCase.call(t, ctx)

			if res.Summary != testCase.want {
				t.Errorf("Summary = %#v, want %#v (content %q)", res.Summary, testCase.want, res.Content)
			}
		})
	}
}
