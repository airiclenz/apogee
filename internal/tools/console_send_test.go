package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

func consoleSendCall(callID string, id int, input string, raw bool, waitMS int) domain.ToolCall {
	args := fmt.Sprintf(`{"id":%d,"input":%q,"raw":%t,"wait_ms":%d}`, id, input, raw, waitMS)
	return domain.ToolCall{ID: callID, Tool: "console_send", Arguments: []byte(args)}
}

// openTestConsole opens a Console through the tool itself and returns its id, so the send tests
// drive exactly the wiring the model does rather than a registry the test assembled by hand.
func openTestConsole(t *testing.T, ctx context.Context, command string) int {
	t.Helper()
	res, err := NewConsoleOpen(t.TempDir(), nil).Execute(ctx, consoleOpenCall("open", command, 200))
	if err != nil || res.IsError {
		t.Fatalf("console_open(%q) = %q (err=%v)", command, res.Content, err)
	}
	ids := console.FromContext(ctx).OpenIDs()
	if len(ids) == 0 {
		t.Fatalf("console_open(%q) registered nothing", command)
	}
	return ids[len(ids)-1]
}

// consoleOwnerCase is one cell of the ownership table console_send, console_read and
// console_close are each driven through: who opened the target Console, who names its id, and
// whether that caller may reach it (F-37, ADR 0059 §6).
type consoleOwnerCase struct {
	name string
	// opener is the engine-minted owner key the target Console is opened under.
	opener string
	// caller is the owner key of the run that names the target's id.
	caller string
	// reachable says whether that caller may drive it: only the run that opened it may.
	reachable bool
	// callerHasOwn opens one Console under the caller's own key first, so the refusal's open
	// list is checked to name what the caller holds — and not the id it may not address.
	callerHasOwn bool
}

// consoleOwnerCases is the shared table: a run reaches what it opened, and every other pairing —
// a sibling delegation, the top level naming a delegation's Console, a delegation naming the top
// level's — is refused as though the id had never been issued.
//
// Shared with console_read_test.go and console_close_test.go, whose subject is the same one: all
// three tools address a Console through the one lookup in console_common.go.
func consoleOwnerCases() []consoleOwnerCase {
	return []consoleOwnerCase{
		{name: "the run that opened it", opener: "run-1", caller: "run-1", reachable: true},
		{name: "a sibling delegation", opener: "run-1", caller: "run-2"},
		{name: "a sibling holding one of its own", opener: "run-1", caller: "run-2", callerHasOwn: true},
		{name: "the top level naming a delegation's", opener: "run-1", caller: ""},
		{name: "a delegation naming the top level's", opener: "", caller: "run-1"},
	}
}

// checkConsoleOwnership drives one cell of consoleOwnerCases through the tool call the test
// supplies: the run that opened the Console gets an ordinary result, and any other caller gets
// the SAME refusal an id that was never issued produces, listing only its own Consoles — the
// target's existence and its owner's shells are both none of that caller's business.
func checkConsoleOwnership(
	t *testing.T,
	testCase consoleOwnerCase,
	call func(ctx context.Context, id int) (domain.ToolResult, error),
) {
	t.Helper()
	base, _ := consoleTestCtx(t)
	callerCtx := domain.WithConsoleOwner(base, testCase.caller)
	callerOpen := "none"
	if testCase.callerHasOwn {
		callerOpen = strconv.Itoa(openTestConsole(t, callerCtx, "cat"))
	}
	target := openTestConsole(t, domain.WithConsoleOwner(base, testCase.opener), "cat")

	res, err := call(callerCtx, target)

	if err != nil {
		t.Fatalf("Execute err = %v, want nil (ownership is a result, not a Go error)", err)
	}
	if testCase.reachable {
		if res.IsError {
			t.Fatalf("result = %q, want the run that opened console %d to reach it", res.Content, target)
		}
		return
	}
	want := fmt.Sprintf("no console %d (open consoles: %s)", target, callerOpen)
	if !res.IsError || res.Content != want {
		t.Errorf("result = %q (isError=%v), want the unknown-id refusal %q", res.Content, res.IsError, want)
	}
}

