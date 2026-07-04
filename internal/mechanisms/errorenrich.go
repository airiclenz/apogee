package mechanisms

import (
	"context"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// error_enrichment registers the repeated-error clarification Mechanism in the catalogue
// constructor table (Phase-4 item 11, Wave 3 history-aware family). Default-off (D1). It is ported
// from apogee-sim internal/proxy/error_enrichment.go @pin and RELOCATED to post-tool-result
// (catalogue Table A / hook-mutation-api §5): the sim injected its guidance into the next request,
// but apogee owns the loop and can enrich the failing tool result before the model ever sees it.
func init() { catalogue[errorEnrichmentID] = newErrorEnrichment }

const errorEnrichmentID domain.MechanismID = "error_enrichment"

// errorEnrichmentMarker leads the appended guidance (apogee-sim enrichError @pin). It is the
// idempotency marker: a result already carrying it — or an earlier result in history enriched for
// the same file — is not enriched again, so a repeated-error episode gets one hint, not one per
// failure (the item's "≥2 same-file errors → one enriched hint, marker-deduped").
const errorEnrichmentMarker = "This error has occurred multiple times. Suggestions:"

// errorCategory is the coarse classification error_enrichment keys its guidance on (apogee-sim
// errorCategory @pin). It stays Mechanism-internal — it is not a field on domain.ToolResult
// (hook-mutation-api §5: ToolResult.IsError is authoritative; the finer classification is the
// Mechanism's own concern).
type errorCategory string

const (
	errSyntaxError errorCategory = "syntax_error"
	errMissingFile errorCategory = "missing_file"
	errImportError errorCategory = "import_error"
	errTypeError   errorCategory = "type_error"
	errBuildError  errorCategory = "build_error"
	errRuntime     errorCategory = "runtime_error"
	errPermission  errorCategory = "permission_error"
	errUnknown     errorCategory = "unknown"
)

// classifyError maps a tool-result error message to a category (apogee-sim classifyError @pin), in
// the sim's priority order — syntax, import, type, build, permission, runtime, then the
// missing-file fallback, else unknown.
func classifyError(content string) errorCategory {
	lower := strings.ToLower(content)
	switch {
	case containsAny(lower, "syntax error", "syntaxerror", "unexpected token", "parse error", "invalid syntax", "unexpected eof"):
		return errSyntaxError
	case containsAny(lower, "module not found", "cannot find module", "modulenotfounderror", "importerror", "no module named", "unresolved import"):
		return errImportError
	case containsAny(lower, "type error", "typeerror", "cannot assign", "incompatible type", "type mismatch"):
		return errTypeError
	case containsAny(lower, "build failed", "compilation error", "compile error", "does not compile", "build error"):
		return errBuildError
	case containsAny(lower, "permission denied", "eacces", "not permitted", "access denied"):
		return errPermission
	case containsAny(lower, "runtime error", "panic:", "traceback", "exception", "stack trace"):
		return errRuntime
	case containsAny(lower, "not found", "no such file", "does not exist", "enoent"):
		return errMissingFile
	default:
		return errUnknown
	}
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// errorEnrichmentMechanism is the post-tool-result Mechanism that appends category-specific
// guidance to a write-tool error result when the same file has already failed the same way earlier
// this Session (catalogue Table A `error_enrichment`). It carries no per-Mechanism state; strikes-3
// self-regulation routes through the loop's per-Session tracker (item 3). Unlike the sim, apogee
// narrows the exempt set (C1): error_enrichment is a strikes-3 response-repair Mechanism disabled
// under Bypass, not an exempt one.
type errorEnrichmentMechanism struct{}

// newErrorEnrichment builds the error_enrichment Mechanism. It needs no injected Deps (D3): it reads
// only the failing result, its originating call, and the conversation on the LoopView it is handed.
func newErrorEnrichment(Deps) (domain.Mechanism, error) { return errorEnrichmentMechanism{}, nil }

// Descriptor identifies error_enrichment as a strikes-3 response-repair Mechanism (catalogue
// Table A, C1) — disabled under Bypass (ADR 0006 / D5), withdrawn after repeated non-help.
func (errorEnrichmentMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          errorEnrichmentID,
		Capability:  domain.CapResponseRepair,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering declares no constraints (catalogue Table A: "none"): error_enrichment classifies read-
// vs-write from the originating call and stands alone at post-tool-result.
func (errorEnrichmentMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{}
}

// PostToolResult appends the enrichment hint to a write-tool error result when an earlier write to
// the same file failed with the same category this Session (apogee-sim detectRepeatedErrors +
// injectErrorEnrichmentIfNeeded @pin, one enriched error at a time). It is a no-op — booking no
// fire (the loop keys the acted fire on *ToolResult changing, R4) — when the result is not an
// error, the originating call is not a write, the path is unknown, the category is unknown or a
// plain missing-file (the sim skips both), no earlier same-file/same-category failure exists, or an
// earlier result already carried the enrichment for this file.
func (errorEnrichmentMechanism) PostToolResult(_ context.Context, call domain.ToolCall, result *domain.ToolResult, view domain.LoopView) error {
	// The current failure uses the authoritative flag; prior failures in history string-sniff,
	// because the committed tool-result Message no longer carries IsError (hook-mutation-api §5).
	if !result.IsError || !isWriteTool(call.Tool) {
		return nil
	}
	rawPath := toolCallPath(call.Arguments)
	if rawPath == "" {
		return nil
	}
	cat := classifyError(result.Content)
	if cat == errUnknown || cat == errMissingFile {
		return nil
	}

	np := normalizePath(rawPath)
	conv := view.Conversation()
	if !priorWriteErrorMatches(conv, np, call.ID, cat) {
		return nil
	}
	if strings.Contains(result.Content, errorEnrichmentMarker) || alreadyEnriched(conv, np) {
		return nil
	}

	hint := enrichError(enrichedError{Category: cat, ToolName: call.Tool, FilePath: np, OrigError: result.Content})
	result.Content = strings.TrimRight(result.Content, "\n") + "\n\n" + hint
	return nil
}

// priorWriteErrorMatches reports whether an earlier write-tool call to path np (excluding the
// current call) produced a result that classifies to the same category (apogee-sim
// detectRepeatedErrors's "last error matches an earlier one by file+category" @pin). The current
// result is not yet committed to history, so no self-match is possible; the current assistant
// message IS in history, so the pending call is excluded by ID.
func priorWriteErrorMatches(conv domain.ConversationView, np, currentCallID string, cat errorCategory) bool {
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant {
			return true
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == currentCallID || !isWriteTool(tc.Tool) {
				continue
			}
			p := toolCallPath(tc.Arguments)
			if p == "" || normalizePath(p) != np {
				continue
			}
			res, _, ok := conv.ResultFor(tc.ID)
			if !ok || !contentMatchesAny(res.Content, generalErrorSignals) {
				continue
			}
			if classifyError(res.Content) == cat {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// alreadyEnriched reports whether a tool result in history already carries the enrichment marker
// for path np — the cross-Turn dedup that keeps one hint per repeated-error episode.
func alreadyEnriched(conv domain.ConversationView, np string) bool {
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleTool &&
			strings.Contains(m.Content, errorEnrichmentMarker) &&
			strings.Contains(m.Content, np) {
			found = true
			return false
		}
		return true
	})
	return found
}

// enrichedError is one repeated failure error_enrichment renders guidance for.
type enrichedError struct {
	Category  errorCategory
	ToolName  string
	FilePath  string
	OrigError string
}

// enrichError renders the category-specific guidance appended to the failing result (apogee-sim
// enrichError @pin, wording preserved so a ported Mechanism speaks to the model in the phrasing its
// A/B measured). The original error is previewed to 200 chars.
func enrichError(e enrichedError) string {
	preview := e.OrigError
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Your %s call for %s produced an error: %q\n", e.ToolName, e.FilePath, preview)
	b.WriteString(errorEnrichmentMarker + "\n")
	switch e.Category {
	case errSyntaxError:
		fmt.Fprintf(&b, "- Read %s with read_file to see the current state of the file.\n", e.FilePath)
		b.WriteString("- Pay attention to bracket matching and proper indentation.\n")
		b.WriteString("- Ensure all function bodies, string literals, and blocks are properly closed.\n")
	case errImportError:
		b.WriteString("- Check the available files with list_files to find the correct module path.\n")
		b.WriteString("- Verify the import path matches the actual file/package location.\n")
	case errTypeError:
		fmt.Fprintf(&b, "- Read %s with read_file to check existing type definitions and function signatures.\n", e.FilePath)
		b.WriteString("- Ensure your modifications preserve existing types and interfaces.\n")
	case errBuildError:
		fmt.Fprintf(&b, "- Read %s with read_file to understand the existing code structure.\n", e.FilePath)
		b.WriteString("- Ensure your modifications preserve the existing API and function signatures.\n")
		b.WriteString("- Check that all imports are correct and all referenced symbols exist.\n")
	case errPermission:
		b.WriteString("- Check if the file is read-only or if the directory exists.\n")
		b.WriteString("- Try writing to a different location.\n")
	case errRuntime:
		fmt.Fprintf(&b, "- Read %s with read_file to review the code causing the runtime error.\n", e.FilePath)
		b.WriteString("- Check for nil/null pointer access, array bounds, and uninitialized variables.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
