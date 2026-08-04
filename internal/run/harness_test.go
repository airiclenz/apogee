package run

// The hermetic Upstream every internal/run test drives. internal/run reaches the engine
// through the PUBLIC agent.New, which binds the real provider client, so there is no fake
// responder seam to inject here — the fake has to be a real OpenAI-compatible server. This
// is the httptest SSE shape internal/agent, internal/tui and cmd/apogee already script
// their Upstreams with (internal/tui/e2e_test.go), narrowed to what a Firing exercises.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// upstream is a scripted OpenAI-compatible server that records every request it answered,
// so a test can assert what actually reached the wire — the message history (the fresh-
// context proof) and the offered tool menu (the unregistered-delegate proof).
type upstream struct {
	url string

	mu   sync.Mutex
	reqs []request
}

// request is the decoded subset of one chat-completions body the tests assert on.
type request struct {
	Roles []string // the role of each message, in order
	Texts []string // the content of each message, in order (a null content reads as "")
	Tools []string // the names of the tools offered on the menu
}

// userMsgs counts the user-role messages the request carried.
func (r request) userMsgs() int {
	n := 0
	for _, role := range r.Roles {
		if role == string(domain.RoleUser) {
			n++
		}
	}
	return n
}

// lastRoleIs reports whether the request's final message carries role want.
func (r request) lastRoleIs(want domain.Role) bool {
	return len(r.Roles) > 0 && r.Roles[len(r.Roles)-1] == string(want)
}

// lastTextHas reports whether the request's final message contains want. It is how a script
// tells a SUB-AGENT's fresh conversation from its parent's: the two look identical by role
// (one user message), but the sub-agent's carries the delegated task.
func (r request) lastTextHas(want string) bool {
	return len(r.Texts) > 0 && strings.Contains(r.Texts[len(r.Texts)-1], want)
}

// offers reports whether name is on the request's tool menu.
func (r request) offers(name string) bool {
	for _, t := range r.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// newUpstream starts a server that answers each request through reply, which receives the
// decoded request so it can branch on the conversation exactly as a real model would.
func newUpstream(t *testing.T, reply func(w http.ResponseWriter, req request)) *upstream {
	t.Helper()
	up := &upstream{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRequest(r)
		up.mu.Lock()
		up.reqs = append(up.reqs, req)
		up.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		reply(w, req)
	}))
	t.Cleanup(srv.Close)
	up.url = srv.URL
	return up
}

// alwaysFinal is the simplest script: every request gets one final, no-tool reply.
func alwaysFinal(text string) func(http.ResponseWriter, request) {
	return func(w http.ResponseWriter, _ request) { writeFinal(w, text) }
}

// requests returns what the server saw, under its lock.
func (u *upstream) requests() []request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]request(nil), u.reqs...)
}

// calls reports how many requests reached the server.
func (u *upstream) calls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.reqs)
}

