package main

// The MCP seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// The connections this session is running on, one per configured server, and the recipe for dialling
// one more: `mcp-servers:` is editable mid-session (ADR 0037 decision 6), and applying it is
// validate-then-commit PER SERVER (ADR 0109, adr-0037-amend-1107) — dial the servers the edit adds or
// changes, move the tool registry onto the result, close the sessions it replaced, in that order — so
// a server that cannot be reached keeps its old session, or stays absent, and costs the healthy ones
// nothing.

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
)

// mcpEntry is one configured server the session is connected to: the resolved config it was dialled
// with and its own one-server session. The config is what a later reconnect diffs against
// (mcp.ServerConfig.Equal), so it is the config the session was DIALLED with — a re-dial that failed
// leaves it alone, which is what makes the next commit of the same config a retry.
type mcpEntry struct {
	config  mcp.ServerConfig
	session mcpSession
}

// mcpConnect dials ONE configured server under ctx, its endpoint checked against the url-safety host
// lists allowHosts and denyHosts. The context is the caller's because a session's life is bound to it
// (go-sdk's SSE transport binds its GET stream to the Connect context), so it must outlive the dial:
// it is the wiring's session context, never one that ends when the dial returns.
type mcpConnect func(ctx context.Context, server mcp.ServerConfig, allowHosts, denyHosts []string) (mcpSession, error)

// liveMCP owns the MCP connections the session is running on — a name→entry map, one session per
// server — and the recipe for dialling another. It exists because `mcp-servers:` is editable
// mid-session (ADR 0037 decision 6) and a connection is not a value: applying the key means dialling
// the servers it adds or changes, moving the tool registry onto the result and closing the sessions
// that were replaced, in that order, each server on its own (ADR 0109).
//
// The holder is what makes the tool registry's rebuild honest afterwards: the build recipe folds in
// whatever this holder currently carries, so a rebuild driven by any reason at all — a search endpoint
// edit, a later reconnect — carries the connections that are live now rather than the ones the
// process started with.
//
// The mutex guards the entries for liveTools' reason: the writes come from the Update goroutine and
// the surfaced tools are called on the loop's worker goroutine, which reaches them through the
// registry rather than through this holder. An installed map is never written again — every change
// installs a fresh one — so a snapshot read under the lock stays valid after it is released.
type liveMCP struct {
	mu      sync.Mutex
	entries map[string]mcpEntry
	// order is the entries' names in config order, which is the order their tools are surfaced in.
	order []string

	// ctx is the wiring's session context, the one every dial runs under (see mcpConnect).
	ctx context.Context
	// connect dials one server, as startup dialled each — bar a repo server whose pin moved, which the
	// session it returns reports as skipped (mcpSession.Skipped).
	connect mcpConnect
}

// newLiveMCP holds the servers this run connected at startup, in config order, and the recipe for
// dialling another under ctx.
func newLiveMCP(ctx context.Context, entries []mcpEntry, connect mcpConnect) *liveMCP {
	m := &liveMCP{ctx: ctx, connect: connect}
	held := make(map[string]mcpEntry, len(entries))
	order := make([]string, 0, len(entries))
	for _, entry := range entries {
		held[entry.config.Name] = entry
		order = append(order, entry.config.Name)
	}
	m.install(held, order)
	return m
}

// tools surfaces what the connected servers advertise, in config order, for folding into a registry
// build.
func (m *liveMCP) tools() []apogee.Tool {
	entries, order := m.snapshot()
	var out []apogee.Tool
	for _, name := range order {
		out = append(out, entries[name].session.Tools()...)
	}
	return out
}

// session is the live session the holder carries for the server named name, nil when it carries
// none — a server never configured, removed, or whose first dial failed.
func (m *liveMCP) session(name string) mcpSession {
	entries, _ := m.snapshot()
	return entries[name].session
}

// close tears down every server the session ended on — the run's own defer, which is why it reads the
// holder rather than closing the sessions the process started with.
func (m *liveMCP) close() error {
	entries, order := m.snapshot()
	var errs []error
	for _, name := range order {
		errs = append(errs, closeSession(entries[name].session))
	}
	return errors.Join(errs...)
}

// snapshot reads the installed entries and their order under the lock.
func (m *liveMCP) snapshot() (map[string]mcpEntry, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries, m.order
}

