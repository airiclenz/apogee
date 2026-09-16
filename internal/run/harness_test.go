package run

// The hermetic Upstream every internal/run test drives. internal/run reaches the engine
// through the PUBLIC agent.New, which binds the real provider client, so there is no fake
// responder seam to inject here — the Upstream has to be a real OpenAI-compatible server.
// That server is stubllm: each test scripts a stubllm.Script, stubllm.New serves it on a
// loopback port, and the request log is what a test reads to assert what actually reached
// the wire — the message history (the fresh-context proof), the offered tool menu (the
// unregistered-delegate proof). A request the Script did not anticipate is a 500, which the
// run reports as an error: fix the Script, never loosen it.

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// finalScript is the simplest Script: one final, no-tool reply, and nothing for a second
// request. A test that fires twice against one server scripts two turns.
func finalScript(text string) stubllm.Script {
	return stubllm.Script{Turns: []stubllm.Turn{{Text: text}}}
}

// roleCount counts the messages of a request that carry role.
func roleCount(r stubllm.Request, role domain.Role) int {
	n := 0
	for _, m := range r.Messages {
		if m.Role == string(role) {
			n++
		}
	}
	return n
}

// answersATool reports whether a request's last message is a tool result — the shape a
// conversation has when the model is back with what its call returned.
func answersATool(r stubllm.Request) bool {
	return len(r.Messages) > 0 && r.Messages[len(r.Messages)-1].Role == string(domain.RoleTool)
}

// cancelWhen cancels a Firing the moment the stub logs a request that want selects. It is how
// a test stops a run at a precise point in its conversation: the Turn scripted for that request
// is a hang, so the stub writes nothing once the request context dies and the client sees only
// its own cancellation, never an assistant message. The log is polled rather than hooked
// because the log is the stub's own word that a request has landed.
func cancelWhen(t *testing.T, up *stubllm.Server, cancel context.CancelFunc, want func(stubllm.Request) bool) {
	t.Helper()
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			for _, r := range up.Requests() {
				if want(r) {
					cancel()
					return
				}
			}
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
}

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

// ---------------------------------------------------------------------------

// denyingGate is one user-origin `gate:` entry over argv, as a `reactions:` file resolves it,
// answering `deny` to every call it is asked about. It is the cheapest possible proof that the
// sync lane a Spec carried is armed on the Agent: a gate that never ran cannot refuse anything.
func denyingGate(id string) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   domain.ClassGate,
		On:      []domain.Moment{domain.MomentPreToolExec},
		Handler: domain.ArgvHandler{Argv: []string{"/bin/sh", "-c", "echo deny"}},
		Timeout: 5 * time.Second,
	}
}

// ---------------------------------------------------------------------------

// notingTool is a read-only tool with a trivial, quotable outcome. It exists so a scripted
// Firing can exercise the ordinary tool-call/tool-result pair in Plan mode — the class the
// ladder runs rather than gates — without a gate, a subprocess or a file on disk.
type notingTool struct{}

func (notingTool) Name() string            { return "note_something" }
func (notingTool) Description() string     { return "records a note and reads it straight back" }
func (notingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (notingTool) ReadOnly() bool          { return true }

func (notingTool) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	var decoded struct {
		Note string `json:"note"`
	}
	_ = json.Unmarshal(call.Arguments, &decoded)
	return domain.ToolResult{CallID: call.ID, Content: "noted: " + decoded.Note}, nil
}
