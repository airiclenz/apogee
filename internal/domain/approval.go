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
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

// ApprovalRequest describes the pending tool call the human is asked to allow.
type ApprovalRequest struct {
	Tool      string
	Arguments json.RawMessage
	Reason    string // why approval is required (e.g. "write", "unconfinable MCP tool")
}

// ApprovalDecision is the Approver's verdict.
type ApprovalDecision string

const (
	ApprovalAllow           ApprovalDecision = "allow"
	ApprovalDeny            ApprovalDecision = "deny"
	ApprovalAllowForSession ApprovalDecision = "allow-for-session"
)
