package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/testtime"
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

	outcomes, err := requestAndDrainMCP(t, fixture.set,
		[]mcp.ServerConfig{stdioServer("docs", "docs-mcp"), stdioServer("wiki", "wiki-mcp-v2")}, fixture.tools, spy)

	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if want := []string{"wiki"}; !slices.Equal(dials.dialled, want) {
		t.Errorf("dialled %v, want %v: an unchanged server is left alone", dials.dialled, want)
	}
	if got := fixture.set.session("docs"); got != mcpSession(docsSession) || docsSession.isClosed() {
		t.Errorf("docs is on %p (closed %v), want its own session, open (%p)", got, docsSession.isClosed(), docsSession)
	}
	if !wikiSession.isClosed() {
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

	outcomes, err := requestAndDrainMCP(t, fixture.set, []mcp.ServerConfig{stdioServer("docs", "docs-mcp-v2")}, fixture.tools, spy)

	if err != nil {
		t.Fatalf("reconnect: %v, want the failure as the server's outcome", err)
	}
	if got := fixture.set.session("docs"); got != mcpSession(docsSession) || docsSession.isClosed() {
		t.Errorf("docs is on %p (closed %v), want its old session kept open (%p)", got, docsSession.isClosed(), docsSession)
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

	outcomes, err := requestAndDrainMCP(t, fixture.set,
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

	_, err := requestAndDrainMCP(t, fixture.set, []mcp.ServerConfig{
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

// A save that only reorders unchanged live servers dials none of them, yet the tools still follow
// config order (ADR 0109 decision 4): the request marks a commit wanted, and that commit swaps the
// same sessions in, in the new order.
func TestLiveMCPReorderOnlySaveReappliesToolOrder(t *testing.T) {
	t.Parallel()
	docs, docsSession := liveServer(stdioServer("docs", "docs-mcp"))
	wiki, wikiSession := liveServer(stdioServer("wiki", "wiki-mcp"))
	dials := newMCPDialRecorder()
	fixture := newMCPFixtureWith([]mcpEntry{docs, wiki}, "", dials.connect)
	spy := &applySettingSpy{}

	err := fixture.set.request([]mcp.ServerConfig{
		stdioServer("wiki", "wiki-mcp"), stdioServer("docs", "docs-mcp"),
	}, openGuard, false)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !fixture.set.wantsRebuild() {
		t.Error("a reorder-only request wants no commit, so the new order never reaches the engine")
	}
	outcomes, err := drainMCP(t, fixture.set, fixture.tools, spy)

	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(dials.dialled) != 0 {
		t.Errorf("a reorder re-dialled %v, want no dial", dials.dialled)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want one carrying the new order", len(spy.swaps))
	}
	want := []string{"wiki__live", "docs__live"}
	if got := toolNames(spy.swaps[0].All()); !slices.Equal(got, want) {
		t.Errorf("swapped tools = %v, want %v", got, want)
	}
	if len(outcomes) != 0 {
		t.Errorf("a reorder wrote notes %q, want none: no server reconnected", outcomeNotes(outcomes))
	}
	if docsSession.isClosed() || wikiSession.isClosed() {
		t.Error("a reorder closed a live session")
	}
	if fixture.set.wantsRebuild() {
		t.Error("the commit left a rebuild wanted")
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
	if _, err := requestAndDrainMCP(t, fixture.set, servers, fixture.tools, &applySettingSpy{}); err != nil {
		t.Fatalf("first reconnect: %v", err)
	}
	dials.dialled = nil
	delete(dials.failing, "broken")
	delete(dials.failing, "wiki")

	outcomes, err := requestAndDrainMCP(t, fixture.set, servers, fixture.tools, &applySettingSpy{})

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

// openGuard is a url-safety guard that admits every endpoint the tests below configure.
var openGuard = mcpGuard(nil, nil)

// mcpWaitBudget bounds every wait below on a condition another goroutine makes true.
var mcpWaitBudget = testtime.Budget(10 * time.Second)

// mcpQuietWindow is how long a test watches for something that must NOT happen yet.
var mcpQuietWindow = testtime.Fixed(50 * time.Millisecond)

// jobServers are the servers a batch of dial jobs dials, in order.
func jobServers(jobs []mcpDialJob) []string {
	names := make([]string, 0, len(jobs))
	for _, job := range jobs {
		names = append(names, job.server)
	}
	return names
}

// awaitClosed waits, within mcpWaitBudget, for ch to close; what names it in the failure.
func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(mcpWaitBudget):
		t.Fatalf("timed out after %s waiting for %s", mcpWaitBudget, what)
	}
}

// heldDial is a connect seam that answers every server at once, bar the one launching held: that
// dial signals entered, waits for its context to end, signals cancelled, and still answers with
// stale — the result a superseded dial hands back.
type heldDial struct {
	held      string
	entered   chan struct{}
	cancelled chan struct{}
	stale     *fakeMCPSession
	sessions  map[string]*fakeMCPSession
}

func newHeldDial(held string) *heldDial {
	return &heldDial{
		held: held, entered: make(chan struct{}), cancelled: make(chan struct{}),
		stale: &fakeMCPSession{}, sessions: map[string]*fakeMCPSession{},
	}
}

func (d *heldDial) connect(ctx context.Context, server mcp.ServerConfig, _, _ []string) (mcpSession, error) {
	if server.Command != d.held {
		session := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: server.Name + "__tool"}}}
		d.sessions[server.Command] = session
		return session, nil
	}
	close(d.entered)
	<-ctx.Done()
	close(d.cancelled)
	return d.stale, nil
}

// A request dials nothing on its caller: it queues one job per server whose config moved — the
// changed one and the added one, in config order — and none for the server left as it was.
func TestLiveMCPRequestDialsOnlyChangedServers(t *testing.T) {
	t.Parallel()
	docs, _ := liveServer(stdioServer("docs", "docs-mcp"))
	wiki, _ := liveServer(stdioServer("wiki", "wiki-mcp"))
	dials := newMCPDialRecorder()
	fixture := newMCPFixtureWith([]mcpEntry{docs, wiki}, "", dials.connect)

	err := fixture.set.request([]mcp.ServerConfig{
		stdioServer("docs", "docs-mcp"), stdioServer("wiki", "wiki-mcp-v2"), stdioServer("alpha", "alpha-mcp"),
	}, openGuard, false)

	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if len(dials.dialled) != 0 {
		t.Errorf("request dialled %v on its caller, want nothing until a job runs", dials.dialled)
	}
	if got, want := jobServers(fixture.set.takeWork()), []string{"wiki", "alpha"}; !slices.Equal(got, want) {
		t.Errorf("jobs = %v, want %v", got, want)
	}
	if jobs := fixture.set.takeWork(); len(jobs) != 0 {
		t.Errorf("takeWork handed out %v a second time", jobServers(jobs))
	}
}

// A new server whose first dial failed is diffed as configured-but-failed (ADR 0109 decision 7): a
// save that changes only another server queues nothing for it, and a save of its own config does.
func TestLiveMCPUnrelatedSaveNeverRedialsAFailedNewServer(t *testing.T) {
	t.Parallel()
	dials := newMCPDialRecorder()
	dials.failing["broken"] = errors.New("dial: connection refused")
	fixture := newMCPFixtureWith(nil, "", dials.connect)
	spy := &applySettingSpy{}
	first := []mcp.ServerConfig{stdioServer("docs", "docs-mcp"), stdioServer("broken", "broken-mcp")}
	if err := fixture.set.request(first, openGuard, false); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := drainMCP(t, fixture.set, fixture.tools, spy); err != nil {
		t.Fatalf("first commit: %v", err)
	}

	unrelated := []mcp.ServerConfig{stdioServer("docs", "docs-mcp-v2"), stdioServer("broken", "broken-mcp")}
	if err := fixture.set.request(unrelated, openGuard, false); err != nil {
		t.Fatalf("unrelated request: %v", err)
	}
	unrelatedJobs := jobServers(fixture.set.takeWork())
	own := []mcp.ServerConfig{stdioServer("docs", "docs-mcp-v2"), stdioServer("broken", "broken-mcp-v2")}
	if err := fixture.set.request(own, openGuard, false); err != nil {
		t.Fatalf("own request: %v", err)
	}
	ownJobs := jobServers(fixture.set.takeWork())

	if want := []string{"docs"}; !slices.Equal(unrelatedJobs, want) {
		t.Errorf("a save that changed only docs queued %v, want %v", unrelatedJobs, want)
	}
	if want := []string{"broken"}; !slices.Equal(ownJobs, want) {
		t.Errorf("a save of broken's own config queued %v, want %v", ownJobs, want)
	}
}

// The retry-failed flag re-dials every server whose last dial failed — a new server left absent and a
// changed server whose old session was kept — and never the healthy one; without it, an unchanged
// request queues nothing.
func TestLiveMCPRetryFailedRedialsEveryFailedServer(t *testing.T) {
	t.Parallel()
	wiki, _ := liveServer(stdioServer("wiki", "wiki-mcp"))
	docs, _ := liveServer(stdioServer("docs", "docs-mcp"))
	dials := newMCPDialRecorder()
	dials.failing["docs"] = errors.New("dial: connection refused")
	dials.failing["broken"] = errors.New("dial: connection refused")
	fixture := newMCPFixtureWith([]mcpEntry{wiki, docs}, "", dials.connect)
	servers := []mcp.ServerConfig{
		stdioServer("wiki", "wiki-mcp"), stdioServer("docs", "docs-mcp-v2"), stdioServer("broken", "broken-mcp"),
	}
	if err := fixture.set.request(servers, openGuard, false); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := drainMCP(t, fixture.set, fixture.tools, &applySettingSpy{}); err != nil {
		t.Fatalf("first commit: %v", err)
	}

	if err := fixture.set.request(servers, openGuard, false); err != nil {
		t.Fatalf("plain request: %v", err)
	}
	plain := jobServers(fixture.set.takeWork())
	if err := fixture.set.request(servers, openGuard, true); err != nil {
		t.Fatalf("retry request: %v", err)
	}
	retried := jobServers(fixture.set.takeWork())

	if len(plain) != 0 {
		t.Errorf("an unchanged request without the flag queued %v, want nothing", plain)
	}
	if want := []string{"docs", "broken"}; !slices.Equal(retried, want) {
		t.Errorf("the retry queued %v, want %v: every failed server, never the healthy wiki", retried, want)
	}
}

// A request that moves a server back onto the config its live session runs drops the dial it was
// waiting on — cancelled in flight, or discarded once finished — and queues none: no re-dial, no
// tool churn (ADR 0109 decision 2).
func TestLiveMCPRevertToTheLiveConfigDialsNothing(t *testing.T) {
	t.Parallel()
	t.Run("in flight", func(t *testing.T) {
		t.Parallel()
		docs, docsSession := liveServer(stdioServer("docs", "docs-mcp"))
		dial := newHeldDial("docs-mcp-v2")
		fixture := newMCPFixtureDialling([]mcpEntry{docs}, "", dial.connect)
		spy := &applySettingSpy{}
		if err := fixture.set.request([]mcp.ServerConfig{stdioServer("docs", "docs-mcp-v2")}, openGuard, false); err != nil {
			t.Fatalf("request v2: %v", err)
		}
		finished := runJobsAsync(fixture.set.takeWork())
		awaitClosed(t, dial.entered, "the v2 dial to start")

		if err := fixture.set.request([]mcp.ServerConfig{stdioServer("docs", "docs-mcp")}, openGuard, false); err != nil {
			t.Fatalf("revert: %v", err)
		}
		awaitClosed(t, dial.cancelled, "the revert to cancel the v2 dial")
		awaitClosed(t, finished, "the cancelled job to return")
		outcomes, err := drainMCP(t, fixture.set, fixture.tools, spy)

		assertRevertLeftDocsAlone(t, fixture, spy, docsSession, outcomes, err)
		if !dial.stale.isClosed() {
			t.Error("the cancelled dial's session was left open")
		}
	})
	t.Run("pending", func(t *testing.T) {
		t.Parallel()
		docs, docsSession := liveServer(stdioServer("docs", "docs-mcp"))
		dials := newMCPDialRecorder()
		fixture := newMCPFixtureWith([]mcpEntry{docs}, "", dials.connect)
		spy := &applySettingSpy{}
		if err := fixture.set.request([]mcp.ServerConfig{stdioServer("docs", "docs-mcp-v2")}, openGuard, false); err != nil {
			t.Fatalf("request v2: %v", err)
		}
		for _, job := range fixture.set.takeWork() {
			job.run(context.Background())
		}

		if err := fixture.set.request([]mcp.ServerConfig{stdioServer("docs", "docs-mcp")}, openGuard, false); err != nil {
			t.Fatalf("revert: %v", err)
		}
		outcomes, err := drainMCP(t, fixture.set, fixture.tools, spy)

		assertRevertLeftDocsAlone(t, fixture, spy, docsSession, outcomes, err)
		if !dials.sessions["docs"].isClosed() {
			t.Error("the discarded v2 session was left open")
		}
	})
}

// runJobsAsync runs every job on a goroutine of its own and closes the channel it returns once all
// have returned.
func runJobsAsync(jobs []mcpDialJob) <-chan struct{} {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for _, job := range jobs {
			job.run(context.Background())
		}
	}()
	return finished
}

// assertRevertLeftDocsAlone is the revert test's claim: no outcome, no swap, and docs still on its
// own live session.
func assertRevertLeftDocsAlone(
	t *testing.T, fixture *mcpFixture, spy *applySettingSpy, docsSession *fakeMCPSession, outcomes []mcpOutcome, err error,
) {
	t.Helper()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if jobs := fixture.set.takeWork(); len(jobs) != 0 {
		t.Errorf("the revert queued %v, want nothing", jobServers(jobs))
	}
	if len(outcomes) != 0 || spy.drove() != 0 {
		t.Errorf("outcomes %q, swaps %d: want neither for a revert", outcomeNotes(outcomes), len(spy.swaps))
	}
	if got := fixture.set.session("docs"); got != mcpSession(docsSession) || docsSession.isClosed() {
		t.Errorf("docs is on %p (closed %v), want its live session %p, open", got, docsSession.isClosed(), docsSession)
	}
}

// A removal takes effect on the keystroke (ADR 0109 decision 5): the server's session is retired
// before request returns, with no swap, and its tools leave the registry at the next commit.
func TestLiveMCPRemovalRefusesItsToolsAtOnce(t *testing.T) {
	t.Parallel()
	docs, docsSession := liveServer(stdioServer("docs", "docs-mcp"))
	wiki, _ := liveServer(stdioServer("wiki", "wiki-mcp"))
	fixture := newMCPFixtureWith([]mcpEntry{docs, wiki}, "", newMCPDialRecorder().connect)
	spy := &applySettingSpy{}

	err := fixture.set.request([]mcp.ServerConfig{stdioServer("wiki", "wiki-mcp")}, openGuard, false)

	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !docsSession.isRetired() {
		t.Error("the removed server's tools still answer after request returned")
	}
	if len(spy.swaps) != 0 {
		t.Errorf("request swapped the tool set %d time(s); the swap waits for the commit", len(spy.swaps))
	}
	if _, err := drainMCP(t, fixture.set, fixture.tools, spy); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want 1 to take the removed server's tools out", len(spy.swaps))
	}
	if _, isFound := spy.swaps[0].Lookup("docs__live"); isFound {
		t.Error("the committed registry still carries the removed server's tools")
	}
	if !docsSession.isClosed() {
		t.Error("the removed server's session was never closed")
	}
}

