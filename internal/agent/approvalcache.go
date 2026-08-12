package agent

import (
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
)

// approvalCache is the Session's allow-for-session memory: the set of ApprovalRequest.CacheKey
// identities a human has already cleared for the rest of this Session. What ONE cleared identity
// stands for is the resolver's to decide (gateCacheKey — resolution.go): the tool name plus a
// digest of the call's arguments for every class but MCP, which keeps ADR 0012's server grain.
// The memory itself only remembers keys, and remembers them for as long as the Session lasts —
// including across a /clear (see ClearContext). There is ONE per agent
// tree — it hangs off the approver seam (queuedApprover), the single object a parent and all its
// descendants share — so "allow for session" means the whole Session and not merely the Agent that
// happened to ask. An allow granted inside a sub-agent therefore clears the prompt for its parent
// and its siblings too, and outlives the child that earned it.
//
// It is guarded because the tree is concurrent: a depth-0 fan-out runs siblings at the same instant
// (ADR 0039), and while the prompt slot serializes the ASKING, the reads happen off it. It is
// in-memory only and never persisted — a restored Session starts with an empty memory, which is the
// conservative direction (a prompt too many, never an unapproved call).
//
// Every method is nil-receiver-safe: a rig that reaches the seam unwrapped (a unit test, a Driver
// embedding the engine differently) degrades to prompting per call rather than panicking.
type approvalCache struct {
	mu      sync.Mutex
	allowed map[string]bool
}

// Allowed reports whether key has been allowed for this Session. An empty key is never allowed:
// it is the "this decision cannot be remembered" signal a forced gate carries, so it must not be
// answerable from memory even if something managed to store one.
func (c *approvalCache) Allowed(key string) bool {
	if c == nil || key == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.allowed[key]
}

// Allow remembers key for the rest of this Session. An empty key is a no-op — the same
// unrememberable-decision signal, refused at the write side too, so the rule holds however the
// caller reached here.
func (c *approvalCache) Allow(key string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.allowed == nil {
		c.allowed = make(map[string]bool)
	}
	c.allowed[key] = true
}

// sessionAllows reaches the Session's allow-for-session memory through the Approver an Agent holds.
// The memory is not the Agent's to own — it belongs to the queueing seam (queuedApprovals), the one
// object a parent and all its descendants share — so a dispatch that wants to know "has the human
// already cleared this key" asks the seam it is about to call rather than a map of its own. That is
// what makes an allow granted in one sub-agent visible to its parent and its siblings.
//
// Anything that is not the seam has no memory and answers nil: a nil Approver (no human to ask at
// all) and any Approver a rig installed without going through construction. nil is a working answer,
// not a failure — approvalCache's methods are nil-receiver-safe, so such a rig simply prompts on
// every call, which is the conservative direction.
func sessionAllows(ap domain.Approver) *approvalCache {
	q, ok := ap.(*queuedApprover)
	if !ok || q == nil {
		return nil
	}
	return q.cache
}
