package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The Session's allow-for-session memory (approvalcache.go, in the approver seam)
// ----------------------------------------------------------------------------
//
// "Allow for session" is a promise about the SESSION, so the question every test here asks is the
// same one: was the human asked again for something they had already allowed? The first group
// exercises the seam DIRECTLY, because the rules it enforces must hold whoever populates
// ApprovalRequest.CacheKey. The second group drives real Agent trees, where the promise is actually
// made or broken: dispatch populates the key, and an allow granted anywhere in the tree must clear
// the prompt everywhere in it.

// seamApprover is a host Approver that answers with one scripted decision and keeps every request
// it was handed — the record that answers "was the human asked at all". Its optional channels let a
// test park a call inside the prompt, which is the only state a queued twin can be observed in.
type seamApprover struct {
	decision domain.ApprovalDecision

	mu   sync.Mutex
	seen []domain.ApprovalRequest

	entered chan struct{} // signalled on entry when non-nil, so a test knows the prompt is open
	release chan struct{} // waited on before answering when non-nil, holding the prompt open
}

func (s *seamApprover) Approve(_ context.Context, req domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	s.mu.Lock()
	s.seen = append(s.seen, req)
	s.mu.Unlock()

	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.release != nil {
		<-s.release
	}
	return s.decision, nil
}

// keysSeen returns the cache key of each request the human was actually asked, in order.
func (s *seamApprover) keysSeen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, 0, len(s.seen))
	for _, req := range s.seen {
		out = append(out, req.CacheKey)
	}
	return out
}

// TestApprovalSeam_RemembersOnlyRememberableAllows pins which answers the seam is allowed to
// remember, one sequential call at a time: an allow-for-session under a non-empty key and nothing
// else. The distinctions are the whole safety story — a key that is not the same key, and a
// decision that is not "for the session", must each still reach the human.
func TestApprovalSeam_RemembersOnlyRememberableAllows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		decision    domain.ApprovalDecision
		keys        []string
		wantPrompts int
	}{
		{
			name:        "one allow-for-session answers every later call under the same key",
			decision:    domain.ApprovalAllowForSession,
			keys:        []string{"write:notes.md", "write:notes.md", "write:notes.md"},
			wantPrompts: 1,
		},
		{
			name:        "a different key is a different decision",
			decision:    domain.ApprovalAllowForSession,
			keys:        []string{"write:notes.md", "mcp-server:github"},
			wantPrompts: 2,
		},
		{
			name:        "an empty key is a decision that can never be remembered",
			decision:    domain.ApprovalAllowForSession,
			keys:        []string{"", "", ""},
			wantPrompts: 3,
		},
		{
			name:        "a plain allow authorises its own call and nothing later",
			decision:    domain.ApprovalAllow,
			keys:        []string{"write:notes.md", "write:notes.md"},
			wantPrompts: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			inner := &seamApprover{decision: tc.decision}
			seam := queuedApprovals(inner)

			for i, key := range tc.keys {
				got, err := seam.Approve(context.Background(), domain.ApprovalRequest{Tool: "write_file", CacheKey: key})
				if err != nil {
					t.Fatalf("call %d returned err %v, want it answered", i, err)
				}
				if got != tc.decision {
					t.Errorf("call %d returned %q, want %q — a cached answer must match the one the human gave", i, got, tc.decision)
				}
			}

			if keys := inner.keysSeen(); len(keys) != tc.wantPrompts {
				t.Errorf("the human was asked %d times (keys %v), want %d", len(keys), keys, tc.wantPrompts)
			}
		})
	}
}

