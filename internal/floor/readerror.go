package floor

import (
	"path"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// Read-result inspection: was the result paired with a call a FAILED read, and does a path the
// model spelled two different ways name the same file. Both questions are asked by the guards that
// reason across Turns (the read cache above all), and both are answered here once.

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
// containing an `error:` string classified as a failed read, which is how the read family came to
// tell the model that files it had just read successfully did not exist and should be written from
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
