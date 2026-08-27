package stubllm

import (
	"fmt"
	"testing"
	"time"
)

// Request is one request the stub received, as the test sees it afterwards. It is the stub's
// half of an assertion: what the agent actually asked, in order, and which Turn answered.
type Request struct {
	// N is the request's 1-based position in the run, counted whether or not the log is on.
	N int
	// Model is the model id the request named.
	Model string
	// Messages is the conversation the request carried, in wire order.
	Messages []Message
	// Tools are the names of the tools the request offered, in wire order.
	Tools []string
	// Stream reports whether the request asked for SSE.
	Stream bool
	// Unmatched reports that no Turn answered: the stub replied HTTP 500.
	Unmatched bool
	// TurnIndex is the index of the Turn that answered, or -1 when Unmatched.
	TurnIndex int
	// At is when the request arrived.
	At time.Time
}

// Message is one message off a request, reduced to what a test or a matcher reads.
type Message struct {
	Role       string
	Content    string
	ToolCallID string     // set on a tool-result message: the call it answers
	ToolCalls  []ToolCall // set on an assistant message that issued calls
}

// Requests returns a copy of the request log, oldest first. It is empty when the Server was
// built with WithRequestLog(false).
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Request, len(s.requests))
	copy(out, s.requests)
	return out
}

// LastMessage returns the text of the last message on request n (1-based), or "" when there is
// no such request. It is the shortest way to assert what the agent actually asked the model.
func (s *Server) LastMessage(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if n < 1 || n > len(s.requests) {
		return ""
	}
	return lastText(s.requests[n-1].Messages)
}

// Unmatched returns the logged requests no Turn answered. A non-empty result is the stub
// saying the run went somewhere the script did not anticipate.
func (s *Server) Unmatched() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Request
	for _, r := range s.requests {
		if r.Unmatched {
			out = append(out, r)
		}
	}
	return out
}

// AssertConsumed fails t when a non-repeat Turn was never served. A script whose later turns
// never played means the run stopped early, which is exactly the failure a test asserting only
// on the final frame would miss.
func (s *Server) AssertConsumed(t testing.TB) {
	t.Helper()

	s.mu.Lock()
	unserved := s.matcher.unserved()
	s.mu.Unlock()

	for _, i := range unserved {
		t.Errorf("stubllm: turn %d was never served — the run did not reach it", i)
	}
}

// lastText is the text of the last message in a request, or "" when there is none.
func lastText(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].Content
}

// lastToolResultName is the name of the tool whose result the request's last message carries,
// or "" when the last message is not a tool result. The wire shape carries only the call id,
// so the name is read off the assistant message that issued that call.
func lastToolResultName(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	last := messages[len(messages)-1]
	if last.Role != "tool" || last.ToolCallID == "" {
		return ""
	}
	for i := len(messages) - 2; i >= 0; i-- {
		for _, call := range messages[i].ToolCalls {
			if call.ID == last.ToolCallID {
				return call.Name
			}
		}
	}
	return ""
}

// String renders a request for a failure message: who asked what, and what answered.
func (r Request) String() string {
	answer := fmt.Sprintf("turn %d", r.TurnIndex)
	if r.Unmatched {
		answer = "UNMATCHED"
	}
	return fmt.Sprintf("request %d (%s): %q", r.N, answer, lastText(r.Messages))
}
