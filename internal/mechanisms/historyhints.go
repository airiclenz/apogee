package mechanisms

import (
	"path"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Wave-3 history-aware hint family (Phase-4 item 11): error_enrichment (post-tool-result),
// read_loop (pre-request), read_repeat + tool_loop_interceptor (post-response), and
// cached_content_intercept (pre-tool-exec) — the cross-turn aggregators ported from the pinned
// apogee-sim source per the catalogue (docs/design/mechanism-catalogue.md Table A/B). Every one
// decides by scanning the conversation across Turns, so it reads the loop's history through the
// LoopView / ConversationView the hook is handed rather than its single mutable value. All ship
// default-off (D1) and self-regulate through the loop's per-Session tracker (strikes-3, item 3);
// none is exempt, so all are disabled under Bypass (D5).
//
// This file carries the helpers the family shares. The read/write tool-name sets and the
// path/error-content sniffers are ported from apogee-sim (internal/toolsets/toolsets.go,
// internal/proxy/{read_loop_detector,error_enrichment,next_step}.go @pin). The read-tool set
// (readToolNames / isReadTool / toolCallPath, offramps.go) already lives in the package and is
// reused here. Write detection has TWO semantics (robustness.go): the history family asks "did this
// call mutate a file / was it a write action" and so uses isFileMutatingTool — the apogee-complete
// superset that also carries apogee's own edit tools; only the content-repair Mechanisms (syntax,
// autofix) use the narrower sim-only isWriteTool.

// listToolNames is apogee-sim's list-tool set (internal/toolsets/toolsets.go ListTools @pin),
// carrying apogee's own list_dir spelling alongside the sim's — the directory-listing calls
// greenfield detection inspects for an empty workspace.
var listToolNames = map[string]bool{
	"list_files": true, "listFiles": true,
	"list_dir": true, "listDir": true, "list_directory": true,
}

// isListTool reports whether name is one of the directory-listing tools greenfield detection reads.
func isListTool(name string) bool { return listToolNames[name] }

// normalizePath canonicalises a file path for cross-Turn comparison (apogee-sim normalizePath
// @pin): a leading "./" is dropped and the path is lexically cleaned, so "./a/b" and "a/b" compare
// equal when the model refers to the same file two different ways. Callers extract a non-empty path
// before calling, so the path.Clean("")==="." corner never arises in practice.
func normalizePath(p string) string {
	return path.Clean(strings.TrimPrefix(p, "./"))
}

// readErrorSignals are the substrings a read-family tool result carries when the read failed
// (apogee-sim isToolResultError, read_loop_detector.go @pin). A committed tool-result Message drops
// the authoritative ToolResult.IsError flag (appendToolResult copies only Content), so a Mechanism
// scanning history for a PRIOR failed read has only the text to go on — the current result still
// uses IsError where the hook receives the live *ToolResult.
var readErrorSignals = []string{"not found", "no such file", "does not exist", "error:"}

// generalErrorSignals are the substrings error_enrichment treats as a failed tool result in history
// (apogee-sim findToolResultError, error_enrichment.go @pin) — a broader set than readErrorSignals
// because a build/runtime failure need not spell "not found".
var generalErrorSignals = []string{"error", "failed", "traceback", "exception", "panic"}

// contentMatchesAny reports whether the lower-cased content contains any of the signals.
func contentMatchesAny(content string, signals []string) bool {
	lower := strings.ToLower(content)
	for _, s := range signals {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// resultIsReadError reports whether the tool result paired with callID reads as a failed read
// (apogee-sim isToolResultError @pin). A missing result is treated as no-error (conservative — a
// call still in flight is not yet a failure).
func resultIsReadError(conv domain.ConversationView, callID string) bool {
	res, _, ok := conv.ResultFor(callID)
	if !ok {
		return false
	}
	return contentMatchesAny(res.Content, readErrorSignals)
}

// firstUserContent returns the first non-empty user message's content (apogee-sim firstUserContent,
// next_step.go @pin) — the prompt deriveWriteTarget and the read-loop hints mine for a target file.
func firstUserContent(conv domain.ConversationView) string {
	var out string
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleUser && strings.TrimSpace(m.Content) != "" {
			out = m.Content
			return false
		}
		return true
	})
	return out
}

// writtenPaths collects the normalized paths the model has successfully issued a write-tool call for
// (apogee-sim writtenPaths, next_step.go @pin) — deriveWriteTarget excludes these so its suggestion
// always points at remaining work.
func writtenPaths(conv domain.ConversationView) map[string]bool {
	out := make(map[string]bool)
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant {
			return true
		}
		for _, tc := range m.ToolCalls {
			if !isFileMutatingTool(tc.Tool) {
				continue
			}
			if p := toolCallPath(tc.Arguments); p != "" {
				out[normalizePath(p)] = true
			}
		}
		return true
	})
	return out
}

// fileExtRe matches a filename token with a recognised source/doc extension (apogee-sim
// fileExtPattern, next_step.go @pin).
var fileExtRe = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9_\-./]*\.(?:js|jsx|ts|tsx|py|go|rs|rb|java|kt|swift|c|cc|cpp|cxx|h|hpp|cs|php|sh|bash|zsh|html|css|scss|md|json|ya?ml|toml)\b`)

// backtickRe captures a filename-like token wrapped in single backticks (apogee-sim backtickPattern,
// next_step.go @pin) — a strong intent signal in a prompt.
var backtickRe = regexp.MustCompile("`([^`\\s]+)`")

// deriveWriteTarget inspects the first user message and returns a single concrete filename the model
// is likely expected to write next (apogee-sim deriveWriteTarget, next_step.go @pin), or "" when
// none can be derived with confidence. A backtick-wrapped filename that survives the written-set
// filter wins; failing that, the first extension-bearing token. Files already written are excluded
// so the suggestion points at remaining work.
func deriveWriteTarget(conv domain.ConversationView) string {
	prompt := firstUserContent(conv)
	if prompt == "" {
		return ""
	}
	written := writtenPaths(conv)

	for _, m := range backtickRe.FindAllStringSubmatch(prompt, -1) {
		cand := strings.TrimSpace(m[1])
		if !fileExtRe.MatchString(cand) {
			continue
		}
		if written[normalizePath(cand)] {
			continue
		}
		return cand
	}
	for _, cand := range fileExtRe.FindAllString(prompt, -1) {
		cand = strings.TrimSpace(cand)
		if written[normalizePath(cand)] {
			continue
		}
		return cand
	}
	return ""
}

// requestContains reports whether any message in the request already carries text — the
// idempotency guard the InjectContext-based hints (read_loop) use so a deterministic hint is never
// injected twice into the same request. The hint text is its own marker (the filehint pattern):
// a re-computed hint for unchanged state is byte-identical, so a substring match recognises it
// without polluting the model-facing wording with a synthetic tag.
func requestContains(conv domain.ConversationView, text string) bool {
	if text == "" {
		return false
	}
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if strings.Contains(m.Content, text) {
			found = true
			return false
		}
		return true
	})
	return found
}

// pathInList reports whether candidate normalises to any path already in paths (apogee-sim
// pathInList @pin) — used to avoid a "create X — also create X" hint when the derived next step is
// one of the warned-about paths.
func pathInList(candidate string, paths []string) bool {
	target := normalizePath(candidate)
	for _, p := range paths {
		if normalizePath(p) == target {
			return true
		}
	}
	return false
}
