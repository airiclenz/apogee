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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/prompt"
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

// ----------------------------------------------------------------------------
// The configured system prompt (ADR 0023) — Config.SystemPrompt seeds position 0
// ----------------------------------------------------------------------------
//
// The template is seeded into the REQUEST projection by buildRequest, so the tests below drive
// the same recording fake as the emit-seam tests above and assert on the captured
// provider.Request: exactly ONE system message, rendered from live inputs, merged with the
// mechanism directives and the profile tool block, and absent from history and the snapshot.

// promptTemplate exercises all three placeholders with {{workspace}} REPEATED, so a render
// assertion covers every substitution and the every-occurrence rule in one template.
const promptTemplate = "You are apogee working in {{workspace}} on {{datetime}} in {{mode}} mode. Stay inside {{workspace}}."

// promptWorkspace is the workspace path the template renders; menuConfig injects the tool
// registry explicitly, so setting it changes nothing but the rendered prompt.
const promptWorkspace = "/tmp/apogee-prompt-ws"

// promptNow is the fixed render clock the date assertions read (late in the day, so a
// date-only render is visibly not a timestamp).
var promptNow = time.Date(2026, 7, 26, 23, 59, 30, 0, time.UTC)

// countSystemMessages counts the wire request's system messages — the "exactly one merged
// system message" property every seeding assertion below rests on.
func countSystemMessages(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == string(domain.RoleSystem) {
			n++
		}
	}
	return n
}

// TestPromptSeam_ConfiguredPromptNativeSingleSystemMessage: with a configured template and a
// native (zero) profile the wire request opens with EXACTLY one system message carrying the
// rendered prompt, and the native tools array is untouched — the prompt does not trip the
// non-native suppression.
func TestPromptSeam_ConfiguredPromptNativeSingleSystemMessage(t *testing.T) {
	sink := &recordingSink{}
	cfg := menuConfig(t, sink) // zero Profile: native tool calls
	cfg.Mode = domain.ModeAskBefore
	cfg.WorkspaceDir = promptWorkspace
	cfg.SystemPrompt = promptTemplate

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	a.now = func() time.Time { return promptNow }
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := responder.last
	if n := countSystemMessages(got.Messages); n != 1 {
		t.Fatalf("wire request has %d system messages, want exactly 1 (the seeded prompt)", n)
	}
	want := prompt.Render(promptTemplate, prompt.Inputs{
		Workspace: promptWorkspace,
		Mode:      string(domain.ModeAskBefore),
		Now:       promptNow,
	})
	if got.Messages[0].Role != string(domain.RoleSystem) || got.Messages[0].Content != want {
		t.Errorf("first wire message = %+v\nwant a system message %q", got.Messages[0], want)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "read_file" {
		t.Errorf("Tools = %+v, want the native read_file spec (a prompt must not suppress the native array)", got.Tools)
	}
}

// TestPromptSeam_ConfiguredPromptMergesDirectivesAndToolBlock: prompt + a mechanism directive
// (AppendToSystem) + a non-native profile's tool block all land in ONE system message, in the
// required order prompt → directives → tool block.
func TestPromptSeam_ConfiguredPromptMergesDirectivesAndToolBlock(t *testing.T) {
	sink := &recordingSink{}
	cfg := menuConfig(t, sink)
	cfg.Mode = domain.ModeAskBefore
	cfg.WorkspaceDir = promptWorkspace
	cfg.SystemPrompt = promptTemplate
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}
	cfg.Mechanisms = domain.NewMechanismRegistry()
	// seedingHook stands in for a Mechanism's directive: it appends through the same
	// Request.AppendToSystem seam the catalogued nudges use.
	const directive = "Always cite the files you read. [seed]"
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, seedingHook{text: directive}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	a.now = func() time.Time { return promptNow }
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := responder.last
	if n := countSystemMessages(got.Messages); n != 1 {
		t.Fatalf("wire request has %d system messages, want exactly 1 (prompt, directive and tool block merged)", n)
	}
	block, err := processing.InstructionsFor(cfg.Profile, a.toolMenu())
	if err != nil {
		t.Fatalf("InstructionsFor: %v", err)
	}
	rendered := prompt.Render(promptTemplate, prompt.Inputs{
		Workspace: promptWorkspace,
		Mode:      string(domain.ModeAskBefore),
		Now:       promptNow,
	})
	want := rendered + "\n\n" + directive + "\n\n" + block
	if got.Messages[0].Role != string(domain.RoleSystem) || got.Messages[0].Content != want {
		t.Errorf("merged system message = %q\nwant %q", got.Messages[0].Content, want)
	}
}

