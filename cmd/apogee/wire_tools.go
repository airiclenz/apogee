package main

// The tool-set seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// What the session's tools are made of and how they change: the live holder that owns the registry
// the engine is running on — re-pointing a registered tool where it can, going through the single
// SwapTools door where it must (ADR 0037 binding F) — plus the startup builders behind it, the
// registry assembly that folds the discovered MCP tools onto the built-in set, and the validation
// that turns a `mechanisms:` config block into the ID list the engine arms (ADR 0015 §1).

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
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
	// endpoint is the web-search endpoint the live set was BUILT from. It is remembered because a
	// rebuild driven by something else entirely — a reconnect to another set of MCP servers — must
	// carry the endpoint this session is actually on, not the one it launched with: rebuilding from
	// the startup value would quietly revert a search-endpoint edit made an hour earlier.
	endpoint string

	// disabled is the `tools.disabled:` roster the live set was built from, remembered for the
	// endpoint's exact reason: a rebuild driven by anything else must carry the roster this session
	// is on, or an MCP reconnect would quietly hand back a tool the user switched off.
	disabled []string

	// build assembles the whole set as this session would have assembled it at startup, for a given
	// web-search endpoint and a given disabled roster — host tools plus the MCP tools that are
	// connected now.
	build func(webSearchEndpoint string, disabled []string) *apogee.ToolRegistry
}

// newLiveTools holds the registry the session was constructed with, the endpoint and roster it was
// built from, and the recipe for another.
func newLiveTools(current *apogee.ToolRegistry, endpoint string, disabled []string,
	build func(webSearchEndpoint string, disabled []string) *apogee.ToolRegistry) *liveTools {
	return &liveTools{current: current, endpoint: endpoint, disabled: disabled, build: build}
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
		t.endpoint = endpoint
		t.mu.Unlock()
		return nil
	}
	_, disabled := t.built()
	return t.rebuildWith(endpoint, disabled, engine)
}

// setDisabled moves the session onto another `tools.disabled:` roster. Unlike the search endpoint
// above there is no tool to re-point: which tools EXIST is the set's identity, so this is always the
// swap door (ADR 0037 binding F) — build the set the roster describes, hand it to the engine, and the
// next request's tool list is offered from it. Being idle-only, SwapTools can refuse mid-run, and the
// refusal lands on the row over a value already persisted; re-committing retries it.
func (t *liveTools) setDisabled(disabled []string, engine settingsEngine) error {
	endpoint, _ := t.built()
	return t.rebuildWith(endpoint, disabled, engine)
}

// rebuild reassembles the whole set as this session would assemble it NOW and hands it to the engine
// — the door a change to the tool SET itself goes through when the change is not about any one tool's
// configuration (ADR 0037 binding F). Today that is one caller: a reconnect to another set of MCP
// servers, whose tools are part of the set's identity rather than a field on a tool already in it.
func (t *liveTools) rebuild(engine settingsEngine) error {
	endpoint, disabled := t.built()
	return t.rebuildWith(endpoint, disabled, engine)
}

// built reports the two values the live set was assembled from — the search endpoint and the
// disabled-tool roster — which is what a rebuild driven by something else entirely must carry
// forward rather than re-derive from the startup snapshot.
func (t *liveTools) built() (string, []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.endpoint, t.disabled
}

// rebuildWith is the swap every door shares: build, hand to the engine, and become the live set only
// once the engine has taken it. Being idle-only, SwapTools can refuse mid-run — and then nothing has
// moved, which is what makes re-committing the edit a retry rather than a second half-application.
func (t *liveTools) rebuildWith(endpoint string, disabled []string, engine settingsEngine) error {
	next := t.build(endpoint, disabled)
	if err := engine.SwapTools(next); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current, t.endpoint, t.disabled = next, endpoint, disabled
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
// cfg.DisabledTools (`tools.disabled:`) prunes the BUILT-IN half only, which is the half it names:
// an MCP server's tools come and go with the server, so the way to stop offering them is to stop
// connecting it (`mcp-servers:`) rather than to list every tool it happens to advertise.
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
	})
	for _, t := range mcpTools {
		if err := registry.Register(t); err != nil {
			fmt.Fprintf(os.Stderr, "apogee: skipping MCP tool %q: %v\n", t.Name(), err)
		}
	}
	return registry
}

// mechanismIDs validates every `mechanisms:` config key against the known catalogue and returns the
// enabled IDs in sorted canonical order for Config.EnableMechanisms — the engine (apogee.New/Resume)
// builds them, derives their Deps, and runs the stacking gates (ADR 0015 §1: wire.go collapses to a
// YAML→ID-list producer). EVERY key is validated here, enabled AND disabled: the engine only ever
// sees the enabled IDs, so a typo'd DISABLED key — never constructed — must still fail loudly at this
// startup boundary (phase-4-review-fixes item 5). An unknown key, whether true or false, is a loud
// error naming the known catalogue. Keys are walked in sorted spelling so the returned list (and any
// engine-side build error over it) is deterministic; the dispatch order is the registry's own
// topo-sort (ADR 0003), independent of this order. With nothing enabled it returns nil, so
// Config.EnableMechanisms stays empty and the engine arms nothing (today's behaviour for a config
// without a mechanisms block).
func mechanismIDs(enabled map[string]bool, known []apogee.MechanismID) ([]apogee.MechanismID, error) {
	knownSet := make(map[string]bool, len(known))
	for _, id := range known {
		knownSet[string(id)] = true
	}

	keys := make([]string, 0, len(enabled))
	for id := range enabled {
		keys = append(keys, id)
	}
	sort.Strings(keys)

	ids := make([]apogee.MechanismID, 0, len(keys))
	for _, id := range keys {
		if !knownSet[id] {
			return nil, fmt.Errorf("apogee: unknown mechanism %q; known: %s", id, knownMechanismList(known))
		}
		if enabled[id] {
			ids = append(ids, apogee.MechanismID(id))
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, nil
}

// knownMechanismList renders the known catalogue for the unknown-key error, matching the engine's
// own unknown-ID error tail (an empty catalogue renders "(none)").
func knownMechanismList(known []apogee.MechanismID) string {
	if len(known) == 0 {
		return "(none)"
	}
	parts := make([]string, len(known))
	for i, id := range known {
		parts[i] = string(id)
	}
	return strings.Join(parts, ", ")
}
