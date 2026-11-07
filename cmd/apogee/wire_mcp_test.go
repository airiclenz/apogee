package main

import (
	"errors"
	"slices"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
)

// mcpDialRecorder stands in for one server's dial: it records every server it is asked for, refuses
// the ones named in failing, and otherwise answers with a session advertising `<name>__tool`.
type mcpDialRecorder struct {
	failing  map[string]error
	dialled  []string
	sessions map[string]*fakeMCPSession
}

func newMCPDialRecorder() *mcpDialRecorder {
	return &mcpDialRecorder{failing: map[string]error{}, sessions: map[string]*fakeMCPSession{}}
}

func (r *mcpDialRecorder) connect(servers []mcp.ServerConfig) (mcpSession, error) {
	name := servers[0].Name
	r.dialled = append(r.dialled, name)
	if err := r.failing[name]; err != nil {
		return nil, err
	}
	session := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: name + "__tool"}}}
	r.sessions[name] = session
	return session, nil
}

// liveServer is a live entry for name, dialled with config, advertising `<name>__live`.
func liveServer(config mcp.ServerConfig) (mcpEntry, *fakeMCPSession) {
	session := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: config.Name + "__live"}}}
	return mcpEntry{config: config, session: session}, session
}

// stdioServer is a configured stdio server named name launching command.
func stdioServer(name, command string) mcp.ServerConfig {
	return mcp.ServerConfig{Name: name, Transport: mcp.TransportStdio, Command: command}
}

// outcomeNotes are the transcript notes a reconnect's outcomes carry, in order.
func outcomeNotes(outcomes []mcpOutcome) []string {
	notes := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		notes = append(notes, outcome.note)
	}
	return notes
}

// A reconnect is per server (ADR 0109): a server whose resolved config is unchanged is not dialled
// and keeps the very session it had, while the changed one beside it is re-dialled, replaced and its
// old session closed.
func TestLiveMCPReconnectLeavesAnUnchangedServerAlone(t *testing.T) {
	t.Parallel()
	docs, docsSession := liveServer(stdioServer("docs", "docs-mcp"))
	wiki, wikiSession := liveServer(stdioServer("wiki", "wiki-mcp"))
	dials := newMCPDialRecorder()
	fixture := newMCPFixtureWith([]mcpEntry{docs, wiki}, "", dials.connect)
	spy := &applySettingSpy{}

	outcomes, err := fixture.set.reconnect(
		[]mcp.ServerConfig{stdioServer("docs", "docs-mcp"), stdioServer("wiki", "wiki-mcp-v2")}, fixture.tools, spy)

	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if want := []string{"wiki"}; !slices.Equal(dials.dialled, want) {
		t.Errorf("dialled %v, want %v: an unchanged server is left alone", dials.dialled, want)
	}
	if got := fixture.set.session("docs"); got != mcpSession(docsSession) || docsSession.closed {
		t.Errorf("docs is on %p (closed %v), want its own session, open (%p)", got, docsSession.closed, docsSession)
	}
	if !wikiSession.closed {
		t.Error("wiki's replaced session is still open")
	}
	if want := []string{"mcp wiki reconnected"}; !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q", outcomeNotes(outcomes), want)
	}
	if len(spy.swaps) != 1 {
		t.Errorf("SwapTools calls = %d, want one for the whole reconnect", len(spy.swaps))
	}
}

// A changed server whose re-dial fails keeps its old session AND the old config that session was
// dialled with — so the next reconnect still sees it as changed — and the row names it.
func TestLiveMCPFailedRedialKeepsTheOldSession(t *testing.T) {
	t.Parallel()
	before := stdioServer("docs", "docs-mcp")
	docs, docsSession := liveServer(before)
	dials := newMCPDialRecorder()
	dials.failing["docs"] = errors.New("mcp: server \"docs\": exec: not found")
	fixture := newMCPFixtureWith([]mcpEntry{docs}, "", dials.connect)
	spy := &applySettingSpy{}

	outcomes, err := fixture.set.reconnect([]mcp.ServerConfig{stdioServer("docs", "docs-mcp-v2")}, fixture.tools, spy)

	if err != nil {
		t.Fatalf("reconnect: %v, want the failure as the server's outcome", err)
	}
	if got := fixture.set.session("docs"); got != mcpSession(docsSession) || docsSession.closed {
		t.Errorf("docs is on %p (closed %v), want its old session kept open (%p)", got, docsSession.closed, docsSession)
	}
	entries, _ := fixture.set.snapshot()
	if !entries["docs"].config.Equal(before) {
		t.Errorf("docs' entry carries %+v, want the OLD config %+v its session was dialled with", entries["docs"].config, before)
	}
	want := `mcp reconnect failed: docs: server "docs": exec: not found — previous connection kept`
	if len(outcomes) != 1 || outcomes[0].note != want {
		t.Errorf("notes = %q, want [%q]", outcomeNotes(outcomes), want)
	}
	if err := mcpRowFailures(outcomes); err == nil || err.Error() != `docs: server "docs": exec: not found` {
		t.Errorf("row failure = %v, want the server's clause", err)
	}
	if spy.drove() != 0 {
		t.Errorf("a reconnect where nothing moved still swapped the tool set %d time(s)", len(spy.swaps))
	}
}

