package mechanisms

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// cotMenu is a menu offering a read, write, and list tool — enough for every nudge's tool-presence
// gate.
var cotMenu = []domain.ToolDef{{Name: "read_file"}, {Name: "write_file"}, {Name: "list_dir"}}

// readOnlyTurns appends n assistant turns that each call tool (a read-only tool) followed by its
// result — the shape countReadOnlyTurns counts.
func readOnlyTurns(n int, tool string) []domain.Message {
	var msgs []domain.Message
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		msgs = append(msgs,
			domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: id, Tool: tool, Arguments: []byte(`{"path":"p` + id + `"}`)}}},
			domain.Message{Role: domain.RoleTool, ToolCallID: id, Content: "listing/content"},
		)
	}
	return msgs
}

func hasSystemMarker(req *domain.Request, marker string) bool {
	for _, m := range req.State().Messages {
		if m.Role == domain.RoleSystem && strings.Contains(m.Content, marker) {
			return true
		}
	}
	return false
}

func TestCotNudgesDescriptorsAndBuild(t *testing.T) {
	t.Parallel()
	for _, id := range []domain.MechanismID{toolUseDirectiveID, stallNudgeID, listNudgeID} {
		m, err := Build(id, Deps{})
		if err != nil {
			t.Fatalf("Build(%q): %v", id, err)
		}
		d := m.Descriptor()
		if d.ID != id {
			t.Errorf("built ID = %q, want %q", d.ID, id)
		}
		if d.Capability != domain.CapProactiveNudge {
			t.Errorf("%q Capability = %q, want proactive-nudge", id, d.Capability)
		}
		if d.Suppression != domain.SuppressStrikesThree {
			t.Errorf("%q Suppression = %q, want strikes-3", id, d.Suppression)
		}
		if _, ok := m.(domain.PreRequestHook); !ok {
			t.Errorf("%q does not implement PreRequestHook", id)
		}
	}
}

// stall_nudge ⊥ list_nudge: the two declare each other incompatible, so a registry holding both
// fails the construction-time incompatibility gate (item 2), which is how apogee enforces the sim's
// "only one fires" exclusivity per config.
func TestStallListIncompatible(t *testing.T) {
	t.Parallel()
	stall, _ := newStallNudge(Deps{})
	list, _ := newListNudge(Deps{})
	reg := domain.NewMechanismRegistry()
	if err := reg.Add(stall); err != nil {
		t.Fatalf("Add(stall): %v", err)
	}
	if err := reg.Add(list); err != nil {
		t.Fatalf("Add(list): %v", err)
	}
	if err := reg.ValidateIncompatibilities(); err == nil {
		t.Error("registering both stall_nudge and list_nudge should fail the incompatibility gate")
	}
}

// tool_use_directive fires when an action was asked for, tools are available, and the model has
// neither written a file nor used any tool yet — injected once, idempotent on the marker.
func TestToolUseDirectiveInjectsOnce(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "fix the bug in main.go"},
	}, cotMenu)

	if err := (toolUseDirectiveMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if !hasSystemMarker(req, cotToolUseMarker) {
		t.Fatal("tool-use directive not injected for an action prompt with no tool use yet")
	}
	mid := req.Revision()
	if err := (toolUseDirectiveMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("second PreRequest: %v", err)
	}
	if req.Revision() != mid {
		t.Fatal("tool-use directive re-injected despite the marker already present")
	}
}

// It stays silent once the model has already used a tool (fires only before first tool use).
func TestToolUseDirectiveSkipsAfterToolUse(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "fix the bug in main.go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "package main"},
	}, cotMenu)
	before := req.Revision()
	if err := (toolUseDirectiveMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("tool-use directive injected after the model had already used a tool")
	}
}

// An analysis request is not pushed into a tool call (the directive requires !hasAnalysis).
func TestToolUseDirectiveSkipsAnalysis(t *testing.T) {
	t.Parallel()
	req := shaperRequest([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "summarize what this project does"},
	}, cotMenu)
	before := req.Revision()
	if err := (toolUseDirectiveMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("tool-use directive injected for an analysis request")
	}
}

// stall_nudge fires when an action has stalled: read-only for at least the stall threshold of turns,
// a write tool available.
func TestStallNudgeFiresAtThreshold(t *testing.T) {
	t.Parallel()
	msgs := append([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
	}, readOnlyTurns(cotStallThreshold, "read_file")...)
	msgs = append(msgs, domain.Message{Role: domain.RoleUser, Content: "now fix the failing test"})

	req := shaperRequest(msgs, cotMenu)
	if err := (stallNudgeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if !hasSystemMarker(req, cotStallMarker) {
		t.Fatal("stall nudge did not fire at the stall threshold")
	}
}

// Below the stall threshold there is no nudge.
func TestStallNudgeSkipsBelowThreshold(t *testing.T) {
	t.Parallel()
	msgs := append([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
	}, readOnlyTurns(cotStallThreshold-2, "read_file")...)
	msgs = append(msgs, domain.Message{Role: domain.RoleUser, Content: "now fix the failing test"})

	req := shaperRequest(msgs, cotMenu)
	before := req.Revision()
	if err := (stallNudgeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("stall nudge fired below the stall threshold")
	}
}

// list_nudge fires when an analysis request has listed directories but read no files.
func TestListNudgeFiresOnListWithoutRead(t *testing.T) {
	t.Parallel()
	msgs := append([]domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "analyze the project structure"},
	}, readOnlyTurns(cotListNudgeThreshold, "list_dir")...)

	req := shaperRequest(msgs, cotMenu)
	if err := (listNudgeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if !hasSystemMarker(req, cotListNudgeMarker) {
		t.Fatal("list nudge did not fire on list-without-read")
	}
}

// Once a file has been read, the list nudge is silent (filesRead != 0).
func TestListNudgeSkipsAfterRead(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "SYS"},
		{Role: domain.RoleUser, Content: "analyze the project structure"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "l1", Tool: "list_dir", Arguments: []byte(`{"path":"."}`)}}},
		{Role: domain.RoleTool, ToolCallID: "l1", Content: "a.go\nb.go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "r1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "r1", Content: "package a"},
	}
	req := shaperRequest(msgs, cotMenu)
	before := req.Revision()
	if err := (listNudgeMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("list nudge fired after the model had already read a file")
	}
}