// TestPromptSeam_ConfiguredPromptRendersFreshPerRequest: both live inputs — the date and the
// autonomy mode — are re-read per request, so a mode switch and a new day land on the next Turn
// rather than being frozen at construction.
func TestPromptSeam_ConfiguredPromptRendersFreshPerRequest(t *testing.T) {
	sink := &recordingSink{}
	cfg := menuConfig(t, sink)
	cfg.Mode = domain.ModeAskBefore
	cfg.WorkspaceDir = promptWorkspace
	cfg.SystemPrompt = promptTemplate

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	a.now = func() time.Time { return promptNow }
	if err := a.Submit(domain.UserInput{Text: "one"}); err != nil {
		t.Fatalf("Submit 1: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step 1: %v", err)
	}
	turn1 := responder.last.Messages[0].Content
	if !strings.Contains(turn1, "2026-07-26") || !strings.Contains(turn1, string(domain.ModeAskBefore)) {
		t.Fatalf("Turn 1 prompt = %q, want the fixed date and the ask-before mode", turn1)
	}

	// Roll the clock to the next day and drop to Plan: the next request must render both.
	nextDay := promptNow.AddDate(0, 0, 1)
	a.now = func() time.Time { return nextDay }
	a.SetMode(domain.ModePlan)
	if err := a.Submit(domain.UserInput{Text: "two"}); err != nil {
		t.Fatalf("Submit 2: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step 2: %v", err)
	}
	turn2 := responder.last.Messages[0].Content
	if !strings.Contains(turn2, "2026-07-27") {
		t.Errorf("Turn 2 prompt = %q, want the rolled-over date 2026-07-27", turn2)
	}
	if !strings.Contains(turn2, string(domain.ModePlan)) || strings.Contains(turn2, string(domain.ModeAskBefore)) {
		t.Errorf("Turn 2 prompt = %q, want the live plan mode and no trace of ask-before", turn2)
	}
}

