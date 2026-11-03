package agent

import (
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
)

// requestRoom is the one module that knows how big a request is and how much room it has left:
// the request's measure (weight), the room left after it (left), a fresh tool result's share of
// that room (freshShare), the predictive guard's window check (exceeds) and the elision both
// structural bounds render through (fit). It has one reason to change — the arithmetic the fold
// rests on — so no second copy of any of it lives at the seams that read it.
//
// It is a value built from the values the arithmetic reads: one Budget view, the growthBounds
// derived from it (deriveGrowthBounds, the one substitution site for an unknown window), the
// standing rows of ONE render of the standing table (standingRenders — the rows ContextCost
// counts, joined into the system content buildRequest seeds at position 0), the tool menu the
// request offers and the tool surface that menu sends (toolSurface, wire.go — on a text-tool
// profile the instruction block, never the menu JSON). It reaches no Agent, so the arithmetic is
// testable as a table; Agent.requestRoom gathers the values live.
//
// A room is built once per tool-result commit and handed down as a value, never cached on the
// Agent past that commit (commitCall, dispatch.go — ADR 0084 D1): the standing content moves on a
// mode switch, a task_list call or an opened seat, so a room outliving its commit would measure a
// render the request no longer carries.
type requestRoom struct {
	budget   domain.Budget
	bounds   growthBounds
	rows     []standingRender
	standing string
	menu     []domain.ToolDef
	tools    toolSurface
}

// newRequestRoom builds the room from one Budget view, the rows of one standing render (nil when
// nothing is seeded), the tool menu and the tool surface that menu sends.
func newRequestRoom(b domain.Budget, rows []standingRender, menu []domain.ToolDef, tools toolSurface) requestRoom {
	return requestRoom{
		budget:   b,
		bounds:   deriveGrowthBounds(b),
		rows:     rows,
		standing: joinStanding(rows),
		menu:     menu,
		tools:    tools,
	}
}

// requestRoom gathers this Agent's room live from ONE render of the standing table: the Budget is
// derived from those very rows (budgetOf), so the room's measure, its share and its Budget all
// read the render the room carries, and the tool menu as it stands now with the surface it sends.
func (a *Agent) requestRoom() requestRoom {
	rows := a.standingRenders()
	menu := a.toolMenu()
	return newRequestRoom(a.budgetOf(rows), rows, menu, a.toolSurfaceOf(menu))
}

// weight is the estimated tokens of a request carrying msgs: the standing system content seeded
// ahead of them, the messages and the tool surface (r.tools — what the menu sends, the instruction
// block on a text-tool profile), through the Budget's chars-per-token ratio — the predictive
// guard's own measure, with no comfort margin (ADR 0018 §7) and no second estimator.
func (r requestRoom) weight(msgs []domain.Message) int {
	return r.budget.EstimateTokens(r.requestChars(requestMessages(r.standing, msgs)))
}

// requestChars is the characters a request carrying msgs and offering the room's menu sends: the
// messages PromptChars-style plus the room's tool surface (toolSurface, wire.go). It is the one
// measure weight, freshShare and exceeds share, and the figure calibration counts over the sent
// request (streamResponse).
func (r requestRoom) requestChars(msgs []domain.Message) int {
	return domain.PromptChars(msgs, nil) + r.tools.chars
}

// left is the room a request carrying msgs leaves: the room (the advertised window less the reply
// reserve) less its weight. It goes negative once the request is already past the room.
func (r requestRoom) left(msgs []domain.Message) int {
	return r.bounds.room - r.weight(msgs)
}

// freshShare is the share, in tokens, of the room a tool result about to be appended may take —
// the bound it is clamped to once its trailers' weight is taken off (trailedShare). With
// a known window it is the result's share of the room left in the request: the advertised window
// less the reply reserve — the very room the predictive guard measures (deriveGrowthBounds,
// exceeds) — less the estimate of contents, the next request as buildRequest will send it without
// the result (Agent.requestContents: the standing system content, the conversation, the
// delegation-ledger note, the wrap-up directive, the pending deferred injections and the tool
// surface), divided across the pending results of the Turn still to append
// (turnLifecycle.resultsToAppend, counted by dispatchTools). It is recomputed at each append, so
// the room a small result leaves unused rolls forward to the next. The share is clamped to [1/8 of
// the History allocation, the History allocation]: never above the structural ceiling the fold's
// arithmetic rests on (structuralFloor), and never below the floor, where the predictive fold
// makes room rather than the result shrinking to nothing.
//
// contents is weighed as the predictive guard weighs the request it checks — its messages and the
// room's tool surface (contents offers the room's own menu — requestContents) through the Budget's
// ratio, no comfort margin (ADR 0018 §7) — so a result held at its share
// leaves a request the guard finds within the room. With no window known the bound is the History
// floor (growthBounds.historyFloor) — the conservative ceiling the fold renders against — exactly
// as before the room-left bound (ADR 0018 §8).
//
// Structural like the clamp it feeds (ADR 0006): no config key, on under Bypass.
func (r requestRoom) freshShare(contents *domain.Request, pending int) int {
	if !r.bounds.windowKnown {
		return r.bounds.historyFloor
	}
	left := r.bounds.room - r.budget.EstimateTokens(r.requestChars(contents.State().Messages))
	share := left / max(pending, 1)
	return min(max(share, r.resultFloor()), r.bounds.historyFloor)
}

