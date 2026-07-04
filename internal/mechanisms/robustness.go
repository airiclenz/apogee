package mechanisms

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Wave-1 response-robustness Mechanisms (Phase-4 item 5): validate, syntax, and autofix,
// ported from the pinned apogee-sim source (docs/design/mechanism-catalogue.md Table A). All
// three are post-response Mechanisms with Capability response-repair and SuppressionPolicy
// strikes-3, dispatched in the deterministic order validate → autofix → syntax (repair precedes
// correction — sim internal/proxy/response_analysis.go:72-88 @pin; see autofix.go).
//
// Correction delivery is by ActionRetry — retry-in-place per the amended C5 (R1, owner-ratified
// 2026-07-04; docs/plans/phase-4-review-fixes-plan.md). The loop, unlike the sim's proxy, owns
// the stream and can reset it (StreamResetEvent), so validate/syntax return ActionRetry{Inject:
// correction} and the loop re-streams the corrected request in the SAME Turn: the superseded
// assistant message (text + tool calls) and then the correction as a role-safe user message are
// appended to the in-flight request — request-scoped, never committed to history — exactly the
// exchange the sim's retryWithCorrection built (internal/proxy/response_validator.go @pin). The
// retry short-circuits the remaining post-response cascade and is bounded by the loop's
// maxPostResponseRetries; at the cap the last response passes through. C5's substance stands:
// feed_forward_correction stays folded — no standalone Mechanism — and ActionDefer keeps its
// next-request semantics, but Wave 1 no longer uses it. autofix repairs in place: the loop
// dispatches a Response's tool calls only after post-response review, so a formatter write-back
// via Response.SetToolCallArguments reaches the tool that runs.
const (
	validateID domain.MechanismID = "validate"
	syntaxID   domain.MechanismID = "syntax"
	autofixID  domain.MechanismID = "autofix"
)

// robustnessIssue is one problem validate or syntax found in a tool call — the correctable unit
// buildCorrectionMessage renders into the model-facing retry correction. context carries
// the optional supporting lists the sim's message includes (available_tools, required_params).
type robustnessIssue struct {
	message string
	context map[string]string
}

// hasIssues reports whether any correctable problem was found — the gate validate/syntax use to
// decide between an ActionRetry correction and a no-op.
func hasIssues(issues []robustnessIssue) bool { return len(issues) > 0 }

// buildCorrectionMessage renders the model-facing retry correction from the issues found,
// ported verbatim from apogee-sim's buildCorrectionMessage (internal/proxy/response_validator.go
// @pin) so a ported Mechanism speaks to the model in the wording its A/B measured.
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

// writeToolNames is apogee-sim's write-tool set (internal/toolsets/toolsets.go @pin) and backs
// semantic (a) of this package's TWO write-detection semantics: CONTENT REPAIR — "this call carries
// a full file payload whose content can be syntax-checked/formatted" (syntax.go, autofix.go). A tool
// outside this set is not a full-file write, so it carries no path/content to check or format. It is
// deliberately NOT extended with apogee's own edit tools: their payloads are old/new-string fragments
// or patches, not files, so syntax/autofix must never act on them (S1, 2026-07-04). Semantic (b) —
// "this call mutated a file / was a write action" — is isFileMutatingTool below, the apogee-complete
// superset the history family uses.
var writeToolNames = map[string]bool{
	"write_file":      true,
	"writeFile":       true,
	"write_to_file":   true,
	"create_file":     true,
	"edit_file":       true,
	"editFile":        true,
	"replace_in_file": true,
}

// isWriteTool reports whether name is one of the sim's full-file-writing tools whose content syntax
// and autofix inspect — semantic (a), content-repair-only (see writeToolNames). It is NOT the "did
// this call mutate a file" predicate; that is isFileMutatingTool.
func isWriteTool(name string) bool { return writeToolNames[name] }

// isFileMutatingTool reports whether name mutated a file / was a write action — semantic (b) of this
// package's two write-detection semantics (see writeToolNames for (a)). It is the apogee-complete
// superset: apogee-sim's writeToolNames UNION apogee's own edit tools (edit_existing_file,
// single_find_and_replace, multi_find_and_replace; names verified against internal/tools), reusing
// wave4WriteTools (decompose.go) as the single source of that superset. The history-family
// Mechanisms — read_repeat, read_loop, cached_content_intercept, error_enrichment,
// tool_loop_interceptor, the off-ramps, and deriveWriteTarget — use it so their write-since /
// read-then-write / progress detection sees apogee's real edit menu, not just the sim spellings.
func isFileMutatingTool(name string) bool { return wave4WriteTools[name] }

// writePathContent extracts the file path and content a write tool call carries, matching the
// sim's arg-shape handling (path or file_path; content). ok is false when the arguments are not a
// JSON object or either field is absent/empty — the "nothing to check" case syntax/autofix skip.
func writePathContent(args json.RawMessage) (path, content string, ok bool) {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return "", "", false
	}
	path, _ = m["path"].(string)
	if path == "" {
		path, _ = m["file_path"].(string)
	}
	content, _ = m["content"].(string)
	if path == "" || content == "" {
		return "", "", false
	}
	return path, content, true
}

// replaceContentArg returns args with its "content" field replaced by content — how autofix folds
// the formatted payload back into a tool call's arguments before Response.SetToolCallArguments
// writes it to the call the loop will dispatch. It preserves the call's other arguments.
func replaceContentArg(args json.RawMessage, content string) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return nil, err
	}
	m["content"] = content
	return json.Marshal(m)
}
