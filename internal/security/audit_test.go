package security

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

func auditCall(tool, id string) domain.ToolCall {
	args, _ := json.Marshal(map[string]string{"k": "v"})
	return domain.ToolCall{ID: id, Tool: tool, Arguments: args}
}

func TestAuditLog_RecordsCallDecisionResult(t *testing.T) {
	t.Parallel()
	log := NewAuditLog()

	log.RecordCall(
		auditCall("write_file", "call-1"),
		AuditAllowed, "",
		domain.ToolResult{CallID: "call-1", Content: "wrote 3 bytes"},
	)
	log.RecordCall(
		auditCall("terminal", "call-2"),
		AuditDangerousRefused, "recursive force-delete of a root path",
		domain.ToolResult{CallID: "call-2", Content: "refused", IsError: true},
	)

	recs := log.Records()
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}

	if recs[0].Tool != "write_file" || recs[0].CallID != "call-1" || recs[0].Decision != AuditAllowed {
		t.Errorf("record 0 = %+v, want write_file/call-1/allowed", recs[0])
	}
	if recs[0].IsError {
		t.Errorf("record 0 IsError = true, want false")
	}
	if recs[0].Result != "wrote 3 bytes" {
		t.Errorf("record 0 Result = %q, want the result content", recs[0].Result)
	}

	if recs[1].Decision != AuditDangerousRefused || recs[1].Reason == "" || !recs[1].IsError {
		t.Errorf("record 1 = %+v, want a refused error decision with a reason", recs[1])
	}
	if recs[0].Time.IsZero() || recs[1].Time.IsZero() {
		t.Errorf("audit records must carry a timestamp")
	}
}

func TestAuditLog_AppendOnlyOrderPreserved(t *testing.T) {
	t.Parallel()
	log := NewAuditLog()
	for _, id := range []string{"a", "b", "c"} {
		log.RecordCall(auditCall("grep", id), AuditAllowed, "", domain.ToolResult{CallID: id})
	}
	recs := log.Records()
	if len(recs) != 3 || recs[0].CallID != "a" || recs[1].CallID != "b" || recs[2].CallID != "c" {
		t.Fatalf("append order not preserved: %+v", recs)
	}
	// Records() returns a copy: mutating it must not affect the log.
	recs[0].Tool = "tampered"
	if log.Records()[0].Tool == "tampered" {
		t.Fatal("Records() leaked internal storage (a copy is required)")
	}
}

func TestAuditLog_CapEvictsOldestAndCountsDropped(t *testing.T) {
	t.Parallel()
	const cap = 3
	log := NewAuditLogWithCap(cap)

	// Append more than the cap; the ring keeps only the most-recent `cap` records and
	// counts the rest as dropped (overflow is observable, not silent).
	const total = 7
	ids := []string{"a", "b", "c", "d", "e", "f", "g"}
	for _, id := range ids {
		log.RecordCall(auditCall("grep", id), AuditAllowed, "", domain.ToolResult{CallID: id})
	}

	if got := log.Len(); got != cap {
		t.Errorf("Len() = %d, want capped at %d", got, cap)
	}
	if got := log.Dropped(); got != total-cap {
		t.Errorf("Dropped() = %d, want %d (total %d - cap %d)", got, total-cap, total, cap)
	}

	// The retained window must be the LAST `cap` records, in append order.
	recs := log.Records()
	if len(recs) != cap {
		t.Fatalf("Records() = %d, want %d", len(recs), cap)
	}
	wantTail := ids[total-cap:] // {"e", "f", "g"}
	for i, want := range wantTail {
		if recs[i].CallID != want {
			t.Errorf("retained record %d CallID = %q, want %q (ring must keep newest in order)", i, recs[i].CallID, want)
		}
	}
}

func TestAuditLog_UnderCapDropsNothing(t *testing.T) {
	t.Parallel()
	log := NewAuditLogWithCap(5)
	for _, id := range []string{"a", "b", "c"} {
		log.RecordCall(auditCall("grep", id), AuditAllowed, "", domain.ToolResult{CallID: id})
	}
	if got := log.Dropped(); got != 0 {
		t.Errorf("Dropped() = %d under cap, want 0", got)
	}
	if got := log.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
}

func TestAuditLog_TruncatesLargeResult(t *testing.T) {
	t.Parallel()
	log := NewAuditLog()
	big := strings.Repeat("x", maxAuditResultBytes+500)
	log.RecordCall(auditCall("read_file", "big"), AuditAllowed, "", domain.ToolResult{Content: big})
	if got := len(log.Records()[0].Result); got != maxAuditResultBytes {
		t.Fatalf("result length = %d, want truncated to %d", got, maxAuditResultBytes)
	}
}
