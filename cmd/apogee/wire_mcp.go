package main

// The MCP seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// The connections this session is running on and the recipe for another set: `mcp-servers:` is
// editable mid-session (ADR 0037 decision 6), and applying it is validate-then-commit — dial the new
// set, move the tool registry onto it, tear the old set down, in that order — so a set that cannot be
// reached leaves the session on the connections it already has.

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
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

	// connect dials a whole set, all-or-nothing, exactly as startup dialled the first one — bar a
	// repo server whose pin moved, which the set it returns reports as skipped (mcpSession.Skipped).
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

// errMCPResolverUnbound refuses an observe reaction's mcp: call that fired before the session dialled
// its MCP servers: there is no live set to resolve its server against yet.
var errMCPResolverUnbound = errors.New("MCP servers are not connected yet")

// mcpResolverSlot is the late bind between the session's Reaction Runner and the resolver over its
// live MCP set (adr-0076-amend-1029). The Runner is built at boot (resolveConfig), before
// wireSession dials any server, so its executor holds resolver() and wireSession binds the real
// resolver once the live set exists. It is safe for concurrent use: the Runner's workers resolve
// through it while the bind may land.
type mcpResolverSlot struct {
	resolve atomic.Pointer[domain.MCPResolver]
}

// bind stores the resolver the Runner's mcp: entries resolve through, replacing any earlier one. A
// nil slot binds nothing — a wiring whose boot built no Runner.
func (s *mcpResolverSlot) bind(resolve domain.MCPResolver) {
	if s == nil {
		return
	}
	s.resolve.Store(&resolve)
}

// resolver is the domain.MCPResolver the Runner's executor is handed: it resolves through whatever
// bind stored last, and refuses with errMCPResolverUnbound while nothing is bound.
func (s *mcpResolverSlot) resolver() domain.MCPResolver {
	return func(server, tool string, project bool) (domain.MCPCall, error) {
		bound := s.resolve.Load()
		if bound == nil {
			return nil, errMCPResolverUnbound
		}
		return (*bound)(server, tool, project)
	}
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
// reported rather than fatal, because a session that is already running is not a session to kill
// over a server that has gone away — and only once the rebuilt registry has been taken by the engine
// are the old sessions torn down. Every failure leaves the session on the connections and the tool
// set it already had, which is what makes re-committing the edit a retry.
//
// A repo server whose pin moved is not a failure: the connect skips it and the rest of the set lands.
// note names each such server and its moved files (mcpSkipNote) — empty when the connect skipped
// none, and always empty beside an error — for the caller to relay on its own surface.
//
// A busy engine is one of those failures: SwapTools is idle-only, so a reconnect committed mid-run is
// refused and reported on the row over a value the file already carries (ADR 0037 binding A).
func (m *liveMCP) reconnect(servers []mcp.ServerConfig, set *liveTools, engine settingsEngine) (note string, err error) {
	next, err := m.connect(servers)
	if err != nil {
		return "", mcpReconnectFailed(err)
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
		return "", mcpReconnectFailed(err)
	}
	// Nothing dispatches to the old sessions any more, so they go. A close that fails leaves the human
	// nothing to do about it and does not turn a reconnect that worked into an apply that failed.
	_ = closeSession(previous)
	return mcpSkipNote(next.Skipped()), nil
}

// mcpSkipNote is what a reconnect says about the repo servers its connect skipped for a moved pin:
// one mcpSkipClause per server, joined by "; " — the empty string when it skipped none.
func mcpSkipNote(skipped []mcp.Denied) string {
	clauses := make([]string, 0, len(skipped))
	for _, s := range skipped {
		clauses = append(clauses, mcpSkipClause(s))
	}
	return strings.Join(clauses, "; ")
}

// mcpSkipClause is the sentence for ONE repo server a connect skipped for a moved pin, worded from
// its name and its de-labelled refusal (mcpSentence) — which names the moved files and that the
// server is proposed again — never re-worded here, so every surface that reports a skip (the startup
// notice, a reconnect's note) says what internal/mcp refused with.
func mcpSkipClause(s mcp.Denied) string {
	return "mcp server " + s.Name + " skipped — " + mcpSentence(s.Err)
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
//
// The sentence it builds drops internal/mcp's own `mcp: ` label from the refusal it carries. Every
// surface that prints this one has already said `mcp` — the `mcp-servers:` row is that key, and the
// url-safety row's clause is labelled by mcpNoteFor — so keeping the inner label stutters the word at
// the reader (`; mcp reconnect failed: mcp: server "docs": …`). Only the RENDERING loses it: the
// refusal stays wrapped, so a caller matching a sentinel internal/mcp wrapped (security.ErrURLBlocked,
// mcp.ErrEndpointDenied) still reaches it through errors.Is.
func mcpReconnectFailed(err error) error {
	return mcpReconnectError{err: err}
}

// mcpReconnectError is mcpReconnectFailed's sentence. It is a type rather than an fmt.Errorf because
// the two things it owes are in tension there: the text has to be composed from the CARRIED error's
// de-labelled sentence, which %w cannot spell, while the error itself has to stay in the chain.
type mcpReconnectError struct{ err error }

// Error is the sentence the row prints.
func (e mcpReconnectError) Error() string {
	return "reconnect failed: " + mcpSentence(e.err) + " — previous connections kept"
}

// Unwrap keeps the refusal reachable: errors.Is/As see exactly what the fmt.Errorf %w form exposed.
func (e mcpReconnectError) Unwrap() error { return e.err }

// mcpErrorPrefix is how internal/mcp opens the refusals it words itself (`mcp: server %q: …`). It is
// matched on this side of the seam, never produced: the prefix belongs to that package's sentences,
// which are ratified and not ours to re-word.
const mcpErrorPrefix = "mcp: "

// mcpSentence is one of those refusals with that label taken off, for embedding in a sentence a
// surface has already labelled `mcp`. An error that never carried the label comes back byte for byte.
func mcpSentence(err error) string {
	return strings.TrimPrefix(err.Error(), mcpErrorPrefix)
}