// A newer request for a server cancels the dial in flight for it (ADR 0109 decision 6): the stale
// result is closed when it returns and yields no outcome, and only the newest dial lands.
func TestLiveMCPNewerRequestCancelsTheInFlightDial(t *testing.T) {
	t.Parallel()
	docs, _ := liveServer(stdioServer("docs", "docs-mcp"))
	dial := newHeldDial("docs-mcp-v2")
	fixture := newMCPFixtureDialling([]mcpEntry{docs}, "", dial.connect)
	spy := &applySettingSpy{}
	if err := fixture.set.request([]mcp.ServerConfig{stdioServer("docs", "docs-mcp-v2")}, openGuard, false); err != nil {
		t.Fatalf("request v2: %v", err)
	}
	finished := runJobsAsync(fixture.set.takeWork())
	awaitClosed(t, dial.entered, "the v2 dial to start")

	if err := fixture.set.request([]mcp.ServerConfig{stdioServer("docs", "docs-mcp-v3")}, openGuard, false); err != nil {
		t.Fatalf("request v3: %v", err)
	}
	awaitClosed(t, dial.cancelled, "the v3 request to cancel the v2 dial")
	awaitClosed(t, finished, "the cancelled job to return")
	outcomes, err := drainMCP(t, fixture.set, fixture.tools, spy)

	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !dial.stale.isClosed() {
		t.Error("the superseded dial's session was left open")
	}
	if want := []string{"mcp docs reconnected"}; !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q: the superseded dial says nothing", outcomeNotes(outcomes), want)
	}
	if got := fixture.set.session("docs"); got != mcpSession(dial.sessions["docs-mcp-v3"]) {
		t.Errorf("docs is on %p, want the v3 session", got)
	}
}

