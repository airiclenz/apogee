package agent

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// auditEvents returns every AuditEvent recorded on the sink, in order.
func auditEvents(events []domain.Event) []domain.AuditEvent {
	var out []domain.AuditEvent
	for _, e := range events {
		if ae, ok := e.(domain.AuditEvent); ok {
			out = append(out, ae)
		}
	}
	return out
}

// TestAuditEvent_EmittedForExecutedCall is the M1 regression: an executed tool call's
// audit record is surfaced to the EventSink as a domain.AuditEvent, so the trail is
// OBSERVABLE rather than held only in the in-process ring no observer reads. Before the
// fix nothing threaded a.guards.Audit to any observer.
func TestAuditEvent_EmittedForExecutedCall(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, result: "the answer"})
	cfg.Mode = domain.ModeAskBefore

	driveToolCall(t, cfg, sink, "c1", "lookup", `{"q":"x"}`)

	audits := auditEvents(sink.events)
	if len(audits) != 1 {
		t.Fatalf("AuditEvent count = %d, want 1", len(audits))
	}
	ae := audits[0]
	if ae.Tool != "lookup" || ae.CallID != "c1" {
		t.Errorf("AuditEvent = %+v, want lookup/c1", ae)
	}
	if ae.Decision != string(security.AuditAllowed) {
		t.Errorf("AuditEvent.Decision = %q, want %q", ae.Decision, security.AuditAllowed)
	}
	if ae.IsError {
		t.Errorf("AuditEvent.IsError = true, want false for a clean result")
	}
}

// TestAuditEvent_EmittedForRefusedCall proves a guardrail-refused call (Tier-1 dangerous
// action) is also surfaced as an AuditEvent — a blocked call is observable, not silently
// dropped (M1 covers blocked records, not just executed ones).
func TestAuditEvent_EmittedForRefusedCall(t *testing.T) {
	sink := &recordingSink{}
	// A read-only tool with dangerous args trips the Tier-1 dangerous-action floor before
	// execution, exercising the recordBlocked path.
	cfg := configWithTools(sink, fakeTool{name: "shell", readOnly: true, result: "x"})
	cfg.Mode = domain.ModeAskBefore

	driveToolCall(t, cfg, sink, "c1", "shell", `{"command":"rm -rf /"}`)

	audits := auditEvents(sink.events)
	if len(audits) != 1 {
		t.Fatalf("AuditEvent count = %d, want 1 for a refused call", len(audits))
	}
	if audits[0].Decision != string(security.AuditDangerousRefused) {
		t.Errorf("AuditEvent.Decision = %q, want %q", audits[0].Decision, security.AuditDangerousRefused)
	}
}

// TestAuditEvent_SubAgentRecordReachesParentObserver is the M1 sub-agent half: a sub-agent
// records into its OWN (isolated) audit ring, but because it emits through the parent's
// EventSink at Depth > 0, its audit record reaches the same observer instead of vanishing
// when the child Agent is discarded. The depth on the event distinguishes it.
func TestAuditEvent_SubAgentRecordReachesParentObserver(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, result: "child-answer"})
	cfg.Mode = domain.ModeAskBefore

	// Build a parent agent and synthesise a child the same way newChildAgent does, then
	// drive a tool call on the child: its AuditEvent must carry the child's depth and land
	// on the shared sink.
	a, err := newAgent(cfg, &echoResponder{reply: "ignored"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	child, err := a.newChildAgent()
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	// The child shares the parent's EventSink and records through its own guards.
	child.recordExecuted(0, domain.ToolCall{ID: "s1", Tool: "lookup"}, security.AuditAllowed, "",
		domain.ToolResult{CallID: "s1", Content: "child-answer"})

	audits := auditEvents(sink.events)
	if len(audits) != 1 {
		t.Fatalf("AuditEvent count = %d, want 1 from the sub-agent", len(audits))
	}
	if audits[0].Depth != 1 {
		t.Errorf("sub-agent AuditEvent.Depth = %d, want 1 (nested), so the parent observer can attribute it", audits[0].Depth)
	}
	if audits[0].CallID != "s1" {
		t.Errorf("AuditEvent.CallID = %q, want s1", audits[0].CallID)
	}
	// And the child's own ring is non-empty (isolated from the parent's), proving the
	// record was genuinely produced one level down.
	if child.guards.Audit.Len() != 1 {
		t.Errorf("child audit ring len = %d, want 1", child.guards.Audit.Len())
	}
}
