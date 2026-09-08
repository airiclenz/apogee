package reactions

import "github.com/airiclenz/apogee/internal/domain"

// maxPendingWrites bounds the call-id map the file-changed derivation keeps between a write
// tool's call and its result. A call whose result never arrives — a cancelled run, an abandoned
// Turn — would otherwise leak an entry for the life of the session, so the map refuses to grow
// past this and the surplus calls simply produce no file-changed event.
const maxPendingWrites = 256

// WriteTarget answers whether a tool call writes to a path, and which one — the single fact the
// file-changed derivation needs and the one this package cannot compute, since the answer lives
// in internal/tools and depends on the run's own registry. Every root injects the same closure:
// tools.WorkspaceWriteTarget over its registry lookup. It returns ("", false) for a call that
// writes nothing inspectable, and the absolute, symlink-resolved destination otherwise.
type WriteTarget func(domain.ToolCall) (string, bool)

// firing is one Reaction event a domain.Event produced, with the payload fields derivable from that
// event. The runner stamps the rest — the Reaction's name, the time, the workspace and the Schedule
// — as it fans the firing out to each subscribing Reaction.
type firing struct {
	Event   Event
	Payload Payload
}

// matcher maps one domain.Event to the Reaction events it produces. It is pure in the sense that
// matters: no clock, no filesystem, no network, no goroutine — the only state it keeps is the
// bounded map correlating a write tool's call with its result.
//
// It is built over the SUBSCRIBED set — the union of every active Reaction's events — and an event
// that can only produce an unsubscribed Reaction event costs nothing at all: no WriteTarget call,
// no pending entry, no allocation. With no active Reaction the subscribed set is empty and the
// matcher is a no-op for every engine event, which is the ordinary case for a user who configured
// none.
type matcher struct {
	subscribed  map[Event]bool
	writeTarget WriteTarget
	pending     map[string]pendingWrite

	// project reduces a closed seam's working value to the payload's "value" document. It is a
	// field rather than a direct call so a test can count the projections and prove an
	// unsubscribed seam never reaches one; production always holds projectSeamValue.
	project func(domain.Moment, any) any
}

// pendingWrite remembers the tool and destination of a write call whose result has not arrived.
type pendingWrite struct {
	tool string
	path string
}

// newMatcher builds a matcher over the given subscribed set. writeTarget may be nil, in which
// case no file-changed event is ever derived — a root that cannot resolve a write target is a
// root whose file-changed Reactions simply never fire, rather than one that panics.
func newMatcher(subscribed map[Event]bool, writeTarget WriteTarget) *matcher {
	return &matcher{
		subscribed:  subscribed,
		writeTarget: writeTarget,
		pending:     make(map[string]pendingWrite),
		project:     projectSeamValue,
	}
}

// wants reports whether any active Reaction subscribes to e.
func (m *matcher) wants(e Event) bool { return m.subscribed[e] }

// match returns the Reaction events ev produced, in the order they should be delivered. A Depth-0
// Turn that closed its Exchange produces two — the boundary first, then the closure — so a Reaction
// subscribing to both sees them in that order.
func (m *matcher) match(ev domain.Event) []firing {
	if len(m.subscribed) == 0 {
		return nil
	}
	switch e := ev.(type) {
	case domain.TurnEvent:
		return m.matchTurn(e)
	case domain.ApprovalEvent:
		return m.matchApproval(e)
	case domain.ErrorEvent:
		return m.matchError(e)
	case domain.ToolCallEvent:
		m.rememberWrite(e)
		return nil
	case domain.ToolResultEvent:
		return m.matchToolResult(e)
	case domain.SeamClosedEvent:
		return m.matchSeamClosed(e)
	default:
		return nil
	}
}

// matchTurn maps a Turn boundary. Only Depth 0 counts: a sub-agent runs the same loop and would
// otherwise fire a turn-finished Reaction for every step of every delegation (ADR 0073 §4).
func (m *matcher) matchTurn(ev domain.TurnEvent) []firing {
	if ev.Depth != 0 {
		return nil
	}
	closed := ev.Status == domain.StatusExchangeComplete
	if !m.wants(TurnFinished) && !(closed && m.wants(ExchangeFinished)) {
		return nil
	}

	payload := Payload{
		Status:     string(ev.Status),
		Faulted:    ev.Faulted,
		StepCapped: ev.StepCapped,
	}
	payload.applyBase(ev.EventBase)

	var out []firing
	if m.wants(TurnFinished) {
		out = append(out, firingOf(TurnFinished, payload))
	}
	if closed && m.wants(ExchangeFinished) {
		out = append(out, firingOf(ExchangeFinished, payload))
	}
	return out
}