// TestPromptSeam_ConfiguredPromptNeverEntersHistoryOrSnapshot: the rendered prompt is a
// request projection — no committed message carries it, and it is absent from the snapshot.
func TestPromptSeam_ConfiguredPromptNeverEntersHistoryOrSnapshot(t *testing.T) {
	const marker = "MARKER-SYSTEM-PROMPT-7f3"

	sink := &recordingSink{}
	cfg := menuConfig(t, sink)
	cfg.Mode = domain.ModeAskBefore
	cfg.WorkspaceDir = promptWorkspace
	cfg.SystemPrompt = "Remember " + marker + " while working in {{workspace}}."

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !strings.Contains(responder.last.Messages[0].Content, marker) {
		t.Fatalf("the wire request never carried the prompt: %+v", responder.last.Messages)
	}

	for i, m := range a.conv.Messages() {
		if m.Role == domain.RoleSystem {
			t.Errorf("committed message %d is a system message; the seeded prompt must stay off history", i)
		}
		if strings.Contains(m.Content, marker) {
			t.Errorf("committed message %d carries the seeded prompt: %q", i, m.Content)
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
		t.Error("the encoded snapshot carries the seeded prompt; it must be request-scoped only")
	}
}

// TestNewAgentRejectsUnknownPromptPlaceholder: a template with an unknown placeholder fails
// construction (the engine's own gate, mirroring the bad-profile one) with an error naming the
// offender and listing the known three.
func TestNewAgentRejectsUnknownPromptPlaceholder(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.SystemPrompt = "hi {{foo}}"

	_, err := newAgent(cfg, &recordingResponder{reply: "unused"})
	if err == nil {
		t.Fatal("newAgent accepted an unknown placeholder; a bad template must fail construction")
	}
	for _, want := range []string{"{{foo}}", prompt.PlaceholderWorkspace, prompt.PlaceholderDatetime, prompt.PlaceholderMode} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// ----------------------------------------------------------------------------
// Workspace context files (Config.ContextFiles) — the second standing source
// ----------------------------------------------------------------------------
//
// Item 2 of the workspace-context-files plan: the cache filled at a session boundary
// (contextfiles_test.go owns WHAT is discovered) rides the SAME seeded system message the
// template does. These tests therefore live here, on the seam harness: they drive the recording
// fake and assert on the captured provider.Request — one merged system message, prompt first,
// blocks in list order, content verbatim, and stable for the life of a session.
//
// TestPromptSeam_NativeProfileByteIdentical above is the complement: no prompt AND no context
// files still seeds nothing at all.

// contextSeamConfig returns a menuConfig rooted at dir and looking for names — the wire-facing
// counterpart of contextfiles_test.go's contextConfig.
func contextSeamConfig(t *testing.T, sink domain.EventSink, dir string, names ...string) domain.Config {
	t.Helper()
	cfg := menuConfig(t, sink)
	cfg.WorkspaceDir = dir
	cfg.ContextFiles = names
	return cfg
}

// contextBlock renders the block a readable context file is expected to contribute — spelled out
// here rather than borrowed from the production helper so the tests pin the wire shape itself.
func contextBlock(name, content string) string {
	return "## Workspace context: " + name + "\n\n" + content
}

// seedSystemMessage drives one Turn and returns the wire request's leading system message,
// failing when the request does not open with exactly one.
func seedSystemMessage(t *testing.T, a *Agent, responder *recordingResponder, text string) string {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit(%q): %v", text, err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step(%q): %v", text, err)
	}
	got := responder.last
	if n := countSystemMessages(got.Messages); n != 1 {
		t.Fatalf("wire request has %d system messages, want exactly 1: %+v", n, got.Messages)
	}
	if got.Messages[0].Role != string(domain.RoleSystem) {
		t.Fatalf("first wire message = %+v, want the seeded system message at position 0", got.Messages[0])
	}
	return got.Messages[0].Content
}

// TestContextSeam_FilesSeedWithoutAPrompt: context files are an INDEPENDENT standing source —
// found content seeds the system message even with no template configured. The block's trailing
// newline is trimmed, so a file's own line ending never widens the gap to the next block.
func TestContextSeam_FilesSeedWithoutAPrompt(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "Run make check before committing.\n")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md") // zero Profile, no SystemPrompt

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	got := seedSystemMessage(t, a, responder, "hi")

	want := contextBlock("AGENTS.md", "Run make check before committing.")
	if got != want {
		t.Errorf("seeded system message = %q\nwant %q", got, want)
	}
}

// TestContextSeam_PromptThenBlocksInListOrder: the rendered prompt leads, then every found file
// in LIST order (not alphabetical, not disk order), each under a header naming it.
func TestContextSeam_PromptThenBlocksInListOrder(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "agents guidance")
	writeWorkspaceFile(t, dir, "CONVENTIONS.md", "conventions guidance")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "CONVENTIONS.md", "MISSING.md", "AGENTS.md")
	cfg.Mode = domain.ModeAskBefore
	cfg.SystemPrompt = promptTemplate

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	a.now = func() time.Time { return promptNow }

	got := seedSystemMessage(t, a, responder, "hi")

	rendered := prompt.Render(promptTemplate, prompt.Inputs{
		Workspace: dir,
		Mode:      string(domain.ModeAskBefore),
		Now:       promptNow,
	})
	want := rendered + "\n\n" +
		contextBlock("CONVENTIONS.md", "conventions guidance") + "\n\n" +
		contextBlock("AGENTS.md", "agents guidance")
	if got != want {
		t.Errorf("seeded system message = %q\nwant %q", got, want)
	}
}

