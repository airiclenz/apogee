package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

// consoleTestCtx returns a context carrying a fresh Console registry, closed when the test ends.
// Every Console tool reads its registry off the context (console.FromContext), which is what the
// engine installs on each dispatch, so this is the whole of the wiring a tool needs.
func consoleTestCtx(t *testing.T) (context.Context, *console.Registry) {
	t.Helper()
	registry := console.New()
	t.Cleanup(registry.CloseAll)
	return console.WithRegistry(context.Background(), registry), registry
}

func consoleOpenCall(id, command string, waitMS int) domain.ToolCall {
	return domain.ToolCall{
		ID:        id,
		Tool:      "console_open",
		Arguments: []byte(fmt.Sprintf(`{"command":%q,"wait_ms":%d}`, command, waitMS)),
	}
}

// skipWithoutPOSIXShell skips a test that needs a real `sh` behind a pseudo-terminal.
func skipWithoutPOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell under a pseudo-terminal; consoles are unsupported on Windows")
	}
}

func TestConsoleOpen_Markers(t *testing.T) {
	t.Parallel()
	tool := NewConsoleOpen(t.TempDir(), nil)

	if tool.Name() != "console_open" {
		t.Errorf("Name() = %q, want console_open", tool.Name())
	}
	if tool.ReadOnly() {
		t.Error("console_open must be write-capable (ReadOnly()==false)")
	}
	if !domain.IsSubprocessTool(tool) {
		t.Error("console_open must be a SubprocessTool: it starts a shell")
	}
	if !domain.IsDefaultOff(tool) {
		t.Error("console_open must ship default-off (ADR 0057)")
	}
}

// TestConsoleOpen_OpensAndNamesTheConsole pins the first line of the open result: the model
// addresses everything else in the family by the id it reads here, so the id and the command it
// belongs to have to be the first thing in the result.
func TestConsoleOpen_OpensAndNamesTheConsole(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, registry := consoleTestCtx(t)
	tool := NewConsoleOpen(t.TempDir(), nil)

	res, err := tool.Execute(ctx, consoleOpenCall("c1", "sh", 200))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("open produced an error result: %q", res.Content)
	}
	if first, _, _ := strings.Cut(res.Content, "\n"); first != "console 1 opened: sh" {
		t.Errorf("first line = %q, want %q", first, "console 1 opened: sh")
	}
	if ids := registry.OpenIDs(); !slices.Equal(ids, []int{1}) {
		t.Errorf("OpenIDs() = %v, want [1]", ids)
	}
}

// TestConsoleOpen_ExitedProgramIsReportedNotHidden covers the other half of the open result: a
// command that is over before the window closes reports its exit code, because "console 2 opened"
// on its own would tell the model it has something to talk to when it has not.
func TestConsoleOpen_ExitedProgramIsReportedNotHidden(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	tool := NewConsoleOpen(t.TempDir(), nil)

	res, err := tool.Execute(ctx, consoleOpenCall("c1", "exit 3", 2000))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !strings.Contains(res.Content, "exited with code 3") {
		t.Errorf("result = %q, want it to report the exit code", res.Content)
	}
}

// TestConsoleOpen_ScrubsCredentialsAndAsksForADumbTerminal asserts on the spec the tool hands the
// registry: the console's program runs in the operator's environment minus apogee's own key, and
// is told it is talking to a dumb terminal so it spends none of the model's context on escape
// sequences the Console strips anyway.
func TestConsoleOpen_ScrubsCredentialsAndAsksForADumbTerminal(t *testing.T) {
	// Not parallel: t.Setenv plus the package-level opener swap.
	t.Setenv("APOGEE_API_KEY", "sk-secret-value")
	t.Setenv("APOGEE_TEST_PROVIDER_KEY", "sk-configured-value")

	var captured console.OpenSpec
	restore := openConsole
	openConsole = func(registry *console.Registry, spec console.OpenSpec) (*console.Console, error) {
		captured = spec
		return registry.Open(spec)
	}
	t.Cleanup(func() { openConsole = restore })

	ctx, _ := consoleTestCtx(t)
	tool := NewConsoleOpen(t.TempDir(), []string{"APOGEE_TEST_PROVIDER_KEY"})

	if _, err := tool.Execute(ctx, consoleOpenCall("c1", "sh", 10)); err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}

	if !slices.Contains(captured.Env, consoleTermVar) {
		t.Errorf("child environment does not carry %q", consoleTermVar)
	}
	for _, entry := range captured.Env {
		if strings.HasPrefix(entry, "APOGEE_API_KEY=") || strings.HasPrefix(entry, "APOGEE_TEST_PROVIDER_KEY=") {
			t.Errorf("a credential reached the console's environment: %q", entry)
		}
	}
	if captured.Command != "sh" {
		t.Errorf("Command = %q, want the model's own line", captured.Command)
	}
	if captured.Owner != "" {
		t.Errorf("Owner = %q, want empty at the top level", captured.Owner)
	}
}

