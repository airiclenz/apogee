package mechanisms

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Wave-3 history-aware hint family (Phase-4 item 11): error_enrichment (post-tool-result) and
// read_repeat (post-response) — the family's other members left it, the identical-repeat detector
// and the redundant-re-read interceptor promoted to the tool-loop-breaker and read-cache Floor
// guards (ADR 0071), read_loop retired outright in v0.20.0 on the same verdict — the cross-turn
// aggregators ported from the pinned apogee-sim source per the catalogue
// (docs/design/mechanism-catalogue.md Table A/B). Every one
// decides by scanning the conversation across Turns, so it reads the loop's history through the
// LoopView / ConversationView the hook is handed rather than its single mutable value. All ship
// default-off (D1) and self-regulate through the loop's per-Session tracker (strikes-3, item 3);
// none is exempt, so all are disabled under Bypass (D5).
//
// This file carries the helpers the family shares. The read/write tool-name sets and the
// path/error-content sniffers are ported from apogee-sim (internal/toolsets/toolsets.go,
// internal/proxy/{read_loop_detector,error_enrichment}.go @pin). The read-tool set
// (readToolNames / isReadTool / toolCallPath) lives here, having outlived the two recovery rows
// that first carried it — both are Floor guards now (ADR 0071), and internal/floor keeps its own
// copy. Write detection has TWO semantics (robustness.go): the history family asks "did this
// call mutate a file / was it a write action" and so uses isFileMutatingTool — the apogee-complete
// superset that also carries apogee's own edit tools; only the content-repair Mechanisms (syntax,
// autofix) use the narrower sim-only isWriteTool.

// wave4WriteTools is the write-tool set the history family inspects for "has the model written a
// file yet" — apogee-sim toolsets.WriteTools @pin extended with apogee's own write-tool spellings
// (edit_existing_file / single_&_multi_find_and_replace, joined 2026-08-10 by the file-operation
// trio copy_file / move_file / delete_file), so the scans fire on apogee's real menu (apogee's
// read_file / list_dir / grep already appear in the sim's read/list/search sets, so only the write
// set needs the apogee names added). It is the apogee-complete file-mutation superset and doubles
// as the single source for isFileMutatingTool (robustness.go, semantic (b) of write detection —
// S1, 2026-07-04): both point at this one set.
//
// Its apogee half is exactly internal/tools' workspaceScopedWriter set — the built-ins whose
// execution mutates a NAMED workspace file. TestWave4WriteToolsCoversEveryWorkspaceWritingBuiltin
// (writedetection_test.go) pins that correspondence against the registered menu, so a write tool
// added to internal/tools can no longer land here as a silent non-write. The sim spellings above it
// are additive: they name no registered apogee tool and the pin never asks them to.
var wave4WriteTools = map[string]bool{
	"write_file": true, "writeFile": true, "write_to_file": true, "create_file": true,
	"edit_file": true, "editFile": true, "replace_in_file": true,
	"edit_existing_file": true, "single_find_and_replace": true, "multi_find_and_replace": true,
	"copy_file": true, "move_file": true, "delete_file": true,
}

// readSpellings and listSpellings are the read- and list-tool SPELLING families: each lists one tool
// concept written every way apogee's real menu can present it, so a newly-supported spelling is added
// in ONE place and every set composed from it inherits the addition (F8, post-v1.3.0 review). They are
// the read/list counterparts of wave4WriteTools above — the write side's single source. The families
// carry SPELLINGS only: WHICH concepts a given set treats as a read or a list stays that set's own
// documented membership (the sets serve different purposes and several carry @pin rationales), so each
// set below composes from a family via toolSet and adds its own local spellings rather than
// hand-copying the family's — the drift class this consolidation closes.
var (
	// readSpellings is apogee-sim's read-tool set (toolsets.ReadTools @pin: read_file / readFile) plus
	// apogee's own open_file spelling — read_file.go renderFile is read-only and returns the file
	// body, so it counts as a file read wherever a read set is consulted. "open_file" is a RETIRED
	// tool name (merged into read_file on 2026-08-11) kept as a spelling a model may still emit,
	// exactly like "readFile", which was never a registered tool either.
	readSpellings = []string{"read_file", "readFile", "open_file"}

	// listSpellings is apogee-sim's list-tool set (toolsets.ListTools @pin: list_files / listFiles /
	// list_dir / listDir) plus apogee's own list_directory spelling, so a directory listing counts
	// however the menu spells it. The four gap fixes (F8) are exactly the sets below that had been
	// hand-maintained short of this complete family.
	listSpellings = []string{"list_files", "listFiles", "list_dir", "listDir", "list_directory"}
)

// toolSet unions one or more spelling groups — the families above and/or a set's own local spellings —
// into a name→true membership set. It is the composition seam F8 introduces: a set names the families
// it draws from instead of copying their spellings, so a family addition reaches every set at once.
func toolSet(groups ...[]string) map[string]bool {
	set := make(map[string]bool)
	for _, g := range groups {
		for _, name := range g {
			set[name] = true
		}
	}
	return set
}

// readToolNames are the tools whose calls count as a file read across the history family. It
// composes from the read spelling family (readSpellings, above) — apogee-sim's ReadTools
// @pin plus the retired open_file spelling, a separate read tool until it merged into read_file on
// 2026-08-11, kept because models may still emit the name — so the library read set that shares the
// family stays identical by construction rather than by a hand-maintained copy.
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
// next_step.go @pin) — the task statement a hint quotes back so its correction names what the model
// was asked to do.
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