// gatedDial is a connect seam that holds every dial until release is closed, whatever its context
// does, then answers with session — or with err when it is set. entered closes when the first dial
// starts.
type gatedDial struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	err     error
	session *fakeMCPSession
}

func newGatedDial(name string) *gatedDial {
	return &gatedDial{
		entered: make(chan struct{}), release: make(chan struct{}),
		session: &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: name + "__tool"}}},
	}
}

func (d *gatedDial) connect(_ context.Context, _ mcp.ServerConfig, _, _ []string) (mcpSession, error) {
	d.once.Do(func() { close(d.entered) })
	<-d.release
	if d.err != nil {
		return nil, d.err
	}
	return d.session, nil
}

// denyOldEndpointMidDial moves docs from host A to host B, and — while B's dial is held in dial —
// denies A through the request path: the live tool set's deny list moves, then the admitted set is
// requested under it, as a later `mcp-servers:` save would, so the drop under test is
// requestServerLocked's. (The `url-safety:` row's own deny half, settingsApplier.denyMCP, is driven by
// denyLiveEndpointThroughURLSafety.) It releases the dial and returns once the job has returned, with
// nothing committed yet.
func denyOldEndpointMidDial(t *testing.T, dial *gatedDial) (*mcpFixture, *fakeMCPSession, *applySettingSpy) {
	t.Helper()
	onB := mcp.ServerConfig{Name: "docs", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://b.example.com/"}
	docs, onA := liveServer(mcp.ServerConfig{Name: "docs", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://a.example.com/"})
	fixture := newMCPFixtureDialling([]mcpEntry{docs}, "", dial.connect)
	spy := &applySettingSpy{}
	if err := fixture.set.request([]mcp.ServerConfig{onB}, openGuard, false); err != nil {
		t.Fatalf("request B: %v", err)
	}
	finished := runJobsAsync(fixture.set.takeWork())
	awaitClosed(t, dial.entered, "the dial to B to start")

	deny := []string{"a.example.com"}
	if err := fixture.tools.setDenyHosts(deny, spy); err != nil {
		t.Fatalf("setDenyHosts: %v", err)
	}
	if err := fixture.set.request([]mcp.ServerConfig{onB}, mcpGuard(nil, deny), false); err != nil {
		t.Fatalf("request under the deny: %v", err)
	}
	if fixture.set.session("docs") != nil {
		t.Error("the session on the denied endpoint A still serves after the deny")
	}
	close(dial.release)
	awaitClosed(t, finished, "the dial to B to return")
	return fixture, onA, spy
}

// A deny of a server's OLD endpoint closes the session on it and nothing else: the dial already
// running for the new, still-admitted endpoint lands at the next commit, so the server ends
// connected on B.
func TestLiveMCPDenyOfTheOldEndpointKeepsTheDialToTheNewOne(t *testing.T) {
	t.Parallel()
	dial := newGatedDial("docs")
	fixture, onA, spy := denyOldEndpointMidDial(t, dial)

	outcomes, err := drainMCP(t, fixture.set, fixture.tools, spy)

	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got := fixture.set.session("docs"); got != mcpSession(dial.session) {
		t.Errorf("docs is on %p, want the session on B (%p)", got, dial.session)
	}
	if dial.session.isClosed() {
		t.Error("the session on B was closed")
	}
	if !onA.isClosed() {
		t.Error("the session on the denied endpoint A was left open")
	}
	if want := []string{"mcp docs connected"}; !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q", outcomeNotes(outcomes), want)
	}
}

// A dial to the new endpoint that survived the old endpoint's deny and then failed is a failed dial
// like any other: the retry gesture re-dials it and the server comes back on B.
func TestLiveMCPDenyOfTheOldEndpointLeavesAFailedNewDialRetryable(t *testing.T) {
	t.Parallel()
	dial := newGatedDial("docs")
	dial.err = errors.New("dial: connection refused")
	fixture, _, spy := denyOldEndpointMidDial(t, dial)
	if _, err := drainMCP(t, fixture.set, fixture.tools, spy); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !fixture.set.hasFailedDial() {
		t.Fatal("the failed dial to B is not marked failed: the retry gesture cannot bring docs back")
	}
	dial.err = nil

	if _, err := requestAndDrainMCP(t, fixture.set, []mcp.ServerConfig{
		{Name: "docs", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://b.example.com/"},
	}, fixture.tools, spy); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if got := fixture.set.session("docs"); got != mcpSession(dial.session) {
		t.Errorf("docs is on %p after the retry, want the session on B (%p)", got, dial.session)
	}
}

// denyLiveEndpointThroughURLSafety is the `url-safety:` row's deny of a server's LIVE endpoint while
// its config has already moved on: docs is connected on host A, the config file has it on host B, and
// B's dial is held in dial when the row commits `url-safety.deny-hosts [a.example.com]`. The edit
// changes no configured server's verdict, so only the live endpoint's deny can close A. It returns,
// with the dial still held, once the row has answered, after checking the A session was retired and
// dropped on the keystroke and that the row named it.
func denyLiveEndpointThroughURLSafety(t *testing.T, dial *gatedDial) (*mcpFixture, *fakeMCPSession, *applySettingSpy, <-chan struct{}) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, "mcp-servers:\n"+
		"  - name: docs\n"+
		"    transport: streamable-http\n"+
		"    endpoint: https://b.example.com/\n")
	onB := resolvedMCPServer(t, path, "docs")
	configA := onB
	configA.Endpoint = "https://a.example.com/"
	docs, onA := liveServer(configA)
	fixture := newMCPFixtureDialling([]mcpEntry{docs}, "", dial.connect)
	spy := &applySettingSpy{}
	if err := fixture.set.request([]mcp.ServerConfig{onB}, openGuard, false); err != nil {
		t.Fatalf("request B: %v", err)
	}
	finished := runJobsAsync(fixture.set.takeWork())
	awaitClosed(t, dial.entered, "the dial to B to start")
	applier := fakeApplier(t)
	applier.engine = spy
	applier.tools = fixture.tools
	applier.mcp = fixture.set
	applier.configPath = path

	note, err := applySettingFor(applier)("url-safety.deny-hosts", "[a.example.com]")

	if err != nil {
		t.Fatalf("apply url-safety.deny-hosts: %v", err)
	}
	if want := toolRosterNote + "; mcp server docs disconnected — its endpoint is denied"; note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
	if !onA.isRetired() {
		t.Error("the session on the denied endpoint A still answers calls after the row returned")
	}
	if got := fixture.set.session("docs"); got != nil {
		t.Errorf("the holder still carries %p for docs after A's deny; want no live entry", got)
	}
	return fixture, onA, spy, finished
}

// A `url-safety:` deny of the endpoint a server is still connected on closes that session on the
// keystroke, though its config is already on another, still-admitted endpoint: the dial to that one
// is left to land, and docs ends connected on B.
func TestURLSafetyDenyOfTheLiveEndpointRetiresItWhileTheNewDialLands(t *testing.T) {
	t.Parallel()
	dial := newGatedDial("docs")
	fixture, onA, spy, finished := denyLiveEndpointThroughURLSafety(t, dial)
	if fixture.set.isDialQueued() {
		t.Error("the deny queued a second dial while the dial to B was still running")
	}
	close(dial.release)
	awaitClosed(t, finished, "the dial to B to return")

	outcomes, err := drainMCP(t, fixture.set, fixture.tools, spy)

	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got := fixture.set.session("docs"); got != mcpSession(dial.session) {
		t.Errorf("docs is on %p, want the session on B (%p)", got, dial.session)
	}
	if !onA.isClosed() {
		t.Error("the session on the denied endpoint A was left open")
	}
	if want := []string{"mcp docs connected"}; !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q", outcomeNotes(outcomes), want)
	}
}

