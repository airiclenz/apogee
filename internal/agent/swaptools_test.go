package agent

// The one idle-only tool-registry mutator (ADR 0037, binding F): SwapTools replaces the resolved
// tool set whole at a quiescent boundary. What these pin is the SEAM's contract — the next
// dispatch and the next sub-agent spawn run on the new set, a swap mid-Exchange is refused with
// the idle-only class error and leaves the old set serving, and a nil registry is refused rather
// than silently disarming every tool.

import (
	"context"
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// toolRegistry builds a registry over tools, failing the test on a registration the domain type
// refuses (a duplicate or an empty name — the registry's own build-time validation).
func toolRegistry(t *testing.T, list ...domain.Tool) *domain.ToolRegistry {
	t.Helper()
	reg := domain.NewToolRegistry()
	for _, tool := range list {
		if err := reg.Register(tool); err != nil {
			t.Fatalf("Register(%q): %v", tool.Name(), err)
		}
	}
	return reg
}

// toolNames lists a registry's tools in registration order, so a test can assert WHICH set an
// Agent (or a spawned sub-agent) is running on.
func toolNames(reg *domain.ToolRegistry) []string {
	if reg == nil {
		return nil
	}
	names := make([]string, 0, len(reg.All()))
	for _, tool := range reg.All() {
		names = append(names, tool.Name())
	}
	return names
}

// TestSwapToolsMovesTheNextDispatchToTheNewSet: after a swap at idle the model's menu and the
// call resolution both come from the new registry — the tool that was swapped IN runs, the one
// swapped OUT is no longer offered and no longer resolves.
func TestSwapToolsMovesTheNextDispatchToTheNewSet(t *testing.T) {
	oldRan, newRan := 0, 0
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "old_tool", readOnly: true, ran: &oldRan, result: "old"})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "new_tool", `{}`),
		contentScript("done"),
	}}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	next := toolRegistry(t, fakeTool{name: "new_tool", readOnly: true, ran: &newRan, result: "new"})
	if err := a.SwapTools(next); err != nil {
		t.Fatalf("SwapTools: %v", err)
	}

	if _, found := a.lookupTool("old_tool"); found {
		t.Error("the swapped-out tool still resolves; SwapTools replaces the set, it does not merge")
	}
	if got := toolNames(a.tools); len(got) != 1 || got[0] != "new_tool" {
		t.Errorf("live tool set = %v, want [new_tool]", got)
	}
	if menu := a.toolMenu(); len(menu) != 1 || menu[0].Name != "new_tool" {
		t.Errorf("the model's tool menu = %+v, want only new_tool", menu)
	}

	res := runExchange(t, a, "use the new tool")

	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("Exchange status = %v, want complete", res.Status)
	}
	if newRan != 1 {
		t.Errorf("the swapped-in tool ran %d times, want 1", newRan)
	}
	if oldRan != 0 {
		t.Errorf("the swapped-out tool ran %d times, want 0", oldRan)
	}
}

// TestSwapToolsRefusedMidExchange: an Exchange left open by a cancel refuses the swap with the
// idle-only class error, and the session keeps serving the tools it had.
func TestSwapToolsRefusedMidExchange(t *testing.T) {
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "old_tool", readOnly: true, result: "old"})
	responder := blockingResponder{started: make(chan struct{})}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "slow"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	if _, err := a.Step(ctx); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !a.InExchange() {
		t.Fatal("the cancelled Exchange is not open; the refusal below would prove nothing")
	}

	before := a.tools
	err = a.SwapTools(toolRegistry(t, fakeTool{name: "new_tool", readOnly: true, result: "new"}))

	if !errors.Is(err, domain.ErrInputPending) {
		t.Errorf("SwapTools mid-Exchange err = %v, want it to match ErrInputPending", err)
	}
	if a.tools != before {
		t.Error("a refused SwapTools replaced the tool set")
	}
	if _, found := a.lookupTool("old_tool"); !found {
		t.Error("the old tool stopped resolving after a refused swap")
	}
}

// TestSwapToolsRefusesANilRegistry: nil is how CONSTRUCTION spells "no tools"; mid-session it is
// far more likely a dropped build result, so it is refused rather than silently disarming the
// session's tools.
func TestSwapToolsRefusesANilRegistry(t *testing.T) {
	t.Parallel()

	cfg := configWithTools(&recordingSink{}, fakeTool{name: "old_tool", readOnly: true, result: "old"})
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	if err := a.SwapTools(nil); !errors.Is(err, errSwapToolsMissingRegistry) {
		t.Errorf("SwapTools(nil) err = %v, want errSwapToolsMissingRegistry", err)
	}
	if _, found := a.lookupTool("old_tool"); !found {
		t.Error("a refused SwapTools disarmed the session's tools")
	}

	if err := a.SwapTools(domain.NewToolRegistry()); err != nil {
		t.Fatalf("SwapTools(empty registry): %v — an explicit empty set is the way to run tool-less", err)
	}
	if _, found := a.lookupTool("old_tool"); found {
		t.Error("the empty registry did not take effect")
	}
}

// TestSwapToolsIsSeenByTheNextSubAgentSpawn: the child derives its tools from the parent's LIVE
// registry at spawn (defaultSubAgentTools), so a sub-agent spawned after a swap runs on the new
// set — and still never on more than the parent has.
func TestSwapToolsIsSeenByTheNextSubAgentSpawn(t *testing.T) {
	t.Parallel()

	cfg := configWithTools(&recordingSink{}, fakeTool{name: "old_tool", readOnly: true, result: "old"})
	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	before, err := a.newChildAgent("call_sub")
	if err != nil {
		t.Fatalf("newChildAgent before the swap: %v", err)
	}
	if got := toolNames(before.tools); len(got) != 1 || got[0] != "old_tool" {
		t.Fatalf("pre-swap child tools = %v, want [old_tool]", got)
	}

	next := toolRegistry(t,
		fakeTool{name: "new_tool", readOnly: true, result: "new"},
		fakeTool{name: "other_tool", readOnly: true, result: "other"},
	)
	if err := a.SwapTools(next); err != nil {
		t.Fatalf("SwapTools: %v", err)
	}

	after, err := a.newChildAgent("call_sub")
	if err != nil {
		t.Fatalf("newChildAgent after the swap: %v", err)
	}

	got := toolNames(after.tools)
	if len(got) != 2 || got[0] != "new_tool" || got[1] != "other_tool" {
		t.Errorf("post-swap child tools = %v, want [new_tool other_tool]", got)
	}
	if names := toolNames(before.tools); len(names) != 1 || names[0] != "old_tool" {
		t.Errorf("the already-spawned child's tools moved under it: %v", names)
	}
}
