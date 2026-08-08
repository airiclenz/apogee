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
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

// ApprovalRequest describes the pending tool call the human is asked to allow.
type ApprovalRequest struct {
	Tool      string
	Arguments json.RawMessage
	Reason    string // why approval is required (e.g. "write", "unconfinable MCP tool")
	// SubAgentTask is the delegated task of the SUB-AGENT whose call this is — the answer to
	// "which agent is asking", which stops being obvious the moment several children run at
	// once and their prompts queue one behind another (ADR 0039). It is the task text from the
	// spawning sub_agent call, carried by the child that call spawned, so a nested delegation
	// names its OWN task rather than its parent's.
	//
	// It is empty when the top-level agent asks: a session that never delegates carries no
	// extra fact, and its prompt reads exactly as it always has.
	SubAgentTask string
}

// ApprovalDecision is the Approver's verdict.
type ApprovalDecision string

const (
	ApprovalAllow           ApprovalDecision = "allow"
	ApprovalDeny            ApprovalDecision = "deny"
	ApprovalAllowForSession ApprovalDecision = "allow-for-session"
)