// A dial to the new endpoint that fails after the row denied the live one leaves docs absent and
// retryable — never on the denied session, which no failed dial may keep or bring back.
func TestURLSafetyDenyOfTheLiveEndpointLeavesAFailedNewDialRetryable(t *testing.T) {
	t.Parallel()
	dial := newGatedDial("docs")
	dial.err = errors.New("dial: connection refused")
	fixture, onA, spy, finished := denyLiveEndpointThroughURLSafety(t, dial)
	close(dial.release)
	awaitClosed(t, finished, "the dial to B to return")

	if _, err := drainMCP(t, fixture.set, fixture.tools, spy); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if got := fixture.set.session("docs"); got != nil {
		t.Errorf("docs is on %p after B's dial failed, want no session: A is denied", got)
	}
	if !onA.isClosed() {
		t.Error("the session on the denied endpoint A was left open")
	}
	if names := toolNames(fixture.set.tools()); len(names) != 0 {
		t.Errorf("live MCP tools = %v, want none", names)
	}
	if !fixture.set.hasFailedDial() {
		t.Fatal("the failed dial to B is not marked failed: the retry gesture cannot bring docs back")
	}
	dial.err = nil
	if _, err := requestAndDrainMCP(t, fixture.set, []mcp.ServerConfig{
		{Name: "docs", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://b.example.com/"},
	}, fixture.tools, spy); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := fixture.set.session("docs"); got != mcpSession(dial.session) {
		t.Errorf("docs is on %p after the retry, want the session on B (%p)", got, dial.session)
	}
}

// keptSessionSeed is the shape the kept-session deny tests start from: docs is connected on host A,
// the config file at path has it on host B, and B's dial has run and failed, so the session on A is
// a kept connection. dial is already released, so every later dial answers at once — refused while
// dial.err is set, with dial.session once a test clears it.
type keptSessionSeed struct {
	fixture *mcpFixture
	onA     *fakeMCPSession
	onB     mcp.ServerConfig
	dial    *gatedDial
	spy     *applySettingSpy
	path    string
}

// seedKeptSession builds a keptSessionSeed whose dial to B has failed but is NOT yet committed: its
// failed result is held for the next commit. allowHosts, when non-nil, is put in force on the live
// tool set before docs is requested onto B, and the request runs under it.
func seedKeptSession(t *testing.T, allowHosts []string) keptSessionSeed {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, "mcp-servers:\n"+
		"  - name: docs\n"+
		"    transport: streamable-http\n"+
		"    endpoint: https://b.example.com/\n")
	onB := resolvedMCPServer(t, path, "docs")
	configA := onB
	configA.Endpoint = "https://a.example.com/"
	docs, onA := liveServer(configA)
	dial := newGatedDial("docs")
	dial.err = errors.New("dial: connection refused")
	close(dial.release)
	fixture := newMCPFixtureDialling([]mcpEntry{docs}, "", dial.connect)
	spy := &applySettingSpy{}
	if allowHosts != nil {
		if err := fixture.tools.setAllowHosts(allowHosts, spy); err != nil {
			t.Fatalf("setAllowHosts: %v", err)
		}
	}
	if err := fixture.set.request([]mcp.ServerConfig{onB}, mcpGuard(allowHosts, nil), false); err != nil {
		t.Fatalf("request B: %v", err)
	}
	for _, job := range fixture.set.takeWork() {
		job.run(context.Background())
	}
	return keptSessionSeed{fixture: fixture, onA: onA, onB: onB, dial: dial, spy: spy, path: path}
}

