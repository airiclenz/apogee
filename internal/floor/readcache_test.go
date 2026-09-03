package floor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// readFileTool mirrors apogee's real read_file schema — it DECLARES max_lines, so the cap has a
// field to attach to (the tool menu the guard reads through LoopView.Tools()).
var readFileTool = domain.ToolDef{
	Name:   "read_file",
	Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"start_line":{"type":"integer"},"end_line":{"type":"integer"},"max_lines":{"type":"integer"}}}`),
}

// editCall is an edit_existing_file call over path — apogee's own edit spelling, a write-since on
// the isFileMutatingTool superset though it carries no file body.
func editCall(id, path string) domain.ToolCall {
	return domaintest.Call(id, "edit_existing_file", map[string]string{"path": path, "old": "a", "new": "b"})
}

// toolResult is a committed tool result for callID.
func toolResult(callID, content string) domain.Message {
	return domaintest.ToolResultMessage(callID, content)
}

// cacheReadWithTools runs the guard once against the pending call over history, with tools as the
// menu it sees, and returns the (possibly capped) call and whether the guard reported a cap.
func cacheReadWithTools(history []domain.Message, call domain.ToolCall, tools []domain.ToolDef) (domain.ToolCall, bool) {
	c := call
	view := domain.NewRequest("m", history, tools, domain.Budget{}, 0, nil).View()
	ok := CacheRead(view, domain.NewToolCallEdit(&c))
	return c, ok
}

// cacheRead runs the guard over the default menu (apogee's read_file, whose schema declares
// max_lines).
func cacheRead(history []domain.Message, call domain.ToolCall) (domain.ToolCall, bool) {
	return cacheReadWithTools(history, call, []domain.ToolDef{readFileTool})
}

// hasMaxLines reports whether the read arguments carry a max_lines cap.
func hasMaxLines(args json.RawMessage) bool {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return false
	}
	_, ok := m["max_lines"]
	return ok
}

// A read of a file already read successfully (and not written since) is capped to a header-only
// slice, so the full content already in context is not re-dumped (apogee-sim detectCachedReread
// @pin, expressed as an argument cap because the pre-tool-exec seam shapes the pending call).
func TestCacheReadCapsRedundantReRead(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
		assistantCall(readCall("r2", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r2", "a.go"))
	if !ok {
		t.Error("CacheRead reported no cap on a redundant re-read")
	}
	if !hasMaxLines(got.Arguments) {
		t.Errorf("redundant re-read not capped; args = %s", got.Arguments)
	}
}

// A read of a file not read before is untouched — a novel read is legitimate work.
func TestCacheReadLeavesNovelReadUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit b.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(readCall("r2", "b.go")),
	}
	call := readCall("r2", "b.go")
	got, ok := cacheRead(history, call)
	if ok {
		t.Error("CacheRead reported a cap on a novel read")
	}
	if string(got.Arguments) != string(call.Arguments) {
		t.Errorf("novel read arguments mutated: %s vs %s", got.Arguments, call.Arguments)
	}
}

// A file written after its last successful read may have changed, so re-reading it is not
// redundant — the guard leaves it alone (the "unchanged path" strengthening over the sim).
func TestCacheReadLeavesWrittenSinceUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(writeCall("w1", "a.go")),
		toolResult("w1", "ok"),
		assistantCall(readCall("r2", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r2", "a.go"))
	if ok || hasMaxLines(got.Arguments) {
		t.Errorf("a re-read after a write was capped; the file may have changed. args = %s", got.Arguments)
	}
}

// The guard must NOT cap a re-read of a file EDITED after its last read — the edit may have changed
// the file, so its cached copy is stale. This holds only because isFileMutatingTool counts
// edit_existing_file as a write-since (the 2026-08-10 write-detection pin, moved here with the
// subject it pins).
func TestCacheReadLeavesEditedSinceUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(editCall("e1", "a.go")),
		toolResult("e1", "edited a.go"),
		assistantCall(readCall("r2", "a.go")),
	}
	got, ok := cacheRead(history, readCall("r2", "a.go"))
	if ok || hasMaxLines(got.Arguments) {
		t.Errorf("a re-read after an edit was capped; the cached copy is stale. args = %s", got.Arguments)
	}
}

// A targeted read (an explicit line range/limit) is not a redundant full re-dump — it is left
// intact, the model's own bound untouched.
func TestCacheReadLeavesRangedReadUntouched(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a"),
		assistantCall(readCall("r2", "a.go")),
	}
	ranged := domain.ToolCall{ID: "r2", Tool: "read_file", Arguments: json.RawMessage(`{"path":"a.go","max_lines":50}`)}
	got, ok := cacheRead(history, ranged)
	if ok {
		t.Error("CacheRead reported a cap on a ranged read")
	}
	if string(got.Arguments) != string(ranged.Arguments) {
		t.Errorf("a ranged read was mutated: %s vs %s", got.Arguments, ranged.Arguments)
	}
	if !strings.Contains(string(got.Arguments), "50") {
		t.Error("the model's explicit max_lines was overwritten")
	}
}

// An MCP-style read tool whose argument schema does NOT declare max_lines (a strict server with
// additionalProperties:false) is inspected but never mutated — appending max_lines would hand it an
// argument it rejects, so the redundant re-read proceeds uncapped.
func TestCacheReadSkipsToolWithoutMaxLinesSchema(t *testing.T) {
	t.Parallel()
	mcpRead := func(id, path string) domain.ToolCall {
		args, _ := json.Marshal(map[string]string{"path": path})
		return domain.ToolCall{ID: id, Tool: "readFile", Arguments: args}
	}
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(mcpRead("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
		assistantCall(mcpRead("r2", "a.go")),
	}
	mcpReadTool := domain.ToolDef{
		Name:   "readFile",
		Schema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"path":{"type":"string"}}}`),
	}
	call := mcpRead("r2", "a.go")
	got, ok := cacheReadWithTools(history, call, []domain.ToolDef{mcpReadTool})
	if ok || hasMaxLines(got.Arguments) {
		t.Errorf("a read tool without a max_lines schema was capped; args = %s", got.Arguments)
	}
	if string(got.Arguments) != string(call.Arguments) {
		t.Errorf("arguments mutated: %s vs %s", got.Arguments, call.Arguments)
	}
}

