package confinetest

import (
	"strings"
	"testing"
)

// TestSkipOrFail pins the one decision ProbeNetworkRequired rests on: a step the host cannot
// run is skipped unless the caller required it, and a required step fails instead — with the
// skip's own reason still in the message, so the failure says what could not run.
func TestSkipOrFail(t *testing.T) {
	t.Parallel()
	const reason = "confinetest: backend reports NetworkEgress==false; skipping network battery"
	tests := []struct {
		name     string
		required bool
		wantSkip bool
	}{
		{"not_required_skips", false, true},
		{"required_fails", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			skip, msg := skipOrFail(tt.required, reason)

			if skip != tt.wantSkip {
				t.Errorf("skipOrFail(%v, …) skip = %v, want %v", tt.required, skip, tt.wantSkip)
			}
			if !strings.Contains(msg, reason) {
				t.Errorf("skipOrFail(%v, …) msg = %q, want it to carry the reason %q", tt.required, msg, reason)
			}
		})
	}
}