// TestContextSeam_UnreadableFileNeverReachesTheModel: an error entry is kept for the user-facing
// notice, but it contributes no block — the model is never told about a file it cannot be shown.
func TestContextSeam_UnreadableFileNeverReachesTheModel(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "AGENTS.md"), 0o755); err != nil { // present, unreadable
		t.Fatalf("Mkdir: %v", err)
	}
	writeWorkspaceFile(t, dir, "CONVENTIONS.md", "conventions guidance")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md", "CONVENTIONS.md")

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	got := seedSystemMessage(t, a, responder, "hi")

	if want := contextBlock("CONVENTIONS.md", "conventions guidance"); got != want {
		t.Errorf("seeded system message = %q\nwant %q (the unreadable file contributes nothing)", got, want)
	}
}

// TestContextSeam_EscapingSymlinkNeverReachesTheModel is the wire-facing half of the workspace
// fence on this path: a clone shipping `AGENTS.md` as a symlink to a file OUTSIDE the workspace
// contributes nothing to the seeded system message — not the block, not a byte of the target —
// while the readable sibling beside it still seeds byte-identical content. The name gate cannot
// see this case (the configured name is a plain relative one); the refusal comes from reading
// through security.SafeOpen. The user is still told, through the same present-but-unreadable
// notice a broken file gets.
func TestContextSeam_EscapingSymlinkNeverReachesTheModel(t *testing.T) {
	dir, marker := escapingContextWorkspace(t, "file")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md", "CONVENTIONS.md")

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	got := seedSystemMessage(t, a, responder, "hi")

	if want := contextBlock("CONVENTIONS.md", "conventions"); got != want {
		t.Errorf("seeded system message = %q\nwant %q (the escaping file contributes nothing)", got, want)
	}
	if strings.Contains(got, marker) {
		t.Errorf("the seeded system message carries a file from OUTSIDE the workspace: %q", got)
	}

	report := a.ContextFilesReport()
	if len(report.Files) != 2 {
		t.Fatalf("report holds %d notes, want 2 (the refusal is reported, never silent): %+v", len(report.Files), report.Files)
	}
	if note := report.Files[0]; note.Name != "AGENTS.md" || note.Err == "" || note.Bytes != 0 {
		t.Errorf("first note = %+v, want AGENTS.md carrying its refusal and no size", note)
	}
}

// TestContextSeam_MergesDirectivesAndToolBlock: prompt → context files → mechanism directive →
// tool block, all in ONE system message — the content joins the existing merge, it does not open
// a second message.
func TestContextSeam_MergesDirectivesAndToolBlock(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "agents guidance")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")
	cfg.Mode = domain.ModeAskBefore
	cfg.SystemPrompt = promptTemplate
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}
	cfg.Mechanisms = domain.NewMechanismRegistry()
	const directive = "Always cite the files you read. [seed]"
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, seedingHook{text: directive}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	a.now = func() time.Time { return promptNow }

	got := seedSystemMessage(t, a, responder, "hi")

	block, err := processing.InstructionsFor(cfg.Profile, a.toolMenu())
	if err != nil {
		t.Fatalf("InstructionsFor: %v", err)
	}
	rendered := prompt.Render(promptTemplate, prompt.Inputs{
		Workspace: dir,
		Mode:      string(domain.ModeAskBefore),
		Now:       promptNow,
	})
	want := rendered + "\n\n" + contextBlock("AGENTS.md", "agents guidance") + "\n\n" + directive + "\n\n" + block
	if got != want {
		t.Errorf("merged system message = %q\nwant %q", got, want)
	}
}

