package mechanisms

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// syntax registers the write-content syntax-check Mechanism in the catalogue constructor table
// (Phase-4 item 5). Default-off (D1).
func init() { catalogue[syntaxID] = newSyntax }

// syntaxMechanism is the post-response write-content syntax checker (catalogue Table A `syntax`;
// ported from apogee-sim internal/syntax + internal/proxy/response_analysis.go's
// validateWriteSyntax @pin). For each file-writing tool call it detects the language from the
// path and checks the content — Go through the real parser, other languages through a
// bracket/string/truncation heuristic — deferring a correction when the content is broken.
//
// It runs after validate and before autofix (catalogue Table A): validate has already ruled out a
// malformed call, and autofix's in-place formatting comes last. Broken content that reaches
// autofix cannot be formatted (a formatter needs parseable input), so gating it here — before the
// tool runs — is what keeps a syntactically-broken write from being dispatched unchallenged.
type syntaxMechanism struct{}

// newSyntax builds the syntax Mechanism. It needs no injected Deps (D3): the checks are in-process
// and read only the response.
func newSyntax(Deps) (domain.Mechanism, error) { return syntaxMechanism{}, nil }

// Descriptor identifies syntax as a strikes-3 response-repair Mechanism (catalogue Table A).
func (syntaxMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          syntaxID,
		Capability:  domain.CapResponseRepair,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering runs syntax after validate and before autofix (catalogue Table A).
func (syntaxMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{
		After:  []domain.MechanismID{validateID},
		Before: []domain.MechanismID{autofixID},
	}
}

// PostResponse checks the syntax of every write tool call's content and, on any error, defers a
// correction into the next request. A response with no correctable write content is a no-op.
func (syntaxMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	calls := resp.ToolCalls()
	if len(calls) == 0 {
		return domain.PostResponseDecision{}, nil
	}
	issues := validateWriteSyntax(calls)
	if !hasIssues(issues) {
		return domain.PostResponseDecision{}, nil
	}
	return domain.PostResponseDecision{Action: domain.ActionDefer, Inject: buildCorrectionMessage(issues)}, nil
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