// TestApprovalSeam_TwinCoalescesWhileItWaits pins the twin: a duplicate request that queued behind
// the very prompt that allowed its key. Reading the memory before queueing cannot save it — the key
// was not allowed yet when it looked — so the seam re-checks on the far side of the wait and clears
// it without a second prompt.
func TestApprovalSeam_TwinCoalescesWhileItWaits(t *testing.T) {
	t.Parallel()

	inner := &seamApprover{
		decision: domain.ApprovalAllowForSession,
		entered:  make(chan struct{}, 1),
		release:  make(chan struct{}),
	}
	seam := queuedApprovals(inner)
	req := domain.ApprovalRequest{Tool: "write_file", CacheKey: "write:notes.md"}

	type answer struct {
		decision domain.ApprovalDecision
		err      error
	}
	first := make(chan answer, 1)
	go func() {
		d, err := seam.Approve(context.Background(), req)
		first <- answer{d, err}
	}()
	<-inner.entered // the visible prompt is open and holds the slot

	twin := make(chan answer, 1)
	go func() {
		d, err := seam.Approve(context.Background(), req)
		twin <- answer{d, err}
	}()
	time.Sleep(20 * time.Millisecond) // long enough for the twin to be genuinely queued behind it

	close(inner.release) // the human allows it for the session; the twin gets the slot next

	for _, got := range []struct {
		name string
		ch   chan answer
	}{{"the answered request", first}, {"the twin", twin}} {
		select {
		case a := <-got.ch:
			if a.err != nil {
				t.Errorf("%s returned err %v, want it answered", got.name, a.err)
			}
			if a.decision != domain.ApprovalAllowForSession {
				t.Errorf("%s returned %q, want %q", got.name, a.decision, domain.ApprovalAllowForSession)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s never returned", got.name)
		}
	}

	if keys := inner.keysSeen(); len(keys) != 1 {
		t.Errorf("the human was asked %d times (keys %v), want 1 — the twin re-prompted for an allow already given", len(keys), keys)
	}
}

// TestApprovalCache_NilReceiverAndEmptyKeyAreSafe pins the two degradations the cache promises. A
// nil cache is a rig that reached the seam unwrapped: it must prompt per call, not panic. An empty
// key is refused at BOTH ends, so the "cannot be remembered" rule holds however a caller gets here.
func TestApprovalCache_NilReceiverAndEmptyKeyAreSafe(t *testing.T) {
	t.Parallel()

	var absent *approvalCache
	absent.Allow("write:notes.md") // must not panic
	if absent.Allowed("write:notes.md") {
		t.Error("a nil cache reported a remembered allow; an unwrapped rig must degrade to prompting")
	}

	cache := &approvalCache{}
	cache.Allow("")
	if cache.Allowed("") {
		t.Error("an empty key was remembered; a forced gate's answer must never pre-clear anything")
	}

	cache.Allow("write:notes.md")
	if !cache.Allowed("write:notes.md") {
		t.Error("a remembered key read back as not allowed")
	}
	if cache.Allowed("mcp-server:github") {
		t.Error("an unrelated key read back as allowed")
	}
}

// ----------------------------------------------------------------------------
// The promise across a real agent tree (dispatch → seam)
// ----------------------------------------------------------------------------

// TestSessionApprovals_ChildInheritsTheParentsAllow is the reported bug in one run: the human
// allows a tool for the Session at the top level, the parent then delegates, and the child calls
// the very same tool. The child must NOT re-raise a prompt the human already answered — before the
// memory moved to the shared seam it did, because each Agent carried a private map that a child was
// deliberately not given.
func TestSessionApprovals_ChildInheritsTheParentsAllow(t *testing.T) {
	const (
		parentInput = "touch it, then delegate"
		childTask   = "touch it again"
	)

	sink := &recordingSink{}
	approver := &seamApprover{decision: domain.ApprovalAllowForSession}
	ran := 0
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "touch_thing", ran: &ran, result: "touched"})
	cfg.Approver = approver

	up := newRoutedResponder().
		route(parentInput, nil, toolCallScript("t0", "touch_thing", `{}`)). // the one prompt
		route(parentInput, nil, subAgentCallScript("c1", childTask)).
		route(childTask, nil, toolCallScript("t1", "touch_thing", `{}`)). // must ride the parent's allow
		route(childTask, nil, contentScript("child done")).
		route(parentInput, nil, contentScript("parent done"))

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: parentInput}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}

	if keys := approver.keysSeen(); len(keys) != 1 {
		t.Errorf("the human was asked %d times (%v), want once — the sub-agent re-prompted for an allow already granted", len(keys), keys)
	}
	if ran != 2 {
		t.Errorf("the gated tool ran %d times, want 2 (the parent's call and the child's)", ran)
	}
}

// TestSessionApprovals_AChildsAllowClearsTheParentAndALaterSibling drives the write-back direction,
// which is the half a per-Agent map could never do at all: the allow is granted INSIDE a child, and
// it must outlive that child — clearing the same key for a sibling delegated afterwards and for the
// parent itself. One prompt for the whole tree is the entire assertion.
func TestSessionApprovals_AChildsAllowClearsTheParentAndALaterSibling(t *testing.T) {
	const (
		parentInput = "delegate twice, then touch it myself"
		firstTask   = "touch it first"
		secondTask  = "touch it second"
	)

	sink := &recordingSink{}
	approver := &seamApprover{decision: domain.ApprovalAllowForSession}
	ran := 0
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "touch_thing", ran: &ran, result: "touched"})
	cfg.Approver = approver

	up := newRoutedResponder().
		route(parentInput, nil, subAgentCallScript("c1", firstTask)).
		route(firstTask, nil, toolCallScript("t1", "touch_thing", `{}`)). // the one prompt, raised in a child
		route(firstTask, nil, contentScript("first child done")).
		route(parentInput, nil, subAgentCallScript("c2", secondTask)).
		route(secondTask, nil, toolCallScript("t2", "touch_thing", `{}`)). // a later sibling: pre-cleared
		route(secondTask, nil, contentScript("second child done")).
		route(parentInput, nil, toolCallScript("t3", "touch_thing", `{}`)). // the parent: pre-cleared too
		route(parentInput, nil, contentScript("parent done"))

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: parentInput}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}

	if keys := approver.keysSeen(); len(keys) != 1 {
		t.Errorf("the human was asked %d times (%v), want once — a child's allow must survive it and reach its parent and siblings", len(keys), keys)
	}
	if ran != 3 {
		t.Errorf("the gated tool ran %d times, want 3 (both children and the parent)", ran)
	}
}
