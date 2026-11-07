package agent

// Swapping the Agent's tool set mid-session (ADR 0037, binding F). Every configuration change
// that adds, removes or reconfigures a TOOL — the web-search endpoint appearing or disappearing,
// an MCP server set reconnecting — resolves to the same physical act: the host builds a fresh
// tool registry and hands it over whole. SwapTools is the single door for that act, deliberately
// generic so no second registry-mutation path (a per-tool setter, a registry mutator, a
// re-construction backdoor) has to exist beside it.
//
// The engine never learns WHY the set changed: which tools a configuration yields is a
// composition-root question (cmd/apogee builds the host tools and folds the MCP tools in), so
// the engine applies what it is handed — the same wire-silent posture Rebind takes for the
// per-model bindings (ADR 0031).

import (
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

var (
	// errSwapToolsMissingRegistry refuses a nil registry. A nil registry is how CONSTRUCTION
	// spells "this Agent has no tools" (resolveTools), but it is not a thing a host asks for
	// mid-session: an empty registry says the same thing explicitly, and refusing nil keeps a
	// dropped build result from silently disarming every tool the session had.
	errSwapToolsMissingRegistry = errors.New("apogee: SwapTools: a tool registry is required (pass an empty registry for a tool-less Agent)")

	// errSwapToolsInExchange refuses a swap while a Step or Run is in progress or an Exchange is
	// open. It WRAPS domain.ErrInputPending so the
	// idle-only class stays matchable with errors.Is, and adds the reason because this refusal is
	// user-facing: the settings surface renders it on the row whose edit could not be applied.
	errSwapToolsInExchange = fmt.Errorf("%w: the tool set can only be swapped between runs", domain.ErrInputPending)
)

// SwapTools replaces the Agent's resolved tool set outright with registry — the ONE door for
// every mid-session tool-set change (ADR 0037, binding F): the web-search endpoint being enabled
// or disabled, and the MCP reconnect that rebuilds host tools plus the folded MCP tools. Callers
// build the WHOLE set, exactly as the composition root builds it at construction; this is a
// replacement, never a merge, so a tool absent from registry is gone from the session.
//
// Idle-only, like Rebind and ClearContext: it refuses while a Step or Run is in progress or an
// Exchange is open (errSwapToolsInExchange, which matches domain.ErrInputPending), and the host
// applies a change observed mid-run at the terminal boundary — or lets the user re-commit the edit,
// since the value is already persisted. Unlike the other idle-only doors it may be CALLED from any
// goroutine: the loop's tool reads (the per-request menu in toolMenu, the per-call resolution in
// toolByName) all run inside Step or Run, which hold stepMu for the whole call, and this takes
// stepMu with TryLock. A held lock is a Step in progress and is refused at once, without waiting
// for it; a free one is held across the inExchange read and the commit, so no Step can start in
// between, and the hot path itself still takes no lock. CanSwapTools asks the same question
// without swapping.
//
// Validate-then-commit: the registry is checked whole — non-nil, and every entry a real tool with
// a non-empty, unique name — before anything is mutated, so a malformed set leaves the session on
// the tools it had.
//
// Sub-agents see the new set from the next spawn: newChildAgent derives the child's tools from
// this Agent's LIVE registry at spawn (defaultSubAgentTools reads a.tools, narrowing it — never
// expanding, ADR 0005), and a spawn happens mid-Exchange on the worker goroutine, where a swap is
// refused. So the two can never interleave: a sub-agent spawned after a swap runs on the new set,
// one already mid-flight keeps the set it was spawned with.
//
// What stands: everything else. The conversation and Turn counters, the autonomy mode, session
// approvals, the confinement flag, the per-model bindings and the model profile all outlive a
// tool-set change — including the profile's roster axis, whose deltas this set is expected to
// already carry: the host that assembles a registry is the layer that folds its own roster in
// (ADR 0057), and the engine's applyRoster stands down for the rest of the session once this door
// has been taken. Config.Tools stays the immutable construction seed (only resolveTools reads
// it), so a swapped Agent's live tools are a.tools alone.
func (a *Agent) SwapTools(registry *domain.ToolRegistry) error {
	release, err := a.holdIdleForSwap()
	if err != nil {
		return err
	}
	defer release()
	if registry == nil {
		return errSwapToolsMissingRegistry
	}
	seen := make(map[string]bool)
	for i, t := range registry.All() {
		if t == nil {
			return fmt.Errorf("%w: SwapTools: registry entry %d is nil", domain.ErrInvalidTool, i)
		}
		name := t.Name()
		if name == "" {
			return fmt.Errorf("%w: SwapTools: registry entry %d has an empty name", domain.ErrInvalidTool, i)
		}
		if seen[name] {
			return fmt.Errorf("%w: SwapTools: %q", domain.ErrDuplicateTool, name)
		}
		seen[name] = true
	}

	// Commit — from here on nothing can fail.
	a.tools = registry
	// And the set is the HOST's from here on, so the engine stops composing one of its own: a model
	// switch re-runs the roster ladder over the set it ASSEMBLED (applyRoster, ADR 0057), and doing
	// that under a host-supplied registry would rebuild the built-in menu over the top of whatever
	// the host folded in — every MCP tool in this very registry among it. The flag is never set back:
	// once the host has taken the set over, it owns it for the rest of the session and folds its own
	// roster deltas in where it builds.
	a.ownsToolSet = false
	return nil
}

// CanSwapTools reports whether SwapTools would accept a registry now: nil at an idle boundary, and
// errSwapToolsInExchange (matching domain.ErrInputPending) while a Step or Run is in progress or an
// Exchange is open — the same refusal, read the same way under stepMu. It swaps nothing, so a host
// can ask before doing expensive work whose only use is a swap (dialling MCP servers). The answer is
// a snapshot: a Step may start the moment it returns, which is why SwapTools checks again itself.
func (a *Agent) CanSwapTools() error {
	release, err := a.holdIdleForSwap()
	if err != nil {
		return err
	}
	release()
	return nil
}

// holdIdleForSwap takes stepMu without waiting and checks the boundary under it: a held lock (a Step
// or Run in progress) or an open Exchange is errSwapToolsInExchange with nothing held, and an idle
// boundary hands back the release the caller runs once it is done with that boundary.
func (a *Agent) holdIdleForSwap() (release func(), err error) {
	if !a.stepMu.TryLock() {
		return nil, errSwapToolsInExchange
	}
	if a.turns.inExchange {
		a.stepMu.Unlock()
		return nil, errSwapToolsInExchange
	}
	return a.stepMu.Unlock, nil
}
