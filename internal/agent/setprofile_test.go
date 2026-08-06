package agent

// The idle-only model-profile mutator (ADR 0037): SetProfile re-selects the parse seam's
// collaborators from a new profile at a quiescent boundary. What these pin is the SEAM's
// contract — the NEXT response is read through the new parser and the emit half follows, a swap
// mid-Exchange is refused with the idle-only class error, and a profile the translation refuses
// leaves the session parsing exactly as it did (validate-then-commit).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fencedReadFileCall is a markdown-fenced tool call in the model's VISIBLE content — prose to a
// native profile, a recoverable call to the markdown-fenced one. That difference is the whole
// observable of a profile swap.
const fencedReadFileCall = "Let me read that file.\n\n```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nmain.go\n```"

// answerOnce drives one Submit+Step round against the fake responder and returns the assistant
// message the seam committed for it.
func answerOnce(t *testing.T, a *Agent, text string) domain.Message {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	return lastAssistantMessage(t, a)
}

// TestSetProfileMovesTheNextResponseToTheNewParser: the same wire content that was prose under
// the native profile is recovered as a tool call and stripped of its markup under the
// markdown-fenced one — and cfg.Profile moves with the collaborators, so the emit half starts
// rendering the wire-only tool menu the new format needs.
func TestSetProfileMovesTheNextResponseToTheNewParser(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	a := newProfileAgent(t, cfg, echoResponder{reply: fencedReadFileCall})

	before := answerOnce(t, a, "read main.go")
	if len(before.ToolCalls) != 0 {
		t.Fatalf("the native profile recovered %d call(s) from visible content, want 0", len(before.ToolCalls))
	}
	if !strings.Contains(before.Content, "TOOL_NAME") {
		t.Fatalf("the native profile stripped markup it should have left alone: %q", before.Content)
	}
	if block := a.toolInstructions(a.toolMenu()); block != "" {
		t.Fatalf("the native profile injected emit instructions: %q", block)
	}

	if err := a.SetProfile(domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}

	after := answerOnce(t, a, "read main.go again")
	if len(after.ToolCalls) != 1 {
		t.Fatalf("assistant ToolCalls after the swap = %d, want 1", len(after.ToolCalls))
	}
	if got := after.ToolCalls[0].Tool; got != "read_file" {
		t.Errorf("recovered call Tool = %q, want read_file", got)
	}
	if strings.Contains(after.Content, "TOOL_NAME") || strings.Contains(after.Content, "```") {
		t.Errorf("committed content still carries tool markup after the swap: %q", after.Content)
	}
	if !strings.Contains(after.Content, "Let me read that file.") {
		t.Errorf("committed content lost its prose after the swap: %q", after.Content)
	}
	if block := a.toolInstructions(a.toolMenu()); !strings.Contains(block, "read_file") {
		t.Errorf("the emit half did not follow the swap; instructions = %q", block)
	}
}

// TestSetProfileMovesTheStripperToo: the inline thinking channel is the profile's other half — a
// delimited channel is invisible under the native profile and lifted into reasoning under the
// delimited one.
func TestSetProfileMovesTheStripperToo(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	a := newProfileAgent(t, cfg, echoResponder{reply: "<think>weighing it up</think>The answer is 42."})

	before := answerOnce(t, a, "think about it")
	if !strings.Contains(before.Content, "weighing it up") {
		t.Fatalf("the native profile stripped an inline channel it does not know: %q", before.Content)
	}

	profile := domain.ModelProfile{Thinking: domain.ThinkingProfile{
		Style: domain.ThinkingDelimited,
		Start: "<think>",
		End:   "</think>",
	}}
	if err := a.SetProfile(profile); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}

	after := answerOnce(t, a, "think about it again")
	if strings.Contains(after.Content, "weighing it up") {
		t.Errorf("the delimited channel survived in visible content: %q", after.Content)
	}
	if !strings.Contains(after.Content, "The answer is 42.") {
		t.Errorf("the stripper ate the visible answer: %q", after.Content)
	}
	assertReasoning(t, after, "weighing it up")
}

// TestSetProfileRefusedMidExchange: an Exchange left open by a cancel refuses the swap with the
// idle-only class error, and the session keeps reading responses with the profile it had.
func TestSetProfileRefusedMidExchange(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	responder := blockingResponder{started: make(chan struct{})}
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "slow"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started
		cancel()
	}()
	if _, err := a.Step(ctx); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if !a.InExchange() {
		t.Fatal("the cancelled Exchange is not open; the refusal below would prove nothing")
	}

	err = a.SetProfile(domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced})

	if !errors.Is(err, domain.ErrInputPending) {
		t.Errorf("SetProfile mid-Exchange err = %v, want it to match ErrInputPending", err)
	}
	if a.cfg.Profile.ToolCallFormat != "" {
		t.Errorf("a refused SetProfile moved cfg.Profile to %q", a.cfg.Profile.ToolCallFormat)
	}
	if _, found := a.textParser.ParseToolCall(fencedReadFileCall); found {
		t.Error("a refused SetProfile installed the new parser anyway")
	}
}

// TestSetProfileKeepsTheOldParserOnAnInvalidProfile: validate-then-commit — a profile whose
// translation fails (unknown tool-call format, unknown thinking style) leaves all three fields
// on the profile the session was running, so nothing is ever half-swapped.
func TestSetProfileKeepsTheOldParserOnAnInvalidProfile(t *testing.T) {
	t.Parallel()

	live := domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}
	cases := []struct {
		name    string
		profile domain.ModelProfile
	}{
		{
			name:    "unknown tool-call format",
			profile: domain.ModelProfile{ToolCallFormat: domain.ToolCallFormat("telepathy")},
		},
		{
			// The parser translates fine here and the STRIPPER is what refuses — the case that
			// proves the commit waits for both collaborators, not just the first.
			name:    "unknown thinking style",
			profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingStyle("telepathy")}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := baseConfig(&recordingSink{})
			cfg.Profile = live
			a := newProfileAgent(t, cfg, echoResponder{reply: "ok"})

			if err := a.SetProfile(tc.profile); err == nil {
				t.Fatal("SetProfile accepted a profile processing.ParserFor refuses")
			}

			if a.cfg.Profile != live {
				t.Errorf("cfg.Profile = %+v after a refused swap, want the live one %+v", a.cfg.Profile, live)
			}
			if _, found := a.textParser.ParseToolCall(fencedReadFileCall); !found {
				t.Error("the live parser stopped recovering fenced calls after a refused swap")
			}
			if _, reasoning := a.stripper.Strip("<think>x</think>visible"); reasoning != "" {
				t.Errorf("the live stripper was replaced by a refused swap; it lifted %q", reasoning)
			}
		})
	}
}