// install puts entries and their order in force under the lock. Neither is written after this.
func (m *liveMCP) install(entries map[string]mcpEntry, order []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries, m.order = entries, order
}

// mcpOutcome is what one reconnect says about ONE server it dialled: the transcript note (exactly one
// of the four ADR 0109 sentences) and, for a failure, the row's clause and its cause.
type mcpOutcome struct {
	// note is `mcp <server> reconnected`, `mcp <server> connected`,
	// `mcp reconnect failed: <server>: … — previous connection kept` or `mcp connect failed: <server>: …`.
	note string
	// rowFailure is `<server>: …` for a failed dial, empty on success.
	rowFailure string
	// err is the failure's cause, kept so the row's error still wraps what internal/mcp refused with.
	err error
}

// mcpLanded is the outcome of a dial that answered: wasLive says whether it replaced a session.
func mcpLanded(server string, wasLive bool) mcpOutcome {
	if wasLive {
		return mcpOutcome{note: "mcp " + server + " reconnected"}
	}
	return mcpOutcome{note: "mcp " + server + " connected"}
}

// mcpFailed is the outcome of a dial that did not answer: wasLive says whether the server's old
// session is kept, which is the half of the sentence the human most needs — the alternative reading
// of a failed reconnect is that the server's tools are gone. The cause is de-labelled (mcpSentence),
// since every surface that prints it has already said `mcp`.
func mcpFailed(server string, wasLive bool, err error) mcpOutcome {
	clause := server + ": " + mcpSentence(err)
	note := "mcp connect failed: " + clause
	if wasLive {
		note = "mcp reconnect failed: " + clause + " — previous connection kept"
	}
	return mcpOutcome{note: note, rowFailure: clause, err: err}
}

// reconnect moves the session onto servers, server by server (ADR 0109): an unchanged server
// (mcp.ServerConfig.Equal against the config its live session was dialled with) is left alone, a
// server no longer configured is dropped, and an added or changed one is dialled — "validate" is the
// connection itself, per server. A failed re-dial keeps that server's old session and its OLD config,
// a failed new server stays absent, and neither costs any other server anything. Then one rebuild and
// one SwapTools put the result in force, and only once the engine has taken it are the replaced and
// removed sessions closed. It runs synchronously on its caller.
//
// The outcomes are one per server dialled, in config order; the unchanged and removed servers have
// none. A repo server whose pin moved is a failed dial whose cause is the pin's refusal: it keeps an
// old session as any other failure does, and the re-read the connect cued asks about it again.
//
// The error is for a refusal of the WHOLE call, with nothing moved, worded by mcpReconnectFailed: a
// busy engine, a set mcp.ValidateServers refuses, or a registry the engine would not take. A busy
// engine is asked FIRST (CanSwapTools), before anything is dialled: a swap that is going to be
// refused has no use for the connections, and dialling them mid-Turn would only spend a server's
// handshake to close it straight down. The set is validated next, as startup validates it before
// its first dial, so a duplicate name never dials half a set.
func (m *liveMCP) reconnect(servers []mcp.ServerConfig, set *liveTools, engine settingsEngine) ([]mcpOutcome, error) {
	if err := engine.CanSwapTools(); err != nil {
		return nil, mcpReconnectFailed(err)
	}
	if err := mcp.ValidateServers(servers); err != nil {
		return nil, mcpReconnectFailed(err)
	}
	// The dials are guarded by the host lists the live tool set is on NOW, which is the same read the
	// network tools' own rebuild makes.
	spec := set.built()
	previous, previousOrder := m.snapshot()
	next := make(map[string]mcpEntry, len(servers))
	order := make([]string, 0, len(servers))
	var outcomes []mcpOutcome
	var opened []mcpSession
	for _, server := range servers {
		old, isLive := previous[server.Name]
		if isLive && old.config.Equal(server) {
			next[server.Name] = old
			order = append(order, server.Name)
			continue
		}
		session, err := m.dial(server, spec)
		if err != nil {
			outcomes = append(outcomes, mcpFailed(server.Name, isLive, err))
			if isLive {
				next[server.Name] = old
				order = append(order, server.Name)
			}
			continue
		}
		opened = append(opened, session)
		next[server.Name] = mcpEntry{config: server, session: session}
		order = append(order, server.Name)
		outcomes = append(outcomes, mcpLanded(server.Name, isLive))
	}
	displaced := displacedSessions(previous, next)
	if len(opened) == 0 && len(displaced) == 0 {
		// Nothing landed and nothing left: the registry would be rebuilt onto the tools it already
		// carries, so the engine is not asked to swap for a set that did not move.
		return outcomes, nil
	}
	// The rebuild below folds the holder's tools, so the new entries have to be installed before it —
	// and put back by hand if the engine will not take the registry built from them.
	m.install(next, order)
	if err := set.rebuild(engine); err != nil {
		m.install(previous, previousOrder)
		// No orphan: the sessions just opened are closed before the failure is reported. The close
		// error is dropped because it describes connections nobody is going to use either way, and the
		// failure worth reporting is the one that already happened.
		for _, session := range opened {
			_ = closeSession(session)
		}
		return nil, mcpReconnectFailed(err)
	}
	// Nothing dispatches to the displaced sessions any more, so they go. A close that fails leaves the
	// human nothing to do about it and does not turn a reconnect that worked into an apply that failed.
	for _, session := range displaced {
		_ = closeSession(session)
	}
	return outcomes, nil
}