func TestConsoleSend_Markers(t *testing.T) {
	t.Parallel()
	tool := NewConsoleSend()

	if tool.Name() != "console_send" {
		t.Errorf("Name() = %q, want console_send", tool.Name())
	}
	if tool.ReadOnly() {
		t.Error("console_send must be write-capable (ReadOnly()==false)")
	}
	if !domain.IsSubprocessTool(tool) {
		t.Error("console_send must carry the Subprocess marker: the shell on the other end executes what it is sent")
	}
	if !domain.IsDefaultOff(tool) {
		t.Error("console_send must ship default-off (ADR 0057)")
	}
}

// TestConsoleSend_RunsTheLineAndReportsLiveness is the family's whole point in one call: the line
// reaches a shell that is still the shell the previous Turn opened, and the result says it is
// still there to talk to.
func TestConsoleSend_RunsTheLineAndReportsLiveness(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	id := openTestConsole(t, ctx, "sh")

	res, err := NewConsoleSend().Execute(ctx, consoleSendCall("c1", id, "echo hi", false, 2000))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("send produced an error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "hi") {
		t.Errorf("result = %q, want it to carry the command's output", res.Content)
	}
	if !strings.HasSuffix(res.Content, consoleAliveStatus) {
		t.Errorf("result = %q, want it to end with %q", res.Content, consoleAliveStatus)
	}
}

// TestConsoleSend_RawSendsNoNewline pins the raw rule from both sides: raw bytes are typed but not
// entered, so nothing runs until a later newline arrives — which is also how a lone control
// character (a JSON \u0003 for Ctrl-C) is sent without pressing Enter behind it.
func TestConsoleSend_RawSendsNoNewline(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	id := openTestConsole(t, ctx, "sh")
	tool := NewConsoleSend()

	typed, err := tool.Execute(ctx, consoleSendCall("c1", id, "echo $((40+2))", true, 500))
	if err != nil {
		t.Fatalf("raw Execute err = %v, want nil", err)
	}
	if strings.Contains(typed.Content, "42") {
		t.Fatalf("raw send result = %q, want the line typed but NOT run", typed.Content)
	}

	entered, err := tool.Execute(ctx, consoleSendCall("c2", id, "\n", true, 2000))
	if err != nil {
		t.Fatalf("newline Execute err = %v, want nil", err)
	}
	if !strings.Contains(entered.Content, "42") {
		t.Errorf("result after the newline = %q, want the line to have run", entered.Content)
	}
}

// TestConsoleSend_UnknownIDNamesTheOpenConsoles covers the refusal a model actually meets: an id
// from before a /new, or one it simply invented. It is an error RESULT naming what IS open, so
// the next call can be right.
func TestConsoleSend_UnknownIDNamesTheOpenConsoles(t *testing.T) {
	t.Parallel()
	ctx, _ := consoleTestCtx(t)

	res, err := NewConsoleSend().Execute(ctx, consoleSendCall("c1", 99, "hi", false, 10))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil (an unknown id is a result)", err)
	}
	if !res.IsError || res.Content != "no console 99 (open consoles: none)" {
		t.Errorf("result = %q (isError=%v), want the unknown-id refusal", res.Content, res.IsError)
	}
}

func TestConsoleSend_UnknownIDListsTheConsolesThatAreOpen(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	id := openTestConsole(t, ctx, "sh")

	res, err := NewConsoleSend().Execute(ctx, consoleSendCall("c1", id+7, "hi", false, 10))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if want := fmt.Sprintf("(open consoles: %d)", id); !res.IsError || !strings.Contains(res.Content, want) {
		t.Errorf("result = %q, want it to contain %q", res.Content, want)
	}
}