// seedCommittedKeptSession is seedKeptSession with B's failed dial committed: the note says the
// previous connection was kept, and docs is still on A.
func seedCommittedKeptSession(t *testing.T, allowHosts []string) keptSessionSeed {
	t.Helper()
	seed := seedKeptSession(t, allowHosts)
	outcomes, err := drainMCP(t, seed.fixture.set, seed.fixture.tools, seed.spy)
	if err != nil {
		t.Fatalf("commit B's failed dial: %v", err)
	}
	if notes := outcomeNotes(outcomes); len(notes) != 1 || !strings.HasSuffix(notes[0], "— previous connection kept") {
		t.Fatalf("notes = %q, want one `mcp reconnect failed: docs: … — previous connection kept`", notes)
	}
	if got := seed.fixture.set.session("docs"); got != mcpSession(seed.onA) {
		t.Fatalf("docs is on %p after B's dial failed, want the kept session on A (%p)", got, seed.onA)
	}
	return seed
}

// applyURLSafety commits one `url-safety:` row on the seed's fixture, as the settings screen would.
func (s keptSessionSeed) applyURLSafety(t *testing.T, key, value string) (string, error) {
	t.Helper()
	applier := fakeApplier(t)
	applier.engine = s.spy
	applier.tools = s.fixture.tools
	applier.mcp = s.fixture.set
	applier.configPath = s.path
	return applySettingFor(applier)(key, value)
}

