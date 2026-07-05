package mechanisms

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// syntax registers the write-content syntax-check Mechanism in the catalogue constructor table
// (Phase-4 item 5). Default-off (D1).
func init() {
	catalogue[syntaxID] = newSyntax
	descriptors[syntaxID] = syntaxDescriptor
}

// syntaxMechanism is the post-response write-content syntax checker (catalogue Table A `syntax`;
// ported from apogee-sim internal/syntax + internal/proxy/response_analysis.go's
// validateWriteSyntax @pin). For each file-writing tool call it detects the language from the
// path and checks the content — Go through the real parser, other languages through a
// bracket/string/truncation heuristic — retrying in place with a correction when the content is
// broken (ActionRetry, the retry-in-place delivery of the amended C5, R1; see robustness.go).
//
// It runs after validate AND after autofix (catalogue Table A): validate has already ruled out a
// malformed call, and autofix has already repaired what a formatter could fix — the sim's cascade
// is detect → tryAutoFix → correct-the-remainder (internal/proxy/response_analysis.go:72-88
// @pin) — so the retry here corrects only the post-repair remainder instead of re-correcting
// issues a formatter had already fixed, while still gating a broken write before the tool runs.
type syntaxMechanism struct{}

// newSyntax builds the syntax Mechanism. It needs no injected Deps (D3): the checks are in-process
// and read only the response.
func newSyntax(Deps) (domain.Mechanism, error) { return syntaxMechanism{}, nil }

// syntaxDescriptor identifies syntax as a strikes-3 response-repair Mechanism (catalogue Table A).
var syntaxDescriptor = domain.MechanismDescriptor{
	ID:          syntaxID,
	Capability:  domain.CapResponseRepair,
	Suppression: domain.SuppressStrikesThree,
}

// Descriptor returns syntax's static catalogue descriptor.
func (syntaxMechanism) Descriptor() domain.MechanismDescriptor { return syntaxDescriptor }

// Ordering runs syntax after validate and after autofix (catalogue Table A): repair precedes
// correction, so the correction covers only the post-repair remainder.
func (syntaxMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{
		After: []domain.MechanismID{validateID, autofixID},
	}
}

// PostResponse checks the syntax of every write tool call's content and, on any error, retries in
// place with a correction — the loop re-streams the corrected request in the same Turn (R1). A
// response with no correctable write content is a no-op.
func (syntaxMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	calls := resp.ToolCalls()
	if len(calls) == 0 {
		return domain.PostResponseDecision{}, nil
	}
	issues := validateWriteSyntax(calls)
	if !hasIssues(issues) {
		return domain.PostResponseDecision{}, nil
	}
	return domain.PostResponseDecision{Action: domain.ActionRetry, Inject: buildCorrectionMessage(issues)}, nil
}

// validateWriteSyntax collects the syntax problems across every write tool call whose content is
// a recognised language (apogee-sim's validateWriteSyntax). A non-write tool, a call with no
// path/content, or an unrecognised extension carries nothing to check and is skipped.
func validateWriteSyntax(calls []domain.ToolCall) []robustnessIssue {
	var issues []robustnessIssue
	for _, call := range calls {
		if !isWriteTool(call.Tool) {
			continue
		}
		path, content, ok := writePathContent(call.Arguments)
		if !ok {
			continue
		}
		if detectLanguage(path) == "" {
			continue
		}
		result := checkSyntax(path, content)
		if result.valid {
			continue
		}
		for _, e := range result.errors {
			issues = append(issues, robustnessIssue{
				message: fmt.Sprintf("syntax error in %s at line %d: %s", path, e.line, e.message),
			})
		}
	}
	return issues
}
