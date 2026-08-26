package agent

// The Console registry as LIVE HOST STATE on the Agent (ADR 0059): that a tool call can reach it
// through the context seam at all, that one engine has exactly one of them however deep the
// delegation nests, that ownership is what a delegation's end reaps, and that none of it ever
// reaches a Session snapshot.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ---------------------------------------------------------------------------
// Fakes & helpers
// ---------------------------------------------------------------------------

// consoleOpener is the fake tool this file drives the engine with: on every call it opens one
// `sh` Console through the two things dispatch is supposed to have installed — the registry seam
// and the spawning call id — and records what it saw. Opening through the CONTEXT rather than
// through a held registry is the whole point: it is the path a real console tool takes, so a
// dispatch that forgot to install either would fail here rather than in item 4.
type consoleOpener struct {
	registries []*console.Registry // the registry each call found on its context, in call order
	consoles   []*console.Console  // the Console each call opened
	owners     []string            // the spawn call id each call stamped on it
	failure    error               // the first thing that went wrong inside a call, if any
}

// tool returns the opener as a read-only Tool, so the fake runs in every mode without an Approver.
func (o *consoleOpener) tool() domain.Tool {
	return fakeTool{name: "open_console", readOnly: true, execute: o.execute}
}

// execute opens one Console and reports it as an ordinary result. A failure is recorded rather
// than returned: a Go error out of a tool cuts the Turn short, and the test wants the Exchange to
// finish so it can assert on what the engine did afterwards.
func (o *consoleOpener) execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	registry := console.FromContext(ctx)
	if registry == nil {
		o.failure = errors.New("the tool call's context carries no console registry")
		return domain.ToolResult{CallID: call.ID, Content: "no registry"}, nil
	}
	o.registries = append(o.registries, registry)

	owner := domain.SpawnCallIDFromContext(ctx)
	opened, err := registry.Open(console.OpenSpec{Owner: owner, Command: "sh", Argv: []string{"sh"}})
	if err != nil {
		o.failure = fmt.Errorf("open a console: %w", err)
		return domain.ToolResult{CallID: call.ID, Content: "open failed"}, nil
	}
	o.consoles = append(o.consoles, opened)
	o.owners = append(o.owners, owner)
	return domain.ToolResult{CallID: call.ID, Content: fmt.Sprintf("console %d opened", opened.ID)}, nil
}

// openConsoleScripts returns the scripted stream pairs for n Exchanges that each ask for one
// open_console call and then finish. One scriptedResponder serves a whole tree — a child speaks
// over its parent's Upstream — so the scripts of every Agent in a test come from this one list.
func openConsoleScripts(n int) [][]provider.Delta {
	scripts := make([][]provider.Delta, 0, 2*n)
	for i := range n {
		scripts = append(scripts,
			toolCallScript(fmt.Sprintf("c%d", i), "open_console", `{}`),
			contentScript("opened"))
	}
	return scripts
}

// runOneExchange drives a full Exchange on a, failing the test if it does not complete.
func runOneExchange(t *testing.T, a *Agent, text string) {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("Run status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}
}

// newConsoleAgent builds a top-level Agent wired to a fresh opener, scripted for n Exchanges, and
// registers the teardown that stops whatever shells the test leaves running.
func newConsoleAgent(t *testing.T, n int) (*Agent, *consoleOpener) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a Console needs a pseudo-terminal; Windows is a later plan (ADR 0059)")
	}

	opener := &consoleOpener{}
	a, err := newAgent(configWithTools(&recordingSink{}, opener.tool()),
		&scriptedResponder{scripts: openConsoleScripts(n)})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(a.consoles.CloseAll)
	return a, opener
}

// assertOpenIDs fails when the registry does not hold exactly these ids.
func assertOpenIDs(t *testing.T, registry *console.Registry, want ...int) {
	t.Helper()
	got := registry.OpenIDs()
	if len(got) != len(want) {
		t.Fatalf("open console ids = %v, want %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("open console ids = %v, want %v", got, want)
		}
	}
}

// assertOpenedCleanly fails when a scripted Exchange did not reach the opener, or the opener hit
// something on the way.
func assertOpenedCleanly(t *testing.T, opener *consoleOpener, wantCalls int) {
	t.Helper()
	if opener.failure != nil {
		t.Fatalf("the fake tool could not open its Console: %v", opener.failure)
	}
	if len(opener.consoles) != wantCalls {
		t.Fatalf("the fake tool opened %d Consoles, want %d", len(opener.consoles), wantCalls)
	}
}

// ---------------------------------------------------------------------------
// The seam and the sharing
// ---------------------------------------------------------------------------

// TestDispatchCarriesTheConsoleRegistryAndTheOwner is the seam itself: a tool call reaches the
// engine's registry through the context, and the spawn call id rides beside it so the Console it
// opens is stamped with the run that owns it — empty at the top level, the delegation's call id
// under one.
func TestDispatchCarriesTheConsoleRegistryAndTheOwner(t *testing.T) {
	t.Parallel()

	parent, opener := newConsoleAgent(t, 2)
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	runOneExchange(t, parent, "open one at the top level")
	runOneExchange(t, child, "open one under the delegation")

	assertOpenedCleanly(t, opener, 2)
	for i, registry := range opener.registries {
		if registry != parent.consoles {
			t.Errorf("call %d found registry %p on its context, want the engine's %p",
				i, registry, parent.consoles)
		}
	}
	if want := []string{"", "call_sub"}; opener.owners[0] != want[0] || opener.owners[1] != want[1] {
		t.Errorf("console owners = %v, want %v — the top level owns nothing, the delegation owns its own",
			opener.owners, want)
	}
}

