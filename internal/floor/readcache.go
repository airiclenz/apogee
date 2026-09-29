package floor

import (
	"encoding/json"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
)

// readCacheLines is the max_lines a redundant re-read is capped to — the file's content is already
// in the conversation, so a header-only slice loses nothing while reclaiming the window the full
// re-dump would have cost.
const readCacheLines = 1

// readSliceKeys are the read arguments that bound what comes back to a slice of the file — an
// explicit line range or line limit. capReadArguments leaves a pending call carrying one untouched,
// and priorSuccessfulReadUnchanged refuses a prior read carrying one as the cached copy: the ONE
// list both ask, so a new slicing argument is added in one place.
var readSliceKeys = []string{"start_line", "end_line", "max_lines"}

// CacheRead is the read cache guard (the `read-cache` key, ADR 0071): a read of a file this
// conversation already read in full successfully — no line range, limit or locate, its result not
// since pruned to a stub, and with no write to the file, and no call to any non-read-only tool,
// since — is capped to a header-only slice by appending max_lines to its arguments. The
// model's existing copy stays the source of truth, and the window the re-dump would have cost is
// reclaimed.
//
// It reports whether it capped anything. ok is false — and the pending call is left byte-identical —
// when the call is not a read, carries no path, targets a file not read successfully before (or
// changed since: written by name, or followed by a non-read-only call such as a shell command that
// may have rewritten it unnamed, so the copy may be stale; or read only in slices, or whose full read
// was pruned, so no copy is in the conversation), already asks for an explicit line range
// or limit (a targeted read is not a redundant full re-dump), has arguments that are not a JSON
// object, or names a tool whose schema does not declare max_lines. That last gate is what makes the
// no-op literal rather than hoped-for: a strict MCP server (additionalProperties:false) rejects an
// argument it never declared, so an undeclared field is never appended and the re-read simply
// proceeds uncapped.
//
// The decision logic is apogee-sim's detectCachedReread @pin, unchanged by the promotion save for
// the change-since check apogee has always added: the guard shapes a request without steering it, so
// it needs no per-model proof and stays on under Bypass. It reads nothing but the pending call and
// the conversation and tool menu the view exposes — no clock, no filesystem, no state between calls.
func CacheRead(view domain.LoopView, edit *domain.ToolCallEdit) (ok bool) {
	if !isReadTool(edit.Tool()) {
		return false
	}
	args := edit.Arguments()
	rawPath := toolCallPath(args)
	if rawPath == "" {
		return false
	}
	if !priorSuccessfulReadUnchanged(view.Conversation(), view.Tools(), normalizePath(rawPath), edit.ID()) {
		return false
	}
	if !toolDeclaresMaxLines(view.Tools(), edit.Tool()) {
		return false
	}
	capped, capOK := capReadArguments(args)
	if !capOK {
		return false
	}
	edit.SetArguments(capped)
	return true
}

