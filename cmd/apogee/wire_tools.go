package main

// The tool-set seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// What the session's tools are made of and how they change: the live holder that owns the registry
// the engine is running on — re-pointing a registered tool where it can, going through the single
// SwapTools door where it must (ADR 0037 binding F) — plus the startup builders behind it and the
// registry assembly that folds the discovered MCP tools onto the built-in set. Turning a
// `mechanisms:` config block into the ID list the engine arms is mechanisms.ResolveEnabled's job
// (ADR 0015 §1), which every Driver reaches directly.

import (
	"fmt"
	"os"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// liveTools is the tool set the session is running on. It exists because two different kinds of
// change can reach the tools mid-session and only one of them needs the engine: re-pointing a tool
// that is already registered is a write on the tool itself (the registry holds the pointer, so the
// loop resolves the same object), while adding or removing a tool is a whole-registry replacement
// that has to go through Agent.SwapTools — the single door of ADR 0037 binding F.
//
// Holding the CURRENT registry is what keeps the first kind honest after the second has happened: a
// swap installs a new registry on the Agent, and a root that kept looking up tools in the old one
// would be re-pointing objects nothing dispatches to any more.
//
// The mutex guards the pointer for the same reason lateEngine's does: the writes come from the
// Update goroutine (the pane's keypress) and the tools themselves are read on the loop's worker
// goroutine, which reaches them through the Agent rather than through this holder.
type liveTools struct {
	mu      sync.Mutex
	current *apogee.ToolRegistry
	// spec is what the live set was BUILT from. It is remembered because a rebuild driven by
	// something else entirely — a reconnect to another set of MCP servers — must carry the
	// configuration this session is actually on, not the one it launched with: rebuilding from the
	// startup values would quietly revert a search-endpoint edit made an hour earlier, hand back a
	// tool the user switched off, or re-open a host the operator closed.
	spec toolSetSpec

	// build assembles the whole set as this session would have assembled it at startup, for a given
	// spec — host tools plus the MCP tools that are connected now.
	build func(spec toolSetSpec) *apogee.ToolRegistry
}

// toolSetSpec is the configuration one live tool set is built from: the web-search endpoint, the
// roster of built-in tools taken off the menu, the bound model's own roster axis, and the two
// `url-safety:` host lists. They travel as one value because every door below moves exactly ONE of
// them and has to hand the rest on untouched — carrying them as separate arguments would make the
// adjacent name lists an argument order away from silently applying a deny list as an allow list.
type toolSetSpec struct {
	// endpoint is the web-search endpoint the set's web_search tool was built with.
	endpoint string

	// disabled is the `tools.disabled:` roster the set was pruned by.
	disabled []string

	// roster is the bound model's `tools:` profile axis the set was composed under — the most
	// specific rung of the roster ladder (ADR 0057). It belongs to the SET rather than to any one
	// tool for the same reason the host lists below do: which tools exist is what the set IS, and no
	// tool exposes a setter for it, so a model whose profile lifts or drops a tool means building
	// again.
	roster domain.ToolRosterDelta

	// allowHosts and denyHosts are the `url-safety:` host lists the set's URLGuard was built from.
	// They belong to the SET rather than to any one tool: the guard is handed to every network tool
	// at construction and none of them has a setter for it, so moving a list means building again.
	allowHosts []string
	denyHosts  []string
}

// newLiveTools holds the registry the session was constructed with, the spec it was built from, and
// the recipe for another.
func newLiveTools(current *apogee.ToolRegistry, spec toolSetSpec,
	build func(spec toolSetSpec) *apogee.ToolRegistry) *liveTools {
	return &liveTools{current: current, spec: spec, build: build}
}

// setSearchEndpoint moves web_search to endpoint. While the tool is registered — the ordinary case,
// since the built-in set always carries it (an "off" endpoint registers a tool that declines
// gracefully rather than no tool at all) — the move is a write on the tool the registry already
// holds: nothing is rebuilt, nothing is swapped, and the change is in force for the next call even
// mid-run, which is the whole point of the tool owning a setter.
//
// The swap door is the fallback for a session whose registry has NO web_search to re-point — a set
// narrowed by an earlier swap, or a registry an embedder assembled by hand. Then the endpoint is
// part of the set's identity, so the set is rebuilt and handed to the engine; being idle-only, that
// path can be refused mid-run, and the refusal is what the row reports over a value it has already
// persisted (binding A).
func (t *liveTools) setSearchEndpoint(endpoint string, engine settingsEngine) error {
	if ws := t.webSearch(); ws != nil {
		ws.SetEndpoint(endpoint)
		t.mu.Lock()
		t.spec.endpoint = endpoint
		t.mu.Unlock()
		return nil
	}
	spec := t.built()
	spec.endpoint = endpoint
	return t.rebuildWith(spec, engine)
}

// setDisabled moves the session onto another `tools.disabled:` roster. Unlike the search endpoint
// above there is no tool to re-point: which tools EXIST is the set's identity, so this is always the
// swap door (ADR 0037 binding F) — build the set the roster describes, hand it to the engine, and the
// next request's tool list is offered from it. Being idle-only, SwapTools can refuse mid-run, and the
// refusal lands on the row over a value already persisted; re-committing retries it.
func (t *liveTools) setDisabled(disabled []string, engine settingsEngine) error {
	spec := t.built()
	spec.disabled = disabled
	return t.rebuildWith(spec, engine)
}

// setProfileRoster moves the session onto the roster axis of the model it is now bound to — the
// profile's `tools:` deltas (ADR 0057). It is the swap door for setDisabled's reason rather than a
// write on a tool: the roster decides which tools EXIST, which is the set's identity (ADR 0037
// binding F), so the set is built again under the new axis and handed to the engine whole. It is
// the composition root's half of the ADR's Bounds — the engine's own re-compose seam stands down
// under the registry this root injects, so the host that assembled the set is the one that folds
// the deltas in. Being idle-only, SwapTools can refuse mid-run, and then nothing has moved.
func (t *liveTools) setProfileRoster(roster domain.ToolRosterDelta, engine settingsEngine) error {
	spec := t.built()
	spec.roster = roster
	return t.rebuildWith(spec, engine)
}

// setAllowHosts moves the session onto another `url-safety.allow-hosts:` list, and setDenyHosts onto
// another deny list. Both are the swap door for the roster's reason rather than the endpoint's: the
// guard is built WITH the set (registryWithMCP hands one URLGuard to every network tool) and no tool
// exposes a setter for it, so which hosts are reachable is part of the set's identity. Being
// idle-only, SwapTools can refuse mid-run, and the refusal lands on the row over a value already
// persisted; re-committing retries it.
func (t *liveTools) setAllowHosts(hosts []string, engine settingsEngine) error {
	spec := t.built()
	spec.allowHosts = hosts
	return t.rebuildWith(spec, engine)
}

// setDenyHosts moves the session onto another `url-safety.deny-hosts:` list — setAllowHosts above
// carries the reasoning both share.
func (t *liveTools) setDenyHosts(hosts []string, engine settingsEngine) error {
	spec := t.built()
	spec.denyHosts = hosts
	return t.rebuildWith(spec, engine)
}

// rebuild reassembles the whole set as this session would assemble it NOW and hands it to the engine
// — the door a change to the tool SET itself goes through when the change is not about any one tool's
// configuration (ADR 0037 binding F). Today that is one caller: a reconnect to another set of MCP
// servers, whose tools are part of the set's identity rather than a field on a tool already in it.
func (t *liveTools) rebuild(engine settingsEngine) error {
	return t.rebuildWith(t.built(), engine)
}

// built reports the spec the live set was assembled from, which is what a rebuild driven by
// something else entirely must carry forward rather than re-derive from the startup snapshot.
func (t *liveTools) built() toolSetSpec {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.spec
}

// rebuildWith is the swap every door shares: build, hand to the engine, and become the live set only
// once the engine has taken it. Being idle-only, SwapTools can refuse mid-run — and then nothing has
// moved, which is what makes re-committing the edit a retry rather than a second half-application.
func (t *liveTools) rebuildWith(spec toolSetSpec, engine settingsEngine) error {
	next := t.build(spec)
	if err := engine.SwapTools(next); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current, t.spec = next, spec
	return nil
}

// webSearch resolves the live web_search tool, or nil when the current set has none (or has
// something else under that name — an embedder's own search tool is not one this may re-point).
func (t *liveTools) webSearch() *tools.WebSearch {
	t.mu.Lock()
	registry := t.current
	t.mu.Unlock()
	if registry == nil {
		return nil
	}
	found, ok := registry.Lookup("web_search")
	if !ok {
		return nil
	}
	ws, _ := found.(*tools.WebSearch)
	return ws
}

// registryWithMCP builds the Agent's tool registry: the built-in default tools scoped to the
// workspace (with the same host configuration the Agent would derive from Config — the
// url-safety guard, the web-search endpoint, the Asker, the Presenter, the credential scrub) PLUS
// the dynamically discovered MCP tools registered on top. MCP tools are DYNAMIC (discovered from a
// server at runtime), so they are NOT in DefaultTools — they ride the registry as classMCP
// ExternalEffectTools the dispatch disposition gates in Auto. A duplicate name (an MCP server's
// qualified tool colliding with a built-in — unlikely given the alias prefix) is dropped with a
// stderr notice rather than failing startup; the built-in wins.
//
// The roster ladder — cfg.DisabledTools / cfg.EnabledTools (`tools.disabled:` / `tools.enabled:`)
// and the bound model's cfg.Profile.Tools axis (ADR 0057) — prunes and lifts the BUILT-IN half
// only, which is the half it names: an MCP server's tools come and go with the server, so the way
// to stop offering them is to stop connecting it (`mcp-servers:`) rather than to list every tool it
// happens to advertise.
func registryWithMCP(workspace string, cfg apogee.Config, mcpTools []apogee.Tool) *apogee.ToolRegistry {
	registry := tools.NewDefaultRegistryWithHost(workspace, tools.HostTools{
		// The `url-safety:` host layer, off the same Config the engine would have read it from and
		// through the same constructor — this hand-assembly must not be the one path on which a
		// configured deny quietly stops applying, or connecting an MCP server would re-open a host
		// the operator closed in every session without MCP.
		URLGuard:          security.NewURLGuard(cfg.URLAllowHosts, cfg.URLDenyHosts),
		WebSearchEndpoint: cfg.WebSearchEndpoint,
		Asker:             cfg.Asker,
		Presenter:         cfg.Presenter,
		Disabled:          cfg.DisabledTools,
		// The two rungs above the global disable — the `tools.enabled:` lift and the bound model's
		// `tools:` axis (ADR 0057) — off the same Config the engine would have read them from. This
		// hand-assembly must not be the one path on which a configured roster quietly stops
		// applying, or connecting an MCP server would silently re-broaden the menu in every
		// session without MCP.
		Enabled:       cfg.EnabledTools,
		ProfileRoster: cfg.Profile.Tools,
		// The credential variables the execution tools must drop, off the same Config the engine
		// would have read them from — this hand-assembly must not be the one place a subprocess
		// inherits the operator's `api-key-env:` key, or connecting an MCP server would quietly
		// re-open the exposure a session without MCP is closed against.
		SecretEnvVars: cfg.SecretEnvVars,
		// The read-only mounts the session opened up (the skill source dirs), off the same Config
		// the engine would have read them from — this hand-assembly must not be the one place a
		// read tool loses them, or the model could read a skill's bundled files in a session
		// without MCP and not in one with it.
		ExtraReadRoots: cfg.ExtraReadRoots,
		// And the pathless mounts beside them (the shipped skills' `shipped:<id>` tree), off the
		// same Config for the same reason: an MCP session must not be the one place a shipped
		// skill's announced files: line names a folder the read tools refuse.
		VirtualReadRoots: cfg.VirtualReadRoots,
	})
	for _, t := range mcpTools {
		if err := registry.Register(t); err != nil {
			fmt.Fprintf(os.Stderr, "apogee: skipping MCP tool %q: %v\n", t.Name(), err)
		}
	}
	return registry
}
