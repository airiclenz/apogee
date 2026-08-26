package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

func consoleReadCall(callID string, id, waitMS int) domain.ToolCall {
	args := fmt.Sprintf(`{"id":%d,"wait_ms":%d}`, id, waitMS)
	return domain.ToolCall{ID: callID, Tool: "console_read", Arguments: []byte(args)}
}

// openUndrainedConsole opens a Console whose output nobody has taken yet, and returns its id
// beside the Console itself. It is what a test needs when the OUTPUT is the subject: the open
// call collects its window from the same buffer every later read draws on, so an ordinary open
// would have consumed the very bytes the test is about. A negative wait_ms is the documented way
// to ask for no collection at all (consoleWait).
//
// Shared with console_close_test.go, whose subject — the tail nobody read — is the same one.
func openUndrainedConsole(t *testing.T, ctx context.Context, command string) (int, *console.Console) {
	t.Helper()
	res, err := NewConsoleOpen(t.TempDir(), nil).Execute(ctx, consoleOpenCall("open", command, -1))
	if err != nil || res.IsError {
		t.Fatalf("console_open(%q) = %q (err=%v)", command, res.Content, err)
	}
	registry := console.FromContext(ctx)
	ids := registry.OpenIDs()
	if len(ids) == 0 {
		t.Fatalf("console_open(%q) registered nothing", command)
	}
	id := ids[len(ids)-1]
	opened, ok := registry.Get(id)
	if !ok {
		t.Fatalf("console %d is not in the registry that just issued it", id)
	}
	return id, opened
}

// waitForConsoleExit blocks until the Console's process has been reaped, so a test asserting on
// what a finished program produced is not racing the program's own exit.
func waitForConsoleExit(t *testing.T, c *console.Console) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !c.Alive() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("console %d was still running after 10s", c.ID)
}

func TestConsoleRead_Markers(t *testing.T) {
	t.Parallel()
	tool := NewConsoleRead()

	if tool.Name() != "console_read" {
		t.Errorf("Name() = %q, want console_read", tool.Name())
	}
	if !domain.IsReadOnly(tool) {
		t.Error("console_read must declare itself read-only: it types nothing and starts nothing")
	}
	if domain.IsSubprocessTool(tool) {
		t.Error("console_read must NOT carry the Subprocess marker: it reads a buffer, it runs nothing")
	}
	if !domain.IsDefaultOff(tool) {
		t.Error("console_read must ship default-off (ADR 0057)")
	}
}

// TestConsoleRead_QuietConsoleReportsLivenessAndNothingElse pins the zero-wait default: a poll
// of a program that has said nothing answers immediately with the one fact there is — it is
// still there — rather than sitting on a window the model never asked for.
func TestConsoleRead_QuietConsoleReportsLivenessAndNothingElse(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	id := openTestConsole(t, ctx, "cat")

	res, err := NewConsoleRead().Execute(ctx, consoleReadCall("c1", id, 0))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("read produced an error result: %q", res.Content)
	}
	if res.Content != consoleAliveStatus {
		t.Errorf("result = %q, want exactly %q for a console that has printed nothing", res.Content, consoleAliveStatus)
	}
}

// TestConsoleRead_WaitReturnsAsSoonAsOutputArrives is why wait_ms exists at all: a model watching
// a program that is still working gets its answer the moment the program speaks, so waiting
// costs one call instead of a poll loop that costs a Turn each time round.
func TestConsoleRead_WaitReturnsAsSoonAsOutputArrives(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, registry := consoleTestCtx(t)
	id := openTestConsole(t, ctx, "cat")
	target, ok := registry.Get(id)
	if !ok {
		t.Fatalf("console %d is not in the registry that just issued it", id)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = target.Write([]byte("late\n"))
	}()

	start := time.Now()
	res, err := NewConsoleRead().Execute(ctx, consoleReadCall("c1", id, 2000))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !strings.Contains(res.Content, "late") {
		t.Errorf("result = %q, want the output that arrived inside the window", res.Content)
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("read took %v, want it to return when the output arrived rather than at the end of the window", elapsed)
	}
}

// TestConsoleRead_AnnouncesTheOutputItsBufferDropped pins the hole in the stream. A program that
// prints more than the ring holds loses its oldest output, and a model reading the survivors as
// though they were continuous would draw conclusions from a splice — so the result says so.
func TestConsoleRead_AnnouncesTheOutputItsBufferDropped(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	id, target := openUndrainedConsole(t, ctx, "yes | head -c 2000000")
	waitForConsoleExit(t, target)

	res, err := NewConsoleRead().Execute(ctx, consoleReadCall("c1", id, 0))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	firstLine, _, _ := strings.Cut(res.Content, "\n")
	if !strings.Contains(firstLine, "bytes of earlier output dropped") {
		t.Errorf("first line = %q, want the dropped-bytes note after overflowing the ring", firstLine)
	}
	if !strings.HasSuffix(res.Content, "exited with code 0") {
		t.Errorf("result did not end with the exit status: %q", lastLine(res.Content))
	}
}

// TestConsoleRead_UnknownIDIsAnErrorResult keeps a stale id a MODEL-fixable mistake: an id from
// before a restart, or one already closed, comes back as a refusal it can act on rather than as
// a Go error that would roll the whole Turn back.
func TestConsoleRead_UnknownIDIsAnErrorResult(t *testing.T) {
	t.Parallel()
	ctx, _ := consoleTestCtx(t)

	res, err := NewConsoleRead().Execute(ctx, consoleReadCall("c1", 7, 0))

	if err != nil {
		t.Fatalf("Execute err = %v, want an error RESULT rather than a Go error", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "no console 7") {
		t.Errorf("result = %q (IsError=%v), want a refusal naming console 7", res.Content, res.IsError)
	}
}

// TestConsoleRead_AnswersOnlyToTheRunThatOpenedIt is F-37 at the read end: a run that did not
// open a Console cannot watch what it prints, and the refusal it gets tells it nothing about the
// id it named (ADR 0059 §6). The table is shared with console_send and console_close, whose
// lookup this is.
func TestConsoleRead_AnswersOnlyToTheRunThatOpenedIt(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	for _, testCase := range consoleOwnerCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			checkConsoleOwnership(t, testCase, func(ctx context.Context, id int) (domain.ToolResult, error) {
				return NewConsoleRead().Execute(ctx, consoleReadCall("c1", id, 0))
			})
		})
	}
}

// lastLine is the tail of a result, for failure messages that would otherwise print a megabyte.
func lastLine(content string) string {
	if idx := strings.LastIndex(content, "\n"); idx >= 0 {
		return content[idx+1:]
	}
	return content
}
