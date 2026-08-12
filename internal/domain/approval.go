package domain

import (
	"context"
	"encoding/json"
)

// ----------------------------------------------------------------------------
// Approval (CONTEXT: Approval; ADR 0004 — MCP gates through Approval even in Auto)
// ----------------------------------------------------------------------------

// Approver is the host-supplied human-in-the-loop gate on a single tool call. In
// Ask-Before mode it is consulted for every call; in Auto mode it is consulted only
// for tools that cannot be confined (e.g. MCP — ADR 0004). It is called
// synchronously inside a Step and may block on the human; cancelling ctx unblocks it.
//
// An Approver NEVER has to be safe for concurrent use: the engine QUEUES requests on its
// side, so an implementation sees one Approve at a time even while a depth-0 sub-agent
// fan-out has several children running (ADR 0039 decision 12). That queue is what makes
// "one prompt on the screen" true without every Driver building a queue of its own — the
// asking child blocks on its turn while its siblings keep working, and the host's
// wait-tolerance (ADR 0031) is what lets a queued request wait as long as the human takes.
//
// The queue spans BOTH kinds of prompt, not just this one: an Approval and an ask_user question
// (Asker) contend for a single PromptSlot, so a host is never asked to approve something while it
// still owes an answer to a question, or the reverse. A Driver therefore needs exactly one prompt
// surface, and may implement Approver and Asker over the same one.
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

// ApprovalRequest describes the pending tool call the human is asked to allow.
type ApprovalRequest struct {
	Tool      string
	Arguments json.RawMessage
	Reason    string // why approval is required (e.g. "write", "unconfinable MCP tool")
	// Remedy is the OPTIONAL one-line route out of the condition that forced this approval — the
	// answer to "and what do I do about it", for the gates whose cause is something the user can
	// actually change. Today that is the confinement-unavailable pair: the Auto ladder cell where
	// the host cannot fence a subprocess, and the runtime demote where the box failed to establish.
	//
	// It is EMPTY on every gate whose cause is the autonomy rung itself — an ask-before write has
	// nothing to fix, only a mode to be in — so most prompts read exactly as they always have. Like
	// Reason it is a bare sentence: any label a Driver paints in front of it is that Driver's
	// presentation choice, not the engine's (ADR 0031, wire-silent).
	Remedy string
	// SubAgentTask is the delegated task of the SUB-AGENT whose call this is — the answer to
	// "which agent is asking", which stops being obvious the moment several children run at
	// once and their prompts queue one behind another (ADR 0039). It is the task text from the
	// spawning sub_agent call, carried by the child that call spawned, so a nested delegation
	// names its OWN task rather than its parent's.
	//
	// It is empty when the top-level agent asks: a session that never delegates carries no
	// extra fact, and its prompt reads exactly as it always has.
	SubAgentTask string
	// SubAgentName is the OPTIONAL short name the spawning sub_agent call gave that child — the
	// few words a human recognises it by where SubAgentTask is a whole sentence. It is DISPLAY
	// identity only, never privilege (ADR 0005): a prompt reads better for it, nothing is
	// decided by it.
	//
	// It is empty whenever the delegation carried no name (and always at depth 0), which is the
	// signal to fall back to SubAgentTask — so a Driver needs no second flag to know which of
	// the two to paint.
	SubAgentName string
	// CacheKey is the allow-for-session IDENTITY of this request: the key an
	// ApprovalAllowForSession verdict is remembered under for the rest of the Session, so a later
	// call resolving to the same key raises no second prompt — anywhere in the agent tree, since
	// the memory is the Session's rather than one Agent's.
	//
	// An EMPTY key means this decision can never be remembered: the request is a forced gate (a
	// Tier-2 speed-bump, a runtime demote), where "allow for session" authorises this one call and
	// nothing more. The engine populates the field; a host may read it — to grey out an affordance
	// that would not be honoured, say — but is free to ignore it, because it is the engine and not
	// the host that keeps the memory.
	CacheKey string
	// ResolvedPath is where this call's path argument REALLY points — the absolute path with
	// every symlink resolved — and it is populated ONLY when that differs from the path the
	// argument names. Empty is therefore the ordinary case and means "the argument names its
	// own target", so a host that renders this unconditionally adds nothing to an ordinary
	// prompt and names the redirection on the one prompt where the two part company.
	//
	// It exists because every other field on this request quotes the model: a `docs/notes.md`
	// whose `docs` is a symlink out of the workspace reads as an in-workspace write on a pane
	// built from Arguments alone, right up to the moment it lands elsewhere. The engine
	// already computes this path — it is what the blast-radius classification judges the call
	// by — and this field carries that same value, so what the human is shown and what the
	// gate decided from cannot be two different readings of one call.
	//
	// It is a path the MODEL's argument produced, so a Driver treats it as model-authored
	// text like any other: strip it, flatten it, bound it, exactly as it does Arguments.
	// Today only a write target populates it; a read that follows a symlink is the same fact
	// about a different verb and rides this same field.
	ResolvedPath string
}

// ApprovalDecision is the Approver's verdict.
type ApprovalDecision string

const (
	ApprovalAllow           ApprovalDecision = "allow"
	ApprovalDeny            ApprovalDecision = "deny"
	ApprovalAllowForSession ApprovalDecision = "allow-for-session"
)