// decodeRequest pulls the roles and the tool menu off one chat-completions body. Decoding
// is best-effort: the handler runs on the server goroutine, where a test-failure call is
// not permitted, and a body this fake cannot read simply records as empty.
func decodeRequest(r *http.Request) request {
	body, _ := io.ReadAll(r.Body)
	var decoded struct {
		Messages []struct {
			Role string `json:"role"`
			// A pointer, exactly as the provider marshals it: a tool-call-only assistant
			// message serialises its content as JSON null.
			Content *string `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(body, &decoded)

	req := request{
		Roles: make([]string, len(decoded.Messages)),
		Texts: make([]string, len(decoded.Messages)),
		Tools: make([]string, len(decoded.Tools)),
	}
	for i, m := range decoded.Messages {
		req.Roles[i] = m.Role
		if m.Content != nil {
			req.Texts[i] = *m.Content
		}
	}
	for i, tl := range decoded.Tools {
		req.Tools[i] = tl.Function.Name
	}
	return req
}

// writeFinal streams one content chunk, a stop finish and the terminator — the wire shape
// of a final no-tool Turn that ends the Exchange.
func writeFinal(w http.ResponseWriter, text string) {
	sseData(w, sseChunk{Choices: []sseChoice{{Delta: sseDelta{Content: text}}}})
	sseData(w, sseChunk{Choices: []sseChoice{{FinishReason: "stop"}}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// writeToolCall streams one native tool call, a tool_calls finish and the terminator — the
// wire shape of a Turn that asks for a tool.
func writeToolCall(w http.ResponseWriter, id, name, args string) {
	sseData(w, sseChunk{Choices: []sseChoice{{Delta: sseDelta{ToolCalls: []sseToolCall{{
		ID: id, Type: "function", Function: sseFunc{Name: name, Arguments: args},
	}}}}}})
	sseData(w, sseChunk{Choices: []sseChoice{{FinishReason: "tool_calls"}}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// writeToolCallWithText streams one Turn that both SAYS something and asks for a tool — the
// shape a model takes when it narrates before it calls. The prose is committed on the
// assistant message but ends no Exchange, so this Turn is not the run's final answer.
func writeToolCallWithText(w http.ResponseWriter, text, id, name, args string) {
	sseData(w, sseChunk{Choices: []sseChoice{{Delta: sseDelta{Content: text}}}})
	writeToolCall(w, id, name, args)
}

// sseData writes v as one SSE data event. Writes are best-effort for the same
// server-goroutine reason decodeRequest is; the fixed chunk structs never fail to marshal.
func sseData(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

// The on-the-wire SSE chunk shape the provider parses — only the fields these fakes set.
type sseChunk struct {
	Choices []sseChoice `json:"choices"`
}

type sseChoice struct {
	Delta        sseDelta `json:"delta"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

type sseDelta struct {
	Content   string        `json:"content,omitempty"`
	ToolCalls []sseToolCall `json:"tool_calls,omitempty"`
}

type sseToolCall struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Function sseFunc `json:"function"`
}

type sseFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ---------------------------------------------------------------------------

// planSpec is the baseline Firing: read-only Plan against the fake Upstream. Events is
// deliberately left nil so every test also exercises the discard path Once installs.
func planSpec(endpoint, prompt string) Spec {
	return Spec{
		Config: domain.Config{Endpoint: endpoint, Model: "test-model", Mode: domain.ModePlan},
		Prompt: prompt,
	}
}

// at pins a clock at ts.
func at(ts time.Time) func() time.Time {
	return func() time.Time { return ts }
}

// ---------------------------------------------------------------------------

// recordingSink is a caller-supplied EventSink that keeps every assistant message the run
// emitted, with its nesting depth. It exists so a test can prove a sub-agent's message
// really REACHED the tap before asserting the Result ignored it — without that, a script
// whose sub-agent silently never ran would pass vacuously.
type recordingSink struct {
	mu   sync.Mutex
	msgs []domain.MessageEvent
}

// Emit keeps the assistant messages and discards everything else.
func (s *recordingSink) Emit(e domain.Event) {
	m, ok := e.(domain.MessageEvent)
	if !ok {
		return
	}
	s.mu.Lock()
	s.msgs = append(s.msgs, m)
	s.mu.Unlock()
}

// saw reports whether a message with exactly this text arrived at this depth.
func (s *recordingSink) saw(depth int, text string) bool {
	for _, m := range s.messages() {
		if m.Depth == depth && m.Text == text {
			return true
		}
	}
	return false
}

// messages returns what was observed, under the lock.
func (s *recordingSink) messages() []domain.MessageEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.MessageEvent(nil), s.msgs...)
}

// ---------------------------------------------------------------------------

// gatingTool is write-capable and declares nothing, so the ladder classifies it as a
// third-party writer — the class Auto gates rather than runs (resolution.go). It records
// whether it ever executed, which is the assertion that a denied gate really blocked it.
type gatingTool struct {
	mu       sync.Mutex
	executed bool
}

func (g *gatingTool) Name() string            { return "risky_write" }
func (g *gatingTool) Description() string     { return "writes something the host cannot vouch for" }
func (g *gatingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (g *gatingTool) Execute(context.Context, domain.ToolCall) (domain.ToolResult, error) {
	g.mu.Lock()
	g.executed = true
	g.mu.Unlock()
	return domain.ToolResult{Content: "wrote it"}, nil
}

// ran reports whether Execute was ever reached.
func (g *gatingTool) ran() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.executed
}

// stubConfiner satisfies the Confiner injection Auto construction requires. It reports full
// capability and confines nothing — no test here launches a subprocess.
type stubConfiner struct{}

func (stubConfiner) Capabilities() domain.ConfinementCaps {
	return domain.ConfinementCaps{FSWrite: true, NetworkEgress: true}
}

func (stubConfiner) Confine(context.Context, domain.ConfinementBox, *exec.Cmd) error { return nil }

// stubAsker and stubPresenter are the human-facing delegates a caller might leave on the
// Config. Once must unregister both; reaching either is itself the failure.
type stubAsker struct{}

func (stubAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	return domain.AskAnswer{}, nil
}

type stubPresenter struct{}

func (stubPresenter) Present(context.Context, domain.PresentRequest) (domain.PresentOutcome, error) {
	return domain.PresentOutcome{}, nil
}
