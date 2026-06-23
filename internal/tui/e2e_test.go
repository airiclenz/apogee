package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// The hermetic end-to-end proof (phase-2 detail plan §4 P2.6)
// ----------------------------------------------------------------------------
//
// These tests drive the *real* Agent — the real provider client over a scripted
// OpenAI-compatible httptest model, the real default tools, the real concurrency seam
// (teaSink + uiApprover + the worker), and the real Model.Update folding the event stream —
// with no terminal in the loop. They are the deliverable proof the broad plan asks for: hold
// a coding conversation, watch tokens stream, watch a tool call, approve the write, see the
// result and the final message, then snapshot, resume, and continue.
//
// The package imports internal/agent here (test-only) to build a real engine; production
// internal/tui still depends only on internal/domain and the narrow Engine interface, so the
// ADR-0010 invariant (no internal/ import of the root module path) is untouched — none of
// these imports is the bare root path.

// The scripted model's canonical strings, shared by the server and the assertions.
const (
	narrationText       = "I'll create greeting.txt for you.\n"
	writeFileArgs       = `{"path":"greeting.txt","content":"Hello, Apogee!\n"}`
	finalMessageText    = "Done — greeting.txt is written."
	followUpMessageText = "Glad to help. Anything else?"
	greetingFileName    = "greeting.txt"
	greetingFileBody    = "Hello, Apogee!\n"
)

// ----------------------------------------------------------------------------
// A scripted OpenAI-compatible streaming model
// ----------------------------------------------------------------------------

// scriptedModel returns an httptest server speaking the SSE wire the provider dials
// (provider/stream.go). It is stateless across requests and decides each reply from the
// request's own message history, exactly as a real model does: a fresh task narrates and
// requests write_file; a request whose history ends in a tool result answers with the final
// message that closes the Exchange; a later user turn (the conversation already wrote the
// file) gets a plain closing reply with no tool. Those three branches drive, in order, a
// tool Turn, a final Turn, and — after resume — the continuation Turn.
func scriptedModel() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roles := requestRoles(r)
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case lastRoleIs(roles, string(domain.RoleTool)):
			// The previous Turn ran the tool; commit the final assistant message.
			writeFinal(w, finalMessageText)
		case countRole(roles, string(domain.RoleUser)) >= 2:
			// A second user turn after the file task — a plain closing reply, no tool.
			writeFinal(w, followUpMessageText)
		default:
			// A fresh task: narrate, then ask to write the file.
			writeToolCall(w, narrationText, "call_1", "write_file", writeFileArgs)
		}
	}))
}

// requestRoles decodes the role of each message in the request body. Only the roles are
// needed to choose a reply, so the rest of the OpenAI request shape is ignored.
func requestRoles(r *http.Request) []string {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	roles := make([]string, len(req.Messages))
	for i, m := range req.Messages {
		roles[i] = m.Role
	}
	return roles
}

// lastRoleIs reports whether the final message in the request carries role want.
func lastRoleIs(roles []string, want string) bool {
	return len(roles) > 0 && roles[len(roles)-1] == want
}

// countRole counts messages with the given role.
func countRole(roles []string, want string) int {
	n := 0
	for _, r := range roles {
		if r == want {
			n++
		}
	}
	return n
}

