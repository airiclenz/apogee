package mechanisms

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// hasMarker reports whether any request message carries the file-hint marker (the injected hint,
// which lands in the system message when the conversation ends in a tool result).
func hasMarker(req *domain.Request) bool {
	for _, m := range req.State().Messages {
		if strings.Contains(m.Content, fileHintMarker) {
			return true
		}
	}
	return false
}

func TestFileHintDescriptorAndOrdering(t *testing.T) {
	t.Parallel()
	m, err := newFileHint(Deps{})
	if err != nil {
		t.Fatalf("newFileHint: %v", err)
	}
	d := m.Descriptor()
	if d.ID != fileHintID {
		t.Errorf("ID = %q, want %q", d.ID, fileHintID)
	}
	if d.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want proactive-nudge", d.Capability)
	}
	if d.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want strikes-3", d.Suppression)
	}
	if o := m.Ordering(); len(o.Before) != 0 || len(o.After) != 0 {
		t.Errorf("Ordering = %+v, want none (catalogue Table A)", o)
	}
	if _, ok := m.(domain.PreRequestHook); !ok {
		t.Error("filehint does not implement PreRequestHook")
	}
}

// listThenPrompt is a conversation where the model listed a directory (3 files) but has not read
// anything, ending in the tool result — the open hint opportunity, ending in a tool result so the
// role-safe inject appends to the system prompt.
func listThenPrompt(prompt string) []domain.Message {
	return []domain.Message{
		{Role: domain.RoleUser, Content: prompt},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "list_dir"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "main.go\nconfig.go\nserver.go"},
	}
}

// A directory listing the model has not read from, plus a prompt naming a listed file, injects a
// role-safe hint (the conversation ends in a tool result, so the hint appends to the system prompt).
func TestFileHintInjectsRoleSafeHint(t *testing.T) {
	t.Parallel()
	req := shaperRequest(listThenPrompt("fix the config in config.go"), nil)
	before := req.Revision()

	if err := (fileHintMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() == before {
		t.Fatal("an open hint opportunity should have injected a hint")
	}
	if !hasMarker(req) {
		t.Error("hint not injected")
	}
	// Ends-in-tool-result: the inject is role-safe — it does NOT leave a user message after the
	// tool result (which strict chat templates reject); it folds into the system prompt.
	msgs := req.State().Messages
	if msgs[len(msgs)-1].Role != domain.RoleTool {
		t.Errorf("last message role = %q, want tool (inject must not append a user message after a tool result)", msgs[len(msgs)-1].Role)
	}
	if msgs[0].Role != domain.RoleSystem || !strings.Contains(msgs[0].Content, fileHintMarker) {
		t.Error("hint not folded into the system prompt for the ends-in-tool-result case")
	}
}

// The marker makes injection idempotent: a request already carrying the hint is not re-injected.
func TestFileHintIdempotent(t *testing.T) {
	t.Parallel()
	msgs := append([]domain.Message{
		{Role: domain.RoleSystem, Content: "sys\n\n" + fileHintMarker + " to your task:\n- config.go"},
	}, listThenPrompt("fix the config in config.go")...)
	req := shaperRequest(msgs, nil)
	before := req.Revision()

	if err := (fileHintMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("hint re-injected despite the marker already being present")
	}
}

// Once the model has read a file after listing, the opportunity is closed — no hint.
func TestFileHintSkipsAfterRead(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "fix the config in config.go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "list_dir"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "main.go\nconfig.go\nserver.go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c2", Content: "package main"},
	}
	req := shaperRequest(msgs, nil)
	before := req.Revision()
	if err := (fileHintMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("hint injected after the model already read a file; the opportunity is closed")
	}
}

// Fewer than the minimum listed files means no hint (apogee-sim fileHintMinFiles).
func TestFileHintSkipsTooFewFiles(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "fix the config in config.go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "list_dir"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "config.go\nserver.go"},
	}
	req := shaperRequest(msgs, nil)
	before := req.Revision()
	if err := (fileHintMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("hint injected with fewer than the minimum listed files")
	}
}

// A greenfield creation task with no files written yet is suppressed — hinting at existing files to
// read is unhelpful when the goal is creating new ones (apogee-sim isCreationFocused guard).
func TestFileHintSuppressesGreenfieldCreation(t *testing.T) {
	t.Parallel()
	req := shaperRequest(listThenPrompt("create and build `a.go` and `b.go` in the project"), nil)
	before := req.Revision()
	if err := (fileHintMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("hint injected for a greenfield creation task with no files written")
	}
}

func TestFileHintBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	m, err := Build(fileHintID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", fileHintID, err)
	}
	if m.Descriptor().ID != fileHintID {
		t.Errorf("built ID = %q, want %q", m.Descriptor().ID, fileHintID)
	}
}