// assertLandsOnB clears the seed's dial refusal, drains the queued re-dial and checks docs ends on B,
// the kept session on A closed, with the one note a landed dial writes.
func (s keptSessionSeed) assertLandsOnB(t *testing.T) {
	t.Helper()
	s.dial.err = nil
	outcomes, err := drainMCP(t, s.fixture.set, s.fixture.tools, s.spy)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got := s.fixture.set.session("docs"); got != mcpSession(s.dial.session) {
		t.Errorf("docs is on %p, want the session on B (%p)", got, s.dial.session)
	}
	if !s.onA.isClosed() {
		t.Error("the kept session on the denied endpoint A was left open")
	}
	if want := []string{"mcp docs connected"}; !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q", outcomeNotes(outcomes), want)
	}
}

// A `url-safety:` deny of the endpoint a failed server's kept session runs on closes that session on
// the keystroke and dials the server's configured endpoint, which the edit still admits: docs ends
// connected on B rather than absent until a retry gesture (ADR 0109 decision 7).
func TestURLSafetyDenyOfAKeptSessionRedialsTheConfiguredEndpoint(t *testing.T) {
	t.Parallel()
	seed := seedCommittedKeptSession(t, nil)
	if !seed.fixture.set.hasFailedDial() {
		t.Fatal("the seed's dial to B is not marked failed")
	}

	note, err := seed.applyURLSafety(t, "url-safety.deny-hosts", "[a.example.com]")

	if err != nil {
		t.Fatalf("apply url-safety.deny-hosts: %v", err)
	}
	if want := toolRosterNote + "; mcp server docs disconnected — its endpoint is denied"; note != want {
		t.Errorf("note = %q, want %q", note, want)
	}
	if !seed.onA.isRetired() {
		t.Error("the kept session on the denied endpoint A still answers calls after the row returned")
	}
	if !seed.fixture.set.isDialQueued() {
		t.Fatal("no dial to the still-admitted endpoint B was queued on the keystroke")
	}
	seed.assertLandsOnB(t)
}

// A deny that closes both the kept session's endpoint and the configured one retires the server and
// dials nothing: there is no admitted endpoint left to dial.
func TestURLSafetyDenyOfAKeptSessionWithBothEndpointsDeniedDialsNothing(t *testing.T) {
	t.Parallel()
	seed := seedCommittedKeptSession(t, nil)

	if _, err := seed.applyURLSafety(t, "url-safety.deny-hosts", "[a.example.com, b.example.com]"); err != nil {
		t.Fatalf("apply url-safety.deny-hosts: %v", err)
	}

	if !seed.onA.isRetired() {
		t.Error("the kept session on the denied endpoint A still answers calls")
	}
	if got := seed.fixture.set.session("docs"); got != nil {
		t.Errorf("docs is on %p, want no live entry", got)
	}
	if seed.fixture.set.isDialQueued() {
		t.Error("a dial was queued for docs though both its endpoints are denied")
	}
}

