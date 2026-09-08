package agent

import (
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/processing"
)

// The Floor guards' configuration keys, one per guard. The key is the guard's identity everywhere
// outside internal/floor: it is what a user writes in config.yaml, what SetFloor switches, and what
// a FloorGuardEvent names — so an observer never has to map an internal name back to the switch
// that turns the behaviour off.
const (
	guardToolCallRepair        = "tool-call-repair"
	guardToolCallSalvage       = "tool-call-salvage"
	guardToolLoopBreaker       = "tool-loop-breaker"
	guardEmptyResponseRecovery = "empty-response-recovery"
	guardToolUseEnforcer       = "tool-use-enforcer"
	guardReadCache             = "read-cache"
	guardToolResultCap         = "tool-result-cap"
)

// The actions a Floor guard books on its event: a guard that re-streams the Turn with a correction
// took guardActionRetry, one that reshaped a pending tool call before it ran took
// guardActionIntercept, one that shrank content in the outgoing request took guardActionCap, and
// one that read a call the model wrote into its own text back onto the response took
// guardActionSalvage.
const (
	guardActionRetry     = "retry"
	guardActionIntercept = "intercept"
	guardActionCap       = "cap"
	guardActionSalvage   = "salvage"
)

// SetFloor replaces the live Floor-guard gates for the rest of the session, mirroring
// SetPruneToolResults. Each guard is consulted once per firing seam, so a guard switched off stops
// at the next seam and one switched back on arms again with no rebuild; nothing already corrected
// is undone, the guards being decisions about a response that has already been reviewed.
//
// It takes the WHOLE FloorConfig rather than one flag at a time because the seven guards are read
// as one value at each seam, and a caller that owns the settings surface owns all seven. It is safe to
// call from another goroutine while a Step runs, like SetMode. A sub-agent spawned AFTER the switch
// inherits the new value at spawn.
func (a *Agent) SetFloor(gates domain.FloorConfig) {
	a.floorMu.Lock()
	a.floor = gates
	a.floorMu.Unlock()
}

// floorConfig reports the live Floor-guard gates under the lock, so a seam's decision is race-free
// against a concurrent SetFloor. cfg.Floor is only the construction seed.
func (a *Agent) floorConfig() domain.FloorConfig {
	a.floorMu.RLock()
	defer a.floorMu.RUnlock()
	return a.floor
}

// runPostResponseGuards runs the post-response Floor guards against resp and reports whether the
// Turn should re-stream and with what correction. It runs BEFORE the lab hooks at this seam
// (runPostResponseHooks): the guards are engine behaviour every model runs with, so a malformed or
// looping response is repaired before a catalogued Mechanism ever looks at it.
//
// The order is ratified (ADR 0071): tool-call salvage, then tool-loop breaker, tool-call repair,
// empty-response recovery, tool-use enforcer — the coarser "you are going in circles" judgment
// before the finer "this call is malformed" one, then the two recoveries for a Turn that produced no
// usable call at all. Those four triggers are disjoint in practice (a response either carries calls
// or does not), so their order is about a stable answer rather than a contested one, and AMONG THEM
// THE FIRST GUARD TO FIRE WINS: its correction is the one the Turn re-streams with, and the
// remaining guards do not run, exactly as an ActionRetry short-circuits the hook cascade.
//
// SALVAGE IS THE EXCEPTION and runs FIRST for it: it does not correct the response, it completes it
// — a call the model wrote as JSON in its text is put back on the response as a call — so it returns
// no retry and does not short-circuit. Running it first is what makes the four below judge the
// response the model MEANT: a Turn whose only call was written into the text is no longer an empty
// or narrating Turn by the time the recoveries look at it, so it is answered by dispatching that
// call rather than by a retry the model did not need.
//
// No guard carries strikes-3 suppression or a Turn-Budget throttle (ADR 0071 decision 1): a Floor
// guard cannot regress Bypass, so it is never withdrawn. The per-Turn maxPostResponseRetries bound —
// shared with the hook retries, counted once by the caller — is the only limiter.
func (a *Agent) runPostResponseGuards(turn int, resp *domain.Response) (retry bool, inject string) {
	gates := a.floorConfig()

	if !gates.DisableToolCallSalvage {
		a.salvageToolCallFromText(turn, resp)
	}
	if !gates.DisableToolLoopBreaker {
		if directive, fired := floor.ToolLoopBreak(resp); fired {
			a.emitFloorGuard(turn, guardToolLoopBreaker, guardActionRetry, "")
			return true, directive
		}
	}
	if !gates.DisableToolCallRepair {
		if correction, fired := floor.ToolCallRepair(resp, a.registeredToolNames()); fired {
			a.emitFloorGuard(turn, guardToolCallRepair, guardActionRetry, "")
			return true, correction
		}
	}
	if !gates.DisableEmptyResponseRecovery {
		if nudge, fired := floor.RecoverEmpty(resp); fired {
			a.emitFloorGuard(turn, guardEmptyResponseRecovery, guardActionRetry, "")
			return true, nudge
		}
	}
	if !gates.DisableToolUseEnforcer {
		if correction, fired := floor.EnforceToolUse(resp); fired {
			a.emitFloorGuard(turn, guardToolUseEnforcer, guardActionRetry, "")
			return true, correction
		}
	}
	return false, ""
}

