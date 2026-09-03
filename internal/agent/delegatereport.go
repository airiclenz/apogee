package agent

// The engine-owned DELEGATE REPORT BLOCK: the part of the standing system content (loop.go's
// standingSystem) that rides between the orientation block and the workspace context files'
// blocks, and only on a DELEGATED Agent (depth > 0). It states the one fact a child cannot learn
// from any configured source — the agent that delegated the task sees nothing of this conversation
// and receives only the child's FINAL reply — and asks for that reply in the shape a parent can
// act on: what was found, what changed, what is unfinished, cited by path:line rather than pasted.
//
// It is engine-owned with no config key and no Mechanism gate, wrapUpDirectiveFormat's precedent
// (subagent.go): the child reads it as the contract for the reply it owes, so it is on under
// Bypass, at every depth above 0, routed and unrouted alike, and no workspace can edit it out.
//
// Position is the same SECURITY property the orientation block's is (F-19, orientation.go): every
// engine-owned part rides AHEAD of the repo-controlled context blocks, so no workspace text can
// precede — and thereby read as a correction of — the host's own statements. delegateReportFence
// below is the other half of that guard for this block, exactly as orientationHeader() is for the
// orientation's.
//
// It RIDES ALONG under the orientation block's rule (ADR 0023 §6 amendment, third addendum
// 2026-09-02): standingSystem composes it in only when a configured source already put something
// in the message, never on its own, so the no-prompt-AND-no-context-files anchor stays
// byte-identical on the wire and the Bypass floor with it.

import "strings"

// DelegateReportBlock is that block's text, verbatim.
//
// EXPORTED — and re-exported as apogee.DelegateReportBlock — because it is a contract line a
// Driver's own tests assert on the wire (SeatFallbackNote's precedent, subagent.go): reading the
// bytes from here is what keeps those assertions from being a retyped copy that can drift.
//
// A plain const rather than a prompts/ asset: the text takes no placeholder, so it needs neither
// the positional loader nor mustPrompt.
const DelegateReportBlock = `You are a sub-agent: another agent delegated this task to you and is waiting on the result. It cannot see this conversation — your FINAL reply is the only thing it receives, so anything you do not write there is lost.

Report what you found, what you changed, and what remains unfinished. Refer to code by path and line (path:line) rather than pasting file contents — the agent you report to can read the workspace itself.`

// delegateReportFence is the block's opening SENTENCE: the line a workspace context file would
// have to spell to pass its own prose off as the engine's delegate block, and the prefix
// forgesStandingStructure (contextfiles.go) fences against. Without it a repo AGENTS.md opening
// "You are a sub-agent: another agent delegated…" would reach a child AFTER the real block and
// read as a correction of it — the F-19 failure the fence is the second half of the guard against.
//
// DERIVED from the const rather than retyped, so the two halves of the fence cannot drift apart,
// and derived through a panic rather than a slice: a "" prefix would make HasPrefix true of every
// line and fence whole context files, which is a far worse failure than not building.
var delegateReportFence = mustFirstSentence(DelegateReportBlock)

// mustFirstSentence returns everything up to and including s's first sentence-ending period,
// panicking when it has none — a programming error in a build-time constant, unreachable in a
// built binary the way mustPrompt's missing asset is.
func mustFirstSentence(s string) string {
	end := strings.Index(s, ". ")
	if end < 0 {
		panic("apogee: DelegateReportBlock has no first sentence for the forgery fence to name")
	}
	return s[:end+1]
}

// delegateReportBlock returns the block for a DELEGATED Agent and "" for a top-level one. Depth is
// the whole gate: a child is a child however it was spawned, so a routed delegation and an
// unrouted one carry the identical text, and a grandchild carries it too — every agent whose reply
// is consumed by another agent rather than read by the human.
//
// KV cache: a constant, so like the orientation block it is prefix-cache-stable for the life of a
// session (ADR 0023 §6).
func (a *Agent) delegateReportBlock() string {
	if a.depth <= 0 {
		return ""
	}
	return DelegateReportBlock
}
