package agent

import (
	"github.com/airiclenz/apogee/internal/domain"
)

// The Context cost report's PRODUCER (ADR 0079). ContextFilesReport (contextfiles.go) measures
// the standing system message as one string — the whole seed, joins included — and that number,
// StandingTokens, stays what it is. ContextCost is the sibling read over the same renders, piece
// by piece: it takes the one walk of the standingBlocks table standingSystem takes
// (standingRenders, standingblocks.go) and adds the tool surface exactly as the wire seam
// (wire.go) sends it. No render path exists here that the request does not take, so a row added
// to the table is counted here without a change.

// contextCostToolMenu and contextCostToolInstructions name the two tool-surface rows: the native
// tools array, or the text block a non-native profile carries in its place.
const (
	contextCostToolMenu         = "tool menu"
	contextCostToolInstructions = "tool instructions"
)

// ContextCost reports what this Agent puts in front of the model at Turn 1 before any user
// message — each standing block that seeds the position-0 system message, in wire order, and the
// tool surface — with the bytes each piece sends and the token estimate those bytes come to
// through the session's chars→token ratio; Calibrated says whether that ratio has been folded
// toward a server's own count yet.
//
// The rows are wire-faithful, which is what makes the report worth charting. The standing rows
// follow standingSystem's ride-along rule to the letter: the engine-owned blocks (orientation,
// delegate report, task list) are rendered — and counted — only when a configured source (the
// prompt template or the workspace context files) seeds the message, so a session that seeds
// nothing reports no standing row at all. The tool surface is one row, never two: on a native
// profile the menu goes as the tools array (`tool menu`, measured PromptChars-style); on a
// non-native profile toProviderRequest folds the rendered instruction block into the system
// channel and suppresses the array, so `tool instructions` REPLACES `tool menu`. A piece that
// renders nothing has no row.
//
// Like ContextFilesReport this is an idle-only read: it renders the prompt (Mode, date, scratch
// dir), composes the mode-filtered menu and reads the token accounting, so a Driver calls it with
// no worker driving the Agent.
func (a *Agent) ContextCost() domain.ContextCost {
	return a.contextCost(a.requestRoom())
}

// contextCost is ContextCost's body over a request room already built: the rows are the room's one
// standing render, the tool surface its menu and the estimate its Budget's, so a tool-result
// commit (contextEstimateEvent) reports from the render its clamp read rather than taking another.
func (a *Agent) contextCost(room requestRoom) domain.ContextCost {
	budget := room.budget
	report := domain.ContextCost{Calibrated: a.tokens.Calibrated()}
	add := func(name string, bytes int) {
		if bytes == 0 {
			return
		}
		report.Rows = append(report.Rows, domain.ContextCostRow{
			Name:   name,
			Bytes:  bytes,
			Tokens: budget.EstimateTokens(bytes),
		})
		report.Bytes += bytes
	}

	for _, row := range room.rows {
		add(row.name, len(row.rendered))
	}

	menu := room.menu
	if block := a.toolInstructions(menu); block != "" {
		add(contextCostToolInstructions, len(block))
	} else {
		add(contextCostToolMenu, domain.PromptChars(nil, menu))
	}

	report.Tokens = budget.EstimateTokens(report.Bytes)
	return report
}

// ContextEstimate is the token estimate of the request this Agent would send next: ContextCost's
// standing blocks and tool surface plus the conversation history, measured the way requestRoom.exceeds
// measures the history on an unknown window (domain.PromptChars over the messages, through the
// Budget's chars→token ratio). ContextCost alone counts only what seeds Turn 1; the history term is
// what the conversation has added since, so a compacted conversation estimates as its compacted
// history, never as a fresh session's.
//
// It is what a Driver shows in place of the server's reading while none exists yet (the TUI's `~`
// gauge). Like ContextCost it is an idle-only read — it renders the prompt, composes the menu and
// reads the conversation — so a Driver takes it with no worker driving the Agent.
func (a *Agent) ContextEstimate() int {
	return a.contextEstimate(a.requestRoom())
}

// contextEstimate is ContextEstimate's body over a request room already built (contextCost).
func (a *Agent) contextEstimate(room requestRoom) int {
	return a.contextCost(room).Tokens + room.budget.EstimateTokens(domain.PromptChars(a.conv.Messages(), nil))
}

// contextReading is one Agent's running context-fill estimate between two Upstream readings
// (domain.ContextEstimateEvent): the fill the latest reply reported — its prompt plus completion
// tokens — and the characters of every tool result appended since that reply. A reading holds for
// the reply it came from and no further: streamResponse forgets it as each request starts, so a
// reply that reports no usage leaves NO reading rather than a stale one, and the estimate falls back
// to the Agent's own composition (ContextEstimate). That one rule also covers every wholesale
// change to the conversation — a clear, a restore, a fold, a server or model switch — since each
// is followed by a request before the next tool result can land. It is a plain value struct the
// loop touches only from the goroutine driving its Agent, so it needs no lock.
type contextReading struct {
	tokens     int  // the reading's prompt plus completion tokens
	isReported bool // the latest reply reported usage, so tokens is a reading
	charsSince int  // the characters of every tool result appended since the reading
}

// forget drops the reading as a request starts: until that request's reply reports usage, there
// is none.
func (r *contextReading) forget() { *r = contextReading{} }

// report takes a reply's server-reported fill as the new reading, restarting the count of what
// has been appended since.
func (r *contextReading) report(promptTokens, completionTokens int) {
	*r = contextReading{tokens: promptTokens + completionTokens, isReported: true}
}

// appended counts chars more characters committed to the conversation since the reading.
func (r *contextReading) appended(chars int) { r.charsSince += chars }

// contextEstimateEvent is the ContextEstimateEvent a tool-result append emits: the reading plus the
// estimated tokens of everything appended since it, through the room's chars→token ratio — or,
// with no reading, this Agent's own estimate (contextEstimate) over the same room. It runs on the
// goroutine driving this Agent, which is what makes ContextEstimate's idle-only read safe here: no
// other reader is driving it.
func (a *Agent) contextEstimateEvent(turn int, room requestRoom) domain.ContextEstimateEvent {
	tokens := a.reading.tokens + room.budget.EstimateTokens(a.reading.charsSince)
	if !a.reading.isReported {
		tokens = a.contextEstimate(room)
	}
	return domain.ContextEstimateEvent{
		EventBase:  a.base(turn),
		Tokens:     tokens,
		Calibrated: a.tokens.Calibrated(),
	}
}
