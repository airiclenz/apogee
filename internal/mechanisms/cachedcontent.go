package mechanisms

import (
	"context"
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// cached_content_intercept registers the redundant-successful-re-read interceptor in the catalogue
// constructor table (Phase-4 item 11, Wave 3 history-aware family). Default-off (D1). It is ported
// from apogee-sim internal/proxy/cached_content_intercept.go @pin and RELOCATED to pre-tool-exec
// (catalogue Table A / hook-mutation-api §5): the decision "this read is redundant" is made from the
// history before the read runs, rather than by rewriting the result after it ran.
//
// FIDELITY NOTE (2026-07-04): the sim intervenes AFTER execution — it replaces the redundant read's
// RESULT with a short "[ALREADY IN CONTEXT] … do not read this file again, proceed" stub
// (buildCachedContentStub @pin). apogee's pre-tool-exec hook surface can only mutate the pending
// *ToolCall (there is no result-substitution or execution-skip primitive, and adding one is outside
// this item's confined diff). So the port expresses the SAME token-saving intent — the file content
// is already in the conversation, so re-dumping it wastes the window — by CAPPING the redundant
// read to a header-only slice (a max_lines cap on the arguments), leaving the model's existing copy
// as the source of truth. The sim's "proceed to the next step" directive is not carried at this
// hook; that guidance is delivered instead by read_repeat (post-response, ActionRetry), which is
// declared IncompatibleWith this Mechanism — the two are alternatives on the same symptom (C2), one
// a silent exec-time token cap, the other a directive-bearing retry. The cap couples to the
// read-file argument schema (max_lines): before mutating, the hook looks the pending tool up in
// view.Tools() and caps ONLY when its schema declares a max_lines property, so a read tool lacking it
// (e.g. a strict MCP server with additionalProperties:false) is inspected but never handed an
// argument it would reject — the re-read proceeds uncapped, a genuine no-op rather than a hoped-for
// one. Surfaced as a divergence-from-source; default-off and bench-gated (D1), so nothing un-vetted
// ships.
func init() {
	catalogue[cachedContentInterceptID] = newCachedContentIntercept
	descriptors[cachedContentInterceptID] = cachedContentDescriptor
}

const cachedContentInterceptID domain.MechanismID = "cached_content_intercept"

// cachedContentReadCap is the max_lines the redundant re-read is capped to — the content is already
// in context, so a header-only slice loses nothing while reclaiming the window the full re-dump
// would have cost.
const cachedContentReadCap = 1

// cachedContentMechanism is the pre-tool-exec Mechanism that caps a redundant re-read (catalogue
// Table A `cached_content_intercept`). It carries no per-Mechanism state; strikes-3 self-regulation
// routes through the loop's per-Session tracker (item 3).
type cachedContentMechanism struct{}

// newCachedContentIntercept builds the cached_content_intercept Mechanism. It needs no injected Deps
// (D3): redundancy is decided from the pending call and the conversation on the LoopView it is
// handed.
func newCachedContentIntercept(Deps) (domain.Mechanism, error) { return cachedContentMechanism{}, nil }

// cachedContentDescriptor identifies cached_content_intercept as a strikes-3 proactive-nudge
// Mechanism (catalogue Table A), incompatible with read_loop and read_repeat (the re-read family is
// pairwise-exclusive, C2 — in apogee a startup gate, so at most one of the three is enabled at a
// time). Disabled under Bypass (D5), withdrawn after repeated non-help.
var cachedContentDescriptor = domain.MechanismDescriptor{
	ID:               cachedContentInterceptID,
	Capability:       domain.CapProactiveNudge,
	Suppression:      domain.SuppressStrikesThree,
	IncompatibleWith: []domain.MechanismID{readLoopID, readRepeatID},
}

// Descriptor returns cached_content_intercept's static catalogue descriptor.
func (cachedContentMechanism) Descriptor() domain.MechanismDescriptor { return cachedContentDescriptor }

// Ordering declares no positive edge (the incompatibility edges above are the only constraint):
// cached_content_intercept is the sole pre-tool-exec Mechanism in this wave.
func (cachedContentMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{}
}

// PreToolExec caps a redundant re-read of an unchanged file (apogee-sim detectCachedReread @pin,
// relocated). It is a no-op — booking no fire (the loop keys the acted fire on the ToolCall
// changing, R4) — when the pending call is not a read, has no path, targets a file not read
// successfully before (or written since — the file may have changed), the pending tool's schema
// does not declare a max_lines property (the cap has nothing to attach to), the read already has an
// explicit line range/limit (a targeted read is not a redundant full re-dump), or the arguments are
// not a JSON object.
func (cachedContentMechanism) PreToolExec(_ context.Context, call *domain.ToolCall, view domain.LoopView) error {
	if !isReadTool(call.Tool) {
		return nil
	}
	rawPath := toolCallPath(call.Arguments)
	if rawPath == "" {
		return nil
	}
	if !priorSuccessfulReadUnchanged(view.Conversation(), normalizePath(rawPath), call.ID) {
		return nil
	}
	if !toolDeclaresMaxLines(view.Tools(), call.Tool) {
		return nil
	}
	if capped, ok := capReadArguments(call.Arguments); ok {
		call.Arguments = capped
	}
	return nil
}

// priorSuccessfulReadUnchanged reports whether path np was read successfully in an earlier Turn and
// not written since (apogee-sim detectCachedReread's "earlier successful read of the same path"
// @pin, strengthened to honour the item's "unchanged path": the sim omitted the write-since check,
// but capping a file that was modified after the earlier read would drop real content, so apogee
// skips a path written after its last successful read). The pending call (currentCallID) is
// excluded — its own assistant message is already committed to history when pre-tool-exec runs.
func priorSuccessfulReadUnchanged(conv domain.ConversationView, np, currentCallID string) bool {
	lastSuccessfulRead := -1
	lastWrite := -1
	for i := 0; i < conv.Len(); i++ {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == currentCallID {
				continue
			}
			p := toolCallPath(tc.Arguments)
			if p == "" || normalizePath(p) != np {
				continue
			}
			switch {
			case isFileMutatingTool(tc.Tool):
				lastWrite = i
			case isReadTool(tc.Tool) && !resultIsReadError(conv, tc.ID):
				lastSuccessfulRead = i
			}
		}
	}
	return lastSuccessfulRead >= 0 && lastWrite < lastSuccessfulRead
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
	for _, k := range []string{"start_line", "end_line", "max_lines"} {
		if _, ok := m[k]; ok {
			return nil, false
		}
	}
	m["max_lines"] = cachedContentReadCap
	out, err := json.Marshal(m)
	if err != nil {
		return nil, false
	}
	return out, true
}

// toolDeclaresMaxLines reports whether the tool named toolName in the hook's tool view (view.Tools())
// declares a max_lines property in its argument schema. The cap is gated on this: a strict MCP server
// (additionalProperties:false) rejects an unknown max_lines argument, so a read tool whose schema does
// not carry the field — or is absent / non-object / unparsable — is inspected but never mutated (no
// mutation ⇒ no fire, R4), making the "benign no-op" fidelity note literally true rather than reliant
// on the third-party tool silently tolerating an unknown field.
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
