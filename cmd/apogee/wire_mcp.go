package main

// The MCP seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// The connections this session is running on and the recipe for another set: `mcp-servers:` is
// editable mid-session (ADR 0037 decision 6), and applying it is validate-then-commit — dial the new
// set, move the tool registry onto it, tear the old set down, in that order — so a set that cannot be
// reached leaves the session on the connections it already has.

import (
	"fmt"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
)

// liveMCP owns the MCP connections the session is running on and the recipe for another set of them.
// It exists because `mcp-servers:` is editable mid-session (ADR 0037 decision 6) and a connection is
// not a value: applying the key means dialling a new set, moving the tool registry onto it and tearing
// the old set down, in that order, so that a set that cannot be reached leaves the session on the
// connections it already has.
//
// The holder is what makes the tool registry's rebuild honest afterwards: the build recipe folds in
// whatever this holder currently carries, so a rebuild driven by any reason at all — a search endpoint
// edit, a later reconnect — carries the connections that are live now rather than the ones the
// process started with.
//
// The mutex guards the pointer for liveTools' reason: the writes come from the Update goroutine and
// the surfaced tools are called on the loop's worker goroutine, which reaches them through the
// registry rather than through this holder.
type liveMCP struct {
	mu      sync.Mutex
	current mcpSession

	// connect dials a whole set, all-or-nothing, exactly as startup dialled the first one.
	connect func(servers []mcp.ServerConfig) (mcpSession, error)
}

// newLiveMCP holds the sessions this run connected at startup, and the recipe for another set.
func newLiveMCP(current mcpSession, connect func(servers []mcp.ServerConfig) (mcpSession, error)) *liveMCP {
	return &liveMCP{current: current, connect: connect}
}

// tools surfaces what the connected servers advertise, for folding into a registry build.
func (m *liveMCP) tools() []apogee.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return nil
	}
	return m.current.Tools()
}

// close tears down whatever set the session ended on — the run's own defer, which is why it reads the
// holder rather than closing the client the process started with.
func (m *liveMCP) close() error {
	m.mu.Lock()
	current := m.current
	m.mu.Unlock()
	return closeSession(current)
}

// swap installs a set and hands back the one it displaced, so a caller that has to put the old one
// back — a reconnect whose registry swap was refused — holds it without a second lock.
func (m *liveMCP) swap(next mcpSession) mcpSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.current
	m.current = next
	return previous
}

// reconnect moves the session onto servers: validate-then-commit, where "validate" is the connection
// itself (ADR 0037 decision 6). The new set is dialled FIRST — all-or-nothing, as at startup, but
// reported rather than fatal, because a session that is already running is not a session to kill over
// a server that has gone away — and only once the rebuilt registry has been taken by the engine are
// the old sessions torn down. Every failure leaves the session on the connections and the tool set it
// already had, which is what makes re-committing the edit a retry.
//
// A busy engine is one of those failures: SwapTools is idle-only, so a reconnect committed mid-run is
// refused and reported on the row over a value the file already carries (ADR 0037 binding A).
func (m *liveMCP) reconnect(servers []mcp.ServerConfig, set *liveTools, engine settingsEngine) error {
	next, err := m.connect(servers)
	if err != nil {
		return mcpReconnectFailed(err)
	}
	// The rebuild below folds the holder's tools, so the new set has to be installed before it — and
	// put back by hand if the engine will not take the registry built from it.
	previous := m.swap(next)
	if err := set.rebuild(engine); err != nil {
		m.swap(previous)
		// No orphan: the sessions just opened are torn down before the failure is reported, exactly as
		// mcp.Connect rolls back a half-connected set. The close error is dropped because it describes
		// connections nobody is going to use either way, and the failure worth reporting is the one
		// that already happened.
		_ = closeSession(next)
		return mcpReconnectFailed(err)
	}
	// Nothing dispatches to the old sessions any more, so they go. A close that fails leaves the human
	// nothing to do about it and does not turn a reconnect that worked into an apply that failed.
	_ = closeSession(previous)
	return nil
}

// closeSession tears a set down, tolerating the holder having none: an embedder that wired no MCP at
// all is a dormant holder, and closing nothing is not an error.
func closeSession(s mcpSession) error {
	if s == nil {
		return nil
	}
	return s.Close()
}

// mcpReconnectFailed is what the row says when a reconnect could not land. It names the outcome the
// human most needs to know — that the servers they were talking to a moment ago are still connected —
// because the alternative reading of a failed reconnect is that the session now has no MCP tools.
func mcpReconnectFailed(err error) error {
	return fmt.Errorf("reconnect failed: %w — previous connections kept", err)
}