// TestContextSeam_ContentIsDataNotTemplate: the SAME token renders in the template and survives
// verbatim in a file — content never meets internal/prompt, so a repo's own braces reach the
// model as written (and can never fail apogee's startup).
func TestContextSeam_ContentIsDataNotTemplate(t *testing.T) {
	dir := t.TempDir()
	const fileContent = "Never write outside {{workspace}}."
	writeWorkspaceFile(t, dir, "AGENTS.md", fileContent)
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")
	cfg.Mode = domain.ModeAskBefore
	cfg.SystemPrompt = promptTemplate // renders {{workspace}} twice

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	a.now = func() time.Time { return promptNow }

	got := seedSystemMessage(t, a, responder, "hi")

	if !strings.Contains(got, contextBlock("AGENTS.md", fileContent)) {
		t.Errorf("seeded system message = %q\nwant the file's braces verbatim in %q", got, fileContent)
	}
	if n := strings.Count(got, prompt.PlaceholderWorkspace); n != 1 {
		t.Errorf("seeded system message carries %s %d times, want exactly 1 (the file's, never the template's):\n%s",
			prompt.PlaceholderWorkspace, n, got)
	}
	if !strings.Contains(got, dir) {
		t.Errorf("seeded system message = %q, want the template's %s rendered to %q", got, prompt.PlaceholderWorkspace, dir)
	}
}

// TestContextSeam_ContentStableWithinASession: a mid-session edit must never swap the content
// under a running session — two consecutive requests carry byte-identical seeded content, so the
// model server's prefix KV cache survives the session.
func TestContextSeam_ContentStableWithinASession(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "first")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)

	turn1 := seedSystemMessage(t, a, responder, "one")
	writeWorkspaceFile(t, dir, "AGENTS.md", "second") // edited mid-session
	turn2 := seedSystemMessage(t, a, responder, "two")

	if turn1 != turn2 {
		t.Errorf("Turn 2 seeded %q, want Turn 1's byte-identical %q", turn2, turn1)
	}
	if want := contextBlock("AGENTS.md", "first"); turn2 != want {
		t.Errorf("seeded system message = %q, want the session's original %q", turn2, want)
	}
}

// TestContextSeam_NewSessionCarriesTheNewBytes: /clear ends the session, so the next request
// speaks from the re-read file — the edit lands on the wire one boundary later.
func TestContextSeam_NewSessionCarriesTheNewBytes(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "first")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	seedSystemMessage(t, a, responder, "one")

	writeWorkspaceFile(t, dir, "AGENTS.md", "second")
	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}

	got := seedSystemMessage(t, a, responder, "two")

	if want := contextBlock("AGENTS.md", "second"); got != want {
		t.Errorf("seeded system message after /clear = %q, want the re-read %q", got, want)
	}
}

