package floor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ToolCallRepair is the tool-call repair guard (the `tool-call-repair` key, ADR 0071): it checks
// each requested tool call against the tool menu the model was shown (LoopView.Tools()) and against
// its own arguments — an unknown tool name, empty or malformed JSON arguments, or a missing required
// parameter — and hands back the correction the engine re-streams the Turn with. ok is false for a
// response with no tool calls, or one whose calls are all well formed: the no-op case, where the
// response stands exactly as the model wrote it.
//
// registered is every tool name the ENGINE holds, whatever the current menu shows, and it is what
// separates a hallucinated tool from a WITHDRAWN one. A menu is not the whole tool set: Plan mode
// offers only the tools Plan can run, and a delegate's wrap-up Turn is shown no tools at all — so a
// call naming a registered tool that is off the menu is not a malformed call at all, it is a call
// the MODE answers, with the refusal that says why (the Plan refusal, the wrap-up drop). Pre-empting
// that with a correction retry spends a retry, replaces the reason the user's own mode gave with a
// generic "not in the tool set", and leaves the model no wiser about the mode it is in — worse than
// no guard, which a Floor guard may never be (ADR 0071 decision 1). Such a call is therefore left
// ALONE, arguments included: the engine below has the better answer and it must reach the model.
// A call naming a tool the engine does not have either is the hallucination the guard is for, and
// is corrected as before — as is every remaining call in the same response, so one withdrawn call
// never shields a genuinely malformed sibling.
//
// The decision logic is otherwise apogee-sim's response validator @pin, unchanged by the promotion:
// the guard changes only what the model sees after its OWN failure, which is why it needs no
// per-model proof and stays on under Bypass. It reads nothing but resp and registered — no clock, no
// filesystem, no state between calls — so the same response always yields the same answer.
func ToolCallRepair(resp *domain.Response, registered []string) (correction string, ok bool) {
	calls := resp.ToolCalls()
	if len(calls) == 0 {
		return "", false
	}
	menu := resp.View().Tools()
	checked := callsTheGuardOwns(calls, menu, registered)
	if len(checked) == 0 {
		return "", false
	}
	issues := validateToolCalls(checked, menu)
	if !hasIssues(issues) {
		return "", false
	}
	return buildCorrectionMessage(issues), true
}

// callsTheGuardOwns drops the calls that belong to the mode rather than to the guard — the ones
// naming a registered tool the current menu withdrew — and returns the rest for validation.
func callsTheGuardOwns(calls []domain.ToolCall, menu []domain.ToolDef, registered []string) []domain.ToolCall {
	owned := make([]domain.ToolCall, 0, len(calls))
	for _, call := range calls {
		if !withdrawnFromMenu(call.Tool, menu, registered) {
			owned = append(owned, call)
		}
	}
	return owned
}

// withdrawnFromMenu reports whether name is a tool the engine has but this request's menu did not
// offer. A name the menu carries is on offer, and a name the engine does not have at all is a
// hallucination, not a withdrawal — both are the guard's own business.
func withdrawnFromMenu(name string, menu []domain.ToolDef, registered []string) bool {
	if name == "" || toolKnown(name, menu) {
		return false
	}
	for _, r := range registered {
		if r == name {
			return true
		}
	}
	return false
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
