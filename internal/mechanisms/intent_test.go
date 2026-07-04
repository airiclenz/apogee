package mechanisms

import "testing"

// hasActionIntent recognises imperative action requests and rejects questions and empty input —
// the classifier the tool_use_enforcer off-ramp gates on (apogee-sim intent parity).
func TestHasActionIntent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		msg  string
		want bool
	}{
		{"please fix the bug in main.go", true},
		{"implement the parser", true},
		{"read config.yaml and update it", true},
		{"what does this function do?", false}, // question word + trailing ?
		{"how should I structure the package?", false},
		{"the weather is nice today", false}, // no action verb
		{"", false},
	}
	for _, tt := range tests {
		if got := hasActionIntent(tt.msg); got != tt.want {
			t.Errorf("hasActionIntent(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

// hasAnalysisIntent recognises read-only analysis requests (verbs and phrases) the enforcer must
// not push toward a tool call.
func TestHasAnalysisIntent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		msg  string
		want bool
	}{
		{"summarize the module", true},
		{"walk me through the design", true}, // phrase
		{"give me an overview of the code", true},
		{"fix the failing test", false}, // action, not analysis
		{"", false},
	}
	for _, tt := range tests {
		if got := hasAnalysisIntent(tt.msg); got != tt.want {
			t.Errorf("hasAnalysisIntent(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}