// TestContextSeam_SubAgentRequestCarriesParentBlocks: a sub-agent belongs to the parent's
// session, so its own requests speak from the parent's bytes — even with the file gone from disk
// by the time it spawns.
func TestContextSeam_SubAgentRequestCarriesParentBlocks(t *testing.T) {
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, "AGENTS.md", "parent bytes")
	cfg := contextSeamConfig(t, &recordingSink{}, dir, "AGENTS.md")

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	child, err := a.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	got := seedSystemMessage(t, child, responder, "delegated task")

	if want := contextBlock("AGENTS.md", "parent bytes"); got != want {
		t.Errorf("sub-agent seeded %q, want the parent session's %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// The restore seam — a stored system message is normalized away (ADR 0023)
// ----------------------------------------------------------------------------
//
// No apogee build writes a system message into the conversation, so these tests hand-craft the
// snapshot a legacy or hand-edited record can have. restoreState drops the leading system run, so
// buildRequest's unconditional prepend still leaves EXACTLY one system message on the wire.

// legacySnapshot builds a v1 snapshot carrying stored verbatim as the conversation, so a test can
// pose the shapes the engine itself never writes.
func legacySnapshot(t *testing.T, inExchange bool, exchangeStart int, stored ...domain.Message) domain.Session {
	t.Helper()
	state, err := json.Marshal(agentState{
		Conversation:  domain.NewConversation(stored),
		InExchange:    inExchange,
		ExchangeStart: exchangeStart,
	})
	if err != nil {
		t.Fatalf("marshal agentState: %v", err)
	}
	return domain.Session{Version: domain.SessionVersion, State: state}
}

// TestRestoreSeam_StoredSystemMessageDroppedAndNotDoubledOnTheWire: restoring a snapshot whose
// conversation OPENS with a system message drops it — the restored history holds none (so the next
// snapshot is clean) and the next request carries exactly one system message: the freshly rendered
// configured prompt, never the stored text.
func TestRestoreSeam_StoredSystemMessageDroppedAndNotDoubledOnTheWire(t *testing.T) {
	const stale = "MARKER-STORED-SYSTEM-PROMPT-c41"

	sink := &recordingSink{}
	cfg := menuConfig(t, sink) // zero Profile: native tool calls
	cfg.Mode = domain.ModeAskBefore
	cfg.WorkspaceDir = promptWorkspace
	cfg.SystemPrompt = promptTemplate

	responder := &recordingResponder{reply: "All done."}
	a := newProfileAgent(t, cfg, responder)
	a.now = func() time.Time { return promptNow }

	snap := legacySnapshot(t, false, 0,
		domain.Message{Role: domain.RoleSystem, Content: "You are a helpful assistant. " + stale},
		domain.Message{Role: domain.RoleUser, Content: "one"},
		domain.Message{Role: domain.RoleAssistant, Content: "first"},
	)
	if err := a.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	for i, m := range a.conv.Messages() {
		if m.Role == domain.RoleSystem {
			t.Errorf("restored message %d is a system message; the restore seam must normalize it away", i)
		}
	}
	if got := a.conv.Len(); got != 2 {
		t.Fatalf("restored conversation has %d messages, want 2 (only the stored system message dropped)", got)
	}
	if !containsUser(&a.conv, "one") {
		t.Error("the restore dropped more than the leading system message: user message \"one\" is gone")
	}

	// seedSystemMessage fails unless the wire request opens with EXACTLY one system message.
	got := seedSystemMessage(t, a, responder, "two")

	want := prompt.Render(promptTemplate, prompt.Inputs{
		Workspace: promptWorkspace,
		Mode:      string(domain.ModeAskBefore),
		Now:       promptNow,
	})
	if got != want {
		t.Errorf("wire system message = %q\nwant the freshly rendered prompt %q", got, want)
	}
	if strings.Contains(got, stale) {
		t.Errorf("the stored system message reached the wire: %q", got)
	}
}

// TestRestoreSeam_ExchangeBoundaryShiftsWithTheDroppedPrefix: dropping the stored system message
// shifts every later message down, so the cached rollback boundary moves with it — AbortExchange
// on a restored mid-Exchange snapshot still rolls back to that Exchange's own opening message
// rather than one message too far into the answered history.
func TestRestoreSeam_ExchangeBoundaryShiftsWithTheDroppedPrefix(t *testing.T) {
	a := newProfileAgent(t, baseConfig(&recordingSink{}), &recordingResponder{reply: "unused"})

	// Stored: [0] system, [1] user "done", [2] assistant "ok", [3] user "open" — the Exchange
	// still in flight opened at index 3.
	snap := legacySnapshot(t, true, 3,
		domain.Message{Role: domain.RoleSystem, Content: "stored prompt"},
		domain.Message{Role: domain.RoleUser, Content: "done"},
		domain.Message{Role: domain.RoleAssistant, Content: "ok"},
		domain.Message{Role: domain.RoleUser, Content: "open"},
	)
	if err := a.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if got := a.exchangeBoundary(); got != 2 {
		t.Errorf("restored exchangeBoundary() = %d, want 2 (shifted down by the dropped system message)", got)
	}

	a.AbortExchange()

	if got := a.conv.Len(); got != 2 {
		t.Fatalf("after abort conv.Len() = %d, want 2 (the answered exchange kept, the open one dropped)", got)
	}
	if containsUser(&a.conv, "open") {
		t.Error("the abort kept the open Exchange's user message; the boundary was too high")
	}
	if !containsUser(&a.conv, "done") {
		t.Error("the abort dropped the answered exchange's user message; the boundary was too low")
	}
}
