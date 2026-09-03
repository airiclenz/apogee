package floor

import (
	"strings"
	"testing"
)

// hasIssues is the gate between a correction retry and a no-op.
func TestHasIssues(t *testing.T) {
	t.Parallel()

	if hasIssues(nil) {
		t.Error("hasIssues(nil) = true, want false")
	}
	if !hasIssues([]robustnessIssue{{message: "unknown tool"}}) {
		t.Error("hasIssues(one issue) = false, want true")
	}
}

// The correction's wording is the one the A/B measured, so its shape is pinned: the fixed opening,
// one bullet per issue, the optional supporting lists indented under their bullet, and the closing
// example of a valid call.
func TestBuildCorrectionMessage(t *testing.T) {
	t.Parallel()

	got := buildCorrectionMessage([]robustnessIssue{
		{
			message: `unknown tool "reed_file"`,
			context: map[string]string{"available_tools": "read_file, write_file"},
		},
		{
			message: `missing arguments for "write_file"`,
			context: map[string]string{"required_params": "path, content"},
		},
	})

	want := strings.Join([]string{
		"Your previous tool call had errors. Please fix and try again:",
		`- unknown tool "reed_file"`,
		"  Available tools: read_file, write_file",
		`- missing arguments for "write_file"`,
		"  Required parameters: path, content",
		`Produce a valid tool call with correct JSON arguments, e.g.: {"param": "value"}`,
	}, "\n")

	if got != want {
		t.Errorf("buildCorrectionMessage =\n%s\n\nwant\n%s", got, want)
	}
}