// An edit that closes no endpoint a failed server is connected or configured on leaves it as it was:
// its kept session serves on and nothing dials it.
func TestURLSafetyDenyLeavesAnUntouchedFailedServerUndialled(t *testing.T) {
	t.Parallel()
	seed := seedCommittedKeptSession(t, nil)

	if _, err := seed.applyURLSafety(t, "url-safety.deny-hosts", "[c.example.com]"); err != nil {
		t.Fatalf("apply url-safety.deny-hosts: %v", err)
	}

	if seed.fixture.set.isDialQueued() {
		t.Error("an edit that denies neither of docs' endpoints queued a dial")
	}
	if got := seed.fixture.set.session("docs"); got != mcpSession(seed.onA) {
		t.Errorf("docs is on %p, want the kept session on A (%p)", got, seed.onA)
	}
}

// The request path's deny (requestServerLocked) re-dials a kept session's server the same way: a
// request under lists that close A, with the retry-failed flag off, queues the dial to B on its own.
func TestLiveMCPRequestDenyOfAKeptSessionRedialsTheConfiguredEndpoint(t *testing.T) {
	t.Parallel()
	seed := seedCommittedKeptSession(t, nil)

	err := seed.fixture.set.request([]mcp.ServerConfig{seed.onB}, mcpGuard(nil, []string{"a.example.com"}), false)

	if err != nil {
		t.Fatalf("request under the deny: %v", err)
	}
	if !seed.onA.isRetired() {
		t.Error("the kept session on the denied endpoint A still answers calls")
	}
	if !seed.fixture.set.isDialQueued() {
		t.Fatal("no dial to the still-admitted endpoint B was queued")
	}
	seed.assertLandsOnB(t)
}

// A server whose configured endpoint was denied at its last request is not dialled when a deny closes
// its kept session, even though the edit's own lists would admit it: here the host-list swap is
// refused mid-Turn, so those lists never come into force, and the dial would run under lists that
// are not the session's.
func TestURLSafetyDenyRefusedMidTurnOfAKeptSessionOnADeniedConfigDialsNothing(t *testing.T) {
	t.Parallel()
	seed := seedCommittedKeptSession(t, []string{"a.example.com"})
	seed.spy.swapErr = errors.New("engine busy")

	_, err := seed.applyURLSafety(t, "url-safety.allow-hosts", "[b.example.com]")

	if !errors.Is(err, seed.spy.swapErr) {
		t.Fatalf("apply url-safety.allow-hosts = %v, want the mid-Turn refusal", err)
	}
	if !seed.onA.isRetired() {
		t.Error("the kept session on the denied endpoint A still answers calls after the refused swap")
	}
	if seed.fixture.set.isDialQueued() {
		t.Error("a dial was queued for a config the last request denied")
	}
}

// A deny that lands after B's dial failed but before that failure was committed still re-dials: the
// held failed result is superseded, so no `previous connection kept` note is written about the session
// the deny has just closed, and docs ends on B.
func TestURLSafetyDenyOfAKeptSessionWithAnUncommittedFailedDialRedials(t *testing.T) {
	t.Parallel()
	seed := seedKeptSession(t, nil)

	if _, err := seed.applyURLSafety(t, "url-safety.deny-hosts", "[a.example.com]"); err != nil {
		t.Fatalf("apply url-safety.deny-hosts: %v", err)
	}

	if !seed.fixture.set.isDialQueued() {
		t.Fatal("no dial to the still-admitted endpoint B was queued")
	}
	seed.assertLandsOnB(t)
}

// Every finished dial lands in ONE swap, its tools in config order whatever order the dials
// finished in, with the outcomes in config order too.
func TestLiveMCPCommitIsOneSwapInConfigOrder(t *testing.T) {
	t.Parallel()
	docs, _ := liveServer(stdioServer("docs", "docs-mcp"))
	fixture := newMCPFixtureWith([]mcpEntry{docs}, "", newMCPDialRecorder().connect)
	spy := &applySettingSpy{}
	err := fixture.set.request([]mcp.ServerConfig{
		stdioServer("alpha", "alpha-mcp"), stdioServer("docs", "docs-mcp"), stdioServer("zeta", "zeta-mcp"),
	}, openGuard, false)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	jobs := fixture.set.takeWork()
	slices.Reverse(jobs)
	for _, job := range jobs {
		job.run(context.Background())
	}

	outcomes, err := fixture.set.commit(fixture.tools, spy)

	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want one for every finished dial", len(spy.swaps))
	}
	if want := []string{"alpha__tool", "docs__live", "zeta__tool"}; !slices.Equal(toolNames(fixture.set.tools()), want) {
		t.Errorf("tools = %v, want %v", toolNames(fixture.set.tools()), want)
	}
	if want := []string{"mcp alpha connected", "mcp zeta connected"}; !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q", outcomeNotes(outcomes), want)
	}
}

