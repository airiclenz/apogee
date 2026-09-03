package mechanisms

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Wave-3 history-aware hint family (Phase-4 item 11): error_enrichment (post-tool-result),
// read_loop (pre-request) and read_repeat (post-response) — the family's other two members, the
// identical-repeat detector and the redundant-re-read interceptor, left the catalogue when they were
// promoted to the tool-loop-breaker and read-cache Floor guards (ADR 0071) — the cross-turn aggregators ported from the pinned
// apogee-sim source per the catalogue (docs/design/mechanism-catalogue.md Table A/B). Every one
// decides by scanning the conversation across Turns, so it reads the loop's history through the
// LoopView / ConversationView the hook is handed rather than its single mutable value. All ship
// default-off (D1) and self-regulate through the loop's per-Session tracker (strikes-3, item 3);
// none is exempt, so all are disabled under Bypass (D5).
//
// This file carries the helpers the family shares. The read/write tool-name sets and the
// path/error-content sniffers are ported from apogee-sim (internal/toolsets/toolsets.go,
// internal/proxy/{read_loop_detector,error_enrichment,next_step}.go @pin). The read-tool set
// (readToolNames / isReadTool / toolCallPath) lives here, having outlived the two recovery rows
// that first carried it — both are Floor guards now (ADR 0071), and internal/floor keeps its own
// copy. Write detection has TWO semantics (robustness.go): the history family asks "did this
// call mutate a file / was it a write action" and so uses isFileMutatingTool — the apogee-complete
// superset that also carries apogee's own edit tools; only the content-repair Mechanisms (syntax,
// autofix) use the narrower sim-only isWriteTool.

// listToolNames are the directory-listing calls greenfield detection inspects for an empty workspace.
// It composes from the list spelling family (listSpellings, decompose.go) — apogee-sim's ListTools
// @pin plus apogee's own list_directory — the complete five-spelling family the other list sets now
// consolidate onto.
var listToolNames = toolSet(listSpellings)

// isListTool reports whether name is one of the directory-listing tools greenfield detection reads.
func isListTool(name string) bool { return listToolNames[name] }

// readToolNames are the tools whose calls count as a file read across the history family. It
// composes from the read spelling family (readSpellings, decompose.go) — apogee-sim's ReadTools
// @pin plus the retired open_file spelling, a separate read tool until it merged into read_file on
// 2026-08-11, kept because models may still emit the name — so the cot/filehint/library read sets
// that share the family stay identical by construction rather than by hand-maintained copies.
var readToolNames = toolSet(readSpellings)

// isReadTool reports whether name is one of the file-reading tools progress detection counts.
func isReadTool(name string) bool { return readToolNames[name] }

// toolCallPath extracts the file path a tool call targets, matching apogee-sim's
// toolsets.ExtractPath @pin (path / file_path / filePath / filename) plus destination — the second
// half of the source/destination pair copy_file and move_file carry (internal/tools/file_ops.go).
// "" when the arguments are not a JSON object or carry no path key — the "no path to count" case
// progress detection skips.
//
// destination is read LAST, so a call carrying one of the four original spellings alongside it
// keeps today's precedence. Reporting the destination — the file the write landed on — matches
// deriveWriteTarget's semantics; the accepted limit is that a move's vacated SOURCE stays invisible
// to the path-keyed family, cache invalidation included (destination-only, owner call 2026-08-22).
func toolCallPath(args json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "filePath", "filename", "destination"} {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// normalizePath canonicalises a file path for cross-Turn comparison (apogee-sim normalizePath
// @pin): a leading "./" is dropped and the path is lexically cleaned, so "./a/b" and "a/b" compare
// equal when the model refers to the same file two different ways. Callers extract a non-empty path
// before calling, so the path.Clean("")==="." corner never arises in practice.
func normalizePath(p string) string {
	return path.Clean(strings.TrimPrefix(p, "./"))
}

// readErrorSignals are the substrings a FAILED read-family tool result leads with (apogee-sim
// isToolResultError, read_loop_detector.go @pin). They are the legacy fallback only: a committed
// tool-result Message now carries the authoritative flag as domain.Message.ToolOutcome (stamped by
// appendToolResult), and resultIsReadError consults the text solely for a record snapshotted before
// that marker existed.
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

// resultIsReadError reports whether the tool result paired with callID is a failed read
// (apogee-sim isToolResultError @pin). A missing result is treated as no-error (conservative — a
// call still in flight is not yet a failure).
//
// The committed marker decides whenever there is one: it is the tool's own IsError verdict, and no
// amount of text sniffing can improve on it. Sniffing the whole body is in fact actively wrong —
// a successful read_file's content is the FILE, so any source file mentioning "does not exist" or
// containing an `error:` string classified as a failed read, which is how read_loop came to tell
// the model that files it had just read successfully did not exist and should be written from
// scratch. The sniff survives only for a record snapshotted before the marker existed, and is
// anchored to the result's first line, where a tool's failure message actually is.
func resultIsReadError(conv domain.ConversationView, callID string) bool {
	res, _, ok := conv.ResultFor(callID)
	if !ok {
		return false
	}
	switch res.ToolOutcome {
	case domain.ToolOutcomeFailed:
		return true
	case domain.ToolOutcomeSucceeded:
		return false
	}
	return firstLineMatchesAny(res.Content, readErrorSignals)
}

// firstLineMatchesAny reports whether the first non-blank line of content contains any of the
// signals — the anchored form of contentMatchesAny. A tool that failed says so up front (the
// built-in read tools' whole result is that one sentence); a tool that succeeded may say anything
// at all further down, because further down is the payload.
func firstLineMatchesAny(content string, signals []string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return contentMatchesAny(line, signals)
	}
	return false
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
// always points at remaining work. A thin delegate to the shared written-paths shape
// (writtenPathsSince, historyscan.go) over the whole conversation and the write superset.
func writtenPaths(conv domain.ConversationView) map[string]bool {
	return writtenPathsSince(conv, wave4WriteTools, 0)
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
// idempotency guard the InjectContext-based hints (read_loop, filehint) use so a deterministic
// hint is never injected twice into the same request. The hint text is its own marker: a
// re-computed hint for unchanged state is byte-identical (filehint anchors on its stable
// fileHintMarker lead), so a substring match recognises it without polluting the model-facing
// wording with a synthetic tag.
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