// matchApproval maps BOTH phases of one Approval, each to the notice named after it:
// approval-requested reports that a human is being waited on, approval-decided reports the verdict
// that same request reached, carried on the payload as "decision". The decided phase was
// deliberately not an event under ADR 0073 §2 — ADR 0076 A6 supersedes that: the two halves are one
// pair, and a reaction that only learns a prompt was raised can never tell an answered one from an
// abandoned one.
func (m *matcher) matchApproval(ev domain.ApprovalEvent) []firing {
	var event Event
	switch ev.Phase {
	case domain.ApprovalRequested:
		event = ApprovalRequested
	case domain.ApprovalDecided:
		event = ApprovalDecided
	default:
		return nil
	}
	if !m.wants(event) {
		return nil
	}
	payload := Payload{
		Tool:         ev.Request.Tool,
		Reason:       ev.Request.Reason,
		Remedy:       ev.Request.Remedy,
		SubAgentName: ev.Request.SubAgentName,
		Scope:        ev.Request.Scope,
	}
	if event == ApprovalDecided {
		payload.Decision = string(ev.Decision)
	}
	payload.applyBase(ev.EventBase)
	return []firing{firingOf(event, payload)}
}

// matchSeamClosed maps a closed seam to the notice named after it — pre-request to
// pre-request-finished, and so on for the other four (Moment.Closing). Only Depth 0 counts, for
// matchTurn's reason: a sub-agent crosses the same five seams on every step of every delegation,
// and a notice per crossing would bury the top-level pass a user asked to watch (ADR 0073 §4).
//
// The working value is projected HERE, on the engine's own goroutine inside its Emit call,
// because the reference the event carries is valid only for that call — the loop resumes
// mutating the value the moment Emit returns, and the firing this produces may not run for
// minutes. A seam nothing subscribes to costs one map lookup and no projection at all.
func (m *matcher) matchSeamClosed(ev domain.SeamClosedEvent) []firing {
	if ev.Depth != 0 {
		return nil
	}
	notice := ev.Seam.Closing()
	if notice == "" || !m.wants(notice) {
		return nil
	}
	payload := Payload{
		Seam:      ev.Seam,
		Reactions: append([]string(nil), ev.Fired...),
		Value:     m.project(ev.Seam, ev.Value),
	}
	payload.applyBase(ev.EventBase)
	return []firing{firingOf(notice, payload)}
}

// matchError maps a recovered engine fault, at any depth.
func (m *matcher) matchError(ev domain.ErrorEvent) []firing {
	if !m.wants(Error) {
		return nil
	}
	payload := Payload{Source: ev.Source, Error: ev.Err}
	payload.applyBase(ev.EventBase)
	return []firing{firingOf(Error, payload)}
}

// rememberWrite records a write call so its RESULT can be reported as a file-changed event —
// the result alone carries no tool name and no arguments, and a call alone has not happened yet.
// It is skipped entirely when no Reaction subscribes to file-changed, so an unsubscribed run never
// calls WriteTarget and never grows the map.
func (m *matcher) rememberWrite(ev domain.ToolCallEvent) {
	if !m.wants(FileChanged) || m.writeTarget == nil || len(m.pending) >= maxPendingWrites {
		return
	}
	path, ok := m.writeTarget(ev.Call)
	if !ok {
		return
	}
	m.pending[ev.Call.ID] = pendingWrite{tool: ev.Call.Tool, path: path}
}

// matchToolResult closes a remembered write. The entry is dropped whether or not the write
// succeeded — the call is over either way — but only a SUCCESSFUL result fires the event: a
// refused or failed write changed no file.
func (m *matcher) matchToolResult(ev domain.ToolResultEvent) []firing {
	if !m.wants(FileChanged) || len(m.pending) == 0 {
		return nil
	}
	write, ok := m.pending[ev.Result.CallID]
	if !ok {
		return nil
	}
	delete(m.pending, ev.Result.CallID)
	if ev.Result.IsError {
		return nil
	}
	payload := Payload{Tool: write.tool, Path: write.path}
	payload.applyBase(ev.EventBase)
	return []firing{firingOf(FileChanged, payload)}
}

// applyBase copies the emitting agent's identity onto a payload.
func (p *Payload) applyBase(base domain.EventBase) {
	p.Depth = base.Depth
	p.Turn = base.Turn
	p.CallID = base.CallID
}

// firingOf stamps the event name onto a copy of the payload.
func firingOf(e Event, payload Payload) firing {
	payload.Event = e
	return firing{Event: e, Payload: payload}
}