// salvageToolCallFromText runs the tool-call salvage guard (floor.SalvageToolCall) and, when it
// fires, puts what it read back onto the response: the text stripped of the block that held the
// call, and one appended ToolCall per call the model wrote. It returns nothing — salvage completes a
// response rather than correcting it, so there is no retry to ask for and no cascade to stop.
//
// Two gates stand in front of it, both about whether reading the text as a call could be WRONG here
// rather than about whether the model stumbled:
//
//   - Only a NATIVE profile (processing.IsNative). A markdown-fenced or custom-regex profile already
//     extracts a call from the visible text at the parse seam (assembleResponse), so salvaging the
//     same text again would dispatch the call twice.
//   - Never on the WRAP-UP Turn (Agent.wrapUp). That Turn is offered no menu at all and its reply is
//     a closing report by construction: a call salvaged out of it would be a call the delegation had
//     already been told it could not make.
//
// The menu it salvages AGAINST is the REQUEST's — resp.View().Tools(), what this Turn was actually
// offered — and deliberately not registeredToolNames' whole registry, which is the repair guard's
// question. A name the menu withdrew is not a call to run: the mode that withdrew it owes the model
// its own answer, exactly as it does for a native call carrying that name.
//
// Each salvaged call is given a deterministic Turn-derived ID in the loop's own synthesized style
// (loop.go's text_call_<turn>), extended with the call's position because one Turn's text may hold
// several: snapshot, resume and tests stay stable across runs.
func (a *Agent) salvageToolCallFromText(turn int, resp *domain.Response) {
	if a.wrapUp || !processing.IsNative(a.textParser) {
		return
	}

	calls, text, fired := floor.SalvageToolCall(resp, offeredToolNames(resp.View()))
	if !fired {
		return
	}

	resp.SetText(text)
	names := make([]string, 0, len(calls))
	for i, call := range calls {
		call.ID = fmt.Sprintf("text_call_%d_%d", turn, i)
		resp.AppendToolCall(call)
		names = append(names, call.Tool)
	}
	a.emitFloorGuard(turn, guardToolCallSalvage, guardActionSalvage,
		fmt.Sprintf("salvaged %s from content", strings.Join(names, ", ")))
}

// runPreToolExecGuards runs the pre-tool-exec Floor guards against the call the loop is about to
// dispatch, reshaping it in place. Like the post-response guards it runs BEFORE the lab hooks at
// this seam (runPreToolExecHooks), so a catalogued Mechanism sees the call the floor left behind.
//
// There is one guard here today — the read cache — so there is no order to ratify; a second would
// join the same chain, each consulted independently because a shaping guard has nothing to
// short-circuit. A guard that changed nothing is silent, exactly as a hook that did not intervene
// books no fire.
//
// The call is passed through a domain.ToolCallEdit, the same wrapper the hooks mutate through, so a
// guard's write reaches the pending call the loop owns and the loop commits what the guard left.
func (a *Agent) runPreToolExecGuards(turn int, call *domain.ToolCall) {
	if a.floorConfig().DisableReadCache {
		return
	}
	if floor.CacheRead(a.loopView(turn), domain.NewToolCallEdit(call)) {
		a.emitFloorGuard(turn, guardReadCache, guardActionIntercept, "")
	}
}

// runPreRequestGuards runs the pre-request Floor guards against the request the loop is about to
// send, reshaping it in place. Like the other two seams it runs BEFORE the lab hooks at this seam
// (runPreRequestHooks), so a catalogued Mechanism shapes the request the floor left behind.
//
// There is one guard here today — the tool-result cap — so there is no order to ratify. It edits
// only the PROJECTED REQUEST: the conversation keeps every result whole, so a later Turn, a session
// snapshot and the rendered transcript are unaffected by what the model was spared reading again.
//
// The guard runs on EVERY request the Turn sends, the re-derived one a fold produced included: the
// fold rewrote the history, so the results the cap would have trimmed may not even be there any
// more, and re-asking is cheaper than reasoning about which ones survived.
func (a *Agent) runPreRequestGuards(turn int, req *domain.Request) {
	if a.floorConfig().DisableToolResultCap {
		return
	}
	if capped := floor.CapToolResults(req); capped > 0 {
		a.emitFloorGuard(turn, guardToolResultCap, guardActionCap, "")
	}
}

// emitFloorGuard books one guard firing as a FloorGuardEvent, the guards' counterpart to the
// MechanismFiredEvent a hook books. It is emitted only when the guard ACTED — a guard that found
// nothing is silent, the same rule the hook seam applies.
//
// detail is the guard's optional supporting text, rendered verbatim where a Driver renders it at all
// (the TUI's hidden debug view) and empty for every guard whose key and action already say the whole
// of what it did. Only salvage fills it today: which tool it read back out of the text is the one
// fact its key cannot carry.
func (a *Agent) emitFloorGuard(turn int, guard, action, detail string) {
	a.cfg.Events.Emit(domain.FloorGuardEvent{
		EventBase: a.base(turn),
		Guard:     guard,
		Action:    action,
		Detail:    detail,
	})
}
