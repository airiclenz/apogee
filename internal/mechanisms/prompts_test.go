package mechanisms

import (
	"strings"
	"testing"
)

// TestPromptAssetsKeepTheirMarkers pins the one coupling the move of the directives into
// prompts/*.txt could otherwise break silently: each idempotency marker stayed a Go const while its
// directive became an embedded asset, so the "the marker is inside the text" invariant now spans two
// files. AppendToSystem suppresses a repeat inject by looking for the marker in the system prompt it
// already built, so a marker that no longer occurs in its directive makes that check miss and the
// directive inject twice.
func TestPromptAssetsKeepTheirMarkers(t *testing.T) {
	cases := []struct {
		name      string
		marker    string
		directive string
	}{
		{"cot_tool_use", cotToolUseMarker, cotToolUseDirective},
		{"cot_stall", cotStallMarker, cotStallDirective},
		{"cot_list_nudge", cotListNudgeMarker, cotListNudgeDirective},
		{"decompose_focus", decomposeFocusMarker, decomposeFocusDirective},
		{"decompose_continuation", decomposeContinuationMarker, decomposeContinuationDirective},
		{"library_injection", libraryInjectionMarker, libraryInjectionHeader},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.directive, tc.marker) {
				t.Errorf("idempotency marker %q is no longer inside its prompt asset %q: a re-worded asset that drops its marker makes AppendToSystem's no-op check miss, so the directive injects twice on the same request",
					tc.marker, tc.directive)
			}
		})
	}
}