// toolDeclaresMaxLines has three conservative fallbacks that all withhold the cap: the pending read
// tool is (a) absent from the tool menu (the realistic case — toolfilter narrowing removed it from
// Tools()), (b) present with an empty schema, or (c) present with a schema that does not parse. In
// each, max_lines cannot be confirmed as a declared property, and appending it might hand a strict
// tool an argument it rejects — so a genuine redundant re-read is left byte-identical.
func TestCacheReadSchemaGateConservativeFallbacks(t *testing.T) {
	t.Parallel()
	// a.go was read successfully earlier and not written since, so the read below is genuinely
	// redundant: only the schema gate stands between it and a cap.
	history := []domain.Message{
		userMsg("edit a.go"),
		assistantCall(readCall("r1", "a.go")),
		toolResult("r1", "package a\nfunc F() {}"),
		assistantCall(readCall("r2", "a.go")),
	}
	// otherTool stands in for a narrowed menu that no longer carries the pending read tool.
	otherTool := domain.ToolDef{
		Name:   "list_dir",
		Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}
	cases := []struct {
		name  string
		tools []domain.ToolDef
	}{
		{"absent from the menu", []domain.ToolDef{otherTool}},
		{"present with an empty schema", []domain.ToolDef{{Name: "read_file"}}},
		{"present with malformed schema JSON", []domain.ToolDef{{Name: "read_file", Schema: json.RawMessage(`{"type":"object","properties":`)}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			call := readCall("r2", "a.go")
			got, ok := cacheReadWithTools(history, call, tc.tools)
			if ok || hasMaxLines(got.Arguments) {
				t.Errorf("redundant re-read capped despite an unconfirmed schema; args = %s", got.Arguments)
			}
			if string(got.Arguments) != string(call.Arguments) {
				t.Errorf("arguments mutated: %s vs %s", got.Arguments, call.Arguments)
			}
		})
	}
}
