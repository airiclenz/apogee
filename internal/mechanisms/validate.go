package mechanisms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// validate registers the tool-call validation Mechanism in the catalogue constructor table
// (Phase-4 item 5). It is default-off (D1) — the config surface builds it only when the
// `mechanisms:` block enables it.
func init() { catalogue[validateID] = newValidate }

// validateMechanism is the post-response tool-call validator (catalogue Table A `validate`;
// ported from apogee-sim internal/validate + internal/proxy/response_validator.go @pin). It
// checks each requested tool call against the tool menu the model was shown (LoopView.Tools())
// and its own arguments: an unknown tool name, empty/malformed JSON arguments, or a missing
// required parameter each yield a correction the loop re-streams in the same Turn (ActionRetry
// — the retry-in-place delivery of the amended C5, R1; see robustness.go). The retry
// short-circuits the rest of the response-repair cascade, so a malformed call is corrected
// before the finer content passes ever see it.
//
// It carries no per-Mechanism state: the descriptor's strikes-3 policy routes its
// self-regulation through the loop's per-Session tracker (item 3), the same as every catalogued
// Mechanism, so the sim's ad-hoc syntax-fail counter is not re-implemented here.
type validateMechanism struct{}

// newValidate builds the validate Mechanism. It needs no injected Deps (D3): validation reads
// only the response and the tool menu already on its LoopView.
func newValidate(Deps) (domain.Mechanism, error) { return validateMechanism{}, nil }

// Descriptor identifies validate as a strikes-3 response-repair Mechanism (catalogue Table A) —
// disabled under Bypass (ADR 0006) and withdrawn by self-regulation after repeated non-help.
func (validateMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          validateID,
		Capability:  domain.CapResponseRepair,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering runs validate before syntax and autofix (catalogue Table A): validation is the
// coarsest check, so a malformed call is corrected before the finer content passes look at it.
func (validateMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{Before: []domain.MechanismID{syntaxID, autofixID}}
}

// PostResponse validates the response's tool calls and, on any error, retries in place with a
// correction — the loop re-streams the corrected request in the same Turn (R1). A response with
// no tool calls, or with only valid calls, is a no-op.
func (validateMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	calls := resp.ToolCalls()
	if len(calls) == 0 {
		return domain.PostResponseDecision{}, nil
	}
	issues := validateToolCalls(calls, resp.View().Tools())
	if !hasIssues(issues) {
		return domain.PostResponseDecision{}, nil
	}
	return domain.PostResponseDecision{Action: domain.ActionRetry, Inject: buildCorrectionMessage(issues)}, nil
}

// validateToolCalls collects the validation problems across every requested call (apogee-sim's
// validate.ValidateToolCalls, adapted to already-parsed domain.ToolCalls — the wire-level id/type
// checks the sim did are unnecessary once the loop has parsed the call).
func validateToolCalls(calls []domain.ToolCall, tools []domain.ToolDef) []robustnessIssue {
	var issues []robustnessIssue
	for _, call := range calls {
		issues = append(issues, validateCall(call, tools)...)
	}
	return issues
}

// validateCall checks one call: a present function name, membership in the tool menu, and valid
// arguments. A missing name short-circuits the rest (there is nothing left to check).
func validateCall(call domain.ToolCall, tools []domain.ToolDef) []robustnessIssue {
	if call.Tool == "" {
		return []robustnessIssue{{message: "tool call missing function name"}}
	}

	var issues []robustnessIssue
	if len(tools) > 0 && !toolKnown(call.Tool, tools) {
		issues = append(issues, robustnessIssue{
			message: fmt.Sprintf("function %q not in the tool set provided to the model", call.Tool),
			context: map[string]string{"available_tools": strings.Join(toolNames(tools), ", ")},
		})
	}
	return append(issues, validateArguments(call, tools)...)
}

// validateArguments checks a call's arguments are a JSON object and carry every required
// parameter the tool's schema declares. Empty or non-object arguments are the malformed-call case.
func validateArguments(call domain.ToolCall, tools []domain.ToolDef) []robustnessIssue {
	raw := strings.TrimSpace(string(call.Arguments))
	if raw == "" {
		return []robustnessIssue{{message: "tool call has empty arguments (expected JSON object)"}}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return []robustnessIssue{{message: fmt.Sprintf("arguments are not valid JSON: %s", err.Error())}}
	}

	required := requiredParams(call.Tool, tools)
	var issues []robustnessIssue
	for _, req := range required {
		if _, ok := parsed[req]; !ok {
			issues = append(issues, robustnessIssue{
				message: fmt.Sprintf("missing required parameter %q for function %q", req, call.Tool),
				context: map[string]string{"required_params": strings.Join(required, ", ")},
			})
		}
	}
	return issues
}

// toolKnown reports whether name is in the tool menu the model was shown.
func toolKnown(name string, tools []domain.ToolDef) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// toolNames lists the tool-menu names, for the "Available tools: …" line of an unknown-tool
// correction.
func toolNames(tools []domain.ToolDef) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// requiredParams reads the "required" list from a tool's JSON-schema arguments (ToolDef.Schema),
// mirroring apogee-sim's ExtractToolDefsFromPipeline. A tool absent from the menu, or a schema
// without a required array, yields no required parameters (nothing to enforce).
func requiredParams(name string, tools []domain.ToolDef) []string {
	for _, t := range tools {
		if t.Name != name {
			continue
		}
		if len(t.Schema) == 0 {
			return nil
		}
		var s struct {
			Required []string `json:"required"`
		}
		if json.Unmarshal(t.Schema, &s) == nil {
			return s.Required
		}
		return nil
	}
	return nil
}
