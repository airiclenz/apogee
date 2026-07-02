package agent

// Loop-level tests for the model-profile EMIT seam (prompt-seam item 2): a non-native profile
// injects the text tool menu + format instructions as a wire-only system message and suppresses
// the native tools array (D2/D3/D4); a native/zero profile is byte-identical; and the injected
// text never enters domain history or the snapshot. They drive the recording fake through the
// unexported newAgent seam and assert on the captured provider.Request (see harness_test.go).

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
)

// readOnlyTool is a minimal read-only fake Tool used to build a deterministic wire tool menu for
// the emit-seam tests — read-only so it survives Plan-mode filtering (the write tool does not).
type readOnlyTool struct {
	name   string
	desc   string
	schema string
}

func (t readOnlyTool) Name() string            { return t.name }
func (t readOnlyTool) Description() string     { return t.desc }
func (t readOnlyTool) Schema() json.RawMessage { return json.RawMessage(t.schema) }
func (t readOnlyTool) ReadOnly() bool          { return true }
func (t readOnlyTool) Execute(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	return domain.ToolResult{CallID: call.ID, Content: "ok"}, nil
}

// menuConfig returns a baseConfig wired with one read-only tool (read_file) so the tool menu is
// non-empty and deterministic — the input the emit seam renders into instructions.
func menuConfig(t *testing.T, sink domain.EventSink) domain.Config {
	t.Helper()
	reg := domain.NewToolRegistry()
	if err := reg.Register(readOnlyTool{
		name:   "read_file",
		desc:   "Read a file from the workspace",
		schema: `{"type":"object","properties":{"path":{"type":"string"}}}`,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	cfg := baseConfig(sink)
	cfg.Tools = reg
	return cfg
}

func firstSystemMessage(msgs []domain.Message) (domain.Message, bool) {
	for _, m := range msgs {
		if m.Role == domain.RoleSystem {
			return m, true
		}
	}
	return domain.Message{}, false
}

// TestPromptSeam_NonNativeInjectsMenuAndSuppressesTools: a markdown-fenced profile suppresses the
// native tools array and prepends a system message carrying the vector-exact menu + instructions.
func TestPromptSeam_NonNativeInjectsMenuAndSuppressesTools(t *testing.T) {
	sink := &recordingSink{}
	cfg := menuConfig(t, sink)
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := responder.last
	if got.Tools != nil {
		t.Errorf("Tools = %+v, want nil (native array suppressed for a non-native format)", got.Tools)
	}
	if len(got.Messages) == 0 || got.Messages[0].Role != string(domain.RoleSystem) {
		t.Fatalf("first wire message = %+v, want a system message at position 0", got.Messages)
	}

	// Vector-exact: the injected block is precisely what the renderer produces over this request's
	// menu — proving the loop wired InstructionsFor over the mode-filtered menu, not a stale copy.
	want, err := processing.InstructionsFor(cfg.Profile, a.toolMenu())
	if err != nil {
		t.Fatalf("InstructionsFor: %v", err)
	}
	if got.Messages[0].Content != want {
		t.Errorf("system message = %q\nwant %q", got.Messages[0].Content, want)
	}
	// Concrete anchors so a silent renderer regression is caught here too.
	for _, sub := range []string{"## Available Tools", "**read_file**", "## Tool Call Format", "```tool", "TOOL_NAME", "BEGIN_ARG"} {
		if !strings.Contains(got.Messages[0].Content, sub) {
			t.Errorf("injected block missing %q:\n%s", sub, got.Messages[0].Content)
		}
	}
}

// TestPromptSeam_NativeProfileByteIdentical: a zero profile injects nothing and keeps the native
// tools array — the wire request is byte-identical to the pre-change loop (no system message).
func TestPromptSeam_NativeProfileByteIdentical(t *testing.T) {
	sink := &recordingSink{}
	cfg := menuConfig(t, sink) // zero Profile

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := responder.last
	if len(got.Tools) != 1 || got.Tools[0].Name != "read_file" {
		t.Errorf("Tools = %+v, want the native read_file spec (no suppression for a native profile)", got.Tools)
	}
	if _, ok := firstSystemMessage(wireToDomain(got.Messages)); ok {
		t.Error("a system message was injected for a native profile; the wire request must be byte-identical")
	}
	if len(got.Messages) == 0 || got.Messages[0].Role != string(domain.RoleUser) {
		t.Fatalf("first wire message = %+v, want the user message unchanged at position 0", got.Messages)
	}
}

// TestPromptSeam_AppendsToSeededSystemMessage: when the wire projection already carries a system
// message (an embedder seeds one via a pre-request hook), the block is appended to it — one merged
// system message, never a second (D3).
func TestPromptSeam_AppendsToSeededSystemMessage(t *testing.T) {
	sink := &recordingSink{}
	cfg := menuConfig(t, sink)
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}
	cfg.Mechanisms = domain.NewMechanismRegistry()
	const seed = "You are a helpful assistant. [seed]"
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, seedingHook{text: seed}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := responder.last
	systems := 0
	for _, m := range got.Messages {
		if m.Role == string(domain.RoleSystem) {
			systems++
		}
	}
	if systems != 1 {
		t.Fatalf("wire request has %d system messages, want exactly 1 (append, not a second)", systems)
	}
	block, err := processing.InstructionsFor(cfg.Profile, a.toolMenu())
	if err != nil {
		t.Fatalf("InstructionsFor: %v", err)
	}
	want := seed + "\n\n" + block
	if got.Messages[0].Role != string(domain.RoleSystem) || got.Messages[0].Content != want {
		t.Errorf("merged system message = %q\nwant %q", got.Messages[0].Content, want)
	}
}

// TestPromptSeam_NextTurnReflectsMenuChange: switching to Plan mode between Turns re-renders the
// injected block over the filtered menu — the write tool drops out on the next request, proving
// the block tracks the per-request mode-filtered menu turn-by-turn (D2), not a one-time snapshot.
func TestPromptSeam_NextTurnReflectsMenuChange(t *testing.T) {
	sink := &recordingSink{}
	cfg := menuConfig(t, sink)
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}
	if err := cfg.Tools.Register(thirdPartyWriter{name: "write_file"}); err != nil {
		t.Fatalf("Register write tool: %v", err)
	}
	cfg.Mode = domain.ModeAskBefore

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	// Turn 1 (Ask-Before): the full menu, so write_file is offered in the text block.
	if err := a.Submit(domain.UserInput{Text: "one"}); err != nil {
		t.Fatalf("Submit 1: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	if sys, ok := firstSystemMessage(wireToDomain(responder.last.Messages)); !ok || !strings.Contains(sys.Content, "write_file") {
		t.Fatalf("Turn 1 block should offer write_file: %q", sys.Content)
	}

	// Switch to Plan (read-only) and take a second Turn: write_file must drop out of the re-rendered
	// block while read_file stays.
	a.SetMode(domain.ModePlan)
	if err := a.Submit(domain.UserInput{Text: "two"}); err != nil {
		t.Fatalf("Submit 2: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step 2: %v", err)
	}
	sys, ok := firstSystemMessage(wireToDomain(responder.last.Messages))
	if !ok {
		t.Fatal("Turn 2 wire request has no system message")
	}
	if strings.Contains(sys.Content, "write_file") {
		t.Errorf("Turn 2 block still offers write_file after switching to Plan:\n%s", sys.Content)
	}
	if !strings.Contains(sys.Content, "read_file") {
		t.Errorf("Turn 2 block lost the read-only read_file:\n%s", sys.Content)
	}
}

// TestPromptSeam_InjectedTextNeverEntersHistoryOrSnapshot: the rendered block is wire-only — no
// committed message carries it, and it is absent from the encoded snapshot.
func TestPromptSeam_InjectedTextNeverEntersHistoryOrSnapshot(t *testing.T) {
	sink := &recordingSink{}
	cfg := menuConfig(t, sink)
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	const marker = "## Available Tools"
	for i, m := range a.conv.Messages() {
		if m.Role == domain.RoleSystem {
			t.Errorf("committed message %d is a system message; the injected block must stay off history", i)
		}
		if strings.Contains(m.Content, marker) {
			t.Errorf("committed message %d carries the injected menu text: %q", i, m.Content)
		}
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	raw, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if bytes.Contains(raw, []byte(marker)) {
		t.Error("the encoded snapshot carries the injected menu text; it must be wire-only")
	}
}

// seedingHook is a pre-request hook standing in for an embedder that seeds a system message onto
// the wire projection before the emit seam runs.
type seedingHook struct{ text string }

func (h seedingHook) PreRequest(_ context.Context, req *domain.Request) error {
	req.AppendToSystem("[seed]", h.text)
	return nil
}

// wireToDomain reduces provider messages to the role/content the emit-seam assertions read, so the
// system-message helpers can be shared across the domain-history and wire-projection checks.
func wireToDomain(msgs []provider.Message) []domain.Message {
	out := make([]domain.Message, len(msgs))
	for i, m := range msgs {
		out[i] = domain.Message{Role: domain.Role(m.Role), Content: m.Content}
	}
	return out
}
