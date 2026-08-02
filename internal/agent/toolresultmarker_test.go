package agent

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// appendToolResult is the ONE seam every tool result crosses into history, so it is where the
// result's IsError verdict is projected onto the committed message. Both outcomes are recorded
// explicitly: a history scanner must be able to tell "this succeeded" from "nothing was recorded",
// which is the whole reason the marker is a tri-state rather than a bool.
func TestAppendToolResultCommitsTheOutcomeMarker(t *testing.T) {
	a, err := newAgent(configWithTools(&recordingSink{}), echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "go"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
		{ID: "ok", Tool: "read_file"},
		{ID: "bad", Tool: "read_file"},
	}})
	// The success carries error-shaped text (a file body quoting an error string) and the failure
	// carries none, so only the marker can tell them apart — exactly the case text sniffing got
	// backwards.
	a.appendToolResult(0, domain.ToolResult{CallID: "ok", Content: "[File: e.go]\nreturn fmt.Errorf(\"error: does not exist\")"})
	a.appendToolResult(0, domain.ToolResult{CallID: "bad", Content: "gone.go", IsError: true})

	if got := a.conv.At(2).ToolOutcome; got != domain.ToolOutcomeSucceeded {
		t.Errorf("committed IsError:false result carries ToolOutcome %q, want %q", got, domain.ToolOutcomeSucceeded)
	}
	if got := a.conv.At(3).ToolOutcome; got != domain.ToolOutcomeFailed {
		t.Errorf("committed IsError:true result carries ToolOutcome %q, want %q", got, domain.ToolOutcomeFailed)
	}
}

// The marker is only worth having if it outlives the process: a Mechanism scanning a resumed
// session's history reads the SAME fact the live loop recorded. It rides the conversation's own
// marshal as an omitempty sibling, so this round-trip needs no SessionVersion bump.
func TestToolOutcomeMarkerSurvivesSnapshotResume(t *testing.T) {
	a, err := newAgent(configWithTools(&recordingSink{}), echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: "go"})
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}})
	a.appendToolResult(0, domain.ToolResult{CallID: "c1", Content: "no such file: gone.go", IsError: true})

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	restored, err := newAgent(configWithTools(&recordingSink{}), echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent (restore target): %v", err)
	}
	if err := restored.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if got := restored.conv.At(2).ToolOutcome; got != domain.ToolOutcomeFailed {
		t.Errorf("restored tool result carries ToolOutcome %q, want %q", got, domain.ToolOutcomeFailed)
	}
}