// TestChildSharesTheParentsConsoleRegistry pins the sharing by handle (subagent.go): one engine
// has ONE set of Consoles, which is what makes the cap of four mean four across a whole tree
// rather than four per delegation. A copy here would give every delegation its own four and a
// child's Close nothing to reap.
func TestChildSharesTheParentsConsoleRegistry(t *testing.T) {
	t.Parallel()

	parent, err := newAgent(baseConfig(&recordingSink{}), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	grandchild, err := child.newChildAgent("call_sub_sub", "the nested task", "")
	if err != nil {
		t.Fatalf("newChildAgent (depth 2): %v", err)
	}

	if child.consoles != parent.consoles {
		t.Errorf("child registry = %p, want the parent's %p", child.consoles, parent.consoles)
	}
	if grandchild.consoles != parent.consoles {
		t.Errorf("depth-2 registry = %p, want the top-level %p", grandchild.consoles, parent.consoles)
	}
}

// ---------------------------------------------------------------------------
// The three close sites
// ---------------------------------------------------------------------------

// TestDelegationEndClosesOnlyItsOwnConsoles is the ownership contract (ADR 0059 §6): the child's
// Close reaps the Consoles its delegation opened and leaves the parent's running. sub_agent's
// deferred sub.Close() is the production caller — normal, error, cancelled and faulted exits all
// arrive here.
func TestDelegationEndClosesOnlyItsOwnConsoles(t *testing.T) {
	t.Parallel()

	parent, opener := newConsoleAgent(t, 2)
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	runOneExchange(t, parent, "open one at the top level")
	runOneExchange(t, child, "open one under the delegation")
	assertOpenedCleanly(t, opener, 2)
	assertOpenIDs(t, parent.consoles, opener.consoles[0].ID, opener.consoles[1].ID)

	if err := child.Close(); err != nil {
		t.Fatalf("child Close: %v", err)
	}

	assertOpenIDs(t, parent.consoles, opener.consoles[0].ID)
	if !opener.consoles[0].Alive() {
		t.Error("the top-level Console died with the delegation, want it left running")
	}
	if opener.consoles[1].Alive() {
		t.Error("the delegation's Console is still alive after its delegation ended")
	}
}

// TestTopLevelCloseClosesEveryConsole is the engine-exit site: the top-level Agent owns nothing by
// call id — its id is empty — and is the last thing standing, so its Close takes everything down
// including the Consoles a delegation left behind.
func TestTopLevelCloseClosesEveryConsole(t *testing.T) {
	t.Parallel()

	parent, opener := newConsoleAgent(t, 2)
	child, err := parent.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	runOneExchange(t, parent, "open one at the top level")
	runOneExchange(t, child, "open one under the delegation")
	assertOpenedCleanly(t, opener, 2)

	if err := parent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	assertOpenIDs(t, parent.consoles)
	for i, opened := range opener.consoles {
		if opened.Alive() {
			t.Errorf("console %d is still alive after the engine closed", opener.consoles[i].ID)
		}
	}
}

// TestClearContextClosesEveryConsole is the one place Console lifetime diverges from the undo
// journal's (ADR 0059 §1): /new drops the history the Console ids live in, so leaving the
// processes running would leave shells nothing in the new session can name. The journal, by
// contrast, deliberately survives — `/undo` still reaches the writes of the forgotten Exchange.
func TestClearContextClosesEveryConsole(t *testing.T) {
	t.Parallel()

	a, opener := newConsoleAgent(t, 1)
	runOneExchange(t, a, "open one")
	assertOpenedCleanly(t, opener, 1)

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	assertOpenIDs(t, a.consoles)
	if opener.consoles[0].Alive() {
		t.Error("the Console survived /new, want it closed with the session it was named in")
	}
	if a.journal == nil {
		t.Error("the undo journal was dropped by ClearContext, want it to survive (ADR 0051)")
	}
}

// ---------------------------------------------------------------------------
// Never serialized
// ---------------------------------------------------------------------------

// TestSnapshotCarriesNoConsoleState is the live-host-state half of ADR 0059 §1: running processes
// cannot be written into a session file, so the snapshot says nothing about them and taking one
// leaves them running — while the RESTORE, a session boundary, closes them.
func TestSnapshotCarriesNoConsoleState(t *testing.T) {
	t.Parallel()

	a, opener := newConsoleAgent(t, 1)
	runOneExchange(t, a, "open one")
	assertOpenedCleanly(t, opener, 1)

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// The KEYS are the assertion, not the payload: the conversation legitimately quotes the tool's
	// name and result, while a serialized registry could only ever arrive as a field of its own.
	var state map[string]json.RawMessage
	if err := json.Unmarshal(snap.State, &state); err != nil {
		t.Fatalf("decode the snapshot state: %v", err)
	}
	for key := range state {
		if strings.Contains(strings.ToLower(key), "console") {
			t.Errorf("the snapshot carries a %q field, want the console registry withheld", key)
		}
	}

	// Taking the snapshot is a pure read: the Console it says nothing about is still running.
	if !opener.consoles[0].Alive() {
		t.Error("the Console died when the session was snapshotted, want it untouched by session state")
	}

	// Restoring it is a session boundary, so the Console goes: its id lives in the history the
	// swap drops (ADR 0059 §1, amended 2026-08-26). The full contract is pinned by
	// TestRestoreSession_ClosesEveryConsoleOfTheOutgoingSession in restoresession_test.go.
	if err := a.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	assertOpenIDs(t, a.consoles)
}