// One broken server costs the healthy one nothing: both are new, the healthy one connects and its
// tools reach the registry, and the broken one holds no entry at all.
func TestLiveMCPBrokenServerSparesTheHealthyOne(t *testing.T) {
	t.Parallel()
	dials := newMCPDialRecorder()
	dials.failing["broken"] = errors.New("dial: connection refused")
	fixture := newMCPFixtureWith(nil, "", dials.connect)
	spy := &applySettingSpy{}

	outcomes, err := fixture.set.reconnect(
		[]mcp.ServerConfig{stdioServer("broken", "broken-mcp"), stdioServer("docs", "docs-mcp")}, fixture.tools, spy)

	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if fixture.set.session("broken") != nil {
		t.Error("the server whose first dial failed holds a live entry; it stays absent")
	}
	if got := fixture.set.session("docs"); got != mcpSession(dials.sessions["docs"]) {
		t.Errorf("docs is on %p, want the session its dial opened", got)
	}
	want := []string{"mcp connect failed: broken: dial: connection refused", "mcp docs connected"}
	if !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q", outcomeNotes(outcomes), want)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want 1", len(spy.swaps))
	}
	if _, isFound := spy.swaps[0].Lookup("docs__tool"); !isFound {
		t.Error("the swapped-in registry has none of the healthy server's tools")
	}
}

// The holder surfaces its servers' tools in config order — the order of the set the last reconnect
// moved onto — whichever of them were dialled and whichever were left alone.
func TestLiveMCPToolsFollowConfigOrder(t *testing.T) {
	t.Parallel()
	docs, _ := liveServer(stdioServer("docs", "docs-mcp"))
	dials := newMCPDialRecorder()
	fixture := newMCPFixtureWith([]mcpEntry{docs}, "", dials.connect)

	_, err := fixture.set.reconnect([]mcp.ServerConfig{
		stdioServer("alpha", "alpha-mcp"), stdioServer("docs", "docs-mcp"), stdioServer("zeta", "zeta-mcp"),
	}, fixture.tools, &applySettingSpy{})

	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	want := []string{"alpha__tool", "docs__live", "zeta__tool"}
	if got := toolNames(fixture.set.tools()); !slices.Equal(got, want) {
		t.Errorf("tools = %v, want %v", got, want)
	}
}

// Re-committing the same config after a failure is the retry (ADR 0037 decision 1, ADR 0109
// decision 7): it dials each server whose last dial failed — a new server that stayed absent, and a
// changed server whose old session was kept — exactly once more, and the healthy server not at all.
func TestLiveMCPRecommitRedialsOnlyTheFailedServer(t *testing.T) {
	t.Parallel()
	wiki, _ := liveServer(stdioServer("wiki", "wiki-mcp"))
	dials := newMCPDialRecorder()
	dials.failing["broken"] = errors.New("dial: connection refused")
	dials.failing["wiki"] = errors.New("dial: connection refused")
	fixture := newMCPFixtureWith([]mcpEntry{wiki}, "", dials.connect)
	servers := []mcp.ServerConfig{
		stdioServer("docs", "docs-mcp"), stdioServer("broken", "broken-mcp"), stdioServer("wiki", "wiki-mcp-v2"),
	}
	if _, err := fixture.set.reconnect(servers, fixture.tools, &applySettingSpy{}); err != nil {
		t.Fatalf("first reconnect: %v", err)
	}
	dials.dialled = nil
	delete(dials.failing, "broken")
	delete(dials.failing, "wiki")

	outcomes, err := fixture.set.reconnect(servers, fixture.tools, &applySettingSpy{})

	if err != nil {
		t.Fatalf("re-commit: %v", err)
	}
	if want := []string{"broken", "wiki"}; !slices.Equal(dials.dialled, want) {
		t.Errorf("the re-commit dialled %v, want %v: each failed server once more, the healthy one not at all",
			dials.dialled, want)
	}
	if want := []string{"mcp broken connected", "mcp wiki reconnected"}; !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q", outcomeNotes(outcomes), want)
	}
}