// A dial that answered is admitted again at commit under the lists the session is on THEN: one they
// now deny is closed instead of installed and noted as the disconnection it is, never as a failure.
func TestLiveMCPCommitReadmitsUnderTheLiveLists(t *testing.T) {
	t.Parallel()
	dials := newMCPDialRecorder()
	fixture := newMCPFixtureWith(nil, "", dials.connect)
	spy := &applySettingSpy{}
	server := mcp.ServerConfig{Name: "docs", Transport: mcp.TransportStreamableHTTP, Endpoint: "https://mcp.example.com/"}
	if err := fixture.set.request([]mcp.ServerConfig{server}, openGuard, false); err != nil {
		t.Fatalf("request: %v", err)
	}
	for _, job := range fixture.set.takeWork() {
		job.run(context.Background())
	}
	if err := fixture.tools.setDenyHosts([]string{"mcp.example.com"}, spy); err != nil {
		t.Fatalf("setDenyHosts: %v", err)
	}

	outcomes, err := drainMCP(t, fixture.set, fixture.tools, spy)

	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if want := []string{"mcp server docs disconnected — its endpoint is denied"}; !slices.Equal(outcomeNotes(outcomes), want) {
		t.Errorf("notes = %q, want %q", outcomeNotes(outcomes), want)
	}
	if err := mcpRowFailures(outcomes); err != nil {
		t.Errorf("row failure = %v, want none: a denial is not a failed dial", err)
	}
	if fixture.set.session("docs") != nil {
		t.Error("the denied server was installed")
	}
	if !dials.sessions["docs"].isClosed() {
		t.Error("the denied dial's session was left open")
	}
}

// A swap the engine refuses at commit is the "still pending" outcome: no outcome, no close, and the
// results wait for the next commit, which lands them.
func TestLiveMCPRefusedSwapLeavesResultsPending(t *testing.T) {
	t.Parallel()
	dials := newMCPDialRecorder()
	fixture := newMCPFixtureWith(nil, "", dials.connect)
	spy := &applySettingSpy{swapErr: errors.New("input pending: the tool set can only be swapped between runs")}
	if err := fixture.set.request([]mcp.ServerConfig{stdioServer("docs", "docs-mcp")}, openGuard, false); err != nil {
		t.Fatalf("request: %v", err)
	}

	refused, refusal := drainMCP(t, fixture.set, fixture.tools, spy)
	spy.swapErr = nil
	landed, err := drainMCP(t, fixture.set, fixture.tools, spy)

	if _, isPending := errors.AsType[mcpPendingError](refusal); !isPending {
		t.Fatalf("refused commit error = %v, want the still-pending outcome", refusal)
	}
	if len(refused) != 0 {
		t.Errorf("the refused commit wrote notes %q", outcomeNotes(refused))
	}
	if err != nil {
		t.Fatalf("later commit: %v", err)
	}
	if want := []string{"mcp docs connected"}; !slices.Equal(outcomeNotes(landed), want) {
		t.Errorf("later notes = %q, want %q", outcomeNotes(landed), want)
	}
	session := dials.sessions["docs"]
	if got := fixture.set.session("docs"); got != mcpSession(session) || session.isClosed() {
		t.Errorf("docs is on %p (closed %v), want the pending dial's session %p, open", got, session.isClosed(), session)
	}
}

// Teardown leaves nothing behind (ADR 0109 decision 10): close cancels the dial in flight and waits
// for it, and waits for a live session that is slow to shut down.
func TestLiveMCPCloseCancelsAndJoinsEverything(t *testing.T) {
	t.Parallel()
	slow := &fakeMCPSession{closeGate: make(chan struct{})}
	dial := newHeldDial("gated-mcp")
	fixture := newMCPFixtureDialling(
		[]mcpEntry{{config: stdioServer("slow", "slow-mcp"), session: slow}}, "", dial.connect)
	servers := []mcp.ServerConfig{stdioServer("slow", "slow-mcp"), stdioServer("gated", "gated-mcp")}
	if err := fixture.set.request(servers, openGuard, false); err != nil {
		t.Fatalf("request: %v", err)
	}
	runJobsAsync(fixture.set.takeWork())
	awaitClosed(t, dial.entered, "the gated dial to start")

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_ = fixture.set.close()
	}()

	awaitClosed(t, dial.cancelled, "close to cancel the gated dial")
	select {
	case <-closed:
		t.Fatal("close returned while a live session was still shutting down")
	case <-time.After(mcpQuietWindow):
	}
	close(slow.closeGate)
	awaitClosed(t, closed, "close to return once the slow session shut down")
	if !slow.isClosed() || !dial.stale.isClosed() {
		t.Errorf("slow closed %v, gated dial's session closed %v: want both", slow.isClosed(), dial.stale.isClosed())
	}
}

// A dial job the program never ran before teardown is inert: run after close it dials nothing and
// installs nothing.
func TestLiveMCPJobNeverRunAfterCloseIsInert(t *testing.T) {
	t.Parallel()
	dials := newMCPDialRecorder()
	fixture := newMCPFixtureWith(nil, "", dials.connect)
	if err := fixture.set.request([]mcp.ServerConfig{stdioServer("docs", "docs-mcp")}, openGuard, false); err != nil {
		t.Fatalf("request: %v", err)
	}
	jobs := fixture.set.takeWork()
	if err := fixture.set.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for _, job := range jobs {
		job.run(context.Background())
	}

	if len(dials.dialled) != 0 {
		t.Errorf("a job run after close dialled %v", dials.dialled)
	}
	if fixture.set.session("docs") != nil {
		t.Error("a job run after close installed a session")
	}
}
