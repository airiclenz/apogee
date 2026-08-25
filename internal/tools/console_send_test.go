package tools

import (
	"context"
	"fmt"
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
