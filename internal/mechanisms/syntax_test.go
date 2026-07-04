package mechanisms

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// Broken Go content in a write call retries in place (R1) with a correction pointing at the file
// and line — the Go parser is authoritative for Go.
func TestSyntaxBrokenGoRetriesWithCorrection(t *testing.T) {
	t.Parallel()
	resp := responseWith(nil, writeCall("c1", "broken.go", "package main\nfunc main() {\n"))
	decision := postResponse(t, syntaxID, resp)

	if decision.Action != domain.ActionRetry {
		t.Fatalf("Action = %q, want %q", decision.Action, domain.ActionRetry)
	}
	if !strings.Contains(decision.Inject, "syntax error in broken.go") {
		t.Errorf("Inject = %q, want it to name the broken file", decision.Inject)
	}
}

// A non-Go language uses the bracket/string heuristic: an unclosed paren in a .js write is caught.
func TestSyntaxBrokenGenericRetriesWithCorrection(t *testing.T) {
	t.Parallel()
	resp := responseWith(nil, writeCall("c1", "app.js", "const x = (1 + 2\n"))
	decision := postResponse(t, syntaxID, resp)
	if decision.Action != domain.ActionRetry {
		t.Fatalf("Action = %q, want %q", decision.Action, domain.ActionRetry)
	}
	if !strings.Contains(decision.Inject, "app.js") {
		t.Errorf("Inject = %q, want it to name the broken file", decision.Inject)
	}
}

// Valid content, a non-write tool, and an unrecognised extension are each no-ops.
func TestSyntaxNoOpCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call domain.ToolCall
	}{
		{name: "valid go", call: writeCall("c1", "ok.go", "package main\n\nfunc main() {}\n")},
		{
			name: "non-write tool",
			call: domain.ToolCall{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"broken.go","content":"package main\nfunc main() {"}`)},
		},
		{name: "unrecognised extension", call: writeCall("c1", "notes.bin", "func main() {")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			decision := postResponse(t, syntaxID, responseWith(nil, tt.call))
			if decision.Action != "" || decision.Inject != "" {
				t.Errorf("decision = %+v, want the no-op zero decision", decision)
			}
		})
	}
}