// resultFloor is the least a fresh tool result is ever bounded to: 1/8 of the History allocation
// (freshResultFloorDivisor), never below one token. Below it the predictive fold makes room
// rather than the result shrinking further.
func (r requestRoom) resultFloor() int {
	return max(r.bounds.historyFloor/freshResultFloorDivisor, 1)
}

// trailerWeight is the estimated tokens a result's trailers add to its tool message: the
// characters trailed carries past bare — bare with its advice spans and fired budget notes
// rendered on (withTrailers, dispatch.go) — through the Budget's chars-per-token ratio, rounded up
// as every estimate is. The trailers' fences do not depend on the body they follow, so bare may
// carry no content at all.
func (r requestRoom) trailerWeight(bare, trailed domain.Message) int {
	chars := domain.PromptChars([]domain.Message{trailed}, nil) - domain.PromptChars([]domain.Message{bare}, nil)
	return r.budget.EstimateTokens(chars)
}

// trailedShare is the bound a fresh tool result carrying trailers (trailerWeight tokens of them)
// is clamped to: its share (freshShare, never above the History allocation) less the trailers'
// weight, so the result and its trailers together stay within the share — on a known window the
// room left, on an unknown one the History floor — and never above the History allocation. It
// never drops below resultFloor: past that the predictive fold makes the room, as it does for a
// bare result.
func (r requestRoom) trailedShare(share, trailers int) int {
	return max(min(share, r.bounds.historyFloor)-trailers, r.resultFloor())
}

// structuralFloor is the CEILING behind both structural clamps: the whole History allocation — the
// most any single body could occupy and still leave the conversation renderable. A reference block
// is bounded by its share of it (refBound); a tool result by its share of the room left in the
// request, never above it (freshResultBound), and by exactly it when no window is known. Content past it
// can never survive ANY reducer, so committing it whole buys nothing and can doom the Turn
// outright: a fresh giant body IS the most recent message the emergency fold's own summary call
// must carry, which that call can render only byte-elided to its room (renderBudgetedTranscript) —
// and before it elided one (2026-11-02), kept whole, overflowing the fold that was supposed to
// rescue the Turn.
//
// With an unknown window (a zero History allocation — Allocate had no basis to allocate) the floor
// measures against compactUnknownWindowTranscriptTokens instead of standing down — the substitution
// growthBounds.historyFloor makes at the one site every growth bound shares (deriveGrowthBounds,
// compact.go), so this clamp and the boundary trigger bound against one number. Being inert there
// was not the conservative choice it looked like: an unclamped giant body becomes the one message
// the emergency fold cannot shed — kept whole by the fold's transcript render until 2026-11-02,
// and so re-wedging the session bounding the fold was meant to un-wedge (audit 2026-08-01,
// follow-up B); byte-elided by it since, the last-resort rescue rather than the intended shape. Keying the floor to the fold's own unknown-window budget makes the two meet exactly:
// content that survives the clamp is, by construction, content the fold can still render.
//
// The threshold sits deliberately far above the tool-result-cap Floor guard's: the whole History
// allocation — the working room less what this session's standing content MEASURES (plus headroom,
// each part floored at 2% of the room), floored at half that room and then capped — the cap binds
// below the floor at a small window — chosen because it puts History at or below the emergency
// fold's own transcript budget, which is the property that keeps the fold
// survivable, while the guard's tighter 40%-of-working-room cap shapes the ordinary case. That
// ordering used to be arithmetic that only held above ~8.9k tokens; it is now construction: the
// Budget caps History at the fold's transcript budget itself (HistoryCap, applied where History is
// produced in Agent.budget), so content that survives this clamp is content the fold can render at
// EVERY advertised window (ADR 0018 §8).
func (r requestRoom) structuralFloor() int {
	return r.bounds.historyFloor
}