func TestConsoleSend_MissingInputIsAnErrorResult(t *testing.T) {
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	call := domain.ToolCall{ID: "c1", Tool: "console_send", Arguments: []byte(`{"id":1}`)}

	res, err := NewConsoleSend().Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "input is required") {
		t.Errorf("result = %q, want the missing-input refusal", res.Content)
	}
}

// TestConsoleSend_ApprovalScope pins the one line the approval pane gains: what the call reaches,
// derived from the arguments alone, and never more than one line.
func TestConsoleSend_ApprovalScope(t *testing.T) {
	t.Parallel()
	tool := NewConsoleSend()

	cases := []struct {
		name string
		args string
		want string
	}{
		{"numeric id", `{"id":3,"input":"ls"}`, "→ console 3"},
		{"quoted id", `{"id":"3","input":"ls"}`, "→ console 3"},
		{"no id", `{"input":"ls"}`, ""},
		{"unusable id", `{"id":"three","input":"ls"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tool.ApprovalScope(domain.ToolCall{ID: "c1", Tool: "console_send", Arguments: []byte(tc.args)})
			if got != tc.want {
				t.Errorf("ApprovalScope(%s) = %q, want %q", tc.args, got, tc.want)
			}
			if strings.Contains(got, "\n") {
				t.Errorf("ApprovalScope(%s) = %q, want a single line", tc.args, got)
			}
		})
	}
}

// TestConsoleSend_QuotedIDAddressesTheSameConsole is the decode rule end to end: a model that
// quotes its numbers still reaches the console it means, rather than spending a Turn learning JSON.
func TestConsoleSend_QuotedIDAddressesTheSameConsole(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	id := openTestConsole(t, ctx, "sh")
	call := domain.ToolCall{
		ID:        "c1",
		Tool:      "console_send",
		Arguments: []byte(fmt.Sprintf(`{"id":"%d","input":"echo quoted-ok","wait_ms":2000}`, id)),
	}

	res, err := NewConsoleSend().Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError || !strings.Contains(res.Content, "quoted-ok") {
		t.Errorf("result = %q (isError=%v), want the quoted id to address console %d", res.Content, res.IsError, id)
	}
}

// TestConsoleSend_DenialStopLabelNamesTheWritableRoots proves the Console family's fence label
// travels with the box the Console was opened under: a confined Console whose output carries an
// OS-denial signature is stopped, and the line the model then reads NAMES the roots it may write
// to — the workspace and the session scratch dir among them — instead of describing them.
func TestConsoleSend_DenialStopLabelNamesTheWritableRoots(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	scratch := t.TempDir()
	box := domain.ConfinementBox{WorkspaceRoot: t.TempDir(), WritablePaths: []string{scratch}}
	ctx = domain.WithConfinement(ctx, domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      box,
	})
	id := openTestConsole(t, ctx, "sh")

	res, err := NewConsoleSend().Execute(ctx,
		consoleSendCall("c1", id, `echo "touch: /etc/f: Operation not permitted" >&2`, false, 5000))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !strings.Contains(res.Content, confinementDenialStopLabel(box)) {
		t.Errorf("send result = %q, want the stop label naming %s and %s", res.Content, box.WorkspaceRoot, scratch)
	}
}

// TestConsoleSend_AnswersOnlyToTheRunThatOpenedIt is the sharpest end of F-37: driving a shell is
// the privilege the owner key exists to hold, so a run that names a Console it did not open is
// refused in the words an id that never existed gets.
func TestConsoleSend_AnswersOnlyToTheRunThatOpenedIt(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	for _, testCase := range consoleOwnerCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			checkConsoleOwnership(t, testCase, func(ctx context.Context, id int) (domain.ToolResult, error) {
				return NewConsoleSend().Execute(ctx, consoleSendCall("c1", id, "echo hi", false, 500))
			})
		})
	}
}
