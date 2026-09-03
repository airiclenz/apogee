package floor

import (
	"fmt"
	"strings"
)

// The model-facing correction a repair guard hands back with its retry. The wording is the one
// apogee-sim's A/B measured (internal/proxy/response_validator.go @pin) and is kept verbatim: a
// guard that changes what the model reads changes what the measurement said.

// robustnessIssue is one problem a repair guard found in a tool call — the correctable unit
// buildCorrectionMessage renders into the retry correction. context carries the optional supporting
// lists the message includes (available_tools, required_params).
type robustnessIssue struct {
	message string
	context map[string]string
}

// hasIssues reports whether any correctable problem was found — the gate a repair guard uses to
// decide between a correction retry and a no-op.
func hasIssues(issues []robustnessIssue) bool { return len(issues) > 0 }

// buildCorrectionMessage renders the model-facing retry correction from the issues found, ported
// verbatim from apogee-sim's buildCorrectionMessage (internal/proxy/response_validator.go @pin) so
// the guard speaks to the model in the wording its A/B measured.
func buildCorrectionMessage(issues []robustnessIssue) string {
	var b strings.Builder
	b.WriteString("Your previous tool call had errors. Please fix and try again:\n")
	for _, issue := range issues {
		fmt.Fprintf(&b, "- %s\n", issue.message)
		if tools, ok := issue.context["available_tools"]; ok {
			fmt.Fprintf(&b, "  Available tools: %s\n", tools)
		}
		if params, ok := issue.context["required_params"]; ok {
			fmt.Fprintf(&b, "  Required parameters: %s\n", params)
		}
	}
	b.WriteString("Produce a valid tool call with correct JSON arguments, e.g.: {\"param\": \"value\"}")
	return b.String()
}