// exceeds reports whether req's prompt is ALREADY estimated to be too big for the model's context
// window — the predictive half of overflow protection, read by step() between building a request
// and sending it. conv is the conversation the request was projected from.
//
// The measure is the one the whole engine shares: domain.PromptChars over the request's projected
// messages plus the room's tool surface (requestChars — req is checked before MomentPreRequest, so
// it still offers the room's own menu), through the Budget's calibrated chars→token ratio
// (domain.Budget.EstimateTokens), so this guard can never disagree with the compaction trigger or
// a hook reading the same Budget. The threshold is the HARD room — the ADVERTISED window less the
// reply reserve (Window − ResponseReserve) — uncalibratedRoomMargin times it while the ratio is
// still the uncalibrated seed — deliberately not a softer fraction: a fold is a lossy rewrite of
// the user's history, so it must fire only when the estimate says the request cannot fit at all,
// never as a comfort margin. That is also why it reads Window rather than the working ceiling
// ContextLimit: a `working-window:` bound is a soft line the reducers keep the session under, and
// folding at it would fire this guard on every request a bounded session deliberately lets run
// past its working room while still fitting the server's window. The History allocation — the
// working room less the measured standing reservations, floored at half that room and then capped
// at the fold's transcript budget, which at a small window binds below that floor — stays the
// boundary trigger's business (Budget.HistoryExceedsAllocation), not this one's — and that one
// DOES follow the working ceiling, which is how the bound actually bites.
//
// With an UNKNOWN window (no discovery, no config: Allocate returns the zero Allocation, leaving
// no working room) BOTH sides of the compare change. The room becomes
// compactUnknownWindowTranscriptTokens — the same conservative ceiling the emergency fold renders
// against, substituted at the one site every growth bound draws on (deriveGrowthBounds,
// compact.go) — and the measure becomes the TRANSCRIPT alone: conv, without the tool menu
// and without the standing system content the request projection seeds at position 0. That
// ceiling is a transcript budget (compact.go), the transcript is the only part a fold can shed,
// and the boundary trigger measures exactly the same quantity against the same number
// (historyExceedsAllocation), so on an unknown window all three bounds read one number through one
// measure.
//
// Measuring the whole REQUEST against that ceiling instead would compare a request-sized quantity
// to a transcript-sized bound, and the fixed costs alone settle it: the default 19-tool menu is
// ~11.5k characters ≈ 3.8k tokens at a code-heavy 3.0 chars/token, already past the ceiling with an
// empty conversation. The guard would then fire on a four-message session and fold every Exchange
// without any fold ever getting under the bound — a comfort margin, which decision 7 forbids
// outright (audit 2026-08-01, follow-up B).
//
// It used to be INERT here, which left the ONE case the reactive path cannot cover (a 400 the
// provider cannot classify as an overflow) with no protection at all on exactly the sessions that
// have no window to protect them. Bounding against an assumed small window means a large window a
// server never advertised is managed as if it were small — the give-up and saturation events both
// name `context-window:` for that reason, and a pinned window restores the exact arithmetic above.
//
// The estimate is advisory either way: an over-estimate costs one fold, and an under-estimate
// costs nothing, because the wire overflow still routes to the reactive path. That asymmetry is
// why an UNCALIBRATED Budget is measured against uncalibratedRoomMargin (documented on it) times
// the room rather than the bare room — the ratio's uncertainty is independent of the window's, so
// the margin applies to the assumed room as well.
func (r requestRoom) exceeds(req *domain.Request, conv []domain.Message) bool {
	room, chars := r.bounds.room, 0
	if r.bounds.windowKnown {
		chars = r.requestChars(req.State().Messages)
	} else {
		chars = domain.PromptChars(conv, nil)
	}
	if r.budget.Used == 0 {
		room *= uncalibratedRoomMargin
	}
	return r.budget.EstimateTokens(chars) > room
}

// fit is the RENDERING both structural clamps share: content whose estimated tokens exceed
// bound is replaced by the head/tail-plus-marker elision (context.TruncateToolResult — the same
// shape the tool-result-cap guard renders), so the model reads ONE "the middle was dropped, re-read the
// range" idiom whichever seam produced it. Content within bound is returned untouched.
//
// The line elision keeps whole head and tail lines, so a body of a few very long lines (minified
// JSON, a one-line page) comes out of it still over bound — or longer than it went in. Such a body
// is byte-elided instead (context.ElideToBytes, the same marker) to the bound in characters, so
// the result is within bound for any input shape; content the line elision already brings under
// bound comes back exactly as that elision renders it.
func (r requestRoom) fit(content string, bound int) string {
	if r.budget.EstimateTokens(len(content)) <= bound {
		return content
	}
	maxChars := int(float64(bound) * r.budget.CharsPerToken)
	clamped := apogeectx.TruncateToolResult(content, maxChars)
	if r.budget.EstimateTokens(len(clamped)) <= bound {
		return clamped
	}
	return apogeectx.ElideToBytes(content, maxChars)
}