// priorSuccessfulReadUnchanged reports whether path np was read in full, successfully, in an earlier
// Turn, that copy is still in the conversation, and nothing since could have changed the file
// (apogee-sim detectCachedReread's "earlier successful read of the same path" @pin, strengthened to
// honour "unchanged": the sim omitted the write-since check, but capping a file modified after the
// earlier read would drop real content). Two kinds of later call void the cached copy: a write-tool
// call naming np, and — whatever path it names, or none — any call that invalidatesReadCache,
// because a shell line, an interpreter or an MCP server can rewrite np without ever naming it. A call
// in the same assistant message as the read counts as after it. The pending call (currentCallID) is
// excluded — its own assistant message is already committed to history when the pre-tool-exec seam
// runs.
//
// Only the EARLIEST full read since the last change stands as the copy, and only while its result is
// unpruned. A read carrying any readSliceKeys key or a locate returned a slice or windows, never the
// file, so it is skipped. The earliest is the one to judge because a later bare read may be one this
// guard capped to a header: prepareCall caps a local copy of the call, so history still holds the
// bare arguments, and only an earlier full read can vouch for it. That earliest read being pruned to
// a stub means the file is no longer in the conversation, and the re-read must go through whole —
// the guard never leaves the model worse off than the bare loop would.
func priorSuccessfulReadUnchanged(conv domain.ConversationView, tools []domain.ToolDef, np, currentCallID string) bool {
	copyCallID := ""
	for i := 0; i < conv.Len(); i++ {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		changed := false
		firstFullRead := ""
		for _, tc := range m.ToolCalls {
			if tc.ID == currentCallID {
				continue
			}
			if invalidatesReadCache(tools, tc.Tool) {
				changed = true
			}
			p := toolCallPath(tc.Arguments)
			if p == "" || normalizePath(p) != np {
				continue
			}
			switch {
			case isFileMutatingTool(tc.Tool):
				changed = true
			case firstFullRead == "" && isReadTool(tc.Tool) && isFullRead(tc.Arguments) && !resultIsReadError(conv, tc.ID):
				firstFullRead = tc.ID
			}
		}
		switch {
		case changed:
			copyCallID = ""
		case copyCallID == "":
			copyCallID = firstFullRead
		}
	}
	if copyCallID == "" {
		return false
	}
	res, _, ok := conv.ResultFor(copyCallID)
	return !ok || !apogeectx.IsPruneStub(res.Content)
}

// isFullRead reports whether a read call's arguments asked for the whole file: no readSliceKeys key
// and no locate (a locate without a range returns the windows around its hits, not the file). An
// argument set that is not a JSON object is not a full read — nothing vouches for what it returned.
func isFullRead(args json.RawMessage) bool {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return false
	}
	return !hasAnyKey(m, readSliceKeys) && !hasAnyKey(m, []string{"locate"})
}

// hasAnyKey reports whether m carries any of keys.
func hasAnyKey(m map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// invalidatesReadCache reports whether a call to toolName may have changed a file the conversation
// already read: true for every tool that is not a read tool and whose ToolDef in the guard's tool
// view does not carry ReadOnly — terminal, python_exec, every MCP tool — and for a tool the view does
// not list at all, since nothing then vouches that it left the workspace alone. It is the read
// cache's own question, deliberately separate from isFileMutatingTool, which the loop breaker, the
// intent and tool-use guards share and which names only the write tools that name their file.
func invalidatesReadCache(tools []domain.ToolDef, toolName string) bool {
	if isReadTool(toolName) {
		return false
	}
	for _, t := range tools {
		if t.Name == toolName {
			return !t.ReadOnly
		}
	}
	return true
}

// capReadArguments returns the read call's arguments with a header-only max_lines cap applied, and
// whether it changed anything. It is a no-op (ok=false) when the arguments are not a JSON object or
// already carry an explicit start_line / end_line / max_lines — a model that asked for a specific
// slice is not issuing a redundant full re-dump, and its request is left intact.
func capReadArguments(args json.RawMessage) (json.RawMessage, bool) {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return nil, false
	}
	if hasAnyKey(m, readSliceKeys) {
		return nil, false
	}
	m["max_lines"] = readCacheLines
	out, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return out, true
}

// toolDeclaresMaxLines reports whether the tool named toolName in the guard's tool view
// (LoopView.Tools()) declares a max_lines property in its argument schema. The cap is gated on this:
// a strict MCP server (additionalProperties:false) rejects an unknown max_lines argument, so a read
// tool whose schema does not carry the field — or is absent from the menu, non-object, or
// unparsable — is inspected but never mutated.
func toolDeclaresMaxLines(tools []domain.ToolDef, toolName string) bool {
	for _, t := range tools {
		if t.Name != toolName {
			continue
		}
		if len(t.Schema) == 0 {
			return false
		}
		var s struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(t.Schema, &s) != nil {
			return false
		}
		_, ok := s.Properties["max_lines"]
		return ok
	}
	return false
}