// writeToolCall streams a narration chunk, then one native tool call, then a tool_calls
// finish and the SSE terminator — the wire shape of a Turn that asks for a tool.
func writeToolCall(w http.ResponseWriter, narration, id, name, args string) {
	sseData(w, sseChunkBody{Choices: []sseChoiceBody{{Delta: sseDeltaBody{Content: narration}}}})
	sseData(w, sseChunkBody{Choices: []sseChoiceBody{{Delta: sseDeltaBody{ToolCalls: []sseToolCallBody{{
		ID: id, Type: "function", Function: sseFuncBody{Name: name, Arguments: args},
	}}}}}})
	sseData(w, sseChunkBody{Choices: []sseChoiceBody{{FinishReason: "tool_calls"}}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// writeFinal streams one content chunk then a stop finish and the terminator — the wire
// shape of a final no-tool Turn that ends the Exchange.
func writeFinal(w http.ResponseWriter, text string) {
	sseData(w, sseChunkBody{Choices: []sseChoiceBody{{Delta: sseDeltaBody{Content: text}}}})
	sseData(w, sseChunkBody{Choices: []sseChoiceBody{{FinishReason: "stop"}}})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// sseData writes v as one SSE data event. Writes are best-effort (the handler runs on the
// server goroutine, where a test-failure call is not permitted); the fixed chunk structs
// never fail to marshal.
func sseData(w io.Writer, v any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

// The on-the-wire SSE chunk shape the provider parses (a subset of provider/stream.go's
// sseChunk — only the fields this scripted model sets).
type sseChunkBody struct {
	Choices []sseChoiceBody `json:"choices"`
}

type sseChoiceBody struct {
	Delta        sseDeltaBody `json:"delta"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

type sseDeltaBody struct {
	Content   string            `json:"content,omitempty"`
	ToolCalls []sseToolCallBody `json:"tool_calls,omitempty"`
}

type sseToolCallBody struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function sseFuncBody `json:"function"`
}

type sseFuncBody struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ----------------------------------------------------------------------------
// The live-Model driver (stands in for the running Bubble Tea program)
// ----------------------------------------------------------------------------

// uiHarness is the programSender the Bridge binds in place of a real *tea.Program: every Msg
// the seam Sends (events from the teaSink, the approval request from the uiApprover, the
// worker's terminal Msg) lands in its inbox, and runExchange drains them into a real Model
// through the real Update. The seam Sends from the single worker goroutine; runExchange reads
// on the test goroutine — so only one goroutine ever touches the Model and the run is
// race-clean without a lock. The buffer is sized well past any scripted Turn's event count so
// a Send never blocks the worker.
type uiHarness struct {
	inbox     chan tea.Msg
	approvals int // how many approval prompts the run resolved
}

// uiHarness satisfies the program seam.
var _ programSender = (*uiHarness)(nil)

func newUIHarness() *uiHarness { return &uiHarness{inbox: make(chan tea.Msg, 256)} }

// Send enqueues a Msg from the worker goroutine; runExchange drains it.
func (h *uiHarness) Send(msg tea.Msg) { h.inbox <- msg }

// runExchange launches the worker for one user message over the live Model, drives every Msg
// the seam Sends through the real Update — auto-approving any prompt as a human pressing "a"
// would — and returns the settled Model and the worker's terminal Msg once the Exchange ends.
//
// It mirrors submit()'s effect (record the user turn, switch to running, hold the CancelFunc)
// but launches the worker directly rather than through the input-key path: that path is unit-
// tested (model_test.go) and returns a Batch that also schedules the cosmetic spinner tick, so
// driving it here would pull a timer into the loop. Launching the worker explicitly keeps the
// drive loop to exactly the real event/approval folds with nothing timer-driven.
func (h *uiHarness) runExchange(t *testing.T, ctx context.Context, m Model, eng Engine, text string) (Model, tea.Msg) {
	t.Helper()

	m.transcript.addUser(text)
	cmd, cancel := startExchange(ctx, eng, domain.UserInput{Text: text})
	defer cancel()
	m.cancel = cancel
	m.state = stateRunning
	m.refreshViewport()

	go func() { h.Send(cmd()) }() // run the worker; its terminal Msg arrives like any other

	for msg := range h.inbox {
		m = step(t, m, msg)
		switch msg.(type) {
		case approvalReqMsg:
			if m.state != stateAwaitingApproval {
				t.Fatalf("after approvalReqMsg state = %v, want awaitingApproval", m.state)
			}
			h.approvals++
			// Model the human pressing "a" (allow): the keypress sends the decision back over
			// the rendezvous reply channel and unblocks the worker's Approve.
			m = step(t, m, tea.KeyPressMsg{Code: 'a'})
		case exchangeDoneMsg, cancelledMsg, errMsg:
			return m, msg
		}
	}
	return m, nil
}

// terminalResult extracts the StepResult a terminal worker Msg carries (zero value for an
// errMsg, which carries an error rather than a result).
func terminalResult(msg tea.Msg) domain.StepResult {
	switch m := msg.(type) {
	case exchangeDoneMsg:
		return m.Result
	case cancelledMsg:
		return m.Result
	default:
		return domain.StepResult{}
	}
}

// newE2EEngine builds a real ask-before Agent bound to endpoint/model, the default tool set
// rooted at workspace, and the seam's sink/approver. The Agent is the concrete type the
// narrow Engine interface abstracts — this is the binding cmd/apogee makes in production,
// exercised here through the real provider client.
func newE2EEngine(t *testing.T, endpoint, model, workspace string, sink domain.EventSink, approver domain.Approver) *agent.Agent {
	t.Helper()
	eng, err := agent.New(domain.Config{
		Endpoint:     endpoint,
		Model:        model,
		Mode:         domain.ModeAskBefore,
		Events:       sink,
		Approver:     approver,
		Tools:        tools.NewDefaultRegistry(workspace),
		WorkspaceDir: workspace,
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

// e2eOptions are the display values the status line renders for the e2e Model.
func e2eOptions(endpoint, workspace string) Options {
	return Options{
		Model:     "test-model",
		Endpoint:  endpoint,
		Mode:      domain.ModeAskBefore,
		Workspace: workspace,
	}
}

// plainTranscript renders the model's transcript with styling stripped, for content asserts.
func plainTranscript(m Model) string {
	return ansiPattern.ReplaceAllString(m.transcript.render(), "")
}

// ----------------------------------------------------------------------------
// P2.6 part 1 — the conversation works end to end through the TUI
// ----------------------------------------------------------------------------

// TestE2EConversationThroughTUI is the deliverable proof: a real Agent, driven through the
// real seam against the scripted model, streams narration, requests write_file, has the write
// approved through the UI, runs the tool into a temp workspace, and renders tokens → tool call
// → result → final message — all with no terminal.
func TestE2EConversationThroughTUI(t *testing.T) {
	t.Parallel()
	srv := scriptedModel()
	defer srv.Close()

	workspace := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bridge := NewBridge()
	h := newUIHarness()
	bridge.Bind(h)
	eng := newE2EEngine(t, srv.URL, "test-model", workspace, bridge.Sink(), bridge.Approver())

	m := step(t, newModel(ctx, eng, e2eOptions(srv.URL, workspace)), tea.WindowSizeMsg{Width: 100, Height: 30})

	m, term := h.runExchange(t, ctx, m, eng, "create a greeting file")

	// The Exchange ran to its natural end and the model is idle again.
	done, ok := term.(exchangeDoneMsg)
	if !ok {
		t.Fatalf("terminal Msg = %T, want exchangeDoneMsg", term)
	}
	if done.Result.Status != domain.StatusExchangeComplete {
		t.Errorf("terminal status = %q, want %q", done.Result.Status, domain.StatusExchangeComplete)
	}
	if m.state != stateIdle {
		t.Errorf("final state = %v, want idle", m.state)
	}

	// The real human gate fired exactly once, for the one write_file call.
	if h.approvals != 1 {
		t.Errorf("approval prompts resolved = %d, want 1", h.approvals)
	}

	// The approved write reached the real tool and landed in the workspace.
	got, err := os.ReadFile(filepath.Join(workspace, greetingFileName))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != greetingFileBody {
		t.Errorf("written file body = %q, want %q", got, greetingFileBody)
	}

	// The transcript folded the whole real event stream: the narration, the tool call (name
	// and arguments), and the final message.
	transcript := plainTranscript(m)
	for _, want := range []string{"I'll create greeting.txt", "write_file", greetingFileName, finalMessageText} {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript missing %q:\n%s", want, transcript)
		}
	}
}

// ----------------------------------------------------------------------------
// P2.6 part 1 (continued) — snapshot on quit, resume, continue at the right Turn
// ----------------------------------------------------------------------------

// TestE2ESnapshotResumeContinues runs the file-edit Exchange, snapshots it on a clean quit
// through the real saver seam, resumes the Agent from the written file, and continues the
// conversation — proving the resumed Exchange picks up at the Turn the snapshot left off at
// (turn 2, after exchange 1's turns 0 and 1) rather than restarting at zero. This closes the
// P2.5 save↔resume acceptance end to end through the product surface.
func TestE2ESnapshotResumeContinues(t *testing.T) {
	t.Parallel()
	srv := scriptedModel()
	defer srv.Close()

	workspace := t.TempDir()
	sessionsDir := filepath.Join(t.TempDir(), "sessions")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Exchange 1: run the file-edit conversation, then snapshot on a clean quit ---
	bridge1 := NewBridge()
	h1 := newUIHarness()
	bridge1.Bind(h1)
	eng1 := newE2EEngine(t, srv.URL, "test-model", workspace, bridge1.Sink(), bridge1.Approver())

	store := session.NewStore(sessionsDir)
	var savedPath string
	save := func(s domain.Session) error {
		path, err := store.Save(s)
		savedPath = path
		return err
	}
	opts1 := e2eOptions(srv.URL, workspace)
	opts1.Save = save
	m1 := step(t, newModel(ctx, eng1, opts1), tea.WindowSizeMsg{Width: 100, Height: 30})

	m1, term1 := h1.runExchange(t, ctx, m1, eng1, "create a greeting file")
	if r := terminalResult(term1); r.Status != domain.StatusExchangeComplete {
		t.Fatalf("exchange 1 status = %q, want %q", r.Status, domain.StatusExchangeComplete)
	}

	// A clean quit (idle, non-empty transcript) snapshots through the saver seam and exits.
	_, quitCmd := stepCmd(t, m1, keyEsc())
	if _, isQuit := cmdMsg(quitCmd).(tea.QuitMsg); !isQuit {
		t.Fatal("esc at idle did not quit")
	}
	if savedPath == "" {
		t.Fatal("a clean quit wrote no snapshot")
	}

	// --- Resume from the written snapshot and continue the conversation ---
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	snap, err := domain.DecodeSession(data)
	if err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}

	bridge2 := NewBridge()
	h2 := newUIHarness()
	bridge2.Bind(h2)
	eng2, err := agent.Resume(domain.Config{
		Endpoint:     srv.URL,
		Model:        "test-model",
		Mode:         domain.ModeAskBefore,
		Events:       bridge2.Sink(),
		Approver:     bridge2.Approver(),
		Tools:        tools.NewDefaultRegistry(workspace),
		WorkspaceDir: workspace,
	}, snap)
	if err != nil {
		t.Fatalf("agent.Resume: %v", err)
	}
	t.Cleanup(func() { _ = eng2.Close() })

	m2 := step(t, newModel(ctx, eng2, e2eOptions(srv.URL, workspace)), tea.WindowSizeMsg{Width: 100, Height: 30})
	m2, term2 := h2.runExchange(t, ctx, m2, eng2, "thanks!")

	// The resumed Exchange continues at turn 2 — the turnIndex the snapshot carried — not at
	// zero: a reset would surface here as turn 0.
	r2 := terminalResult(term2)
	if r2.Status != domain.StatusExchangeComplete {
		t.Errorf("resumed exchange status = %q, want %q", r2.Status, domain.StatusExchangeComplete)
	}
	if r2.TurnIndex != 2 {
		t.Errorf("resumed turn index = %d, want 2 (continues after exchange 1's turns 0 and 1)", r2.TurnIndex)
	}
	if m2.transcript.turn != 2 {
		t.Errorf("status-line turn = %d, want 2 (continued from the snapshot)", m2.transcript.turn)
	}
	// The continuation reply rendered, and no second approval was needed (no tool this Turn).
	if got := plainTranscript(m2); !strings.Contains(got, followUpMessageText) {
		t.Errorf("resumed transcript missing the continuation reply %q:\n%s", followUpMessageText, got)
	}
	if h2.approvals != 0 {
		t.Errorf("resumed run resolved %d approvals, want 0 (no tool this Turn)", h2.approvals)
	}
}
