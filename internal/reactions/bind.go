package reactions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/airiclenz/apogee/internal/domain"
)

// The observe lane's late binds and its no-MCP disarm: the pieces a Driver root composes around
// its Runner before the Agent and the MCP set it fires into exist. The Runner is built first, so
// its executor holds a slot's answer and the root fills the slot once the real value is built.

// ErrProjectRunUnbound refuses an adopted repo reaction's `run:` that fired before an Agent bound
// its fenced runner into the slot: running it without the Agent's permit would run it unfenced.
var ErrProjectRunUnbound = errors.New("refusing to run a project reaction before its session's engine is built")

// ErrMCPResolverUnbound refuses an observe reaction's mcp: call that fired before the session dialled
// its MCP servers: there is no live set to resolve its server against yet.
var ErrMCPResolverUnbound = errors.New("MCP servers are not connected yet")

// ProjectRunSlot is the late bind between a Driver's Reaction Runner and the Agent whose permit an
// adopted repo reaction's observe-lane `run:` spawns under (ADR 0104 §8). The Runner is built
// first and holds Runner(); the Agent, once New or Resume has built it, stores its fenced runner
// through Bind (domain.Config.BindProjectRun). A later Agent — a rebuilt session engine — simply
// rebinds. It is safe for concurrent use: the Runner's workers read it while a bind may land. The
// zero value is an unbound slot.
type ProjectRunSlot struct {
	run atomic.Pointer[domain.ProjectRun]
}

// Bind stores the Agent's fenced runner, replacing any earlier one.
func (s *ProjectRunSlot) Bind(run domain.ProjectRun) {
	s.run.Store(&run)
}

// Runner is the domain.ProjectRun the Runner's executor is handed: the bound Agent's runner, or
// ErrProjectRunUnbound while none is bound. A nil slot answers nil, which the executor reads as no
// fenced runner at all.
func (s *ProjectRunSlot) Runner() domain.ProjectRun {
	if s == nil {
		return nil
	}
	return func(ctx context.Context, r domain.Reaction, stdin string, env []string) error {
		run := s.run.Load()
		if run == nil {
			return ErrProjectRunUnbound
		}
		return (*run)(ctx, r, stdin, env)
	}
}

// MCPSlot is the late bind between a session's Reaction Runner and the resolver over its live MCP
// set (adr-0076-amend-1029). The Runner is built at boot, before the root dials any server, so its
// executor holds Resolver() and the root binds the real resolver once the live set exists. It is
// safe for concurrent use: the Runner's workers resolve through it while the bind may land. The
// zero value is an unbound slot.
type MCPSlot struct {
	resolve atomic.Pointer[domain.MCPResolver]
}

// Bind stores the resolver the Runner's mcp: entries resolve through, replacing any earlier one. A
// nil slot binds nothing — a wiring whose boot built no Runner.
func (s *MCPSlot) Bind(resolve domain.MCPResolver) {
	if s == nil {
		return
	}
	s.resolve.Store(&resolve)
}

// Resolver is the domain.MCPResolver the Runner's executor is handed: it resolves through whatever
// Bind stored last, and refuses with ErrMCPResolverUnbound while nothing is bound. A nil slot
// answers nil, which the executor reads as MCP servers not connected in this run.
func (s *MCPSlot) Resolver() domain.MCPResolver {
	if s == nil {
		return nil
	}
	return func(server, tool string, project bool) (domain.MCPCall, error) {
		bound := s.resolve.Load()
		if bound == nil {
			return nil, ErrMCPResolverUnbound
		}
		return (*bound)(server, tool, project)
	}
}

// The two sentences a Driver that dials no MCP server says about a user reaction's mcp: handler
// (adr-0076-amend-1029), once per entry at setup. An advise or observe entry is dropped before
// arming; a gate entry stays armed and escalates every call it sees to an ask, which a Firing's
// deny-all Approver refuses.
const (
	inertMCPNotice     = "reaction %q: its mcp: handler is inert here — MCP servers are not connected in this run"
	inertMCPGateNotice = "reaction %q: its mcp: gate cannot run here — MCP servers are not connected in this " +
		"run, so every call it gates is refused"
)

// DisarmUnconnectedMCP is the one filter every Driver path that arms reactions without an MCP
// resolver runs — every Firing (headless, the daemon, the TUI's /schedule) and `apogee probe
// context` — before it reads the lanes. It returns list without its advise and observe mcp:
// entries and with its gate mcp: entries kept, and tells report one notice per entry: the inert
// sentence for a dropped one, the refused-gate sentence for a kept gate. Notices are deduplicated
// by reaction id and text, because one `reactions:` entry with both `run:` and `advise:` as mcp:
// resolves to two Reactions under one id. A nil report drops the notices; list itself is never
// modified, and a list with no mcp: entry comes back as it was.
func DisarmUnconnectedMCP(list []domain.Reaction, report func(string)) []domain.Reaction {
	if !slices.ContainsFunc(list, isMCPReaction) {
		return list
	}
	kept := make([]domain.Reaction, 0, len(list))
	said := make(map[string]bool)
	for _, r := range list {
		if !isMCPReaction(r) {
			kept = append(kept, r)
			continue
		}
		format := inertMCPNotice
		if r.Class == domain.ClassGate {
			format = inertMCPGateNotice
			kept = append(kept, r)
		}
		msg := fmt.Sprintf(format, r.ID)
		if said[msg] || report == nil {
			continue
		}
		said[msg] = true
		report(msg)
	}
	return kept
}

// isMCPReaction reports whether r calls a tool on an MCP server.
func isMCPReaction(r domain.Reaction) bool {
	_, ok := r.Handler.(domain.MCPHandler)
	return ok
}