// TestConsoleOpen_StampsTheEngineMintedOwnerKey pins WHICH identity ownership rides on. The tool
// reads the engine-minted owner key, which is what a delegation's end reaps by; the spawn call id
// beside it is display identity the model chose, and two siblings of one Turn can carry the same
// one — so a context carrying only that owns nothing.
func TestConsoleOpen_StampsTheEngineMintedOwnerKey(t *testing.T) {
	// Not parallel: the package-level opener swap.
	cases := []struct {
		name  string
		stamp func(context.Context) context.Context
		want  string
	}{
		{
			name:  "the engine's owner key is what the Console records",
			stamp: func(ctx context.Context) context.Context { return domain.WithConsoleOwner(ctx, "run-7") },
			want:  "run-7",
		},
		{
			name:  "a spawn call id alone owns nothing",
			stamp: func(ctx context.Context) context.Context { return domain.WithSpawnCallID(ctx, "call_sub") },
			want:  "",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var captured console.OpenSpec
			restore := openConsole
			openConsole = func(registry *console.Registry, spec console.OpenSpec) (*console.Console, error) {
				captured = spec
				return registry.Open(spec)
			}
			t.Cleanup(func() { openConsole = restore })
			ctx, _ := consoleTestCtx(t)

			_, err := NewConsoleOpen(t.TempDir(), nil).Execute(testCase.stamp(ctx), consoleOpenCall("c1", "sh", 10))

			if err != nil {
				t.Fatalf("Execute err = %v, want nil", err)
			}
			if captured.Owner != testCase.want {
				t.Errorf("Owner = %q, want %q", captured.Owner, testCase.want)
			}
		})
	}
}

// TestConsoleOpen_RefusesAShellResolvingInsideTheWorkspace pins the exec fence at the one
// resolution site this tool has: an `sh` the model could have written is not an `sh`, so a PATH
// leading into the workspace is refused by name rather than executed.
func TestConsoleOpen_RefusesAShellResolvingInsideTheWorkspace(t *testing.T) {
	skipWithoutPOSIXShell(t)
	// Not parallel: t.Setenv.
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bin, "sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("PATH", bin)

	ctx, _ := consoleTestCtx(t)
	res, err := NewConsoleOpen(root, nil).Execute(ctx, consoleOpenCall("c1", "echo hi", 10))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil (a refused program is a result)", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "resolves inside") {
		t.Errorf("result = %q (isError=%v), want the exec fence refusal", res.Content, res.IsError)
	}
}

func TestConsoleOpen_WorkdirEscapeIsAnErrorResult(t *testing.T) {
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	call := domain.ToolCall{
		ID:        "c1",
		Tool:      "console_open",
		Arguments: []byte(`{"command":"pwd","workdir":"../../etc"}`),
	}

	res, err := NewConsoleOpen(t.TempDir(), nil).Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError {
		t.Errorf("result = %q, want a workdir escaping the root refused", res.Content)
	}
}

// TestConsoleOpen_ConfinementUnavailablePropagates is the fail-closed case: a Confinement handle
// carrying no Confiner is broken wiring, and opening a shell that then OUTLIVES the Turn is the
// last thing to do about it. The demotion reaches the loop as a Go error, as it does for terminal.
func TestConsoleOpen_ConfinementUnavailablePropagates(t *testing.T) {
	t.Parallel()
	ctx, registry := consoleTestCtx(t)
	ctx = domain.WithConfinement(ctx, domain.Confinement{
		Box: domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := NewConsoleOpen(t.TempDir(), nil).Execute(ctx, consoleOpenCall("c1", "sh", 10))

	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("Execute err = %v, want ErrConfinementUnavailable", err)
	}
	if ids := registry.OpenIDs(); len(ids) != 0 {
		t.Errorf("OpenIDs() = %v, want none — a refused confinement must start nothing", ids)
	}
}

// TestConsoleOpen_ConfinesThroughTheHandleOnContext proves the Confine handoff happens on the
// command the pseudo-terminal actually starts: the Prepare hook the tool builds is what the
// process layer calls before the pty is opened.
func TestConsoleOpen_ConfinesThroughTheHandleOnContext(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	confiner := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}}
	ctx = domain.WithConfinement(ctx, domain.Confinement{
		Confiner: confiner,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	res, err := NewConsoleOpen(t.TempDir(), nil).Execute(ctx, consoleOpenCall("c1", "sh", 100))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("open produced an error result: %q", res.Content)
	}
	if confiner.confineCount() != 1 {
		t.Errorf("Confine called %d times, want 1", confiner.confineCount())
	}
}

// TestConsoleOpen_RefusesPastTheCapAndNamesTheOpenConsoles pins the fixed cap of four (ADR 0059
// §6). The refusal names the ids, because a model that has lost track of its consoles needs to be
// told which one to close, not merely that it may not have another.
func TestConsoleOpen_RefusesPastTheCapAndNamesTheOpenConsoles(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	tool := NewConsoleOpen(t.TempDir(), nil)

	for open := 1; open <= console.MaxOpen; open++ {
		res, err := tool.Execute(ctx, consoleOpenCall(fmt.Sprintf("c%d", open), "sh", 10))
		if err != nil || res.IsError {
			t.Fatalf("open %d = %q (err=%v), want a live console", open, res.Content, err)
		}
	}

	res, err := tool.Execute(ctx, consoleOpenCall("c5", "sh", 10))

	if err != nil {
		t.Fatalf("Execute err = %v, want nil (the cap is a result)", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "1, 2, 3, 4") {
		t.Errorf("result = %q (isError=%v), want the cap refusal naming the open ids", res.Content, res.IsError)
	}
}

func TestConsoleOpen_EmptyCommandIsAnErrorResult(t *testing.T) {
	t.Parallel()
	ctx, _ := consoleTestCtx(t)
	call := domain.ToolCall{ID: "c1", Tool: "console_open", Arguments: []byte(`{"command":"   "}`)}

	res, err := NewConsoleOpen(t.TempDir(), nil).Execute(ctx, call)

	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "command is required") {
		t.Errorf("result = %q, want the missing-command refusal", res.Content)
	}
}
