package mechanisms

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// What the Wave-1 response-robustness Mechanisms left behind. All three rows are gone: the
// tool-call validator was PROMOTED to the `tool-call-repair` Floor guard (ADR 0071, its decision
// logic now in internal/floor/repair.go), and syntax and autofix — the write-content syntax check
// and the formatter repair — RETIRED OUTRIGHT in v0.20.0 on the same ratified verdict. This file is
// the remainder the surviving `library` row still reads: the correctable-issue type, the
// tool-call validation helpers library observes through, and the "did this call mutate a file"
// predicate. The correction MESSAGE those issues once rendered into went with the rows that sent it:
// internal/floor/correction.go builds the guards' own.

// robustnessIssue is one problem found in a tool call — the unit library files an observation from.
// context carries the optional supporting lists the sim's message includes (available_tools,
// required_params).
type robustnessIssue struct {
	message string
	context map[string]string
}

// hasIssues reports whether any correctable problem was found — the gate library reads to tell a
// clean call from a corrected one.
func hasIssues(issues []robustnessIssue) bool { return len(issues) > 0 }

// isFileMutatingTool reports whether name mutated a file / was a write action. It is this package's
// ONLY write-detection semantic since the content-repair rows retired (v0.20.0): the narrower
// sim-only isWriteTool — "this call carries a full file payload" — went with syntax and autofix, the
// two Mechanisms that read it. It is the apogee-complete superset: apogee-sim's write-tool set UNION
// apogee's own write tools (edit_existing_file,
// single_find_and_replace, multi_find_and_replace, copy_file, move_file, delete_file; names
// verified against internal/tools, and pinned to its registered menu there), reusing
// wave4WriteTools (historyhints.go) as the single source of that superset. library's narration
// check uses it — as the whole retired history-aware family once did, and as the internal/floor
// guards' own copy of the set still does — so write-since / read-then-write / progress detection
// sees apogee's real edit menu, not just the sim spellings.
func isFileMutatingTool(name string) bool { return wave4WriteTools[name] }

// toolNames lists the tool-menu names for an issue's "available_tools" context. It lived beside the
// tool-call validator until that behaviour was promoted to the tool-call-repair Floor guard
// (ADR 0071), and moved here with its one surviving caller.
func toolNames(tools []domain.ToolDef) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// The tool-call validation helpers. They were the tool-call validator Mechanism's own until that
// behaviour was promoted to the `tool-call-repair` Floor guard (ADR 0071, internal/floor/repair.go);
// they stay here because the library Mechanism observes the SAME issues to record its corrections,
// and a second reading of a call's problems would be a second answer to one question.

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
