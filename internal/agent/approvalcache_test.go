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
// same one: was the human asked again for something they had already allowed? The seam is exercised
// directly rather than through a whole Agent — nothing populates ApprovalRequest.CacheKey on the
// dispatch path yet, and the rules the seam enforces are the ones that must hold whoever populates
// it.

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