// dial connects one server under the holder's context and spec's host lists. A session that skipped
// the server for a moved pin is closed (it holds nothing) and reported as the dial's failure, its
// cause the pin's own refusal.
func (m *liveMCP) dial(server mcp.ServerConfig, spec toolSetSpec) (mcpSession, error) {
	session, err := m.connect(m.ctx, server, spec.allowHosts, spec.denyHosts)
	if err != nil {
		return nil, err
	}
	if skipped := session.Skipped(); len(skipped) > 0 {
		_ = closeSession(session)
		return nil, skipped[0].Err
	}
	return session, nil
}

// displacedSessions are the sessions of previous that next no longer carries — a removed server's,
// or one a fresh dial replaced.
func displacedSessions(previous, next map[string]mcpEntry) []mcpSession {
	var out []mcpSession
	for name, old := range previous {
		if entry, isKept := next[name]; !isKept || entry.session != old.session {
			out = append(out, old.session)
		}
	}
	return out
}

// mcpRowFailures is what the `mcp-servers` row reports about a reconnect that landed: nil when every
// dial answered, otherwise an error whose text is the failed servers' clauses joined by "; " — the
// row reads `saved — live apply failed: <server>: …` — and which still wraps each cause.
func mcpRowFailures(outcomes []mcpOutcome) error {
	var failed mcpRowError
	for _, outcome := range outcomes {
		if outcome.rowFailure != "" {
			failed = append(failed, outcome)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return failed
}

// mcpRowError is mcpRowFailures' error: a type rather than an errors.Join because the text joins the
// clauses on one line, while the causes stay in the chain for errors.Is/As.
type mcpRowError []mcpOutcome

// Error is the row's sentence: the failed servers' clauses, joined by "; ".
func (e mcpRowError) Error() string {
	clauses := make([]string, 0, len(e))
	for _, outcome := range e {
		clauses = append(clauses, outcome.rowFailure)
	}
	return strings.Join(clauses, "; ")
}

// Unwrap keeps every failed dial's cause reachable.
func (e mcpRowError) Unwrap() []error {
	causes := make([]error, 0, len(e))
	for _, outcome := range e {
		causes = append(causes, outcome.err)
	}
	return causes
}

// mcpSkipClause is the sentence for ONE repo server a connect skipped for a moved pin, worded from
// its name and its de-labelled refusal (mcpSentence) — which names the moved files and that the
// server is proposed again — never re-worded here, so the startup notice says what internal/mcp
// refused with. (A reconnect reports a skip as that server's failed dial instead, mcpFailed.)
func mcpSkipClause(s mcp.Denied) string {
	return "mcp server " + s.Name + " skipped — " + mcpSentence(s.Err)
}

// closeSession closes one server's session, tolerating there being none: closing nothing is not an
// error.
func closeSession(s mcpSession) error {
	if s == nil {
		return nil
	}
	return s.Close()
}

// mcpReconnectFailed is what the row says when a reconnect was refused WHOLE, with nothing moved — a
// busy engine, a set that fails validation, a registry the engine would not take (liveMCP.reconnect);
// one server's failed dial is worded per server instead (mcpFailed). It names the outcome the human
// most needs to know — that the servers they were talking to a moment ago are still connected —
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
